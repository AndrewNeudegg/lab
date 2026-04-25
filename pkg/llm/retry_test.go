package llm

import (
	"context"
	"fmt"
	"testing"
	"time"
)

type sequenceProvider struct {
	name      string
	failures  int
	calls     int
	retryable bool
}

func (p *sequenceProvider) Name() string { return p.name }

func (p *sequenceProvider) Complete(context.Context, CompletionRequest) (CompletionResponse, error) {
	p.calls++
	if p.calls <= p.failures {
		err := fmt.Errorf("temporary failure")
		if p.retryable {
			return CompletionResponse{}, Retryable(err)
		}
		return CompletionResponse{}, err
	}
	return CompletionResponse{Message: Message{Role: "assistant", Content: "ok"}}, nil
}

func TestRetryProviderRetriesRetryableErrors(t *testing.T) {
	inner := &sequenceProvider{name: "test", failures: 2, retryable: true}
	provider := newRetryProvider(inner, RetryConfig{MaxAttempts: 3, BaseDelay: time.Millisecond, MaxDelay: time.Millisecond}, func(context.Context, time.Duration) error {
		return nil
	})
	resp, err := provider.Complete(context.Background(), CompletionRequest{})
	if err != nil {
		t.Fatalf("expected retry success: %v", err)
	}
	if resp.Message.Content != "ok" {
		t.Fatalf("unexpected response: %+v", resp)
	}
	if inner.calls != 3 {
		t.Fatalf("expected 3 calls, got %d", inner.calls)
	}
}

func TestRetryProviderDoesNotRetryPermanentErrors(t *testing.T) {
	inner := &sequenceProvider{name: "test", failures: 2, retryable: false}
	provider := newRetryProvider(inner, RetryConfig{MaxAttempts: 3, BaseDelay: time.Millisecond, MaxDelay: time.Millisecond}, func(context.Context, time.Duration) error {
		return nil
	})
	_, err := provider.Complete(context.Background(), CompletionRequest{})
	if err == nil {
		t.Fatalf("expected permanent error")
	}
	if inner.calls != 1 {
		t.Fatalf("expected 1 call, got %d", inner.calls)
	}
}

func TestRetryProviderStopsAfterMaxAttempts(t *testing.T) {
	inner := &sequenceProvider{name: "test", failures: 5, retryable: true}
	provider := newRetryProvider(inner, RetryConfig{MaxAttempts: 3, BaseDelay: time.Millisecond, MaxDelay: time.Millisecond}, func(context.Context, time.Duration) error {
		return nil
	})
	_, err := provider.Complete(context.Background(), CompletionRequest{})
	if err == nil {
		t.Fatalf("expected retry exhaustion")
	}
	if inner.calls != 3 {
		t.Fatalf("expected 3 calls, got %d", inner.calls)
	}
}

func TestIsRetryableRecognizesTransientStatusText(t *testing.T) {
	if !IsRetryable(fmt.Errorf("gemini provider returned 503 Service Unavailable")) {
		t.Fatalf("expected 503 to be retryable")
	}
	if IsRetryable(fmt.Errorf("provider returned 400 Bad Request")) {
		t.Fatalf("expected 400 to be permanent")
	}
}
