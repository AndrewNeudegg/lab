package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

type Gemini struct {
	name   string
	base   string
	apiKey string
	client *http.Client
}

func NewGemini(baseURL, apiKey string) *Gemini {
	return &Gemini{name: "gemini", base: strings.TrimRight(baseURL, "/"), apiKey: apiKey, client: http.DefaultClient}
}

func (p *Gemini) Name() string {
	return p.name
}

func (p *Gemini) Complete(ctx context.Context, req CompletionRequest) (CompletionResponse, error) {
	body, err := json.Marshal(geminiRequest{Contents: geminiContents(req.Messages)})
	if err != nil {
		return CompletionResponse{}, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.base+"/models/"+req.Model+":generateContent", bytes.NewReader(body))
	if err != nil {
		return CompletionResponse{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if p.apiKey != "" {
		httpReq.Header.Set("X-goog-api-key", p.apiKey)
	}
	resp, err := p.client.Do(httpReq)
	if err != nil {
		return CompletionResponse{}, Retryable(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		err := fmt.Errorf("gemini provider returned %s", resp.Status)
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			return CompletionResponse{}, Retryable(err)
		}
		return CompletionResponse{}, err
	}
	var wire geminiResponse
	if err := json.NewDecoder(resp.Body).Decode(&wire); err != nil {
		return CompletionResponse{}, err
	}
	if len(wire.Candidates) == 0 || len(wire.Candidates[0].Content.Parts) == 0 {
		return CompletionResponse{}, Retryable(fmt.Errorf("gemini provider returned no content"))
	}
	return CompletionResponse{
		Message: Message{Role: "assistant", Content: wire.Candidates[0].Content.Parts[0].Text},
		Usage: Usage{
			InputTokens:  wire.UsageMetadata.PromptTokenCount,
			OutputTokens: wire.UsageMetadata.CandidatesTokenCount,
			TotalTokens:  wire.UsageMetadata.TotalTokenCount,
		},
	}, nil
}

type geminiRequest struct {
	Contents []geminiContent `json:"contents"`
}

type geminiContent struct {
	Role  string       `json:"role,omitempty"`
	Parts []geminiPart `json:"parts"`
}

type geminiPart struct {
	Text string `json:"text"`
}

type geminiResponse struct {
	Candidates []struct {
		Content geminiContent `json:"content"`
	} `json:"candidates"`
	UsageMetadata struct {
		PromptTokenCount     int `json:"promptTokenCount"`
		CandidatesTokenCount int `json:"candidatesTokenCount"`
		TotalTokenCount      int `json:"totalTokenCount"`
	} `json:"usageMetadata"`
}

func geminiContents(messages []Message) []geminiContent {
	contents := make([]geminiContent, 0, len(messages))
	for _, msg := range messages {
		role := "user"
		if msg.Role == "assistant" {
			role = "model"
		}
		prefix := ""
		if msg.Role == "system" {
			prefix = "System instruction:\n"
		}
		contents = append(contents, geminiContent{Role: role, Parts: []geminiPart{{Text: prefix + msg.Content}}})
	}
	return contents
}
