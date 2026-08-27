package who

import (
	"bytes"
	"crypto/sha256"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"
)

// M49Resolver verifies a source M49 code against the reference release chosen
// by the caller. Returning false retains the source row but excludes it from
// canonical geography features.
type M49Resolver func(canonicalM49, sourceName string) (mappedM49 string, ok bool)

// PreviewOptions carries only deterministic parsing inputs. No option permits a
// database write or an implicit year/geography substitution.
type PreviewOptions struct {
	Dataset    *DatasetDefinition
	SourceURL  string
	AccessedAt time.Time
	ResolveM49 M49Resolver
	Limits     Limits
}

// Preview is the complete in-memory result of staging a WHO CSV.
type Preview struct {
	Dataset      *DatasetDefinition
	SourceURL    string
	AccessedAt   time.Time
	SHA256       string
	Bytes        int64
	Schema       Schema
	Accounting   RowAccounting
	Diagnostics  Diagnostics
	Series       []Series
	Observations []Observation
}

// Valid reports whether the preview can be confirmed by a later persistence
// layer. Warnings (such as unmapped source geographies) do not block it.
func (preview Preview) Valid() bool {
	return preview.Diagnostics.Errors == 0
}

// RowAccounting proves that the importer did not silently truncate or sample
// the source file.
type RowAccounting struct {
	RowsRead           int
	UniqueObservations int
	ExactDuplicates    int
	ConflictingRows    int
	InvalidRows        int
}

// DiagnosticSeverity marks a preview finding as a warning or a blocking error.
type DiagnosticSeverity string

const (
	DiagnosticWarning DiagnosticSeverity = "warning"
	DiagnosticError   DiagnosticSeverity = "error"
)

// Diagnostic identifies the logical CSV row and field that caused a preview
// finding. Key is a stable source-row key where one was available.
type Diagnostic struct {
	Severity DiagnosticSeverity
	Code     string
	Row      int
	Field    string
	Key      string
	Message  string
}

// Diagnostics keeps bounded examples while preserving exact total counts.
type Diagnostics struct {
	Entries   []Diagnostic
	Warnings  int
	Errors    int
	Truncated bool
}

func (diagnostics *Diagnostics) add(limits Limits, diagnostic Diagnostic) {
	if diagnostic.Severity == DiagnosticError {
		diagnostics.Errors++
	} else {
		diagnostics.Warnings++
	}
	if len(diagnostics.Entries) >= limits.MaxDiagnostics {
		diagnostics.Truncated = true
		return
	}
	diagnostics.Entries = append(diagnostics.Entries, diagnostic)
}

// SeriesIdentity contains the complete stable tuple which defines one series.
type SeriesIdentity struct {
	Dataset       string
	IndicatorCode string
	Measure       string
	Unit          string
	Statistic     string
	ValueColumn   string
	ValueKind     string
	Dimensions    map[string]string
}

// CanonicalDimensions returns stable JSON for arbitrary DIM_* values.
func CanonicalDimensions(dimensions map[string]string) (string, error) {
	canonical := make(map[string]string, len(dimensions))
	for key, value := range dimensions {
		canonical[strings.TrimSpace(key)] = strings.TrimSpace(value)
	}
	encoded, err := json.Marshal(canonical)
	if err != nil {
		return "", fmt.Errorf("encode canonical dimensions: %w", err)
	}
	return string(encoded), nil
}

