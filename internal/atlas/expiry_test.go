package atlas

import (
	"bytes"
	"testing"
	"time"
)

func TestExpirePreviewsClearsOverduePreviewArtifacts(t *testing.T) {
	service, ctx := integrationAtlas(t)
	now := time.Date(2026, time.August, 27, 12, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }

	createPreview := func(expiresAt time.Time, raw []byte) int64 {
		t.Helper()
		job, err := service.insertJob(ctx, nil, "preview", "queued", "https://data.who.int/", &expiresAt)
		if err != nil {
			t.Fatal(err)
		}
		if err := service.stageJob(ctx, job.ID, "preview_ready", "https://example.invalid/data.csv", raw, nil, nil); err != nil {
			t.Fatal(err)
		}
		return job.ID
	}

	overdueID := createPreview(now.Add(-time.Minute), []byte("overdue"))
	freshRaw := []byte("fresh")
	freshID := createPreview(now.Add(time.Minute), freshRaw)
	if err := service.ExpirePreviews(ctx); err != nil {
		t.Fatal(err)
	}

	var overdueState string
	var overdueRaw []byte
	var overdueFinished bool
	if err := service.pool.QueryRow(ctx, `
		SELECT state, staged_raw_csv, finished_at IS NOT NULL
		FROM import_job WHERE id = $1`, overdueID).Scan(&overdueState, &overdueRaw, &overdueFinished); err != nil {
		t.Fatal(err)
	}
	if overdueState != "expired" || overdueRaw != nil || !overdueFinished {
		t.Fatalf("overdue preview = state %q, raw %q, finished %t; want expired, NULL, true", overdueState, overdueRaw, overdueFinished)
	}

	var freshState string
	var freshStored []byte
	var freshFinished bool
	if err := service.pool.QueryRow(ctx, `
		SELECT state, staged_raw_csv, finished_at IS NOT NULL
		FROM import_job WHERE id = $1`, freshID).Scan(&freshState, &freshStored, &freshFinished); err != nil {
		t.Fatal(err)
	}
	if freshState != "preview_ready" || !bytes.Equal(freshStored, freshRaw) || freshFinished {
		t.Fatalf("fresh preview = state %q, raw %q, finished %t; want preview_ready, %q, false", freshState, freshStored, freshFinished, freshRaw)
	}
}
