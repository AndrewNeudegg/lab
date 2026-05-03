package knowledge

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"time"
)

func QuerySpace(space Space, req QueryRequest, now time.Time) (QueryResult, error) {
	normalized, err := NormalizeSpace(space)
	if err != nil {
		return QueryResult{}, err
	}
	query := strings.TrimSpace(req.Query)
	if query == "" {
		return QueryResult{}, fmt.Errorf("query is required")
	}
	limit := req.Limit
	if limit <= 0 {
		limit = 8
	}
	if limit > 20 {
		limit = 20
	}
	queryTerms := topTerms(query, 12)
	if len(queryTerms) == 0 {
		queryTerms = normalized.Insight.KeyTerms
	}
	evidence := rankEvidence(selectedSources(normalized.Sources, req.SourceIDs), queryTerms, ReportModeResearch, limit)
	return QueryResult{
		Query:     query,
		Terms:     queryTerms,
		Evidence:  evidence,
		CreatedAt: now,
	}, nil
}

func normalizeSourceProvenance(provenance SourceProvenance, source Source) SourceProvenance {
	provenance.URI = strings.TrimSpace(firstNonEmpty(provenance.URI, source.URI))
	provenance.CanonicalURI = strings.TrimSpace(provenance.CanonicalURI)
	provenance.ContentType = strings.TrimSpace(provenance.ContentType)
	provenance.ContentHash = strings.TrimSpace(provenance.ContentHash)
	provenance.SnapshotPath = strings.TrimSpace(provenance.SnapshotPath)
	provenance.Extractor = strings.TrimSpace(provenance.Extractor)
	if source.Content != "" {
		provenance.ByteCount = len([]byte(source.Content))
		if provenance.ContentHash == "" {
			provenance.ContentHash = contentHash(source.Content)
		}
	}
	return provenance
}

func normalizeSourceIngestion(ingestion SourceIngestion, hasContent bool) SourceIngestion {
	ingestion.State = strings.ToLower(strings.TrimSpace(ingestion.State))
	ingestion.Stage = strings.TrimSpace(ingestion.Stage)
	ingestion.Message = strings.TrimSpace(ingestion.Message)
	ingestion.Error = strings.TrimSpace(ingestion.Error)
	if ingestion.State == "" {
		if hasContent {
			ingestion.State = SourceStatusReady
		}
	}
	if ingestion.State == SourceStatusReady && ingestion.Message == "" {
		ingestion.Message = "Source is analysed and available for retrieval."
	}
	if ingestion.State == SourceStatusFailed && ingestion.Stage == "" {
		ingestion.Stage = "ingestion"
	}
	return ingestion
}

func normalizeSourceChunks(source Source) []SourceChunk {
	if source.Content == "" {
		return nil
	}
	rawChunks := sourceChunks(source.Content)
	chunks := make([]SourceChunk, 0, len(rawChunks))
	for index, text := range rawChunks {
		text = strings.TrimSpace(text)
		if text == "" {
			continue
		}
		chunks = append(chunks, SourceChunk{
			ID:            fmt.Sprintf("%s_chunk_%03d", source.ID, index+1),
			SourceID:      source.ID,
			SourceTitle:   source.Title,
			Index:         index,
			CitationLabel: fmt.Sprintf("%s.%d", compactCitationPrefix(source.ID), index+1),
			Text:          text,
			Terms:         topTerms(text, 6),
			WordCount:     len(contentWords(text, true)),
		})
	}
	return chunks
}

func contentHash(content string) string {
	sum := sha256.Sum256([]byte(content))
	return fmt.Sprintf("sha256:%x", sum[:])
}

func compactCitationPrefix(id string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return "C"
	}
	parts := strings.Split(id, "_")
	if len(parts) > 0 {
		last := strings.TrimSpace(parts[len(parts)-1])
		if len(last) >= 4 {
			return strings.ToUpper(last[:4])
		}
	}
	if len(id) > 4 {
		id = id[:4]
	}
	return strings.ToUpper(id)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
