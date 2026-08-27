package atlas

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/supercakecrumb/context-atlas/internal/api"
	"github.com/supercakecrumb/context-atlas/internal/reference"
	"github.com/supercakecrumb/context-atlas/internal/store"
	"github.com/supercakecrumb/context-atlas/internal/who"
)

func TestNormalizeGroupsDropsLegacySyntheticRoot(t *testing.T) {
	parent := "m49:000"
	groups, err := normalizeGroups([]reference.Group{
		{ID: "m49:000", Name: "Legacy root", Kind: "global"},
		{ID: "m49:001", Name: "World", Kind: "global", ParentID: &parent, MemberM49: []string{"004"}},
		{ID: "un:ldc", Name: "Least developed countries", Kind: "un_designation", MemberM49: []string{"004"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 2 {
		t.Fatalf("groups = %#v, want two public groups", groups)
	}
	if groups[0].id != "m49:001" || groups[0].parent != "" || groups[0].kind != "world" {
		t.Fatalf("legacy root normalization = %#v", groups[0])
	}
	if groups[1].id != "un:ldc" || groups[1].kind != "ldc" {
		t.Fatalf("UN designation normalization = %#v", groups[1])
	}
}

func TestPreviewRecordContainsSummaryNotObservations(t *testing.T) {
	preview := who.Preview{
		Observations: []who.Observation{{SourceRowKey: "must-not-be-persisted"}},
		Schema:       who.Schema{Headers: []string{"YEAR", "VALUE"}},
	}
	record := previewRecord{Version: 1, Summary: summarizePreview(preview)}
	encoded, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encoded, []byte(`"observations"`)) || bytes.Contains(encoded, []byte("must-not-be-persisted")) {
		t.Fatalf("preview payload retained parsed observations: %s", encoded)
	}
}

func TestConfirmPreviewPublishesSnapshotAndClearsStaging(t *testing.T) {
	service, ctx := integrationAtlas(t)
	if err := service.Seed(ctx); err != nil {
		t.Fatal(err)
	}
	definition, ok := definitionForID("suicide-mortality")
	if !ok {
		t.Fatal("missing curated suicide definition")
	}
	raw := []byte(`IND_ID,IND_CODE,IND_UUID,IND_PER_CODE,DIM_TIME,DIM_TIME_TYPE,DIM_GEO_CODE_M49,DIM_GEO_CODE_TYPE,DIM_PUBLISH_STATE_CODE,IND_NAME,GEO_NAME_SHORT,DIM_SEX,DIM_AGE,RATE_PER_100000_N,RATE_PER_100000_NL,RATE_PER_100000_NU
16BBF41,SDGSUICIDE,16BBF41,SDGSUICIDE,2020,YEAR,4,COUNTRY,PUBLISHED,Suicide mortality,Afghanistan,FEMALE,TOTAL,0,0,0
16BBF41,SDGSUICIDE,16BBF41,SDGSUICIDE,2021,YEAR,4,COUNTRY,DRAFT,Suicide mortality,Afghanistan,MALE,TOTAL,1.5,1,2
`)
	downloadURL := "https://srhdpeuwpubsa.blob.core.windows.net/whdh/DATADOT/INDICATOR/16BBF41_ALL_LATEST.csv"
	preview, err := who.BuildPreview(raw, who.PreviewOptions{
		Dataset: &definition, SourceURL: downloadURL, AccessedAt: time.Date(2026, time.August, 27, 0, 0, 0, 0, time.UTC), ResolveM49: service.resolveM49,
	})
	if err != nil {
		t.Fatal(err)
	}
	record := previewRecord{
		Version: 1, Dataset: &definition, Summary: summarizePreview(preview),
		Artifact: artifactMetadata{PageURL: definition.PageURL, DownloadURL: downloadURL},
	}
	encoded, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encoded, []byte(`"observations"`)) {
		t.Fatal("persisted preview must not contain an observation slice")
	}
	expiresAt := time.Now().UTC().Add(previewTTL)
	job, err := service.insertJob(ctx, &definition.ID, "preview", "queued", definition.PageURL, &expiresAt)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.stageJob(ctx, job.ID, "preview_ready", downloadURL, raw, encoded, nil); err != nil {
		t.Fatal(err)
	}
	run, err := service.ConfirmPreview(ctx, strconv.FormatInt(job.ID, 10))
	if err != nil {
		t.Fatal(err)
	}
	if run.Kind != "confirm" || run.Status != "succeeded" || run.Snapshot == nil {
		t.Fatalf("confirmation run = %#v", run)
	}
	history, err := service.ImportRuns(ctx, 1, 25)
	if err != nil {
		t.Fatal(err)
	}
	var historic *api.ImportRun
	for index := range history.Runs {
		if history.Runs[index].ID == run.ID {
			historic = &history.Runs[index]
			break
		}
	}
	if historic == nil || historic.Kind != "confirm" || historic.Status != "succeeded" || historic.PreviewID != run.ID {
		t.Fatalf("confirmed import history = %#v, want succeeded confirmation", historic)
	}

	var state string
	var staged []byte
	var releaseID int64
	if err := service.pool.QueryRow(ctx, `SELECT state, staged_raw_csv, release_id FROM import_job WHERE id = $1`, job.ID).Scan(&state, &staged, &releaseID); err != nil {
		t.Fatal(err)
	}
	if state != "confirmed" || staged != nil {
		t.Fatalf("confirmed job state/raw = %q/%q, want confirmed/NULL", state, staged)
	}
	var observations int
	if err := service.pool.QueryRow(ctx, `SELECT count(*) FROM observation WHERE release_id = $1`, releaseID).Scan(&observations); err != nil {
		t.Fatal(err)
	}
	if observations != 2 {
		t.Fatalf("observations = %d, want both source rows retained", observations)
	}
	var publishStates int
	if err := service.pool.QueryRow(ctx, `SELECT count(DISTINCT publish_state) FROM observation WHERE release_id = $1`, releaseID).Scan(&publishStates); err != nil {
		t.Fatal(err)
	}
	if publishStates != 2 {
		t.Fatalf("publish states = %d, want PUBLISHED and DRAFT", publishStates)
	}
	var pinned int
	if err := service.pool.QueryRow(ctx, `SELECT count(*) FROM catalog_snapshot_release WHERE snapshot_id = $1 AND release_id = $2`, run.Snapshot.ID, releaseID).Scan(&pinned); err != nil {
		t.Fatal(err)
	}
	if pinned != 1 {
		t.Fatalf("confirmed release was not pinned by returned snapshot")
	}
}

func TestTerminalJobsClearStagedCSV(t *testing.T) {
	service, ctx := integrationAtlas(t)
	for _, state := range []string{"confirmed", "expired", "succeeded", "unchanged", "failed", "interrupted"} {
		t.Run(state, func(t *testing.T) {
			job, err := service.insertJob(ctx, nil, "preview", "queued", "https://data.who.int/", nil)
			if err != nil {
				t.Fatal(err)
			}
			if err := service.stageJob(ctx, job.ID, "running", "https://example.invalid/data.csv", []byte("raw"), nil, nil); err != nil {
				t.Fatal(err)
			}
			if err := service.finishJob(ctx, job.ID, nil, nil, state, nil); err != nil {
				t.Fatal(err)
			}
			var cleared bool
			if err := service.pool.QueryRow(ctx, `SELECT staged_raw_csv IS NULL FROM import_job WHERE id = $1`, job.ID).Scan(&cleared); err != nil {
				t.Fatal(err)
			}
			if !cleared {
				t.Fatalf("terminal %s job retained staged CSV", state)
			}
		})
	}
}

func TestFinishRefreshPublishesChangedM49WithUnchangedWHOReleases(t *testing.T) {
	service, ctx := integrationAtlas(t)
	if err := service.Seed(ctx); err != nil {
		t.Fatal(err)
	}
	definition, ok := definitionForID("suicide-mortality")
	if !ok {
		t.Fatal("missing curated definition")
	}
	var releaseID int64
	if err := service.pool.QueryRow(ctx, `
		INSERT INTO dataset_release (
			dataset_id, source_url, citation, accessed_at, raw_csv, sha256, response_metadata, csv_headers,
			schema_fingerprint, parser_version, source_row_count, imported_row_count,
			duplicate_row_count, rejected_row_count, diagnostics
		) VALUES ($1, $2, 'fixture', now(), $3, $4, '{}'::jsonb, '[]'::jsonb, $5, 'fixture', 0, 0, 0, 0, '{}'::jsonb)
		RETURNING id`, definition.ID, definition.PageURL, []byte("fixture"), strings.Repeat("a", 64), strings.Repeat("b", 64),
	).Scan(&releaseID); err != nil {
		t.Fatal(err)
	}
	tx, err := service.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	initial, err := service.publishSnapshotTx(ctx, tx, map[string]int64{definition.ID: releaseID})
	if err == nil {
		err = tx.Commit(ctx)
	}
	if err != nil {
		_ = tx.Rollback(ctx)
		t.Fatal(err)
	}

	originalReference := service.referenceRaw
	service.referenceRaw = append(append([]byte(nil), originalReference...), '\n')
	t.Cleanup(func() { service.referenceRaw = originalReference })
	if err := service.EnsureReference(ctx); err != nil {
		t.Fatal(err)
	}
	changed, err := service.insertJob(ctx, nil, "manual_refresh", "queued", refreshSourceURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.finishRefresh(ctx, changed.ID, nil, 0); err != nil {
		t.Fatal(err)
	}

	var changedSnapshot string
	var changedM49 int64
	if err := service.pool.QueryRow(ctx, `
		SELECT snapshot.id, snapshot.m49_release_id
		FROM catalog_head head JOIN catalog_snapshot snapshot ON snapshot.id = head.snapshot_id
		WHERE head.singleton`).Scan(&changedSnapshot, &changedM49); err != nil {
		t.Fatal(err)
	}
	if changedSnapshot == initial.ref.ID || strconv.FormatInt(changedM49, 10) == initial.ref.M49ReferenceRelease {
		t.Fatalf("reference-only refresh did not advance M49 snapshot: old=%+v new=%s/%d", initial.ref, changedSnapshot, changedM49)
	}
	var pinnedRelease int64
	if err := service.pool.QueryRow(ctx, `SELECT release_id FROM catalog_snapshot_release WHERE snapshot_id = $1`, changedSnapshot).Scan(&pinnedRelease); err != nil {
		t.Fatal(err)
	}
	if pinnedRelease != releaseID {
		t.Fatalf("reference-only snapshot changed WHO release: got %d, want %d", pinnedRelease, releaseID)
	}

	unchanged, err := service.insertJob(ctx, nil, "manual_refresh", "queued", refreshSourceURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.finishRefresh(ctx, unchanged.ID, nil, 0); err != nil {
		t.Fatal(err)
	}
	var repeatedSnapshot string
	var snapshots int
	if err := service.pool.QueryRow(ctx, `
		SELECT head.snapshot_id, (SELECT count(*) FROM catalog_snapshot)
		FROM catalog_head head WHERE head.singleton`).Scan(&repeatedSnapshot, &snapshots); err != nil {
		t.Fatal(err)
	}
	if repeatedSnapshot != changedSnapshot || snapshots != 2 {
		t.Fatalf("unchanged reference/WHO refresh created a snapshot: head=%s snapshots=%d", repeatedSnapshot, snapshots)
	}
	var state string
	if err := service.pool.QueryRow(ctx, `SELECT state FROM import_job WHERE id = $1`, unchanged.ID).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != "unchanged" {
		t.Fatalf("unchanged reference/WHO refresh state = %q, want unchanged", state)
	}
}

func TestFinishRefreshRepinsHistoricalReleaseWithoutDuplicatingIt(t *testing.T) {
	service, ctx := integrationAtlas(t)
	if err := service.Seed(ctx); err != nil {
		t.Fatal(err)
	}
	definition, ok := definitionForID("suicide-mortality")
	if !ok {
		t.Fatal("missing curated definition")
	}
	releaseA := insertFixtureRelease(t, service, ctx, definition, "A", strings.Repeat("a", 64), strings.Repeat("b", 64))
	releaseB := insertFixtureRelease(t, service, ctx, definition, "B", strings.Repeat("c", 64), strings.Repeat("d", 64))
	tx, err := service.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.publishSnapshotTx(ctx, tx, map[string]int64{definition.ID: releaseA})
	if err == nil {
		err = tx.Commit(ctx)
	}
	if err != nil {
		_ = tx.Rollback(ctx)
		t.Fatal(err)
	}
	if current, err := service.releaseIsCurrent(ctx, definition.ID, releaseA); err != nil || !current {
		t.Fatalf("initial A release current=%t err=%v, want true/nil", current, err)
	}
	if current, err := service.releaseIsCurrent(ctx, definition.ID, releaseB); err != nil || current {
		t.Fatalf("historical B release current=%t err=%v, want false/nil", current, err)
	}

	toB, err := service.insertJob(ctx, &definition.ID, "manual_refresh", "queued", refreshSourceURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.finishRefresh(ctx, toB.ID, []refreshCandidate{{
		job: toB, definition: definition, reuseReleaseID: &releaseB,
	}}, 0); err != nil {
		t.Fatal(err)
	}
	assertHeadRelease(t, service, ctx, definition.ID, releaseB)
	if current, err := service.releaseIsCurrent(ctx, definition.ID, releaseA); err != nil || current {
		t.Fatalf("after B pin, historical A current=%t err=%v, want false/nil", current, err)
	}
	if found, exists, err := service.existingRelease(ctx, definition.ID, strings.Repeat("a", 64)); err != nil || !exists || found != releaseA {
		t.Fatalf("historical A checksum lookup = %d/%t/%v, want %d/true/nil", found, exists, err, releaseA)
	}

	toA, err := service.insertJob(ctx, &definition.ID, "manual_refresh", "queued", refreshSourceURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.finishRefresh(ctx, toA.ID, []refreshCandidate{{
		job: toA, definition: definition, reuseReleaseID: &releaseA,
	}}, 0); err != nil {
		t.Fatal(err)
	}
	assertHeadRelease(t, service, ctx, definition.ID, releaseA)
	if current, err := service.releaseIsCurrent(ctx, definition.ID, releaseA); err != nil || !current {
		t.Fatalf("after A re-pin, current=%t err=%v, want true/nil", current, err)
	}

	var releases, snapshots int
	if err := service.pool.QueryRow(ctx, `
		SELECT (SELECT count(*) FROM dataset_release WHERE dataset_id = $1),
		       (SELECT count(*) FROM catalog_snapshot)`, definition.ID).Scan(&releases, &snapshots); err != nil {
		t.Fatal(err)
	}
	if releases != 2 || snapshots != 3 {
		t.Fatalf("A→B→A releases/snapshots = %d/%d, want immutable 2 releases and 3 snapshots", releases, snapshots)
	}

	unchanged, err := service.insertJob(ctx, &definition.ID, "manual_refresh", "queued", refreshSourceURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.finishRefresh(ctx, unchanged.ID, nil, 0); err != nil {
		t.Fatal(err)
	}
	if err := service.pool.QueryRow(ctx, `SELECT count(*) FROM catalog_snapshot`).Scan(&snapshots); err != nil {
		t.Fatal(err)
	}
	if snapshots != 3 {
		t.Fatalf("unchanged current release created snapshot %d", snapshots)
	}
	var state string
	if err := service.pool.QueryRow(ctx, `SELECT state FROM import_job WHERE id = $1`, unchanged.ID).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != "unchanged" {
		t.Fatalf("unchanged current release state = %q, want unchanged", state)
	}
}

func TestConfirmedGenericDatasetParticipatesInRefreshAndFreshness(t *testing.T) {
	service, ctx := integrationAtlas(t)
	raw, err := os.ReadFile(filepath.Join("..", "who", "testdata", "generic.csv"))
	if err != nil {
		t.Fatal(err)
	}
	definition, releaseA := confirmGenericFixture(t, service, ctx, raw)
	rawB := []byte(strings.Replace(string(raw), ",1.5,1,2", ",2.5,1,3", 1))
	_, releaseB := confirmGenericFixture(t, service, ctx, rawB)
	if releaseA == releaseB {
		t.Fatal("changed generic source reused its immutable release")
	}

	definitions, err := service.refreshDefinitions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var stored *who.DatasetDefinition
	for index := range definitions {
		if definitions[index].ID == definition.ID {
			stored = &definitions[index]
			break
		}
	}
	if stored == nil || stored.PageURL != definition.PageURL || stored.ValueColumn != definition.ValueColumn || stored.IndicatorCode != definition.IndicatorCode {
		t.Fatalf("generic definition was not loaded for refresh: %#v", stored)
	}
	freshness, err := service.Freshness(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var genericFreshness *api.DatasetFreshness
	for index := range freshness.Datasets {
		if freshness.Datasets[index].DatasetID == definition.ID {
			genericFreshness = &freshness.Datasets[index]
			break
		}
	}
	if genericFreshness == nil || genericFreshness.LastSuccessAt == nil || genericFreshness.LastAttemptState != "succeeded" {
		t.Fatalf("generic freshness = %#v, want confirmed success", genericFreshness)
	}

	toA, err := service.insertJob(ctx, &definition.ID, "manual_refresh", "queued", refreshSourceURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.finishRefresh(ctx, toA.ID, []refreshCandidate{{
		job: toA, definition: definition, reuseReleaseID: &releaseA,
	}}, 0); err != nil {
		t.Fatal(err)
	}
	assertHeadRelease(t, service, ctx, definition.ID, releaseA)

	unchanged, err := service.insertJob(ctx, &definition.ID, "manual_refresh", "queued", refreshSourceURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.finishRefresh(ctx, unchanged.ID, nil, 0); err != nil {
		t.Fatal(err)
	}
	var releases, snapshots int
	if err := service.pool.QueryRow(ctx, `
		SELECT (SELECT count(*) FROM dataset_release WHERE dataset_id = $1),
		       (SELECT count(*) FROM catalog_snapshot)`, definition.ID).Scan(&releases, &snapshots); err != nil {
		t.Fatal(err)
	}
	if releases != 2 || snapshots != 3 {
		t.Fatalf("generic releases/snapshots = %d/%d, want immutable 2 releases and three snapshots", releases, snapshots)
	}
	var state string
	if err := service.pool.QueryRow(ctx, `SELECT state FROM import_job WHERE id = $1`, unchanged.ID).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != "unchanged" {
		t.Fatalf("unchanged generic refresh state = %q, want unchanged", state)
	}
}

func confirmGenericFixture(t *testing.T, service *Service, ctx context.Context, raw []byte) (who.DatasetDefinition, int64) {
	t.Helper()
	if err := service.Seed(ctx); err != nil {
		t.Fatal(err)
	}
	page, err := who.ValidateIndicatorPageURL("https://data.who.int/indicators/i/F08B4FD/ABC1234")
	if err != nil {
		t.Fatal(err)
	}
	const downloadURL = "https://srhdpeuwpubsa.blob.core.windows.net/whdh/DATADOT/INDICATOR/ABC1234_ALL_LATEST.csv"
	preview, err := who.BuildPreview(raw, who.PreviewOptions{
		SourceURL: downloadURL, AccessedAt: time.Date(2026, time.August, 27, 0, 0, 0, 0, time.UTC), ResolveM49: service.resolveM49,
	})
	if err != nil {
		t.Fatal(err)
	}
	definition := genericDefinition(page, preview)
	preview.Dataset = &definition
	record := previewRecord{
		Version: 1, Dataset: &definition, Summary: summarizePreview(preview),
		Artifact: artifactMetadata{PageURL: page.URL, DownloadURL: downloadURL},
	}
	encoded, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	expiresAt := time.Now().UTC().Add(previewTTL)
	job, err := service.insertJob(ctx, nil, "preview", "queued", page.URL, &expiresAt)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.stageJob(ctx, job.ID, "preview_ready", downloadURL, raw, encoded, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ConfirmPreview(ctx, strconv.FormatInt(job.ID, 10)); err != nil {
		t.Fatal(err)
	}
	var releaseID int64
	if err := service.pool.QueryRow(ctx, `SELECT release_id FROM import_job WHERE id = $1`, job.ID).Scan(&releaseID); err != nil {
		t.Fatal(err)
	}
	return definition, releaseID
}

func insertFixtureRelease(t *testing.T, service *Service, ctx context.Context, definition who.DatasetDefinition, raw, checksum, fingerprint string) int64 {
	t.Helper()
	var releaseID int64
	err := service.pool.QueryRow(ctx, `
		INSERT INTO dataset_release (
			dataset_id, source_url, citation, accessed_at, raw_csv, sha256, response_metadata, csv_headers,
			schema_fingerprint, parser_version, source_row_count, imported_row_count,
			duplicate_row_count, rejected_row_count, diagnostics
		) VALUES ($1, $2, 'fixture', now(), $3, $4, '{}'::jsonb, '[]'::jsonb, $5, 'fixture', 0, 0, 0, 0, '{}'::jsonb)
		RETURNING id`, definition.ID, definition.PageURL, []byte(raw), checksum, fingerprint,
	).Scan(&releaseID)
	if err != nil {
		t.Fatal(err)
	}
	return releaseID
}

func assertHeadRelease(t *testing.T, service *Service, ctx context.Context, datasetID string, want int64) {
	t.Helper()
	var got int64
	if err := service.pool.QueryRow(ctx, `
		SELECT pinned.release_id
		FROM catalog_head head
		JOIN catalog_snapshot_release pinned ON pinned.snapshot_id = head.snapshot_id
		WHERE head.singleton AND pinned.dataset_id = $1`, datasetID).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("catalog head release = %d, want %d", got, want)
	}
}

func integrationAtlas(t *testing.T) (*Service, context.Context) {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	// Keep the test-name fuse adjacent to the destructive reset below.
	if err := store.ValidateTestDSN(dsn); err != nil {
		t.Fatalf("refusing to reset TEST_DATABASE_URL: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	persistence, err := store.Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(persistence.Close)
	if _, err := persistence.Pool().Exec(ctx, `DROP SCHEMA public CASCADE`); err != nil {
		t.Fatal(err)
	}
	if _, err := persistence.Pool().Exec(ctx, `CREATE SCHEMA public`); err != nil {
		t.Fatal(err)
	}
	if err := persistence.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	referenceDir, err := filepath.Abs(filepath.Join("..", "..", "assets", "reference"))
	if err != nil {
		t.Fatal(err)
	}
	service, err := New(persistence.Pool(), referenceDir, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(service.Close)
	return service, ctx
}
