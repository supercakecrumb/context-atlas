// Package store owns PostgreSQL persistence for the Context Atlas monolith.
package store

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/supercakecrumb/context-atlas/internal/store/db"
)

var (
	ErrNoSnapshot         = errors.New("catalog snapshot does not exist")
	ErrUnsafeTestDSN      = errors.New("refusing destructive test database operation")
	ErrIncompleteSnapshot = errors.New("snapshot must pin one release for every imported dataset")
)

// Store wraps the shared pool and sqlc-generated queries.
type Store struct {
	pool *pgxpool.Pool
	q    *db.Queries
}

// Open creates and verifies a PostgreSQL pool.
func Open(ctx context.Context, dsn string) (*Store, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("open PostgreSQL pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping PostgreSQL: %w", err)
	}
	return New(pool), nil
}

// New wraps an existing pool. It is useful for application wiring and tests.
func New(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool, q: db.New(pool)}
}

// Pool exposes the pool for bounded operations that are not yet part of the
// generated query surface.
func (s *Store) Pool() *pgxpool.Pool { return s.pool }

// Queries exposes generated sqlc queries for adapters that need a stable CRUD
// operation. Snapshot creation remains a Store method because it is atomic.
func (s *Store) Queries() *db.Queries { return s.q }

// Close releases the pool.
func (s *Store) Close() {
	if s != nil && s.pool != nil {
		s.pool.Close()
	}
}

// ValidateTestDSN is the fuse required before any test code drops schema
// objects. It deliberately reports no DSN details.
func ValidateTestDSN(dsn string) error {
	config, err := pgx.ParseConfig(dsn)
	if err != nil {
		return fmt.Errorf("%w: invalid DSN", ErrUnsafeTestDSN)
	}
	if !strings.Contains(strings.ToLower(config.Database), "test") {
		return fmt.Errorf("%w: database name must contain test", ErrUnsafeTestDSN)
	}
	return nil
}

// SnapshotInput is the complete, immutable release set to publish together.
// ID is supplied by the caller so shared URLs are stable and reproducible.
type SnapshotInput struct {
	ID           string
	M49ReleaseID int64
	ReleaseIDs   []int64
}

// SnapshotRelease identifies one dataset release pinned by a snapshot.
type SnapshotRelease struct {
	DatasetID string
	ReleaseID int64
}

// Snapshot is the provenance anchor returned by catalog reads and shares.
type Snapshot struct {
	ID           string
	M49ReleaseID int64
	CreatedAt    time.Time
	Releases     []SnapshotRelease
}

