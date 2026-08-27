package atlas

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/supercakecrumb/context-atlas/internal/api"
	"github.com/supercakecrumb/context-atlas/internal/who"
)

const jobColumns = `
	id, dataset_id, release_id, requested_by, import_kind, state, source_url,
	discovered_url, staged_raw_csv, preview, error_message, expires_at,
	created_at, started_at, finished_at`

type job struct {
	ID            int64
	DatasetID     *string
	ReleaseID     *int64
	RequestedBy   string
	ImportKind    string
	State         string
	SourceURL     string
	DiscoveredURL *string
	StagedRawCSV  []byte
	Preview       []byte
	ErrorMessage  *string
	ExpiresAt     *time.Time
	CreatedAt     time.Time
	StartedAt     *time.Time
	FinishedAt    *time.Time
}

type previewRecord struct {
	Version  int                    `json:"version"`
	Dataset  *who.DatasetDefinition `json:"dataset"`
	Summary  previewSummary         `json:"summary"`
	Artifact artifactMetadata       `json:"artifact"`
}

// previewSummary deliberately excludes parsed observations. The bounded raw CSV
// remains the source of truth; confirmation reparses it inside its transaction.
type previewSummary struct {
	SourceURL   string                    `json:"source_url"`
	AccessedAt  time.Time                 `json:"accessed_at"`
	SHA256      string                    `json:"sha256"`
	Bytes       int64                     `json:"bytes"`
	Schema      who.Schema                `json:"schema"`
	Accounting  who.RowAccounting         `json:"accounting"`
	Diagnostics who.Diagnostics           `json:"diagnostics"`
	Units       []string                  `json:"units"`
	Dimensions  []api.DimensionDefinition `json:"dimensions"`
	Unmapped    []api.Geography           `json:"unmapped_geographies"`
}

type artifactMetadata struct {
	PageURL       string `json:"page_url"`
	PageETag      string `json:"page_etag,omitempty"`
	PageModified  string `json:"page_modified,omitempty"`
	DownloadURL   string `json:"download_url"`
	ETag          string `json:"etag,omitempty"`
	LastModified  string `json:"last_modified,omitempty"`
	ContentType   string `json:"content_type,omitempty"`
	ContentLength int64  `json:"content_length,omitempty"`
}

func scanJob(row pgx.Row) (job, error) {
	var value job
	err := row.Scan(
		&value.ID, &value.DatasetID, &value.ReleaseID, &value.RequestedBy,
		&value.ImportKind, &value.State, &value.SourceURL, &value.DiscoveredURL,
		&value.StagedRawCSV, &value.Preview, &value.ErrorMessage, &value.ExpiresAt,
		&value.CreatedAt, &value.StartedAt, &value.FinishedAt,
	)
	return value, err
}

func parseJobID(raw string) (int64, error) {
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id < 1 {
		return 0, fmt.Errorf("%w: invalid import job ID", api.ErrNotFound)
	}
	return id, nil
}

