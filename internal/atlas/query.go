package atlas

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/supercakecrumb/context-atlas/internal/api"
)

// resolvedSnapshot is shared with observations.go. It always carries a
// complete release list, so every read/export has reproducible provenance.
type resolvedSnapshot struct {
	meta         api.ResponseMeta
	m49ReleaseID int64
}

func (s *Service) resolveSnapshot(ctx context.Context, requested string) (resolvedSnapshot, error) {
	if s == nil || s.pool == nil {
		return resolvedSnapshot{}, api.ErrUnavailable
	}

	var (
		id           string
		m49ReleaseID int64
		createdAt    time.Time
		m49Version   string
		err          error
	)
	if requested == "" {
		err = s.pool.QueryRow(ctx, `
			SELECT snapshot.id, snapshot.m49_release_id, snapshot.created_at, m49.classification_version
			FROM catalog_head head
			JOIN catalog_snapshot snapshot ON snapshot.id = head.snapshot_id
			JOIN m49_reference_release m49 ON m49.id = snapshot.m49_release_id
			WHERE head.singleton
		`).Scan(&id, &m49ReleaseID, &createdAt, &m49Version)
	} else {
		err = s.pool.QueryRow(ctx, `
			SELECT snapshot.id, snapshot.m49_release_id, snapshot.created_at, m49.classification_version
			FROM catalog_snapshot snapshot
			JOIN m49_reference_release m49 ON m49.id = snapshot.m49_release_id
			WHERE snapshot.id = $1
		`, requested).Scan(&id, &m49ReleaseID, &createdAt, &m49Version)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return resolvedSnapshot{}, api.ErrNotFound
	}
	if err != nil {
		return resolvedSnapshot{}, fmt.Errorf("%w: resolve catalog snapshot: %v", api.ErrUnavailable, err)
	}

	rows, err := s.pool.Query(ctx, `
		SELECT release.id, release.dataset_id, release.sha256, release.source_url, release.accessed_at,
		       release.citation, release.parser_version
		FROM catalog_snapshot_release pinned
		JOIN dataset_release release ON release.id = pinned.release_id
		WHERE pinned.snapshot_id = $1
		ORDER BY pinned.dataset_id
	`, id)
	if err != nil {
		return resolvedSnapshot{}, fmt.Errorf("%w: list snapshot releases: %v", api.ErrUnavailable, err)
	}
	defer rows.Close()

	releases := make([]api.DatasetReleaseRef, 0)
	for rows.Next() {
		var releaseID int64
		var datasetID, sha, sourceURL, citation, parserVersion string
		var accessedAt time.Time
		if err := rows.Scan(&releaseID, &datasetID, &sha, &sourceURL, &accessedAt, &citation, &parserVersion); err != nil {
			return resolvedSnapshot{}, fmt.Errorf("%w: scan snapshot release: %v", api.ErrUnavailable, err)
		}
		releases = append(releases, api.DatasetReleaseRef{
			ID: strconv.FormatInt(releaseID, 10), DatasetID: datasetID, SHA256: sha,
			SourceURL: sourceURL, AccessedAt: accessedAt.UTC(), Citation: citation, ParserVersion: parserVersion,
		})
	}
	if err := rows.Err(); err != nil {
		return resolvedSnapshot{}, fmt.Errorf("%w: read snapshot releases: %v", api.ErrUnavailable, err)
	}

	return resolvedSnapshot{
		m49ReleaseID: m49ReleaseID,
		meta: api.ResponseMeta{
			Snapshot: api.SnapshotRef{ID: id, CreatedAt: createdAt.UTC(), M49ReferenceRelease: m49Version},
			Releases: releases,
		},
	}, nil
}

