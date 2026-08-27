// Package atlas connects the immutable Context Atlas catalog to PostgreSQL.
package atlas

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/supercakecrumb/context-atlas/internal/reference"
	"github.com/supercakecrumb/context-atlas/internal/who"
)

const (
	parserVersion   = "who-preview-v1"
	previewTTL      = 24 * time.Hour
	refreshStaleAge = 24 * time.Hour
	freshnessAge    = 72 * time.Hour

	seedLockKey    int64 = 404_207_001
	refreshLockKey int64 = 404_207_002
)

// Service is the single data/import implementation used by API and scheduler
// wiring. It owns background work, but never owns or closes the shared pool.
type Service struct {
	pool         *pgxpool.Pool
	referenceDir string

	logger       *slog.Logger
	fetcher      who.Fetcher
	reference    reference.Document
	referenceRaw []byte
	referenceAt  time.Time
	mapBody      []byte
	mapETag      string
	leaves       map[string]reference.Area

	root         context.Context
	cancel       context.CancelFunc
	wg           sync.WaitGroup
	previewSlots chan struct{}
	refreshSlots chan struct{}
	now          func() time.Time
}

// New validates the checked-in reference assets once and prepares a service.
// Database seed work is deliberately deferred until migrations are available.
func New(pool *pgxpool.Pool, referenceDir string, logger *slog.Logger) (*Service, error) {
	if pool == nil {
		return nil, errors.New("atlas requires a PostgreSQL pool")
	}
	if referenceDir == "" {
		referenceDir = "assets/reference"
	}
	document, err := reference.ValidateDir(referenceDir)
	if err != nil {
		return nil, fmt.Errorf("validate reference assets: %w", err)
	}
	raw, err := os.ReadFile(filepath.Join(referenceDir, "un-m49-current.json"))
	if err != nil {
		return nil, fmt.Errorf("read M49 reference: %w", err)
	}
	geometry, err := os.ReadFile(filepath.Join(referenceDir, "natural-earth-admin0-50m.geojson"))
	if err != nil {
		return nil, fmt.Errorf("read Natural Earth geometry: %w", err)
	}
	info, err := os.Stat(filepath.Join(referenceDir, "un-m49-current.json"))
	if err != nil {
		return nil, fmt.Errorf("stat M49 reference: %w", err)
	}
	if logger == nil {
		logger = slog.Default()
	}
	root, cancel := context.WithCancel(context.Background())
	leaves := make(map[string]reference.Area, len(document.Areas))
	for _, area := range document.Areas {
		leaves[area.M49] = area
	}
	mapSum := sha256.Sum256(geometry)
	return &Service{
		pool:         pool,
		referenceDir: referenceDir,
		logger:       logger,
		fetcher:      who.NewFetcher(who.FetchOptions{}),
		reference:    document,
		referenceRaw: raw,
		referenceAt:  info.ModTime().UTC(),
		mapBody:      geometry,
		mapETag:      `"sha256-` + hex.EncodeToString(mapSum[:]) + `"`,
		leaves:       leaves,
		root:         root,
		cancel:       cancel,
		previewSlots: make(chan struct{}, 2),
		refreshSlots: make(chan struct{}, 1),
		now:          time.Now,
	}, nil
}

// Close cancels owned background work. It intentionally does not close pool.
func (s *Service) Close() {
	if s == nil {
		return
	}
	s.cancel()
	s.wg.Wait()
}

// Seed idempotently makes both checked-in M49 data and curated WHO definitions
// available. Public reads call it before resolving a snapshot.
func (s *Service) Seed(ctx context.Context) error {
	if err := s.EnsureReference(ctx); err != nil {
		return err
	}
	return s.ensureDefinitions(ctx)
}

// EnsureReference idempotently inserts the immutable checked-in M49 release.
func (s *Service) EnsureReference(ctx context.Context) error {
	_, err := s.ensureReference(ctx)
	return err
}

