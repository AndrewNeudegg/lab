package text

import (
	"context"
	"encoding/json"
	"testing"
)

func TestCorrectToolFixesSearchQueryAndReturnsVariants(t *testing.T) {
	raw, err := CorrectTool{}.Run(context.Background(), json.RawMessage(`{"text":"kittens in pijamas","mode":"search_query","max_variants":4}`))
	if err != nil {
		t.Fatalf("run correct: %v", err)
	}
	var result Result
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if result.Corrected != "kittens in pajamas" {
		t.Fatalf("corrected = %q, want kittens in pajamas", result.Corrected)
	}
	if !result.Changed || len(result.Corrections) != 1 {
		t.Fatalf("expected one correction, got %+v", result)
	}
	if len(result.Alternatives) == 0 || result.Alternatives[0] != "kittens in pyjamas" {
		t.Fatalf("expected pyjamas alternative, got %+v", result.Alternatives)
	}
	if len(result.SearchQueries) < 3 || result.SearchQueries[0] != "kittens in pajamas" || result.SearchQueries[1] != "kittens in pyjamas" || result.SearchQueries[2] != "kittens in pijamas" {
		t.Fatalf("unexpected search queries: %+v", result.SearchQueries)
	}
}

func TestCorrectToolAppliesLightGrammar(t *testing.T) {
	raw, err := CorrectTool{}.Run(context.Background(), json.RawMessage(`{"text":"i found teh docs for a API and an cat","mode":"all"}`))
	if err != nil {
		t.Fatalf("run correct: %v", err)
	}
	var result Result
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if result.Corrected != "I found the docs for an API and a cat" {
		t.Fatalf("corrected = %q", result.Corrected)
	}
	if len(result.Corrections) != 4 {
		t.Fatalf("expected four corrections, got %+v", result.Corrections)
	}
}

func TestCorrectToolHonoursBritishLocale(t *testing.T) {
	raw, err := CorrectTool{}.Run(context.Background(), json.RawMessage(`{"text":"pijamas","mode":"spelling","locale":"en-GB"}`))
	if err != nil {
		t.Fatalf("run correct: %v", err)
	}
	var result Result
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if result.Corrected != "pyjamas" {
		t.Fatalf("corrected = %q, want pyjamas", result.Corrected)
	}
}

func TestCorrectToolRequiresText(t *testing.T) {
	if _, err := (CorrectTool{}).Run(context.Background(), json.RawMessage(`{"text":"   "}`)); err == nil {
		t.Fatalf("expected text required error")
	}
}
