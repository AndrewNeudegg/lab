package knowledge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/andrewneudegg/lab/pkg/llm"
)

const (
	sourceAnalysisSchema = `{
  "type": "object",
  "required": ["summary", "key_terms", "questions", "claims", "entities", "reliability_notes"],
  "additionalProperties": false,
  "properties": {
    "summary": {"type": "string"},
    "key_terms": {"type": "array", "items": {"type": "string"}},
    "questions": {"type": "array", "items": {"type": "string"}},
    "claims": {
      "type": "array",
      "items": {
        "type": "object",
        "required": ["id", "text", "importance"],
        "additionalProperties": false,
        "properties": {
          "id": {"type": "string"},
          "text": {"type": "string"},
          "importance": {"type": "string"}
        }
      }
    },
    "entities": {
      "type": "array",
      "items": {
        "type": "object",
        "required": ["name", "type", "description"],
        "additionalProperties": false,
        "properties": {
          "name": {"type": "string"},
          "type": {"type": "string"},
          "description": {"type": "string"}
        }
      }
    },
    "reliability_notes": {"type": "array", "items": {"type": "string"}}
  }
}`

	askSchema = `{
  "type": "object",
  "required": ["answer", "key_findings", "gaps"],
  "additionalProperties": false,
  "properties": {
    "answer": {"type": "string"},
    "key_findings": {"type": "array", "items": {"type": "string"}},
    "gaps": {"type": "array", "items": {"type": "string"}}
  }
}`

	researchPlanSchema = `{
  "type": "object",
  "required": ["rewritten_objective", "clarifying_questions", "search_queries", "steps", "expected_outputs"],
  "additionalProperties": false,
  "properties": {
    "rewritten_objective": {"type": "string"},
    "clarifying_questions": {"type": "array", "items": {"type": "string"}},
    "search_queries": {"type": "array", "items": {"type": "string"}},
    "steps": {"type": "array", "items": {"type": "string"}},
    "expected_outputs": {"type": "array", "items": {"type": "string"}}
  }
}`

	sourceEvaluationSchema = `{
  "type": "object",
  "required": ["decision", "relevance_score", "reason", "coverage", "follow_up_queries"],
  "additionalProperties": false,
  "properties": {
    "decision": {"type": "string", "enum": ["accept", "reject", "partial"]},
    "relevance_score": {"type": "integer", "minimum": 0, "maximum": 100},
    "reason": {"type": "string"},
    "coverage": {"type": "array", "items": {"type": "string"}},
    "follow_up_queries": {"type": "array", "items": {"type": "string"}}
  }
}`

	researchCoverageDecisionSchema = `{
  "type": "object",
  "required": ["decision", "stop_reason", "supported_claims", "gaps", "follow_up_queries", "coverage"],
  "additionalProperties": false,
  "properties": {
    "decision": {"type": "string", "enum": ["complete", "continue"]},
    "stop_reason": {"type": "string"},
    "supported_claims": {"type": "array", "items": {"type": "string"}},
    "gaps": {"type": "array", "items": {"type": "string"}},
    "follow_up_queries": {"type": "array", "items": {"type": "string"}},
    "coverage": {"type": "array", "items": {"type": "string"}}
  }
}`

	researchReportSchema = `{
  "type": "object",
  "required": ["answer", "key_findings", "gaps"],
  "additionalProperties": false,
  "properties": {
    "answer": {"type": "string"},
    "key_findings": {"type": "array", "items": {"type": "string"}},
    "gaps": {"type": "array", "items": {"type": "string"}}
  }
}`
)

type LanguageModel struct {
	provider llm.Provider
	model    string
}

const jsonCompletionAttempts = 3

func NewLanguageModel(provider llm.Provider, model string) LanguageModel {
	return LanguageModel{provider: provider, model: strings.TrimSpace(model)}
}

func (m LanguageModel) configured() error {
	if m.provider == nil {
		return errors.New("knowledge language model provider is not configured")
	}
	if strings.TrimSpace(m.model) == "" {
		return errors.New("knowledge language model name is not configured")
	}
	return nil
}

