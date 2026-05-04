package knowledge

import (
	"bytes"
	"compress/zlib"
	"context"
	"encoding/json"
	"errors"
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

	text, err := extractEmbeddedPDFText(body)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "Evidence labels stay visible") {
		t.Fatalf("text = %q, want extracted PDF literal string", text)
	}
}

func TestExtractPDFTextUsesConfiguredPDFTextCommand(t *testing.T) {
	dir := t.TempDir()
	pdftotext := writeExecutable(t, filepath.Join(dir, "pdftotext"), `#!/bin/sh
printf 'Full academic paper text from pdftotext'
`)

	_, text, extractor, err := ExtractFetchedText(context.Background(), []byte("%PDF-1.7\n% paper bytes\n"), "application/pdf", TextExtractionOptions{
		PDFTextCommand: pdftotext,
		PDFOCR:         PDFOCROptions{Disabled: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if extractor != "pdf" || !strings.Contains(text, "Full academic paper text from pdftotext") {
		t.Fatalf("extractor = %q text = %q, want pdftotext PDF text", extractor, text)
	}
}

func TestExtractPDFTextUsesOCRWhenEmbeddedTextIsMissing(t *testing.T) {
	dir := t.TempDir()
	pdftoppm := writeExecutable(t, filepath.Join(dir, "pdftoppm"), `#!/bin/sh
prefix="${9}"
printf 'fake image' > "${prefix}-1.png"
`)
	tesseract := writeExecutable(t, filepath.Join(dir, "tesseract"), `#!/bin/sh
printf 'OCR cheese taxonomy evidence'
`)

	_, text, extractor, err := ExtractFetchedText(context.Background(), []byte("%PDF-1.7\n% scanned image only\n"), "application/pdf", TextExtractionOptions{
		PDFOCR: PDFOCROptions{
			PDFToPPMCommand:  pdftoppm,
			TesseractCommand: tesseract,
			MaxPages:         1,
			Timeout:          time.Second,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if extractor != "pdf+ocr" || !strings.Contains(text, "OCR cheese taxonomy evidence") {
		t.Fatalf("extractor = %q text = %q, want OCR text", extractor, text)
	}
}

func TestExtractPDFTextUsesOCRWhenEmbeddedTextIsUnreadable(t *testing.T) {
	dir := t.TempDir()
	pdftoppm := writeExecutable(t, filepath.Join(dir, "pdftoppm"), `#!/bin/sh
prefix="${9}"
printf 'fake image' > "${prefix}-1.png"
`)
	tesseract := writeExecutable(t, filepath.Join(dir, "tesseract"), `#!/bin/sh
printf 'OCR readable PDF evidence'
`)

	body := []byte("%PDF-1.7\nBT (\001\002\003\004\005\006) Tj ET\n")
	_, text, extractor, err := ExtractFetchedText(context.Background(), body, "application/pdf", TextExtractionOptions{
		PDFOCR: PDFOCROptions{
			PDFToPPMCommand:  pdftoppm,
			TesseractCommand: tesseract,
			MaxPages:         1,
			Timeout:          time.Second,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if extractor != "pdf+ocr" || !strings.Contains(text, "OCR readable PDF evidence") {
		t.Fatalf("extractor = %q text = %q, want OCR text for unreadable embedded text", extractor, text)
	}
}

func TestExtractPDFTextRejectsUnreadableEmbeddedTextWhenOCRDisabled(t *testing.T) {
	body := []byte("%PDF-1.7\nBT (\001\002\003\004\005\006) Tj ET\n")
	_, _, _, err := ExtractFetchedText(context.Background(), body, "application/pdf", TextExtractionOptions{
		PDFOCR: PDFOCROptions{Disabled: true},
	})
	if err == nil || !strings.Contains(err.Error(), "not readable enough") {
		t.Fatalf("err = %v, want unreadable embedded text error", err)
	}
}

func TestExtractPDFTextReportsMissingOCRDependency(t *testing.T) {
	_, _, _, err := ExtractFetchedText(context.Background(), []byte("%PDF-1.7\n% scanned image only\n"), "application/pdf", TextExtractionOptions{
		PDFOCR: PDFOCROptions{PDFToPPMCommand: "homelabd-missing-pdftoppm"},
	})
	if err == nil || !strings.Contains(err.Error(), "PDF OCR unavailable") {
		t.Fatalf("err = %v, want OCR dependency error", err)
	}
}

func writeExecutable(t *testing.T, path string, content string) string {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
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

func TestHybridRetrievalUsesSourceSectionsAndSemanticTerms(t *testing.T) {
	now := time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC)
	space, err := NewSpace(CreateSpaceRequest{Title: "Cheese corpus"}, "kspace_cheese", now)
	if err != nil {
		t.Fatal(err)
	}
	source, err := NewSource(AddSourceRequest{
		Title: "Cheese field guide",
		Content: strings.Join([]string{
			"# Fresh cheese",
			"Mozzarella has high moisture and an elastic texture.",
			"",
			"# Aged cheese",
			"Cheddar has a firm texture and sharp flavour after ageing.",
		}, "\n"),
	}, "ksrc_cheese", now)
	if err != nil {
		t.Fatal(err)
	}
	source.Summary = "A cheese taxonomy source covering fresh and aged families."
	source.Claims = []SourceClaim{{ID: "claim_mozzarella", Text: "Mozzarella is a fresh pasta filata cheese.", Importance: "high"}}
	source.KeyTerms = []string{"cheese", "mozzarella", "fresh", "aged"}
	source.Ingestion = SourceIngestion{State: SourceStatusReady, Stage: "model_indexed", StartedAt: now, CompletedAt: now}
	source, err = NormalizeSource(source)
	if err != nil {
		t.Fatal(err)
	}
	space, err = AddSource(space, source, now)
	if err != nil {
		t.Fatal(err)
	}

	query, err := QuerySpace(space, QueryRequest{Query: "pasta filata mozzarella", Limit: 2}, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(query.Evidence) == 0 {
		t.Fatal("expected hybrid evidence")
	}
	first := query.Evidence[0]
	if first.SectionTitle != "Fresh cheese" || first.Retrieval != "hybrid" || first.SemanticScore == 0 || first.SourceSummary == "" {
		t.Fatalf("evidence = %#v, want fresh-cheese hybrid retrieval with source summary", first)
	}
	if !contains(first.Terms, "pasta") || !contains(first.Terms, "mozzarella") {
		t.Fatalf("terms = %#v, want lexical and semantic matched terms", first.Terms)
	}
	index, err := BuildRetrievalIndex(space, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(index.Chunks) != 2 || index.Chunks[0].SectionTitle == "" || len(index.Chunks[0].SemanticTerms) == 0 {
		t.Fatalf("index = %#v, want sectioned semantic chunk index", index)
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

func TestResearchCoverageDecisionIsModelBacked(t *testing.T) {
	now := time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC)
	space := modelBackedTestSpace(t, now)
	provider := &scriptedKnowledgeProvider{contents: []string{`{
		"decision":"continue",
		"stop_reason":"The evidence covers stored review notes but not external corroboration.",
		"supported_claims":["Visible evidence supports review."],
		"gaps":["External corroboration is missing."],
		"follow_up_queries":["external evidence review source transparency"],
		"coverage":["stored review notes"]
	}`}}
	model := NewLanguageModel(provider, "test-model")
	run := ResearchRun{
		ID:        "krun_loop",
		Objective: "Review evidence practices",
		Depth:     "standard",
		Status:    ResearchRunStatusDiscovering,
		Mode:      ReportModeResearch,
		Plan: ResearchPlan{
			SearchQueries:   []string{"evidence review"},
			ExpectedOutputs: []string{"Review report"},
		},
		SourceIDs: []string{space.Sources[0].ID},
	}
	evidence, err := ResearchEvidence(space, run, 4)
	if err != nil {
		t.Fatal(err)
	}
	decision, resp, err := model.EvaluateResearchCoverage(context.Background(), space, run, ResearchLoop{
		ID:      "loop_one",
		Index:   1,
		Query:   "evidence review",
		Queries: []string{"evidence review"},
		Status:  "evaluating",
	}, evidence, now)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Decision != "continue" || len(decision.FollowUpQueries) != 1 || resp.Usage.TotalTokens == 0 {
		t.Fatalf("decision = %#v response = %#v, want model-backed continue decision", decision, resp)
	}
	if len(provider.requests) != 1 || provider.requests[0].ResponseFormat == nil || provider.requests[0].ResponseFormat.Name != "knowledge_research_coverage_decision" {
		t.Fatalf("requests = %#v, want strict coverage decision request", provider.requests)
	}
}

func TestResearchCoverageDecisionRetriesMalformedJSONAndCompactsLoops(t *testing.T) {
	now := time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC)
	space := modelBackedTestSpace(t, now)
	provider := &scriptedKnowledgeProvider{contents: []string{
		`{"decision":"continue","stop_reason":"truncated"`,
		`{
			"decision":"complete",
			"stop_reason":"The accepted source and evidence cover the objective.",
			"supported_claims":["Visible evidence supports review."],
			"gaps":[],
			"follow_up_queries":[],
			"coverage":["stored review notes"]
		}`,
	}}
	model := NewLanguageModel(provider, "test-model")
	run := ResearchRun{
		ID:        "krun_loop",
		Objective: "Review evidence practices",
		Depth:     "standard",
		Status:    ResearchRunStatusDiscovering,
		Mode:      ReportModeResearch,
		Plan: ResearchPlan{
			SearchQueries:   []string{"evidence review"},
			ExpectedOutputs: []string{"Review report"},
		},
		SourceIDs: []string{space.Sources[0].ID},
		ResearchLoops: []ResearchLoop{{
			ID:              "loop_previous",
			Index:           1,
			Query:           "evidence review",
			Queries:         []string{"evidence review"},
			Status:          "completed",
			Decision:        "continue",
			CandidateIDs:    []string{"candidate_one", "candidate_two"},
			SourceIDs:       []string{space.Sources[0].ID},
			AcceptedCount:   1,
			FailedCount:     8,
			EvidenceCount:   1,
			Gaps:            []string{"Need external corroboration."},
			FollowUpQueries: []string{"external evidence review source transparency"},
			SupportedClaims: []string{"Visible evidence supports review."},
			Coverage:        []string{"stored review notes"},
			StopReason:      "Stored evidence was useful but not enough.",
		}},
	}
	evidence, err := ResearchEvidence(space, run, 4)
	if err != nil {
		t.Fatal(err)
	}
	decision, resp, err := model.EvaluateResearchCoverage(context.Background(), space, run, ResearchLoop{
		ID:           "loop_current",
		Index:        2,
		Query:        "external evidence review",
		Queries:      []string{"external evidence review"},
		Status:       "evaluating",
		CandidateIDs: []string{"candidate_three"},
		SourceIDs:    []string{space.Sources[0].ID},
	}, evidence, now)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Decision != "complete" || resp.Usage.TotalTokens != 30 {
		t.Fatalf("decision = %#v response = %#v, want retried complete decision with aggregated usage", decision, resp)
	}
	if len(provider.requests) != 2 {
		t.Fatalf("requests = %d, want malformed response retry", len(provider.requests))
	}
	if provider.requests[1].MaxTokens <= provider.requests[0].MaxTokens {
		t.Fatalf("retry max tokens = %d, first = %d, want larger retry budget", provider.requests[1].MaxTokens, provider.requests[0].MaxTokens)
	}
	lastMessage := provider.requests[1].Messages[len(provider.requests[1].Messages)-1].Content
	if !strings.Contains(lastMessage, "previous response") || !strings.Contains(lastMessage, "not valid complete JSON") {
		t.Fatalf("retry prompt = %q, want JSON correction instruction", lastMessage)
	}
	requestJSON := provider.requests[0].Messages[len(provider.requests[0].Messages)-1].Content
	if strings.Contains(requestJSON, "candidate_ids") || strings.Contains(requestJSON, "candidate_one") {
		t.Fatalf("coverage prompt included candidate IDs: %s", requestJSON)
	}
	if !strings.Contains(requestJSON, "accepted_count") || !strings.Contains(requestJSON, "follow_up_queries") {
		t.Fatalf("coverage prompt = %s, want compact loop diagnostics", requestJSON)
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

func TestStoreDeletesSpacesAndSourceArtifacts(t *testing.T) {
	now := time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC)
	root := filepath.Join(t.TempDir(), "knowledge")
	store := NewStore(root)
	space := modelBackedTestSpace(t, now)
	if err := store.Save(space); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Load(space.ID)
	if err != nil {
		t.Fatal(err)
	}
	snapshotPath := filepath.Join(root, loaded.Sources[0].Provenance.SnapshotPath)
	if _, err := os.Stat(snapshotPath); err != nil {
		t.Fatalf("snapshot missing before delete: %v", err)
	}
	if err := store.DeleteSourceArtifacts(loaded.ID, loaded.Sources[0].ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(snapshotPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("snapshot stat after source delete = %v, want not exist", err)
	}

	if err := store.Save(space); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "runs", space.ID, "krun_one"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "runs", space.ID, "krun_one", "state.json"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := store.Delete(space.ID); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		filepath.Join(root, space.ID+".json"),
		filepath.Join(root, "snapshots", space.ID),
		filepath.Join(root, "indexes", space.ID),
		filepath.Join(root, "runs", space.ID),
	} {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("%s stat after space delete = %v, want not exist", path, err)
		}
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
		Coverage: []ResearchCoverage{{
			ID:            "coverage_one",
			Topic:         "Cheese taxonomy",
			Status:        "covered",
			SourceIDs:     []string{"ksrc_one"},
			EvidenceCount: 1,
			Notes:         "One cited source covers the topic.",
		}},
		Candidates: []SourceCandidate{{
			ID:     "candidate_one",
			Title:  "Evidence source",
			URL:    "https://example.com/evidence",
			Status: "imported",
		}},
		ResearchLoops: []ResearchLoop{{
			ID:              "loop_one",
			Index:           1,
			Query:           "cheese taxonomy",
			Queries:         []string{"cheese taxonomy sources"},
			Status:          "completed",
			Decision:        "complete",
			StopReason:      "Coverage sufficient.",
			CandidateIDs:    []string{"candidate_one"},
			SourceIDs:       []string{"ksrc_one"},
			AcceptedCount:   1,
			EvidenceCount:   1,
			SupportedClaims: []string{"Cheese taxonomy is covered."},
			StartedAt:       now,
			FinishedAt:      now,
		}},
		Events:    []ResearchRunEvent{{ID: "event_one", Stage: "discovery", Message: "Imported source", CreatedAt: now}},
		CreatedAt: now,
		UpdatedAt: now,
	}
	space, err = AddResearchRun(space, run, now)
	if err != nil {
		t.Fatal(err)
	}
	report := Report{
		ID:       "kreport_workspace",
		RunID:    run.ID,
		Question: "Discover online evidence",
		Mode:     ReportModeResearch,
		Answer:   "The run produced a final answer [S1].",
		Evidence: []Evidence{{
			ID:            "evidence_one",
			SourceID:      "ksrc_one",
			SourceTitle:   "Evidence source",
			CitationLabel: "S1",
			Excerpt:       "Evidence source text.",
			Score:         3,
		}},
		CreatedAt: now,
	}
	space, err = AddReport(space, report, now)
	if err != nil {
		t.Fatal(err)
	}
	run.ReportID = report.ID
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
	for _, name := range []string{"state.json", "events.jsonl", "sources.json", "candidates.json", "loops.json", "coverage.json", "report.json", "evidence.json"} {
		if _, err := os.Stat(filepath.Join(workspace, name)); err != nil {
			t.Fatalf("stat %s: %v", name, err)
		}
	}
	indexData, err := os.ReadFile(filepath.Join(root, "indexes", loaded.ID, "chunks.json"))
	if err != nil {
		t.Fatalf("stat retrieval index: %v", err)
	}
	var index RetrievalIndex
	if err := json.Unmarshal(indexData, &index); err != nil {
		t.Fatalf("decode retrieval index: %v", err)
	}
	if index.SpaceID != loaded.ID || len(index.Chunks) != 0 {
		t.Fatalf("index = %#v, want empty index for source-free workspace space", index)
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
	summaries, err := store.ListSummaries()
	if err != nil {
		t.Fatal(err)
	}
	if summaries == nil || len(summaries) != 0 {
		t.Fatalf("summaries = %#v, want non-nil empty slice", summaries)
	}
}

func TestStoreListSummariesOmitsHeavySourceBodies(t *testing.T) {
	now := time.Date(2026, 5, 4, 12, 0, 0, 0, time.UTC)
	store := NewStore(filepath.Join(t.TempDir(), "knowledge"))
	space, err := NewSpace(CreateSpaceRequest{Title: "Large corpus"}, "kspace_large", now)
	if err != nil {
		t.Fatal(err)
	}
	source, err := NewSource(AddSourceRequest{
		Title:   "Long paper",
		Kind:    SourceKindURL,
		URI:     "https://example.test/paper.pdf",
		Content: strings.Repeat("Knowledge research should preserve full source text for retrieval while list views stay lightweight. ", 200),
	}, "ksrc_large", now)
	if err != nil {
		t.Fatal(err)
	}
	source.Summary = "A paper about keeping Knowledge list views lightweight."
	source.KeyTerms = []string{"knowledge", "retrieval", "summaries"}
	source.Questions = []string{"How should large corpora load?"}
	source, err = NormalizeSource(source)
	if err != nil {
		t.Fatal(err)
	}
	space, err = AddSource(space, source, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(space); err != nil {
		t.Fatal(err)
	}

	summaries, err := store.ListSummaries()
	if err != nil {
		t.Fatal(err)
	}
	if len(summaries) != 1 || len(summaries[0].Sources) != 1 {
		t.Fatalf("summaries = %#v, want one space with one source", summaries)
	}
	summarySource := summaries[0].Sources[0]
	if summarySource.Content != "" || len(summarySource.Sections) != 0 || len(summarySource.Chunks) != 0 {
		t.Fatalf("summary source retained heavy fields: content=%d sections=%d chunks=%d", len(summarySource.Content), len(summarySource.Sections), len(summarySource.Chunks))
	}
	if summarySource.WordCount == 0 || summarySource.Provenance.ByteCount == 0 || summarySource.Summary == "" {
		t.Fatalf("summary source = %#v, want metadata, provenance, and model summary", summarySource)
	}
	if summaries[0].Insight.SourceCount != 1 || summaries[0].Insight.WordCount == 0 {
		t.Fatalf("insight = %#v, want source and word counts", summaries[0].Insight)
	}

	loaded, err := store.Load(space.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Sources[0].Content == "" || len(loaded.Sources[0].Chunks) == 0 {
		t.Fatalf("full load lost source body: content=%d chunks=%d", len(loaded.Sources[0].Content), len(loaded.Sources[0].Chunks))
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