// Catalog returns all datasets, measures, complete dimension tuples, and exact
// available years at one immutable snapshot.
func (s *Service) Catalog(ctx context.Context, requested string) (api.CatalogResult, error) {
	snapshot, err := s.resolveSnapshot(ctx, requested)
	if err != nil {
		return api.CatalogResult{}, err
	}
	releases := make(map[string]api.DatasetReleaseRef, len(snapshot.meta.Releases))
	for _, release := range snapshot.meta.Releases {
		releases[release.ID] = release
	}

	datasetRows, err := s.pool.Query(ctx, `
		SELECT dataset.id, dataset.title, dataset.who_indicator_id, dataset.who_measure_code,
		       dataset.source_url, pinned.release_id
		FROM catalog_snapshot_release pinned
		JOIN dataset ON dataset.id = pinned.dataset_id
		WHERE pinned.snapshot_id = $1
		ORDER BY dataset.title, dataset.id
	`, snapshot.meta.Snapshot.ID)
	if err != nil {
		return api.CatalogResult{}, fmt.Errorf("%w: list catalog datasets: %v", api.ErrUnavailable, err)
	}
	defer datasetRows.Close()

	datasets := make([]api.Dataset, 0, len(snapshot.meta.Releases))
	for datasetRows.Next() {
		var id, name, indicatorID, code, sourceURL string
		var releaseID int64
		if err := datasetRows.Scan(&id, &name, &indicatorID, &code, &sourceURL, &releaseID); err != nil {
			return api.CatalogResult{}, fmt.Errorf("%w: scan catalog dataset: %v", api.ErrUnavailable, err)
		}
		release, ok := releases[strconv.FormatInt(releaseID, 10)]
		if !ok {
			return api.CatalogResult{}, fmt.Errorf("%w: snapshot has an incomplete release reference", api.ErrUnavailable)
		}
		capabilities := []string{"line", "map", "association", "table"}
		if definition, ok := definitionForID(id); ok {
			capabilities = append([]string{"table"}, definition.Capabilities...)
		}
		datasets = append(datasets, api.Dataset{
			ID: id, Name: name, WHOIdentifier: indicatorID, WHOCode: code, SourceURL: sourceURL,
			Citation: release.Citation, Capabilities: sortedUnique(capabilities), Release: release,
		})
	}
	if err := datasetRows.Err(); err != nil {
		return api.CatalogResult{}, fmt.Errorf("%w: read catalog datasets: %v", api.ErrUnavailable, err)
	}

	type storedMeasure struct{ datasetID, code, name, description string }
	measureRows, err := s.pool.Query(ctx, `
		SELECT measure.dataset_id, measure.code, measure.title, measure.description
		FROM catalog_snapshot_release pinned
		JOIN measure ON measure.dataset_id = pinned.dataset_id
		WHERE pinned.snapshot_id = $1
		ORDER BY measure.dataset_id, measure.code
	`, snapshot.meta.Snapshot.ID)
	if err != nil {
		return api.CatalogResult{}, fmt.Errorf("%w: list catalog measures: %v", api.ErrUnavailable, err)
	}
	defer measureRows.Close()
	storedMeasures := make([]storedMeasure, 0)
	for measureRows.Next() {
		var measure storedMeasure
		if err := measureRows.Scan(&measure.datasetID, &measure.code, &measure.name, &measure.description); err != nil {
			return api.CatalogResult{}, fmt.Errorf("%w: scan catalog measure: %v", api.ErrUnavailable, err)
		}
		storedMeasures = append(storedMeasures, measure)
	}
	if err := measureRows.Err(); err != nil {
		return api.CatalogResult{}, fmt.Errorf("%w: read catalog measures: %v", api.ErrUnavailable, err)
	}

	seriesRows, err := s.pool.Query(ctx, `
		SELECT series.id, series.dataset_id, series.measure_code, series.label, series.dimensions,
		       series.unit, series.statistic, series.value_kind,
		       COALESCE(
			   array_agg(DISTINCT observation.year ORDER BY observation.year)
			       FILTER (WHERE observation.publish_state = 'PUBLISHED' AND observation.value_status = 'numeric'),
			   '{}'::smallint[]
		       )
		FROM catalog_snapshot_release pinned
		JOIN observation ON observation.release_id = pinned.release_id
		JOIN series ON series.id = observation.series_id
		WHERE pinned.snapshot_id = $1
		GROUP BY series.id, series.dataset_id, series.measure_code, series.label, series.dimensions,
		         series.unit, series.statistic, series.value_kind
		ORDER BY series.dataset_id, series.measure_code, series.label, series.id
	`, snapshot.meta.Snapshot.ID)
	if err != nil {
		return api.CatalogResult{}, fmt.Errorf("%w: list catalog series: %v", api.ErrUnavailable, err)
	}
	defer seriesRows.Close()

	type measureTraits struct{ unit, statistic, valueKind string }
	traits := map[string]measureTraits{}
	dimensionValues := map[string]map[string]struct{}{}
	series := make([]api.Series, 0)
	for seriesRows.Next() {
		var id int64
		var datasetID, measureCode, label, unit, statistic, valueKind string
		var dimensionsJSON []byte
		var years []int16
		if err := seriesRows.Scan(&id, &datasetID, &measureCode, &label, &dimensionsJSON, &unit, &statistic, &valueKind, &years); err != nil {
			return api.CatalogResult{}, fmt.Errorf("%w: scan catalog series: %v", api.ErrUnavailable, err)
		}
		dimensions, err := decodeDimensions(dimensionsJSON)
		if err != nil {
			return api.CatalogResult{}, fmt.Errorf("%w: decode catalog series dimensions: %v", api.ErrUnavailable, err)
		}
		for code, value := range dimensions {
			if dimensionValues[code] == nil {
				dimensionValues[code] = map[string]struct{}{}
			}
			dimensionValues[code][value] = struct{}{}
		}
		availableYears := make([]int, len(years))
		for i, year := range years {
			availableYears[i] = int(year)
		}
		measureID := catalogMeasureID(datasetID, measureCode)
		if _, exists := traits[measureID]; !exists {
			traits[measureID] = measureTraits{unit: unit, statistic: statistic, valueKind: apiValueKind(valueKind)}
		}
		series = append(series, api.Series{
			ID: strconv.FormatInt(id, 10), DatasetID: datasetID, MeasureID: measureID, Name: seriesName(label, dimensions),
			Unit: unit, Statistic: statistic, ValueKind: apiValueKind(valueKind), Dimensions: dimensions, AvailableYears: availableYears,
		})
	}
	if err := seriesRows.Err(); err != nil {
		return api.CatalogResult{}, fmt.Errorf("%w: read catalog series: %v", api.ErrUnavailable, err)
	}

	measures := make([]api.Measure, 0, len(storedMeasures))
	for _, measure := range storedMeasures {
		measureID := catalogMeasureID(measure.datasetID, measure.code)
		trait := traits[measureID]
		if trait.valueKind == "" {
			if definition, ok := definitionForID(measure.datasetID); ok {
				trait = measureTraits{unit: definition.Unit, statistic: definition.Statistic, valueKind: apiValueKind(definition.ValueKind)}
			} else {
				trait.valueKind = "number"
			}
		}
		measures = append(measures, api.Measure{
			ID: measureID, DatasetID: measure.datasetID, Name: measure.name, Description: measure.description,
			Unit: trait.unit, Statistic: trait.statistic, ValueKind: trait.valueKind,
		})
	}

	dimensions := make([]api.DimensionDefinition, 0, len(dimensionValues))
	for code, values := range dimensionValues {
		valueList := make([]string, 0, len(values))
		for value := range values {
			valueList = append(valueList, value)
		}
		sort.Strings(valueList)
		dimensions = append(dimensions, api.DimensionDefinition{Code: code, Name: dimensionName(code), Values: valueList})
	}
	sort.Slice(dimensions, func(i, j int) bool { return dimensions[i].Code < dimensions[j].Code })
	return api.CatalogResult{Meta: snapshot.meta, Datasets: datasets, Measures: measures, Series: series, Dimensions: dimensions}, nil
}

