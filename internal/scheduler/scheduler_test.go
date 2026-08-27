package scheduler

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/supercakecrumb/context-atlas/internal/api"
)

func TestNextDaily(t *testing.T) {
	for _, tc := range []struct {
		now  string
		want string
	}{
		{now: "2026-08-27T01:00:00Z", want: "2026-08-27T02:15:00Z"},
		{now: "2026-08-27T02:15:00Z", want: "2026-08-28T02:15:00Z"},
		{now: "2026-08-27T19:00:00+03:00", want: "2026-08-28T02:15:00Z"},
	} {
		now, err := time.Parse(time.RFC3339, tc.now)
		if err != nil {
			t.Fatal(err)
		}
		if got := NextDaily(now, 2, 15).Format(time.RFC3339); got != tc.want {
			t.Fatalf("NextDaily(%s) = %s, want %s", tc.now, got, tc.want)
		}
	}
}

func TestStaleEpisodeAlertsOnceAndResetsAfterFresh(t *testing.T) {
	refresher := &staleRefresher{freshness: api.FreshnessResult{Datasets: []api.DatasetFreshness{{DatasetID: "suicide-mortality", Stale: true}}}}
	var logs bytes.Buffer
	var alerts []string
	scheduler := Scheduler{
		Refresher: refresher,
		Logger:    slog.New(slog.NewTextHandler(&logs, nil)),
		StaleAlert: func(_ context.Context, message string) error {
			alerts = append(alerts, message)
			return nil
		},
	}
	episode := staleEpisode{}

	scheduler.checkStale(context.Background(), &episode)
	scheduler.checkStale(context.Background(), &episode)
	if len(alerts) != 1 || strings.Count(logs.String(), "WHO catalog data is stale") != 1 {
		t.Fatalf("first stale episode alerts=%d logs=%q, want one each", len(alerts), logs.String())
	}

	refresher.freshness = api.FreshnessResult{Datasets: []api.DatasetFreshness{{DatasetID: "suicide-mortality"}}}
	scheduler.checkStale(context.Background(), &episode)
	refresher.freshness = api.FreshnessResult{Datasets: []api.DatasetFreshness{{DatasetID: "suicide-mortality", Stale: true}}}
	scheduler.checkStale(context.Background(), &episode)
	if len(alerts) != 2 || strings.Count(logs.String(), "WHO catalog data is stale") != 2 {
		t.Fatalf("second stale episode alerts=%d logs=%q, want two each", len(alerts), logs.String())
	}
}

func TestExpirePreviewsDelegatesAndLogsFailure(t *testing.T) {
	refresher := &staleRefresher{}
	var logs bytes.Buffer
	scheduler := Scheduler{
		Refresher: refresher,
		Logger:    slog.New(slog.NewTextHandler(&logs, nil)),
	}

	scheduler.expirePreviews(context.Background())
	if refresher.expireCalls != 1 {
		t.Fatalf("expiry calls = %d, want 1", refresher.expireCalls)
	}
	refresher.expireErr = errors.New("database unavailable")
	scheduler.expirePreviews(context.Background())
	if refresher.expireCalls != 2 || !strings.Contains(logs.String(), "expire preview artifacts failed") {
		t.Fatalf("expiry calls/logs = %d/%q, want 2 and failure log", refresher.expireCalls, logs.String())
	}
}

type staleRefresher struct {
	freshness   api.FreshnessResult
	expireCalls int
	expireErr   error
}

func (f *staleRefresher) Freshness(context.Context) (api.FreshnessResult, error) {
	return f.freshness, nil
}
func (*staleRefresher) LastSuccessfulRefresh(context.Context) (time.Time, error) {
	return time.Time{}, nil
}
func (f *staleRefresher) ExpirePreviews(context.Context) error {
	f.expireCalls++
	return f.expireErr
}
func (*staleRefresher) MarkInterruptedFailed(context.Context) error { return nil }
func (*staleRefresher) RefreshAll(context.Context, string) error    { return nil }
