package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type catalogReaderFunc func(context.Context, string) (CatalogResult, error)

func (f catalogReaderFunc) Catalog(ctx context.Context, snapshot string) (CatalogResult, error) {
	return f(ctx, snapshot)
}

type observationReaderFake struct {
	query ObservationQuery
	csv   CSVExport
	wrote bool
}

type geographyReaderFake struct{}

func (geographyReaderFake) Geographies(context.Context, GeographyQuery) (GeographyResult, error) {
	return GeographyResult{Meta: testMeta("snap-1")}, nil
}
func (geographyReaderFake) Groups(context.Context, string) (GroupResult, error) {
	return GroupResult{Meta: testMeta("snap-1")}, nil
}
func (geographyReaderFake) Admin0Map(context.Context, string) (GeoJSONExport, error) {
	return GeoJSONExport{
		Meta: testMeta("snap-1"), ETag: "map-sha",
		Body: []byte(`{"type":"FeatureCollection","features":[]}`),
	}, nil
}

func (f *observationReaderFake) Observations(_ context.Context, query ObservationQuery) (ObservationResult, error) {
	f.query = query
	return ObservationResult{
		Meta: testMeta("snap-1"), Pagination: Pagination{Page: query.Page, PageSize: query.PageSize, Total: 1},
		Observations: []Observation{{
			SeriesID: "series-a", Year: query.Years[0], Status: "numeric", PublishState: "PUBLISHED",
		}},
	}, nil
}

func (f *observationReaderFake) ObservationsCSV(_ context.Context, query ObservationQuery) (CSVExport, error) {
	f.query = query
	f.csv = CSVExport{
		Meta: testMeta("snap-1"), Filename: "observations.csv", ETag: "csv-sha",
		Write: func(_ context.Context, w io.Writer) error {
			f.wrote = true
			_, err := io.WriteString(w, "snapshot,year\nsnap-1,2020\n")
			return err
		},
	}
	return f.csv, nil
}

type associationReaderFunc func(context.Context, AssociationQuery) (AssociationResult, error)

func (f associationReaderFunc) Association(ctx context.Context, query AssociationQuery) (AssociationResult, error) {
	return f(ctx, query)
}

type healthCheckerFunc func(context.Context) (HealthReport, error)

func (f healthCheckerFunc) Health(ctx context.Context) (HealthReport, error) { return f(ctx) }

type feedbackServiceFunc func(context.Context, FeedbackRequest) (FeedbackReceipt, error)

func (f feedbackServiceFunc) SubmitFeedback(ctx context.Context, request FeedbackRequest) (FeedbackReceipt, error) {
	return f(ctx, request)
}

type importAdminFake struct {
	url string
}

func (f *importAdminFake) CreatePreview(_ context.Context, url string) (ImportPreview, error) {
	f.url = url
	return ImportPreview{ID: "preview-1", IndicatorURL: url, Status: "pending"}, nil
}
func (*importAdminFake) Preview(context.Context, string) (ImportPreview, error) {
	return ImportPreview{ID: "preview-1", Status: "ready"}, nil
}
func (*importAdminFake) ConfirmPreview(context.Context, string) (ImportRun, error) {
	return ImportRun{ID: "run-1", Status: "pending", Kind: "confirm"}, nil
}
func (*importAdminFake) Refresh(context.Context) (ImportRun, error) {
	return ImportRun{ID: "run-2", Status: "pending", Kind: "manual_refresh"}, nil
}
func (*importAdminFake) ImportRuns(context.Context, int, int) (ImportRunResult, error) {
	return ImportRunResult{}, nil
}
func (*importAdminFake) Freshness(context.Context) (FreshnessResult, error) {
	return FreshnessResult{}, nil
}

func testMeta(snapshot string) ResponseMeta {
	return ResponseMeta{
		Snapshot: SnapshotRef{ID: snapshot, CreatedAt: time.Unix(1, 0).UTC(), M49ReferenceRelease: "m49-1"},
		Releases: []DatasetReleaseRef{{
			ID: "release-1", DatasetID: "dataset-1", SHA256: strings.Repeat("a", 64),
			SourceURL: "https://data.who.int/example.csv", AccessedAt: time.Unix(2, 0).UTC(),
			Citation: "WHO example", ParserVersion: "v1",
		}},
	}
}

