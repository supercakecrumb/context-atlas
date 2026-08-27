package atlas

import (
	"context"
	"math"
	"strconv"
	"strings"
	"testing"

	"github.com/supercakecrumb/context-atlas/internal/api"
)

func TestIntegrationExactYearsDeduplicateGroupsAndCalculatePearson(t *testing.T) {
	service, ctx := integrationAtlas(t)
	if err := service.Seed(ctx); err != nil {
		t.Fatal(err)
	}
	fixture := seedAssociationFixture(t, service, ctx)

	exact, err := service.Observations(ctx, api.ObservationQuery{
		Snapshot: fixture.snapshot, Series: []string{fixture.xSeries}, Years: []int{2020}, Page: 1, PageSize: 25,
	})
	if err != nil {
		t.Fatal(err)
	}
	if exact.Pagination.Total != 4 || len(exact.Observations) != 4 {
		t.Fatalf("exact-year observations = %+v, want four 2020 rows", exact.Pagination)
	}
	wantX := map[string]float64{"004": 1, "008": 2, "012": 3, "020": 4}
	for _, observation := range exact.Observations {
		if observation.Year != 2020 || observation.NumericValue == nil || *observation.NumericValue != wantX[observation.SourceGeography.M49] {
			t.Fatalf("exact-year observation = %+v, want 2020 fixture value", observation)
		}
	}

	missingYear, err := service.Observations(ctx, api.ObservationQuery{
		Snapshot: fixture.snapshot, Series: []string{fixture.xSeries}, Years: []int{2019}, Page: 1, PageSize: 25,
	})
	if err != nil {
		t.Fatal(err)
	}
	if missingYear.Pagination.Total != 0 {
		t.Fatalf("2019 query returned %d rows; years must never be substituted", missingYear.Pagination.Total)
	}

	grouped, err := service.Observations(ctx, api.ObservationQuery{
		Snapshot: fixture.snapshot, Series: []string{fixture.xSeries}, Years: []int{2020},
		Groups: []string{fixture.groupA, fixture.groupB}, Page: 1, PageSize: 25,
	})
	if err != nil {
		t.Fatal(err)
	}
	if grouped.Pagination.Total != 4 || len(grouped.Observations) != 4 {
		t.Fatalf("overlapping groups returned %d rows, want four unique countries", grouped.Pagination.Total)
	}
	unique := map[string]bool{}
	for _, observation := range grouped.Observations {
		unique[observation.SourceGeography.M49] = true
	}
	if len(unique) != 4 {
		t.Fatalf("overlapping groups returned duplicate countries: %#v", grouped.Observations)
	}

	association, err := service.Association(ctx, api.AssociationQuery{
		Snapshot: fixture.snapshot, XSeries: fixture.xSeries, XYear: 2020, YSeries: fixture.ySeries, YYear: 2021,
		Groups: []string{fixture.groupA, fixture.groupB},
	})
	if err != nil {
		t.Fatal(err)
	}
	if association.Coverage != (api.AssociationCoverage{SelectedUniverse: 4, YOnlyMissing: 1, Paired: 3}) {
		t.Fatalf("association coverage = %+v, want four de-duplicated countries and three pairs", association.Coverage)
	}
	if association.PearsonR == nil || math.Abs(*association.PearsonR-1) > 1e-12 {
		t.Fatalf("Pearson r = %v, want 1 from independently selected 2020/2021 values", association.PearsonR)
	}
	if association.XYear != 2020 || association.YYear != 2021 || len(association.Points) != 3 {
		t.Fatalf("association exact-year result = %+v", association)
	}
}

type associationFixture struct {
	snapshot string
	xSeries  string
	ySeries  string
	groupA   string
	groupB   string
}