func (s *Service) ensureReference(ctx context.Context) (int64, error) {
	if s == nil || s.pool == nil {
		return 0, errors.New("atlas service is not initialized")
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return 0, fmt.Errorf("begin M49 seed transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, seedLockKey); err != nil {
		return 0, fmt.Errorf("lock M49 seed: %w", err)
	}

	sum := sha256.Sum256(s.referenceRaw)
	checksum := hex.EncodeToString(sum[:])
	var releaseID int64
	err = tx.QueryRow(ctx, `
		INSERT INTO m49_reference_release (classification_version, source_url, accessed_at, raw_payload, sha256)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (sha256) DO NOTHING
		RETURNING id`,
		s.reference.Classification.VersionLabel,
		s.reference.Classification.SourceURL,
		s.referenceAt,
		s.referenceRaw,
		checksum,
	).Scan(&releaseID)
	if errors.Is(err, pgx.ErrNoRows) {
		err = tx.QueryRow(ctx, `SELECT id FROM m49_reference_release WHERE sha256 = $1`, checksum).Scan(&releaseID)
	}
	if err != nil {
		return 0, fmt.Errorf("insert or find M49 release: %w", err)
	}

	var geographyCount int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM m49_geography WHERE m49_release_id = $1`, releaseID).Scan(&geographyCount); err != nil {
		return 0, fmt.Errorf("count M49 geographies: %w", err)
	}
	if geographyCount == 0 {
		for _, area := range s.reference.Areas {
			if _, err := tx.Exec(ctx, `
				INSERT INTO m49_geography (m49_release_id, code, name, geography_kind, is_leaf, iso_alpha2, iso_alpha3)
				VALUES ($1, $2, $3, 'country_or_area', true, $4, $5)`,
				releaseID, area.M49, area.Name, area.ISOAlpha2, area.ISOAlpha3,
			); err != nil {
				return 0, fmt.Errorf("insert M49 geography %s: %w", area.M49, err)
			}
		}
		if err := s.insertGroups(ctx, tx, releaseID); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit M49 seed: %w", err)
	}
	return releaseID, nil
}

type seedGroup struct {
	id      string
	parent  string
	name    string
	kind    string
	custom  bool
	members []string
}

func (s *Service) insertGroups(ctx context.Context, tx pgx.Tx, releaseID int64) error {
	groups, err := normalizeGroups(s.reference.Groups)
	if err != nil {
		return err
	}
	pending := make(map[string]seedGroup, len(groups))
	for _, group := range groups {
		pending[group.id] = group
	}
	inserted := make(map[string]struct{}, len(groups))
	for len(pending) > 0 {
		progress := false
		for id, group := range pending {
			if group.parent != "" {
				if _, ok := inserted[group.parent]; !ok {
					continue
				}
			}
			if _, err := tx.Exec(ctx, `
				INSERT INTO m49_group (m49_release_id, code, parent_code, name, group_kind, is_custom)
				VALUES ($1, $2, NULLIF($3, ''), $4, $5, $6)`,
				releaseID, group.id, group.parent, group.name, group.kind, group.custom,
			); err != nil {
				return fmt.Errorf("insert M49 group %s: %w", group.id, err)
			}
			inserted[id] = struct{}{}
			delete(pending, id)
			progress = true
		}
		if !progress {
			return errors.New("M49 group hierarchy has a missing or cyclic parent")
		}
	}
	for _, group := range groups {
		for _, member := range group.members {
			if _, leaf := s.leaves[member]; !leaf {
				return fmt.Errorf("M49 group %s includes non-leaf member %s", group.id, member)
			}
			if _, err := tx.Exec(ctx, `
				INSERT INTO m49_group_member (m49_release_id, group_code, geography_code)
				VALUES ($1, $2, $3)`, releaseID, group.id, member); err != nil {
				return fmt.Errorf("insert M49 group member %s/%s: %w", group.id, member, err)
			}
		}
	}
	return nil
}

func normalizeGroups(groups []reference.Group) ([]seedGroup, error) {
	result := make([]seedGroup, 0, len(groups))
	for _, group := range groups {
		// Old asset revisions used this synthetic empty parent. It is neither a
		// useful filter nor a valid public API group.
		if group.ID == "" || group.ID == "m49:000" || len(group.MemberM49) == 0 {
			continue
		}
		kind, err := normalizeGroupKind(group.ID, group.Kind)
		if err != nil {
			return nil, err
		}
		parent := ""
		if group.ParentID != nil {
			parent = *group.ParentID
			// Compatibility with the rejected synthetic root in older assets.
			// Its descendants are public top-level groups after the root is dropped.
			if parent == "m49:000" {
				parent = ""
			}
		}
		members := append([]string(nil), group.MemberM49...)
		sort.Strings(members)
		result = append(result, seedGroup{
			id: group.ID, parent: parent, name: group.Name, kind: kind,
			custom: strings.HasPrefix(group.ID, "custom:"), members: members,
		})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].id < result[j].id })
	return result, nil
}

func normalizeGroupKind(id, raw string) (string, error) {
	switch raw {
	case "world", "global":
		return "world", nil
	case "region", "subregion", "intermediate_region", "ldc", "lldc", "sids", "custom":
		return raw, nil
	case "un_designation":
		switch strings.TrimPrefix(strings.ToLower(id), "un:") {
		case "ldc":
			return "ldc", nil
		case "lldc":
			return "lldc", nil
		case "sids":
			return "sids", nil
		}
	}
	return "", fmt.Errorf("M49 group %q has unsupported kind %q", id, raw)
}

func (s *Service) ensureDefinitions(ctx context.Context) error {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return fmt.Errorf("begin dataset seed transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, seedLockKey); err != nil {
		return fmt.Errorf("lock dataset seed: %w", err)
	}
	for _, definition := range who.CuratedDefinitions() {
		definition, encoded, err := marshalDatasetDefinition(definition)
		if err != nil {
			return fmt.Errorf("validate seeded dataset: %w", err)
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO dataset (id, who_indicator_id, who_measure_code, title, source_url, definition)
			VALUES ($1, $2, $3, $4, $5, $6::jsonb)
			ON CONFLICT (id) DO NOTHING`,
			definition.ID, definition.IndicatorID, definition.IndicatorCode, definition.Name, definition.PageURL, encoded,
		); err != nil {
			return fmt.Errorf("seed dataset %s: %w", definition.ID, err)
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO measure (dataset_id, code, title, description)
			VALUES ($1, $2, $3, '')
			ON CONFLICT DO NOTHING`, definition.ID, definition.IndicatorCode, definition.Name); err != nil {
			return fmt.Errorf("seed measure %s: %w", definition.ID, err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit dataset seed: %w", err)
	}
	return nil
}

func (s *Service) resolveM49(raw, _ string) (string, bool) {
	code, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || code < 1 || code > 999 {
		return "", false
	}
	canonical := fmt.Sprintf("%03d", code)
	_, ok := s.leaves[canonical]
	return canonical, ok
}

func definitionForID(id string) (who.DatasetDefinition, bool) {
	return who.CuratedDefinition(id)
}

func marshalDatasetDefinition(definition who.DatasetDefinition) (who.DatasetDefinition, []byte, error) {
	definition.ID = strings.TrimSpace(definition.ID)
	definition.Name = strings.TrimSpace(definition.Name)
	definition.IndicatorID = strings.ToUpper(strings.TrimSpace(definition.IndicatorID))
	definition.IndicatorCode = strings.TrimSpace(definition.IndicatorCode)
	definition.ValueColumn = strings.TrimSpace(definition.ValueColumn)
	definition.ValueKind = apiValueKind(strings.TrimSpace(definition.ValueKind))
	if definition.ID == "" || definition.Name == "" || definition.IndicatorID == "" || definition.IndicatorCode == "" || definition.ValueColumn == "" || definition.ValueKind == "" {
		return who.DatasetDefinition{}, nil, errors.New("dataset definition is incomplete")
	}
	switch definition.ValueKind {
	case "number", "category", "composition":
	default:
		return who.DatasetDefinition{}, nil, fmt.Errorf("dataset definition has unsupported value kind %q", definition.ValueKind)
	}
	page, err := who.ValidateIndicatorPageURL(definition.PageURL)
	if err != nil {
		return who.DatasetDefinition{}, nil, fmt.Errorf("validate dataset definition page: %w", err)
	}
	if page.IndicatorID != definition.IndicatorID {
		return who.DatasetDefinition{}, nil, fmt.Errorf("dataset definition page indicator %s does not match %s", page.IndicatorID, definition.IndicatorID)
	}
	definition.PageURL = page.URL
	encoded, err := json.Marshal(definition)
	if err != nil {
		return who.DatasetDefinition{}, nil, fmt.Errorf("encode dataset definition: %w", err)
	}
	return definition, encoded, nil
}

func apiValueKind(value string) string {
	if value == "numeric" {
		return "number"
	}
	return value
}
