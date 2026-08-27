// Context Atlas serves the API and compiled SPA from one process.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/google/uuid"

	"github.com/supercakecrumb/context-atlas/internal/api"
	"github.com/supercakecrumb/context-atlas/internal/atlas"
	"github.com/supercakecrumb/context-atlas/internal/auth"
	"github.com/supercakecrumb/context-atlas/internal/config"
	"github.com/supercakecrumb/context-atlas/internal/feedback"
	"github.com/supercakecrumb/context-atlas/internal/scheduler"
	"github.com/supercakecrumb/context-atlas/internal/store"
	"github.com/supercakecrumb/context-atlas/internal/telegram"
	"github.com/supercakecrumb/context-atlas/internal/webapp"
)

const healthcheckTimeout = 3 * time.Second

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "context-atlas:", err)
		os.Exit(1)
	}
}

func run(args []string, stdout io.Writer) error {
	switch {
	case len(args) == 0:
		return serve()
	case len(args) == 1 && args[0] == "openapi":
		return writeOpenAPI(stdout)
	case len(args) == 1 && args[0] == "--healthcheck":
		return healthcheck(context.Background())
	default:
		return errors.New("usage: context-atlas [openapi|--healthcheck]")
	}
}

// writeOpenAPI deliberately constructs only the API schema, so code generation
// never requires database credentials or a running Postgres instance.
func writeOpenAPI(w io.Writer) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(api.New(api.Options{}).OpenAPI())
}

func serve() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: cfg.LogLevel}))
	slog.SetDefault(logger)

	database, err := store.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer database.Close()
	if err := database.Migrate(ctx); err != nil {
		return err
	}

	atlasService, err := atlas.New(database.Pool(), cfg.ReferenceDir, logger)
	if err != nil {
		return err
	}
	defer atlasService.Close()
	if err := atlasService.Seed(ctx); err != nil {
		return err
	}

	handler, staleAlert, err := applicationHandler(ctx, cfg, database, atlasService, logger)
	if err != nil {
		return err
	}
	server := &http.Server{
		Addr:              ":" + strconv.Itoa(cfg.Port),
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      2 * time.Minute,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}

	go scheduler.Scheduler{
		Refresher:  atlasService,
		Logger:     logger,
		StaleAge:   cfg.RefreshInterval,
		StaleAlert: staleAlert,
		HourUTC:    cfg.RefreshHourUTC,
		MinuteUTC:  cfg.RefreshMinuteUTC,
	}.Run(ctx)

	errCh := make(chan error, 1)
	go func() { errCh <- server.ListenAndServe() }()
	logger.Info("HTTP server started", "addr", server.Addr)

	select {
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("graceful HTTP shutdown: %w", err)
		}
		logger.Info("HTTP server stopped")
		return nil
	}
}

func applicationHandler(ctx context.Context, cfg config.Config, database *store.Store, atlasService *atlas.Service, logger *slog.Logger) (http.Handler, func(context.Context, string) error, error) {
	spa, err := spaHandler(cfg.WebDistDir)
	if err != nil {
		return nil, nil, err
	}

	opts := api.Options{
		Catalog:      atlasService,
		Geographies:  atlasService,
		Observations: atlasService,
		Associations: atlasService,
		Health:       atlasService,
		Imports:      atlasService,
		Logger:       logger,
	}
	var staleAlert func(context.Context, string) error
	if cfg.SnagboxEnabled() {
		adapter := feedbackAdapter{reporter: feedback.New(cfg.SnagboxURL, cfg.SnagboxToken)}
		opts.Feedback = adapter
		staleAlert = adapter.AlertStaleData
	}

	login := spa
	if cfg.AuthEnabled() {
		ownerID := onlyOwnerID(cfg.AdminTelegramIDs)
		service, err := auth.NewService(database.Pool(), cfg.PublicBaseURL, cfg.SessionKey)
		if err != nil {
			return nil, nil, err
		}
		handler, err := auth.NewHandler(service, service.Sessions, service.Sessions, cfg.SessionKey, cfg.PublicBaseURL, strconv.FormatInt(ownerID, 10))
		if err != nil {
			return nil, nil, err
		}
		opts.AdminAuth = handler.RequireAdmin
		opts.Sessions = adminSessionAdapter{
			sessions: service.Sessions,
			csrfKey:  cfg.SessionKey,
			ownerID:  ownerID,
			secure:   strings.HasPrefix(strings.ToLower(cfg.PublicBaseURL), "https://"),
		}
		bot, err := telegram.New(cfg.TelegramBotToken, ownerID, service, logger)
		if err != nil {
			return nil, nil, err
		}
		go bot.Start(ctx)
		login = handler.Login(spa)
	}

	apiServer := api.New(opts)
	mux := http.NewServeMux()
	mux.Handle("/api/", apiServer)
	mux.Handle(api.HealthPath, apiServer)
	mux.Handle("/login", login)
	mux.Handle("/", spa)
	return mux, staleAlert, nil
}

