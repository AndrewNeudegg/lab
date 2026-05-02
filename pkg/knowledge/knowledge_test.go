package knowledge

import (
	"bytes"
	"compress/zlib"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/andrewneudegg/lab/pkg/llm"
)

func TestSourceNormalisationDoesNotFabricateModelAnalysis(t *testing.T) {
	now := time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC)
	source, err := NewSource(AddSourceRequest{
		Title: "Retrieval notes",
		Content: strings.Join([]string{
			"Retrieval quality depends on clean source text and focused questions.",
			"Source grounded answers should show evidence so reviewers can inspect the claim.",
			"Research workflows need gaps when source coverage is thin.",
		}, " "),
	}, "ksrc_test", now)
	if err != nil {
		t.Fatal(err)
	}
	if source.WordCount == 0 || len(source.Chunks) == 0 {
		t.Fatalf("source = %#v, want word count and chunk index", source)
	}
	if source.Summary != "" || len(source.KeyTerms) != 0 || len(source.Questions) != 0 {
		t.Fatalf("source = %#v, want no fabricated model analysis", source)
	}
}

func TestLanguageModelAnalysesSourceWithStrictJSON(t *testing.T) {
	now := time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC)
	provider := &scriptedKnowledgeProvider{contents: []string{`{
		"summary":"**Evidence** stays visible for review.",
		"key_terms":["evidence","review"],
		"questions":["How should reviewers inspect evidence?"],
		"claims":[{"id":"c1","text":"Grounded answers need visible evidence.","importance":"high"}],
		"entities":[{"name":"Knowledge Space","type":"product","description":"Research corpus"}],
		"reliability_notes":["Source is operator-authored."]
	}`}}
	model := NewLanguageModel(provider, "test-model")
	source, err := NewSource(AddSourceRequest{
		Title:   "Evaluation notes",
		Content: "Source transparency improves review because evidence stays visible next to generated answers.",
	}, "ksrc_one", now)
	if err != nil {
		t.Fatal(err)
	}

	analyzed, err := model.AnalyzeSource(context.Background(), source, now)
	if err != nil {
		t.Fatal(err)
	}
	if analyzed.Summary == "" || !contains(analyzed.KeyTerms, "evidence") || len(analyzed.Claims) != 1 || len(analyzed.Entities) != 1 {
		t.Fatalf("analyzed = %#v, want model-populated analysis", analyzed)
	}
	if analyzed.Ingestion.State != SourceStatusReady || analyzed.Ingestion.Stage != "model_indexed" {
		t.Fatalf("ingestion = %#v, want model indexed ready state", analyzed.Ingestion)
	}
	if !strings.Contains(analyzed.Provenance.Extractor, "language-model") {
		t.Fatalf("provenance = %#v, want language model extractor", analyzed.Provenance)
	}
	if len(provider.requests) != 1 || provider.requests[0].ResponseFormat == nil || provider.requests[0].ResponseFormat.Name != "knowledge_source_analysis" {
		t.Fatalf("requests = %#v, want strict source analysis request", provider.requests)
	}
}

func TestURLSourceFetchesAndExtractsTextForModelAnalysis(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		rw.Header().Set("Content-Type", "text/html")
		_, _ = rw.Write([]byte(`<html><head><title>Notebook source</title></head><body><article>Evidence retrieval depends on durable source snapshots. Grounded answers cite chunks for review.</article></body></html>`))
	}))
	defer server.Close()
	now := time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC)

	source, err := BuildSource(context.Background(), AddSourceRequest{
		Kind: "url",
		URI:  server.URL,
	}, "ksrc_url", now, HTTPFetcher{Client: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	if source.Title != "Notebook source" || source.Ingestion.State != SourceStatusProcessing || source.Ingestion.Stage != "text_extracted" {
		t.Fatalf("source = %#v, want fetched title and processing text extraction state", source)
	}
	if source.Provenance.ContentHash == "" || len(source.Chunks) == 0 {
		t.Fatalf("source = %#v, want provenance hash and chunks", source)
	}
}

func TestExtractPDFTextReadsCompressedTextStream(t *testing.T) {
	var compressed bytes.Buffer
	writer := zlib.NewWriter(&compressed)
	_, _ = writer.Write([]byte("BT /F1 12 Tf (Evidence labels stay visible) Tj ET"))
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	body := append([]byte("<< /Filter /FlateDecode >>\nstream\n"), compressed.Bytes()...)
	body = append(body, []byte("\nendstream")...)

	text, err := extractPDFText(body)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "Evidence labels stay visible") {
		t.Fatalf("text = %q, want extracted PDF literal string", text)
	}
}

