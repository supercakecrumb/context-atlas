// Package worldbank imports a reviewed set of World Development Indicators.
package worldbank

import (
	"fmt"
	"reflect"

	"github.com/supercakecrumb/context-atlas/internal/who"
)

const Source = "World Bank"
const ParserVersion = "wdi-json-v1"
const LicenseURL = "https://creativecommons.org/licenses/by/4.0/"

// ponytail: reuse the existing preview model in who; move shared types when a
// third parser needs them, without inventing a provider framework here.
func Definitions() []who.DatasetDefinition {
	items := []struct{ id, code, name, unit, statistic, description string }{
		{"population", "SP.POP.TOTL", "Population", "people", "midyear population", "De facto midyear population. Estimates and underlying sources follow the World Bank release metadata."},
		{"urban-population", "SP.URB.TOTL.IN.ZS", "Urban population", "percent of population", "share", "Urban definitions vary by country; the UN Population Division collects and smooths national data."},
		{"gdp-per-capita-ppp", "NY.GDP.PCAP.PP.KD", "GDP per capita, PPP", "constant 2021 international dollars", "per capita", "Economic output adjusted for purchasing power, in constant 2021 international dollars. This is not household or take-home income."},
		{"gini", "SI.POV.GINI", "Gini index", "index (0–100)", "index", "Household-survey inequality; income and consumption measures differ. Read the country-year source note before comparing. Survey years are irregular."},
		{"unemployment", "SL.UEM.TOTL.ZS", "Unemployment (modelled ILO estimate)", "percent of total labour force", "modelled ILO estimate", "Total unemployment as a share of the labour force, modelled by the ILO. This is not a count of all people without jobs."},
	}
	result := make([]who.DatasetDefinition, 0, len(items))
	for _, item := range items {
		result = append(result, who.DatasetDefinition{
			ID: "wdi-" + item.id, Name: item.name, SourceSystem: Source, Description: item.description,
			PageURL: "https://data.worldbank.org/indicator/" + item.code, IndicatorID: item.code, IndicatorCode: item.code,
			ValueColumn: "value", Unit: item.unit, Statistic: item.statistic, ValueKind: "number",
			Dimensions: []string{"DIM_SEX"}, Capabilities: []string{"line", "map", "association"},
		})
	}
	return result
}

func DefinitionForURL(raw string) (who.DatasetDefinition, bool) {
	for _, definition := range Definitions() {
		if raw == definition.PageURL {
			return definition, true
		}
	}
	return who.DatasetDefinition{}, false
}

func ValidateDefinition(definition who.DatasetDefinition) error {
	known, ok := DefinitionForURL(definition.PageURL)
	if !ok || !reflect.DeepEqual(known, definition) {
		return fmt.Errorf("unsupported or altered World Bank indicator definition")
	}
	return nil
}