func perform(handler http.Handler, method, target, body string, headers map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	for name, value := range headers {
		req.Header.Set(name, value)
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	return w
}

func TestCatalogPinnedSnapshotETagAndCORS(t *testing.T) {
	reader := catalogReaderFunc(func(_ context.Context, snapshot string) (CatalogResult, error) {
		if snapshot != "snap-1" {
			t.Fatalf("snapshot = %q, want snap-1", snapshot)
		}
		return CatalogResult{Meta: testMeta(snapshot), Datasets: []Dataset{}}, nil
	})
	server := New(Options{Catalog: reader})

	first := perform(server, http.MethodGet, CatalogPath+"?snapshot=snap-1", "", nil)
	if first.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", first.Code, first.Body.String())
	}
	if got := first.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Fatalf("CORS = %q", got)
	}
	if got := first.Header().Get("Cache-Control"); !strings.Contains(got, "immutable") {
		t.Fatalf("cache-control = %q", got)
	}
	if got := first.Header().Get("X-Atlas-Snapshot"); got != "snap-1" {
		t.Fatalf("snapshot header = %q", got)
	}
	etag := first.Header().Get("ETag")
	if etag == "" {
		t.Fatal("missing ETag")
	}
	var body map[string]any
	if err := json.Unmarshal(first.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	meta := body["meta"].(map[string]any)
	if got := meta["snapshot"].(map[string]any)["id"]; got != "snap-1" {
		t.Fatalf("body snapshot = %v", got)
	}

	second := perform(server, http.MethodGet, CatalogPath+"?snapshot=snap-1", "", map[string]string{"If-None-Match": `"other", ` + etag})
	if second.Code != http.StatusNotModified {
		t.Fatalf("conditional status = %d, body = %s", second.Code, second.Body.String())
	}
	if second.Body.Len() != 0 {
		t.Fatalf("304 body = %q", second.Body.String())
	}
}

func TestCatalogRejectsSnapshotSubstitution(t *testing.T) {
	server := New(Options{Catalog: catalogReaderFunc(func(context.Context, string) (CatalogResult, error) {
		return CatalogResult{Meta: testMeta("different")}, nil
	})})
	w := perform(server, http.MethodGet, CatalogPath+"?snapshot=pinned", "", nil)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	if got := w.Header().Get("Content-Type"); !strings.HasPrefix(got, "application/problem+json") {
		t.Fatalf("content-type = %q", got)
	}
}

