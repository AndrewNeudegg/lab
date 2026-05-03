package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type OpenAICompatible struct {
	name   string
	base   string
	apiKey string
	client *http.Client
}

func NewOpenAICompatible(name, baseURL, apiKey string) *OpenAICompatible {
	return &OpenAICompatible{name: name, base: strings.TrimRight(baseURL, "/"), apiKey: apiKey, client: http.DefaultClient}
}

func (p *OpenAICompatible) Name() string {
	return p.name
}

func (p *OpenAICompatible) Capabilities() ProviderCapabilities {
	caps := ProviderCapabilities{
		SystemMessages: true,
		MaxTokensField: "max_tokens",
	}
	if p.officialOpenAI() {
		caps.NativeJSONSchema = true
		caps.ToolCalling = true
		caps.SimultaneousToolsAndStructuredResponse = true
		caps.MaxTokensField = "max_completion_tokens"
		return caps
	}
	if strings.EqualFold(p.name, "ollama") {
		caps.ToolCalling = true
	}
	return caps
}

func (p *OpenAICompatible) Complete(ctx context.Context, req CompletionRequest) (CompletionResponse, error) {
	payload := map[string]any{
		"model":       req.Model,
		"messages":    req.Messages,
		"temperature": req.Temperature,
	}
	if req.MaxTokens > 0 {
		if p.officialOpenAI() {
			payload["max_completion_tokens"] = req.MaxTokens
		} else {
			payload["max_tokens"] = req.MaxTokens
		}
	}
	if len(req.Tools) > 0 {
		payload["tools"] = openAITools(req.Tools)
		payload["tool_choice"] = "auto"
	}
	if req.ResponseFormat != nil && p.officialOpenAI() {
		jsonSchema := map[string]any{
			"name":   req.ResponseFormat.Name,
			"strict": req.ResponseFormat.Strict,
			"schema": json.RawMessage(req.ResponseFormat.Schema),
		}
		if req.ResponseFormat.Description != "" {
			jsonSchema["description"] = req.ResponseFormat.Description
		}
		payload["response_format"] = map[string]any{
			"type":        "json_schema",
			"json_schema": jsonSchema,
		}
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return CompletionResponse{}, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.base+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return CompletionResponse{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if p.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
	}
	resp, err := p.client.Do(httpReq)
	if err != nil {
		return CompletionResponse{}, Retryable(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		details, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		err := fmt.Errorf("llm provider returned %s: %s", resp.Status, strings.TrimSpace(string(details)))
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			return CompletionResponse{}, RetryableAfter(err, RetryAfterHeader(resp.Header.Get("Retry-After"), time.Now()))
		}
		return CompletionResponse{}, err
	}
	var wire struct {
		Choices []struct {
			Message struct {
				Role      string `json:"role"`
				Content   string `json:"content"`
				ToolCalls []struct {
					ID       string `json:"id"`
					Type     string `json:"type"`
					Function struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"message"`
		} `json:"choices"`
		Usage struct {
			InputTokens      int `json:"input_tokens"`
			OutputTokens     int `json:"output_tokens"`
			TotalTokens      int `json:"total_tokens"`
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
		} `json:"usage"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&wire); err != nil {
		return CompletionResponse{}, err
	}
	if len(wire.Choices) == 0 {
		return CompletionResponse{}, Retryable(fmt.Errorf("llm provider returned no choices"))
	}
	usage := Usage{
		InputTokens:  wire.Usage.InputTokens,
		OutputTokens: wire.Usage.OutputTokens,
		TotalTokens:  wire.Usage.TotalTokens,
	}
	if usage.InputTokens == 0 {
		usage.InputTokens = wire.Usage.PromptTokens
	}
	if usage.OutputTokens == 0 {
		usage.OutputTokens = wire.Usage.CompletionTokens
	}
	if usage.TotalTokens == 0 {
		usage.TotalTokens = usage.InputTokens + usage.OutputTokens
	}
	message := wire.Choices[0].Message
	return CompletionResponse{
		Message:   Message{Role: message.Role, Content: message.Content},
		ToolCalls: openAIToolCalls(message.ToolCalls),
		Usage:     usage,
	}, nil
}

func (p *OpenAICompatible) officialOpenAI() bool {
	return strings.Contains(p.base, "api.openai.com")
}

func openAITools(tools []ToolSpec) []map[string]any {
	out := make([]map[string]any, 0, len(tools))
	for _, tool := range tools {
		function := map[string]any{
			"name":        tool.Name,
			"description": tool.Description,
			"parameters":  json.RawMessage(tool.Schema),
		}
		out = append(out, map[string]any{
			"type":     "function",
			"function": function,
		})
	}
	return out
}

func openAIToolCalls(calls []struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}) []ToolCall {
	if len(calls) == 0 {
		return nil
	}
	out := make([]ToolCall, 0, len(calls))
	for _, call := range calls {
		if call.Type != "" && call.Type != "function" {
			continue
		}
		args := json.RawMessage(strings.TrimSpace(call.Function.Arguments))
		if len(args) == 0 || !json.Valid(args) {
			args = json.RawMessage(`{}`)
		}
		out = append(out, ToolCall{ID: call.ID, Name: call.Function.Name, Args: args})
	}
	return out
}
