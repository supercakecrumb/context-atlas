package atlas

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/supercakecrumb/context-atlas/internal/api"
	"github.com/supercakecrumb/context-atlas/internal/who"
)

const refreshSourceURL = "https://data.who.int/"

// Refresh starts an owner-requested catalog refresh and returns immediately.
func (s *Service) Refresh(ctx context.Context) (api.ImportRun, error) {
	if err := s.Seed(ctx); err != nil {
		return api.ImportRun{}, err
	}
	select {
	case s.refreshSlots <- struct{}{}:
	case <-ctx.Done():
		return api.ImportRun{}, ctx.Err()
	default:
		return api.ImportRun{}, fmt.Errorf("%w: a catalog refresh is already running", api.ErrConflict)
	}
	parent, err := s.insertJob(ctx, nil, "manual_refresh", "queued", refreshSourceURL, nil)
	if err != nil {
		<-s.refreshSlots
		return api.ImportRun{}, err
	}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer func() { <-s.refreshSlots }()
		if err := s.runRefresh(s.root, parent.ID, "manual_refresh"); err != nil {
			s.logger.Error("catalog refresh failed", "job_id", parent.ID, "error", err)
		}
	}()
	return importRunFromJob(parent, previewRecord{}), nil
}

// RefreshAll is the blocking scheduler adapter. Trigger accepts scheduler's
// startup_catchup/scheduled labels plus the manual API label.
func (s *Service) RefreshAll(ctx context.Context, trigger string) error {
	kind, err := refreshKind(trigger)
	if err != nil {
		return err
	}
	if err := s.Seed(ctx); err != nil {
		return err
	}
	select {
	case s.refreshSlots <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	defer func() { <-s.refreshSlots }()
	parent, err := s.insertJob(ctx, nil, kind, "queued", refreshSourceURL, nil)
	if err != nil {
		return err
	}
	return s.runRefresh(ctx, parent.ID, kind)
}

func refreshKind(trigger string) (string, error) {
	switch trigger {
	case "manual_refresh", "manual", "refresh":
		return "manual_refresh", nil
	case "scheduled":
		return "scheduled", nil
	case "startup_catchup", "startup":
		return "startup", nil
	default:
		return "", fmt.Errorf("unknown refresh trigger %q", trigger)
	}
}

// refreshDefinitions starts from datasets that have actually been confirmed,
// then adds only curated launch datasets that do not yet have a stored release.
func (s *Service) refreshDefinitions(ctx context.Context) ([]who.DatasetDefinition, error) {
	definitions, err := s.activeDefinitions(ctx)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]struct{}, len(definitions)+len(who.CuratedDefinitions()))
	for _, definition := range definitions {
		seen[definition.ID] = struct{}{}
	}
	for _, definition := range who.CuratedDefinitions() {
		if _, exists := seen[definition.ID]; exists {
			continue
		}
		definition, _, err = marshalDatasetDefinition(definition)
		if err != nil {
			return nil, fmt.Errorf("validate curated refresh definition: %w", err)
		}
		definitions = append(definitions, definition)
	}
	sort.Slice(definitions, func(i, j int) bool { return definitions[i].ID < definitions[j].ID })
	return definitions, nil
}

