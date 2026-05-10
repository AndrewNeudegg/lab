package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/andrewneudegg/lab/pkg/config"
	"github.com/andrewneudegg/lab/pkg/llm"
)

func TestBuildProviderAddsUsableOpenAIAsFallback(t *testing.T) {
	cfg := config.Config{
		DefaultProvider: "gemini",
		Providers: map[string]config.ProviderConfig{
			"gemini": {
				Type:    "gemini",
				BaseURL: "https://generativelanguage.googleapis.com/v1beta",
				Model:   "gemini-flash-latest",
				APIKey:  "gemini-key",
			},
			"openai": {
				Type:    "openai-compatible",
				BaseURL: "https://api.openai.com/v1",
				Model:   "gpt-5.1",
				APIKey:  "openai-key",
			},
		},
	}

	provider, model, err := buildProvider(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if model != "gemini-flash-latest" {
		t.Fatalf("model = %q, want default Gemini model", model)
	}
	if provider.Name() != "fallback(gemini->openai)" {
		t.Fatalf("provider = %q, want Gemini primary with OpenAI fallback", provider.Name())
	}
}

func TestBuildProviderDoesNotAddUnconfiguredOpenAIAsFallback(t *testing.T) {
	cfg := config.Config{
		DefaultProvider: "gemini",
		Providers: map[string]config.ProviderConfig{
			"gemini": {
				Type:    "gemini",
				BaseURL: "https://generativelanguage.googleapis.com/v1beta",
				Model:   "gemini-flash-latest",
				APIKey:  "gemini-key",
			},
			"openai": {
				Type:    "openai-compatible",
				BaseURL: "https://api.openai.com/v1",
				Model:   "gpt-5.1",
			},
		},
	}

	provider, _, err := buildProvider(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if provider.Name() != "gemini" {
		t.Fatalf("provider = %q, want Gemini only when OpenAI has no API key", provider.Name())
	}
}

func TestBuildProviderFallsThroughBeforeRetryingPrimary(t *testing.T) {
	geminiRequests := 0
	gemini := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		geminiRequests++
		http.Error(w, "quota exhausted", http.StatusTooManyRequests)
	}))
	defer gemini.Close()
	openaiRequests := 0
	openai := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		openaiRequests++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`))
	}))
	defer openai.Close()
	cfg := config.Config{
		DefaultProvider: "gemini",
		Providers: map[string]config.ProviderConfig{
			"gemini": {
				Type:    "gemini",
				BaseURL: gemini.URL,
				Model:   "gemini-test",
				APIKey:  "gemini-key",
			},
			"openai": {
				Type:    "openai-compatible",
				BaseURL: openai.URL,
				Model:   "openai-test",
				APIKey:  "openai-key",
			},
		},
	}

	provider, _, err := buildProvider(cfg)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := provider.Complete(context.Background(), llm.CompletionRequest{
		Model:    "default",
		Messages: []llm.Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}

	if resp.Provider != "openai" || resp.Message.Content != "ok" {
		t.Fatalf("response = %#v, want OpenAI fallback", resp)
	}
	if geminiRequests != 1 {
		t.Fatalf("gemini requests = %d, want immediate provider fallthrough before retry", geminiRequests)
	}
	if openaiRequests != 1 {
		t.Fatalf("openai requests = %d, want one fallback request", openaiRequests)
	}
}
