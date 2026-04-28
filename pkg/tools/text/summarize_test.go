package text

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/andrewneudegg/lab/pkg/llm"
)

func TestSummarizeToolUsesConfiguredLLM(t *testing.T) {
	provider := &summaryProvider{content: `{"summary":"Fix task title summaries"}`}
	raw, err := NewSummarizeTool(provider, "summary-model").Run(context.Background(), json.RawMessage(`{"text":"Work this task to completion if possible. Task goal: make active task rows readable","purpose":"task_title","max_characters":84}`))
	if err != nil {
		t.Fatalf("run summarize: %v", err)
	}
	var result SummaryResult
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if result.Summary != "Fix task title summaries" {
		t.Fatalf("summary = %q", result.Summary)
	}
	if result.Fallback {
		t.Fatalf("fallback = true, want LLM result")
	}
	if len(provider.requests) != 1 {
		t.Fatalf("request count = %d, want 1", len(provider.requests))
	}
	req := provider.requests[0]
	if req.Model != "summary-model" || req.MaxTokens != 128 {
		t.Fatalf("request = %#v, want configured model and token cap", req)
	}
	if !strings.Contains(req.Messages[1].Content, "Maximum characters: 84") {
		t.Fatalf("user prompt = %q, want max character instruction", req.Messages[1].Content)
	}
}

func TestSummarizeToolClipsModelOutputToMaxCharacters(t *testing.T) {
	provider := &summaryProvider{content: `{"summary":"Make active task list titles concise and useful"}`}
	raw, err := NewSummarizeTool(provider, "summary-model").Run(context.Background(), json.RawMessage(`{"text":"make active task list titles concise and useful","purpose":"task_title","max_characters":24}`))
	if err != nil {
		t.Fatalf("run summarize: %v", err)
	}
	var result SummaryResult
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if len([]rune(result.Summary)) > 24 {
		t.Fatalf("summary length = %d, want <= 24: %q", len([]rune(result.Summary)), result.Summary)
	}
}

func TestSummarizeToolFallsBackWithoutProvider(t *testing.T) {
	raw, err := NewSummarizeTool(nil, "").Run(context.Background(), json.RawMessage(`{"text":"Work this task to completion if possible. Inspect the task workspace before editing. Task goal: when tasks are created the title comes from an LLM summary","purpose":"task_title","max_characters":40}`))
	if err != nil {
		t.Fatalf("run summarize: %v", err)
	}
	var result SummaryResult
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if !result.Fallback {
		t.Fatalf("fallback = false, want true")
	}
	if !strings.Contains(result.Summary, "tasks are created") {
		t.Fatalf("summary = %q, want goal-derived fallback", result.Summary)
	}
	if len([]rune(result.Summary)) > 40 {
		t.Fatalf("summary length = %d, want <= 40", len([]rune(result.Summary)))
	}
}

type summaryProvider struct {
	content  string
	requests []llm.CompletionRequest
}

func (p *summaryProvider) Name() string { return "summary-provider" }

func (p *summaryProvider) Complete(_ context.Context, req llm.CompletionRequest) (llm.CompletionResponse, error) {
	p.requests = append(p.requests, req)
	return llm.CompletionResponse{
		Message:  llm.Message{Role: "assistant", Content: p.content},
		Provider: p.Name(),
		Model:    req.Model,
	}, nil
}
