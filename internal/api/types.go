package api

import "time"

// SnapshotRef identifies the immutable catalog view used to answer a request.
type SnapshotRef struct {
	ID                  string    `json:"id" doc:"Immutable catalog snapshot ID"`
	CreatedAt           time.Time `json:"created_at"`
	M49ReferenceRelease string    `json:"m49_reference_release"`
}

type DatasetReleaseRef struct {
	ID            string    `json:"id"`
	DatasetID     string    `json:"dataset_id"`
	SHA256        string    `json:"sha256"`
	SourceURL     string    `json:"source_url" format:"uri"`
	AccessedAt    time.Time `json:"accessed_at"`
	Citation      string    `json:"citation"`
	ParserVersion string    `json:"parser_version"`
}

// ResponseMeta makes a response reproducible and carries its source attribution.
type ResponseMeta struct {
	Snapshot SnapshotRef         `json:"snapshot"`
	Releases []DatasetReleaseRef `json:"releases"`
}

type Dataset struct {
	ID            string            `json:"id"`
	Name          string            `json:"name"`
	Description   string            `json:"description,omitempty"`
	WHOIdentifier string            `json:"who_identifier"`
	WHOCode       string            `json:"who_code"`
	SourceURL     string            `json:"source_url" format:"uri"`
	Citation      string            `json:"citation"`
	Capabilities  []string          `json:"capabilities"`
	Release       DatasetReleaseRef `json:"release"`
}

