package llm

import "context"

type Anthropic struct{}

func (Anthropic) Name() string { return "anthropic" }

func (Anthropic) Complete(context.Context, CompletionRequest) (CompletionResponse, error) {
	return CompletionResponse{}, ErrProviderNotImplemented("anthropic provider is scaffolded but not enabled in v0.1")
}