// CanonicalJSON returns a deterministic full series tuple. encoding/json sorts
// string map keys, while the struct fixes the order of every other field.
func (identity SeriesIdentity) CanonicalJSON() (string, error) {
	dimensions, err := CanonicalDimensions(identity.Dimensions)
	if err != nil {
		return "", err
	}
	var canonicalDimensions json.RawMessage = []byte(dimensions)
	encoded, err := json.Marshal(struct {
		Dataset       string          `json:"dataset"`
		IndicatorCode string          `json:"indicator_code"`
		Measure       string          `json:"measure"`
		Unit          string          `json:"unit"`
		Statistic     string          `json:"statistic"`
		ValueColumn   string          `json:"value_column"`
		ValueKind     string          `json:"value_kind"`
		Dimensions    json.RawMessage `json:"dimensions"`
	}{
		Dataset:       strings.TrimSpace(identity.Dataset),
		IndicatorCode: strings.TrimSpace(identity.IndicatorCode),
		Measure:       strings.TrimSpace(identity.Measure),
		Unit:          strings.TrimSpace(identity.Unit),
		Statistic:     strings.TrimSpace(identity.Statistic),
		ValueColumn:   strings.TrimSpace(identity.ValueColumn),
		ValueKind:     strings.TrimSpace(identity.ValueKind),
		Dimensions:    canonicalDimensions,
	})
	if err != nil {
		return "", fmt.Errorf("encode canonical series: %w", err)
	}
	return string(encoded), nil
}

// Hash returns the stable SHA-256 identifier for a canonical series tuple.
func (identity SeriesIdentity) Hash() (string, error) {
	canonical, err := identity.CanonicalJSON()
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(canonical))
	return fmt.Sprintf("%x", sum), nil
}

// Series is a unique canonical tuple found in a staged CSV.
type Series struct {
	Hash           string
	CanonicalJSON  string
	DimensionsJSON string
	Identity       SeriesIdentity
	Observations   int
}

// Geography preserves the WHO-provided geography, including aggregate rows.
type Geography struct {
	Code         string
	Name         string
	Type         string
	PublishState string
}

// ValueStatus preserves missing, suppressed, not-applicable, and numeric zero
// as distinct states. The Raw field remains available for source fidelity.
type ValueStatus string

const (
	ValueNumeric       ValueStatus = "numeric"
	ValueMissing       ValueStatus = "missing"
	ValueSuppressed    ValueStatus = "suppressed"
	ValueNotApplicable ValueStatus = "not_applicable"
	ValueInvalid       ValueStatus = "invalid"
)

// Value stores the raw and display source cells alongside an optional numeric
// representation. Numeric zero has a non-nil Numeric pointer.
type Value struct {
	Raw     string
	Display string
	Numeric *float64
	Status  ValueStatus
}

// Observation is one valid source row after normalization. CanonicalM49 is
// empty only when the reference resolver could not map a retained source row.
type Observation struct {
	Row          int
	SeriesHash   string
	SourceRowKey string
	Year         int
	SourceGeo    Geography
	CanonicalM49 string
	Value        Value
	LowerBound   Value
	UpperBound   Value
}

