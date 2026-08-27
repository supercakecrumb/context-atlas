package api

import (
	"context"
	"errors"
	"io"
	"net/http"
)

var (
	ErrNotFound    = errors.New("not found")
	ErrConflict    = errors.New("conflict")
	ErrUnavailable = errors.New("unavailable")
	ErrForbidden   = errors.New("forbidden")
)

type CatalogReader interface {
	Catalog(context.Context, string) (CatalogResult, error)
}

type GeographyQuery struct {
	Snapshot string
	Search   string
}

type GeographyReader interface {
	Geographies(context.Context, GeographyQuery) (GeographyResult, error)
	Groups(context.Context, string) (GroupResult, error)
	Admin0Map(context.Context, string) (GeoJSONExport, error)
}

type ObservationQuery struct {
	Series      []string
	Years       []int
	Geographies []string
	Groups      []string
	Snapshot    string
	Page        int
	PageSize    int
}

type CSVExport struct {
	Meta     ResponseMeta
	Filename string
	ETag     string
	Write    func(context.Context, io.Writer) error
}

type ObservationReader interface {
	Observations(context.Context, ObservationQuery) (ObservationResult, error)
	ObservationsCSV(context.Context, ObservationQuery) (CSVExport, error)
}

type AssociationQuery struct {
	XSeries     string
	XYear       int
	YSeries     string
	YYear       int
	Geographies []string
	Groups      []string
	Snapshot    string
}

type AssociationReader interface {
	Association(context.Context, AssociationQuery) (AssociationResult, error)
}

type GeoJSONExport struct {
	Meta ResponseMeta
	ETag string
	Body []byte
}

type FeedbackService interface {
	SubmitFeedback(context.Context, FeedbackRequest) (FeedbackReceipt, error)
}

type HealthChecker interface {
	Health(context.Context) (HealthReport, error)
}

type ImportAdmin interface {
	CreatePreview(context.Context, string) (ImportPreview, error)
	Preview(context.Context, string) (ImportPreview, error)
	ConfirmPreview(context.Context, string) (ImportRun, error)
	Refresh(context.Context) (ImportRun, error)
	ImportRuns(context.Context, int, int) (ImportRunResult, error)
	Freshness(context.Context) (FreshnessResult, error)
}

type SessionAdmin interface {
	Session(context.Context) (AdminSession, error)
	Logout(context.Context) (http.Cookie, error)
}

type Middleware func(http.Handler) http.Handler
