package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"mime"
	"net/http"
	"net/url"
	"path"
	"strings"

	"github.com/supercakecrumb/context-atlas/internal/who"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
)

const (
	CatalogPath         = "/api/v1/catalog"
	GeographiesPath     = "/api/v1/geographies"
	GroupsPath          = "/api/v1/groups"
	Admin0MapPath       = "/api/v1/maps/admin0-50m.geojson"
	ObservationsPath    = "/api/v1/observations"
	ObservationsCSVPath = "/api/v1/observations.csv"
	AssociationPath     = "/api/v1/association"
	FeedbackPath        = "/api/v1/feedback"
	HealthPath          = "/health"

	AdminPreviewsPath  = "/api/v1/admin/import-previews"
	AdminPreviewPath   = "/api/v1/admin/import-previews/{preview_id}"
	AdminConfirmPath   = "/api/v1/admin/import-previews/{preview_id}/confirm"
	AdminRefreshPath   = "/api/v1/admin/refresh"
	AdminRunsPath      = "/api/v1/admin/import-runs"
	AdminFreshnessPath = "/api/v1/admin/freshness"
	AdminSessionPath   = "/api/v1/admin/session"
)

type Options struct {
	Catalog      CatalogReader
	Geographies  GeographyReader
	Observations ObservationReader
	Associations AssociationReader
	Feedback     FeedbackService
	Health       HealthChecker
	Imports      ImportAdmin
	Sessions     SessionAdmin
	AdminAuth    Middleware
	Logger       *slog.Logger
}

type Server struct {
	mux  *http.ServeMux
	api  huma.API
	opts Options
}