// CreateSnapshot atomically creates a snapshot, pins every imported dataset,
// and moves the explicit latest pointer. Earlier snapshots remain unchanged.
func (s *Store) CreateSnapshot(ctx context.Context, input SnapshotInput) (Snapshot, error) {
	if s == nil || s.pool == nil || s.q == nil {
		return Snapshot{}, errors.New("store is not initialized")
	}
	if strings.TrimSpace(input.ID) == "" || input.M49ReleaseID < 1 || len(input.ReleaseIDs) == 0 {
		return Snapshot{}, errors.New("snapshot ID, M49 release, and at least one dataset release are required")
	}

	seen := make(map[int64]struct{}, len(input.ReleaseIDs))
	for _, id := range input.ReleaseIDs {
		if id < 1 {
			return Snapshot{}, errors.New("snapshot release IDs must be positive")
		}
		if _, duplicate := seen[id]; duplicate {
			return Snapshot{}, errors.New("snapshot release IDs must be unique")
		}
		seen[id] = struct{}{}
	}

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return Snapshot{}, fmt.Errorf("begin snapshot transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := s.q.WithTx(tx)

	if _, err := q.GetM49ReferenceRelease(ctx, input.M49ReleaseID); err != nil {
		return Snapshot{}, fmt.Errorf("find M49 release: %w", err)
	}
	releaseRows, err := q.ListReleaseDatasetPairsByIDs(ctx, input.ReleaseIDs)
	if err != nil {
		return Snapshot{}, fmt.Errorf("find dataset releases: %w", err)
	}
	if len(releaseRows) != len(seen) {
		return Snapshot{}, errors.New("one or more dataset releases do not exist")
	}

	releases := make([]SnapshotRelease, 0, len(releaseRows))
	datasets := make(map[string]struct{}, len(releaseRows))
	for _, row := range releaseRows {
		if _, duplicate := datasets[row.DatasetID]; duplicate {
			return Snapshot{}, errors.New("snapshot cannot pin two releases for one dataset")
		}
		datasets[row.DatasetID] = struct{}{}
		releases = append(releases, SnapshotRelease{DatasetID: row.DatasetID, ReleaseID: row.ID})
	}
	importedDatasets, err := q.ListDatasetsWithRelease(ctx)
	if err != nil {
		return Snapshot{}, fmt.Errorf("list imported datasets: %w", err)
	}
	for _, datasetID := range importedDatasets {
		if _, included := datasets[datasetID]; !included {
			return Snapshot{}, ErrIncompleteSnapshot
		}
	}

	created, err := q.InsertCatalogSnapshot(ctx, db.InsertCatalogSnapshotParams{
		ID:           input.ID,
		M49ReleaseID: input.M49ReleaseID,
	})
	if err != nil {
		return Snapshot{}, fmt.Errorf("create catalog snapshot: %w", err)
	}
	for _, release := range releases {
		if err := q.InsertCatalogSnapshotRelease(ctx, db.InsertCatalogSnapshotReleaseParams{
			SnapshotID: created.ID,
			DatasetID:  release.DatasetID,
			ReleaseID:  release.ReleaseID,
		}); err != nil {
			return Snapshot{}, fmt.Errorf("pin dataset release: %w", err)
		}
	}
	if err := q.SetCatalogHead(ctx, created.ID); err != nil {
		return Snapshot{}, fmt.Errorf("set latest catalog snapshot: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Snapshot{}, fmt.Errorf("commit catalog snapshot: %w", err)
	}

	return Snapshot{
		ID:           created.ID,
		M49ReleaseID: created.M49ReleaseID,
		CreatedAt:    created.CreatedAt.Time.UTC(),
		Releases:     releases,
	}, nil
}

// LatestSnapshot returns the explicit current catalog pointer, not an inferred
// "latest" release. This keeps share URLs and public reads reproducible.
func (s *Store) LatestSnapshot(ctx context.Context) (Snapshot, error) {
	if s == nil || s.q == nil {
		return Snapshot{}, errors.New("store is not initialized")
	}
	current, err := s.q.GetCatalogHead(ctx)
	if errors.Is(err, pgx.ErrNoRows) {
		return Snapshot{}, ErrNoSnapshot
	}
	if err != nil {
		return Snapshot{}, fmt.Errorf("get latest catalog snapshot: %w", err)
	}
	rows, err := s.q.ListCatalogSnapshotReleases(ctx, current.ID)
	if err != nil {
		return Snapshot{}, fmt.Errorf("list latest catalog releases: %w", err)
	}
	releases := make([]SnapshotRelease, 0, len(rows))
	for _, row := range rows {
		releases = append(releases, SnapshotRelease{DatasetID: row.DatasetID, ReleaseID: row.ReleaseID})
	}
	return Snapshot{
		ID:           current.ID,
		M49ReleaseID: current.M49ReleaseID,
		CreatedAt:    current.CreatedAt.Time.UTC(),
		Releases:     releases,
	}, nil
}

// Association is the database-side Pearson result for two independently exact
// series/year selections. Coefficient is absent for too few points or zero
// variance; callers can surface the warning without inventing a substitute.
type Association struct {
	PairedN     int64
	Coefficient *float64
}

// AssociationForExactYears delegates pairing and corr() to PostgreSQL.
func (s *Store) AssociationForExactYears(ctx context.Context, snapshotID string, xSeriesID int64, xYear int16, ySeriesID int64, yYear int16, m49Codes []string) (Association, error) {
	if s == nil || s.q == nil {
		return Association{}, errors.New("store is not initialized")
	}
	result, err := s.q.CalculateAssociation(ctx, db.CalculateAssociationParams{
		SnapshotID: snapshotID,
		SeriesID:   xSeriesID,
		Year:       xYear,
		SeriesID_2: ySeriesID,
		Year_2:     yYear,
		Column6:    m49Codes,
	})
	if err != nil {
		return Association{}, fmt.Errorf("calculate association: %w", err)
	}
	coefficient, parseErr := strconv.ParseFloat(result.PearsonR, 64)
	if parseErr != nil {
		return Association{}, fmt.Errorf("parse Pearson coefficient: %w", parseErr)
	}
	association := Association{PairedN: result.PairedN}
	if result.PairedN >= 3 && !math.IsNaN(coefficient) {
		association.Coefficient = &coefficient
	}
	return association, nil
}
