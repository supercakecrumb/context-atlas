package config

import (
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	defaultPort          = 8080
	defaultRefresh       = 24 * time.Hour
	defaultRefreshHour   = 2
	defaultRefreshMinute = 15
)

type Config struct {
	DatabaseURL      string
	Port             int
	LogLevel         slog.Level
	PublicBaseURL    string
	WebDistDir       string
	ReferenceDir     string
	TelegramBotToken string
	AdminTelegramIDs map[int64]struct{}
	SessionKey       []byte
	SnagboxURL       string
	SnagboxToken     string
	RefreshInterval  time.Duration
	RefreshHourUTC   int
	RefreshMinuteUTC int
}

func Load() (Config, error) {
	cfg := Config{
		DatabaseURL:      strings.TrimSpace(os.Getenv("DATABASE_URL")),
		PublicBaseURL:    strings.TrimRight(strings.TrimSpace(os.Getenv("PUBLIC_BASE_URL")), "/"),
		WebDistDir:       envOr("WEB_DIST_DIR", "web/dist"),
		ReferenceDir:     envOr("REFERENCE_DIR", "assets/reference"),
		TelegramBotToken: strings.TrimSpace(os.Getenv("TELEGRAM_BOT_TOKEN")),
		SnagboxURL:       strings.TrimRight(strings.TrimSpace(os.Getenv("SNAGBOX_URL")), "/"),
		SnagboxToken:     strings.TrimSpace(os.Getenv("SNAGBOX_INGEST_TOKEN")),
	}

	var err error
	if cfg.DatabaseURL == "" {
		return Config{}, errors.New("DATABASE_URL is required")
	}
	if cfg.Port, err = intEnv("PORT", defaultPort, 1, 65535); err != nil {
		return Config{}, err
	}
	if cfg.LogLevel, err = logLevel(envOr("LOG_LEVEL", "info")); err != nil {
		return Config{}, err
	}
	if cfg.RefreshInterval, err = durationEnv("REFRESH_INTERVAL", defaultRefresh); err != nil {
		return Config{}, err
	}
	if cfg.RefreshHourUTC, err = intEnv("REFRESH_HOUR_UTC", defaultRefreshHour, 0, 23); err != nil {
		return Config{}, err
	}
	if cfg.RefreshMinuteUTC, err = intEnv("REFRESH_MINUTE_UTC", defaultRefreshMinute, 0, 59); err != nil {
		return Config{}, err
	}
	if cfg.AdminTelegramIDs, err = parseIDs(os.Getenv("ADMIN_TG_IDS")); err != nil {
		return Config{}, err
	}
	if raw := strings.TrimSpace(os.Getenv("SESSION_ENC_KEY")); raw != "" {
		cfg.SessionKey, err = decodeKey(raw)
		if err != nil {
			return Config{}, err
		}
	}

	authConfigured := cfg.TelegramBotToken != "" || len(cfg.AdminTelegramIDs) > 0 || len(cfg.SessionKey) > 0
	if authConfigured && (cfg.TelegramBotToken == "" || len(cfg.AdminTelegramIDs) == 0 || len(cfg.SessionKey) < 32) {
		return Config{}, errors.New("TELEGRAM_BOT_TOKEN, ADMIN_TG_IDS, and a 32-byte SESSION_ENC_KEY must be configured together")
	}
	if authConfigured && len(cfg.AdminTelegramIDs) != 1 {
		return Config{}, errors.New("ADMIN_TG_IDS must contain exactly one owner Telegram ID")
	}
	if (cfg.SnagboxURL == "") != (cfg.SnagboxToken == "") {
		return Config{}, errors.New("SNAGBOX_URL and SNAGBOX_INGEST_TOKEN must be configured together")
	}
	return cfg, nil
}

func (c Config) AuthEnabled() bool { return c.TelegramBotToken != "" }

func (c Config) SnagboxEnabled() bool { return c.SnagboxURL != "" }

func envOr(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func intEnv(name string, fallback, minValue, maxValue int) (int, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < minValue || value > maxValue {
		return 0, fmt.Errorf("%s must be an integer from %d to %d", name, minValue, maxValue)
	}
	return value, nil
}

func durationEnv(name string, fallback time.Duration) (time.Duration, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback, nil
	}
	value, err := time.ParseDuration(raw)
	if err != nil || value <= 0 {
		return 0, fmt.Errorf("%s must be a positive duration", name)
	}
	return value, nil
}

func logLevel(raw string) (slog.Level, error) {
	var level slog.Level
	if err := level.UnmarshalText([]byte(strings.ToLower(strings.TrimSpace(raw)))); err != nil {
		return 0, fmt.Errorf("LOG_LEVEL: %w", err)
	}
	return level, nil
}

func parseIDs(raw string) (map[int64]struct{}, error) {
	ids := make(map[int64]struct{})
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		id, err := strconv.ParseInt(part, 10, 64)
		if err != nil || id <= 0 {
			return nil, fmt.Errorf("ADMIN_TG_IDS contains invalid Telegram ID %q", part)
		}
		ids[id] = struct{}{}
	}
	return ids, nil
}

func decodeKey(raw string) ([]byte, error) {
	decoded, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		decoded = []byte(raw)
	}
	if len(decoded) < 32 {
		return nil, errors.New("SESSION_ENC_KEY must contain at least 32 bytes (raw or base64)")
	}
	return decoded, nil
}