func New(opts Options) *Server {
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if opts.AdminAuth == nil {
		opts.AdminAuth = denyAdmin
	}

	mux := http.NewServeMux()
	config := huma.DefaultConfig("Context Atlas API", "0.1.0")
	config.OpenAPIPath = "/api/v1/openapi"
	config.DocsPath = "/api/docs"
	config.SchemasPath = "/api/v1/schemas"
	config.RejectUnknownQueryParameters = true
	config.Info.Description = "Public exact-year WHO data exploration and owner-only import administration."
	config.Info.License = &huma.License{Name: "MIT", Identifier: "MIT"}

	humaAPI := humago.New(mux, config)
	if humaAPI.OpenAPI().Components.SecuritySchemes == nil {
		humaAPI.OpenAPI().Components.SecuritySchemes = map[string]*huma.SecurityScheme{}
	}
	humaAPI.OpenAPI().Components.SecuritySchemes["ownerSession"] = &huma.SecurityScheme{
		Type: "apiKey",
		In:   "cookie",
		Name: "context_atlas_session",
	}

	s := &Server{mux: mux, api: humaAPI, opts: opts}
	s.registerPublic()
	s.registerAdmin()
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

func (s *Server) OpenAPI() *huma.OpenAPI {
	return s.api.OpenAPI()
}

type responseHeaders struct {
	ETag         string `header:"ETag"`
	CacheControl string `header:"Cache-Control"`
	Snapshot     string `header:"X-Atlas-Snapshot"`
	Releases     string `header:"X-Atlas-Releases"`
}

type catalogOutput struct {
	Status       int
	ETag         string `header:"ETag"`
	CacheControl string `header:"Cache-Control"`
	Snapshot     string `header:"X-Atlas-Snapshot"`
	Releases     string `header:"X-Atlas-Releases"`
	Body         *CatalogResult
}

type geographyOutput struct {
	Status       int
	ETag         string `header:"ETag"`
	CacheControl string `header:"Cache-Control"`
	Snapshot     string `header:"X-Atlas-Snapshot"`
	Releases     string `header:"X-Atlas-Releases"`
	Body         *GeographyResult
}

type groupOutput struct {
	Status       int
	ETag         string `header:"ETag"`
	CacheControl string `header:"Cache-Control"`
	Snapshot     string `header:"X-Atlas-Snapshot"`
	Releases     string `header:"X-Atlas-Releases"`
	Body         *GroupResult
}

type observationOutput struct {
	Status       int
	ETag         string `header:"ETag"`
	CacheControl string `header:"Cache-Control"`
	Snapshot     string `header:"X-Atlas-Snapshot"`
	Releases     string `header:"X-Atlas-Releases"`
	Body         *ObservationResult
}

type associationOutput struct {
	Status       int
	ETag         string `header:"ETag"`
	CacheControl string `header:"Cache-Control"`
	Snapshot     string `header:"X-Atlas-Snapshot"`
	Releases     string `header:"X-Atlas-Releases"`
	Body         *AssociationResult
}

type streamOutput struct {
	Status             int
	ETag               string `header:"ETag"`
	CacheControl       string `header:"Cache-Control"`
	Snapshot           string `header:"X-Atlas-Snapshot"`
	Releases           string `header:"X-Atlas-Releases"`
	ContentType        string `header:"Content-Type"`
	ContentDisposition string `header:"Content-Disposition"`
	Body               func(huma.Context)
}

type snapshotInput struct {
	Snapshot    string `query:"snapshot" doc:"Immutable snapshot ID; omit for latest"`
	IfNoneMatch string `header:"If-None-Match"`
}

type observationsInput struct {
	Series      []string `query:"series" doc:"Comma-separated exact series IDs"`
	Years       []int    `query:"years" doc:"Comma-separated exact years; never interpolated"`
	Geographies []string `query:"geographies" doc:"Comma-separated source or M49 geography IDs"`
	Groups      []string `query:"groups" doc:"Comma-separated group IDs; overlapping members are de-duplicated"`
	Snapshot    string   `query:"snapshot" doc:"Immutable snapshot ID; omit for latest"`
	Page        int      `query:"page" default:"1" minimum:"1"`
	PageSize    int      `query:"page_size" default:"25" enum:"25,50,100,500" doc:"Use 25, 50, or 100 for tables; 500 is the bounded visualization page"`
	IfNoneMatch string   `header:"If-None-Match"`
}

type observationsCSVInput struct {
	Series      []string `query:"series" doc:"Comma-separated exact series IDs"`
	Years       []int    `query:"years" doc:"Comma-separated exact years; never interpolated"`
	Geographies []string `query:"geographies" doc:"Comma-separated source or M49 geography IDs"`
	Groups      []string `query:"groups" doc:"Comma-separated group IDs; overlapping members are de-duplicated"`
	Snapshot    string   `query:"snapshot" doc:"Immutable snapshot ID; omit for latest"`
	IfNoneMatch string   `header:"If-None-Match"`
}

func (s *Server) registerPublic() {
	huma.Register(s.api, publicRead(huma.Operation{
		OperationID: "get-catalog", Method: http.MethodGet, Path: CatalogPath,
		Summary:   "Get the dataset catalog at an exact snapshot",
		Responses: etagResponses(), Errors: []int{http.StatusNotFound, http.StatusServiceUnavailable},
	}), func(ctx context.Context, in *snapshotInput) (*catalogOutput, error) {
		if s.opts.Catalog == nil {
			return nil, s.fail("get-catalog", ErrUnavailable)
		}
		result, err := s.opts.Catalog.Catalog(ctx, in.Snapshot)
		if err != nil {
			return nil, s.fail("get-catalog", err)
		}
		h, status, err := cacheResponse(result.Meta, in.Snapshot, in.IfNoneMatch, result)
		if err != nil {
			return nil, s.fail("get-catalog", err)
		}
		out := &catalogOutput{Status: status, ETag: h.ETag, CacheControl: h.CacheControl, Snapshot: h.Snapshot, Releases: h.Releases}
		if status != http.StatusNotModified {
			out.Body = &result
		}
		return out, nil
	})

	type geographiesInput struct {
		Snapshot    string `query:"snapshot" doc:"Immutable snapshot ID; omit for latest"`
		Search      string `query:"search" maxLength:"200"`
		IfNoneMatch string `header:"If-None-Match"`
	}
	huma.Register(s.api, publicRead(huma.Operation{
		OperationID: "list-geographies", Method: http.MethodGet, Path: GeographiesPath,
		Summary:   "Search WHO source and canonical M49 geographies",
		Responses: etagResponses(), Errors: []int{http.StatusNotFound, http.StatusServiceUnavailable},
	}), func(ctx context.Context, in *geographiesInput) (*geographyOutput, error) {
		if s.opts.Geographies == nil {
			return nil, s.fail("list-geographies", ErrUnavailable)
		}
		result, err := s.opts.Geographies.Geographies(ctx, GeographyQuery{Snapshot: in.Snapshot, Search: in.Search})
		if err != nil {
			return nil, s.fail("list-geographies", err)
		}
		h, status, err := cacheResponse(result.Meta, in.Snapshot, in.IfNoneMatch, result)
		if err != nil {
			return nil, s.fail("list-geographies", err)
		}
		out := &geographyOutput{Status: status, ETag: h.ETag, CacheControl: h.CacheControl, Snapshot: h.Snapshot, Releases: h.Releases}
		if status != http.StatusNotModified {
			out.Body = &result
		}
		return out, nil
	})

	huma.Register(s.api, publicRead(huma.Operation{
		OperationID: "list-groups", Method: http.MethodGet, Path: GroupsPath,
		Summary:     "Get versioned UN M49 and custom groups",
		Description: "Groups filter and de-duplicate country selections; they never generate averages.",
		Responses:   etagResponses(), Errors: []int{http.StatusNotFound, http.StatusServiceUnavailable},
	}), func(ctx context.Context, in *snapshotInput) (*groupOutput, error) {
		if s.opts.Geographies == nil {
			return nil, s.fail("list-groups", ErrUnavailable)
		}
		result, err := s.opts.Geographies.Groups(ctx, in.Snapshot)
		if err != nil {
			return nil, s.fail("list-groups", err)
		}
		h, status, err := cacheResponse(result.Meta, in.Snapshot, in.IfNoneMatch, result)
		if err != nil {
			return nil, s.fail("list-groups", err)
		}
		out := &groupOutput{Status: status, ETag: h.ETag, CacheControl: h.CacheControl, Snapshot: h.Snapshot, Releases: h.Releases}
		if status != http.StatusNotModified {
			out.Body = &result
		}
		return out, nil
	})

	mapResponses := map[string]*huma.Response{
		"200": {Description: "Pinned Natural Earth Admin-0 GeoJSON", Content: map[string]*huma.MediaType{
			"application/geo+json": {Schema: &huma.Schema{Type: "object"}},
		}},
		"304": {Description: "Not Modified"},
	}
	huma.Register(s.api, publicRead(huma.Operation{
		OperationID: "get-admin0-map", Method: http.MethodGet, Path: Admin0MapPath,
		Summary:   "Get the pinned Natural Earth Admin-0 50m geometry",
		Responses: mapResponses,
		Errors:    []int{http.StatusNotFound, http.StatusServiceUnavailable},
	}), func(ctx context.Context, in *snapshotInput) (*streamOutput, error) {
		if s.opts.Geographies == nil {
			return nil, s.fail("get-admin0-map", ErrUnavailable)
		}
		export, err := s.opts.Geographies.Admin0Map(ctx, in.Snapshot)
		if err != nil {
			return nil, s.fail("get-admin0-map", err)
		}
		if err := validateMeta(export.Meta, in.Snapshot); err != nil {
			return nil, s.fail("get-admin0-map", err)
		}
		etag := normalizeETag(export.ETag)
		if etag == "" {
			return nil, s.fail("get-admin0-map", errors.New("map export is missing an ETag"))
		}
		status := http.StatusOK
		if etagMatches(in.IfNoneMatch, etag) {
			status = http.StatusNotModified
		}
		out := streamHeaders(export.Meta, in.Snapshot, etag)
		out.Status = status
		out.ContentType = "application/geo+json"
		out.Body = func(hctx huma.Context) {
			hctx.SetStatus(status)
			if status == http.StatusOK {
				_, _ = hctx.BodyWriter().Write(export.Body)
			}
		}
		return out, nil
	})

	huma.Register(s.api, publicRead(huma.Operation{
		OperationID: "list-observations", Method: http.MethodGet, Path: ObservationsPath,
		Summary:     "Get normalized observations using exact year filters",
		Description: "Years are exact. The service never interpolates or substitutes a nearby year.",
		Responses:   etagResponses(), Errors: []int{http.StatusBadRequest, http.StatusNotFound, http.StatusServiceUnavailable},
	}), func(ctx context.Context, in *observationsInput) (*observationOutput, error) {
		if err := validateYears(in.Years); err != nil {
			return nil, huma.Error400BadRequest(err.Error())
		}
		if s.opts.Observations == nil {
			return nil, s.fail("list-observations", ErrUnavailable)
		}
		result, err := s.opts.Observations.Observations(ctx, observationQuery(in))
		if err != nil {
			return nil, s.fail("list-observations", err)
		}
		h, status, err := cacheResponse(result.Meta, in.Snapshot, in.IfNoneMatch, result)
		if err != nil {
			return nil, s.fail("list-observations", err)
		}
		out := &observationOutput{Status: status, ETag: h.ETag, CacheControl: h.CacheControl, Snapshot: h.Snapshot, Releases: h.Releases}
		if status != http.StatusNotModified {
			out.Body = &result
		}
		return out, nil
	})

	csvResponses := map[string]*huma.Response{
		"200": {Description: "Streamed observations CSV with provenance columns", Content: map[string]*huma.MediaType{
			"text/csv": {Schema: &huma.Schema{Type: "string", Format: "binary"}},
		}},
		"304": {Description: "Not Modified"},
	}
	huma.Register(s.api, publicRead(huma.Operation{
		OperationID: "download-observations-csv", Method: http.MethodGet, Path: ObservationsCSVPath,
		Summary:     "Stream all matching normalized observations as CSV",
		Description: "This endpoint is intentionally unpaginated and streams from the repository.",
		Responses:   csvResponses,
		Errors:      []int{http.StatusBadRequest, http.StatusNotFound, http.StatusServiceUnavailable},
	}), func(ctx context.Context, in *observationsCSVInput) (*streamOutput, error) {
		if err := validateYears(in.Years); err != nil {
			return nil, huma.Error400BadRequest(err.Error())
		}
		if s.opts.Observations == nil {
			return nil, s.fail("download-observations-csv", ErrUnavailable)
		}
		export, err := s.opts.Observations.ObservationsCSV(ctx, ObservationQuery{
			Series: in.Series, Years: in.Years, Geographies: in.Geographies,
			Groups: in.Groups, Snapshot: in.Snapshot,
		})
		if err != nil {
			return nil, s.fail("download-observations-csv", err)
		}
		if err := validateMeta(export.Meta, in.Snapshot); err != nil {
			return nil, s.fail("download-observations-csv", err)
		}
		if export.Write == nil {
			return nil, s.fail("download-observations-csv", errors.New("CSV export has no writer"))
		}
		etag := normalizeETag(export.ETag)
		if etag == "" {
			return nil, s.fail("download-observations-csv", errors.New("CSV export is missing an ETag"))
		}
		status := http.StatusOK
		if etagMatches(in.IfNoneMatch, etag) {
			status = http.StatusNotModified
		}
		out := streamHeaders(export.Meta, in.Snapshot, etag)
		out.Status = status
		out.ContentType = "text/csv; charset=utf-8"
		filename := path.Base(export.Filename)
		if filename == "." || filename == "/" || filename == "" {
			filename = "context-atlas-observations.csv"
		}
		out.ContentDisposition = mime.FormatMediaType("attachment", map[string]string{"filename": filename})
		out.Body = func(hctx huma.Context) {
			hctx.SetStatus(status)
			if status != http.StatusOK {
				return
			}
			if err := export.Write(ctx, hctx.BodyWriter()); err != nil && !errors.Is(err, context.Canceled) {
				s.opts.Logger.Error("CSV stream failed", "operation", "download-observations-csv", "error", err)
			}
		}
		return out, nil
	})

	type associationInput struct {
		XSeries     string   `query:"x_series" required:"true"`
		XYear       int      `query:"x_year" required:"true" minimum:"1" maximum:"9999"`
		YSeries     string   `query:"y_series" required:"true"`
		YYear       int      `query:"y_year" required:"true" minimum:"1" maximum:"9999"`
		Geographies []string `query:"geographies"`
		Groups      []string `query:"groups"`
		Snapshot    string   `query:"snapshot"`
		IfNoneMatch string   `header:"If-None-Match"`
	}
	huma.Register(s.api, publicRead(huma.Operation{
		OperationID: "explore-association", Method: http.MethodGet, Path: AssociationPath,
		Summary:     "Explore an exact-year association between two series",
		Description: "X and Y use independently selected exact years. Pearson r is absent for n < 3 or zero variance; this endpoint makes no causal claim.",
		Responses:   etagResponses(), Errors: []int{http.StatusNotFound, http.StatusServiceUnavailable},
	}), func(ctx context.Context, in *associationInput) (*associationOutput, error) {
		if s.opts.Associations == nil {
			return nil, s.fail("explore-association", ErrUnavailable)
		}
		result, err := s.opts.Associations.Association(ctx, AssociationQuery{
			XSeries: in.XSeries, XYear: in.XYear, YSeries: in.YSeries, YYear: in.YYear,
			Geographies: in.Geographies, Groups: in.Groups, Snapshot: in.Snapshot,
		})
		if err != nil {
			return nil, s.fail("explore-association", err)
		}
		h, status, err := cacheResponse(result.Meta, in.Snapshot, in.IfNoneMatch, result)
		if err != nil {
			return nil, s.fail("explore-association", err)
		}
		out := &associationOutput{Status: status, ETag: h.ETag, CacheControl: h.CacheControl, Snapshot: h.Snapshot, Releases: h.Releases}
		if status != http.StatusNotModified {
			out.Body = &result
		}
		return out, nil
	})

	type feedbackInput struct{ Body FeedbackRequest }
	type feedbackOutput struct {
		Status int
		Body   FeedbackReceipt
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "submit-feedback", Method: http.MethodPost, Path: FeedbackPath,
		Summary: "Submit public feedback", DefaultStatus: http.StatusAccepted,
		MaxBodyBytes: 8 << 10, Security: []map[string][]string{},
		Errors: []int{http.StatusServiceUnavailable},
	}, func(ctx context.Context, in *feedbackInput) (*feedbackOutput, error) {
		if err := validatePageURL(in.Body.PageURL); err != nil {
			return nil, huma.Error400BadRequest(err.Error())
		}
		if s.opts.Feedback == nil {
			return nil, s.fail("submit-feedback", ErrUnavailable)
		}
		receipt, err := s.opts.Feedback.SubmitFeedback(ctx, in.Body)
		if err != nil {
			return nil, s.fail("submit-feedback", err)
		}
		return &feedbackOutput{Status: http.StatusAccepted, Body: receipt}, nil
	})

	type healthOutput struct {
		Status int
		Body   HealthReport
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "get-health", Method: http.MethodGet, Path: HealthPath,
		Summary: "Check service and required dependencies", Security: []map[string][]string{},
		Errors: []int{http.StatusServiceUnavailable},
	}, func(ctx context.Context, _ *struct{}) (*healthOutput, error) {
		if s.opts.Health == nil {
			return &healthOutput{Status: http.StatusServiceUnavailable, Body: HealthReport{Status: "degraded"}}, nil
		}
		report, err := s.opts.Health.Health(ctx)
		if err != nil {
			s.opts.Logger.Error("health check failed", "error", err)
			return &healthOutput{Status: http.StatusServiceUnavailable, Body: HealthReport{Status: "degraded"}}, nil
		}
		status := http.StatusOK
		for _, dep := range report.Dependencies {
			if dep.Required && !dep.Ready {
				status = http.StatusServiceUnavailable
				report.Status = "degraded"
			}
		}
		if report.Status != "ok" {
			status = http.StatusServiceUnavailable
		}
		return &healthOutput{Status: status, Body: report}, nil
	})
}

