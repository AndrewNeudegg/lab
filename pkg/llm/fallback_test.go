package llm

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

func TestFallbackProviderUsesNextProviderAfterFailure(t *testing.T) {
	provider := NewFallbackProvider([]ProviderCandidate{
		{Name: "gemini", Model: "gemini-flash-latest", Provider: staticProvider{name: "gemini", err: fmt.Errorf("gemini provider returned 429 Too Many Requests")}},
		{Name: "openai", Model: "gpt-5.1", Provider: staticProvider{name: "openai", content: "ok"}},
	})

	resp, err := provider.Complete(context.Background(), CompletionRequest{Model: "default-model"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Provider != "openai" {
		t.Fatalf("provider = %q, want openai", resp.Provider)
	}
	if resp.Model != "gpt-5.1" {
		t.Fatalf("model = %q, want gpt-5.1", resp.Model)
	}
	if resp.Message.Content != "ok" {
		t.Fatalf("content = %q, want ok", resp.Message.Content)
	}
}

func TestFallbackProviderClassifiesCandidateFailures(t *testing.T) {
	provider := NewFallbackProvider([]ProviderCandidate{
		{Name: "openai", Model: "gpt-test", Provider: staticProvider{name: "openai", err: fmt.Errorf("provider returned 400 Bad Request")}},
		{Name: "gemini", Model: "gemini-test", Provider: staticProvider{name: "gemini", err: Retryable(fmt.Errorf("provider returned 503 Service Unavailable"))}},
	})

	_, err := provider.Complete(context.Background(), CompletionRequest{Model: "default-model"})
	if err == nil {
		t.Fatal("expected all providers failed error")
	}
	var allFailed AllProvidersFailedError
	if !errors.As(err, &allFailed) {
		t.Fatalf("error type = %T, want AllProvidersFailedError", err)
	}
	if len(allFailed.Failures) != 2 {
		t.Fatalf("failures = %#v, want two", allFailed.Failures)
	}
	if allFailed.Failures[0].Class != ProviderFailurePermanent {
		t.Fatalf("first class = %q, want permanent", allFailed.Failures[0].Class)
	}
	if allFailed.Failures[1].Class != ProviderFailureRetryable {
		t.Fatalf("second class = %q, want retryable", allFailed.Failures[1].Class)
	}
}

type staticProvider struct {
	name    string
	content string
	err     error
}

func (p staticProvider) Name() string { return p.name }

func (p staticProvider) Complete(context.Context, CompletionRequest) (CompletionResponse, error) {
	if p.err != nil {
		return CompletionResponse{}, p.err
	}
	return CompletionResponse{Message: Message{Role: "assistant", Content: p.content}}, nil
}
