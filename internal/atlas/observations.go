package atlas

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/supercakecrumb/context-atlas/internal/api"
)

// observationFilter is deliberately shared by JSON and CSV reads so those
// exports cannot accidentally apply different geography or group semantics.
type observationFilter struct {
	series      []int64
	years       []int16
	geographies []string
	groups      []string
	page        int
	pageSize    int
}

func normalizeObservationFilter(query api.ObservationQuery) (observationFilter, error) {
	series, err := parseObservationSeriesIDs(query.Series)
	if err != nil {
		return observationFilter{}, err
	}

	years := make([]int16, 0, len(query.Years))
	seenYears := make(map[int]struct{}, len(query.Years))
	for _, year := range query.Years {
		if year < 1 || year > 9999 {
			return observationFilter{}, errors.New("years must be exact calendar years from 1 through 9999")
		}
		if _, exists := seenYears[year]; !exists {
			seenYears[year] = struct{}{}
			years = append(years, int16(year))
		}
	}
	sort.Slice(years, func(i, j int) bool { return years[i] < years[j] })

	page, pageSize := query.Page, query.PageSize
	if page == 0 {
		page = 1
	}
	if pageSize == 0 {
		pageSize = 25
	}
	if page < 1 || (pageSize != 25 && pageSize != 50 && pageSize != 100 && pageSize != 500) {
		return observationFilter{}, errors.New("page must be positive and page_size must be 25, 50, 100, or 500")
	}

	return observationFilter{
		series:      series,
		years:       years,
		geographies: sortedUnique(query.Geographies),
		groups:      sortedUnique(query.Groups),
		page:        page,
		pageSize:    pageSize,
	}, nil
}