func (s *Server) registerAdmin() {
	admin := func(op huma.Operation) huma.Operation {
		op.Security = []map[string][]string{{"ownerSession": {}}}
		op.Middlewares = append(op.Middlewares, httpMiddleware(s.opts.AdminAuth))
		return op
	}

	type previewCreateInput struct {
		Body struct {
			URL string `json:"url" format:"uri" maxLength:"2048"`
		}
	}
	type previewOutput struct{ Body ImportPreview }
	huma.Register(s.api, admin(huma.Operation{
		OperationID: "create-import-preview", Method: http.MethodPost, Path: AdminPreviewsPath,
		Summary: "Stage and inspect a WHO indicator import", DefaultStatus: http.StatusAccepted,
		MaxBodyBytes: 8 << 10,
		Errors:       []int{http.StatusBadRequest, http.StatusForbidden, http.StatusConflict, http.StatusServiceUnavailable},
	}), func(ctx context.Context, in *previewCreateInput) (*previewOutput, error) {
		if err := validateWHOIndicatorURL(in.Body.URL); err != nil {
			return nil, huma.Error400BadRequest(err.Error())
		}
		if s.opts.Imports == nil {
			return nil, s.fail("create-import-preview", ErrUnavailable)
		}
		preview, err := s.opts.Imports.CreatePreview(ctx, in.Body.URL)
		if err != nil {
			return nil, s.fail("create-import-preview", err)
		}
		return &previewOutput{Body: preview}, nil
	})

	type previewInput struct {
		PreviewID string `path:"preview_id"`
	}
	huma.Register(s.api, admin(huma.Operation{
		OperationID: "get-import-preview", Method: http.MethodGet, Path: AdminPreviewPath,
		Summary: "Get a persisted import preview",
		Errors:  []int{http.StatusForbidden, http.StatusNotFound, http.StatusServiceUnavailable},
	}), func(ctx context.Context, in *previewInput) (*previewOutput, error) {
		if s.opts.Imports == nil {
			return nil, s.fail("get-import-preview", ErrUnavailable)
		}
		preview, err := s.opts.Imports.Preview(ctx, in.PreviewID)
		if err != nil {
			return nil, s.fail("get-import-preview", err)
		}
		return &previewOutput{Body: preview}, nil
	})

	type runOutput struct {
		Status int
		Body   ImportRun
	}
	huma.Register(s.api, admin(huma.Operation{
		OperationID: "confirm-import-preview", Method: http.MethodPost, Path: AdminConfirmPath,
		Summary: "Confirm a preview and atomically publish a snapshot", DefaultStatus: http.StatusAccepted,
		Errors: []int{http.StatusForbidden, http.StatusNotFound, http.StatusConflict, http.StatusServiceUnavailable},
	}), func(ctx context.Context, in *previewInput) (*runOutput, error) {
		if s.opts.Imports == nil {
			return nil, s.fail("confirm-import-preview", ErrUnavailable)
		}
		run, err := s.opts.Imports.ConfirmPreview(ctx, in.PreviewID)
		if err != nil {
			return nil, s.fail("confirm-import-preview", err)
		}
		return &runOutput{Status: http.StatusAccepted, Body: run}, nil
	})

	huma.Register(s.api, admin(huma.Operation{
		OperationID: "refresh-catalog", Method: http.MethodPost, Path: AdminRefreshPath,
		Summary: "Start the owner-requested WHO catalog refresh", DefaultStatus: http.StatusAccepted,
		Errors: []int{http.StatusForbidden, http.StatusConflict, http.StatusServiceUnavailable},
	}), func(ctx context.Context, _ *struct{}) (*runOutput, error) {
		if s.opts.Imports == nil {
			return nil, s.fail("refresh-catalog", ErrUnavailable)
		}
		run, err := s.opts.Imports.Refresh(ctx)
		if err != nil {
			return nil, s.fail("refresh-catalog", err)
		}
		return &runOutput{Status: http.StatusAccepted, Body: run}, nil
	})

	type runsInput struct {
		Page     int `query:"page" default:"1" minimum:"1"`
		PageSize int `query:"page_size" default:"25" enum:"25,50,100"`
	}
	type runsOutput struct{ Body ImportRunResult }
	huma.Register(s.api, admin(huma.Operation{
		OperationID: "list-import-runs", Method: http.MethodGet, Path: AdminRunsPath,
		Summary: "List import and refresh jobs",
		Errors:  []int{http.StatusForbidden, http.StatusServiceUnavailable},
	}), func(ctx context.Context, in *runsInput) (*runsOutput, error) {
		if s.opts.Imports == nil {
			return nil, s.fail("list-import-runs", ErrUnavailable)
		}
		result, err := s.opts.Imports.ImportRuns(ctx, in.Page, in.PageSize)
		if err != nil {
			return nil, s.fail("list-import-runs", err)
		}
		return &runsOutput{Body: result}, nil
	})

	type freshnessOutput struct{ Body FreshnessResult }
	huma.Register(s.api, admin(huma.Operation{
		OperationID: "get-import-freshness", Method: http.MethodGet, Path: AdminFreshnessPath,
		Summary: "Get per-dataset refresh freshness",
		Errors:  []int{http.StatusForbidden, http.StatusServiceUnavailable},
	}), func(ctx context.Context, _ *struct{}) (*freshnessOutput, error) {
		if s.opts.Imports == nil {
			return nil, s.fail("get-import-freshness", ErrUnavailable)
		}
		result, err := s.opts.Imports.Freshness(ctx)
		if err != nil {
			return nil, s.fail("get-import-freshness", err)
		}
		return &freshnessOutput{Body: result}, nil
	})

	type sessionOutput struct{ Body AdminSession }
	huma.Register(s.api, admin(huma.Operation{
		OperationID: "get-admin-session", Method: http.MethodGet, Path: AdminSessionPath,
		Summary: "Get the current owner session",
		Errors:  []int{http.StatusForbidden, http.StatusServiceUnavailable},
	}), func(ctx context.Context, _ *struct{}) (*sessionOutput, error) {
		if s.opts.Sessions == nil {
			return nil, s.fail("get-admin-session", ErrUnavailable)
		}
		session, err := s.opts.Sessions.Session(ctx)
		if err != nil {
			return nil, s.fail("get-admin-session", err)
		}
		return &sessionOutput{Body: session}, nil
	})

	type logoutOutput struct {
		Status    int
		SetCookie http.Cookie `header:"Set-Cookie"`
	}
	huma.Register(s.api, admin(huma.Operation{
		OperationID: "delete-admin-session", Method: http.MethodDelete, Path: AdminSessionPath,
		Summary: "End the current owner session", DefaultStatus: http.StatusNoContent,
		Errors: []int{http.StatusForbidden, http.StatusServiceUnavailable},
	}), func(ctx context.Context, _ *struct{}) (*logoutOutput, error) {
		if s.opts.Sessions == nil {
			return nil, s.fail("delete-admin-session", ErrUnavailable)
		}
		cookie, err := s.opts.Sessions.Logout(ctx)
		if err != nil {
			return nil, s.fail("delete-admin-session", err)
		}
		return &logoutOutput{Status: http.StatusNoContent, SetCookie: cookie}, nil
	})
}