func (s *Service) activeDefinitions(ctx context.Context) ([]who.DatasetDefinition, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT dataset.id, dataset.definition
		FROM dataset
		WHERE dataset.definition <> '{}'::jsonb
		  AND EXISTS (SELECT 1 FROM dataset_release WHERE dataset_release.dataset_id = dataset.id)
		ORDER BY dataset.id`)
	if err != nil {
		return nil, fmt.Errorf("list active dataset definitions: %w", err)
	}
	defer rows.Close()
	definitions := make([]who.DatasetDefinition, 0)
	for rows.Next() {
		var id string
		var encoded []byte
		if err := rows.Scan(&id, &encoded); err != nil {
			return nil, fmt.Errorf("scan active dataset definition: %w", err)
		}
		var definition who.DatasetDefinition
		if err := json.Unmarshal(encoded, &definition); err != nil {
			return nil, fmt.Errorf("decode active dataset definition %s: %w", id, err)
		}
		definition, _, err = marshalDatasetDefinition(definition)
		if err != nil {
			return nil, fmt.Errorf("validate active dataset definition %s: %w", id, err)
		}
		if definition.ID != id {
			return nil, fmt.Errorf("active dataset definition ID %s does not match row %s", definition.ID, id)
		}
		definitions = append(definitions, definition)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read active dataset definitions: %w", err)
	}
	return definitions, nil
}

type refreshCandidate struct {
	job            job
	definition     who.DatasetDefinition
	record         previewRecord
	preview        who.Preview
	raw            []byte
	reuseReleaseID *int64
}

func (s *Service) runRefresh(ctx context.Context, parentID int64, kind string) error {
	if err := s.markJobRunning(ctx, parentID); err != nil {
		return fmt.Errorf("start refresh job: %w", err)
	}
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		_ = s.finishJob(ctx, parentID, nil, nil, "failed", failureMessage(err))
		return fmt.Errorf("acquire refresh lock connection: %w", err)
	}
	defer conn.Release()
	var locked bool
	if err := conn.QueryRow(ctx, `SELECT pg_try_advisory_lock($1)`, refreshLockKey).Scan(&locked); err != nil {
		_ = s.finishJob(ctx, parentID, nil, nil, "failed", failureMessage(err))
		return fmt.Errorf("acquire PostgreSQL refresh lock: %w", err)
	}
	if !locked {
		err := fmt.Errorf("%w: catalog refresh is already running in another process", api.ErrConflict)
		_ = s.finishJob(ctx, parentID, nil, nil, "failed", failureMessage(err))
		return err
	}
	defer func() { _, _ = conn.Exec(context.Background(), `SELECT pg_advisory_unlock($1)`, refreshLockKey) }()
	if err := s.ExpirePreviews(ctx); err != nil {
		_ = s.finishJob(ctx, parentID, nil, nil, "failed", failureMessage(err))
		return err
	}

	definitions, err := s.refreshDefinitions(ctx)
	if err != nil {
		_ = s.finishJob(ctx, parentID, nil, nil, "failed", failureMessage(err))
		return err
	}
	candidates := make([]refreshCandidate, 0, len(definitions))
	failed := 0
	for _, definition := range definitions {
		candidate, changed, err := s.fetchRefreshCandidate(ctx, definition, kind)
		if err != nil {
			failed++
			continue
		}
		if changed {
			candidates = append(candidates, candidate)
		}
	}
	return s.finishRefresh(ctx, parentID, candidates, failed)
}

// finishRefresh atomically retains the existing WHO release set or advances it
// with changed candidates. It also advances the snapshot for a newly seeded
// M49 reference release even when every WHO checksum is unchanged.
func (s *Service) finishRefresh(ctx context.Context, parentID int64, candidates []refreshCandidate, failed int) error {
	if len(candidates) == 0 && failed > 0 {
		if err := s.finishJob(ctx, parentID, nil, nil, "failed", refreshFailureMessage(failed)); err != nil {
			return fmt.Errorf("finish refresh job: %w", err)
		}
		return errors.New("all changed dataset refreshes failed; existing catalog was retained")
	}

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return s.failRefreshCandidates(ctx, parentID, candidates, fmt.Errorf("begin refresh transaction: %w", err))
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	head, releases, err := currentSnapshotTx(ctx, tx)
	if err == nil {
		releases, err = completeReleaseSetTx(ctx, tx, releases)
	}
	publishNeeded := head == nil
	if err == nil && head != nil {
		var currentM49ID int64
		currentM49ID, err = s.currentM49ReleaseIDTx(ctx, tx)
		if err == nil && head.ref.M49ReferenceRelease != strconv.FormatInt(currentM49ID, 10) {
			publishNeeded = true
		}
	}
	for index := range candidates {
		candidate := &candidates[index]
		var releaseID int64
		if candidate.reuseReleaseID != nil {
			releaseID = *candidate.reuseReleaseID
		} else if err == nil {
			releaseID, _, err = persistReleaseTx(ctx, tx, candidate.definition, candidate.record, candidate.preview, candidate.raw)
		}
		if err != nil {
			break
		}
		if releases[candidate.definition.ID] != releaseID {
			publishNeeded = true
		}
		releases[candidate.definition.ID] = releaseID
		candidate.job.ReleaseID = &releaseID
	}
	if err == nil && publishNeeded {
		head, err = s.publishSnapshotTx(ctx, tx, releases)
	}
	if err == nil {
		for _, candidate := range candidates {
			state := "succeeded"
			if !publishNeeded {
				state = "unchanged"
			}
			if finishErr := finishJobTx(ctx, tx, candidate.job.ID, &candidate.definition.ID, candidate.job.ReleaseID, state, nil); finishErr != nil {
				err = fmt.Errorf("finish dataset refresh: %w", finishErr)
				break
			}
		}
		if err == nil {
			parentState := "succeeded"
			if !publishNeeded {
				parentState = "unchanged"
			}
			if finishErr := finishJobTx(ctx, tx, parentID, nil, nil, parentState, refreshFailureMessage(failed)); finishErr != nil {
				err = fmt.Errorf("finish parent refresh: %w", finishErr)
			}
		}
	}
	if err != nil {
		_ = tx.Rollback(context.Background())
		return s.failRefreshCandidates(ctx, parentID, candidates, err)
	}
	if err := tx.Commit(ctx); err != nil {
		_ = tx.Rollback(context.Background())
		return s.failRefreshCandidates(ctx, parentID, candidates, err)
	}
	if head == nil && publishNeeded {
		return s.failRefreshCandidates(ctx, parentID, candidates, errors.New("refresh did not publish a catalog snapshot"))
	}
	return nil
}

func (s *Service) fetchRefreshCandidate(ctx context.Context, definition who.DatasetDefinition, kind string) (refreshCandidate, bool, error) {
	job, err := s.insertJob(ctx, &definition.ID, kind, "running", definition.PageURL, nil)
	if err != nil {
		return refreshCandidate{}, false, err
	}
	fetched, err := s.fetcher.FetchFromIndicatorPage(ctx, definition.PageURL)
	if err != nil {
		_ = s.finishJob(ctx, job.ID, &definition.ID, nil, "failed", failureMessage(err))
		return refreshCandidate{}, false, err
	}
	preview, err := who.BuildPreview(fetched.Artifact.Bytes, who.PreviewOptions{
		Dataset: &definition, SourceURL: fetched.Artifact.URL, AccessedAt: fetched.Artifact.AccessedAt, ResolveM49: s.resolveM49,
	})
	record := previewRecord{
		Version: 1, Dataset: &definition, Summary: summarizePreview(preview),
		Artifact: artifactMetadata{
			PageURL: definition.PageURL, PageETag: fetched.Discovery.ETag, PageModified: fetched.Discovery.LastModified,
			DownloadURL: fetched.Artifact.URL, ETag: fetched.Artifact.ETag, LastModified: fetched.Artifact.LastModified,
			ContentType: fetched.Artifact.ContentType, ContentLength: fetched.Artifact.ContentLength,
		},
	}
	encoded, marshalErr := json.Marshal(record)
	if marshalErr != nil {
		err = fmt.Errorf("encode refresh preview: %w", marshalErr)
	}
	if err != nil {
		_ = s.stageJob(ctx, job.ID, "failed", fetched.Artifact.URL, fetched.Artifact.Bytes, encoded, failureMessage(err))
		return refreshCandidate{}, false, err
	}
	if existingID, exists, err := s.existingRelease(ctx, definition.ID, preview.SHA256); err != nil {
		_ = s.finishJob(ctx, job.ID, &definition.ID, nil, "failed", failureMessage(err))
		return refreshCandidate{}, false, err
	} else if exists {
		current, err := s.releaseIsCurrent(ctx, definition.ID, existingID)
		if err != nil {
			_ = s.finishJob(ctx, job.ID, &definition.ID, nil, "failed", failureMessage(err))
			return refreshCandidate{}, false, err
		}
		if err := s.stageJob(ctx, job.ID, "running", fetched.Artifact.URL, fetched.Artifact.Bytes, encoded, nil); err != nil {
			_ = s.finishJob(ctx, job.ID, &definition.ID, nil, "failed", failureMessage(err))
			return refreshCandidate{}, false, err
		}
		if current {
			if err := s.finishJob(ctx, job.ID, &definition.ID, &existingID, "unchanged", nil); err != nil {
				return refreshCandidate{}, false, err
			}
			return refreshCandidate{}, false, nil
		}
		return refreshCandidate{
			job: job, definition: definition, record: record, preview: preview, raw: fetched.Artifact.Bytes,
			reuseReleaseID: &existingID,
		}, true, nil
	}
	if err := s.stageJob(ctx, job.ID, "running", fetched.Artifact.URL, fetched.Artifact.Bytes, encoded, nil); err != nil {
		_ = s.finishJob(ctx, job.ID, &definition.ID, nil, "failed", failureMessage(err))
		return refreshCandidate{}, false, err
	}
	return refreshCandidate{job: job, definition: definition, record: record, preview: preview, raw: fetched.Artifact.Bytes}, true, nil
}

func (s *Service) existingRelease(ctx context.Context, datasetID, checksum string) (int64, bool, error) {
	var id int64
	err := s.pool.QueryRow(ctx, `SELECT id FROM dataset_release WHERE dataset_id = $1 AND sha256 = $2`, datasetID, checksum).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("find existing dataset release: %w", err)
	}
	return id, true, nil
}

func (s *Service) releaseIsCurrent(ctx context.Context, datasetID string, releaseID int64) (bool, error) {
	var current bool
	err := s.pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM catalog_head head
			JOIN catalog_snapshot_release pinned ON pinned.snapshot_id = head.snapshot_id
			WHERE head.singleton AND pinned.dataset_id = $1 AND pinned.release_id = $2
		)`, datasetID, releaseID).Scan(&current)
	if err != nil {
		return false, fmt.Errorf("check current dataset release: %w", err)
	}
	return current, nil
}