func (m LanguageModel) AnalyzeSource(ctx context.Context, source Source, now time.Time) (Source, error) {
	if err := m.configured(); err != nil {
		return Source{}, err
	}
	source, err := NormalizeSource(source)
	if err != nil {
		return Source{}, err
	}
	if source.Ingestion.State == SourceStatusFailed {
		return source, nil
	}
	var analysis struct {
		Summary          string         `json:"summary"`
		KeyTerms         []string       `json:"key_terms"`
		Questions        []string       `json:"questions"`
		Claims           []SourceClaim  `json:"claims"`
		Entities         []SourceEntity `json:"entities"`
		ReliabilityNotes []string       `json:"reliability_notes"`
	}
	resp, err := m.completeJSON(ctx, "knowledge_source_analysis", sourceAnalysisSchema, 2200, []llm.Message{
		{Role: "system", Content: strings.Join([]string{
			"You analyse a source for a research corpus.",
			"Return exactly one JSON object matching the schema.",
			"Do not invent facts. Use only the provided source text and provenance.",
			"Write concise Markdown only where helpful inside string fields.",
			"Questions should be useful follow-up questions this source can help answer.",
		}, "\n")},
		{Role: "user", Content: "Source JSON:\n" + mustJSON(map[string]any{
			"id":         source.ID,
			"title":      source.Title,
			"kind":       source.Kind,
			"uri":        firstNonEmpty(source.Provenance.CanonicalURI, source.Provenance.URI, source.URI),
			"provenance": source.Provenance,
			"content":    boundedText(source.Content, 24000),
		})},
	}, &analysis)
	if err != nil {
		return Source{}, err
	}
	source.Summary = analysis.Summary
	source.KeyTerms = analysis.KeyTerms
	source.Questions = analysis.Questions
	source.Claims = analysis.Claims
	source.Entities = analysis.Entities
	source.Reliability = analysis.ReliabilityNotes
	source.Ingestion = SourceIngestion{
		State:       SourceStatusReady,
		Stage:       "model_indexed",
		Message:     "Source analysed by the configured language model and available for retrieval.",
		StartedAt:   firstTime(source.Ingestion.StartedAt, now),
		CompletedAt: now,
	}
	source.UpdatedAt = now
	source = attachModelProvenance(source, resp)
	return NormalizeSource(source)
}

func (m LanguageModel) AnswerQuestion(ctx context.Context, space Space, req AskRequest, evidence []Evidence, now time.Time) (AskResult, error) {
	if err := m.configured(); err != nil {
		return AskResult{}, err
	}
	question := strings.TrimSpace(req.Question)
	if question == "" {
		return AskResult{}, fmt.Errorf("question is required")
	}
	space, err := NormalizeSpace(space)
	if err != nil {
		return AskResult{}, err
	}
	var answer struct {
		Answer      string   `json:"answer"`
		KeyFindings []string `json:"key_findings"`
		Gaps        []string `json:"gaps"`
	}
	resp, err := m.completeJSON(ctx, "knowledge_grounded_answer", askSchema, 2600, []llm.Message{
		{Role: "system", Content: strings.Join([]string{
			"You answer questions over a research corpus.",
			"Return exactly one JSON object matching the schema.",
			"Use only the provided evidence. If evidence is insufficient, say so clearly in answer and gaps.",
			"Cite evidence labels inline, for example [S1] or [S2].",
			"Do not cite labels that are not present in the evidence list.",
		}, "\n")},
		{Role: "user", Content: "Question:\n" + question + "\n\nCorpus context:\n" + mustJSON(map[string]any{
			"space":    corpusSpaceBrief(space),
			"evidence": evidencePrompt(evidence),
		})},
	}, &answer)
	if err != nil {
		return AskResult{}, err
	}
	return AskResult{
		Question:    question,
		Answer:      strings.TrimSpace(answer.Answer),
		KeyFindings: compactStrings(answer.KeyFindings, 8),
		Evidence:    evidence,
		Gaps:        compactStrings(answer.Gaps, 8),
		Provider:    responseProvider(resp, m.provider),
		Model:       responseModel(resp, m.model),
		Usage:       tokenUsage(resp.Usage),
		CreatedAt:   now,
	}, nil
}