func TestObservationsUseExactYearsAndServerPagination(t *testing.T) {
	fake := &observationReaderFake{}
	server := New(Options{Observations: fake})
	w := perform(server, http.MethodGet, ObservationsPath+"?series=a,b&years=2019,2021&groups=g1,g2&page=2&page_size=50&snapshot=snap-1", "", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	if got := fake.query.Years; len(got) != 2 || got[0] != 2019 || got[1] != 2021 {
		t.Fatalf("years = %v", got)
	}
	if fake.query.Page != 2 || fake.query.PageSize != 50 {
		t.Fatalf("pagination = %d/%d", fake.query.Page, fake.query.PageSize)
	}
	if len(fake.query.Series) != 2 || len(fake.query.Groups) != 2 {
		t.Fatalf("filters = %#v", fake.query)
	}
}

func TestObservationsRejectInvalidYearAndPageSize(t *testing.T) {
	server := New(Options{Observations: &observationReaderFake{}})
	for _, target := range []string{
		ObservationsPath + "?years=0",
		ObservationsPath + "?page_size=501",
	} {
		w := perform(server, http.MethodGet, target, "", nil)
		if w.Code < 400 || w.Code >= 500 {
			t.Fatalf("%s status = %d, body = %s", target, w.Code, w.Body.String())
		}
		if got := w.Header().Get("Content-Type"); !strings.HasPrefix(got, "application/problem+json") {
			t.Fatalf("%s content-type = %q", target, got)
		}
		if got := w.Header().Get("Access-Control-Allow-Origin"); got != "*" {
			t.Fatalf("%s CORS = %q", target, got)
		}
	}
}

func TestObservationsAllowBoundedVisualizationPage(t *testing.T) {
	fake := &observationReaderFake{}
	server := New(Options{Observations: fake})
	w := perform(server, http.MethodGet, ObservationsPath+"?years=2020&page_size=500&snapshot=snap-1", "", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	if fake.query.PageSize != 500 {
		t.Fatalf("page_size = %d", fake.query.PageSize)
	}
}

func TestAssociationKeepsIndependentExactYears(t *testing.T) {
	var got AssociationQuery
	server := New(Options{Associations: associationReaderFunc(func(_ context.Context, query AssociationQuery) (AssociationResult, error) {
		got = query
		return AssociationResult{
			Meta: testMeta("snap-1"), XSeriesID: query.XSeries, XYear: query.XYear,
			YSeriesID: query.YSeries, YYear: query.YYear, Points: []AssociationPoint{}, Warnings: []string{},
		}, nil
	})})
	w := perform(server, http.MethodGet, AssociationPath+"?x_series=x&x_year=2017&y_series=y&y_year=2022&snapshot=snap-1", "", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	if got.XYear != 2017 || got.YYear != 2022 {
		t.Fatalf("years = %d/%d", got.XYear, got.YYear)
	}
}

func TestCSVStreamsWithoutPaginationAndHonorsETag(t *testing.T) {
	fake := &observationReaderFake{}
	server := New(Options{Observations: fake})
	w := perform(server, http.MethodGet, ObservationsCSVPath+"?years=2020&snapshot=snap-1", "", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	if got := w.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/csv") {
		t.Fatalf("content-type = %q", got)
	}
	if got := w.Header().Get("Content-Disposition"); !strings.Contains(got, "observations.csv") {
		t.Fatalf("content-disposition = %q", got)
	}
	if !fake.wrote || !strings.Contains(w.Body.String(), "snap-1,2020") {
		t.Fatalf("CSV not streamed: %q", w.Body.String())
	}

	fake.wrote = false
	conditional := perform(server, http.MethodGet, ObservationsCSVPath+"?years=2020&snapshot=snap-1", "", map[string]string{"If-None-Match": w.Header().Get("ETag")})
	if conditional.Code != http.StatusNotModified || fake.wrote {
		t.Fatalf("conditional status/wrote = %d/%v", conditional.Code, fake.wrote)
	}
}

func TestMapReturnsRawGeoJSONWithSnapshotHeaders(t *testing.T) {
	server := New(Options{Geographies: geographyReaderFake{}})
	w := perform(server, http.MethodGet, Admin0MapPath+"?snapshot=snap-1", "", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	if got := w.Header().Get("Content-Type"); got != "application/geo+json" {
		t.Fatalf("content-type = %q", got)
	}
	if got := w.Header().Get("X-Atlas-Snapshot"); got != "snap-1" {
		t.Fatalf("snapshot = %q", got)
	}
	if got := w.Header().Get("Cache-Control"); !strings.Contains(got, "immutable") {
		t.Fatalf("cache-control = %q", got)
	}
	if !strings.Contains(w.Body.String(), "FeatureCollection") {
		t.Fatalf("body = %q", w.Body.String())
	}
}

func TestAdminRoutesDenyByDefaultAndValidateWHOURL(t *testing.T) {
	imports := &importAdminFake{}
	denied := New(Options{Imports: imports})
	w := perform(denied, http.MethodPost, AdminPreviewsPath, `{"url":"https://data.who.int/indicators/i/a/b"}`, map[string]string{"Content-Type": "application/json"})
	if w.Code != http.StatusUnauthorized || !strings.HasPrefix(w.Header().Get("Content-Type"), "application/problem+json") {
		t.Fatalf("denied status/content-type = %d/%q", w.Code, w.Header().Get("Content-Type"))
	}

	auth := Middleware(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("X-Test-Owner") != "yes" {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	})
	server := New(Options{Imports: imports, AdminAuth: auth})
	bad := perform(server, http.MethodPost, AdminPreviewsPath, `{"url":"https://example.com/indicators/i/a/b"}`, map[string]string{
		"Content-Type": "application/json", "X-Test-Owner": "yes",
	})
	if bad.Code != http.StatusBadRequest {
		t.Fatalf("bad URL status = %d, body = %s", bad.Code, bad.Body.String())
	}
	for _, goodURL := range []string{
		"https://data.who.int/indicators/i/16BBF41",
		"https://data.who.int/indicators/i/F08B4FD/16BBF41",
	} {
		good := perform(server, http.MethodPost, AdminPreviewsPath, `{"url":"`+goodURL+`"}`, map[string]string{
			"Content-Type": "application/json", "X-Test-Owner": "yes",
		})
		if good.Code != http.StatusAccepted || imports.url != goodURL {
			t.Fatalf("good URL status/url = %d/%q, body = %s", good.Code, imports.url, good.Body.String())
		}
	}
}

func TestHealthFailsForRequiredDependency(t *testing.T) {
	server := New(Options{Health: healthCheckerFunc(func(context.Context) (HealthReport, error) {
		return HealthReport{Status: "ok", Dependencies: []DependencyHealth{{Name: "postgres", Required: true, Ready: false}}}, nil
	})})
	w := perform(server, http.MethodGet, HealthPath, "", nil)
	if w.Code != http.StatusServiceUnavailable || !strings.Contains(w.Body.String(), `"status":"degraded"`) {
		t.Fatalf("status/body = %d/%s", w.Code, w.Body.String())
	}
}

func TestFeedbackAcceptsRelativePageURL(t *testing.T) {
	var got FeedbackRequest
	server := New(Options{Feedback: feedbackServiceFunc(func(_ context.Context, request FeedbackRequest) (FeedbackReceipt, error) {
		got = request
		return FeedbackReceipt{ID: "feedback-1", ReceivedAt: time.Unix(3, 0).UTC()}, nil
	})})
	w := perform(server, http.MethodPost, FeedbackPath, `{"message":"Map is unclear","page_url":"/explore?tab=map#chart"}`, map[string]string{
		"Content-Type": "application/json",
	})
	if w.Code != http.StatusAccepted {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	if got.PageURL != "/explore?tab=map#chart" {
		t.Fatalf("page_url = %q", got.PageURL)
	}
}

func TestOpenAPIContainsPublicAndProtectedContracts(t *testing.T) {
	server := New(Options{})
	w := perform(server, http.MethodGet, "/api/v1/openapi.json", "", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	for _, path := range []string{
		CatalogPath, GeographiesPath, GroupsPath, Admin0MapPath, ObservationsPath,
		ObservationsCSVPath, AssociationPath, FeedbackPath, AdminPreviewsPath,
		AdminPreviewPath, AdminConfirmPath, AdminRefreshPath, AdminRunsPath,
		AdminFreshnessPath, AdminSessionPath,
	} {
		if !strings.Contains(w.Body.String(), `"`+path+`"`) {
			t.Errorf("OpenAPI is missing %s", path)
		}
	}
	if !strings.Contains(w.Body.String(), `"ownerSession"`) {
		t.Error("OpenAPI is missing owner session security")
	}
	var document struct {
		Components struct {
			Schemas map[string]json.RawMessage `json:"schemas"`
		} `json:"components"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &document); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{
		"SnapshotRef", "Dataset", "Measure", "Series", "Geography", "Group",
		"Observation", "AssociationResult", "ImportPreview", "ImportRun",
	} {
		if _, ok := document.Components.Schemas[name]; !ok {
			t.Errorf("OpenAPI is missing core schema %s; schemas: %v", name, schemaNames(document.Components.Schemas))
		}
	}
	var observationSchema struct {
		Properties map[string]json.RawMessage `json:"properties"`
	}
	if err := json.Unmarshal(document.Components.Schemas["Observation"], &observationSchema); err != nil {
		t.Fatal(err)
	}
	if _, ok := observationSchema.Properties["publish_state"]; !ok {
		t.Error("Observation schema is missing publish_state")
	}
	statusSchema := string(observationSchema.Properties["status"])
	if !strings.Contains(statusSchema, `"numeric"`) || strings.Contains(statusSchema, `"published"`) {
		t.Errorf("Observation status schema has the wrong enum: %s", statusSchema)
	}
	docs := perform(server, http.MethodGet, "/api/docs", "", nil)
	if docs.Code != http.StatusOK {
		t.Fatalf("docs status = %d, body = %s", docs.Code, docs.Body.String())
	}
}

func schemaNames(schemas map[string]json.RawMessage) []string {
	names := make([]string, 0, len(schemas))
	for name := range schemas {
		names = append(names, name)
	}
	return names
}

func TestETagMatchesWhitespaceAndWeakTags(t *testing.T) {
	for _, header := range []string{`"x"`, `"other", "x"`, `W/"x"`, `*`} {
		if !etagMatches(header, `"x"`) {
			t.Errorf("%q did not match", header)
		}
	}
	if etagMatches(`"other"`, `"x"`) {
		t.Error("unrelated tag matched")
	}
}

func TestWHOIndicatorURLAllowsOnlyCanonicalPage(t *testing.T) {
	for _, raw := range []string{
		"https://data.who.int/indicators/i/16BBF41",
		"https://data.who.int/indicators/i/F08B4FD/16BBF41",
	} {
		if err := validateWHOIndicatorURL(raw); err != nil {
			t.Fatalf("canonical URL rejected: %s: %v", raw, err)
		}
	}
	for _, raw := range []string{
		"http://data.who.int/indicators/i/F08B4FD/16BBF41",
		"https://data.who.int:443/indicators/i/F08B4FD/16BBF41",
		"https://data.who.int/indicators/i/F08B4FD/16BBF41?download=1",
		"https://data.who.int/indicators/i/a/b/extra",
		"https://data.who.int/indicators/i/a/%2e%2e",
	} {
		if err := validateWHOIndicatorURL(raw); err == nil {
			t.Errorf("unsafe/non-canonical URL accepted: %s", raw)
		}
	}
}