func (s *Service) failRefreshCandidates(ctx context.Context, parentID int64, candidates []refreshCandidate, cause error) error {
	for _, candidate := range candidates {
		_ = s.finishJob(ctx, candidate.job.ID, &candidate.definition.ID, nil, "failed", failureMessage(cause))
	}
	_ = s.finishJob(ctx, parentID, nil, nil, "failed", failureMessage(cause))
	return cause
}

func refreshFailureMessage(failed int) *string {
	if failed == 0 {
		return nil
	}
	return failureMessage(fmt.Errorf("%d dataset refreshes failed; previous releases were retained", failed))
}

// LastSuccessfulRefresh is the scheduler freshness seam.
func (s *Service) LastSuccessfulRefresh(ctx context.Context) (time.Time, error) {
	var value *time.Time
	err := s.pool.QueryRow(ctx, `
		SELECT max(finished_at)
		FROM import_job
		WHERE dataset_id IS NULL
		  AND import_kind IN ('manual_refresh', 'scheduled', 'startup', 'refresh')
		  AND state IN ('succeeded', 'unchanged')`).Scan(&value)
	if err != nil {
		return time.Time{}, fmt.Errorf("read last successful refresh: %w", err)
	}
	if value == nil {
		return time.Time{}, nil
	}
	return value.UTC(), nil
}

