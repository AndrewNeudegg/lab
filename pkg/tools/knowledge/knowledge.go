package knowledge

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	knowledgestore "github.com/andrewneudegg/lab/pkg/knowledge"
	"github.com/andrewneudegg/lab/pkg/tool"
)

type Base struct {
	Store            knowledgestore.Repository
	CreateSpace      func(context.Context, knowledgestore.CreateSpaceRequest) (knowledgestore.Space, string, error)
	AddSource        func(context.Context, string, knowledgestore.AddSourceRequest) (knowledgestore.Space, knowledgestore.Source, string, error)
	Query            func(context.Context, string, knowledgestore.QueryRequest) (knowledgestore.QueryResult, string, error)
	Ask              func(context.Context, string, knowledgestore.AskRequest) (knowledgestore.Space, knowledgestore.AskResult, knowledgestore.Report, string, error)
	StartResearchRun func(context.Context, string, knowledgestore.CreateResearchRunRequest) (knowledgestore.Space, knowledgestore.ResearchRun, knowledgestore.Report, string, error)
}

func Register(reg *tool.Registry, base Base) error {
	for _, t := range []tool.Tool{
		ListTool{base: base},
		CreateTool{base: base},
		AddSourceTool{base: base},
		QueryTool{base: base},
		AskTool{base: base},
		ResearchTool{base: base},
	} {
		if err := reg.Register(t); err != nil {
			return err
		}
	}
	return nil
}

func schema(v string) json.RawMessage { return json.RawMessage(v) }

type ListTool struct{ base Base }