func spaHandler(directory string) (http.Handler, error) {
	root := os.DirFS(directory)
	if _, err := fs.Stat(root, "index.html"); err != nil {
		return nil, fmt.Errorf("load compiled web app: %w", err)
	}
	return webapp.New(root), nil
}

func onlyOwnerID(ids map[int64]struct{}) int64 {
	for id := range ids {
		return id
	}
	return 0
}

func healthcheck(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, healthcheckTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, healthURL(), nil)
	if err != nil {
		return err
	}
	response, err := (&http.Client{}).Do(req)
	if err != nil {
		return fmt.Errorf("request health endpoint: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("health endpoint returned %s", response.Status)
	}
	return nil
}

func healthPort() int {
	port, err := strconv.Atoi(strings.TrimSpace(os.Getenv("PORT")))
	if err != nil || port < 1 || port > 65535 {
		return 8080
	}
	return port
}

func healthURL() string {
	return "http://127.0.0.1:" + strconv.Itoa(healthPort()) + api.HealthPath
}

type feedbackAdapter struct {
	reporter feedback.Reporter
}

func (a feedbackAdapter) SubmitFeedback(ctx context.Context, request api.FeedbackRequest) (api.FeedbackReceipt, error) {
	if err := a.reporter.Report(ctx, request.Message, request.PageURL, ""); err != nil {
		if errors.Is(err, feedback.ErrDisabled) {
			return api.FeedbackReceipt{}, api.ErrUnavailable
		}
		return api.FeedbackReceipt{}, err
	}
	return api.FeedbackReceipt{ID: uuid.NewString(), ReceivedAt: time.Now().UTC()}, nil
}

func (a feedbackAdapter) AlertStaleData(ctx context.Context, message string) error {
	return a.reporter.Report(ctx, message, "/admin#data-freshness", "scheduler")
}

type adminSessionAdapter struct {
	sessions interface {
		Revoke(context.Context, string) error
	}
	csrfKey []byte
	ownerID int64
	secure  bool
}

func (a adminSessionAdapter) Session(ctx context.Context) (api.AdminSession, error) {
	session, ok := auth.SessionFromContext(ctx)
	if !ok || session.SubjectID != strconv.FormatInt(a.ownerID, 10) {
		return api.AdminSession{}, api.ErrForbidden
	}
	return api.AdminSession{
		OwnerTelegramID: a.ownerID,
		ExpiresAt:       session.ExpiresAt.UTC(),
		CSRFToken:       auth.CSRFTokenFor(a.csrfKey, session.Token),
	}, nil
}

func (a adminSessionAdapter) Logout(ctx context.Context) (http.Cookie, error) {
	session, ok := auth.SessionFromContext(ctx)
	if !ok || session.SubjectID != strconv.FormatInt(a.ownerID, 10) {
		return http.Cookie{}, api.ErrForbidden
	}
	if err := a.sessions.Revoke(ctx, session.Token); err != nil {
		return http.Cookie{}, err
	}
	return http.Cookie{
		Name:     auth.SessionCookieName,
		Path:     "/",
		Expires:  time.Unix(0, 0),
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   a.secure,
		SameSite: http.SameSiteLaxMode,
	}, nil
}
