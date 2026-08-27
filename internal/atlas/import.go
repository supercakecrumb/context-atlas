package atlas

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/supercakecrumb/context-atlas/internal/api"
	"github.com/supercakecrumb/context-atlas/internal/store/db"
	"github.com/supercakecrumb/context-atlas/internal/who"
)

// ConfirmPreview reparses the staged CSV and atomically publishes its release
// together with the next immutable catalog snapshot.
func (s *Service) ConfirmPreview(ctx context.Context, rawID string) (api.ImportRun, error) {
	id, err := parseJobID(rawID)
	if err != nil {
		return api.ImportRun{}, err
	}
	if err := s.Seed(ctx); err != nil {
		return api.ImportRun{}, err
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return api.ImportRun{}, fmt.Errorf("begin preview confirmation: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, refreshLockKey); err != nil {
		return api.ImportRun{}, fmt.Errorf("lock catalog confirmation: %w", err)
	}
	value, err := getJobTx(ctx, tx, id)
	if err != nil {
		return api.ImportRun{}, err
	}
	if value.State == "preview_ready" && value.ExpiresAt != nil && !value.ExpiresAt.After(s.now().UTC()) {
		if _, err := tx.Exec(ctx, `UPDATE import_job SET state = 'expired', staged_raw_csv = NULL, finished_at = now() WHERE id = $1`, value.ID); err != nil {
			return api.ImportRun{}, fmt.Errorf("expire preview: %w", err)
		}
		if err := tx.Commit(ctx); err != nil {
			return api.ImportRun{}, fmt.Errorf("commit preview expiry: %w", err)
		}
		return api.ImportRun{}, fmt.Errorf("%w: import preview has expired", api.ErrConflict)
	}
	if value.State != "preview_ready" {
		return api.ImportRun{}, fmt.Errorf("%w: import preview is %s", api.ErrConflict, value.State)
	}
	record, err := decodePreview(value)
	if err != nil {
		return api.ImportRun{}, err
	}
	preview, definition, err := s.rebuildPreview(record, value.StagedRawCSV)
	if err != nil {
		failure := failureMessage(err)
		if finishErr := finishJobTx(ctx, tx, value.ID, nil, nil, "failed", failure); finishErr != nil {
			return api.ImportRun{}, fmt.Errorf("record invalid preview: %w", finishErr)
		}
		if commitErr := tx.Commit(ctx); commitErr != nil {
			return api.ImportRun{}, fmt.Errorf("commit invalid preview: %w", commitErr)
		}
		return api.ImportRun{}, fmt.Errorf("%w: %v", api.ErrConflict, err)
	}
	if err := ensureDatasetTx(ctx, tx, definition); err != nil {
		return api.ImportRun{}, err
	}
	releaseID, existing, err := persistReleaseTx(ctx, tx, definition, record, preview, value.StagedRawCSV)
	if err != nil {
		return api.ImportRun{}, err
	}

	head, releases, err := currentSnapshotTx(ctx, tx)
	if err != nil {
		return api.ImportRun{}, err
	}
	if !existing || head == nil || releases[definition.ID] != releaseID {
		releases, err = completeReleaseSetTx(ctx, tx, releases)
		if err != nil {
			return api.ImportRun{}, err
		}
		releases[definition.ID] = releaseID
		head, err = s.publishSnapshotTx(ctx, tx, releases)
		if err != nil {
			return api.ImportRun{}, err
		}
	}
	if err := finishJobTx(ctx, tx, value.ID, &definition.ID, &releaseID, "confirmed", nil); err != nil {
		return api.ImportRun{}, fmt.Errorf("confirm import job: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return api.ImportRun{}, fmt.Errorf("commit preview confirmation: %w", err)
	}
	return confirmedRun(value, definition.ID, rawID, head, preview), nil
}

func (s *Service) rebuildPreview(record previewRecord, raw []byte) (who.Preview, who.DatasetDefinition, error) {
	if record.Dataset == nil {
		return who.Preview{}, who.DatasetDefinition{}, errors.New("persisted preview has no dataset definition")
	}
	if len(raw) == 0 {
		return who.Preview{}, who.DatasetDefinition{}, errors.New("persisted preview has no staged CSV")
	}
	definition := *record.Dataset
	sourceURL := record.Artifact.DownloadURL
	if sourceURL == "" {
		sourceURL = record.Summary.SourceURL
	}
	preview, err := who.BuildPreview(raw, who.PreviewOptions{
		Dataset: &definition, SourceURL: sourceURL, AccessedAt: record.Summary.AccessedAt, ResolveM49: s.resolveM49,
	})
	if err != nil {
		return preview, definition, err
	}
	if record.Summary.SHA256 != "" && preview.SHA256 != record.Summary.SHA256 {
		return preview, definition, errors.New("staged CSV checksum no longer matches the preview")
	}
	return preview, definition, nil
}

func ensureDatasetTx(ctx context.Context, tx pgx.Tx, definition who.DatasetDefinition) error {
	definition, encoded, err := marshalDatasetDefinition(definition)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO dataset (id, who_indicator_id, who_measure_code, title, source_url, definition)
		VALUES ($1, $2, $3, $4, $5, $6::jsonb)
		ON CONFLICT (id) DO NOTHING`,
		definition.ID, definition.IndicatorID, definition.IndicatorCode, definition.Name, definition.PageURL, encoded,
	); err != nil {
		return fmt.Errorf("ensure preview dataset: %w", err)
	}
	var persisted []byte
	if err := tx.QueryRow(ctx, `SELECT definition FROM dataset WHERE id = $1`, definition.ID).Scan(&persisted); err != nil {
		return fmt.Errorf("read persisted dataset definition: %w", err)
	}
	var stored who.DatasetDefinition
	if err := json.Unmarshal(persisted, &stored); err != nil {
		return fmt.Errorf("decode persisted dataset definition: %w", err)
	}
	_, storedEncoded, err := marshalDatasetDefinition(stored)
	if err != nil {
		return fmt.Errorf("validate persisted dataset definition: %w", err)
	}
	if !bytes.Equal(storedEncoded, encoded) {
		return fmt.Errorf("dataset %s already has a different immutable definition", definition.ID)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO measure (dataset_id, code, title, description)
		VALUES ($1, $2, $3, '')
		ON CONFLICT DO NOTHING`, definition.ID, definition.IndicatorCode, definition.Name); err != nil {
		return fmt.Errorf("ensure preview measure: %w", err)
	}
	return nil
}

func persistReleaseTx(ctx context.Context, tx pgx.Tx, definition who.DatasetDefinition, record previewRecord, preview who.Preview, raw []byte) (int64, bool, error) {
	metadata, err := json.Marshal(record.Artifact)
	if err != nil {
		return 0, false, fmt.Errorf("encode release metadata: %w", err)
	}
	headers, err := json.Marshal(preview.Schema.Headers)
	if err != nil {
		return 0, false, fmt.Errorf("encode release headers: %w", err)
	}
	diagnostics, err := json.Marshal(preview.Diagnostics)
	if err != nil {
		return 0, false, fmt.Errorf("encode release diagnostics: %w", err)
	}
	var releaseID int64
	err = tx.QueryRow(ctx, `
		INSERT INTO dataset_release (
			dataset_id, source_url, citation, accessed_at, raw_csv, sha256, response_metadata, csv_headers,
			schema_fingerprint, parser_version, source_row_count, imported_row_count,
			duplicate_row_count, rejected_row_count, diagnostics
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15
		)
		ON CONFLICT (dataset_id, sha256) DO NOTHING
		RETURNING id`,
		definition.ID, preview.SourceURL, citation(definition), preview.AccessedAt, raw, preview.SHA256,
		metadata, headers, preview.Schema.Fingerprint, parserVersion,
		preview.Accounting.RowsRead, preview.Accounting.UniqueObservations, preview.Accounting.ExactDuplicates,
		preview.Accounting.ConflictingRows+preview.Accounting.InvalidRows, diagnostics,
	).Scan(&releaseID)
	if errors.Is(err, pgx.ErrNoRows) {
		if err := tx.QueryRow(ctx, `SELECT id FROM dataset_release WHERE dataset_id = $1 AND sha256 = $2`, definition.ID, preview.SHA256).Scan(&releaseID); err != nil {
			return 0, false, fmt.Errorf("find unchanged dataset release: %w", err)
		}
		return releaseID, true, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("insert dataset release: %w", err)
	}
	if err := insertPreviewFacts(ctx, tx, definition, releaseID, preview); err != nil {
		return 0, false, err
	}
	return releaseID, false, nil
}

func insertPreviewFacts(ctx context.Context, tx pgx.Tx, definition who.DatasetDefinition, releaseID int64, preview who.Preview) error {
	seriesIDs := make(map[string]int64, len(preview.Series))
	for _, item := range preview.Series {
		dimensionsHash := sha256.Sum256([]byte(item.DimensionsJSON))
		id, err := upsertSeries(ctx, tx, definition, item, hex.EncodeToString(dimensionsHash[:]))
		if err != nil {
			return err
		}
		seriesIDs[item.Hash] = id
	}
	geographies := db.New(tx)
	geographyIDs := make(map[string]int64)
	rows := make([][]any, 0, len(preview.Observations))
	for _, observation := range preview.Observations {
		key := sourceGeographyKey(observation)
		geographyID, exists := geographyIDs[key]
		if !exists {
			m49 := pgtype.Text{}
			if observation.CanonicalM49 != "" {
				m49 = pgtype.Text{String: observation.CanonicalM49, Valid: true}
			}
			geography, err := geographies.InsertSourceGeography(ctx, db.InsertSourceGeographyParams{
				SourceSystem: "WHO", SourceCode: observation.SourceGeo.Code, Name: observation.SourceGeo.Name,
				GeographyKind: observation.SourceGeo.Type, CanonicalM49Code: m49,
			})
			if err != nil {
				return fmt.Errorf("insert source geography %q: %w", observation.SourceGeo.Name, err)
			}
			geographyID = geography.ID
			geographyIDs[key] = geographyID
		}
		seriesID, exists := seriesIDs[observation.SeriesHash]
		if !exists {
			return fmt.Errorf("observation refers to unknown series %s", observation.SeriesHash)
		}
		rows = append(rows, []any{
			definition.ID, releaseID, seriesID, geographyID, int16(observation.Year), observation.Value.Raw, observation.Value.Display,
			numericArgument(observation.Value.Numeric), numericArgument(boundNumeric(observation, observation.LowerBound)),
			numericArgument(boundNumeric(observation, observation.UpperBound)), string(observation.Value.Status), observation.SourceGeo.PublishState,
			observation.SourceRowKey, nullableText(observation.CanonicalM49),
		})
	}
	if len(rows) == 0 {
		return nil
	}
	_, err := tx.CopyFrom(ctx, pgx.Identifier{"observation"}, []string{
		"dataset_id", "release_id", "series_id", "source_geography_id", "year", "raw_value", "display_value",
		"numeric_value", "lower_bound", "upper_bound", "value_status", "publish_state", "source_row_key", "canonical_m49_code",
	}, pgx.CopyFromRows(rows))
	if err != nil {
		return fmt.Errorf("copy observations: %w", err)
	}
	return nil
}

func upsertSeries(ctx context.Context, tx pgx.Tx, definition who.DatasetDefinition, item who.Series, dimensionsHash string) (int64, error) {
	valueKind := apiValueKind(item.Identity.ValueKind)
	var id int64
	err := tx.QueryRow(ctx, `
		INSERT INTO series (dataset_id, measure_code, label, dimensions, dimensions_hash, unit, statistic, value_kind)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (dataset_id, measure_code, dimensions_hash, unit, statistic, value_kind) DO NOTHING
		RETURNING id`,
		definition.ID, definition.IndicatorCode, item.Identity.Measure, []byte(item.DimensionsJSON), dimensionsHash,
		item.Identity.Unit, item.Identity.Statistic, valueKind,
	).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		err = tx.QueryRow(ctx, `
			SELECT id FROM series
			WHERE dataset_id = $1 AND measure_code = $2 AND dimensions_hash = $3
				AND unit = $4 AND statistic = $5 AND value_kind = $6`,
			definition.ID, definition.IndicatorCode, dimensionsHash, item.Identity.Unit, item.Identity.Statistic, valueKind,
		).Scan(&id)
	}
	if err != nil {
		return 0, fmt.Errorf("insert or find series: %w", err)
	}
	return id, nil
}

