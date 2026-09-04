package worldbank

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/supercakecrumb/context-atlas/internal/who"
)

type Metadata struct {
	Producer    string `json:"producer"`
	SourceNote  string `json:"source_note"`
	LastUpdated string `json:"last_updated"`
	LicenseURL  string `json:"license_url"`
}

type namedCode struct {
	ID    string `json:"id"`
	Value string `json:"value"`
}
type indicatorMetadata struct {
	ID                 string    `json:"id"`
	Name               string    `json:"name"`
	Source             namedCode `json:"source"`
	SourceNote         string    `json:"sourceNote"`
	SourceOrganization string    `json:"sourceOrganization"`
}
type countryMetadata struct {
	ID     string    `json:"id"`
	ISO2   string    `json:"iso2Code"`
	Name   string    `json:"name"`
	Region namedCode `json:"region"`
}
type sourceRow struct {
	Indicator namedCode       `json:"indicator"`
	Country   namedCode       `json:"country"`
	ISO3      string          `json:"countryiso3code"`
	Date      string          `json:"date"`
	Value     json.RawMessage `json:"value"`
	Unit      string          `json:"unit"`
	Status    string          `json:"obs_status"`
	Footnote  string          `json:"footnote"`
}

// unpack reparses and accounts for every stored response page, including when
// confirmation runs after a restart. No archive paths are written to disk.
func unpack(raw []byte) (map[string][]json.RawMessage, map[string]pageMeta, error) {
	if len(raw) > maxBytes {
		return nil, nil, fmt.Errorf("world bank artifact exceeds 50 MiB")
	}
	archive, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return nil, nil, err
	}
	if len(archive.File) > 3*maxPages {
		return nil, nil, fmt.Errorf("too many World Bank response files")
	}
	files := map[string]*zip.File{}
	var size uint64
	for _, file := range archive.File {
		size += file.UncompressedSize64
		if size > maxBytes || file.UncompressedSize64 > maxBytes || files[file.Name] != nil {
			return nil, nil, fmt.Errorf("oversized or duplicate World Bank response file")
		}
		files[file.Name] = file
	}
	groups := map[string][]json.RawMessage{}
	metadata := map[string]pageMeta{}
	for _, group := range []string{"indicator", "countries", "observations"} {
		pages := 1
		for page := 1; page <= pages; page++ {
			name := fmt.Sprintf("%s-%03d.json", group, page)
			file := files[name]
			if file == nil {
				return nil, nil, fmt.Errorf("missing World Bank response %s", name)
			}
			reader, err := file.Open()
			if err != nil {
				return nil, nil, err
			}
			body, readErr := io.ReadAll(io.LimitReader(reader, maxBytes+1))
			_ = reader.Close()
			if readErr != nil || len(body) > maxBytes {
				return nil, nil, fmt.Errorf("invalid World Bank response file %s", name)
			}
			meta, rows, err := decodePage(body)
			if err != nil {
				return nil, nil, err
			}
			if page == 1 {
				metadata[group] = meta
				pages = meta.Pages
			}
			first := metadata[group]
			if meta.Page != page || meta.Pages != first.Pages || meta.Total != first.Total || meta.SourceID != first.SourceID || meta.LastUpdated != first.LastUpdated {
				return nil, nil, fmt.Errorf("inconsistent World Bank pages")
			}
			groups[group] = append(groups[group], rows...)
			delete(files, name)
		}
		if len(groups[group]) != metadata[group].Total {
			return nil, nil, fmt.Errorf("incomplete World Bank %s responses", group)
		}
	}
	if len(files) != 0 {
		return nil, nil, fmt.Errorf("unexpected files in World Bank artifact")
	}
	return groups, metadata, nil
}