func (m LanguageModel) PlanResearch(ctx context.Context, space Space, req CreateResearchRunRequest) (ResearchPlan, llm.CompletionResponse, error) {
	if err := m.configured(); err != nil {
		return ResearchPlan{}, llm.CompletionResponse{}, err
	}
	space, err := NormalizeSpace(space)
	if err != nil {
		return ResearchPlan{}, llm.CompletionResponse{}, err
	}
	var plan ResearchPlan
	resp, err := m.completeJSON(ctx, "knowledge_research_plan", researchPlanSchema, 1800, []llm.Message{
		{Role: "system", Content: strings.Join([]string{
			"You plan a source-grounded research run.",
			"Return exactly one JSON object matching the schema.",
			"The plan should be executable over the stored corpus and explicit about missing context.",
			"Clarifying questions are for the operator to consider, but do not block the run.",
		}, "\n")},
		{Role: "user", Content: "Research request:\n" + mustJSON(map[string]any{
			"objective":        req.Objective,
			"question":         req.Question,
			"scope":            req.Scope,
			"depth":            req.Depth,
			"mode":             req.Mode,
			"discover_sources": req.DiscoverSources,
			"space":            corpusSpaceBrief(space),
			"sources":          corpusSourceBriefs(selectedSources(space.Sources, req.SourceIDs)),
		})},
	}, &plan)
	if err != nil {
		return ResearchPlan{}, llm.CompletionResponse{}, err
	}
	return normalizeResearchPlan(plan), resp, nil
}

func (m LanguageModel) EvaluateSourceForRun(ctx context.Context, source Source, run ResearchRun, candidate SourceCandidate, now time.Time) (SourceEvaluation, llm.CompletionResponse, error) {
	if err := m.configured(); err != nil {
		return SourceEvaluation{}, llm.CompletionResponse{}, err
	}
	source, err := NormalizeSource(source)
	if err != nil {
		return SourceEvaluation{}, llm.CompletionResponse{}, err
	}
	run = normalizeResearchRun(run)
	var output SourceEvaluation
	resp, err := m.completeJSON(ctx, "knowledge_source_evaluation", sourceEvaluationSchema, 1200, []llm.Message{
		{Role: "system", Content: strings.Join([]string{
			"You decide whether a fetched source belongs in a research corpus for a specific run.",
			"Return exactly one JSON object matching the schema.",
			"Accept sources that materially help answer the objective or cover a planned output.",
			"Use partial when the source is relevant but narrow, incomplete, or needs stronger corroboration.",
			"Reject unreadable, unrelated, mostly navigation, or low-information sources.",
			"Do not invent facts beyond the source summary, claims, provenance, and excerpt.",
		}, "\n")},
		{Role: "user", Content: "Run and source:\n" + mustJSON(map[string]any{
			"run": map[string]any{
				"objective": run.Objective,
				"question":  firstNonEmpty(run.Question, run.Objective),
				"scope":     run.Scope,
				"plan":      run.Plan,
			},
			"candidate": candidate,
			"source": map[string]any{
				"id":          source.ID,
				"title":       source.Title,
				"uri":         firstNonEmpty(source.Provenance.CanonicalURI, source.Provenance.URI, source.URI),
				"summary":     source.Summary,
				"key_terms":   source.KeyTerms,
				"claims":      source.Claims,
				"entities":    source.Entities,
				"reliability": source.Reliability,
				"word_count":  source.WordCount,
				"excerpt":     boundedText(source.Content, 8000),
			},
			"evaluated_at": now,
		})},
	}, &output)
	if err != nil {
		return SourceEvaluation{}, llm.CompletionResponse{}, err
	}
	return normalizeSourceEvaluation(output), resp, nil
}

