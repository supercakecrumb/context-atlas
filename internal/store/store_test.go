package store

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/supercakecrumb/context-atlas/internal/store/db"
)

func TestValidateTestDSN(t *testing.T) {
	tests := []struct {
		name string
		dsn  string
		want bool
	}{
		{name: "test database URL", dsn: "postgres://user:password@localhost/context_atlas_test?sslmode=disable", want: true},
		{name: "test database keyword DSN", dsn: "host=localhost dbname=atlas_test user=postgres", want: true},
		{name: "production database", dsn: "postgres://user:password@localhost/context_atlas?sslmode=disable"},
		{name: "invalid DSN", dsn: "://not-a-dsn"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateTestDSN(tc.dsn)
			if (err == nil) != tc.want {
				t.Fatalf("ValidateTestDSN() error = %v, want success = %v", err, tc.want)
			}
			if !tc.want && !errors.Is(err, ErrUnsafeTestDSN) {
				t.Fatalf("ValidateTestDSN() error = %v, want ErrUnsafeTestDSN", err)
			}
		})
	}
}

func TestMigrationsAndPersistenceInvariants(t *testing.T) {
	store, ctx := integrationStore(t)
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("second migration must be idempotent: %v", err)
	}

	m49ReleaseID := seedM49Release(t, store, ctx)
	firstReleaseID, firstSeriesID := seedRelease(t, store, ctx, "first")

	if _, err := store.Pool().Exec(ctx, `UPDATE dataset_release SET citation = 'changed' WHERE id = $1`, firstReleaseID); err == nil {
		t.Fatal("dataset release update unexpectedly succeeded")
	} else {
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != "55000" {
			t.Fatalf("dataset release update error = %v, want immutable SQLSTATE", err)
		}
	}

	snapshot, err := store.CreateSnapshot(ctx, SnapshotInput{
		ID:           "snapshot-one",
		M49ReleaseID: m49ReleaseID,
		ReleaseIDs:   []int64{firstReleaseID},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := len(snapshot.Releases); got != 1 {
		t.Fatalf("snapshot has %d releases, want 1", got)
	}
	association, err := store.AssociationForExactYears(ctx, snapshot.ID, firstSeriesID, 2020, firstSeriesID, 2020, nil)
	if err != nil {
		t.Fatal(err)
	}
	if association.PairedN != 1 || association.Coefficient != nil {
		t.Fatalf("association included non-published numeric row: %+v", association)
	}

	secondReleaseID, _ := seedRelease(t, store, ctx, "second")
	_, err = store.CreateSnapshot(ctx, SnapshotInput{
		ID:           "snapshot-incomplete",
		M49ReleaseID: m49ReleaseID,
		ReleaseIDs:   []int64{secondReleaseID},
	})
	if !errors.Is(err, ErrIncompleteSnapshot) {
		t.Fatalf("incomplete snapshot error = %v, want ErrIncompleteSnapshot", err)
	}
	var incompleteCount int
	if err := store.Pool().QueryRow(ctx, `SELECT count(*) FROM catalog_snapshot WHERE id = 'snapshot-incomplete'`).Scan(&incompleteCount); err != nil {
		t.Fatal(err)
	}
	if incompleteCount != 0 {
		t.Fatalf("incomplete snapshot persisted %d rows", incompleteCount)
	}

	if _, err := store.CreateSnapshot(ctx, SnapshotInput{
		ID:           "snapshot-two",
		M49ReleaseID: m49ReleaseID,
		ReleaseIDs:   []int64{firstReleaseID, secondReleaseID},
	}); err != nil {
		t.Fatal(err)
	}
	latest, err := store.LatestSnapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if latest.ID != "snapshot-two" || len(latest.Releases) != 2 {
		t.Fatalf("latest snapshot = %+v, want snapshot-two with two releases", latest)
	}
}

func TestSourceGeographyIdentityPreservesRenameAndRemapHistory(t *testing.T) {
	store, ctx := integrationStore(t)
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	insert := func(name, m49 string, mapped bool) int64 {
		t.Helper()
		canonical := pgtype.Text{}
		if mapped {
			canonical = pgtype.Text{String: m49, Valid: true}
		}
		geography, err := store.Queries().InsertSourceGeography(ctx, db.InsertSourceGeographyParams{
			SourceSystem:     "WHO",
			SourceCode:       "XKX",
			Name:             name,
			GeographyKind:    "country_or_area",
			CanonicalM49Code: canonical,
		})
		if err != nil {
			t.Fatal(err)
		}
		return geography.ID
	}

	original := insert("Original name", "001", true)
	if same := insert("Original name", "001", true); same != original {
		t.Fatalf("exact immutable identity created a duplicate: %d != %d", same, original)
	}
	if renamed := insert("Renamed", "001", true); renamed == original {
		t.Fatal("WHO rename overwrote immutable source geography")
	}
	if remapped := insert("Original name", "002", true); remapped == original {
		t.Fatal("WHO remap overwrote immutable source geography")
	}
	unmapped := insert("Unmapped", "", false)
	if same := insert("Unmapped", "", false); same != unmapped {
		t.Fatalf("NULL canonical M49 identity created a duplicate: %d != %d", same, unmapped)
	}
}

func integrationStore(t *testing.T) (*Store, context.Context) {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	if err := ValidateTestDSN(dsn); err != nil {
		t.Fatalf("refusing to reset TEST_DATABASE_URL: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	store, err := Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)

	// The test-DSN fuse above is intentionally adjacent to this destructive reset.
	if _, err := store.Pool().Exec(ctx, `DROP SCHEMA public CASCADE`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Pool().Exec(ctx, `CREATE SCHEMA public`); err != nil {
		t.Fatal(err)
	}
	return store, ctx
}

func seedM49Release(t *testing.T, store *Store, ctx context.Context) int64 {
	t.Helper()
	var id int64
	err := store.Pool().QueryRow(ctx, `
		INSERT INTO m49_reference_release (classification_version, source_url, accessed_at, raw_payload, sha256)
		VALUES ($1, $2, now(), $3, $4)
		RETURNING id`,
		"current", "https://unstats.un.org/m49", []byte("m49"), strings.Repeat("a", 64),
	).Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func seedRelease(t *testing.T, store *Store, ctx context.Context, suffix string) (int64, int64) {
	t.Helper()
	datasetID := "dataset-" + suffix
	measureCode := "measure-" + suffix
	if _, err := store.Pool().Exec(ctx, `
		INSERT INTO dataset (id, who_indicator_id, who_measure_code, title, source_url)
		VALUES ($1, $2, $3, $4, $5)`,
		datasetID, "indicator-"+suffix, measureCode, "Dataset "+suffix, "https://data.who.int/"+suffix,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Pool().Exec(ctx, `
		INSERT INTO measure (dataset_id, code, title)
		VALUES ($1, $2, $3)`, datasetID, measureCode, "Measure "+suffix); err != nil {
		t.Fatal(err)
	}

	var releaseID int64
	err := store.Pool().QueryRow(ctx, `
		INSERT INTO dataset_release (
			dataset_id, source_url, citation, accessed_at, raw_csv, sha256,
			response_metadata, csv_headers, schema_fingerprint, parser_version,
			source_row_count, imported_row_count, duplicate_row_count, rejected_row_count, diagnostics
		) VALUES (
			$1, $2, $3, now(), $4, $5, $6, $7, $8, $9, 2, 2, 0, 0, $10
		) RETURNING id`,
		datasetID,
		"https://data.who.int/download/"+suffix,
		"WHO citation",
		[]byte("YEAR,VALUE\\n2020,1"),
		strings.Repeat("d", 64),
		[]byte(`{"status":200}`),
		[]byte(`["YEAR","VALUE"]`),
		strings.Repeat("b", 64),
		"v1",
		[]byte(`{}`),
	).Scan(&releaseID)
	if err != nil {
		t.Fatal(err)
	}

	var seriesID, geographyID int64
	err = store.Pool().QueryRow(ctx, `
		INSERT INTO series (dataset_id, measure_code, label, dimensions, dimensions_hash, unit, statistic, value_kind)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id`,
		datasetID, measureCode, "Series "+suffix, []byte(`{}`), strings.Repeat("c", 64), "per 100k", "estimate", "rate",
	).Scan(&seriesID)
	if err != nil {
		t.Fatal(err)
	}
	err = store.Pool().QueryRow(ctx, `
		INSERT INTO source_geography (source_system, source_code, name, geography_kind, canonical_m49_code)
		VALUES ('WHO', $1, $2, 'country_or_area', '001')
		RETURNING id`, "geo-"+suffix, "Geography "+suffix).Scan(&geographyID)
	if err != nil {
		t.Fatal(err)
	}

	insertObservation := `
		INSERT INTO observation (
			dataset_id, release_id, series_id, source_geography_id, year, raw_value,
			display_value, numeric_value, value_status, publish_state, source_row_key, canonical_m49_code
		) VALUES ($1, $2, $3, $4, 2020, '1', '1', 1, 'numeric', $5, $6, $7)`
	if _, err := store.Pool().Exec(ctx, insertObservation, datasetID, releaseID, seriesID, geographyID, "PUBLISHED", "row-"+suffix, "001"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Pool().Exec(ctx, insertObservation, datasetID, releaseID, seriesID, geographyID, "DRAFT", "row-duplicate-"+suffix, "001"); err == nil {
		t.Fatal("duplicate observation differing only in publish state unexpectedly succeeded")
	}
	var draftGeographyID int64
	if err := store.Pool().QueryRow(ctx, `
		INSERT INTO source_geography (source_system, source_code, name, geography_kind, canonical_m49_code)
		VALUES ('WHO', $1, $2, 'country_or_area', '002')
		RETURNING id`, "geo-draft-"+suffix, "Draft geography "+suffix).Scan(&draftGeographyID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Pool().Exec(ctx, insertObservation, datasetID, releaseID, seriesID, draftGeographyID, "DRAFT", "row-draft-"+suffix, "002"); err != nil {
		t.Fatal(err)
	}
	return releaseID, seriesID
}