func TestAskUsesModelOverRetrievedEvidence(t *testing.T) {
	now := time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC)
	space := modelBackedTestSpace(t, now)
	query, err := QuerySpace(space, QueryRequest{Query: "research evidence", Limit: 3}, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(query.Evidence) == 0 || query.Evidence[0].ChunkID == "" {
		t.Fatalf("query = %#v, want chunk-backed evidence", query)
	}
	provider := &scriptedKnowledgeProvider{contents: []string{`{
		"answer":"Research should be reviewed against visible cited evidence [S1].",
		"key_findings":["[S1] Evidence and gaps are stored for review."],
		"gaps":["The answer only uses selected corpus evidence."]
	}`}}
	model := NewLanguageModel(provider, "test-model")

	answer, err := model.AnswerQuestion(context.Background(), space, AskRequest{Question: "How should research be reviewed?"}, query.Evidence, now)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(answer.Answer, "[S1]") || len(answer.KeyFindings) != 1 || answer.Provider != "scripted" || answer.Model != "test-model" {
		t.Fatalf("answer = %#v, want model answer with citation and provenance", answer)
	}
}

func TestResearchPlanAndReportAreModelBacked(t *testing.T) {
	now := time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC)
	space := modelBackedTestSpace(t, now)
	provider := &scriptedKnowledgeProvider{contents: []string{
		`{
			"rewritten_objective":"Review source-grounded research",
			"clarifying_questions":["Which audience is this for?"],
			"search_queries":["research evidence gaps"],
			"steps":["Retrieve corpus evidence","Synthesize cited findings"],
			"expected_outputs":["Markdown research report"]
		}`,
		`{
			"answer":"## Finding\nThe corpus says research runs should keep evidence and gaps visible [S1].",
			"key_findings":["[S1] Evidence and gaps are review artefacts."],
			"gaps":["No external sources were connected for this run."]
		}`,
	}}
	model := NewLanguageModel(provider, "test-model")
	plan, planResp, err := model.PlanResearch(context.Background(), space, CreateResearchRunRequest{
		Objective: "Review source-grounded research",
		Depth:     "deep",
	})
	if err != nil {
		t.Fatal(err)
	}
	if plan.RewrittenObjective == "" || len(plan.SearchQueries) != 1 || planResp.Usage.TotalTokens == 0 {
		t.Fatalf("plan = %#v response = %#v, want model plan with usage", plan, planResp)
	}
	query, err := QuerySpace(space, QueryRequest{Query: strings.Join(plan.SearchQueries, " "), Limit: 3}, now)
	if err != nil {
		t.Fatal(err)
	}
	run := ResearchRun{
		ID:        "krun_test",
		Objective: "Review source-grounded research",
		Depth:     "deep",
		Status:    ResearchRunStatusSynthesizing,
		Mode:      ReportModeResearch,
		Plan:      plan,
		CreatedAt: now,
		UpdatedAt: now,
	}
	report, err := model.SynthesizeReport(context.Background(), space, run, query.Evidence, "kreport_test", now)
	if err != nil {
		t.Fatal(err)
	}
	if report.Answer == "" || len(report.Evidence) == 0 || report.Provider != "scripted" {
		t.Fatalf("report = %#v, want model report with evidence and provenance", report)
	}
}

func TestStorePersistsProcessedSpace(t *testing.T) {
	now := time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC)
	store := NewStore(filepath.Join(t.TempDir(), "knowledge"))
	space := modelBackedTestSpace(t, now)
	if err := store.Save(space); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Load(space.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Insight.SourceCount != 1 || loaded.Sources[0].Summary == "" || len(loaded.Sources[0].Claims) != 1 {
		t.Fatalf("loaded = %#v, want persisted model-processed source", loaded)
	}
	if loaded.Sources[0].Provenance.SnapshotPath == "" {
		t.Fatalf("loaded source = %#v, want filesystem snapshot path", loaded.Sources[0])
	}
}

