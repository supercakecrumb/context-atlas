// Package who validates and previews WHO DataDot CSV imports.
//
// It deliberately has no database dependency: callers must persist only a
// preview that has passed validation and been explicitly confirmed.
package who

import (
	"fmt"
	"sort"
)

const (
	// WHODataHost is the only accepted host for submitted indicator pages.
	WHODataHost = "data.who.int"
	// WHOBlobHost is the only accepted host for DataDot indicator downloads.
	WHOBlobHost = "srhdpeuwpubsa.blob.core.windows.net"
	// WHOBlobPathPrefix is the fixed public DataDot indicator-export path.
	WHOBlobPathPrefix = "/whdh/DATADOT/INDICATOR/"
)

// DatasetDefinition is an explicit semantic mapping for a curated MVP dataset.
// Generic imports do not need one, but curated imports must match it exactly.
type DatasetDefinition struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	PageURL       string   `json:"page_url"`
	IndicatorID   string   `json:"indicator_id"`
	IndicatorCode string   `json:"indicator_code"`
	ValueColumn   string   `json:"value_column"`
	Unit          string   `json:"unit"`
	Statistic     string   `json:"statistic"`
	ValueKind     string   `json:"value_kind"`
	Dimensions    []string `json:"dimensions"`
	Capabilities  []string `json:"capabilities"`
}

var curatedDefinitions = []DatasetDefinition{
	{
		ID:            "suicide-mortality",
		Name:          "Suicide mortality",
		PageURL:       "https://data.who.int/indicators/i/F08B4FD/16BBF41",
		IndicatorID:   "16BBF41",
		IndicatorCode: "SDGSUICIDE",
		ValueColumn:   "RATE_PER_100000_N",
		Unit:          "deaths per 100 000 population",
		Statistic:     "rate",
		ValueKind:     "number",
		Dimensions:    []string{"DIM_AGE", "DIM_SEX"},
		Capabilities:  []string{"line", "map", "association"},
	},
	{
		ID:            "alcohol-consumption",
		Name:          "Alcohol consumption",
		PageURL:       "https://data.who.int/indicators/i/EF38E6A/EE6F72A",
		IndicatorID:   "EE6F72A",
		IndicatorCode: "SA_0000001688",
		ValueColumn:   "RATE_PER_CAPITA_N",
		Unit:          "litres of pure alcohol per capita (age 15+)",
		Statistic:     "rate per capita",
		ValueKind:     "number",
		Dimensions:    []string{"DIM_SEX"},
		Capabilities:  []string{"line", "map", "association"},
	},
	{
		ID:            "tobacco-prevalence",
		Name:          "Tobacco prevalence",
		PageURL:       "https://data.who.int/indicators/i/847662C/75DDA77",
		IndicatorID:   "75DDA77",
		IndicatorCode: "M_Est_tob_curr_std",
		ValueColumn:   "PERCENT_POP_N",
		Unit:          "percent of population",
		Statistic:     "prevalence",
		ValueKind:     "number",
		Dimensions:    []string{"DIM_SEX"},
		Capabilities:  []string{"line", "map", "association"},
	},
	{
		ID:            "homicide-mortality",
		Name:          "Homicide mortality",
		PageURL:       "https://data.who.int/indicators/i/60A0E76/361734E",
		IndicatorID:   "361734E",
		IndicatorCode: "VIOLENCE_HOMICIDERATE",
		ValueColumn:   "RATE_PER_100000_N",
		Unit:          "deaths per 100 000 population",
		Statistic:     "rate",
		ValueKind:     "number",
		Dimensions:    []string{"DIM_SEX"},
		Capabilities:  []string{"line", "map", "association"},
	},
	{
		ID:            "aware-antibiotic-consumption",
		Name:          "AWaRe antibiotic consumption",
		PageURL:       "https://data.who.int/indicators/i/B4715F3/19E688D",
		IndicatorID:   "19E688D",
		IndicatorCode: "GLASSAMC_AWARE",
		ValueColumn:   "RATE_PER_100_N",
		Unit:          "percent of antibiotic consumption",
		Statistic:     "composition share",
		ValueKind:     "number",
		Dimensions:    []string{"DIM_AMR_GLASS_AWARE"},
		Capabilities:  []string{"composition", "line", "map"},
	},
	{
		ID:            "road-traffic-mortality",
		Name:          "Road traffic mortality",
		PageURL:       "https://data.who.int/indicators/i/B9D9E6A/D6176E2",
		IndicatorID:   "D6176E2",
		IndicatorCode: "RS_198",
		ValueColumn:   "RATE_PER_100000_N",
		Unit:          "deaths per 100 000 population",
		Statistic:     "rate",
		ValueKind:     "number",
		Capabilities:  []string{"line", "map", "association"},
	},
}

// CuratedDefinitions returns a copy so callers cannot mutate the catalogue.
func CuratedDefinitions() []DatasetDefinition {
	definitions := make([]DatasetDefinition, len(curatedDefinitions))
	for i, definition := range curatedDefinitions {
		definitions[i] = cloneDefinition(definition)
	}
	return definitions
}

// CuratedDefinition finds a curated definition by its stable atlas ID.
func CuratedDefinition(id string) (DatasetDefinition, bool) {
	for _, definition := range curatedDefinitions {
		if definition.ID == id {
			return cloneDefinition(definition), true
		}
	}
	return DatasetDefinition{}, false
}

// DefinitionForIndicator returns the curated dataset attached to a validated
// canonical page, if that page is part of the launch catalogue.
func DefinitionForIndicator(page IndicatorPage) (DatasetDefinition, bool) {
	for _, definition := range curatedDefinitions {
		if definition.IndicatorID == page.IndicatorID {
			return cloneDefinition(definition), true
		}
	}
	return DatasetDefinition{}, false
}

// DefinitionForDownload returns the curated dataset attached to a validated
// DataDot download URL, if it is part of the launch catalogue.
func DefinitionForDownload(download DownloadLink) (DatasetDefinition, bool) {
	for _, definition := range curatedDefinitions {
		if definition.IndicatorID == download.IndicatorID {
			return cloneDefinition(definition), true
		}
	}
	return DatasetDefinition{}, false
}

// ValidateSchema prevents a curated import from silently changing its meaning.
func (definition DatasetDefinition) ValidateSchema(schema Schema) error {
	if definition.ValueColumn == "" {
		return nil
	}
	if schema.ValueColumn != definition.ValueColumn {
		return fmt.Errorf("%w: %s expects %s, got %s", ErrCuratedSchema, definition.ID, definition.ValueColumn, schema.ValueColumn)
	}

	want := append([]string(nil), definition.Dimensions...)
	got := append([]string(nil), schema.DimensionColumns...)
	sort.Strings(want)
	sort.Strings(got)
	if len(want) != len(got) {
		return fmt.Errorf("%w: %s dimensions changed", ErrCuratedSchema, definition.ID)
	}
	for i := range want {
		if want[i] != got[i] {
			return fmt.Errorf("%w: %s dimensions changed", ErrCuratedSchema, definition.ID)
		}
	}
	return nil
}

func cloneDefinition(definition DatasetDefinition) DatasetDefinition {
	definition.Dimensions = append([]string(nil), definition.Dimensions...)
	definition.Capabilities = append([]string(nil), definition.Capabilities...)
	return definition
}