func sourceGeographyKey(observation who.Observation) string {
	return strings.Join([]string{
		"WHO", observation.SourceGeo.Code, observation.SourceGeo.Type, observation.SourceGeo.Name, observation.CanonicalM49,
	}, "\x00")
}

func numericArgument(value *float64) any {
	if value == nil {
		return pgtype.Numeric{}
	}
	return pgtype.Float8{Float64: *value, Valid: true}
}

func boundNumeric(observation who.Observation, value who.Value) *float64 {
	if observation.Value.Status != who.ValueNumeric || value.Status != who.ValueNumeric {
		return nil
	}
	return value.Numeric
}

func nullableText(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func citation(definition who.DatasetDefinition) string {
	return "World Health Organization (WHO): " + definition.Name + " (" + definition.IndicatorID + ")"
}

type publishedSnapshot struct {
	ref      api.SnapshotRef
	releases map[string]int64
}

func currentSnapshotTx(ctx context.Context, tx pgx.Tx) (*publishedSnapshot, map[string]int64, error) {
	var id string
	var m49ID int64
	var createdAt time.Time
	err := tx.QueryRow(ctx, `
		SELECT s.id, s.m49_release_id, s.created_at
		FROM catalog_head h JOIN catalog_snapshot s ON s.id = h.snapshot_id
		WHERE h.singleton`).Scan(&id, &m49ID, &createdAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, map[string]int64{}, nil
	}
	if err != nil {
		return nil, nil, fmt.Errorf("read catalog head: %w", err)
	}
	releases := make(map[string]int64)
	rows, err := tx.Query(ctx, `SELECT dataset_id, release_id FROM catalog_snapshot_release WHERE snapshot_id = $1`, id)
	if err != nil {
		return nil, nil, fmt.Errorf("read catalog snapshot releases: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var datasetID string
		var releaseID int64
		if err := rows.Scan(&datasetID, &releaseID); err != nil {
			return nil, nil, err
		}
		releases[datasetID] = releaseID
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	return &publishedSnapshot{ref: api.SnapshotRef{
		ID: id, M49ReferenceRelease: strconv.FormatInt(m49ID, 10), CreatedAt: createdAt.UTC(),
	}, releases: releases}, releases, nil
}

func completeReleaseSetTx(ctx context.Context, tx pgx.Tx, current map[string]int64) (map[string]int64, error) {
	result := make(map[string]int64, len(current))
	for datasetID, releaseID := range current {
		result[datasetID] = releaseID
	}
	rows, err := tx.Query(ctx, `
		SELECT DISTINCT ON (dataset_id) dataset_id, id
		FROM dataset_release
		ORDER BY dataset_id, id DESC`)
	if err != nil {
		return nil, fmt.Errorf("complete catalog release set: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var datasetID string
		var releaseID int64
		if err := rows.Scan(&datasetID, &releaseID); err != nil {
			return nil, err
		}
		if _, present := result[datasetID]; !present {
			result[datasetID] = releaseID
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

func (s *Service) publishSnapshotTx(ctx context.Context, tx pgx.Tx, releases map[string]int64) (*publishedSnapshot, error) {
	if len(releases) == 0 {
		return nil, errors.New("cannot publish an empty catalog snapshot")
	}
	m49ID, err := s.currentM49ReleaseIDTx(ctx, tx)
	if err != nil {
		return nil, err
	}
	id := "snap-" + uuid.NewString()
	var createdAt time.Time
	if err := tx.QueryRow(ctx, `
		INSERT INTO catalog_snapshot (id, m49_release_id) VALUES ($1, $2) RETURNING created_at`, id, m49ID).Scan(&createdAt); err != nil {
		return nil, fmt.Errorf("insert catalog snapshot: %w", err)
	}
	for datasetID, releaseID := range releases {
		if _, err := tx.Exec(ctx, `
			INSERT INTO catalog_snapshot_release (snapshot_id, dataset_id, release_id)
			VALUES ($1, $2, $3)`, id, datasetID, releaseID); err != nil {
			return nil, fmt.Errorf("pin catalog release %s: %w", datasetID, err)
		}
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO catalog_head (singleton, snapshot_id) VALUES (true, $1)
		ON CONFLICT (singleton) DO UPDATE SET snapshot_id = EXCLUDED.snapshot_id, updated_at = now()`, id); err != nil {
		return nil, fmt.Errorf("advance catalog head: %w", err)
	}
	return &publishedSnapshot{ref: api.SnapshotRef{
		ID: id, M49ReferenceRelease: strconv.FormatInt(m49ID, 10), CreatedAt: createdAt.UTC(),
	}, releases: releases}, nil
}

func (s *Service) currentM49ReleaseIDTx(ctx context.Context, tx pgx.Tx) (int64, error) {
	sum := sha256.Sum256(s.referenceRaw)
	var m49ID int64
	if err := tx.QueryRow(ctx, `SELECT id FROM m49_reference_release WHERE sha256 = $1`, hex.EncodeToString(sum[:])).Scan(&m49ID); err != nil {
		return 0, fmt.Errorf("find seeded M49 release: %w", err)
	}
	return m49ID, nil
}

func confirmedRun(value job, datasetID, previewID string, snapshot *publishedSnapshot, preview who.Preview) api.ImportRun {
	finished := time.Now().UTC()
	if snapshot == nil {
		return api.ImportRun{}
	}
	return api.ImportRun{
		ID: strconv.FormatInt(value.ID, 10), Kind: "confirm", Status: "succeeded", DatasetID: datasetID,
		PreviewID: previewID, Snapshot: &snapshot.ref, Rows: rowAccounting(preview),
		StartedAt: jobStarted(value), FinishedAt: &finished,
	}
}

func rowAccounting(preview who.Preview) api.RowAccounting {
	return api.RowAccounting{
		SourceRows: int64(preview.Accounting.RowsRead), AcceptedRows: int64(preview.Accounting.UniqueObservations),
		CollapsedDuplicates: int64(preview.Accounting.ExactDuplicates),
		RejectedRows:        int64(preview.Accounting.ConflictingRows + preview.Accounting.InvalidRows),
	}
}

func jobStarted(value job) time.Time {
	if value.StartedAt != nil {
		return value.StartedAt.UTC()
	}
	return value.CreatedAt.UTC()
}