// Geographies returns de-duplicated source identities and the complete
// canonical country/area list pinned by the snapshot.
func (s *Service) Geographies(ctx context.Context, query api.GeographyQuery) (api.GeographyResult, error) {
	snapshot, err := s.resolveSnapshot(ctx, query.Snapshot)
	if err != nil {
		return api.GeographyResult{}, err
	}
	rows, err := s.pool.Query(ctx, `
		SELECT DISTINCT source.source_code, source.name, source.geography_kind,
		       COALESCE(observation.canonical_m49_code, source.canonical_m49_code),
		       m49.code IS NOT NULL, COALESCE(m49.is_leaf, false), m49.iso_alpha2, m49.iso_alpha3
		FROM catalog_snapshot_release pinned
		JOIN observation ON observation.release_id = pinned.release_id
		JOIN source_geography source ON source.id = observation.source_geography_id
		LEFT JOIN m49_geography m49
		  ON m49.m49_release_id = $2
		 AND m49.code = COALESCE(observation.canonical_m49_code, source.canonical_m49_code)
		WHERE pinned.snapshot_id = $1
		ORDER BY source.name, source.source_code
	`, snapshot.meta.Snapshot.ID, snapshot.m49ReleaseID)
	if err != nil {
		return api.GeographyResult{}, fmt.Errorf("%w: list source geographies: %v", api.ErrUnavailable, err)
	}
	defer rows.Close()

	geographies := map[string]api.Geography{}
	for rows.Next() {
		var sourceCode, name, kind string
		var m49, iso2, iso3 pgtype.Text
		var mapped, leaf bool
		if err := rows.Scan(&sourceCode, &name, &kind, &m49, &mapped, &leaf, &iso2, &iso3); err != nil {
			return api.GeographyResult{}, fmt.Errorf("%w: scan source geography: %v", api.ErrUnavailable, err)
		}
		geography := api.Geography{
			SourceCode: sourceCode, Name: name, Kind: kind, M49: textValue(m49), ISO2: textValue(iso2), ISO3: textValue(iso3),
			Mapped: mapped, Leaf: leaf,
		}
		geographies[geographyKey(geography)] = geography
	}
	if err := rows.Err(); err != nil {
		return api.GeographyResult{}, fmt.Errorf("%w: read source geographies: %v", api.ErrUnavailable, err)
	}

	canonicalRows, err := s.pool.Query(ctx, `
		SELECT code, name, geography_kind, is_leaf, iso_alpha2, iso_alpha3
		FROM m49_geography
		WHERE m49_release_id = $1
		ORDER BY name, code
	`, snapshot.m49ReleaseID)
	if err != nil {
		return api.GeographyResult{}, fmt.Errorf("%w: list M49 geographies: %v", api.ErrUnavailable, err)
	}
	defer canonicalRows.Close()
	for canonicalRows.Next() {
		var code, name, kind string
		var iso2, iso3 pgtype.Text
		var leaf bool
		if err := canonicalRows.Scan(&code, &name, &kind, &leaf, &iso2, &iso3); err != nil {
			return api.GeographyResult{}, fmt.Errorf("%w: scan M49 geography: %v", api.ErrUnavailable, err)
		}
		geography := api.Geography{
			SourceCode: code, Name: name, Kind: kind, M49: code, ISO2: textValue(iso2), ISO3: textValue(iso3), Mapped: true, Leaf: leaf,
		}
		geographies[geographyKey(geography)] = geography
	}
	if err := canonicalRows.Err(); err != nil {
		return api.GeographyResult{}, fmt.Errorf("%w: read M49 geographies: %v", api.ErrUnavailable, err)
	}

	search := strings.ToLower(strings.TrimSpace(query.Search))
	result := make([]api.Geography, 0, len(geographies))
	for _, geography := range geographies {
		if search != "" && !strings.Contains(strings.ToLower(geography.Name), search) &&
			!strings.Contains(strings.ToLower(geography.SourceCode), search) && !strings.Contains(strings.ToLower(geography.M49), search) {
			continue
		}
		result = append(result, geography)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Name != result[j].Name {
			return result[i].Name < result[j].Name
		}
		if result[i].SourceCode != result[j].SourceCode {
			return result[i].SourceCode < result[j].SourceCode
		}
		return result[i].M49 < result[j].M49
	})
	return api.GeographyResult{Meta: snapshot.meta, Geographies: result}, nil
}

