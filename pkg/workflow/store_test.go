package workflow

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"
)

func TestNormalizeWorkflowDefaultsToLLMStepAndEstimate(t *testing.T) {
	item, err := New(CreateRequest{
		Name: "Research release notes",
		Goal: "Find recent release notes and summarise risk.",
	}, "workflow_123", time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if item.Status != StatusDraft {
		t.Fatalf("status = %q, want draft", item.Status)
	}
	if len(item.Steps) != 1 || item.Steps[0].Kind != StepKindLLM {
		t.Fatalf("steps = %#v, want default LLM step", item.Steps)
	}
	if item.Estimate.EstimatedLLMCalls != 1 || item.Estimate.EstimatedMinutes != 1 {
		t.Fatalf("estimate = %#v, want one short LLM call", item.Estimate)
	}
}

func TestStorePersistsWorkflowWithToolCost(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "workflows"))
	item, err := New(CreateRequest{
		Name: "Research bundle",
		Steps: []Step{{
			Name: "Search web",
			Kind: StepKindTool,
			Tool: "internet.search",
			Args: json.RawMessage(`{"query":"agent workflow design"}`),
		}, {
			Name:   "Summarise",
			Kind:   StepKindLLM,
			Prompt: "Summarise the fetched sources.",
		}},
	}, "workflow_456", time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(item); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Load("workflow_456")
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Estimate.EstimatedToolCalls != 1 || loaded.Estimate.EstimatedLLMCalls != 1 {
		t.Fatalf("estimate = %#v, want one tool and one LLM call", loaded.Estimate)
	}
	if len(loaded.Steps) != 2 || string(loaded.Steps[0].Args) == "" {
		t.Fatalf("steps = %#v, want persisted args", loaded.Steps)
	}
}
