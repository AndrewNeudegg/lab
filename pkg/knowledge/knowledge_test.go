package knowledge

import (
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
