package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
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

func (p *OpenAICompatible) Complete(ctx context.Context, req CompletionRequest) (CompletionResponse, error) {
	payload := map[string]any{
		"model":       req.Model,
		"messages":    req.Messages,
		"temperature": req.Temperature,
	}
	if req.MaxTokens > 0 {
		if strings.Contains(p.base, "api.openai.com") {
			payload["max_completion_tokens"] = req.MaxTokens
		} else {
			payload["max_tokens"] = req.MaxTokens
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
			return CompletionResponse{}, Retryable(err)
		}
		return CompletionResponse{}, err
	}
	var wire struct {
		Choices []struct {
			Message Message `json:"message"`
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
	return CompletionResponse{Message: wire.Choices[0].Message, Usage: usage}, nil
}