func seedAssociationFixture(t *testing.T, service *Service, ctx context.Context) associationFixture {
	t.Helper()
	definition, ok := definitionForID("suicide-mortality")
	if !ok {
		t.Fatal("missing curated suicide definition")
	}
	const (
		datasetID = "suicide-mortality"
		snapshot  = "integration-association"
		groupA    = "test:overlap-a"
		groupB    = "test:overlap-b"
	)
	var m49ReleaseID int64
	if err := service.pool.QueryRow(ctx, `SELECT id FROM m49_reference_release ORDER BY id LIMIT 1`).Scan(&m49ReleaseID); err != nil {
		t.Fatal(err)
	}

	const rowCount = 15
	var releaseID int64
	err := service.pool.QueryRow(ctx, `
		INSERT INTO dataset_release (
			dataset_id, source_url, citation, accessed_at, raw_csv, sha256, response_metadata, csv_headers,
			schema_fingerprint, parser_version, source_row_count, imported_row_count,
			duplicate_row_count, rejected_row_count, diagnostics
		) VALUES ($1, $2, 'fixture', now(), $3, $4, '{}'::jsonb, '["fixture"]'::jsonb, $5, 'fixture-v1', $6, $6, 0, 0, '{}'::jsonb)
		RETURNING id`,
		datasetID, definition.PageURL, []byte("fixture"), strings.Repeat("a", 64), strings.Repeat("b", 64), rowCount,
	).Scan(&releaseID)
	if err != nil {
		t.Fatal(err)
	}
	insertSeries := func(dimensions, hash string) int64 {
		t.Helper()
		var id int64
		err := service.pool.QueryRow(ctx, `
			INSERT INTO series (dataset_id, measure_code, label, dimensions, dimensions_hash, unit, statistic, value_kind)
			VALUES ($1, $2, 'Fixture series', $3::jsonb, $4, 'per 100k', 'estimate', 'number')
			RETURNING id`, datasetID, definition.IndicatorCode, dimensions, hash).Scan(&id)
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	xSeriesID := insertSeries(`{"DIM_SEX":"TOTAL"}`, strings.Repeat("c", 64))
	ySeriesID := insertSeries(`{"DIM_SEX":"FEMALE"}`, strings.Repeat("d", 64))

	codes := []string{"004", "008", "012", "020"}
	geographyIDs := make(map[string]int64, len(codes))
	for _, code := range codes {
		var id int64
		if err := service.pool.QueryRow(ctx, `
			INSERT INTO source_geography (source_system, source_code, name, geography_kind, canonical_m49_code)
			VALUES ('WHO', $1, $2, 'COUNTRY', $1)
			RETURNING id`, code, "Fixture "+code).Scan(&id); err != nil {
			t.Fatal(err)
		}
		geographyIDs[code] = id
	}
	for _, group := range []string{groupA, groupB} {
		if _, err := service.pool.Exec(ctx, `
			INSERT INTO m49_group (m49_release_id, code, name, group_kind, is_custom)
			VALUES ($1, $2, $2, 'custom', true)`, m49ReleaseID, group); err != nil {
			t.Fatal(err)
		}
	}
	for _, member := range []struct{ group, code string }{
		{groupA, "004"}, {groupA, "008"}, {groupA, "020"},
		{groupB, "008"}, {groupB, "012"}, {groupB, "020"},
	} {
		if _, err := service.pool.Exec(ctx, `
			INSERT INTO m49_group_member (m49_release_id, group_code, geography_code) VALUES ($1, $2, $3)`,
			m49ReleaseID, member.group, member.code); err != nil {
			t.Fatal(err)
		}
	}

	insertObservation := func(seriesID int64, year int, code string, value float64, state string) {
		t.Helper()
		valueText := strconv.FormatFloat(value, 'f', -1, 64)
		if _, err := service.pool.Exec(ctx, `
			INSERT INTO observation (
				dataset_id, release_id, series_id, source_geography_id, year, raw_value, display_value,
				numeric_value, value_status, publish_state, source_row_key, canonical_m49_code
			) VALUES ($1, $2, $3, $4, $5, $6, $6, $7, 'numeric', $8, $9, $10)`,
			datasetID, releaseID, seriesID, geographyIDs[code], year, valueText, value, state,
			"fixture-"+strconv.FormatInt(seriesID, 10)+"-"+strconv.Itoa(year)+"-"+code, code,
		); err != nil {
			t.Fatal(err)
		}
	}
	for index, code := range codes {
		insertObservation(xSeriesID, 2020, code, float64(index+1), "PUBLISHED")
		insertObservation(xSeriesID, 2021, code, float64(index+101), "PUBLISHED")
	}
	for index, code := range codes[:3] {
		insertObservation(ySeriesID, 2020, code, float64(8-index), "PUBLISHED")
		insertObservation(ySeriesID, 2021, code, float64((index+1)*2), "PUBLISHED")
	}
	insertObservation(ySeriesID, 2021, "020", 8, "DRAFT")

	if _, err := service.pool.Exec(ctx, `INSERT INTO catalog_snapshot (id, m49_release_id) VALUES ($1, $2)`, snapshot, m49ReleaseID); err != nil {
		t.Fatal(err)
	}
	if _, err := service.pool.Exec(ctx, `
		INSERT INTO catalog_snapshot_release (snapshot_id, dataset_id, release_id) VALUES ($1, $2, $3)`, snapshot, datasetID, releaseID); err != nil {
		t.Fatal(err)
	}
	if _, err := service.pool.Exec(ctx, `INSERT INTO catalog_head (singleton, snapshot_id) VALUES (true, $1)`, snapshot); err != nil {
		t.Fatal(err)
	}
	return associationFixture{
		snapshot: snapshot, xSeries: strconv.FormatInt(xSeriesID, 10), ySeries: strconv.FormatInt(ySeriesID, 10), groupA: groupA, groupB: groupB,
	}
}