func BuildPreview(raw []byte, definition who.DatasetDefinition, accessedAt time.Time, iso3ToM49 map[string]string) (who.Preview, Metadata, error) {
	preview := who.Preview{Dataset: &definition, SourceURL: definition.PageURL, AccessedAt: accessedAt.UTC(), SHA256: fmt.Sprintf("%x", sha256.Sum256(raw)), Bytes: int64(len(raw))}
	var metadata Metadata
	if err := ValidateDefinition(definition); err != nil {
		return preview, metadata, err
	}
	groups, pageMetadata, err := unpack(raw)
	if err != nil {
		return preview, metadata, err
	}
	var indicator indicatorMetadata
	if len(groups["indicator"]) != 1 {
		return preview, metadata, fmt.Errorf("expected one WDI indicator")
	}
	if err := json.Unmarshal(groups["indicator"][0], &indicator); err != nil {
		return preview, metadata, err
	}
	if indicator.ID != definition.IndicatorCode || indicator.Source.ID != "2" || indicator.SourceOrganization == "" || indicator.SourceNote == "" || pageMetadata["observations"].SourceID != "2" {
		return preview, metadata, fmt.Errorf("world bank indicator/source metadata changed or is incomplete")
	}
	if definition.IndicatorCode == "NY.GDP.PCAP.PP.KD" && !strings.Contains(indicator.Name, "constant 2021 international") {
		return preview, metadata, fmt.Errorf("GDP purchasing-power base year changed; review the definition")
	}
	metadata = Metadata{Producer: indicator.SourceOrganization, SourceNote: indicator.SourceNote, LastUpdated: pageMetadata["observations"].LastUpdated, LicenseURL: LicenseURL}
	if _, err := time.Parse("2006-01-02", metadata.LastUpdated); err != nil {
		return preview, metadata, fmt.Errorf("missing or invalid World Bank release update date")
	}
	countries := map[string]countryMetadata{}
	for _, rawCountry := range groups["countries"] {
		var country countryMetadata
		if err := json.Unmarshal(rawCountry, &country); err != nil {
			return preview, metadata, err
		}
		if country.ID == "" || country.ISO2 == "" || country.Name == "" || country.Region.ID == "" || countries[country.ISO2].ID != "" {
			return preview, metadata, fmt.Errorf("incomplete or duplicate World Bank country metadata")
		}
		countries[country.ISO2] = country
	}
	identity := who.SeriesIdentity{Dataset: definition.ID, IndicatorCode: definition.IndicatorCode, Measure: definition.Name, Unit: definition.Unit, Statistic: definition.Statistic, ValueColumn: "value", ValueKind: "number", Dimensions: map[string]string{"DIM_SEX": "TOTAL"}}
	hash, err := identity.Hash()
	if err != nil {
		return preview, metadata, err
	}
	canonical, err := identity.CanonicalJSON()
	if err != nil {
		return preview, metadata, err
	}
	dimensions, err := who.CanonicalDimensions(identity.Dimensions)
	if err != nil {
		return preview, metadata, err
	}
	fields := []string{"indicator", "country", "countryiso3code", "date", "value", "unit", "obs_status", "decimal", "footnote"}
	preview.Schema = who.Schema{Headers: fields, ValueColumn: "value", DimensionColumns: definition.Dimensions, Fingerprint: fmt.Sprintf("%x", sha256.Sum256([]byte(strings.Join(fields, "\n"))))}
	seen := map[string]string{}
	for index, rawRow := range groups["observations"] {
		preview.Accounting.RowsRead++
		var row sourceRow
		if err := json.Unmarshal(rawRow, &row); err != nil {
			return preview, metadata, fmt.Errorf("decode World Bank row %d: %w", index+1, err)
		}
		country, known := countries[row.Country.ID]
		year, yearErr := strconv.Atoi(row.Date)
		if !known || row.Country.Value == "" || (row.ISO3 != "" && row.ISO3 != country.ID) || row.Indicator.ID != definition.IndicatorCode || yearErr != nil || year < 1 || year > 9999 || len(row.Date) != 4 || (row.Unit != "" && row.Unit != definition.Unit) {
			return preview, metadata, fmt.Errorf("invalid identity, year or unit in World Bank row %d", index+1)
		}
		key := strings.Join([]string{"WDI", row.Indicator.ID, country.ID, row.Date}, "|")
		if previous, ok := seen[key]; ok {
			if previous != string(rawRow) {
				return preview, metadata, fmt.Errorf("conflicting World Bank rows for %s", key)
			}
			preview.Accounting.ExactDuplicates++
			continue
		}
		seen[key] = string(rawRow)
		value := who.Value{Raw: string(row.Value), Display: string(row.Value), Status: who.ValueMissing}
		if len(row.Value) == 0 {
			return preview, metadata, fmt.Errorf("missing value field for %s", key)
		}
		if string(row.Value) == "null" {
			value.Display = ""
		} else {
			var number float64
			if err := json.Unmarshal(row.Value, &number); err != nil || math.IsNaN(number) || math.IsInf(number, 0) || number < 0 {
				return preview, metadata, fmt.Errorf("invalid numeric World Bank value for %s", key)
			}
			if (definition.IndicatorCode == "SI.POV.GINI" || strings.Contains(definition.Unit, "percent")) && number > 100 {
				return preview, metadata, fmt.Errorf("world bank percentage/index outside 0–100 for %s", key)
			}
			value.Numeric, value.Status = &number, who.ValueNumeric
		}
		kind, m49 := "country_or_area", ""
		if country.Region.ID == "NA" || country.Region.Value == "Aggregates" {
			kind = "aggregate"
		} else {
			m49 = iso3ToM49[country.ID]
		}
		if kind != "aggregate" && m49 == "" {
			preview.Diagnostics.Warnings++
			if len(preview.Diagnostics.Entries) < 50 {
				preview.Diagnostics.Entries = append(preview.Diagnostics.Entries, who.Diagnostic{Severity: who.DiagnosticWarning, Code: "unmapped_geography", Row: index + 1, Message: "Unmapped World Bank country or area: " + country.ID})
			} else {
				preview.Diagnostics.Truncated = true
			}
		}
		preview.Observations = append(preview.Observations, who.Observation{Row: index + 1, SeriesHash: hash, SourceRowKey: key, Year: year, SourceGeo: who.Geography{Code: country.ID, Name: row.Country.Value, Type: kind, PublishState: "PUBLISHED"}, CanonicalM49: m49, Value: value, SourceNote: row.Footnote, SourceStatus: row.Status})
		preview.Accounting.UniqueObservations++
	}
	preview.Series = []who.Series{{Hash: hash, CanonicalJSON: canonical, DimensionsJSON: dimensions, Identity: identity, Observations: preview.Accounting.UniqueObservations}}
	return preview, metadata, nil
}