func (ListTool) Name() string { return "knowledge.list" }
func (ListTool) Description() string {
	return "List Knowledge Spaces as compact corpus summaries. Use before choosing a space for knowledge.query, knowledge.ask, or knowledge.research."
}
func (ListTool) Schema() json.RawMessage {
	return schema(`{"type":"object","properties":{"query":{"type":"string","description":"Optional text to filter spaces by title, objective, key terms, or source titles."},"include_sources":{"type":"boolean","description":"Include compact source summaries for each matching space."},"limit":{"type":"integer","minimum":1,"description":"Optional maximum number of matching spaces to return."}}}`)
}
func (ListTool) Risk() tool.RiskLevel { return tool.RiskReadOnly }
func (t ListTool) Run(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
	if t.base.Store == nil {
		return nil, errors.New("knowledge store is not configured")
	}
	var req struct {
		Query          string `json:"query"`
		IncludeSources bool   `json:"include_sources"`
		Limit          int    `json:"limit"`
	}
	if len(input) > 0 {
		if err := json.Unmarshal(input, &req); err != nil {
			return nil, err
		}
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	spaces, err := t.base.Store.ListSummaries()
	if err != nil {
		return nil, err
	}
	sort.Slice(spaces, func(i, j int) bool { return spaces[i].UpdatedAt.After(spaces[j].UpdatedAt) })
	query := strings.ToLower(strings.TrimSpace(req.Query))
	out := make([]spaceSummary, 0, len(spaces))
	for _, space := range spaces {
		if query != "" && !spaceMatches(space, query) {
			continue
		}
		out = append(out, compactSpace(space, req.IncludeSources))
		if req.Limit > 0 && len(out) >= req.Limit {
			break
		}
	}
	return json.Marshal(map[string]any{"spaces": out})
}

type CreateTool struct{ base Base }

func (CreateTool) Name() string { return "knowledge.create" }
func (CreateTool) Description() string {
	return "Create a Knowledge Space when no existing corpus fits the research objective."
}
func (CreateTool) Schema() json.RawMessage {
	return schema(`{"type":"object","required":["title"],"properties":{"title":{"type":"string"},"objective":{"type":"string"},"description":{"type":"string"},"created_by":{"type":"string"}}}`)
}
func (CreateTool) Risk() tool.RiskLevel { return tool.RiskLow }
func (t CreateTool) Run(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
	if t.base.CreateSpace == nil {
		return nil, errors.New("knowledge create handler is not configured")
	}
	var req knowledgestore.CreateSpaceRequest
	if err := json.Unmarshal(input, &req); err != nil {
		return nil, err
	}
	space, reply, err := t.base.CreateSpace(ctx, req)
	if err != nil {
		return nil, err
	}
	return json.Marshal(map[string]any{"space": compactSpace(space, false), "reply": reply})
}

type AddSourceTool struct{ base Base }

func (AddSourceTool) Name() string { return "knowledge.add_source" }
func (AddSourceTool) Description() string {
	return "Import a specific URL, text excerpt, file text, note, email, or connected-resource extract into a Knowledge Space for later retrieval."
}
func (AddSourceTool) Schema() json.RawMessage {
	return schema(`{"type":"object","required":["space_id"],"properties":{"space_id":{"type":"string"},"title":{"type":"string"},"kind":{"type":"string","enum":["text","url","file","note","email","mcp"]},"uri":{"type":"string"},"url":{"type":"string","description":"Alias for uri when importing a web page or PDF."},"content":{"type":"string","description":"Source text. URL sources may omit content when the URI is fetchable."}}}`)
}
func (AddSourceTool) Risk() tool.RiskLevel { return tool.RiskLow }
func (t AddSourceTool) Run(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
	if t.base.AddSource == nil {
		return nil, errors.New("knowledge source handler is not configured")
	}
	var req struct {
		SpaceID string `json:"space_id"`
		Title   string `json:"title"`
		Kind    string `json:"kind"`
		URI     string `json:"uri"`
		URL     string `json:"url"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal(input, &req); err != nil {
		return nil, err
	}
	uri := firstNonEmpty(req.URI, req.URL)
	kind := strings.TrimSpace(req.Kind)
	if kind == "" && uri != "" && strings.TrimSpace(req.Content) == "" {
		kind = knowledgestore.SourceKindURL
	}
	if uri == "" && strings.TrimSpace(req.Content) == "" {
		return nil, errors.New("knowledge source requires uri, url, or content")
	}
	space, source, reply, err := t.base.AddSource(ctx, req.SpaceID, knowledgestore.AddSourceRequest{
		Title:   req.Title,
		Kind:    kind,
		URI:     uri,
		Content: req.Content,
	})
	if err != nil {
		return nil, err
	}
	return json.Marshal(map[string]any{
		"space":  compactSpace(space, false),
		"source": compactSource(source),
		"reply":  reply,
	})
}

type QueryTool struct{ base Base }

func (QueryTool) Name() string { return "knowledge.query" }
func (QueryTool) Description() string {
	return "Retrieve source-grounded evidence chunks from a Knowledge Space without generating a final answer."
}
func (QueryTool) Schema() json.RawMessage {
	return schema(`{"type":"object","required":["space_id","query"],"properties":{"space_id":{"type":"string"},"query":{"type":"string"},"source_ids":{"type":"array","items":{"type":"string"}},"limit":{"type":"integer","minimum":1,"maximum":20}}}`)
}
func (QueryTool) Risk() tool.RiskLevel { return tool.RiskReadOnly }
func (t QueryTool) Run(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
	var req struct {
		SpaceID   string   `json:"space_id"`
		Query     string   `json:"query"`
		SourceIDs []string `json:"source_ids"`
		Limit     int      `json:"limit"`
	}
	if err := json.Unmarshal(input, &req); err != nil {
		return nil, err
	}
	result, reply, err := t.query(ctx, req.SpaceID, knowledgestore.QueryRequest{
		Query:     req.Query,
		SourceIDs: req.SourceIDs,
		Limit:     req.Limit,
	})
	if err != nil {
		return nil, err
	}
	return json.Marshal(map[string]any{"result": result, "reply": reply})
}

func (t QueryTool) query(ctx context.Context, spaceID string, req knowledgestore.QueryRequest) (knowledgestore.QueryResult, string, error) {
	if t.base.Query != nil {
		return t.base.Query(ctx, spaceID, req)
	}
	if t.base.Store == nil {
		return knowledgestore.QueryResult{}, "", errors.New("knowledge query handler is not configured")
	}
	space, err := t.base.Store.Load(spaceID)
	if err != nil {
		return knowledgestore.QueryResult{}, "", err
	}
	result, err := knowledgestore.QuerySpace(space, req, time.Now().UTC())
	if err != nil {
		return knowledgestore.QueryResult{}, "", err
	}
	return result, "Knowledge query completed.", nil
}

type AskTool struct{ base Base }

func (AskTool) Name() string { return "knowledge.ask" }
func (AskTool) Description() string {
	return "Ask a selected Knowledge Space a source-grounded question, persist the answer as an ask report, and return citations."
}
func (AskTool) Schema() json.RawMessage {
	return schema(`{"type":"object","required":["space_id","question"],"properties":{"space_id":{"type":"string"},"question":{"type":"string"},"source_ids":{"type":"array","items":{"type":"string"}},"limit":{"type":"integer","minimum":1,"maximum":20}}}`)
}
func (AskTool) Risk() tool.RiskLevel { return tool.RiskLow }
func (t AskTool) Run(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
	if t.base.Ask == nil {
		return nil, errors.New("knowledge ask handler is not configured")
	}
	var req struct {
		SpaceID   string   `json:"space_id"`
		Question  string   `json:"question"`
		SourceIDs []string `json:"source_ids"`
		Limit     int      `json:"limit"`
	}
	if err := json.Unmarshal(input, &req); err != nil {
		return nil, err
	}
	space, result, report, reply, err := t.base.Ask(ctx, req.SpaceID, knowledgestore.AskRequest{
		Question:  req.Question,
		SourceIDs: req.SourceIDs,
		Limit:     req.Limit,
	})
	if err != nil {
		return nil, err
	}
	return json.Marshal(map[string]any{
		"space":  compactSpace(space, false),
		"result": result,
		"report": compactReport(report),
		"reply":  reply,
	})
}

type ResearchTool struct{ base Base }

func (ResearchTool) Name() string { return "knowledge.research" }
func (ResearchTool) Description() string {
	return "Queue durable Knowledge research that can search web and academic sources, import useful sources, and produce a report asynchronously."
}
func (ResearchTool) Schema() json.RawMessage {
	return schema(`{"type":"object","required":["space_id","objective"],"properties":{"space_id":{"type":"string"},"objective":{"type":"string"},"question":{"type":"string"},"mode":{"type":"string","enum":["research","brief","study"]},"depth":{"type":"string","enum":["quick","standard","deep"]},"source_ids":{"type":"array","items":{"type":"string"}},"discover_sources":{"type":"boolean","description":"Defaults to true so the run can learn from web and academic sources; set false for stored-corpus-only runs."}}}`)
}
func (ResearchTool) Risk() tool.RiskLevel { return tool.RiskLow }
func (t ResearchTool) Run(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
	if t.base.StartResearchRun == nil {
		return nil, errors.New("knowledge research handler is not configured")
	}
	var req struct {
		SpaceID         string   `json:"space_id"`
		Objective       string   `json:"objective"`
		Question        string   `json:"question"`
		Mode            string   `json:"mode"`
		Depth           string   `json:"depth"`
		SourceIDs       []string `json:"source_ids"`
		DiscoverSources *bool    `json:"discover_sources"`
	}
	if err := json.Unmarshal(input, &req); err != nil {
		return nil, err
	}
	discover := true
	if req.DiscoverSources != nil {
		discover = *req.DiscoverSources
	}
	space, run, report, reply, err := t.base.StartResearchRun(ctx, req.SpaceID, knowledgestore.CreateResearchRunRequest{
		Objective:       req.Objective,
		Question:        req.Question,
		Mode:            req.Mode,
		Depth:           req.Depth,
		SourceIDs:       req.SourceIDs,
		DiscoverSources: discover,
	})
	if err != nil {
		return nil, err
	}
	out := map[string]any{
		"space":          compactSpace(space, false),
		"run":            compactResearchRun(run),
		"reply":          reply,
		"dashboard_path": knowledgeRunPath(space.ID, run.ID),
	}
	if report.ID != "" {
		out["report"] = compactReport(report)
	}
	return json.Marshal(out)
}

type spaceSummary struct {
	ID                 string              `json:"id"`
	Title              string              `json:"title"`
	Description        string              `json:"description,omitempty"`
	Objective          string              `json:"objective,omitempty"`
	SourceCount        int                 `json:"source_count"`
	WordCount          int                 `json:"word_count"`
	KeyTerms           []string            `json:"key_terms,omitempty"`
	SuggestedQuestions []string            `json:"suggested_questions,omitempty"`
	LatestRun          *researchRunSummary `json:"latest_run,omitempty"`
	LatestReport       *reportSummary      `json:"latest_report,omitempty"`
	Sources            []sourceSummary     `json:"sources,omitempty"`
	CreatedAt          time.Time           `json:"created_at"`
	UpdatedAt          time.Time           `json:"updated_at"`
}

type sourceSummary struct {
	ID          string   `json:"id"`
	Title       string   `json:"title"`
	Kind        string   `json:"kind"`
	URI         string   `json:"uri,omitempty"`
	Summary     string   `json:"summary,omitempty"`
	KeyTerms    []string `json:"key_terms,omitempty"`
	WordCount   int      `json:"word_count"`
	Status      string   `json:"status,omitempty"`
	Stage       string   `json:"stage,omitempty"`
	Error       string   `json:"error,omitempty"`
	ContentType string   `json:"content_type,omitempty"`
	Extractor   string   `json:"extractor,omitempty"`
	ByteCount   int      `json:"byte_count,omitempty"`
}

type researchRunSummary struct {
	ID              string    `json:"id"`
	Objective       string    `json:"objective"`
	Status          string    `json:"status"`
	Mode            string    `json:"mode,omitempty"`
	Depth           string    `json:"depth,omitempty"`
	DiscoverSources bool      `json:"discover_sources,omitempty"`
	SourcesExamined int       `json:"sources_examined,omitempty"`
	EvidenceCount   int       `json:"evidence_count,omitempty"`
	ReportID        string    `json:"report_id,omitempty"`
	WorkspacePath   string    `json:"workspace_path,omitempty"`
	Error           string    `json:"error,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type reportSummary struct {
	ID            string    `json:"id"`
	RunID         string    `json:"run_id,omitempty"`
	Question      string    `json:"question"`
	Mode          string    `json:"mode"`
	EvidenceCount int       `json:"evidence_count"`
	CreatedAt     time.Time `json:"created_at"`
}

func compactSpace(space knowledgestore.Space, includeSources bool) spaceSummary {
	out := spaceSummary{
		ID:                 space.ID,
		Title:              space.Title,
		Description:        space.Description,
		Objective:          space.Objective,
		SourceCount:        space.Insight.SourceCount,
		WordCount:          space.Insight.WordCount,
		KeyTerms:           space.Insight.KeyTerms,
		SuggestedQuestions: space.Insight.SuggestedQuestions,
		CreatedAt:          space.CreatedAt,
		UpdatedAt:          space.UpdatedAt,
	}
	if out.SourceCount == 0 && len(space.Sources) > 0 {
		out.SourceCount = len(space.Sources)
	}
	if len(space.ResearchRuns) > 0 {
		run := compactResearchRun(space.ResearchRuns[0])
		out.LatestRun = &run
	}
	if len(space.Reports) > 0 {
		report := compactReport(space.Reports[0])
		out.LatestReport = &report
	}
	if includeSources {
		out.Sources = make([]sourceSummary, 0, len(space.Sources))
		for _, source := range space.Sources {
			out.Sources = append(out.Sources, compactSource(source))
		}
	}
	return out
}

func compactSource(source knowledgestore.Source) sourceSummary {
	return sourceSummary{
		ID:          source.ID,
		Title:       source.Title,
		Kind:        source.Kind,
		URI:         source.URI,
		Summary:     source.Summary,
		KeyTerms:    source.KeyTerms,
		WordCount:   source.WordCount,
		Status:      source.Ingestion.State,
		Stage:       source.Ingestion.Stage,
		Error:       source.Ingestion.Error,
		ContentType: source.Provenance.ContentType,
		Extractor:   source.Provenance.Extractor,
		ByteCount:   source.Provenance.ByteCount,
	}
}

func compactResearchRun(run knowledgestore.ResearchRun) researchRunSummary {
	return researchRunSummary{
		ID:              run.ID,
		Objective:       run.Objective,
		Status:          run.Status,
		Mode:            run.Mode,
		Depth:           run.Depth,
		DiscoverSources: run.DiscoverSources,
		SourcesExamined: run.SourcesExamined,
		EvidenceCount:   run.EvidenceCount,
		ReportID:        run.ReportID,
		WorkspacePath:   run.WorkspacePath,
		Error:           run.Error,
		CreatedAt:       run.CreatedAt,
		UpdatedAt:       run.UpdatedAt,
	}
}

func compactReport(report knowledgestore.Report) reportSummary {
	return reportSummary{
		ID:            report.ID,
		RunID:         report.RunID,
		Question:      report.Question,
		Mode:          report.Mode,
		EvidenceCount: len(report.Evidence),
		CreatedAt:     report.CreatedAt,
	}
}

func spaceMatches(space knowledgestore.Space, query string) bool {
	parts := []string{space.ID, space.Title, space.Description, space.Objective}
	parts = append(parts, space.Insight.KeyTerms...)
	parts = append(parts, space.Insight.SuggestedQuestions...)
	for _, source := range space.Sources {
		parts = append(parts, source.ID, source.Title, source.URI, source.Summary)
		parts = append(parts, source.KeyTerms...)
	}
	return strings.Contains(strings.ToLower(strings.Join(parts, " ")), query)
}

func knowledgeRunPath(spaceID, runID string) string {
	if strings.TrimSpace(spaceID) == "" || strings.TrimSpace(runID) == "" {
		return ""
	}
	return "/knowledge?space=" + spaceID + "#knowledge-research-" + runID
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
