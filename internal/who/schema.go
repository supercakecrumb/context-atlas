package who

import (
	"crypto/sha256"
	"encoding/csv"
	"fmt"
	"io"
	"sort"
	"strings"
)

const (
	timeColumn            = "DIM_TIME"
	timeTypeColumn        = "DIM_TIME_TYPE"
	geographyM49Column    = "DIM_GEO_CODE_M49"
	geographyTypeColumn   = "DIM_GEO_CODE_TYPE"
	publishStateColumn    = "DIM_PUBLISH_STATE_CODE"
	indicatorIDColumn     = "IND_ID"
	indicatorCodeColumn   = "IND_CODE"
	indicatorUUIDColumn   = "IND_UUID"
	indicatorPeriodColumn = "IND_PER_CODE"
	indicatorNameColumn   = "IND_NAME"
	geographyNameColumn   = "GEO_NAME_SHORT"
)

var requiredHeaders = []string{
	indicatorIDColumn,
	indicatorCodeColumn,
	indicatorUUIDColumn,
	indicatorPeriodColumn,
	timeColumn,
	timeTypeColumn,
	geographyM49Column,
	geographyTypeColumn,
	publishStateColumn,
	indicatorNameColumn,
	geographyNameColumn,
}

var identityDimensionColumns = map[string]struct{}{
	timeColumn:          {},
	timeTypeColumn:      {},
	geographyM49Column:  {},
	geographyTypeColumn: {},
	publishStateColumn:  {},
}

// Schema describes a validated generic DataDot CSV shape. LowerBoundColumn and
// UpperBoundColumn are optional and always belong to ValueColumn when present.
type Schema struct {
	Headers               []string
	Fingerprint           string
	TimeColumn            string
	TimeTypeColumn        string
	GeographyM49Column    string
	GeographyTypeColumn   string
	PublishStateColumn    string
	IndicatorIDColumn     string
	IndicatorCodeColumn   string
	IndicatorUUIDColumn   string
	IndicatorPeriodColumn string
	IndicatorNameColumn   string
	GeographyNameColumn   string
	ValueColumn           string
	LowerBoundColumn      string
	UpperBoundColumn      string
	DimensionColumns      []string

	indices map[string]int
}

// DiscoverSchema validates the generic DataDot CSV contract from its header.
func DiscoverSchema(reader io.Reader) (Schema, error) {
	return DiscoverSchemaWithLimits(reader, DefaultLimits())
}

// DiscoverSchemaWithLimits is DiscoverSchema with an explicit column limit.
func DiscoverSchemaWithLimits(reader io.Reader, limits Limits) (Schema, error) {
	csvReader := csv.NewReader(reader)
	csvReader.FieldsPerRecord = -1
	headers, err := csvReader.Read()
	if err != nil {
		if err == io.EOF {
			return Schema{}, fmt.Errorf("%w: CSV has no header", ErrMalformedCSV)
		}
		return Schema{}, fmt.Errorf("%w: read header: %v", ErrMalformedCSV, err)
	}
	return discoverSchema(headers, limits.normalized())
}

func discoverSchema(headers []string, limits Limits) (Schema, error) {
	if len(headers) == 0 || len(headers) > limits.MaxColumns {
		return Schema{}, fmt.Errorf("%w: CSV has %d columns (limit %d)", ErrUnsupportedSchema, len(headers), limits.MaxColumns)
	}

	indices := make(map[string]int, len(headers))
	normalized := make([]string, len(headers))
	for index, raw := range headers {
		name := strings.TrimSpace(raw)
		if index == 0 {
			name = strings.TrimPrefix(name, "\ufeff")
		}
		if name == "" {
			return Schema{}, fmt.Errorf("%w: empty header at column %d", ErrUnsupportedSchema, index+1)
		}
		if _, exists := indices[name]; exists {
			return Schema{}, fmt.Errorf("%w: duplicate header %q", ErrUnsupportedSchema, name)
		}
		normalized[index] = name
		indices[name] = index
	}
	for _, name := range requiredHeaders {
		if _, exists := indices[name]; !exists {
			return Schema{}, fmt.Errorf("%w: missing required column %s", ErrUnsupportedSchema, name)
		}
	}

	valueColumns := make([]string, 0, 1)
	dimensions := make([]string, 0)
	for _, name := range normalized {
		if strings.HasPrefix(name, "DIM_") {
			if _, core := identityDimensionColumns[name]; !core {
				dimensions = append(dimensions, name)
			}
		}
		if strings.HasSuffix(name, "_N") && len(strings.TrimSuffix(name, "_N")) > 0 {
			valueColumns = append(valueColumns, name)
		}
	}
	if len(valueColumns) != 1 {
		return Schema{}, fmt.Errorf("%w: expected exactly one *_N point-value column, got %d", ErrUnsupportedSchema, len(valueColumns))
	}
	valueColumn := valueColumns[0]
	lowerBound := valueColumn + "L"
	upperBound := valueColumn + "U"
	for _, name := range normalized {
		if strings.HasSuffix(name, "_NL") && name != lowerBound {
			return Schema{}, fmt.Errorf("%w: unmatched lower bound column %s", ErrUnsupportedSchema, name)
		}
		if strings.HasSuffix(name, "_NU") && name != upperBound {
			return Schema{}, fmt.Errorf("%w: unmatched upper bound column %s", ErrUnsupportedSchema, name)
		}
	}
	if _, exists := indices[lowerBound]; !exists {
		lowerBound = ""
	}
	if _, exists := indices[upperBound]; !exists {
		upperBound = ""
	}
	sort.Strings(dimensions)

	sum := sha256.Sum256([]byte(strings.Join(normalized, "\x1f")))
	return Schema{
		Headers:               normalized,
		Fingerprint:           fmt.Sprintf("%x", sum),
		TimeColumn:            timeColumn,
		TimeTypeColumn:        timeTypeColumn,
		GeographyM49Column:    geographyM49Column,
		GeographyTypeColumn:   geographyTypeColumn,
		PublishStateColumn:    publishStateColumn,
		IndicatorIDColumn:     indicatorIDColumn,
		IndicatorCodeColumn:   indicatorCodeColumn,
		IndicatorUUIDColumn:   indicatorUUIDColumn,
		IndicatorPeriodColumn: indicatorPeriodColumn,
		IndicatorNameColumn:   indicatorNameColumn,
		GeographyNameColumn:   geographyNameColumn,
		ValueColumn:           valueColumn,
		LowerBoundColumn:      lowerBound,
		UpperBoundColumn:      upperBound,
		DimensionColumns:      dimensions,
		indices:               indices,
	}, nil
}

func (schema Schema) field(record []string, name string) string {
	return record[schema.indices[name]]
}