func (m LanguageModel) EvaluateResearchCoverage(ctx context.Context, space Space, run ResearchRun, loop ResearchLoop, evidence []Evidence, now time.Time) (ResearchCoverageDecision, llm.CompletionResponse, error) {
	if err := m.configured(); err != nil {
		return ResearchCoverageDecision{}, llm.CompletionResponse{}, err
	}
	space, err := NormalizeSpace(space)
	if err != nil {
		return ResearchCoverageDecision{}, llm.CompletionResponse{}, err
	}
	run = normalizeResearchRun(run)
	loop = normalizeResearchLoops([]ResearchLoop{loop})[0]
	var output ResearchCoverageDecision
	resp, err := m.completeJSON(ctx, "knowledge_research_coverage_decision", researchCoverageDecisionSchema, 2600, []llm.Message{
		{Role: "system", Content: strings.Join([]string{
			"You decide whether an iterative research run has enough source coverage to answer its objective.",
			"Return exactly one JSON object matching the schema.",
			"Use complete only when the accepted source summaries and evidence can directly answer the objective with citations.",
			"Use continue when important aspects are missing, weakly sourced, contradictory, or need primary/academic corroboration.",
			"Follow-up queries must be specific searches that would close the named gaps.",
			"Do not invent facts beyond the provided source summaries, loop state, coverage, and evidence.",
		}, "\n")},
		{Role: "user", Content: "Research coverage review:\n" + mustJSON(map[string]any{
			"objective":    run.Objective,
			"question":     firstNonEmpty(run.Question, run.Objective),
			"scope":        run.Scope,
			"plan":         run.Plan,
			"current_loop": researchLoopPrompt(loop),
			"all_loops":    researchLoopPrompts(run.ResearchLoops),
			"coverage":     run.Coverage,
			"sources":      corpusSourceBriefs(selectedSources(space.Sources, run.SourceIDs)),
			"evidence":     evidencePrompt(evidence),
			"evaluated_at": now,
		})},
	}, &output)
	if err != nil {
		return ResearchCoverageDecision{}, llm.CompletionResponse{}, err
	}
	return normalizeResearchCoverageDecision(output), resp, nil
}

func (m LanguageModel) SynthesizeReport(ctx context.Context, space Space, run ResearchRun, evidence []Evidence, reportID string, now time.Time) (Report, error) {
	if err := m.configured(); err != nil {
		return Report{}, err
	}
	space, err := NormalizeSpace(space)
	if err != nil {
		return Report{}, err
	}
	var output struct {
		Answer      string   `json:"answer"`
		KeyFindings []string `json:"key_findings"`
		Gaps        []string `json:"gaps"`
	}
	resp, err := m.completeJSON(ctx, "knowledge_research_report", researchReportSchema, 4200, []llm.Message{
		{Role: "system", Content: strings.Join([]string{
			"You write source-grounded research reports for a corpus.",
			"Return exactly one JSON object matching the schema.",
			"Use Markdown in the answer field.",
			"Answer the research question directly before discussing process.",
			"Use source summaries to decide which sources matter, and use evidence excerpts for citations.",
			"Use only the provided sources, coverage, and evidence. Cite evidence labels inline.",
			"Identify contradictions, uncertainty, and missing source coverage in gaps.",
		}, "\n")},
		{Role: "user", Content: "Research run:\n" + mustJSON(map[string]any{
			"objective":      run.Objective,
			"question":       firstNonEmpty(run.Question, run.Objective),
			"scope":          run.Scope,
			"depth":          run.Depth,
			"mode":           run.Mode,
			"plan":           run.Plan,
			"research_loops": researchLoopPrompts(run.ResearchLoops),
			"stop_reason":    run.StopReason,
			"coverage":       run.Coverage,
			"space":          corpusSpaceBrief(space),
			"sources":        corpusSourceBriefs(selectedSources(space.Sources, run.SourceIDs)),
			"evidence":       evidencePrompt(evidence),
		})},
	}, &output)
	if err != nil {
		return Report{}, err
	}
	return normalizeReport(Report{
		ID:          strings.TrimSpace(reportID),
		RunID:       run.ID,
		Question:    firstNonEmpty(run.Question, run.Objective),
		Mode:        run.Mode,
		Answer:      output.Answer,
		KeyFindings: output.KeyFindings,
		Evidence:    evidence,
		Gaps:        output.Gaps,
		Provider:    responseProvider(resp, m.provider),
		Model:       responseModel(resp, m.model),
		Usage:       tokenUsage(resp.Usage),
		CreatedAt:   now,
	}), nil
}

