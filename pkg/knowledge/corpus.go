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

func (LocalRetrievalIndexer) Build(space Space, now time.Time) (RetrievalIndex, error) {
	normalized, err := NormalizeSpace(space)
	if err != nil {
		return RetrievalIndex{}, err
	}
	chunks := make([]RetrievalIndexChunk, 0)
	for _, source := range normalized.Sources {
		if source.Ingestion.State == SourceStatusFailed {
			continue
		}
		for _, chunk := range source.Chunks {
			chunks = append(chunks, RetrievalIndexChunk{
				SourceID:      source.ID,
				SourceTitle:   source.Title,
				SourceKind:    source.Kind,
				SourceURI:     firstNonEmpty(source.Provenance.CanonicalURI, source.Provenance.URI, source.URI),
				SourceSummary: shorten(source.Summary, 300),
				SectionID:     chunk.SectionID,
				SectionTitle:  chunk.SectionTitle,
				ChunkID:       chunk.ID,
				CitationLabel: chunk.CitationLabel,
				TextHash:      contentHash(chunk.Text),
				Terms:         chunk.Terms,
				SemanticTerms: chunk.SemanticTerms,
				WordCount:     chunk.WordCount,
			})
		}
	}
	return RetrievalIndex{SpaceID: normalized.ID, UpdatedAt: now, Chunks: chunks}, nil
}

func BuildRetrievalIndex(space Space, now time.Time) (RetrievalIndex, error) {
	return LocalRetrievalIndexer{}.Build(space, now)
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

func normalizeSourceSections(source Source) []SourceSection {
	rawSections := sourceSections(source.Content, source.Title)
	sections := make([]SourceSection, 0, len(rawSections))
	for index, section := range rawSections {
		text := cleanSourceContent(section.Text)
		if text == "" {
			continue
		}
		heading := strings.TrimSpace(section.Heading)
		if heading == "" {
			heading = source.Title
		}
		sections = append(sections, SourceSection{
			ID:          fmt.Sprintf("%s_section_%03d", source.ID, index+1),
			SourceID:    source.ID,
			SourceTitle: source.Title,
			Index:       index,
			Heading:     heading,
			Text:        text,
			Terms:       topTerms(heading+"\n"+text, 10),
			WordCount:   len(contentWords(text, true)),
		})
	}
	return sections
}

func normalizeSourceChunks(source Source) []SourceChunk {
	if source.Content == "" {
		return nil
	}
	sections := source.Sections
	if len(sections) == 0 {
		sections = normalizeSourceSections(source)
	}
	var chunks []SourceChunk
	chunkIndex := 0
	for _, section := range sections {
		rawChunks := sourceChunks(section.Text)
		for _, text := range rawChunks {
			text = strings.TrimSpace(text)
			if text == "" {
				continue
			}
			chunk := SourceChunk{
				ID:            fmt.Sprintf("%s_chunk_%03d", source.ID, chunkIndex+1),
				SourceID:      source.ID,
				SourceTitle:   source.Title,
				SectionID:     section.ID,
				SectionTitle:  section.Heading,
				Index:         chunkIndex,
				CitationLabel: fmt.Sprintf("%s.%d", compactCitationPrefix(source.ID), chunkIndex+1),
				Text:          text,
				Terms:         topTerms(text, 8),
				WordCount:     len(contentWords(text, true)),
			}
			chunk.SemanticTerms = semanticTermsForChunk(source, section, chunk)
			chunks = append(chunks, chunk)
			chunkIndex++
		}
	}
	return chunks
}

type sourceSectionDraft struct {
	Heading string
	Text    string
}

func sourceSections(content, title string) []sourceSectionDraft {
	content = cleanSourceContent(content)
	if content == "" {
		return nil
	}
	lines := strings.Split(content, "\n")
	heading := strings.TrimSpace(title)
	var builder strings.Builder
	var sections []sourceSectionDraft
	flush := func() {
		text := cleanSourceContent(builder.String())
		if text != "" {
			sections = append(sections, sourceSectionDraft{Heading: heading, Text: text})
		}
		builder.Reset()
	}
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if markdownHeadingLevel(trimmed) > 0 {
			flush()
			heading = strings.TrimSpace(strings.TrimLeft(trimmed, "#"))
			continue
		}
		if builder.Len() > 0 {
			builder.WriteByte('\n')
		}
		builder.WriteString(line)
	}
	flush()
	if len(sections) == 0 {
		sections = append(sections, sourceSectionDraft{Heading: title, Text: content})
	}
	return sections
}

func markdownHeadingLevel(line string) int {
	if !strings.HasPrefix(line, "#") {
		return 0
	}
	count := 0
	for count < len(line) && line[count] == '#' {
		count++
	}
	if count == 0 || count > 6 || count >= len(line) || line[count] != ' ' {
		return 0
	}
	return count
}

func semanticTermsForChunk(source Source, section SourceSection, chunk SourceChunk) []string {
	var builder strings.Builder
	contextTerms := compactStrings(append(append([]string{}, section.Terms...), chunk.Terms...), 24)
	builder.WriteString(source.Title)
	builder.WriteByte('\n')
	builder.WriteString(section.Heading)
	builder.WriteByte('\n')
	builder.WriteString(strings.Join(section.Terms, " "))
	builder.WriteByte('\n')
	builder.WriteString(strings.Join(chunk.Terms, " "))
	if score, _ := scoreText(source.Summary, contextTerms); score > 0 {
		builder.WriteByte('\n')
		builder.WriteString(source.Summary)
	}
	for _, term := range source.KeyTerms {
		if containsString(contextTerms, term) {
			builder.WriteByte('\n')
			builder.WriteString(term)
		}
	}
	for _, question := range source.Questions {
		if score, _ := scoreText(question, contextTerms); score > 0 {
			builder.WriteByte('\n')
			builder.WriteString(question)
		}
	}
	for _, claim := range source.Claims {
		if score, _ := scoreText(claim.Text, contextTerms); score > 0 {
			builder.WriteByte('\n')
			builder.WriteString(claim.Text)
			builder.WriteByte(' ')
			builder.WriteString(claim.Importance)
		}
	}
	for _, entity := range source.Entities {
		entityText := entity.Name + " " + entity.Type + " " + entity.Description
		if score, _ := scoreText(entityText, contextTerms); score > 0 {
			builder.WriteByte('\n')
			builder.WriteString(entityText)
		}
	}
	for _, note := range source.Reliability {
		builder.WriteByte('\n')
		builder.WriteString(note)
	}
	return topTerms(builder.String(), 30)
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
