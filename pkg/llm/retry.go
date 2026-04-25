package llm

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

type RetryableError struct {
	Err error
}

func (e RetryableError) Error() string {
	return e.Err.Error()
}

func (e RetryableError) Unwrap() error {
	return e.Err
}

func Retryable(err error) error {
	if err == nil {
		return nil
	}
	return RetryableError{Err: err}
}

func IsRetryable(err error) bool {
	var retryable RetryableError
	if errors.As(err, &retryable) {
		return true
	}
	if err == nil {
		return false
	}
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "429") || strings.Contains(text, "500") || strings.Contains(text, "502") || strings.Contains(text, "503") || strings.Contains(text, "504") || strings.Contains(text, "timeout") || strings.Contains(text, "temporarily")
}

type RetryConfig struct {
	MaxAttempts int
	BaseDelay   time.Duration
	MaxDelay    time.Duration
}

type RetryProvider struct {
	inner Provider
	cfg   RetryConfig
	sleep func(context.Context, time.Duration) error
}

func WithRetry(inner Provider, cfg RetryConfig) Provider {
	return newRetryProvider(inner, cfg, sleepContext)
}

func newRetryProvider(inner Provider, cfg RetryConfig, sleep func(context.Context, time.Duration) error) *RetryProvider {
	if cfg.MaxAttempts <= 0 {
		cfg.MaxAttempts = 3
	}
	if cfg.BaseDelay <= 0 {
		cfg.BaseDelay = 500 * time.Millisecond
	}
	if cfg.MaxDelay <= 0 {
		cfg.MaxDelay = 5 * time.Second
	}
	return &RetryProvider{inner: inner, cfg: cfg, sleep: sleep}
}

func (p *RetryProvider) Name() string {
	return p.inner.Name()
}

func (p *RetryProvider) Complete(ctx context.Context, req CompletionRequest) (CompletionResponse, error) {
	var lastErr error
	for attempt := 1; attempt <= p.cfg.MaxAttempts; attempt++ {
		resp, err := p.inner.Complete(ctx, req)
		if err == nil {
			return resp, nil
		}
		lastErr = err
		if attempt == p.cfg.MaxAttempts || !IsRetryable(err) {
			return CompletionResponse{}, err
		}
		delay := p.backoff(attempt)
		if sleepErr := p.sleep(ctx, delay); sleepErr != nil {
			return CompletionResponse{}, sleepErr
		}
	}
	return CompletionResponse{}, fmt.Errorf("llm retry exhausted: %w", lastErr)
}

func (p *RetryProvider) backoff(attempt int) time.Duration {
	delay := p.cfg.BaseDelay
	for i := 1; i < attempt; i++ {
		delay *= 2
		if delay >= p.cfg.MaxDelay {
			return p.cfg.MaxDelay
		}
	}
	return delay
}

func sleepContext(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
