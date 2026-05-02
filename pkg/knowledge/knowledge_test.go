package knowledge

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSourceProcessingBuildsSummaryTermsAndQuestions(t *testing.T) {
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
	if source.WordCount == 0 || source.Summary == "" {
		t.Fatalf("source = %#v, want processed word count and summary", source)
	}
	if !contains(source.KeyTerms, "source") || !contains(source.KeyTerms, "evidence") {
		t.Fatalf("key terms = %#v, want source and evidence", source.KeyTerms)
	}
	if len(source.Questions) == 0 {
		t.Fatalf("questions = %#v, want suggested questions", source.Questions)
	}
}

func TestResearchReportUsesStoredEvidence(t *testing.T) {
	now := time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC)
	space, err := NewSpace(CreateSpaceRequest{Title: "Investigation"}, "kspace_test", now)
	if err != nil {
		t.Fatal(err)
	}
	source, err := NewSource(AddSourceRequest{
		Title:   "Evaluation notes",
		Content: "Source transparency improves review because evidence stays visible next to generated answers. Reviewers can compare claims with excerpts and identify gaps quickly.",
	}, "ksrc_one", now)
	if err != nil {
		t.Fatal(err)
	}
	space, err = AddSource(space, source, now)
	if err != nil {
		t.Fatal(err)
	}

	report, err := GenerateReport(space, ResearchRequest{Question: "How does evidence help review?"}, "kreport_test", now)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Evidence) == 0 {
		t.Fatalf("report = %#v, want evidence", report)
	}
	if !strings.Contains(report.Answer, "[S1]") {
		t.Fatalf("answer = %q, want cited finding label", report.Answer)
	}
	if len(report.Gaps) == 0 {
		t.Fatalf("gaps = %#v, want source-use note", report.Gaps)
	}
}

func TestURLSourceFetchesAndIndexesChunks(t *testing.T) {
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
	if source.Title != "Notebook source" || source.Ingestion.State != SourceStatusReady {
		t.Fatalf("source = %#v, want fetched title and ready state", source)
	}
	if source.Provenance.ContentHash == "" || len(source.Chunks) == 0 {
		t.Fatalf("source = %#v, want provenance hash and chunks", source)
	}
}

func TestQueryAskAndResearchRunUseCorpusEvidence(t *testing.T) {
	now := time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC)
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
	space, err = AddSource(space, source, now)
	if err != nil {
		t.Fatal(err)
	}

	query, err := QuerySpace(space, QueryRequest{Query: "research evidence", Limit: 3}, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(query.Evidence) == 0 || query.Evidence[0].ChunkID == "" {
		t.Fatalf("query = %#v, want chunk-backed evidence", query)
	}
	answer, err := AnswerQuestion(space, AskRequest{Question: "How should research be reviewed?"}, now)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(answer.Answer, "[S1]") {
		t.Fatalf("answer = %q, want cited answer", answer.Answer)
	}
	updated, run, report, err := CompleteResearchRun(space, CreateResearchRunRequest{
		Objective: "Review source-grounded research",
		Depth:     "deep",
	}, "krun_test", "kreport_run", now)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != ResearchRunStatusCompleted || run.ReportID != report.ID || len(updated.ResearchRuns) != 1 || len(updated.Reports) != 1 {
		t.Fatalf("run = %#v report = %#v updated = %#v, want completed run and stored report", run, report, updated)
	}
}

func TestStorePersistsProcessedSpace(t *testing.T) {
	now := time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC)
	store := NewStore(filepath.Join(t.TempDir(), "knowledge"))
	space, err := NewSpace(CreateSpaceRequest{Title: "Operations"}, "kspace_persist", now)
	if err != nil {
		t.Fatal(err)
	}
	source, err := NewSource(AddSourceRequest{
		Title:   "Runbook",
		Content: "Operators need concise runbooks. Runbooks should include validation commands and known risks.",
	}, "ksrc_persist", now)
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
	loaded, err := store.Load(space.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Insight.SourceCount != 1 || loaded.Sources[0].Summary == "" {
		t.Fatalf("loaded = %#v, want persisted processed source", loaded)
	}
	if loaded.Sources[0].Provenance.SnapshotPath == "" {
		t.Fatalf("loaded source = %#v, want filesystem snapshot path", loaded.Sources[0])
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

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
