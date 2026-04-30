package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGeminiSendsSystemInstructionAndJSONSchema(t *testing.T) {
	skipIfNoLoopback(t)
	var payload map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte(`{"candidates":[{"content":{"parts":[{"text":"{\"ok\":true}"}]}}],"usageMetadata":{"promptTokenCount":2,"candidatesTokenCount":3,"totalTokenCount":5}}`))
	}))
	defer server.Close()

	provider := NewGemini(server.URL, "test")
	provider.client = server.Client()

	resp, err := provider.Complete(context.Background(), CompletionRequest{
		Model:       "gemini-test",
		Temperature: 0,
		MaxTokens:   128,
		Messages: []Message{
			{Role: "system", Content: "Return structured data."},
			{Role: "user", Content: "hi"},
		},
		ResponseFormat: &ResponseFormat{
			Name:   "test_response",
			Schema: json.RawMessage(`{"type":"object","required":["ok"],"properties":{"ok":{"type":"boolean"}}}`),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Usage.InputTokens != 2 || resp.Usage.OutputTokens != 3 || resp.Usage.TotalTokens != 5 {
		t.Fatalf("usage = %+v", resp.Usage)
	}
	systemInstruction, ok := payload["systemInstruction"].(map[string]any)
	if !ok {
		t.Fatalf("systemInstruction missing from payload: %#v", payload)
	}
	systemParts, ok := systemInstruction["parts"].([]any)
	if !ok || len(systemParts) != 1 {
		t.Fatalf("systemInstruction parts = %#v", systemInstruction["parts"])
	}
	if part, ok := systemParts[0].(map[string]any); !ok || part["text"] != "Return structured data." {
		t.Fatalf("systemInstruction part = %#v", systemParts[0])
	}
	generationConfig, ok := payload["generationConfig"].(map[string]any)
	if !ok {
		t.Fatalf("generationConfig missing from payload: %#v", payload)
	}
	if generationConfig["responseMimeType"] != "application/json" {
		t.Fatalf("responseMimeType = %#v", generationConfig["responseMimeType"])
	}
	if generationConfig["maxOutputTokens"] != float64(128) {
		t.Fatalf("maxOutputTokens = %#v", generationConfig["maxOutputTokens"])
	}
	schema, ok := generationConfig["responseJsonSchema"].(map[string]any)
	if !ok || schema["type"] != "object" {
		t.Fatalf("responseJsonSchema = %#v", generationConfig["responseJsonSchema"])
	}
	contents, ok := payload["contents"].([]any)
	if !ok || len(contents) != 1 {
		t.Fatalf("contents = %#v", payload["contents"])
	}
	content, ok := contents[0].(map[string]any)
	if !ok || content["role"] != "user" {
		t.Fatalf("content = %#v", contents[0])
	}
}

func TestGeminiCapabilities(t *testing.T) {
	caps := NewGemini("https://generativelanguage.googleapis.com/v1beta", "test").Capabilities()
	if !caps.NativeJSONSchema || !caps.SystemInstruction {
		t.Fatalf("capabilities = %#v, want native JSON schema and system instruction", caps)
	}
	if caps.ToolCalling {
		t.Fatalf("capabilities = %#v, tool calling should not be assumed by this adapter", caps)
	}
	if caps.MaxTokensField != "maxOutputTokens" {
		t.Fatalf("max tokens field = %q", caps.MaxTokensField)
	}
}