// BuildPreview parses a bounded raw CSV into a fully accounted, immutable-in-
// spirit preview. It never writes data; callers must reject any returned error.
func BuildPreview(raw []byte, options PreviewOptions) (Preview, error) {
	limits := options.Limits.normalized()
	preview := Preview{
		SourceURL:  options.SourceURL,
		AccessedAt: options.AccessedAt.UTC(),
		Bytes:      int64(len(raw)),
	}
	if preview.AccessedAt.IsZero() {
		preview.AccessedAt = time.Now().UTC()
	}
	if int64(len(raw)) > limits.MaxBytes {
		return preview, fmt.Errorf("%w: %d bytes (limit %d)", ErrArtifactTooLarge, len(raw), limits.MaxBytes)
	}
	if options.Dataset != nil {
		definition := cloneDefinition(*options.Dataset)
		preview.Dataset = &definition
	}
	if options.SourceURL != "" {
		link, err := ValidateDownloadURL(options.SourceURL)
		if err != nil {
			return preview, err
		}
		preview.SourceURL = link.URL
		if preview.Dataset != nil && link.IndicatorID != preview.Dataset.IndicatorID {
			return preview, fmt.Errorf("%w: URL indicator %s does not match %s", ErrInvalidDownloadURL, link.IndicatorID, preview.Dataset.IndicatorID)
		}
	}
	sum := sha256.Sum256(raw)
	preview.SHA256 = fmt.Sprintf("%x", sum)

	reader := csv.NewReader(bytes.NewReader(raw))
	reader.FieldsPerRecord = -1
	headers, err := reader.Read()
	if err != nil {
		if errors.Is(err, io.EOF) {
			return preview, fmt.Errorf("%w: CSV has no header", ErrMalformedCSV)
		}
		return preview, fmt.Errorf("%w: read header: %v", ErrMalformedCSV, err)
	}
	schema, err := discoverSchema(headers, limits)
	if err != nil {
		return preview, err
	}
	preview.Schema = schema
	if preview.Dataset != nil {
		if err := preview.Dataset.ValidateSchema(schema); err != nil {
			return preview, err
		}
	}
	reader.FieldsPerRecord = len(schema.Headers)

	seenRows := make(map[string]Observation)
	seriesByHash := make(map[string]*Series)
	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return preview, fmt.Errorf("%w: row %d: %v", ErrMalformedCSV, preview.Accounting.RowsRead+2, err)
		}
		preview.Accounting.RowsRead++
		row := preview.Accounting.RowsRead + 1
		if preview.Accounting.RowsRead > limits.MaxRows {
			preview.Accounting.InvalidRows++
			preview.Diagnostics.add(limits, Diagnostic{
				Severity: DiagnosticError,
				Code:     "row_limit_exceeded",
				Row:      row,
				Message:  fmt.Sprintf("CSV exceeds %d-row limit", limits.MaxRows),
			})
			break
		}

		beforeErrors := preview.Diagnostics.Errors
		observation, identity, dimensions := buildObservation(record, row, schema, preview.Dataset, options.ResolveM49, &preview.Diagnostics, limits)
		if preview.Diagnostics.Errors > beforeErrors {
			preview.Accounting.InvalidRows++
			continue
		}

		canonical, err := identity.CanonicalJSON()
		if err != nil {
			return preview, err
		}
		hash, err := identity.Hash()
		if err != nil {
			return preview, err
		}
		observation.SeriesHash = hash
		observation.SourceRowKey = sourceRowKey(record, schema, observation.Year, dimensions)

		if previous, exists := seenRows[observation.SourceRowKey]; exists {
			if sameObservation(previous, observation) {
				preview.Accounting.ExactDuplicates++
				preview.Diagnostics.add(limits, Diagnostic{
					Severity: DiagnosticWarning,
					Code:     "exact_duplicate_collapsed",
					Row:      row,
					Key:      observation.SourceRowKey,
					Message:  "exact duplicate source row collapsed",
				})
				continue
			}
			preview.Accounting.ConflictingRows++
			preview.Diagnostics.add(limits, Diagnostic{
				Severity: DiagnosticError,
				Code:     "conflicting_duplicate",
				Row:      row,
				Key:      observation.SourceRowKey,
				Message:  "same source-row key has conflicting payloads",
			})
			continue
		}
		seenRows[observation.SourceRowKey] = observation
		preview.Observations = append(preview.Observations, observation)
		preview.Accounting.UniqueObservations++
		if current, exists := seriesByHash[hash]; exists {
			current.Observations++
		} else {
			dimensionsJSON, err := CanonicalDimensions(dimensions)
			if err != nil {
				return preview, err
			}
			seriesByHash[hash] = &Series{
				Hash:           hash,
				CanonicalJSON:  canonical,
				DimensionsJSON: dimensionsJSON,
				Identity:       identity,
				Observations:   1,
			}
		}
	}

	preview.Series = make([]Series, 0, len(seriesByHash))
	for _, series := range seriesByHash {
		preview.Series = append(preview.Series, *series)
	}
	sort.Slice(preview.Series, func(i, j int) bool {
		return preview.Series[i].CanonicalJSON < preview.Series[j].CanonicalJSON
	})
	if !preview.Valid() {
		return preview, fmt.Errorf("%w: %d blocking diagnostics", ErrPreviewInvalid, preview.Diagnostics.Errors)
	}
	return preview, nil
}