func (s *Service) getJob(ctx context.Context, id int64) (job, error) {
	value, err := scanJob(s.pool.QueryRow(ctx, `SELECT `+jobColumns+` FROM import_job WHERE id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return job{}, fmt.Errorf("%w: import job", api.ErrNotFound)
	}
	if err != nil {
		return job{}, fmt.Errorf("get import job: %w", err)
	}
	return value, nil
}

func getJobTx(ctx context.Context, tx pgx.Tx, id int64) (job, error) {
	value, err := scanJob(tx.QueryRow(ctx, `SELECT `+jobColumns+` FROM import_job WHERE id = $1 FOR UPDATE`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return job{}, fmt.Errorf("%w: import job", api.ErrNotFound)
	}
	if err != nil {
		return job{}, fmt.Errorf("lock import job: %w", err)
	}
	return value, nil
}

func (s *Service) insertJob(ctx context.Context, datasetID *string, kind, state, sourceURL string, expiresAt *time.Time) (job, error) {
	value, err := scanJob(s.pool.QueryRow(ctx, `
		INSERT INTO import_job (dataset_id, import_kind, state, source_url, expires_at)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING `+jobColumns, datasetID, kind, state, sourceURL, expiresAt))
	if err != nil {
		return job{}, fmt.Errorf("create import job: %w", err)
	}
	return value, nil
}

func (s *Service) markJobRunning(ctx context.Context, id int64) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE import_job
		SET state = 'running', started_at = COALESCE(started_at, now())
		WHERE id = $1 AND state = 'queued'`, id)
	return err
}

func (s *Service) stageJob(ctx context.Context, id int64, state, discoveredURL string, raw, preview []byte, failure *string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE import_job
		SET state = $2, discovered_url = NULLIF($3, ''),
			staged_raw_csv = CASE WHEN $2 IN ('confirmed', 'expired', 'succeeded', 'unchanged', 'failed', 'interrupted') THEN NULL ELSE $4::bytea END,
			preview = COALESCE($5::jsonb, preview), error_message = $6, started_at = COALESCE(started_at, now()),
			finished_at = CASE WHEN $2 = 'failed' THEN now() ELSE NULL END
		WHERE id = $1`, id, state, discoveredURL, raw, preview, failure)
	return err
}

func finishJobTx(ctx context.Context, tx pgx.Tx, id int64, datasetID *string, releaseID *int64, state string, failure *string) error {
	_, err := tx.Exec(ctx, `
		UPDATE import_job
		SET dataset_id = COALESCE($2, dataset_id), release_id = COALESCE($3, release_id),
			state = $4, error_message = $5, started_at = COALESCE(started_at, now()),
			staged_raw_csv = CASE WHEN $4 IN ('confirmed', 'expired', 'succeeded', 'unchanged', 'failed', 'interrupted') THEN NULL ELSE staged_raw_csv END,
			finished_at = now()
		WHERE id = $1`, id, datasetID, releaseID, state, failure)
	return err
}

func (s *Service) finishJob(ctx context.Context, id int64, datasetID *string, releaseID *int64, state string, failure *string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE import_job
		SET dataset_id = COALESCE($2, dataset_id), release_id = COALESCE($3, release_id),
			state = $4, error_message = $5, started_at = COALESCE(started_at, now()),
			staged_raw_csv = CASE WHEN $4 IN ('confirmed', 'expired', 'succeeded', 'unchanged', 'failed', 'interrupted') THEN NULL ELSE staged_raw_csv END,
			finished_at = now()
		WHERE id = $1`, id, datasetID, releaseID, state, failure)
	return err
}