func (m LanguageModel) completeJSON(ctx context.Context, name, schema string, maxTokens int, messages []llm.Message, target any) (llm.CompletionResponse, error) {
	var lastErr error
	var usage llm.Usage
	for attempt := 1; attempt <= jsonCompletionAttempts; attempt++ {
		resp, err := m.provider.Complete(ctx, llm.CompletionRequest{
			Model:       m.model,
			Temperature: 0,
			MaxTokens:   jsonCompletionMaxTokens(maxTokens, attempt),
			Messages:    jsonCompletionMessages(messages, name, lastErr),
			ResponseFormat: &llm.ResponseFormat{
				Name:   name,
				Schema: json.RawMessage(schema),
				Strict: true,
			},
		})
		if err != nil {
			return llm.CompletionResponse{}, err
		}
		usage = addLLMUsage(usage, resp.Usage)
		raw := bytes.TrimSpace([]byte(resp.Message.Content))
		if len(raw) == 0 {
			lastErr = fmt.Errorf("knowledge language model returned empty JSON for %s", name)
			continue
		}
		if err := decodeStrictJSON(raw, target); err != nil {
			lastErr = fmt.Errorf("knowledge language model returned invalid %s JSON: %w", name, err)
			continue
		}
		resp.Usage = usage
		return resp, nil
	}
	return llm.CompletionResponse{}, lastErr
}

func jsonCompletionMaxTokens(base, attempt int) int {
	if base <= 0 || attempt <= 1 {
		return base
	}
	return base * attempt
}

func jsonCompletionMessages(messages []llm.Message, name string, previous error) []llm.Message {
	if previous == nil {
		return messages
	}
	out := append([]llm.Message{}, messages...)
	out = append(out, llm.Message{Role: "user", Content: strings.Join([]string{
		"The previous response for " + name + " was not valid complete JSON.",
		"Error: " + previous.Error(),
		"Return exactly one complete JSON object matching the requested schema.",
		"Do not include prose, Markdown fences, comments, or multiple JSON values.",
	}, "\n")})
	return out
}

func decodeStrictJSON(raw []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return fmt.Errorf("multiple JSON values")
	}
	return nil
}

func addLLMUsage(left, right llm.Usage) llm.Usage {
	return llm.Usage{
		InputTokens:  left.InputTokens + right.InputTokens,
		OutputTokens: left.OutputTokens + right.OutputTokens,
		TotalTokens:  left.TotalTokens + right.TotalTokens,
	}
}

func attachModelProvenance(source Source, resp llm.CompletionResponse) Source {
	if source.Provenance.Extractor == "" {
		source.Provenance.Extractor = "language-model"
		return source
	}
	if !strings.Contains(source.Provenance.Extractor, "language-model") {
		source.Provenance.Extractor += "+language-model"
	}
	return source
}

func tokenUsage(usage llm.Usage) TokenUsage {
	return TokenUsage{InputTokens: usage.InputTokens, OutputTokens: usage.OutputTokens, TotalTokens: usage.TotalTokens}
}

func responseProvider(resp llm.CompletionResponse, provider llm.Provider) string {
	if strings.TrimSpace(resp.Provider) != "" {
		return strings.TrimSpace(resp.Provider)
	}
	if provider != nil {
		return provider.Name()
	}
	return ""
}

func responseModel(resp llm.CompletionResponse, model string) string {
	if strings.TrimSpace(resp.Model) != "" {
		return strings.TrimSpace(resp.Model)
	}
	return strings.TrimSpace(model)
}

func corpusSpaceBrief(space Space) map[string]any {
	return map[string]any{
		"id":          space.ID,
		"title":       space.Title,
		"description": space.Description,
		"objective":   space.Objective,
		"key_terms":   space.Insight.KeyTerms,
		"sources":     len(space.Sources),
	}
}

func corpusSourceBriefs(sources []Source) []map[string]any {
	out := make([]map[string]any, 0, len(sources))
	for _, source := range sources {
		out = append(out, map[string]any{
			"id":          source.ID,
			"title":       source.Title,
			"kind":        source.Kind,
			"uri":         firstNonEmpty(source.Provenance.CanonicalURI, source.Provenance.URI, source.URI),
			"summary":     source.Summary,
			"key_terms":   source.KeyTerms,
			"claims":      source.Claims,
			"word_count":  source.WordCount,
			"reliability": source.Reliability,
		})
	}
	return out
}

func researchLoopPrompts(loops []ResearchLoop) []map[string]any {
	out := make([]map[string]any, 0, len(loops))
	for _, loop := range loops {
		out = append(out, researchLoopPrompt(loop))
	}
	return out
}

