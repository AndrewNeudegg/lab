package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
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

func (p *Gemini) Capabilities() ProviderCapabilities {
	return ProviderCapabilities{
		NativeJSONSchema:  true,
		SystemInstruction: true,
		MaxTokensField:    "maxOutputTokens",
	}
}

func (p *Gemini) Complete(ctx context.Context, req CompletionRequest) (CompletionResponse, error) {
	contents, systemInstruction := geminiContents(req.Messages)
	body, err := json.Marshal(geminiRequest{
		Contents:          contents,
		SystemInstruction: systemInstruction,
		GenerationConfig:  geminiGenerationConfigFor(req),
	})
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
			return CompletionResponse{}, RetryableAfter(err, RetryAfterHeader(resp.Header.Get("Retry-After"), time.Now()))
		}
		return CompletionResponse{}, err
	}
	var wire geminiResponse
	if err := json.NewDecoder(resp.Body).Decode(&wire); err != nil {
		return CompletionResponse{}, err
	}
	if len(wire.Candidates) == 0 {
		return CompletionResponse{}, geminiNoContentError(wire)
	}
	candidate := wire.Candidates[0]
	content := geminiTextContent(candidate.Content.Parts)
	if strings.TrimSpace(content) == "" {
		return CompletionResponse{}, geminiNoContentError(wire)
	}
	finishReason := strings.TrimSpace(candidate.FinishReason)
	if req.ResponseFormat != nil {
		if err := geminiStructuredFinishError(finishReason); err != nil {
			return CompletionResponse{}, err
		}
	}
	return CompletionResponse{
		Message:      Message{Role: "assistant", Content: content},
		FinishReason: finishReason,
		Usage: Usage{
			InputTokens:  wire.UsageMetadata.PromptTokenCount,
			OutputTokens: wire.UsageMetadata.CandidatesTokenCount,
			TotalTokens:  wire.UsageMetadata.TotalTokenCount,
		},
	}, nil
}

type geminiRequest struct {
	Contents          []geminiContent         `json:"contents"`
	SystemInstruction *geminiContent          `json:"systemInstruction,omitempty"`
	GenerationConfig  *geminiGenerationConfig `json:"generationConfig,omitempty"`
}

type geminiContent struct {
	Role  string       `json:"role,omitempty"`
	Parts []geminiPart `json:"parts"`
}

type geminiPart struct {
	Text string `json:"text"`
}

type geminiGenerationConfig struct {
	Temperature        *float64        `json:"temperature,omitempty"`
	MaxOutputTokens    int             `json:"maxOutputTokens,omitempty"`
	ResponseMIMEType   string          `json:"responseMimeType,omitempty"`
	ResponseJSONSchema json.RawMessage `json:"responseJsonSchema,omitempty"`
}

type geminiResponse struct {
	Candidates []struct {
		Content      geminiContent `json:"content"`
		FinishReason string        `json:"finishReason"`
	} `json:"candidates"`
	PromptFeedback struct {
		BlockReason string `json:"blockReason"`
	} `json:"promptFeedback"`
	UsageMetadata struct {
		PromptTokenCount     int `json:"promptTokenCount"`
		CandidatesTokenCount int `json:"candidatesTokenCount"`
		TotalTokenCount      int `json:"totalTokenCount"`
	} `json:"usageMetadata"`
}

func geminiTextContent(parts []geminiPart) string {
	var b strings.Builder
	for _, part := range parts {
		b.WriteString(part.Text)
	}
	return b.String()
}

func geminiNoContentError(wire geminiResponse) error {
	reason := strings.TrimSpace(wire.PromptFeedback.BlockReason)
	if reason == "" && len(wire.Candidates) > 0 {
		reason = strings.TrimSpace(wire.Candidates[0].FinishReason)
	}
	err := fmt.Errorf("gemini provider returned empty content")
	if reason != "" {
		err = fmt.Errorf("gemini provider returned empty content: %s", reason)
	}
	switch strings.ToUpper(reason) {
	case "SAFETY", "PROHIBITED_CONTENT", "BLOCKLIST", "RECITATION", "SPII":
		return err
	default:
		return Retryable(err)
	}
}

func geminiStructuredFinishError(reason string) error {
	switch strings.ToUpper(strings.TrimSpace(reason)) {
	case "", "STOP":
		return nil
	case "MAX_TOKENS":
		return Retryable(fmt.Errorf("gemini provider returned incomplete structured content: %s", reason))
	case "SAFETY", "PROHIBITED_CONTENT", "BLOCKLIST", "RECITATION", "SPII":
		return fmt.Errorf("gemini provider blocked structured content: %s", reason)
	default:
		return Retryable(fmt.Errorf("gemini provider returned unfinished structured content: %s", reason))
	}
}

func geminiGenerationConfigFor(req CompletionRequest) *geminiGenerationConfig {
	cfg := &geminiGenerationConfig{}
	temp := req.Temperature
	cfg.Temperature = &temp
	if req.MaxTokens > 0 {
		cfg.MaxOutputTokens = req.MaxTokens
	}
	if req.ResponseFormat != nil && len(req.ResponseFormat.Schema) > 0 {
		cfg.ResponseMIMEType = "application/json"
		cfg.ResponseJSONSchema = req.ResponseFormat.Schema
	}
	if cfg.Temperature == nil && cfg.MaxOutputTokens == 0 && cfg.ResponseMIMEType == "" && len(cfg.ResponseJSONSchema) == 0 {
		return nil
	}
	return cfg
}

func geminiContents(messages []Message) ([]geminiContent, *geminiContent) {
	contents := make([]geminiContent, 0, len(messages))
	var systemParts []geminiPart
	for _, msg := range messages {
		if msg.Role == "system" {
			systemParts = append(systemParts, geminiPart{Text: msg.Content})
			continue
		}
		role := "user"
		if msg.Role == "assistant" {
			role = "model"
		}
		contents = append(contents, geminiContent{Role: role, Parts: []geminiPart{{Text: msg.Content}}})
	}
	var systemInstruction *geminiContent
	if len(systemParts) > 0 {
		systemInstruction = &geminiContent{Parts: systemParts}
	}
	return contents, systemInstruction
}
