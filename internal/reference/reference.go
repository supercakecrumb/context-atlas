// Package reference validates the versioned geography assets used by Context Atlas.
package reference

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Document struct {
	SchemaVersion  int            `json:"schema_version"`
	Classification Classification `json:"classification"`
	Areas          []Area         `json:"areas"`
	Groups         []Group        `json:"groups"`
	GeometryJoin   GeometryJoin   `json:"geometry_join"`
}

type Area struct {
	M49                    string  `json:"m49"`
	Name                   string  `json:"name"`
	ISOAlpha2              *string `json:"iso_alpha2"`
	ISOAlpha3              *string `json:"iso_alpha3"`
	RegionM49              string  `json:"region_m49"`
	RegionName             string  `json:"region_name"`
	SubregionM49           string  `json:"subregion_m49"`
	SubregionName          string  `json:"subregion_name"`
	IntermediateRegionM49  *string `json:"intermediate_region_m49"`
	IntermediateRegionName *string `json:"intermediate_region_name"`
	LDC                    bool    `json:"ldc"`
	LLDC                   bool    `json:"lldc"`
	SIDS                   bool    `json:"sids"`
}

type Group struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Kind      string   `json:"kind"`
	ParentID  *string  `json:"parent_id"`
	MemberM49 []string `json:"member_m49"`
}

type Classification struct {
	Name         string `json:"name"`
	VersionLabel string `json:"version_label"`
	SourceURL    string `json:"source_url"`
}

type GeometryJoin struct {
	MatchedSourceFeatureCount int                 `json:"matched_source_feature_count"`
	PublishedFeatureCount     int                 `json:"published_feature_count"`
	UnmatchedGeometry         []UnmatchedGeometry `json:"unmatched_geometry"`
	UnmatchedM49              []string            `json:"unmatched_m49"`
}

type UnmatchedGeometry struct {
	Name      string  `json:"name"`
	ISOAlpha3 *string `json:"iso_alpha3"`
}

type checksumFile struct {
	File   string `json:"file"`
	SHA256 string `json:"sha256"`
}

type checksums struct {
	Artifacts []checksumFile `json:"artifacts"`
}

// ValidateDir checks the static reference snapshot without downloading anything.
func ValidateDir(dir string) (Document, error) {
	var document Document
	data, err := os.ReadFile(filepath.Join(dir, "un-m49-current.json"))
	if err != nil {
		return document, err
	}
	if err := json.Unmarshal(data, &document); err != nil {
		return document, fmt.Errorf("decode M49 reference: %w", err)
	}
	seenAreas := make(map[string]struct{}, len(document.Areas))
	for _, area := range document.Areas {
		if area.M49 == "" {
			return document, fmt.Errorf("M49 reference has an empty area code")
		}
		if strings.Contains(area.Name, "&#") {
			return document, fmt.Errorf("M49 area %q has an encoded HTML name", area.M49)
		}
		if _, duplicate := seenAreas[area.M49]; duplicate {
			return document, fmt.Errorf("M49 reference has duplicate area %q", area.M49)
		}
		seenAreas[area.M49] = struct{}{}
	}
	for _, group := range document.Groups {
		if group.ID == "m49:000" || group.Name == "" {
			return document, fmt.Errorf("reference has an invalid group %q", group.ID)
		}
		seenMembers := make(map[string]struct{}, len(group.MemberM49))
		for _, member := range group.MemberM49 {
			if _, exists := seenAreas[member]; !exists {
				return document, fmt.Errorf("group %q contains unknown M49 %q", group.ID, member)
			}
			if _, duplicate := seenMembers[member]; duplicate {
				return document, fmt.Errorf("group %q repeats M49 %q", group.ID, member)
			}
			seenMembers[member] = struct{}{}
		}
	}

	manifestData, err := os.ReadFile(filepath.Join(dir, "checksums.json"))
	if err != nil {
		return document, err
	}
	var manifest checksums
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		return document, fmt.Errorf("decode checksums: %w", err)
	}
	for _, artifact := range manifest.Artifacts {
		contents, err := os.ReadFile(filepath.Join(dir, artifact.File))
		if err != nil {
			return document, err
		}
		sum := sha256.Sum256(contents)
		if hex.EncodeToString(sum[:]) != artifact.SHA256 {
			return document, fmt.Errorf("checksum mismatch for %s", artifact.File)
		}
	}
	return document, nil
}
