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

func TestRetryProviderHonoursRetryAfterDelay(t *testing.T) {
	inner := &retryAfterSequenceProvider{failures: 1, retryAfter: 2 * time.Second}
	var delays []time.Duration
	provider := newRetryProvider(inner, RetryConfig{MaxAttempts: 2, BaseDelay: time.Millisecond, MaxDelay: time.Millisecond}, func(_ context.Context, delay time.Duration) error {
		delays = append(delays, delay)
		return nil
	})
	resp, err := provider.Complete(context.Background(), CompletionRequest{})
	if err != nil {
		t.Fatalf("expected retry success: %v", err)
	}
	if resp.Message.Content != "ok" {
		t.Fatalf("unexpected response: %+v", resp)
	}
	if len(delays) != 1 || delays[0] != 2*time.Second {
		t.Fatalf("delays = %#v, want retry-after delay", delays)
	}
}

func TestRetryAfterHeaderParsesSecondsAndHTTPDate(t *testing.T) {
	now := time.Date(2026, 5, 3, 12, 0, 0, 0, time.UTC)
	if got := RetryAfterHeader("3", now); got != 3*time.Second {
		t.Fatalf("seconds retry-after = %s, want 3s", got)
	}
	if got := RetryAfterHeader("Sun, 03 May 2026 12:00:05 GMT", now); got != 5*time.Second {
		t.Fatalf("date retry-after = %s, want 5s", got)
	}
	if got := RetryAfterHeader("bad", now); got != 0 {
		t.Fatalf("bad retry-after = %s, want 0", got)
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

type retryAfterSequenceProvider struct {
	failures   int
	calls      int
	retryAfter time.Duration
}

func (p *retryAfterSequenceProvider) Name() string { return "retry-after-test" }

func (p *retryAfterSequenceProvider) Complete(context.Context, CompletionRequest) (CompletionResponse, error) {
	p.calls++
	if p.calls <= p.failures {
		return CompletionResponse{}, RetryableAfter(fmt.Errorf("provider returned 429 Too Many Requests"), p.retryAfter)
	}
	return CompletionResponse{Message: Message{Role: "assistant", Content: "ok"}}, nil
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
