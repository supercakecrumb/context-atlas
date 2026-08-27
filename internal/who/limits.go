package who

const (
	// MaxArtifactBytes is the hard upper bound for a raw WHO CSV artifact.
	MaxArtifactBytes int64 = 50 << 20
	// defaultMaxRows is 2.6x the largest current launch CSV (18,798 rows).
	// The importer rejects the next row rather than truncating, which bounds the
	// in-memory raw bytes, row maps, observations, and diagnostics together.
	defaultMaxRows        = 50_000
	defaultMaxColumns     = 256
	defaultMaxDiagnostics = 1_000
	defaultMaxRedirects   = 3
)

// Limits bound work done for an untrusted remote CSV. Zero values select safe
// defaults; MaxBytes can only tighten the 50 MiB product limit.
type Limits struct {
	MaxBytes       int64
	MaxRows        int
	MaxColumns     int
	MaxDiagnostics int
	MaxRedirects   int
}

// DefaultLimits returns the limits used by both downloading and parsing.
func DefaultLimits() Limits {
	return Limits{
		MaxBytes:       MaxArtifactBytes,
		MaxRows:        defaultMaxRows,
		MaxColumns:     defaultMaxColumns,
		MaxDiagnostics: defaultMaxDiagnostics,
		MaxRedirects:   defaultMaxRedirects,
	}
}

func (limits Limits) normalized() Limits {
	defaults := DefaultLimits()
	if limits.MaxBytes <= 0 || limits.MaxBytes > MaxArtifactBytes {
		limits.MaxBytes = defaults.MaxBytes
	}
	if limits.MaxRows <= 0 {
		limits.MaxRows = defaults.MaxRows
	}
	if limits.MaxColumns <= 0 {
		limits.MaxColumns = defaults.MaxColumns
	}
	if limits.MaxDiagnostics <= 0 {
		limits.MaxDiagnostics = defaults.MaxDiagnostics
	}
	if limits.MaxRedirects <= 0 {
		limits.MaxRedirects = defaults.MaxRedirects
	}
	return limits
}