// MarkInterruptedFailed makes jobs abandoned by a stopped process visible.
func (s *Service) MarkInterruptedFailed(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE import_job
		SET state = 'interrupted', error_message = 'interrupted by process restart',
			staged_raw_csv = NULL, finished_at = now()
		WHERE state IN ('queued', 'running')`)
	if err != nil {
		return fmt.Errorf("mark interrupted import jobs: %w", err)
	}
	return nil
}

// ImportRuns returns reverse-chronological import and refresh history.
func (s *Service) ImportRuns(ctx context.Context, page, pageSize int) (api.ImportRunResult, error) {
	if page < 1 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 25
	}
	if pageSize > 100 {
		pageSize = 100
	}
	var total int64
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM import_job`).Scan(&total); err != nil {
		return api.ImportRunResult{}, fmt.Errorf("count import runs: %w", err)
	}
	rows, err := s.pool.Query(ctx, `SELECT `+jobColumns+` FROM import_job ORDER BY created_at DESC, id DESC LIMIT $1 OFFSET $2`, pageSize, (page-1)*pageSize)
	if err != nil {
		return api.ImportRunResult{}, fmt.Errorf("list import runs: %w", err)
	}
	defer rows.Close()
	result := api.ImportRunResult{Pagination: api.Pagination{Page: page, PageSize: pageSize, Total: total}}
	for rows.Next() {
		value, err := scanJob(rows)
		if err != nil {
			return api.ImportRunResult{}, err
		}
		record, err := decodePreview(value)
		if err != nil {
			return api.ImportRunResult{}, err
		}
		result.Runs = append(result.Runs, importRunFromJob(value, record))
	}
	if err := rows.Err(); err != nil {
		return api.ImportRunResult{}, err
	}
	return result, nil
}

