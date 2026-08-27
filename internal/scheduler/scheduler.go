package scheduler

import (
	"context"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/supercakecrumb/context-atlas/internal/api"
)

type Refresher interface {
	ExpirePreviews(context.Context) error
	Freshness(context.Context) (api.FreshnessResult, error)
	LastSuccessfulRefresh(context.Context) (time.Time, error)
	MarkInterruptedFailed(context.Context) error
	RefreshAll(context.Context, string) error
}

type Scheduler struct {
	Refresher  Refresher
	Logger     *slog.Logger
	StaleAge   time.Duration
	StaleAlert func(context.Context, string) error
	HourUTC    int
	MinuteUTC  int
	Now        func() time.Time
}

func (s Scheduler) Run(ctx context.Context) {
	if s.Refresher == nil {
		return
	}
	if s.Logger == nil {
		s.Logger = slog.Default()
	}
	if s.Now == nil {
		s.Now = time.Now
	}
	if s.StaleAge <= 0 {
		s.StaleAge = 24 * time.Hour
	}
	if err := s.Refresher.MarkInterruptedFailed(ctx); err != nil {
		s.Logger.Error("mark interrupted refresh jobs failed", "error", err)
	}
	s.expirePreviews(ctx)
	episode := staleEpisode{}
	last, err := s.Refresher.LastSuccessfulRefresh(ctx)
	if err != nil {
		s.Logger.Error("read last refresh time failed", "error", err)
	}
	s.checkStale(ctx, &episode)
	if err == nil && (last.IsZero() || s.Now().UTC().Sub(last.UTC()) > s.StaleAge) {
		s.refresh(ctx, "startup_catchup")
		s.checkStale(ctx, &episode)
	}

	now := s.Now().UTC()
	daily := time.NewTimer(NextDaily(now, s.HourUTC, s.MinuteUTC).Sub(now))
	defer daily.Stop()
	cleanup := time.NewTicker(time.Hour)
	defer cleanup.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-cleanup.C:
			s.expirePreviews(ctx)
		case <-daily.C:
			s.checkStale(ctx, &episode)
			s.refresh(ctx, "scheduled")
			s.checkStale(ctx, &episode)
			now = s.Now().UTC()
			daily.Reset(NextDaily(now, s.HourUTC, s.MinuteUTC).Sub(now))
		}
	}
}

func (s Scheduler) expirePreviews(ctx context.Context) {
	if err := s.Refresher.ExpirePreviews(ctx); err != nil {
		s.Logger.Error("expire preview artifacts failed", "error", err)
	}
}

func (s Scheduler) refresh(ctx context.Context, trigger string) {
	if err := s.Refresher.RefreshAll(ctx, trigger); err != nil {
		s.Logger.Error("dataset refresh failed", "trigger", trigger, "error", err)
		return
	}
	s.Logger.Info("dataset refresh completed", "trigger", trigger)
}

type staleEpisode struct{ alerted bool }

// checkStale owns episode suppression; atlas.Freshness owns the fixed 72-hour
// per-dataset policy so partial catalog refreshes cannot hide stale data.
func (s Scheduler) checkStale(ctx context.Context, episode *staleEpisode) {
	freshness, err := s.Refresher.Freshness(ctx)
	if err != nil {
		s.Logger.Error("read dataset freshness failed", "error", err)
		return
	}
	datasets := make([]string, 0, len(freshness.Datasets))
	for _, dataset := range freshness.Datasets {
		if dataset.Stale {
			datasets = append(datasets, dataset.DatasetID)
		}
	}
	if len(datasets) == 0 {
		episode.alerted = false
		return
	}
	if episode.alerted {
		return
	}
	episode.alerted = true
	sort.Strings(datasets)
	s.Logger.Warn("WHO catalog data is stale", "datasets", datasets, "stale_threshold", "72h")
	if s.StaleAlert != nil {
		if err := s.StaleAlert(ctx, staleAlertMessage(datasets)); err != nil {
			s.Logger.Error("WHO catalog stale alert failed", "error", err)
		}
	}
}

func staleAlertMessage(datasets []string) string {
	return "Context Atlas WHO data is stale for: " + strings.Join(datasets, ", ") + "."
}

func NextDaily(now time.Time, hour, minute int) time.Time {
	now = now.UTC()
	next := time.Date(now.Year(), now.Month(), now.Day(), hour, minute, 0, 0, time.UTC)
	if !next.After(now) {
		next = next.Add(24 * time.Hour)
	}
	return next
}
