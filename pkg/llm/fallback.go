package llm

import (
	"context"
	"fmt"
	"strings"
)

type ProviderCandidate struct {
	Name     string
	Model    string
	Provider Provider
}

type ProviderFailureClass string

const (
	ProviderFailurePermanent ProviderFailureClass = "permanent"
	ProviderFailureRetryable ProviderFailureClass = "retryable"
)

type ProviderFailure struct {
	Provider string
	Model    string
	Class    ProviderFailureClass
	Err      error
}

func (f ProviderFailure) Error() string {
	if f.Model != "" {
		return fmt.Sprintf("%s/%s %s: %v", f.Provider, f.Model, f.Class, f.Err)
	}
	return fmt.Sprintf("%s %s: %v", f.Provider, f.Class, f.Err)
}

func (f ProviderFailure) Unwrap() error {
	return f.Err
}

type AllProvidersFailedError struct {
	Failures []ProviderFailure
}

func (e AllProvidersFailedError) Error() string {
	failures := make([]string, 0, len(e.Failures))
	for _, failure := range e.Failures {
		failures = append(failures, failure.Error())
	}
	return "all LLM providers failed: " + strings.Join(failures, "; ")
}

func (e AllProvidersFailedError) Unwrap() []error {
	out := make([]error, 0, len(e.Failures))
	for _, failure := range e.Failures {
		out = append(out, failure)
	}
	return out
}

type FallbackProvider struct {
	candidates []ProviderCandidate
}

func NewFallbackProvider(candidates []ProviderCandidate) *FallbackProvider {
	filtered := make([]ProviderCandidate, 0, len(candidates))
	seen := map[string]bool{}
	for _, candidate := range candidates {
		if candidate.Provider == nil {
			continue
		}
		name := strings.TrimSpace(candidate.Name)
		if name == "" {
			name = candidate.Provider.Name()
		}
		if seen[name] {
			continue
		}
		seen[name] = true
		candidate.Name = name
		filtered = append(filtered, candidate)
	}
	return &FallbackProvider{candidates: filtered}
}

func (p *FallbackProvider) Name() string {
	if len(p.candidates) == 1 {
		return p.candidates[0].Name
	}
	names := make([]string, 0, len(p.candidates))
	for _, candidate := range p.candidates {
		names = append(names, candidate.Name)
	}
	return "fallback(" + strings.Join(names, "->") + ")"
}

func (p *FallbackProvider) Complete(ctx context.Context, req CompletionRequest) (CompletionResponse, error) {
	if len(p.candidates) == 0 {
		return CompletionResponse{}, fmt.Errorf("no LLM providers configured")
	}
	var failures []ProviderFailure
	for _, candidate := range p.candidates {
		candidateReq := req
		if candidate.Model != "" {
			candidateReq.Model = candidate.Model
		}
		resp, err := candidate.Provider.Complete(ctx, candidateReq)
		if err == nil {
			resp.Provider = candidate.Name
			resp.Model = candidateReq.Model
			return resp, nil
		}
		class := ProviderFailurePermanent
		if IsRetryable(err) {
			class = ProviderFailureRetryable
		}
		failures = append(failures, ProviderFailure{Provider: candidate.Name, Model: candidateReq.Model, Class: class, Err: err})
	}
	return CompletionResponse{}, AllProvidersFailedError{Failures: failures}
}