func importRunFromJob(value job, record previewRecord) api.ImportRun {
	run := api.ImportRun{
		ID: strconv.FormatInt(value.ID, 10), Kind: importRunKind(value), Status: importRunStatus(value.State),
		Rows: api.RowAccounting{
			SourceRows: int64(record.Summary.Accounting.RowsRead), AcceptedRows: int64(record.Summary.Accounting.UniqueObservations),
			CollapsedDuplicates: int64(record.Summary.Accounting.ExactDuplicates),
			RejectedRows:        int64(record.Summary.Accounting.ConflictingRows + record.Summary.Accounting.InvalidRows),
		},
		StartedAt: jobStarted(value),
	}
	if value.DatasetID != nil {
		run.DatasetID = *value.DatasetID
	}
	if value.State == "confirmed" {
		run.PreviewID = strconv.FormatInt(value.ID, 10)
	}
	if value.ErrorMessage != nil {
		run.Error = safeWarning(*value.ErrorMessage)
	}
	if value.FinishedAt != nil {
		finished := value.FinishedAt.UTC()
		run.FinishedAt = &finished
	}
	return run
}

func importRunKind(value job) string {
	if value.State == "confirmed" {
		return "confirm"
	}
	switch value.ImportKind {
	case "scheduled":
		return "scheduled_refresh"
	case "startup":
		return "startup_catchup"
	case "manual_refresh", "refresh":
		return "manual_refresh"
	default:
		return "preview"
	}
}

func importRunStatus(state string) string {
	switch state {
	case "queued", "preview_ready":
		return "pending"
	case "confirmed", "succeeded":
		return "succeeded"
	case "running", "failed", "interrupted", "unchanged":
		return state
	default:
		return "failed"
	}
}

// Freshness reports the last outcome for confirmed datasets plus any missing
// curated launch datasets. A stale warning is intentionally slower than
// scheduler catch-up: 72 hours versus 24.
func (s *Service) Freshness(ctx context.Context) (api.FreshnessResult, error) {
	definitions, err := s.refreshDefinitions(ctx)
	if err != nil {
		return api.FreshnessResult{}, err
	}
	result := api.FreshnessResult{Datasets: make([]api.DatasetFreshness, 0, len(definitions))}
	for _, definition := range definitions {
		item, err := s.datasetFreshness(ctx, definition.ID)
		if err != nil {
			return api.FreshnessResult{}, err
		}
		result.Datasets = append(result.Datasets, item)
	}
	return result, nil
}

func (s *Service) datasetFreshness(ctx context.Context, datasetID string) (api.DatasetFreshness, error) {
	item := api.DatasetFreshness{DatasetID: datasetID, LastAttemptState: "never"}
	var success *time.Time
	if err := s.pool.QueryRow(ctx, `
		SELECT max(finished_at) FROM import_job
		WHERE dataset_id = $1 AND state IN ('succeeded', 'unchanged', 'confirmed')`, datasetID).Scan(&success); err != nil {
		return item, fmt.Errorf("read dataset refresh success: %w", err)
	}
	if success != nil {
		value := success.UTC()
		item.LastSuccessAt = &value
		item.Stale = s.now().UTC().Sub(value) > freshnessAge
	} else {
		item.Stale = true
	}
	var state string
	var attempted time.Time
	err := s.pool.QueryRow(ctx, `
		SELECT state, COALESCE(finished_at, started_at, created_at)
		FROM import_job WHERE dataset_id = $1 ORDER BY created_at DESC, id DESC LIMIT 1`, datasetID).Scan(&state, &attempted)
	if errors.Is(err, pgx.ErrNoRows) {
		return item, nil
	}
	if err != nil {
		return item, fmt.Errorf("read dataset refresh attempt: %w", err)
	}
	item.LastAttemptState = importRunStatus(state)
	attempted = attempted.UTC()
	item.LastAttemptAt = &attempted
	return item, nil
}

var _ interface {
	ExpirePreviews(context.Context) error
	LastSuccessfulRefresh(context.Context) (time.Time, error)
	MarkInterruptedFailed(context.Context) error
	RefreshAll(context.Context, string) error
} = (*Service)(nil)

var _ api.ImportAdmin = (*Service)(nil)