func buildObservation(record []string, row int, schema Schema, definition *DatasetDefinition, resolver M49Resolver, diagnostics *Diagnostics, limits Limits) (Observation, SeriesIdentity, map[string]string) {
	dimensions := make(map[string]string, len(schema.DimensionColumns))
	for _, column := range schema.DimensionColumns {
		dimensions[column] = strings.TrimSpace(schema.field(record, column))
	}

	timeType := strings.TrimSpace(schema.field(record, schema.TimeTypeColumn))
	yearText := strings.TrimSpace(schema.field(record, schema.TimeColumn))
	year, err := strconv.Atoi(yearText)
	if timeType != "YEAR" || err != nil || year < 1 || year > 9999 {
		diagnostics.add(limits, Diagnostic{
			Severity: DiagnosticError,
			Code:     "invalid_exact_year",
			Row:      row,
			Field:    schema.TimeColumn,
			Message:  "DIM_TIME must be an integer year when DIM_TIME_TYPE is YEAR",
		})
	}

	geography := Geography{
		Code:         strings.TrimSpace(schema.field(record, schema.GeographyM49Column)),
		Name:         strings.TrimSpace(schema.field(record, schema.GeographyNameColumn)),
		Type:         strings.TrimSpace(schema.field(record, schema.GeographyTypeColumn)),
		PublishState: strings.TrimSpace(schema.field(record, schema.PublishStateColumn)),
	}
	canonicalSourceM49, validM49 := canonicalM49(geography.Code)
	canonicalM49 := ""
	if !validM49 {
		diagnostics.add(limits, Diagnostic{
			Severity: DiagnosticError,
			Code:     "invalid_m49_geography",
			Row:      row,
			Field:    schema.GeographyM49Column,
			Message:  "DIM_GEO_CODE_M49 must be a numeric M49 code from 001 through 999",
		})
	} else if mapped, ok := resolveM49(canonicalSourceM49, geography.Name, resolver); ok {
		canonicalM49 = mapped
	} else {
		diagnostics.add(limits, Diagnostic{
			Severity: DiagnosticWarning,
			Code:     "unmapped_geography",
			Row:      row,
			Field:    schema.GeographyM49Column,
			Message:  fmt.Sprintf("retained source geography %q has no canonical M49 mapping", geography.Name),
		})
	}

	value := parseValue(schema.field(record, schema.ValueColumn))
	if value.Status == ValueInvalid {
		diagnostics.add(limits, Diagnostic{
			Severity: DiagnosticError,
			Code:     "invalid_point_value",
			Row:      row,
			Field:    schema.ValueColumn,
			Message:  "point value is neither numeric nor a recognized missingness status",
		})
	}
	lower := Value{Status: ValueMissing}
	if schema.LowerBoundColumn != "" {
		lower = parseValue(schema.field(record, schema.LowerBoundColumn))
		if lower.Status == ValueInvalid {
			diagnostics.add(limits, Diagnostic{
				Severity: DiagnosticError,
				Code:     "invalid_lower_bound",
				Row:      row,
				Field:    schema.LowerBoundColumn,
				Message:  "lower bound is neither numeric nor a recognized missingness status",
			})
		}
	}
	upper := Value{Status: ValueMissing}
	if schema.UpperBoundColumn != "" {
		upper = parseValue(schema.field(record, schema.UpperBoundColumn))
		if upper.Status == ValueInvalid {
			diagnostics.add(limits, Diagnostic{
				Severity: DiagnosticError,
				Code:     "invalid_upper_bound",
				Row:      row,
				Field:    schema.UpperBoundColumn,
				Message:  "upper bound is neither numeric nor a recognized missingness status",
			})
		}
	}

	dataset := strings.TrimSpace(schema.field(record, schema.IndicatorUUIDColumn))
	unit := ""
	statistic := ""
	valueKind := "number"
	if definition != nil {
		dataset = definition.ID
		unit = definition.Unit
		statistic = definition.Statistic
		valueKind = definition.ValueKind
		if strings.TrimSpace(schema.field(record, schema.IndicatorUUIDColumn)) != definition.IndicatorID || strings.TrimSpace(schema.field(record, schema.IndicatorCodeColumn)) != definition.IndicatorCode {
			diagnostics.add(limits, Diagnostic{
				Severity: DiagnosticError,
				Code:     "curated_indicator_mismatch",
				Row:      row,
				Message:  "row indicator identifiers do not match the selected curated dataset",
			})
		}
	}
	identity := SeriesIdentity{
		Dataset:       dataset,
		IndicatorCode: strings.TrimSpace(schema.field(record, schema.IndicatorCodeColumn)),
		Measure:       strings.TrimSpace(schema.field(record, schema.IndicatorNameColumn)),
		Unit:          unit,
		Statistic:     statistic,
		ValueColumn:   schema.ValueColumn,
		ValueKind:     valueKind,
		Dimensions:    dimensions,
	}
	return Observation{
		Row:          row,
		Year:         year,
		SourceGeo:    geography,
		CanonicalM49: canonicalM49,
		Value:        value,
		LowerBound:   lower,
		UpperBound:   upper,
	}, identity, dimensions
}