// Groups returns the versioned UN M49/custom hierarchy pinned by the snapshot.
func (s *Service) Groups(ctx context.Context, requested string) (api.GroupResult, error) {
	snapshot, err := s.resolveSnapshot(ctx, requested)
	if err != nil {
		return api.GroupResult{}, err
	}
	rows, err := s.pool.Query(ctx, `
		SELECT group_row.code, group_row.parent_code, group_row.name, group_row.group_kind,
		       member.geography_code, geography.name
		FROM m49_group group_row
		LEFT JOIN m49_group_member member
		  ON member.m49_release_id = group_row.m49_release_id AND member.group_code = group_row.code
		LEFT JOIN m49_geography geography
		  ON geography.m49_release_id = member.m49_release_id AND geography.code = member.geography_code
		WHERE group_row.m49_release_id = $1
		ORDER BY group_row.group_kind, group_row.name, group_row.code, member.geography_code
	`, snapshot.m49ReleaseID)
	if err != nil {
		return api.GroupResult{}, fmt.Errorf("%w: list M49 groups: %v", api.ErrUnavailable, err)
	}
	defer rows.Close()

	byID := map[string]*api.Group{}
	ordered := make([]string, 0)
	for rows.Next() {
		var id, name, kind string
		var parent, memberCode, memberName pgtype.Text
		if err := rows.Scan(&id, &parent, &name, &kind, &memberCode, &memberName); err != nil {
			return api.GroupResult{}, fmt.Errorf("%w: scan M49 group: %v", api.ErrUnavailable, err)
		}
		group := byID[id]
		if group == nil {
			group = &api.Group{
				ID: id, Name: name, Kind: kind, ParentID: textValue(parent),
				ClassificationVersion: snapshot.meta.Snapshot.M49ReferenceRelease, Members: []api.GroupMembership{},
			}
			byID[id] = group
			ordered = append(ordered, id)
		}
		if memberCode.Valid {
			group.Members = append(group.Members, api.GroupMembership{M49: memberCode.String, Name: textValue(memberName)})
		}
	}
	if err := rows.Err(); err != nil {
		return api.GroupResult{}, fmt.Errorf("%w: read M49 groups: %v", api.ErrUnavailable, err)
	}
	groups := make([]api.Group, 0, len(ordered))
	for _, id := range ordered {
		groups = append(groups, *byID[id])
	}
	return api.GroupResult{Meta: snapshot.meta, Groups: groups}, nil
}

