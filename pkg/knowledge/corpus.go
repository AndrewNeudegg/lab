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
	evidence := rankEvidence(selectedSources(normalized.Sources, req.SourceIDs), queryTerms, ReportModeResearch)
	if len(evidence) > limit {
		evidence = evidence[:limit]
	}
	return QueryResult{
		Query:     query,
		Terms:     queryTerms,
		Evidence:  evidence,
		CreatedAt: now,
	}, nil
}

func AnswerQuestion(space Space, req AskRequest, now time.Time) (AskResult, error) {
	question := strings.TrimSpace(req.Question)
	if question == "" {
		return AskResult{}, fmt.Errorf("question is required")
	}
	query, err := QuerySpace(space, QueryRequest{
		Query:     question,
		SourceIDs: req.SourceIDs,
		Limit:     req.Limit,
	}, now)
	if err != nil {
		return AskResult{}, err
	}
	normalized, err := NormalizeSpace(space)
	if err != nil {
		return AskResult{}, err
	}
	sources := selectedSources(normalized.Sources, req.SourceIDs)
	findings := findingsFromEvidence(query.Evidence, ReportModeResearch)
	return AskResult{
		Question:  question,
		Answer:    buildAnswer(question, sources, query.Evidence, findings),
		Evidence:  query.Evidence,
		Gaps:      researchGaps(sources, query.Terms, query.Evidence),
		CreatedAt: now,
	}, nil
}

func CompleteResearchRun(space Space, req CreateResearchRunRequest, runID, reportID string, now time.Time) (Space, ResearchRun, Report, error) {
	normalized, err := NormalizeSpace(space)
	if err != nil {
		return Space{}, ResearchRun{}, Report{}, err
	}
	objective := strings.TrimSpace(req.Objective)
	question := strings.TrimSpace(req.Question)
	if objective == "" {
		objective = question
	}
	if question == "" {
		question = objective
	}
	if objective == "" {
		return Space{}, ResearchRun{}, Report{}, fmt.Errorf("research objective is required")
	}
	run := ResearchRun{
		ID:         strings.TrimSpace(runID),
		Objective:  objective,
		Scope:      strings.TrimSpace(req.Scope),
		Depth:      normalizeResearchDepth(req.Depth),
		Status:     ResearchRunStatusCompleted,
		Question:   question,
		Mode:       normalizeReportMode(req.Mode),
		SourceIDs:  compactStrings(req.SourceIDs, 200),
		CreatedAt:  now,
		UpdatedAt:  now,
		StartedAt:  now,
		FinishedAt: now,
		Events: []ResearchRunEvent{
			{ID: fmt.Sprintf("%s_evt_01", runID), Stage: "planning", Message: "Research objective recorded and source scope prepared.", CreatedAt: now},
			{ID: fmt.Sprintf("%s_evt_02", runID), Stage: "retrieval", Message: "Retrieved matching corpus chunks from indexed sources.", CreatedAt: now},
			{ID: fmt.Sprintf("%s_evt_03", runID), Stage: "synthesis", Message: "Created a source-grounded report with evidence and gaps.", CreatedAt: now},
		},
	}
	report, err := GenerateReport(normalized, ResearchRequest{
		Question:  question,
		Mode:      run.Mode,
		SourceIDs: run.SourceIDs,
	}, reportID, now)
	if err != nil {
		run.Status = ResearchRunStatusFailed
		run.Error = err.Error()
		run.Events = append(run.Events, ResearchRunEvent{ID: fmt.Sprintf("%s_evt_04", runID), Stage: "failed", Message: err.Error(), CreatedAt: now})
		normalized, addErr := AddResearchRun(normalized, run, now)
		if addErr != nil {
			return Space{}, ResearchRun{}, Report{}, addErr
		}
		return normalized, run, Report{}, err
	}
	report.RunID = run.ID
	report = normalizeReport(report)
	run.ReportID = report.ID
	run.SourcesExamined = len(selectedSources(normalized.Sources, run.SourceIDs))
	run.EvidenceCount = len(report.Evidence)
	normalized, err = AddReport(normalized, report, now)
	if err != nil {
		return Space{}, ResearchRun{}, Report{}, err
	}
	normalized, err = AddResearchRun(normalized, run, now)
	if err != nil {
		return Space{}, ResearchRun{}, Report{}, err
	}
	return normalized, normalizeResearchRun(run), report, nil
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
		ingestion.Message = "Source is indexed and available for retrieval."
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