func parseValue(raw string) Value {
	display := strings.TrimSpace(raw)
	value := Value{Raw: raw, Display: display}
	if display == "" || strings.EqualFold(display, "NULL") {
		value.Status = ValueMissing
		return value
	}
	switch strings.ToUpper(display) {
	case "SUPPRESSED", "SUPPRESS", "CONFIDENTIAL", "..":
		value.Status = ValueSuppressed
		return value
	case "N/A", "NA", "NOT APPLICABLE", "NOT_APPLICABLE":
		value.Status = ValueNotApplicable
		return value
	}
	number, err := strconv.ParseFloat(display, 64)
	if err != nil || math.IsNaN(number) || math.IsInf(number, 0) {
		value.Status = ValueInvalid
		return value
	}
	value.Numeric = &number
	value.Status = ValueNumeric
	return value
}

func resolveM49(canonical, name string, resolver M49Resolver) (string, bool) {
	if resolver == nil {
		return canonical, true
	}
	mapped, ok := resolver(canonical, name)
	if !ok {
		return "", false
	}
	return canonicalM49(mapped)
}

func canonicalM49(raw string) (string, bool) {
	code, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || code < 1 || code > 999 {
		return "", false
	}
	return fmt.Sprintf("%03d", code), true
}

func sourceRowKey(record []string, schema Schema, year int, dimensions map[string]string) string {
	encoded, _ := json.Marshal(struct {
		IndicatorID     string            `json:"indicator_id"`
		IndicatorCode   string            `json:"indicator_code"`
		IndicatorUUID   string            `json:"indicator_uuid"`
		IndicatorPeriod string            `json:"indicator_period"`
		Year            int               `json:"year"`
		GeographyCode   string            `json:"geography_code"`
		GeographyType   string            `json:"geography_type"`
		Dimensions      map[string]string `json:"dimensions"`
	}{
		IndicatorID:     strings.TrimSpace(schema.field(record, schema.IndicatorIDColumn)),
		IndicatorCode:   strings.TrimSpace(schema.field(record, schema.IndicatorCodeColumn)),
		IndicatorUUID:   strings.TrimSpace(schema.field(record, schema.IndicatorUUIDColumn)),
		IndicatorPeriod: strings.TrimSpace(schema.field(record, schema.IndicatorPeriodColumn)),
		Year:            year,
		GeographyCode:   strings.TrimSpace(schema.field(record, schema.GeographyM49Column)),
		GeographyType:   strings.TrimSpace(schema.field(record, schema.GeographyTypeColumn)),
		Dimensions:      dimensions,
	})
	sum := sha256.Sum256(encoded)
	return fmt.Sprintf("%x", sum)
}

func sameObservation(left, right Observation) bool {
	return left.SeriesHash == right.SeriesHash &&
		left.Year == right.Year &&
		left.SourceGeo == right.SourceGeo &&
		left.CanonicalM49 == right.CanonicalM49 &&
		left.Value.Raw == right.Value.Raw &&
		left.LowerBound.Raw == right.LowerBound.Raw &&
		left.UpperBound.Raw == right.UpperBound.Raw
}