type Measure struct {
	ID          string `json:"id"`
	DatasetID   string `json:"dataset_id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Unit        string `json:"unit"`
	Statistic   string `json:"statistic"`
	ValueKind   string `json:"value_kind" enum:"number,category,composition"`
}

type DimensionDefinition struct {
	Code   string   `json:"code"`
	Name   string   `json:"name"`
	Values []string `json:"values"`
}

// Series is one measure plus its complete canonical dimension tuple.
type Series struct {
	ID             string            `json:"id"`
	DatasetID      string            `json:"dataset_id"`
	MeasureID      string            `json:"measure_id"`
	Name           string            `json:"name"`
	Unit           string            `json:"unit"`
	Statistic      string            `json:"statistic"`
	ValueKind      string            `json:"value_kind" enum:"number,category,composition"`
	Dimensions     map[string]string `json:"dimensions"`
	AvailableYears []int             `json:"available_years"`
}

type CatalogResult struct {
	Meta       ResponseMeta          `json:"meta"`
	Datasets   []Dataset             `json:"datasets"`
	Measures   []Measure             `json:"measures"`
	Series     []Series              `json:"series"`
	Dimensions []DimensionDefinition `json:"dimensions"`
}

// Geography retains the source identity even when no canonical M49 match exists.
type Geography struct {
	SourceCode string `json:"source_code"`
	Name       string `json:"name"`
	Kind       string `json:"kind"`
	M49        string `json:"m49,omitempty" pattern:"^[0-9]{3}$"`
	ISO2       string `json:"iso2,omitempty"`
	ISO3       string `json:"iso3,omitempty"`
	Mapped     bool   `json:"mapped"`
	Leaf       bool   `json:"leaf"`
}

type GeographyResult struct {
	Meta        ResponseMeta `json:"meta"`
	Geographies []Geography  `json:"geographies"`
}

type GroupMembership struct {
	M49  string `json:"m49" pattern:"^[0-9]{3}$"`
	Name string `json:"name"`
}

type Group struct {
	ID                    string            `json:"id"`
	Name                  string            `json:"name"`
	Kind                  string            `json:"kind" enum:"world,region,subregion,intermediate_region,ldc,lldc,sids,custom"`
	ParentID              string            `json:"parent_id,omitempty"`
	ClassificationVersion string            `json:"classification_version"`
	Members               []GroupMembership `json:"members"`
}

type GroupResult struct {
	Meta   ResponseMeta `json:"meta"`
	Groups []Group      `json:"groups"`
}

type Observation struct {
	SeriesID        string    `json:"series_id"`
	ReleaseID       string    `json:"release_id"`
	SourceGeography Geography `json:"source_geography"`
	Year            int       `json:"year" minimum:"1" maximum:"9999"`
	RawValue        string    `json:"raw_value"`
	DisplayValue    string    `json:"display_value"`
	NumericValue    *float64  `json:"numeric_value,omitempty"`
	LowerBound      *float64  `json:"lower_bound,omitempty"`
	UpperBound      *float64  `json:"upper_bound,omitempty"`
	Status          string    `json:"status" enum:"numeric,missing,suppressed,not_applicable"`
	PublishState    string    `json:"publish_state" doc:"Source publication state, such as PUBLISHED"`
	SourceRowKey    string    `json:"source_row_key"`
}

type Pagination struct {
	Page     int   `json:"page" minimum:"1"`
	PageSize int   `json:"page_size" enum:"25,50,100,500"`
	Total    int64 `json:"total" minimum:"0"`
}

type ObservationResult struct {
	Meta         ResponseMeta  `json:"meta"`
	Pagination   Pagination    `json:"pagination"`
	Observations []Observation `json:"observations"`
}

type AssociationPoint struct {
	M49       string  `json:"m49" pattern:"^[0-9]{3}$"`
	Geography string  `json:"geography"`
	X         float64 `json:"x"`
	Y         float64 `json:"y"`
}

type AssociationCoverage struct {
	SelectedUniverse int `json:"selected_universe" minimum:"0"`
	XOnlyMissing     int `json:"x_only_missing" minimum:"0"`
	YOnlyMissing     int `json:"y_only_missing" minimum:"0"`
	BothMissing      int `json:"both_missing" minimum:"0"`
	Paired           int `json:"paired" minimum:"0"`
}

type AssociationResult struct {
	Meta      ResponseMeta        `json:"meta"`
	XSeriesID string              `json:"x_series"`
	XYear     int                 `json:"x_year"`
	YSeriesID string              `json:"y_series"`
	YYear     int                 `json:"y_year"`
	Coverage  AssociationCoverage `json:"coverage"`
	PearsonR  *float64            `json:"pearson_r,omitempty"`
	Points    []AssociationPoint  `json:"points"`
	Warnings  []string            `json:"warnings"`
}

type RowAccounting struct {
	SourceRows          int64 `json:"source_rows" minimum:"0"`
	AcceptedRows        int64 `json:"accepted_rows" minimum:"0"`
	CollapsedDuplicates int64 `json:"collapsed_duplicates" minimum:"0"`
	RejectedRows        int64 `json:"rejected_rows" minimum:"0"`
}

type ImportPreview struct {
	ID                    string                `json:"id"`
	Status                string                `json:"status" enum:"pending,running,ready,failed,expired,confirmed"`
	IndicatorURL          string                `json:"indicator_url" format:"uri"`
	DownloadURL           string                `json:"download_url,omitempty" format:"uri"`
	SchemaFingerprint     string                `json:"schema_fingerprint,omitempty"`
	Headers               []string              `json:"headers"`
	Measures              []Measure             `json:"measures"`
	Units                 []string              `json:"units"`
	Dimensions            []DimensionDefinition `json:"dimensions"`
	Rows                  RowAccounting         `json:"rows"`
	UnmappedGeographies   []Geography           `json:"unmapped_geographies"`
	ExactDuplicates       int64                 `json:"exact_duplicates" minimum:"0"`
	ConflictingDuplicates int64                 `json:"conflicting_duplicates" minimum:"0"`
	Warnings              []string              `json:"warnings"`
	CreatedAt             time.Time             `json:"created_at"`
	ExpiresAt             time.Time             `json:"expires_at"`
}

type ImportRun struct {
	ID         string        `json:"id"`
	Kind       string        `json:"kind" enum:"preview,confirm,manual_refresh,scheduled_refresh,startup_catchup"`
	Status     string        `json:"status" enum:"pending,running,succeeded,failed,interrupted,unchanged"`
	DatasetID  string        `json:"dataset_id,omitempty"`
	PreviewID  string        `json:"preview_id,omitempty"`
	Snapshot   *SnapshotRef  `json:"snapshot,omitempty"`
	Rows       RowAccounting `json:"rows"`
	Error      string        `json:"error,omitempty"`
	StartedAt  time.Time     `json:"started_at"`
	FinishedAt *time.Time    `json:"finished_at,omitempty"`
}

type ImportRunResult struct {
	Pagination Pagination  `json:"pagination"`
	Runs       []ImportRun `json:"runs"`
}

type DatasetFreshness struct {
	DatasetID        string     `json:"dataset_id"`
	LastSuccessAt    *time.Time `json:"last_success_at,omitempty"`
	LastAttemptAt    *time.Time `json:"last_attempt_at,omitempty"`
	LastAttemptState string     `json:"last_attempt_state"`
	Stale            bool       `json:"stale"`
}

type FreshnessResult struct {
	Datasets []DatasetFreshness `json:"datasets"`
}

type AdminSession struct {
	OwnerTelegramID int64     `json:"owner_telegram_id"`
	ExpiresAt       time.Time `json:"expires_at"`
	CSRFToken       string    `json:"csrf_token"`
}

type FeedbackRequest struct {
	Message string `json:"message" minLength:"1" maxLength:"4000"`
	PageURL string `json:"page_url,omitempty" maxLength:"2048" doc:"Relative atlas path or absolute HTTP(S) URL"`
}

type FeedbackReceipt struct {
	ID         string    `json:"id"`
	ReceivedAt time.Time `json:"received_at"`
}

type DependencyHealth struct {
	Name     string `json:"name"`
	Ready    bool   `json:"ready"`
	Required bool   `json:"required"`
	Detail   string `json:"detail,omitempty"`
}

type HealthReport struct {
	Status       string             `json:"status" enum:"ok,degraded"`
	Dependencies []DependencyHealth `json:"dependencies"`
}
