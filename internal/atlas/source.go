package atlas

import (
	"context"
	"fmt"

	"github.com/supercakecrumb/context-atlas/internal/who"
	"github.com/supercakecrumb/context-atlas/internal/worldbank"
)

func (s *Service) iso3ToM49() map[string]string {
	result := make(map[string]string, len(s.leaves))
	for m49, area := range s.leaves {
		if area.ISOAlpha3 != nil && *area.ISOAlpha3 != "" {
			result[*area.ISOAlpha3] = m49
		}
	}
	return result
}

// Both sources use the same staging, owner confirmation and snapshot transaction.
func (s *Service) fetchSource(ctx context.Context, pageURL string, definition *who.DatasetDefinition) (previewRecord, who.Preview, []byte, error) {
	if definition != nil && definition.Source() == worldbank.Source {
		artifact, err := s.wdiFetch(ctx, *definition)
		if err != nil {
			return previewRecord{}, who.Preview{}, nil, err
		}
		preview, metadata, err := worldbank.BuildPreview(artifact.Bytes, *definition, artifact.AccessedAt, s.iso3ToM49())
		record := previewRecord{Version: 2, Dataset: definition, Summary: summarizePreview(preview), Artifact: artifactMetadata{
			PageURL: definition.PageURL, DownloadURL: definition.PageURL, ContentType: "application/zip", ContentLength: int64(len(artifact.Bytes)), WorldBank: &metadata, Files: artifact.Files,
		}}
		if len(artifact.Files) > 0 {
			record.Artifact.DownloadURL = artifact.Files[len(artifact.Files)-1].URL
		}
		return record, preview, artifact.Bytes, err
	}
	page, err := who.ValidateIndicatorPageURL(pageURL)
	if err != nil {
		return previewRecord{}, who.Preview{}, nil, err
	}
	fetched, err := s.fetcher.FetchFromIndicatorPage(ctx, pageURL)
	if err != nil {
		return previewRecord{}, who.Preview{}, nil, err
	}
	preview, err := who.BuildPreview(fetched.Artifact.Bytes, who.PreviewOptions{Dataset: definition, SourceURL: fetched.Artifact.URL, AccessedAt: fetched.Artifact.AccessedAt, ResolveM49: s.resolveM49})
	if definition == nil {
		generic := genericDefinition(page, preview)
		definition = &generic
		preview.Dataset = definition
	}
	record := previewRecord{Version: 1, Dataset: definition, Summary: summarizePreview(preview), Artifact: artifactMetadata{
		PageURL: page.URL, PageETag: fetched.Discovery.ETag, PageModified: fetched.Discovery.LastModified,
		DownloadURL: fetched.Artifact.URL, ETag: fetched.Artifact.ETag, LastModified: fetched.Artifact.LastModified,
		ContentType: fetched.Artifact.ContentType, ContentLength: fetched.Artifact.ContentLength,
	}}
	return record, preview, fetched.Artifact.Bytes, err
}

func releaseCitation(definition who.DatasetDefinition, record previewRecord) string {
	if record.Artifact.WorldBank != nil {
		return fmt.Sprintf("World Bank WDI: %s (%s). Original producers: %s. CC BY 4.0.", definition.Name, definition.IndicatorCode, record.Artifact.WorldBank.Producer)
	}
	return citation(definition)
}

func releaseParser(definition who.DatasetDefinition) string {
	if definition.Source() == worldbank.Source {
		return worldbank.ParserVersion
	}
	return parserVersion
}