// ExpirePreviews clears every expired preview artifact in one atomic update.
func (s *Service) ExpirePreviews(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE import_job
		SET state = 'expired', staged_raw_csv = NULL, finished_at = now()
		WHERE state = 'preview_ready'
		  AND expires_at IS NOT NULL
		  AND expires_at <= $1`, s.now().UTC())
	if err != nil {
		return fmt.Errorf("expire import previews: %w", err)
	}
	return nil
}

func failureMessage(err error) *string {
	if err == nil {
		return nil
	}
	message := err.Error()
	if len(message) > 2_000 {
		message = message[:2_000]
	}
	return &message
}

// CreatePreview persists a queued job and starts bounded background fetching.
func (s *Service) CreatePreview(ctx context.Context, rawURL string) (api.ImportPreview, error) {
	page, err := who.ValidateIndicatorPageURL(rawURL)
	if err != nil {
		return api.ImportPreview{}, fmt.Errorf("%w: %v", api.ErrConflict, err)
	}
	if err := s.Seed(ctx); err != nil {
		return api.ImportPreview{}, err
	}
	var definition *who.DatasetDefinition
	var datasetID *string
	if curated, ok := who.DefinitionForIndicator(page); ok {
		definition = &curated
		datasetID = &curated.ID
	}
	select {
	case s.previewSlots <- struct{}{}:
	case <-ctx.Done():
		return api.ImportPreview{}, ctx.Err()
	default:
		return api.ImportPreview{}, fmt.Errorf("%w: another preview is already being prepared", api.ErrConflict)
	}
	expiresAt := s.now().UTC().Add(previewTTL)
	created, err := s.insertJob(ctx, datasetID, "preview", "queued", page.URL, &expiresAt)
	if err != nil {
		<-s.previewSlots
		return api.ImportPreview{}, err
	}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer func() { <-s.previewSlots }()
		s.fetchPreview(s.root, created.ID, page, definition)
	}()
	return previewFromJob(created, previewRecord{Dataset: definition}), nil
}

func (s *Service) fetchPreview(ctx context.Context, jobID int64, page who.IndicatorPage, definition *who.DatasetDefinition) {
	if err := s.markJobRunning(ctx, jobID); err != nil {
		s.logger.Error("mark preview running", "job_id", jobID, "error", err)
		return
	}
	fetched, err := s.fetcher.FetchFromIndicatorPage(ctx, page.URL)
	if err != nil {
		if updateErr := s.finishJob(ctx, jobID, nil, nil, "failed", failureMessage(err)); updateErr != nil {
			s.logger.Error("record preview fetch failure", "job_id", jobID, "error", updateErr)
		}
		return
	}
	preview, err := who.BuildPreview(fetched.Artifact.Bytes, who.PreviewOptions{
		Dataset: definition, SourceURL: fetched.Artifact.URL, AccessedAt: fetched.Artifact.AccessedAt,
		ResolveM49: s.resolveM49,
	})
	if definition == nil {
		generic := genericDefinition(page, preview)
		definition = &generic
		preview.Dataset = definition
	}
	record := previewRecord{
		Version: 1, Dataset: definition, Summary: summarizePreview(preview),
		Artifact: artifactMetadata{
			PageURL: page.URL, PageETag: fetched.Discovery.ETag, PageModified: fetched.Discovery.LastModified,
			DownloadURL: fetched.Artifact.URL, ETag: fetched.Artifact.ETag,
			LastModified: fetched.Artifact.LastModified, ContentType: fetched.Artifact.ContentType,
			ContentLength: fetched.Artifact.ContentLength,
		},
	}
	encoded, marshalErr := json.Marshal(record)
	if marshalErr != nil {
		err = fmt.Errorf("encode import preview: %w", marshalErr)
	}
	if err != nil {
		if updateErr := s.stageJob(ctx, jobID, "failed", fetched.Artifact.URL, fetched.Artifact.Bytes, encoded, failureMessage(err)); updateErr != nil {
			s.logger.Error("record preview parse failure", "job_id", jobID, "error", updateErr)
		}
		return
	}
	if err := s.stageJob(ctx, jobID, "preview_ready", fetched.Artifact.URL, fetched.Artifact.Bytes, encoded, nil); err != nil {
		s.logger.Error("persist import preview", "job_id", jobID, "error", err)
	}
}

func genericDefinition(page who.IndicatorPage, preview who.Preview) who.DatasetDefinition {
	definition := who.DatasetDefinition{
		ID:           "who-" + strings.ToLower(page.IndicatorID),
		Name:         "WHO indicator " + page.IndicatorID,
		PageURL:      page.URL,
		IndicatorID:  page.IndicatorID,
		ValueColumn:  preview.Schema.ValueColumn,
		ValueKind:    "number",
		Dimensions:   append([]string(nil), preview.Schema.DimensionColumns...),
		Capabilities: []string{"line", "map", "association"},
	}
	if len(preview.Series) > 0 {
		definition.Name = preview.Series[0].Identity.Measure
		definition.IndicatorCode = preview.Series[0].Identity.IndicatorCode
	}
	return definition
}

// Preview returns a persisted preview, marking it expired once its 24-hour
// confirmation window has elapsed.
func (s *Service) Preview(ctx context.Context, rawID string) (api.ImportPreview, error) {
	id, err := parseJobID(rawID)
	if err != nil {
		return api.ImportPreview{}, err
	}
	value, err := s.getJob(ctx, id)
	if err != nil {
		return api.ImportPreview{}, err
	}
	if value.State == "preview_ready" && value.ExpiresAt != nil && !value.ExpiresAt.After(s.now().UTC()) {
		if err := s.finishJob(ctx, value.ID, nil, nil, "expired", nil); err != nil {
			return api.ImportPreview{}, fmt.Errorf("expire import preview: %w", err)
		}
		value.State = "expired"
		now := s.now().UTC()
		value.FinishedAt = &now
	}
	record, err := decodePreview(value)
	if err != nil && len(value.Preview) > 0 {
		return api.ImportPreview{}, err
	}
	return previewFromJob(value, record), nil
}

func decodePreview(value job) (previewRecord, error) {
	if len(value.Preview) == 0 {
		return previewRecord{}, nil
	}
	var record previewRecord
	if err := json.Unmarshal(value.Preview, &record); err != nil {
		return previewRecord{}, fmt.Errorf("decode persisted import preview: %w", err)
	}
	return record, nil
}

func previewFromJob(value job, record previewRecord) api.ImportPreview {
	preview := record.Summary
	definition := record.Dataset
	result := api.ImportPreview{
		ID:           strconv.FormatInt(value.ID, 10),
		Status:       previewStatus(value.State),
		IndicatorURL: value.SourceURL,
		DownloadURL:  record.Artifact.DownloadURL,
		Headers:      append([]string(nil), preview.Schema.Headers...),
		Rows: api.RowAccounting{
			SourceRows:          int64(preview.Accounting.RowsRead),
			AcceptedRows:        int64(preview.Accounting.UniqueObservations),
			CollapsedDuplicates: int64(preview.Accounting.ExactDuplicates),
			RejectedRows:        int64(preview.Accounting.ConflictingRows + preview.Accounting.InvalidRows),
		},
		ExactDuplicates:       int64(preview.Accounting.ExactDuplicates),
		ConflictingDuplicates: int64(preview.Accounting.ConflictingRows),
		Warnings:              previewWarnings(preview.Diagnostics),
		CreatedAt:             value.CreatedAt.UTC(),
	}
	if value.ExpiresAt != nil {
		result.ExpiresAt = value.ExpiresAt.UTC()
	}
	if value.ErrorMessage != nil && *value.ErrorMessage != "" {
		result.Warnings = append(result.Warnings, "import failed: "+safeWarning(*value.ErrorMessage))
	}
	if len(preview.Schema.Headers) > 0 {
		result.SchemaFingerprint = preview.Schema.Fingerprint
	}
	if definition != nil {
		result.Measures = []api.Measure{{
			ID: definition.IndicatorCode, DatasetID: definition.ID, Name: definition.Name,
			Unit: definition.Unit, Statistic: definition.Statistic, ValueKind: apiValueKind(definition.ValueKind),
		}}
	}
	result.Units = append([]string(nil), preview.Units...)
	result.Dimensions = append([]api.DimensionDefinition(nil), preview.Dimensions...)
	result.UnmappedGeographies = append([]api.Geography(nil), preview.Unmapped...)
	return result
}

func previewStatus(state string) string {
	switch state {
	case "queued":
		return "pending"
	case "preview_ready":
		return "ready"
	case "confirmed", "failed", "expired", "running":
		return state
	default:
		return "failed"
	}
}

func previewWarnings(diagnostics who.Diagnostics) []string {
	seen := map[string]struct{}{}
	warnings := make([]string, 0)
	for _, diagnostic := range diagnostics.Entries {
		if diagnostic.Severity != who.DiagnosticWarning || diagnostic.Message == "" {
			continue
		}
		if _, exists := seen[diagnostic.Message]; exists {
			continue
		}
		seen[diagnostic.Message] = struct{}{}
		warnings = append(warnings, diagnostic.Message)
		if len(warnings) == 20 {
			break
		}
	}
	if diagnostics.Truncated {
		warnings = append(warnings, "diagnostic examples were truncated")
	}
	return warnings
}

func safeWarning(message string) string {
	message = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r == '\t' {
			return ' '
		}
		if r < ' ' {
			return -1
		}
		return r
	}, message)
	if len(message) > 500 {
		return message[:500]
	}
	return message
}

func summarizePreview(preview who.Preview) previewSummary {
	summary := previewSummary{
		SourceURL: preview.SourceURL, AccessedAt: preview.AccessedAt, SHA256: preview.SHA256, Bytes: preview.Bytes,
		Schema: preview.Schema, Accounting: preview.Accounting, Diagnostics: preview.Diagnostics,
	}
	units := make(map[string]struct{})
	dimensionValues := make(map[string]map[string]struct{})
	for _, code := range preview.Schema.DimensionColumns {
		dimensionValues[code] = map[string]struct{}{}
	}
	for _, series := range preview.Series {
		if series.Identity.Unit != "" {
			units[series.Identity.Unit] = struct{}{}
		}
		for code, item := range series.Identity.Dimensions {
			if dimensionValues[code] == nil {
				dimensionValues[code] = map[string]struct{}{}
			}
			dimensionValues[code][item] = struct{}{}
		}
	}
	for unit := range units {
		summary.Units = append(summary.Units, unit)
	}
	sort.Strings(summary.Units)
	for code, values := range dimensionValues {
		dimension := api.DimensionDefinition{Code: code, Name: dimensionName(code)}
		for item := range values {
			dimension.Values = append(dimension.Values, item)
		}
		sort.Strings(dimension.Values)
		summary.Dimensions = append(summary.Dimensions, dimension)
	}
	sort.Slice(summary.Dimensions, func(i, j int) bool { return summary.Dimensions[i].Code < summary.Dimensions[j].Code })
	seenUnmapped := map[string]struct{}{}
	for _, observation := range preview.Observations {
		if observation.CanonicalM49 != "" {
			continue
		}
		key := observation.SourceGeo.Code + "\x00" + observation.SourceGeo.Type + "\x00" + observation.SourceGeo.Name
		if _, exists := seenUnmapped[key]; exists {
			continue
		}
		seenUnmapped[key] = struct{}{}
		summary.Unmapped = append(summary.Unmapped, api.Geography{
			SourceCode: observation.SourceGeo.Code, Name: observation.SourceGeo.Name, Kind: observation.SourceGeo.Type,
		})
	}
	sort.Slice(summary.Unmapped, func(i, j int) bool { return summary.Unmapped[i].Name < summary.Unmapped[j].Name })
	return summary
}

func dimensionName(code string) string {
	switch code {
	case "DIM_SEX":
		return "Sex"
	case "DIM_AGE":
		return "Age"
	case "DIM_AMR_GLASS_AWARE":
		return "AWaRe category"
	default:
		return strings.ReplaceAll(strings.TrimPrefix(code, "DIM_"), "_", " ")
	}
}
