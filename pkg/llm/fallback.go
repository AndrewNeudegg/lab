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
	var failures []string
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
		failures = append(failures, fmt.Sprintf("%s: %v", candidate.Name, err))
	}
	return CompletionResponse{}, fmt.Errorf("all LLM providers failed: %s", strings.Join(failures, "; "))
}