func (s *Service) Admin0Map(ctx context.Context, requested string) (api.GeoJSONExport, error) {
	snapshot, err := s.resolveSnapshot(ctx, requested)
	if err != nil {
		return api.GeoJSONExport{}, err
	}
	return api.GeoJSONExport{
		Meta: snapshot.meta, ETag: snapshotETag("map", snapshot.meta.Snapshot.ID, s.mapETag), Body: s.mapBody,
	}, nil
}

// Health checks both PostgreSQL and the explicit catalog-head pointer.
func (s *Service) Health(ctx context.Context) (api.HealthReport, error) {
	report := api.HealthReport{Status: "ok", Dependencies: []api.DependencyHealth{
		{Name: "postgres", Required: true},
		{Name: "catalog_snapshot", Required: false},
	}}
	if s == nil || s.pool == nil {
		report.Status = "degraded"
		report.Dependencies[0].Detail = "pool is not initialized"
		report.Dependencies[1].Detail = "pool is not initialized"
		return report, nil
	}
	if err := s.pool.Ping(ctx); err != nil {
		report.Status = "degraded"
		report.Dependencies[0].Detail = "ping failed"
		report.Dependencies[1].Detail = "database is unavailable"
		return report, nil
	}
	report.Dependencies[0].Ready = true
	var exists bool
	if err := s.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM catalog_head WHERE singleton)`).Scan(&exists); err != nil {
		report.Status = "degraded"
		report.Dependencies[1].Detail = "snapshot check failed"
		return report, nil
	}
	report.Dependencies[1].Ready = exists
	if !exists {
		report.Dependencies[1].Detail = "no published catalog snapshot"
	}
	return report, nil
}

func decodeDimensions(raw []byte) (map[string]string, error) {
	dimensions := map[string]string{}
	if len(raw) == 0 {
		return dimensions, nil
	}
	if err := json.Unmarshal(raw, &dimensions); err != nil {
		return nil, err
	}
	return dimensions, nil
}

func catalogMeasureID(datasetID, measureCode string) string { return datasetID + ":" + measureCode }

func seriesName(label string, dimensions map[string]string) string {
	if len(dimensions) == 0 {
		return label
	}
	keys := make([]string, 0, len(dimensions))
	for key := range dimensions {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, dimensionName(key)+": "+dimensions[key])
	}
	return label + " · " + strings.Join(parts, ", ")
}

func geographyKey(geography api.Geography) string {
	return strings.Join([]string{geography.SourceCode, geography.Name, geography.Kind, geography.M49}, "\x00")
}

func textValue(value pgtype.Text) string {
	if value.Valid {
		return value.String
	}
	return ""
}

func sortedUnique(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			if _, exists := seen[value]; !exists {
				seen[value] = struct{}{}
				result = append(result, value)
			}
		}
	}
	sort.Strings(result)
	return result
}

func snapshotETag(kind, snapshotID string, value any) string {
	payload, _ := json.Marshal(struct {
		Kind     string `json:"kind"`
		Snapshot string `json:"snapshot"`
		Value    any    `json:"value"`
	}{Kind: kind, Snapshot: snapshotID, Value: value})
	sum := sha256.Sum256(payload)
	return `"sha256-` + hex.EncodeToString(sum[:]) + `"`
}

var _ api.CatalogReader = (*Service)(nil)
var _ api.GeographyReader = (*Service)(nil)
var _ api.HealthChecker = (*Service)(nil)