func parseObservationSeriesIDs(raw []string) ([]int64, error) {
	seen := make(map[int64]struct{}, len(raw))
	ids := make([]int64, 0, len(raw))
	for _, value := range raw {
		id, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
		if err != nil || id < 1 {
			return nil, fmt.Errorf("%w: series %q does not exist", api.ErrNotFound, value)
		}
		if _, exists := seen[id]; !exists {
			seen[id] = struct{}{}
			ids = append(ids, id)
		}
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids, nil
}

const observationFilterCTE = `
WITH selected_group_members AS (
	SELECT DISTINCT geography_code
	FROM m49_group_member
	WHERE m49_release_id = $2 AND group_code = ANY($6::text[])
)
`

const observationFilterFrom = `
FROM catalog_snapshot_release pinned
JOIN observation ON observation.release_id = pinned.release_id
JOIN source_geography source ON source.id = observation.source_geography_id
LEFT JOIN m49_geography reference
  ON reference.m49_release_id = $2
 AND reference.code = COALESCE(observation.canonical_m49_code, source.canonical_m49_code)
WHERE pinned.snapshot_id = $1
  AND (COALESCE(cardinality($3::bigint[]), 0) = 0 OR observation.series_id = ANY($3::bigint[]))
  AND (COALESCE(cardinality($4::smallint[]), 0) = 0 OR observation.year = ANY($4::smallint[]))
  AND (COALESCE(cardinality($5::text[]), 0) = 0
       OR source.source_code = ANY($5::text[])
       OR COALESCE(observation.canonical_m49_code, source.canonical_m49_code) = ANY($5::text[]))
  AND (COALESCE(cardinality($6::text[]), 0) = 0
       OR COALESCE(observation.canonical_m49_code, source.canonical_m49_code)
          IN (SELECT geography_code FROM selected_group_members))
`

const observationSelectSQL = observationFilterCTE + `
SELECT observation.series_id, observation.release_id, source.source_code, source.name, source.geography_kind,
       COALESCE(observation.canonical_m49_code, source.canonical_m49_code),
       reference.code IS NOT NULL, COALESCE(reference.is_leaf, false), reference.iso_alpha2, reference.iso_alpha3,
       observation.year, observation.raw_value, observation.display_value,
       observation.numeric_value, observation.lower_bound, observation.upper_bound,
       observation.value_status, observation.publish_state, observation.source_row_key
` + observationFilterFrom + `
ORDER BY observation.year, source.name, source.source_code, observation.series_id, observation.id
LIMIT NULLIF($7::bigint, 0) OFFSET $8::bigint
`

const observationCountSQL = observationFilterCTE + `SELECT count(*) ` + observationFilterFrom

func observationBaseArgs(snapshot resolvedSnapshot, filter observationFilter) []any {
	return []any{
		snapshot.meta.Snapshot.ID,
		snapshot.m49ReleaseID,
		filter.series,
		filter.years,
		filter.geographies,
		filter.groups,
	}
}

func observationSelectArgs(snapshot resolvedSnapshot, filter observationFilter, limit, offset int64) []any {
	args := observationBaseArgs(snapshot, filter)
	return append(args, limit, offset)
}

func (s *Service) ensureObservationSeriesInSnapshot(ctx context.Context, snapshot resolvedSnapshot, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	unique := make(map[int64]struct{}, len(ids))
	for _, id := range ids {
		unique[id] = struct{}{}
	}
	var count int
	err := s.pool.QueryRow(ctx, `
		SELECT count(DISTINCT series.id)
		FROM series
		JOIN catalog_snapshot_release pinned ON pinned.dataset_id = series.dataset_id
		WHERE pinned.snapshot_id = $1 AND series.id = ANY($2::bigint[])
	`, snapshot.meta.Snapshot.ID, ids).Scan(&count)
	if err != nil {
		return fmt.Errorf("%w: validate observation series: %v", api.ErrUnavailable, err)
	}
	if count != len(unique) {
		return api.ErrNotFound
	}
	return nil
}

// Observations returns every selected source row, including non-numeric and
// unpublished states. Group and geography filters are an intersection; group
// membership is de-duplicated in PostgreSQL before it reaches the response.
func (s *Service) Observations(ctx context.Context, query api.ObservationQuery) (api.ObservationResult, error) {
	snapshot, err := s.resolveSnapshot(ctx, query.Snapshot)
	if err != nil {
		return api.ObservationResult{}, err
	}
	filter, err := normalizeObservationFilter(query)
	if err != nil {
		return api.ObservationResult{}, err
	}
	if err := s.ensureObservationSeriesInSnapshot(ctx, snapshot, filter.series); err != nil {
		return api.ObservationResult{}, err
	}

	var total int64
	if err := s.pool.QueryRow(ctx, observationCountSQL, observationBaseArgs(snapshot, filter)...).Scan(&total); err != nil {
		return api.ObservationResult{}, fmt.Errorf("%w: count observations: %v", api.ErrUnavailable, err)
	}
	offset := int64(filter.page-1) * int64(filter.pageSize)
	rows, err := s.pool.Query(ctx, observationSelectSQL, observationSelectArgs(snapshot, filter, int64(filter.pageSize), offset)...)
	if err != nil {
		return api.ObservationResult{}, fmt.Errorf("%w: list observations: %v", api.ErrUnavailable, err)
	}
	defer rows.Close()

	observations := make([]api.Observation, 0, min(filter.pageSize, int(total)))
	for rows.Next() {
		observation, err := scanObservation(rows)
		if err != nil {
			return api.ObservationResult{}, fmt.Errorf("%w: scan observation: %v", api.ErrUnavailable, err)
		}
		observations = append(observations, observation)
	}
	if err := rows.Err(); err != nil {
		return api.ObservationResult{}, fmt.Errorf("%w: read observations: %v", api.ErrUnavailable, err)
	}
	return api.ObservationResult{
		Meta:         snapshot.meta,
		Pagination:   api.Pagination{Page: filter.page, PageSize: filter.pageSize, Total: total},
		Observations: observations,
	}, nil
}

type observationRowScanner interface{ Scan(...any) error }

func scanObservation(row observationRowScanner) (api.Observation, error) {
	var (
		seriesID, releaseID                  int64
		sourceCode, name, kind               string
		m49, iso2, iso3                      pgtype.Text
		mapped, leaf                         bool
		year                                 int16
		rawValue, displayValue               string
		numericValue, lowerBound, upperBound pgtype.Numeric
		valueStatus, publishState, rowKey    string
	)
	if err := row.Scan(
		&seriesID, &releaseID, &sourceCode, &name, &kind, &m49, &mapped, &leaf, &iso2, &iso3,
		&year, &rawValue, &displayValue, &numericValue, &lowerBound, &upperBound,
		&valueStatus, &publishState, &rowKey,
	); err != nil {
		return api.Observation{}, err
	}
	numeric, err := observationNumericPointer(numericValue)
	if err != nil {
		return api.Observation{}, err
	}
	lower, err := observationNumericPointer(lowerBound)
	if err != nil {
		return api.Observation{}, err
	}
	upper, err := observationNumericPointer(upperBound)
	if err != nil {
		return api.Observation{}, err
	}
	return api.Observation{
		SeriesID: strconv.FormatInt(seriesID, 10), ReleaseID: strconv.FormatInt(releaseID, 10),
		SourceGeography: api.Geography{
			SourceCode: sourceCode, Name: name, Kind: kind,
			M49: textValue(m49), ISO2: textValue(iso2), ISO3: textValue(iso3),
			Mapped: mapped, Leaf: leaf,
		},
		Year: int(year), RawValue: rawValue, DisplayValue: displayValue, NumericValue: numeric,
		LowerBound: lower, UpperBound: upper, Status: valueStatus, PublishState: publishState, SourceRowKey: rowKey,
	}, nil
}

func observationNumericPointer(value pgtype.Numeric) (*float64, error) {
	converted, err := value.Float64Value()
	if err != nil {
		return nil, err
	}
	if !converted.Valid {
		return nil, nil
	}
	if math.IsNaN(converted.Float64) || math.IsInf(converted.Float64, 0) {
		return nil, errors.New("database numeric value is not finite")
	}
	result := converted.Float64
	return &result, nil
}

// ObservationsCSV uses exactly the same unpaginated filter as Observations and
// puts reproducibility metadata in comments and columns.
func (s *Service) ObservationsCSV(ctx context.Context, query api.ObservationQuery) (api.CSVExport, error) {
	snapshot, err := s.resolveSnapshot(ctx, query.Snapshot)
	if err != nil {
		return api.CSVExport{}, err
	}
	filter, err := normalizeObservationFilter(query)
	if err != nil {
		return api.CSVExport{}, err
	}
	if err := s.ensureObservationSeriesInSnapshot(ctx, snapshot, filter.series); err != nil {
		return api.CSVExport{}, err
	}
	return api.CSVExport{
		Meta:     snapshot.meta,
		Filename: "context-atlas-observations-" + snapshot.meta.Snapshot.ID + ".csv",
		ETag:     snapshotETag("observations-csv", snapshot.meta.Snapshot.ID, filter),
		Write: func(writeCtx context.Context, writer io.Writer) error {
			return s.writeObservationsCSV(writeCtx, writer, snapshot, filter)
		},
	}, nil
}

func (s *Service) writeObservationsCSV(ctx context.Context, destination io.Writer, snapshot resolvedSnapshot, filter observationFilter) error {
	if err := writeCSVMetadata(destination, snapshot); err != nil {
		return err
	}

	rows, err := s.pool.Query(ctx, observationSelectSQL, observationSelectArgs(snapshot, filter, 0, 0)...)
	if err != nil {
		return fmt.Errorf("query CSV observations: %w", err)
	}
	defer rows.Close()

	releases := make(map[string]api.DatasetReleaseRef, len(snapshot.meta.Releases))
	for _, release := range snapshot.meta.Releases {
		releases[release.ID] = release
	}
	csvWriter := csv.NewWriter(destination)
	if err := csvWriter.Write(observationCSVHeader[:]); err != nil {
		return err
	}
	for rows.Next() {
		observation, err := scanObservation(rows)
		if err != nil {
			return fmt.Errorf("scan CSV observation: %w", err)
		}
		release, ok := releases[observation.ReleaseID]
		if !ok {
			return errors.New("CSV observation release is not pinned by its snapshot")
		}
		if err := csvWriter.Write(observationCSVRecord(snapshot, release, observation)); err != nil {
			return err
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read CSV observations: %w", err)
	}
	csvWriter.Flush()
	return csvWriter.Error()
}

var observationCSVHeader = [...]string{
	"snapshot_id", "snapshot_created_at", "m49_reference_release", "release_id", "release_dataset_id", "release_sha256",
	"release_source_url", "release_accessed_at", "release_citation", "release_parser_version", "series_id",
	"source_code", "source_name", "source_kind", "canonical_m49", "mapped", "leaf", "year", "raw_value",
	"display_value", "numeric_value", "lower_bound", "upper_bound", "value_status", "publish_state", "source_row_key",
}

func observationCSVRecord(snapshot resolvedSnapshot, release api.DatasetReleaseRef, observation api.Observation) []string {
	return []string{
		csvCell(snapshot.meta.Snapshot.ID), csvCell(snapshot.meta.Snapshot.CreatedAt.Format(time.RFC3339)),
		csvCell(snapshot.meta.Snapshot.M49ReferenceRelease), csvCell(release.ID), csvCell(release.DatasetID),
		csvCell(release.SHA256), csvCell(release.SourceURL), csvCell(release.AccessedAt.Format(time.RFC3339)),
		csvCell(release.Citation), csvCell(release.ParserVersion), observation.SeriesID,
		csvCell(observation.SourceGeography.SourceCode), csvCell(observation.SourceGeography.Name),
		csvCell(observation.SourceGeography.Kind), csvCell(observation.SourceGeography.M49),
		strconv.FormatBool(observation.SourceGeography.Mapped), strconv.FormatBool(observation.SourceGeography.Leaf),
		strconv.Itoa(observation.Year), csvCell(observation.RawValue), csvCell(observation.DisplayValue),
		formatObservationFloat(observation.NumericValue), formatObservationFloat(observation.LowerBound),
		formatObservationFloat(observation.UpperBound), csvCell(observation.Status), csvCell(observation.PublishState),
		csvCell(observation.SourceRowKey),
	}
}

func writeCSVMetadata(destination io.Writer, snapshot resolvedSnapshot) error {
	if _, err := fmt.Fprintf(destination, "# Context Atlas snapshot: %s\n# M49 reference release: %s\n", csvComment(snapshot.meta.Snapshot.ID), csvComment(snapshot.meta.Snapshot.M49ReferenceRelease)); err != nil {
		return err
	}
	for _, release := range snapshot.meta.Releases {
		if _, err := fmt.Fprintf(destination, "# Dataset release: %s (%s), accessed %s, source %s, citation %s\n",
			csvComment(release.ID), csvComment(release.DatasetID), csvComment(release.AccessedAt.Format(time.RFC3339)),
			csvComment(release.SourceURL), csvComment(release.Citation)); err != nil {
			return err
		}
	}
	return nil
}

func csvComment(value string) string {
	return csvCell(strings.NewReplacer("\r", " ", "\n", " ").Replace(csvCell(value)))
}

func csvCell(value string) string {
	first, _ := utf8.DecodeRuneInString(value)
	switch first {
	case '=', '+', '-', '@', '\t', '\r':
		return "'" + value
	}
	return value
}

func formatObservationFloat(value *float64) string {
	if value == nil {
		return ""
	}
	return strconv.FormatFloat(*value, 'f', -1, 64)
}

const associationCTE = `
WITH requested_geographies AS (
	SELECT geography.code
	FROM m49_geography geography
	WHERE geography.m49_release_id = $2
	  AND geography.is_leaf
	  AND (
		COALESCE(cardinality($3::text[]), 0) = 0
		OR geography.code = ANY($3::text[])
		OR EXISTS (
			SELECT 1
			FROM catalog_snapshot_release pinned
			JOIN observation ON observation.release_id = pinned.release_id
			JOIN source_geography source ON source.id = observation.source_geography_id
			WHERE pinned.snapshot_id = $1
			  AND COALESCE(observation.canonical_m49_code, source.canonical_m49_code) = geography.code
			  AND source.source_code = ANY($3::text[])
		)
	  )
),
selected_group_members AS (
	SELECT DISTINCT geography_code
	FROM m49_group_member
	WHERE m49_release_id = $2 AND group_code = ANY($4::text[])
),
universe AS (
	SELECT geography.code, geography.name
	FROM m49_geography geography
	JOIN requested_geographies requested ON requested.code = geography.code
	WHERE COALESCE(cardinality($4::text[]), 0) = 0
	   OR geography.code IN (SELECT geography_code FROM selected_group_members)
),
x AS (
	SELECT DISTINCT ON (COALESCE(observation.canonical_m49_code, source.canonical_m49_code))
	       COALESCE(observation.canonical_m49_code, source.canonical_m49_code) AS m49,
	       observation.numeric_value AS value
	FROM catalog_snapshot_release pinned
	JOIN observation ON observation.release_id = pinned.release_id
	JOIN source_geography source ON source.id = observation.source_geography_id
	WHERE pinned.snapshot_id = $1
	  AND observation.series_id = $5
	  AND observation.year = $6
	  AND observation.value_status = 'numeric'
	  AND observation.publish_state = 'PUBLISHED'
	  AND COALESCE(observation.canonical_m49_code, source.canonical_m49_code) IS NOT NULL
	ORDER BY COALESCE(observation.canonical_m49_code, source.canonical_m49_code), observation.source_geography_id
),
y AS (
	SELECT DISTINCT ON (COALESCE(observation.canonical_m49_code, source.canonical_m49_code))
	       COALESCE(observation.canonical_m49_code, source.canonical_m49_code) AS m49,
	       observation.numeric_value AS value
	FROM catalog_snapshot_release pinned
	JOIN observation ON observation.release_id = pinned.release_id
	JOIN source_geography source ON source.id = observation.source_geography_id
	WHERE pinned.snapshot_id = $1
	  AND observation.series_id = $7
	  AND observation.year = $8
	  AND observation.value_status = 'numeric'
	  AND observation.publish_state = 'PUBLISHED'
	  AND COALESCE(observation.canonical_m49_code, source.canonical_m49_code) IS NOT NULL
	ORDER BY COALESCE(observation.canonical_m49_code, source.canonical_m49_code), observation.source_geography_id
)
`

const associationCoverageSQL = associationCTE + `
SELECT count(*)::int,
	   count(*) FILTER (WHERE x.value IS NULL AND y.value IS NOT NULL)::int,
	   count(*) FILTER (WHERE x.value IS NOT NULL AND y.value IS NULL)::int,
	   count(*) FILTER (WHERE x.value IS NULL AND y.value IS NULL)::int,
	   count(*) FILTER (WHERE x.value IS NOT NULL AND y.value IS NOT NULL)::int,
	   corr(x.value::double precision, y.value::double precision)
FROM universe
LEFT JOIN x ON x.m49 = universe.code
LEFT JOIN y ON y.m49 = universe.code
`

const associationPointsSQL = associationCTE + `
SELECT universe.code, universe.name, x.value::double precision, y.value::double precision
FROM universe
JOIN x ON x.m49 = universe.code
JOIN y ON y.m49 = universe.code
ORDER BY universe.name, universe.code
`

func associationArgs(snapshot resolvedSnapshot, query api.AssociationQuery, xSeries, ySeries int64) []any {
	return []any{
		snapshot.meta.Snapshot.ID,
		snapshot.m49ReleaseID,
		sortedUnique(query.Geographies),
		sortedUnique(query.Groups),
		xSeries,
		int16(query.XYear),
		ySeries,
		int16(query.YYear),
	}
}

// Association joins only published numeric values at their independently
// selected exact years. PostgreSQL calculates corr() after the universe has
// been selected, and the API deliberately returns no causal inference.
func (s *Service) Association(ctx context.Context, query api.AssociationQuery) (api.AssociationResult, error) {
	if query.XYear < 1 || query.XYear > 9999 || query.YYear < 1 || query.YYear > 9999 {
		return api.AssociationResult{}, errors.New("association years must be exact calendar years from 1 through 9999")
	}
	x, err := parseObservationSeriesIDs([]string{query.XSeries})
	if err != nil {
		return api.AssociationResult{}, err
	}
	y, err := parseObservationSeriesIDs([]string{query.YSeries})
	if err != nil {
		return api.AssociationResult{}, err
	}
	snapshot, err := s.resolveSnapshot(ctx, query.Snapshot)
	if err != nil {
		return api.AssociationResult{}, err
	}
	if err := s.ensureObservationSeriesInSnapshot(ctx, snapshot, append(x, y...)); err != nil {
		return api.AssociationResult{}, err
	}

	args := associationArgs(snapshot, query, x[0], y[0])
	var coverage api.AssociationCoverage
	var pearson *float64
	if err := s.pool.QueryRow(ctx, associationCoverageSQL, args...).Scan(
		&coverage.SelectedUniverse,
		&coverage.XOnlyMissing,
		&coverage.YOnlyMissing,
		&coverage.BothMissing,
		&coverage.Paired,
		&pearson,
	); err != nil {
		return api.AssociationResult{}, fmt.Errorf("%w: calculate association: %v", api.ErrUnavailable, err)
	}

	pointRows, err := s.pool.Query(ctx, associationPointsSQL, args...)
	if err != nil {
		return api.AssociationResult{}, fmt.Errorf("%w: list association points: %v", api.ErrUnavailable, err)
	}
	defer pointRows.Close()
	points := make([]api.AssociationPoint, 0, coverage.Paired)
	for pointRows.Next() {
		var point api.AssociationPoint
		if err := pointRows.Scan(&point.M49, &point.Geography, &point.X, &point.Y); err != nil {
			return api.AssociationResult{}, fmt.Errorf("%w: scan association point: %v", api.ErrUnavailable, err)
		}
		points = append(points, point)
	}
	if err := pointRows.Err(); err != nil {
		return api.AssociationResult{}, fmt.Errorf("%w: read association points: %v", api.ErrUnavailable, err)
	}

	coefficient, warnings := associationCoefficient(coverage.Paired, pearson)
	return api.AssociationResult{
		Meta: snapshot.meta, XSeriesID: query.XSeries, XYear: query.XYear, YSeriesID: query.YSeries, YYear: query.YYear,
		Coverage: coverage, PearsonR: coefficient, Points: points, Warnings: warnings,
	}, nil
}

func associationCoefficient(paired int, coefficient *float64) (*float64, []string) {
	warnings := make([]string, 0, 2)
	if paired < 30 {
		warnings = append(warnings, "Interpret this association cautiously: fewer than 30 paired observations are available.")
	}
	if paired < 3 {
		return nil, append(warnings, "Pearson r is not reported because fewer than 3 paired observations are available.")
	}
	if coefficient == nil || math.IsNaN(*coefficient) || math.IsInf(*coefficient, 0) {
		return nil, append(warnings, "Pearson r is not reported because one selected series has zero variance.")
	}
	value := *coefficient
	return &value, warnings
}

var _ api.ObservationReader = (*Service)(nil)
var _ api.AssociationReader = (*Service)(nil)
