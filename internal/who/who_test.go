package who

import (
	"context"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCuratedDefinitionsUseOnlyValidatedURLs(t *testing.T) {
	t.Parallel()
	definitions := CuratedDefinitions()
	if len(definitions) != 6 {
		t.Fatalf("definitions = %d, want 6", len(definitions))
	}
	for _, definition := range definitions {
		if definition.ValueKind != "number" {
			t.Fatalf("%s value kind = %q, want number", definition.ID, definition.ValueKind)
		}
		page, err := ValidateIndicatorPageURL(definition.PageURL)
		if err != nil {
			t.Fatalf("validate %s page: %v", definition.ID, err)
		}
		if page.IndicatorID != definition.IndicatorID {
			t.Fatalf("%s page indicator = %s, want %s", definition.ID, page.IndicatorID, definition.IndicatorID)
		}
		download, err := ValidateDownloadURL("https://" + WHOBlobHost + WHOBlobPathPrefix + definition.IndicatorID + "_ALL_LATEST.csv")
		if err != nil {
			t.Fatalf("validate %s download: %v", definition.ID, err)
		}
		if download.IndicatorID != definition.IndicatorID {
			t.Fatalf("%s download indicator = %s, want %s", definition.ID, download.IndicatorID, definition.IndicatorID)
		}
		if got, ok := DefinitionForIndicator(page); !ok || got.ID != definition.ID {
			t.Fatalf("DefinitionForIndicator(%s) = %#v, %t", definition.ID, got, ok)
		}
	}
}

func TestURLValidationRejectsNonCanonicalOrUnapprovedURLs(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{
		"http://data.who.int/indicators/i/F08B4FD/16BBF41",
		"https://data.who.int/indicators/i/F08B4FD/16BBF41?m49=004",
		"https://data.who.int.evil.example/indicators/i/F08B4FD/16BBF41",
		"https://data.who.int/indicators/i/F08B4FD/16BBF41/",
		"https://data.who.int:443/indicators/i/F08B4FD/16BBF41",
	} {
		if _, err := ValidateIndicatorPageURL(raw); !errors.Is(err, ErrInvalidIndicatorURL) {
			t.Fatalf("ValidateIndicatorPageURL(%q) error = %v, want invalid indicator URL", raw, err)
		}
	}
	for _, raw := range []string{
		"https://evil.example/whdh/DATADOT/INDICATOR/16BBF41_ALL_LATEST.csv",
		"https://srhdpeuwpubsa.blob.core.windows.net/whdh/DATADOT/INDICATOR/16BBF41_ALL_LATEST.csv?sig=secret",
		"https://srhdpeuwpubsa.blob.core.windows.net/whdh/DATADOT/INDICATOR/16BBF41.csv",
		"https://srhdpeuwpubsa.blob.core.windows.net/whdh/DATADOT/INDICATOR/../16BBF41_ALL_LATEST.csv",
	} {
		if _, err := ValidateDownloadURL(raw); !errors.Is(err, ErrInvalidDownloadURL) {
			t.Fatalf("ValidateDownloadURL(%q) error = %v, want invalid download URL", raw, err)
		}
	}
}

func TestDiscoverDownloadAnchorRequiresTheSameIndicator(t *testing.T) {
	t.Parallel()
	page, err := ValidateIndicatorPageURL("https://data.who.int/indicators/i/F08B4FD/16BBF41")
	if err != nil {
		t.Fatal(err)
	}
	body := []byte(`
		<a href="https://srhdpeuwpubsa.blob.core.windows.net/whdh/DATADOT/INDICATOR/EE6F72A_ALL_LATEST.csv">other</a>
		<a href="//srhdpeuwpubsa.blob.core.windows.net/whdh/DATADOT/INDICATOR/16BBF41_ALL_LATEST.csv">Download</a>
	`)
	download, err := discoverDownloadAnchor(page, body)
	if err != nil {
		t.Fatal(err)
	}
	if download.IndicatorID != page.IndicatorID {
		t.Fatalf("download indicator = %s, want %s", download.IndicatorID, page.IndicatorID)
	}
	if _, err := discoverDownloadAnchor(page, []byte(`<a href="https://srhdpeuwpubsa.blob.core.windows.net/whdh/DATADOT/INDICATOR/EE6F72A_ALL_LATEST.csv">Download</a>`)); !errors.Is(err, ErrInvalidDownloadURL) {
		t.Fatalf("mismatched download error = %v, want invalid download URL", err)
	}
}

func TestNetworkGuardsRejectUnsafeDNSAndOversizedBodies(t *testing.T) {
	t.Parallel()
	publicResolver := IPResolverFunc(func(context.Context, string) ([]net.IPAddr, error) {
		return []net.IPAddr{{IP: net.ParseIP("8.8.8.8")}}, nil
	})
	if _, err := ResolvePublicHost(context.Background(), publicResolver, WHOBlobHost); err != nil {
		t.Fatalf("public resolver: %v", err)
	}
	for _, address := range []string{"127.0.0.1", "10.0.0.1", "169.254.1.1", "::1", "2001:db8::1"} {
		resolver := IPResolverFunc(func(context.Context, string) ([]net.IPAddr, error) {
			return []net.IPAddr{{IP: net.ParseIP(address)}}, nil
		})
		if _, err := ResolvePublicHost(context.Background(), resolver, WHOBlobHost); !errors.Is(err, ErrUnsafeAddress) {
			t.Fatalf("ResolvePublicHost(%s) error = %v, want unsafe address", address, err)
		}
	}
	if _, err := readCapped(strings.NewReader("123"), 2); !errors.Is(err, ErrArtifactTooLarge) {
		t.Fatalf("readCapped error = %v, want artifact too large", err)
	}
	if isCSVContentType("text/html") || !isCSVContentType("text/csv; charset=utf-8") || !isHTMLContentType("text/html; charset=utf-8") {
		t.Fatal("content-type checks accepted/rejected an unexpected type")
	}
}

func TestRedirectLimitIsBoundedAndValidated(t *testing.T) {
	t.Parallel()
	fetcher := NewFetcher(FetchOptions{Resolver: IPResolverFunc(func(context.Context, string) ([]net.IPAddr, error) {
		return []net.IPAddr{{IP: net.ParseIP("8.8.8.8")}}, nil
	})})
	client := fetcher.httpClient(WHOBlobHost, func(raw string) error {
		_, err := ValidateDownloadURL(raw)
		return err
	})
	request, err := http.NewRequest(http.MethodGet, "https://srhdpeuwpubsa.blob.core.windows.net/whdh/DATADOT/INDICATOR/16BBF41_ALL_LATEST.csv", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.CheckRedirect(request, make([]*http.Request, DefaultLimits().MaxRedirects)); err != nil {
		t.Fatalf("redirect at limit unexpectedly rejected: %v", err)
	}
	if err := client.CheckRedirect(request, make([]*http.Request, DefaultLimits().MaxRedirects+1)); err == nil {
		t.Fatal("redirect beyond limit unexpectedly accepted")
	}
}

func TestBuildPreviewPreservesValuesDimensionsAndUnmappedAreas(t *testing.T) {
	t.Parallel()
	accessedAt := time.Date(2026, time.August, 27, 2, 15, 0, 0, time.UTC)
	resolverInputs := make([]string, 0, 3)
	preview, err := BuildPreview(readFixture(t, "generic.csv"), PreviewOptions{
		AccessedAt: accessedAt,
		ResolveM49: func(code, _ string) (string, bool) {
			resolverInputs = append(resolverInputs, code)
			return code, code == "004"
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !preview.Valid() || preview.Accounting.RowsRead != 3 || preview.Accounting.UniqueObservations != 3 {
		t.Fatalf("preview accounting = %#v, valid=%t", preview.Accounting, preview.Valid())
	}
	if preview.Schema.ValueColumn != "RATE_PER_100000_N" || preview.Schema.LowerBoundColumn != "RATE_PER_100000_NL" || preview.Schema.UpperBoundColumn != "RATE_PER_100000_NU" {
		t.Fatalf("schema = %#v", preview.Schema)
	}
	if len(preview.Schema.DimensionColumns) != 2 || preview.Schema.DimensionColumns[0] != "DIM_AGE" || preview.Schema.DimensionColumns[1] != "DIM_SEX" {
		t.Fatalf("dimension columns = %#v", preview.Schema.DimensionColumns)
	}
	first := preview.Observations[0]
	if first.CanonicalM49 != "004" || first.Value.Numeric == nil || *first.Value.Numeric != 0 || first.Value.Status != ValueNumeric {
		t.Fatalf("zero observation = %#v", first)
	}
	last := preview.Observations[2]
	if last.CanonicalM49 != "" || last.Value.Status != ValueSuppressed || last.Value.Numeric != nil || last.SourceGeo.Code != "999" {
		t.Fatalf("unmapped/suppressed observation = %#v", last)
	}
	if preview.Diagnostics.Warnings != 1 || preview.AccessedAt != accessedAt || len(preview.Series) != 3 {
		t.Fatalf("preview warnings/time/series = %#v, %s, %d", preview.Diagnostics, preview.AccessedAt, len(preview.Series))
	}
	if len(resolverInputs) != 3 || resolverInputs[0] != "004" {
		t.Fatalf("resolver inputs = %#v, want zero-padded M49 codes", resolverInputs)
	}
}

func TestBuildPreviewCollapsesExactDuplicatesAndRejectsConflicts(t *testing.T) {
	t.Parallel()
	preview, err := BuildPreview(readFixture(t, "duplicates.csv"), PreviewOptions{})
	if !errors.Is(err, ErrPreviewInvalid) {
		t.Fatalf("BuildPreview error = %v, want invalid preview", err)
	}
	if preview.Valid() || preview.Accounting.RowsRead != 3 || preview.Accounting.UniqueObservations != 1 || preview.Accounting.ExactDuplicates != 1 || preview.Accounting.ConflictingRows != 1 {
		t.Fatalf("preview accounting = %#v, valid=%t", preview.Accounting, preview.Valid())
	}
	if preview.Diagnostics.Errors != 1 || preview.Diagnostics.Warnings != 1 {
		t.Fatalf("diagnostics = %#v", preview.Diagnostics)
	}
}

func TestBuildPreviewTreatsPublishStateAsConflictingPayload(t *testing.T) {
	t.Parallel()
	preview, err := BuildPreview(readFixture(t, "publish_state_conflict.csv"), PreviewOptions{})
	if !errors.Is(err, ErrPreviewInvalid) {
		t.Fatalf("BuildPreview error = %v, want invalid preview", err)
	}
	if preview.Accounting.UniqueObservations != 1 || preview.Accounting.ConflictingRows != 1 {
		t.Fatalf("preview accounting = %#v", preview.Accounting)
	}
	if got := preview.Observations[0].SourceGeo.PublishState; got != "PUBLISHED" {
		t.Fatalf("persisted publish state = %q, want PUBLISHED", got)
	}
}

func TestRowLimitAcceptsItsBoundaryAndRejectsTheNextRow(t *testing.T) {
	t.Parallel()
	const header = "IND_ID,IND_CODE,IND_UUID,IND_PER_CODE,DIM_TIME,DIM_TIME_TYPE,DIM_GEO_CODE_M49,DIM_GEO_CODE_TYPE,DIM_PUBLISH_STATE_CODE,IND_NAME,GEO_NAME_SHORT,DIM_SEX,RATE_PER_100000_N"
	rows := []string{
		"ABC1234TEST,TEST,ABC1234,TEST,2020,YEAR,4,COUNTRY,PUBLISHED,Example measure,Afghanistan,TOTAL,1",
		"ABC1234TEST,TEST,ABC1234,TEST,2020,YEAR,8,COUNTRY,PUBLISHED,Example measure,Albania,TOTAL,1",
		"ABC1234TEST,TEST,ABC1234,TEST,2020,YEAR,12,COUNTRY,PUBLISHED,Example measure,Algeria,TOTAL,1",
	}
	limits := Limits{MaxRows: 2}
	accepted, err := BuildPreview([]byte(header+"\n"+strings.Join(rows[:2], "\n")+"\n"), PreviewOptions{Limits: limits})
	if err != nil || accepted.Accounting.UniqueObservations != 2 {
		t.Fatalf("boundary preview = %#v, %v", accepted.Accounting, err)
	}
	rejected, err := BuildPreview([]byte(header+"\n"+strings.Join(rows, "\n")+"\n"), PreviewOptions{Limits: limits})
	if !errors.Is(err, ErrPreviewInvalid) {
		t.Fatalf("over-limit error = %v, want invalid preview", err)
	}
	if rejected.Accounting.RowsRead != 3 || rejected.Accounting.UniqueObservations != 2 || rejected.Accounting.InvalidRows != 1 {
		t.Fatalf("over-limit accounting = %#v", rejected.Accounting)
	}
	if got := DefaultLimits().MaxRows; got != 50_000 {
		t.Fatalf("default max rows = %d, want 50000", got)
	}
}

func TestSchemaAndSeriesContractsRejectUnsupportedShapesAndRemainStable(t *testing.T) {
	t.Parallel()
	raw := readFixture(t, "generic.csv")
	schema, err := DiscoverSchema(strings.NewReader(string(raw)))
	if err != nil {
		t.Fatal(err)
	}
	definition, ok := CuratedDefinition("suicide-mortality")
	if !ok {
		t.Fatal("missing suicide definition")
	}
	if err := definition.ValidateSchema(schema); err != nil {
		t.Fatalf("matching curated schema: %v", err)
	}
	if _, err := BuildPreview([]byte(strings.Replace(string(raw), "RATE_PER_100000_NL,RATE_PER_100000_NU", "RATE_PER_100000_NL,RATE_PER_100000_NU,SECOND_N", 1)), PreviewOptions{}); !errors.Is(err, ErrMalformedCSV) && !errors.Is(err, ErrUnsupportedSchema) {
		t.Fatalf("two-value-column error = %v", err)
	}
	if _, err := BuildPreview([]byte(strings.Replace(string(raw), ",2020,YEAR,", ",2020,MONTH,", 1)), PreviewOptions{}); !errors.Is(err, ErrPreviewInvalid) {
		t.Fatalf("non-year row error = %v", err)
	}
	if _, err := BuildPreview([]byte(strings.Replace(string(raw), ",2020,YEAR,4,", ",2020,YEAR,not-m49,", 1)), PreviewOptions{}); !errors.Is(err, ErrPreviewInvalid) {
		t.Fatalf("non-M49 geography error = %v", err)
	}
	for _, rawM49 := range []string{"0", "1000", "not-m49"} {
		if _, ok := canonicalM49(rawM49); ok {
			t.Fatalf("canonicalM49(%q) unexpectedly accepted an invalid code", rawM49)
		}
	}

	left := SeriesIdentity{Dataset: "x", Dimensions: map[string]string{"DIM_SEX": "TOTAL", "DIM_AGE": "ALL"}}
	right := SeriesIdentity{Dataset: "x", Dimensions: map[string]string{"DIM_AGE": "ALL", "DIM_SEX": "TOTAL"}}
	leftHash, err := left.Hash()
	if err != nil {
		t.Fatal(err)
	}
	rightHash, err := right.Hash()
	if err != nil {
		t.Fatal(err)
	}
	if leftHash != rightHash {
		t.Fatalf("canonical series hashes differ: %s != %s", leftHash, rightHash)
	}
}

func TestLiveCuratedImports(t *testing.T) {
	if os.Getenv("CONTEXT_ATLAS_LIVE_WHO") != "1" {
		t.Skip("set CONTEXT_ATLAS_LIVE_WHO=1 to fetch the six public WHO datasets")
	}
	fetcher := NewFetcher(FetchOptions{Timeout: 2 * time.Minute})
	for _, definition := range CuratedDefinitions() {
		t.Run(definition.ID, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
			defer cancel()
			fetched, err := fetcher.FetchFromIndicatorPage(ctx, definition.PageURL)
			if err != nil {
				t.Fatal(err)
			}
			preview, err := BuildPreview(fetched.Artifact.Bytes, PreviewOptions{
				Dataset:    &definition,
				SourceURL:  fetched.Artifact.URL,
				AccessedAt: fetched.Artifact.AccessedAt,
			})
			if err != nil {
				t.Fatal(err)
			}
			if !preview.Valid() || preview.Accounting.RowsRead == 0 || preview.Accounting.UniqueObservations == 0 {
				t.Fatalf("invalid live preview: valid=%t accounting=%#v", preview.Valid(), preview.Accounting)
			}
		})
	}
}

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	bytes, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return bytes
}