func researchLoopPrompt(loop ResearchLoop) map[string]any {
	return map[string]any{
		"id":                loop.ID,
		"index":             loop.Index,
		"query":             boundedText(loop.Query, 700),
		"queries":           promptStrings(loop.Queries, 10, 220),
		"status":            loop.Status,
		"decision":          loop.Decision,
		"stop_reason":       boundedText(loop.StopReason, 900),
		"accepted_count":    loop.AcceptedCount,
		"rejected_count":    loop.RejectedCount,
		"failed_count":      loop.FailedCount,
		"evidence_count":    loop.EvidenceCount,
		"source_ids":        compactStrings(loop.SourceIDs, 20),
		"coverage":          promptStrings(loop.Coverage, 12, 260),
		"supported_claims":  promptStrings(loop.SupportedClaims, 12, 320),
		"gaps":              promptStrings(loop.Gaps, 12, 320),
		"follow_up_queries": promptStrings(loop.FollowUpQueries, 12, 240),
	}
}

func evidencePrompt(evidence []Evidence) []map[string]any {
	out := make([]map[string]any, 0, len(evidence))
	for _, item := range evidence {
		out = append(out, map[string]any{
			"id":       item.ID,
			"label":    item.CitationLabel,
			"source":   item.SourceTitle,
			"kind":     item.SourceKind,
			"uri":      item.SourceURI,
			"chunk_id": item.ChunkID,
			"section":  firstNonEmpty(item.SectionTitle, item.SectionID),
			"excerpt":  boundedText(item.Excerpt, 1400),
			"terms":    item.Terms,
			"summary":  boundedText(item.SourceSummary, 600),
			"retrieval": map[string]any{
				"method":         item.Retrieval,
				"lexical_score":  item.LexicalScore,
				"semantic_score": item.SemanticScore,
				"score":          item.Score,
			},
			"score": item.Score,
		})
	}
	return out
}

func promptStrings(values []string, limit, maxLen int) []string {
	values = compactStrings(values, limit)
	for i, value := range values {
		values[i] = boundedText(value, maxLen)
	}
	return values
}

func normalizeSourceEvaluation(input SourceEvaluation) SourceEvaluation {
	input.Decision = strings.ToLower(strings.TrimSpace(input.Decision))
	switch input.Decision {
	case "accept", "partial", "reject":
	default:
		input.Decision = "reject"
	}
	if input.RelevanceScore < 0 {
		input.RelevanceScore = 0
	}
	if input.RelevanceScore > 100 {
		input.RelevanceScore = 100
	}
	input.Reason = strings.TrimSpace(input.Reason)
	input.Coverage = compactStrings(input.Coverage, 12)
	input.FollowUpQueries = compactStrings(input.FollowUpQueries, 8)
	return input
}

func normalizeResearchCoverageDecision(input ResearchCoverageDecision) ResearchCoverageDecision {
	input.Decision = strings.ToLower(strings.TrimSpace(input.Decision))
	switch input.Decision {
	case "complete", "continue":
	default:
		input.Decision = "continue"
	}
	input.StopReason = strings.TrimSpace(input.StopReason)
	input.SupportedClaims = compactStrings(input.SupportedClaims, 20)
	input.Gaps = compactStrings(input.Gaps, 20)
	input.FollowUpQueries = compactStrings(input.FollowUpQueries, 12)
	input.Coverage = compactStrings(input.Coverage, 20)
	if input.Decision == "continue" && len(input.FollowUpQueries) == 0 && input.StopReason == "" {
		input.StopReason = "Coverage is incomplete but the model did not provide follow-up queries."
	}
	if input.Decision == "complete" && input.StopReason == "" {
		input.StopReason = "Coverage is sufficient to answer the research objective."
	}
	return input
}

func mustJSON(value any) string {
	b, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return "{}"
	}
	return string(b)
}

func boundedText(value string, max int) string {
	value = strings.TrimSpace(value)
	if max <= 0 || len(value) <= max {
		return value
	}
	return value[:max] + "\n[truncated]"
}

func firstTime(values ...time.Time) time.Time {
	for _, value := range values {
		if !value.IsZero() {
			return value
		}
	}
	return time.Time{}
}
