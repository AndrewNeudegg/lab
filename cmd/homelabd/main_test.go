package main

import (
	"testing"

	"github.com/andrewneudegg/lab/pkg/config"
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
