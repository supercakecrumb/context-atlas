package atlas

import (
	"reflect"
	"testing"

	"github.com/supercakecrumb/context-atlas/internal/api"
)

func TestSortedUniqueCanonicalizesPublicFilterValues(t *testing.T) {
	got := sortedUnique([]string{" 840", "840", "", " 031 ", "840", "031"})
	if want := []string{"031", "840"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("sortedUnique() = %#v, want %#v", got, want)
	}
}

func TestSnapshotETagDependsOnPinnedSnapshot(t *testing.T) {
	first := snapshotETag("map", "snapshot-a", "map-body")
	if got := snapshotETag("map", "snapshot-a", "map-body"); got != first {
		t.Fatalf("same immutable input produced different ETags: %q and %q", first, got)
	}
	if got := snapshotETag("map", "snapshot-b", "map-body"); got == first {
		t.Fatal("different pinned snapshots produced the same ETag")
	}
}

func TestGeographyKeyKeepsDistinctSourceIdentities(t *testing.T) {
	base := api.Geography{SourceCode: "840", Name: "United States", Kind: "COUNTRY", M49: "840"}
	renamed := base
	renamed.Name = "United States of America"
	if geographyKey(base) == geographyKey(renamed) {
		t.Fatal("a source geography rename was collapsed")
	}
}

func TestDecodeDimensionsRejectsNonStringValues(t *testing.T) {
	if _, err := decodeDimensions([]byte(`{"DIM_SEX":1}`)); err == nil {
		t.Fatal("non-string dimension value was accepted")
	}
}

func TestSeriesNameMakesDimensionTuplesSelectable(t *testing.T) {
	got := seriesName("Suicide deaths", map[string]string{"DIM_SEX": "FEMALE", "DIM_AGE": "TOTAL"})
	if want := "Suicide deaths · Age: TOTAL, Sex: FEMALE"; got != want {
		t.Fatalf("seriesName() = %q, want %q", got, want)
	}
}