func TestStoreWritesResearchRunWorkspace(t *testing.T) {
	now := time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC)
	root := filepath.Join(t.TempDir(), "knowledge")
	store := NewStore(root)
	space, err := NewSpace(CreateSpaceRequest{Title: "Run workspace"}, "kspace_runs", now)
	if err != nil {
		t.Fatal(err)
	}
	run := ResearchRun{
		ID:              "krun_workspace",
		Objective:       "Discover online evidence",
		Depth:           "standard",
		Status:          ResearchRunStatusDiscovering,
		Mode:            ReportModeResearch,
		DiscoverSources: true,
		MaxSources:      3,
		Candidates: []SourceCandidate{{
			ID:     "candidate_one",
			Title:  "Evidence source",
			URL:    "https://example.com/evidence",
			Status: "imported",
		}},
		Events:    []ResearchRunEvent{{ID: "event_one", Stage: "discovery", Message: "Imported source", CreatedAt: now}},
		CreatedAt: now,
		UpdatedAt: now,
	}
	space, err = AddResearchRun(space, run, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(space); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Load(space.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.ResearchRuns[0].WorkspacePath == "" {
		t.Fatalf("run = %#v, want workspace path", loaded.ResearchRuns[0])
	}
	workspace := filepath.Join(root, loaded.ResearchRuns[0].WorkspacePath)
	for _, name := range []string{"state.json", "events.jsonl", "sources.json"} {
		if _, err := os.Stat(filepath.Join(workspace, name)); err != nil {
			t.Fatalf("stat %s: %v", name, err)
		}
	}
	state, err := os.ReadFile(filepath.Join(workspace, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(state), `"discover_sources": true`) {
		t.Fatalf("state.json = %s, want discovery metadata", string(state))
	}
}

func TestAddResearchRunUpsertsRunState(t *testing.T) {
	now := time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC)
	space, err := NewSpace(CreateSpaceRequest{Title: "Runs"}, "kspace_runs", now)
	if err != nil {
		t.Fatal(err)
	}
	run := ResearchRun{ID: "krun_one", Objective: "Compare", Depth: "standard", Status: ResearchRunStatusQueued, Mode: ReportModeResearch, CreatedAt: now, UpdatedAt: now}
	space, err = AddResearchRun(space, run, now)
	if err != nil {
		t.Fatal(err)
	}
	run.Status = ResearchRunStatusCompleted
	run.ReportID = "kreport_one"
	space, err = AddResearchRun(space, run, now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if len(space.ResearchRuns) != 1 || space.ResearchRuns[0].Status != ResearchRunStatusCompleted || space.ResearchRuns[0].ReportID != "kreport_one" {
		t.Fatalf("research_runs = %#v, want updated existing run", space.ResearchRuns)
	}
}

func TestStoreListReturnsEmptySliceWhenNoSpacesExist(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "knowledge"))

	spaces, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	if spaces == nil || len(spaces) != 0 {
		t.Fatalf("spaces = %#v, want non-nil empty slice", spaces)
	}
}

func modelBackedTestSpace(t *testing.T, now time.Time) Space {
	t.Helper()
	space, err := NewSpace(CreateSpaceRequest{Title: "Corpus"}, "kspace_corpus", now)
	if err != nil {
		t.Fatal(err)
	}
	source, err := NewSource(AddSourceRequest{
		Title:   "Notebook notes",
		Content: "NotebookLM style tools keep source corpora visible. Research runs should record events, evidence, and gaps for later review.",
	}, "ksrc_corpus", now)
	if err != nil {
		t.Fatal(err)
	}
	source.Summary = "Research runs keep source corpora, events, evidence, and gaps visible."
	source.KeyTerms = []string{"research", "evidence", "gaps"}
	source.Questions = []string{"How should research be reviewed?"}
	source.Claims = []SourceClaim{{ID: "claim_one", Text: "Research runs should record evidence and gaps.", Importance: "high"}}
	source.Ingestion = SourceIngestion{State: SourceStatusReady, Stage: "model_indexed", StartedAt: now, CompletedAt: now}
	source, err = NormalizeSource(source)
	if err != nil {
		t.Fatal(err)
	}
	space, err = AddSource(space, source, now)
	if err != nil {
		t.Fatal(err)
	}
	return space
}

type scriptedKnowledgeProvider struct {
	contents []string
	requests []llm.CompletionRequest
}

func (p *scriptedKnowledgeProvider) Name() string {
	return "scripted"
}

func (p *scriptedKnowledgeProvider) Complete(ctx context.Context, req llm.CompletionRequest) (llm.CompletionResponse, error) {
	p.requests = append(p.requests, req)
	if len(p.contents) == 0 {
		return llm.CompletionResponse{}, context.Canceled
	}
	content := p.contents[0]
	p.contents = p.contents[1:]
	return llm.CompletionResponse{
		Message:  llm.Message{Role: "assistant", Content: content},
		Provider: p.Name(),
		Model:    req.Model,
		Usage:    llm.Usage{InputTokens: 10, OutputTokens: 5, TotalTokens: 15},
	}, nil
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
