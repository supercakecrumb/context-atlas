package atlas

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/supercakecrumb/context-atlas/internal/api"
	"github.com/supercakecrumb/context-atlas/internal/who"
	"github.com/supercakecrumb/context-atlas/internal/worldbank"
)

func awaitPreview(t *testing.T, service *Service, ctx context.Context, id string) api.ImportPreview {
	t.Helper()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		preview, err := service.Preview(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if preview.Status == "ready" {
			return preview
		}
		if preview.Status == "failed" || preview.Status == "expired" {
			t.Fatalf("preview %s: %v", preview.Status, preview.Warnings)
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-ticker.C:
		}
	}
}

func TestIntegrationWorldBankConfirmationProvenanceAndRefresh(t *testing.T) {
	service, ctx := integrationAtlas(t)
	raw, err := os.ReadFile("../worldbank/testdata/gini.zip")
	if err != nil {
		t.Fatal(err)
	}
	service.wdiFetch = func(context.Context, who.DatasetDefinition) (worldbank.Artifact, error) {
		return worldbank.Artifact{Bytes: raw, AccessedAt: time.Now().UTC()}, nil
	}
	definition, _ := worldbank.DefinitionForURL("https://data.worldbank.org/indicator/SI.POV.GINI")
	created, err := service.CreatePreview(ctx, definition.PageURL)
	if err != nil {
		t.Fatal(err)
	}
	preview := awaitPreview(t, service, ctx, created.ID)
	if preview.Rows.SourceRows != 5 || preview.Rows.AcceptedRows != 5 {
		t.Fatalf("preview accounting: %+v", preview.Rows)
	}
	run, err := service.ConfirmPreview(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Snapshot == nil || run.Rows.AcceptedRows != 5 {
		t.Fatalf("confirmation: %+v", run)
	}
	catalog, err := service.Catalog(ctx, run.Snapshot.ID)
	if err != nil {
		t.Fatal(err)
	}
	dataset := catalog.Datasets[0]
	if dataset.SourceSystem != worldbank.Source || dataset.WHOIdentifier != "" || dataset.WHOCode != "" || dataset.IndicatorCode != "SI.POV.GINI" || dataset.Description == "" {
		t.Fatalf("incorrect dataset identity: %+v", dataset)
	}
	if dataset.Release.Producer != "Fixture statistical producer" || dataset.Release.SourceNote == "" || dataset.Release.LicenseURL != worldbank.LicenseURL || dataset.Release.ParserVersion != worldbank.ParserVersion || strings.Contains(dataset.Citation, "World Health") {
		t.Fatalf("incorrect provenance: %+v", dataset.Release)
	}
	var original []byte
	var metadata []byte
	if err := service.pool.QueryRow(ctx, `SELECT raw_artifact, response_metadata FROM dataset_release WHERE dataset_id = $1`, definition.ID).Scan(&original, &metadata); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(original, raw) || !bytes.Contains(metadata, []byte("application/zip")) {
		t.Fatal("lost original artifact or content type")
	}
	observations, err := service.Observations(ctx, api.ObservationQuery{Snapshot: run.Snapshot.ID, Series: []string{catalog.Series[0].ID}, Years: []int{2024}, Geographies: []string{"620"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(observations.Observations) != 1 || observations.Observations[0].NumericValue != nil || observations.Observations[0].Year != 2024 {
		t.Fatalf("exact year/null changed: %+v", observations)
	}
	export, err := service.ObservationsCSV(ctx, api.ObservationQuery{Snapshot: run.Snapshot.ID, Series: []string{catalog.Series[0].ID}})
	if err != nil {
		t.Fatal(err)
	}
	var csv bytes.Buffer
	if err := export.Write(ctx, &csv); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(csv.String(), "Income survey fixture") || !strings.Contains(csv.String(), "World Bank") || !strings.Contains(csv.String(), "source_status") {
		t.Fatal("CSV lost notes/provider/status")
	}
	_, changed, err := service.fetchRefreshCandidate(ctx, definition, "manual_refresh")
	if err != nil || changed {
		t.Fatalf("unchanged artifact made a new candidate: %v, %v", changed, err)
	}
	var releases, snapshots int
	if err := service.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM dataset_release), (SELECT count(*) FROM catalog_snapshot)`).Scan(&releases, &snapshots); err != nil {
		t.Fatal(err)
	}
	if releases != 1 || snapshots != 1 {
		t.Fatalf("refresh duplicated releases/snapshots: %d/%d", releases, snapshots)
	}
	// Confirming again cannot republish or retain a staged artifact.
	if _, err := service.ConfirmPreview(ctx, created.ID); err == nil {
		t.Fatal("confirmed twice")
	}
	jobID, _ := strconv.ParseInt(created.ID, 10, 64)
	job, err := service.getJob(ctx, jobID)
	if err != nil {
		t.Fatal(err)
	}
	if job.StagedRawArtifact != nil {
		t.Fatal("confirmed job kept staged bytes")
	}
}

// Opt-in, reproducible live verification. The isolated test database retains
// the five real releases afterward for a local preview; never use production.
func TestIntegrationWorldBankLive(t *testing.T) {
	if os.Getenv("ATLAS_WDI_LIVE") != "1" {
		t.Skip("ATLAS_WDI_LIVE is not enabled")
	}
	service, _ := integrationAtlas(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	for _, definition := range worldbank.Definitions() {
		created, err := service.CreatePreview(ctx, definition.PageURL)
		if err != nil {
			t.Fatal(err)
		}
		preview := awaitPreview(t, service, ctx, created.ID)
		run, err := service.ConfirmPreview(ctx, created.ID)
		if err != nil {
			t.Fatal(err)
		}
		if run.Rows.SourceRows != run.Rows.AcceptedRows+run.Rows.CollapsedDuplicates || run.Rows.RejectedRows != 0 {
			t.Fatalf("unaccounted rows: %+v", run.Rows)
		}
		t.Logf("%s: source=%d accepted=%d duplicates=%d unmapped=%d snapshot=%s", definition.ID, preview.Rows.SourceRows, run.Rows.AcceptedRows, run.Rows.CollapsedDuplicates, len(preview.UnmappedGeographies), run.Snapshot.ID)
	}
	catalog, err := service.Catalog(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog.Datasets) != 5 {
		t.Fatalf("expected five WDI releases, got %d", len(catalog.Datasets))
	}
	encoded, err := json.MarshalIndent(catalog, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if report := os.Getenv("ATLAS_WDI_REPORT"); report != "" {
		if err := os.WriteFile(report, encoded, 0600); err != nil {
			t.Fatal(err)
		}
	}
}
