package worldbank

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

func fixture(t *testing.T) []byte {
	t.Helper()
	raw, err := os.ReadFile("testdata/gini.zip")
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func rewriteFixture(t *testing.T, change func(string, []byte) []byte) []byte {
	t.Helper()
	raw := fixture(t)
	input, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		t.Fatal(err)
	}
	var buffer bytes.Buffer
	output := zip.NewWriter(&buffer)
	for _, file := range input.File {
		reader, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(reader)
		_ = reader.Close()
		if err != nil {
			t.Fatal(err)
		}
		body = change(file.Name, body)
		if body == nil {
			continue
		}
		writer, err := output.Create(file.Name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := writer.Write(body); err != nil {
			t.Fatal(err)
		}
	}
	if err := output.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func TestWorldBankPreviewPreservesMeaningAndRejectsIncompleteArtifacts(t *testing.T) {
	definition, _ := DefinitionForURL("https://data.worldbank.org/indicator/SI.POV.GINI")
	mapping := map[string]string{"PRT": "620", "SWE": "752", "AFE": "004"}
	preview, metadata, err := BuildPreview(fixture(t), definition, time.Now(), mapping)
	if err != nil {
		t.Fatal(err)
	}
	if preview.Accounting.RowsRead != 5 || preview.Accounting.UniqueObservations != 5 || preview.Diagnostics.Warnings != 1 {
		t.Fatalf("bad accounting: %+v", preview)
	}
	if metadata.Producer != "Fixture statistical producer" || metadata.LastUpdated != "2026-07-13" || metadata.LicenseURL != LicenseURL {
		t.Fatalf("lost metadata: %+v", metadata)
	}
	rows := preview.Observations
	if rows[0].CanonicalM49 != "620" || rows[0].Year != 2023 || rows[0].SourceNote != "Income survey fixture" || *rows[0].Value.Numeric != 33.9 {
		t.Fatalf("lost source observation: %+v", rows[0])
	}
	if rows[1].Value.Numeric != nil || rows[1].Value.Status != "missing" || rows[1].Value.Raw != "null" {
		t.Fatalf("null changed: %+v", rows[1])
	}
	if rows[2].Value.Numeric == nil || *rows[2].Value.Numeric != 0 || rows[2].SourceStatus != "E" {
		t.Fatalf("zero or flag changed: %+v", rows[2])
	}
	if rows[3].CanonicalM49 != "" || rows[3].SourceGeo.Type != "aggregate" || rows[4].CanonicalM49 != "" {
		t.Fatal("aggregate or unknown geography was guessed onto the map")
	}
	for _, invalid := range []string{"https://data.worldbank.org/indicator/OTHER", "http://data.worldbank.org/indicator/SI.POV.GINI", "https://data.worldbank.org/indicator/SI.POV.GINI?url=http://localhost", "https://data.worldbank.org.evil/indicator/SI.POV.GINI"} {
		if _, ok := DefinitionForURL(invalid); ok {
			t.Fatalf("accepted URL %s", invalid)
		}
	}
	tests := map[string]func(string, []byte) []byte{
		"missing page": func(name string, body []byte) []byte {
			if name == "observations-002.json" {
				return nil
			}
			return body
		},
		"changed revision": func(name string, body []byte) []byte {
			if name == "observations-002.json" {
				return bytes.ReplaceAll(body, []byte("2026-07-13"), []byte("2026-07-14"))
			}
			return body
		},
		"wrong row count": func(_ string, body []byte) []byte {
			return bytes.ReplaceAll(body, []byte(`"total":5`), []byte(`"total":6`))
		},
		"wrong indicator": func(_ string, body []byte) []byte {
			return bytes.ReplaceAll(body, []byte("SI.POV.GINI"), []byte("OTHER"))
		},
		"nonannual date": func(_ string, body []byte) []byte {
			return bytes.ReplaceAll(body, []byte(`"date":"2023"`), []byte(`"date":"2021-2023"`))
		},
		"nonnumeric value": func(_ string, body []byte) []byte {
			return bytes.ReplaceAll(body, []byte(`"value":33.9`), []byte(`"value":"33.9"`))
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			if _, _, err := BuildPreview(rewriteFixture(t, mutate), definition, time.Now(), mapping); err == nil {
				t.Fatal("invalid artifact accepted")
			}
		})
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) { return fn(request) }

func TestWorldBankFetchRetainsOriginalPagesAndStableChecksum(t *testing.T) {
	raw := fixture(t)
	archive, _ := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	files := map[string][]byte{}
	for _, file := range archive.File {
		reader, _ := file.Open()
		files[file.Name], _ = io.ReadAll(reader)
		_ = reader.Close()
	}
	definition, _ := DefinitionForURL("https://data.worldbank.org/indicator/SI.POV.GINI")
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Scheme != "https" || request.URL.Host != "api.worldbank.org" || request.URL.Query().Get("format") != "json" || request.URL.Query().Get("gapfill") != "" {
			t.Fatalf("unsafe request: %s", request.URL)
		}
		group := "observations"
		if strings.HasPrefix(request.URL.Path, "/v2/indicator/") {
			group = "indicator"
		} else if request.URL.Path == "/v2/country" {
			group = "countries"
		}
		if group == "observations" && request.URL.Query().Get("footnote") != "y" {
			t.Fatal("source footnotes not requested")
		}
		body := files[group+"-00"+request.URL.Query().Get("page")+".json"]
		if body == nil {
			t.Fatalf("unexpected page: %s", request.URL)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(body)), Header: http.Header{"Content-Type": []string{"application/json; charset=utf-8"}}}, nil
	})}
	first, err := fetch(context.Background(), definition, client)
	if err != nil {
		t.Fatal(err)
	}
	second, err := fetch(context.Background(), definition, client)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first.Bytes, second.Bytes) || len(first.Files) != 4 {
		t.Fatal("artifact is incomplete or timestamps change its bytes")
	}
	stored, _ := zip.NewReader(bytes.NewReader(first.Bytes), int64(len(first.Bytes)))
	for _, file := range stored.File {
		reader, _ := file.Open()
		body, _ := io.ReadAll(reader)
		_ = reader.Close()
		if !bytes.Equal(body, files[file.Name]) {
			t.Fatal("original JSON bytes were transformed")
		}
	}
	// A provider error object must never be interpreted as an empty dataset.
	message, _ := json.Marshal([]any{map[string]any{"message": "error"}})
	if _, _, err := decodePage(message); err == nil {
		t.Fatal("provider error accepted")
	}
}