func publicRead(op huma.Operation) huma.Operation {
	op.Security = []map[string][]string{}
	op.Middlewares = append(op.Middlewares, func(ctx huma.Context, next func(huma.Context)) {
		ctx.SetHeader("Access-Control-Allow-Origin", "*")
		next(ctx)
	})
	return op
}

func etagResponses() map[string]*huma.Response {
	return map[string]*huma.Response{"304": {Description: "Not Modified"}}
}

func httpMiddleware(m Middleware) func(huma.Context, func(huma.Context)) {
	return func(ctx huma.Context, next func(huma.Context)) {
		r, w := humago.Unwrap(ctx)
		m(http.HandlerFunc(func(nextW http.ResponseWriter, nextR *http.Request) {
			next(humago.NewContext(ctx.Operation(), nextR, nextW))
		})).ServeHTTP(w, r)
	}
}

func denyAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"type":"about:blank","title":"Unauthorized","status":401,"detail":"owner session required"}`))
	})
}

func observationQuery(in *observationsInput) ObservationQuery {
	return ObservationQuery{
		Series: in.Series, Years: in.Years, Geographies: in.Geographies, Groups: in.Groups,
		Snapshot: in.Snapshot, Page: in.Page, PageSize: in.PageSize,
	}
}

func validateYears(years []int) error {
	for _, year := range years {
		if year < 1 || year > 9999 {
			return fmt.Errorf("year %d is outside the supported calendar-year range 1–9999", year)
		}
	}
	return nil
}

func validateWHOIndicatorURL(raw string) error {
	_, err := who.ValidateIndicatorPageURL(raw)
	return err
}

func validatePageURL(raw string) error {
	if raw == "" {
		return nil
	}
	if strings.ContainsAny(raw, "\r\n") {
		return errors.New("page_url must be a relative atlas path or absolute HTTP(S) URL")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return errors.New("page_url must be a relative atlas path or absolute HTTP(S) URL")
	}
	if !u.IsAbs() {
		if strings.HasPrefix(raw, "/") && !strings.HasPrefix(raw, "//") {
			return nil
		}
		return errors.New("page_url must be a relative atlas path or absolute HTTP(S) URL")
	}
	if (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil {
		return errors.New("page_url must be a relative atlas path or absolute HTTP(S) URL")
	}
	return nil
}

func validateMeta(meta ResponseMeta, requested string) error {
	if meta.Snapshot.ID == "" {
		return errors.New("repository response is missing resolved snapshot metadata")
	}
	if requested != "" && meta.Snapshot.ID != requested {
		return fmt.Errorf("repository resolved snapshot %q for pinned snapshot %q", meta.Snapshot.ID, requested)
	}
	for _, release := range meta.Releases {
		if release.ID == "" || release.SourceURL == "" || release.Citation == "" || release.AccessedAt.IsZero() {
			return fmt.Errorf("release %q has incomplete source metadata", release.ID)
		}
	}
	return nil
}

func cacheResponse[T any](meta ResponseMeta, requested, ifNoneMatch string, body T) (responseHeaders, int, error) {
	if err := validateMeta(meta, requested); err != nil {
		return responseHeaders{}, 0, err
	}
	b, err := json.Marshal(body)
	if err != nil {
		return responseHeaders{}, 0, fmt.Errorf("hash response: %w", err)
	}
	sum := sha256.Sum256(b)
	etag := `"sha256-` + hex.EncodeToString(sum[:]) + `"`
	headers := responseHeaders{
		ETag: etag, CacheControl: cacheControl(requested), Snapshot: meta.Snapshot.ID,
		Releases: releaseIDs(meta.Releases),
	}
	if etagMatches(ifNoneMatch, etag) {
		return headers, http.StatusNotModified, nil
	}
	return headers, http.StatusOK, nil
}

func streamHeaders(meta ResponseMeta, requested, etag string) *streamOutput {
	return &streamOutput{
		ETag: etag, CacheControl: cacheControl(requested), Snapshot: meta.Snapshot.ID,
		Releases: releaseIDs(meta.Releases),
	}
}

func cacheControl(requested string) string {
	if requested != "" {
		return "public, max-age=31536000, immutable"
	}
	return "public, max-age=60, stale-while-revalidate=300"
}

func releaseIDs(releases []DatasetReleaseRef) string {
	ids := make([]string, 0, len(releases))
	for _, release := range releases {
		ids = append(ids, release.ID)
	}
	return strings.Join(ids, ",")
}

func normalizeETag(etag string) string {
	etag = strings.TrimSpace(etag)
	if etag == "" {
		return ""
	}
	if strings.HasPrefix(etag, `"`) || strings.HasPrefix(etag, `W/"`) {
		return etag
	}
	return `"` + strings.Trim(etag, `"`) + `"`
}

func etagMatches(header, current string) bool {
	current = strings.TrimPrefix(strings.TrimSpace(current), "W/")
	for value := range strings.SplitSeq(header, ",") {
		value = strings.TrimSpace(value)
		if value == "*" || strings.TrimPrefix(value, "W/") == current {
			return true
		}
	}
	return false
}

func (s *Server) fail(operation string, err error) error {
	switch {
	case errors.Is(err, ErrNotFound):
		return huma.Error404NotFound("resource not found")
	case errors.Is(err, ErrConflict):
		return huma.Error409Conflict("request conflicts with current state")
	case errors.Is(err, ErrForbidden):
		return huma.Error403Forbidden("owner authorization required")
	case errors.Is(err, ErrUnavailable):
		return huma.NewError(http.StatusServiceUnavailable, "service unavailable")
	default:
		s.opts.Logger.Error("API operation failed", "operation", operation, "error", err)
		return huma.Error500InternalServerError("unexpected error occurred")
	}
}
