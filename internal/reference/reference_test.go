package reference

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func assetDir(t *testing.T) string {
	t.Helper()
	return filepath.Join("..", "..", "assets", "reference")
}

func TestReferenceSnapshotIsValid(t *testing.T) {
	document, err := ValidateDir(assetDir(t))
	if err != nil {
		t.Fatal(err)
	}
	if got := len(document.Areas); got != 248 {
		t.Fatalf("M49 area count = %d, want 248", got)
	}
}

func TestGroupsUseAPITypesAndSkipBlankAntarcticaHierarchy(t *testing.T) {
	document, err := ValidateDir(assetDir(t))
	if err != nil {
		t.Fatal(err)
	}
	allowed := map[string]bool{"world": true, "region": true, "subregion": true, "intermediate_region": true, "ldc": true, "lldc": true, "sids": true, "custom": true}
	for _, group := range document.Groups {
		if !allowed[group.Kind] {
			t.Fatalf("group %q has non-API kind %q", group.ID, group.Kind)
		}
		if group.ID == "m49:000" || group.Name == "" {
			t.Fatalf("invalid group %#v", group)
		}
	}
	for _, area := range document.Areas {
		if area.M49 == "010" && area.RegionM49 != "000" {
			t.Fatalf("Antarctica region = %q, want source blank hierarchy", area.RegionM49)
		}
	}
}

func TestAreaNamesDoNotContainHTMLCharacterReferences(t *testing.T) {
	document, err := ValidateDir(assetDir(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, area := range document.Areas {
		if strings.Contains(area.Name, "&#") {
			t.Fatalf("M49 area %q has encoded name %q", area.M49, area.Name)
		}
	}
}

func TestExSovietGroupHasExactlyTheSpecifiedMembers(t *testing.T) {
	document, err := ValidateDir(assetDir(t))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"031", "051", "112", "233", "268", "398", "417", "428", "440", "498", "643", "762", "795", "804", "860"}
	for _, group := range document.Groups {
		if group.ID == "custom:ex_soviet" {
			if !slices.Equal(group.MemberM49, want) {
				t.Fatalf("ex-Soviet members = %v, want %v", group.MemberM49, want)
			}
			return
		}
	}
	t.Fatal("custom:ex_soviet group is missing")
}

func TestGeometryJoinsUseUniqueKnownM49Codes(t *testing.T) {
	document, err := ValidateDir(assetDir(t))
	if err != nil {
		t.Fatal(err)
	}
	known := make(map[string]struct{}, len(document.Areas))
	for _, area := range document.Areas {
		known[area.M49] = struct{}{}
	}
	data, err := os.ReadFile(filepath.Join(assetDir(t), "natural-earth-admin0-50m.geojson"))
	if err != nil {
		t.Fatal(err)
	}
	var geometry struct {
		Features []struct {
			Properties struct {
				M49 *string `json:"m49"`
			} `json:"properties"`
		} `json:"features"`
	}
	if err := json.Unmarshal(data, &geometry); err != nil {
		t.Fatal(err)
	}
	seen := map[string]struct{}{}
	for _, feature := range geometry.Features {
		if feature.Properties.M49 == nil {
			continue // Unmatched areas are retained and recorded in the M49 asset.
		}
		m49 := *feature.Properties.M49
		if _, exists := known[m49]; !exists {
			t.Fatalf("geometry has unknown M49 %q", m49)
		}
		if _, duplicate := seen[m49]; duplicate {
			t.Fatalf("geometry has duplicate M49 %q", m49)
		}
		seen[m49] = struct{}{}
	}
	if len(seen) == 0 {
		t.Fatal("geometry did not join any M49 areas")
	}
}

func TestNaturalEarthArchivePin(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(assetDir(t), "checksums.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Sources []struct {
			File, Version, SHA256 string
		} `json:"sources"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	for _, source := range manifest.Sources {
		if source.File == "ne_50m_admin_0_countries.zip" {
			if source.Version != "5.1.1" || source.SHA256 != "5fed433373581fa648920435f937d95f2d3c0200e067409c6478dcdf1b853139" {
				t.Fatalf("Natural Earth pin = version %s sha %s", source.Version, source.SHA256)
			}
			return
		}
	}
	t.Fatal("Natural Earth archive pin is missing")
}
