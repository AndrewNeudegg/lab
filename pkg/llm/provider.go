package llm

import (
	"context"
	"encoding/json"
)

type Provider interface {
	Name() string
	Complete(ctx context.Context, req CompletionRequest) (CompletionResponse, error)
}

type ProviderCapabilities struct {
	NativeJSONSchema                       bool   `json:"native_json_schema,omitempty"`
	ToolCalling                            bool   `json:"tool_calling,omitempty"`
	SimultaneousToolsAndStructuredResponse bool   `json:"simultaneous_tools_and_structured_response,omitempty"`
	SystemMessages                         bool   `json:"system_messages,omitempty"`
	SystemInstruction                      bool   `json:"system_instruction,omitempty"`
	MaxTokensField                         string `json:"max_tokens_field,omitempty"`
}

type CapableProvider interface {
	Capabilities() ProviderCapabilities
}

func CapabilitiesOf(provider Provider) ProviderCapabilities {
	if provider == nil {
		return ProviderCapabilities{}
	}
	if capable, ok := provider.(CapableProvider); ok {
		return capable.Capabilities()
	}
	return ProviderCapabilities{
		SystemMessages: true,
		MaxTokensField: "max_tokens",
	}
}

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ToolSpec struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Schema      json.RawMessage `json:"schema"`
}

type ToolCall struct {
	ID   string          `json:"id,omitempty"`
	Name string          `json:"name"`
	Args json.RawMessage `json:"args"`
}

type ResponseFormat struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Schema      json.RawMessage `json:"schema"`
	Strict      bool            `json:"strict"`
}

type Usage struct {
	InputTokens  int `json:"input_tokens,omitempty"`
	OutputTokens int `json:"output_tokens,omitempty"`
	TotalTokens  int `json:"total_tokens,omitempty"`
}

type CompletionRequest struct {
	Model          string          `json:"model"`
	Messages       []Message       `json:"messages"`
	Tools          []ToolSpec      `json:"tools,omitempty"`
	Temperature    float64         `json:"temperature,omitempty"`
	MaxTokens      int             `json:"max_tokens,omitempty"`
	Metadata       json.RawMessage `json:"metadata,omitempty"`
	ResponseFormat *ResponseFormat `json:"response_format,omitempty"`
}

type CompletionResponse struct {
	Message   Message    `json:"message"`
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
	Usage     Usage      `json:"usage,omitempty"`
	Provider  string     `json:"provider,omitempty"`
	Model     string     `json:"model,omitempty"`
}
