package atlas

import (
	"bytes"
	"encoding/csv"
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/supercakecrumb/context-atlas/internal/api"
)

func TestNormalizeObservationFilterKeepsExactSelections(t *testing.T) {
	filter, err := normalizeObservationFilter(api.ObservationQuery{
		Series:      []string{"2", "1", "2"},
		Years:       []int{2021, 2019, 2021},
		Geographies: []string{" 643 ", "031", "643"},
		Groups:      []string{"un:asia", "custom:ex-soviet", "un:asia"},
		Page:        2,
		PageSize:    50,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := filter.series; len(got) != 2 || got[0] != 1 || got[1] != 2 {
		t.Fatalf("series = %v, want [1 2]", got)
	}
	if got, want := len(filter.years), 2; got != want || filter.years[0] != 2019 || filter.years[1] != 2021 {
		t.Fatalf("years = %v, want [2019 2021]", filter.years)
	}
	if got, want := strings.Join(filter.geographies, ","), "031,643"; got != want {
		t.Fatalf("geographies = %q, want %q", got, want)
	}
	if got, want := strings.Join(filter.groups, ","), "custom:ex-soviet,un:asia"; got != want {
		t.Fatalf("groups = %q, want %q", got, want)
	}
	if filter.page != 2 || filter.pageSize != 50 {
		t.Fatalf("pagination = %d/%d, want 2/50", filter.page, filter.pageSize)
	}
}

func TestNormalizeObservationFilterRejectsInvalidInput(t *testing.T) {
	for name, query := range map[string]api.ObservationQuery{
		"non-numeric series": {Series: []string{"x"}},
		"zero year":          {Years: []int{0}},
		"bad page size":      {PageSize: 10},
		"zero page":          {Page: -1},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := normalizeObservationFilter(query); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestObservationArgsMatchSQLPlaceholders(t *testing.T) {
	snapshot := resolvedSnapshot{meta: api.ResponseMeta{Snapshot: api.SnapshotRef{ID: "snapshot-1"}}, m49ReleaseID: 7}
	filter := observationFilter{series: []int64{}, years: []int16{}, geographies: []string{}, groups: []string{}}
	if got := len(observationBaseArgs(snapshot, filter)); got != 6 {
		t.Fatalf("count args = %d, want 6", got)
	}
	if got := len(observationSelectArgs(snapshot, filter, 25, 0)); got != 8 {
		t.Fatalf("select args = %d, want 8", got)
	}
	if strings.Contains(observationCountSQL, "$7") || strings.Contains(observationCountSQL, "$8") {
		t.Fatal("count query must not require pagination parameters")
	}
}

func TestAssociationCoefficientRules(t *testing.T) {
	value := 0.5
	if coefficient, warnings := associationCoefficient(2, &value); coefficient != nil || len(warnings) != 2 {
		t.Fatalf("n=2 = coefficient %v, warnings %v", coefficient, warnings)
	}
	if coefficient, warnings := associationCoefficient(3, nil); coefficient != nil || len(warnings) != 2 {
		t.Fatalf("zero variance = coefficient %v, warnings %v", coefficient, warnings)
	}
	if coefficient, warnings := associationCoefficient(30, &value); coefficient == nil || *coefficient != value || len(warnings) != 0 {
		t.Fatalf("valid coefficient = %v, warnings %v", coefficient, warnings)
	}
}

func TestObservationNumericPointer(t *testing.T) {
	missing, err := observationNumericPointer(pgtype.Numeric{})
	if err != nil || missing != nil {
		t.Fatalf("missing numeric = %v, %v", missing, err)
	}
	value, err := observationNumericPointer(pgtype.Numeric{Int: big.NewInt(1234), Exp: -2, Valid: true})
	if err != nil || value == nil || *value != 12.34 {
		t.Fatalf("numeric = %v, %v", value, err)
	}
}

func TestWriteCSVMetadataUsesPhysicalLines(t *testing.T) {
	var output bytes.Buffer
	snapshot := resolvedSnapshot{meta: api.ResponseMeta{
		Snapshot: api.SnapshotRef{ID: "snapshot-1", M49ReferenceRelease: "UN M49 2026"},
		Releases: []api.DatasetReleaseRef{{
			ID: "42", DatasetID: "suicide", SourceURL: "https://data.who.int/example",
			AccessedAt: time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC), Citation: "WHO\r\nterms\napply",
		}},
	}}
	if err := writeCSVMetadata(&output, snapshot); err != nil {
		t.Fatal(err)
	}
	if _, err := output.WriteString("snapshot_id\n"); err != nil {
		t.Fatal(err)
	}

	lines := strings.Split(output.String(), "\n")
	if got, want := lines[2], "# Dataset release: 42 (suicide), accessed 2026-08-27T12:00:00Z, source https://data.who.int/example, citation WHO  terms apply"; got != want {
		t.Fatalf("citation metadata = %q, want %q", got, want)
	}
	if got := lines[3]; got != "snapshot_id" {
		t.Fatalf("first CSV record = %q, want its own physical line", got)
	}
}

func TestCSVFormulaCellsAreNeutralized(t *testing.T) {
	negative := -1.25
	snapshot := resolvedSnapshot{meta: api.ResponseMeta{
		Snapshot: api.SnapshotRef{ID: "snapshot-1", M49ReferenceRelease: "UN M49 2026"},
		Releases: []api.DatasetReleaseRef{{
			ID: "42", DatasetID: "suicide", SourceURL: "https://data.who.int/example",
			AccessedAt: time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC), Citation: "=HYPERLINK(\"https://evil.example\")",
		}},
	}}
	release := snapshot.meta.Releases[0]
	row := observationCSVRecord(snapshot, release, api.Observation{
		SeriesID: "3", ReleaseID: release.ID, Year: 2020, RawValue: "=1+1", DisplayValue: "+formula",
		NumericValue: &negative, Status: "numeric", PublishState: "PUBLISHED", SourceRowKey: "\trow",
		SourceGeography: api.Geography{SourceCode: "-source", Name: "@geography", Kind: "COUNTRY", M49: "643"},
	})

	var output bytes.Buffer
	writer := csv.NewWriter(&output)
	if err := writer.Write(observationCSVHeader[:]); err != nil {
		t.Fatal(err)
	}
	if err := writer.Write(row); err != nil {
		t.Fatal(err)
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		t.Fatal(err)
	}
	records, err := csv.NewReader(strings.NewReader(output.String())).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	record := records[1]
	for column, want := range map[string]string{
		"source_code":      "'-source",
		"source_name":      "'@geography",
		"raw_value":        "'=1+1",
		"display_value":    "'+formula",
		"release_citation": "'=HYPERLINK(\"https://evil.example\")",
		"source_row_key":   "'\trow",
		"numeric_value":    "-1.25",
	} {
		if got := record[csvColumn(t, column)]; got != want {
			t.Errorf("%s = %q, want %q", column, got, want)
		}
	}

	var metadata bytes.Buffer
	if err := writeCSVMetadata(&metadata, snapshot); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(metadata.String(), "citation '=HYPERLINK(\"https://evil.example\")") {
		t.Fatalf("formula citation was not neutralized in metadata: %q", metadata.String())
	}
}

func TestCSVCellNeutralizesFormulaPrefixes(t *testing.T) {
	for input, want := range map[string]string{
		"=formula":  "'=formula",
		"+formula":  "'+formula",
		"-formula":  "'-formula",
		"@formula":  "'@formula",
		"\tformula": "'\tformula",
		"\rformula": "'\rformula",
		"12.5":      "12.5",
	} {
		if got := csvCell(input); got != want {
			t.Errorf("csvCell(%q) = %q, want %q", input, got, want)
		}
	}
}

func csvColumn(t *testing.T, name string) int {
	t.Helper()
	for index, column := range observationCSVHeader {
		if column == name {
			return index
		}
	}
	t.Fatalf("missing CSV column %q", name)
	return -1
}
