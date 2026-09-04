package worldbank

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"time"

	"github.com/supercakecrumb/context-atlas/internal/who"
)

const maxBytes = 50 << 20
const maxPages = 20
const perPage = 10000
const apiBase = "https://api.worldbank.org/v2/"

type ResponseFile struct {
	Name         string    `json:"name"`
	URL          string    `json:"url"`
	SHA256       string    `json:"sha256"`
	ContentType  string    `json:"content_type"`
	AccessedAt   time.Time `json:"accessed_at"`
	ETag         string    `json:"etag,omitempty"`
	LastModified string    `json:"last_modified,omitempty"`
}

type Artifact struct {
	Bytes      []byte
	Files      []ResponseFile
	AccessedAt time.Time
}

type pageMeta struct {
	Page        int    `json:"page"`
	Pages       int    `json:"pages"`
	Total       int    `json:"total"`
	SourceID    string `json:"sourceid"`
	LastUpdated string `json:"lastupdated"`
}

func decodePage(raw []byte) (pageMeta, []json.RawMessage, error) {
	var envelope []json.RawMessage
	var meta pageMeta
	var rows []json.RawMessage
	if err := json.Unmarshal(raw, &envelope); err != nil || len(envelope) != 2 {
		return meta, nil, fmt.Errorf("invalid World Bank JSON response envelope")
	}
	if err := json.Unmarshal(envelope[0], &meta); err != nil {
		return meta, nil, err
	}
	if err := json.Unmarshal(envelope[1], &rows); err != nil {
		return meta, nil, err
	}
	if meta.Page < 1 || meta.Pages < 1 || meta.Pages > maxPages || meta.Total < 1 || meta.Total > perPage*maxPages || len(rows) == 0 {
		return meta, nil, fmt.Errorf("invalid or empty World Bank pagination")
	}
	return meta, rows, nil
}

// Fetch retains every original JSON response in a deterministic ZIP. Access
// timestamps and transport metadata stay outside the content checksum so an
// unchanged response does not manufacture a new release on each refresh.
func Fetch(ctx context.Context, definition who.DatasetDefinition) (Artifact, error) {
	if err := ValidateDefinition(definition); err != nil {
		return Artifact{}, err
	}
	client := who.NewFetcher(who.FetchOptions{}).PublicClient("api.worldbank.org", func(string) error {
		return fmt.Errorf("world bank API redirects are not accepted")
	})
	defer client.CloseIdleConnections()
	return fetch(ctx, definition, client)
}

func fetch(ctx context.Context, definition who.DatasetDefinition, client *http.Client) (Artifact, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	var buffer bytes.Buffer
	archive := zip.NewWriter(&buffer)
	result := Artifact{AccessedAt: time.Now().UTC()}
	totalBytes := 0
	endpoints := []struct{ name, path string }{
		{"indicator", "indicator/" + definition.IndicatorCode + "?source=2"},
		{"countries", "country?"},
		{"observations", "country/all/indicator/" + definition.IndicatorCode + "?source=2&footnote=y"},
	}
	for _, endpoint := range endpoints {
		pages, count := 1, 0
		var first pageMeta
		for page := 1; page <= pages; page++ {
			requestURL := fmt.Sprintf("%s%s&format=json&per_page=%d&page=%d", apiBase, endpoint.path, perPage, page)
			request, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
			if err != nil {
				return Artifact{}, err
			}
			request.Header.Set("Accept", "application/json")
			response, err := client.Do(request)
			if err != nil {
				return Artifact{}, fmt.Errorf("fetch World Bank %s page %d: %w", endpoint.name, page, err)
			}
			raw, readErr := io.ReadAll(io.LimitReader(response.Body, int64(maxBytes-totalBytes)+1))
			_ = response.Body.Close()
			if readErr != nil {
				return Artifact{}, readErr
			}
			totalBytes += len(raw)
			if response.StatusCode != http.StatusOK {
				return Artifact{}, fmt.Errorf("world bank %s returned HTTP %d", endpoint.name, response.StatusCode)
			}
			mediaType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
			if err != nil || mediaType != "application/json" {
				return Artifact{}, fmt.Errorf("world bank returned non-JSON content")
			}
			if totalBytes > maxBytes {
				return Artifact{}, fmt.Errorf("world bank artifact exceeds 50 MiB")
			}
			meta, rows, err := decodePage(raw)
			if err != nil {
				return Artifact{}, err
			}
			if page == 1 {
				first = meta
				pages = meta.Pages
			}
			if meta.Page != page || meta.Pages != first.Pages || meta.Total != first.Total || meta.SourceID != first.SourceID || meta.LastUpdated != first.LastUpdated {
				return Artifact{}, fmt.Errorf("world bank release changed during pagination")
			}
			count += len(rows)
			name := fmt.Sprintf("%s-%03d.json", endpoint.name, page)
			entry, err := archive.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Store})
			if err != nil {
				return Artifact{}, err
			}
			if _, err := entry.Write(raw); err != nil {
				return Artifact{}, err
			}
			result.Files = append(result.Files, ResponseFile{Name: name, URL: requestURL, SHA256: fmt.Sprintf("%x", sha256.Sum256(raw)), ContentType: response.Header.Get("Content-Type"), AccessedAt: time.Now().UTC(), ETag: response.Header.Get("ETag"), LastModified: response.Header.Get("Last-Modified")})
		}
		if count != first.Total {
			return Artifact{}, fmt.Errorf("world bank %s row count mismatch: %d of %d", endpoint.name, count, first.Total)
		}
	}
	if err := archive.Close(); err != nil {
		return Artifact{}, err
	}
	if buffer.Len() > maxBytes {
		return Artifact{}, fmt.Errorf("world bank ZIP exceeds 50 MiB")
	}
	result.Bytes = buffer.Bytes()
	return result, nil
}
