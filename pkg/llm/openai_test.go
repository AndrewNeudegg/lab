package llm

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOpenAICompatibleUsesMaxCompletionTokensForOpenAI(t *testing.T) {
	skipIfNoLoopback(t)
	var payload map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"}}],"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}}`))
	}))
	defer server.Close()

	provider := NewOpenAICompatible("openai", server.URL+"/api.openai.com/v1", "test")
	provider.client = server.Client()

	resp, err := provider.Complete(context.Background(), CompletionRequest{Model: "gpt-5.1", MaxTokens: 16, Messages: []Message{{Role: "user", Content: "hi"}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := payload["max_tokens"]; ok {
		t.Fatalf("payload used max_tokens: %#v", payload)
	}
	if got := payload["max_completion_tokens"]; got != float64(16) {
		t.Fatalf("max_completion_tokens = %#v, want 16", got)
	}
	if resp.Usage.InputTokens != 3 || resp.Usage.OutputTokens != 2 || resp.Usage.TotalTokens != 5 {
		t.Fatalf("usage = %+v", resp.Usage)
	}
}

func TestOpenAICompatibleCapabilities(t *testing.T) {
	openai := NewOpenAICompatible("openai", "https://api.openai.com/v1", "test")
	openaiCaps := openai.Capabilities()
	if !openaiCaps.NativeJSONSchema || !openaiCaps.ToolCalling || !openaiCaps.SimultaneousToolsAndStructuredResponse {
		t.Fatalf("openai capabilities = %#v", openaiCaps)
	}
	if openaiCaps.MaxTokensField != "max_completion_tokens" {
		t.Fatalf("openai max tokens field = %q", openaiCaps.MaxTokensField)
	}
	local := NewOpenAICompatible("local", "http://127.0.0.1:11434/v1", "")
	localCaps := local.Capabilities()
	if localCaps.NativeJSONSchema || localCaps.ToolCalling {
		t.Fatalf("local capabilities = %#v, want no assumed structured output or tool calling", localCaps)
	}
	ollama := NewOllama("http://127.0.0.1:11434/v1")
	ollamaCaps := ollama.Capabilities()
	if !ollamaCaps.ToolCalling || ollamaCaps.NativeJSONSchema {
		t.Fatalf("ollama capabilities = %#v, want tool calling fallback without native JSON schema", ollamaCaps)
	}
}

func TestOpenAICompatibleSendsJSONSchemaResponseFormatToOpenAI(t *testing.T) {
	skipIfNoLoopback(t)
	var payload map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"{\"ok\":true}"}}]}`))
	}))
	defer server.Close()

	provider := NewOpenAICompatible("openai", server.URL+"/api.openai.com/v1", "test")
	provider.client = server.Client()

	_, err := provider.Complete(context.Background(), CompletionRequest{
		Model:    "gpt-5.1",
		Messages: []Message{{Role: "user", Content: "hi"}},
		ResponseFormat: &ResponseFormat{
			Name:        "test_response",
			Description: "test schema",
			Strict:      true,
			Schema:      json.RawMessage(`{"type":"object","required":["ok"],"properties":{"ok":{"type":"boolean"}},"additionalProperties":false}`),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	responseFormat, ok := payload["response_format"].(map[string]any)
	if !ok || responseFormat["type"] != "json_schema" {
		t.Fatalf("response_format = %#v, want json_schema", payload["response_format"])
	}
	jsonSchema, ok := responseFormat["json_schema"].(map[string]any)
	if !ok {
		t.Fatalf("json_schema = %#v", responseFormat["json_schema"])
	}
	if jsonSchema["name"] != "test_response" || jsonSchema["strict"] != true {
		t.Fatalf("json_schema metadata = %#v", jsonSchema)
	}
	schema, ok := jsonSchema["schema"].(map[string]any)
	if !ok || schema["type"] != "object" {
		t.Fatalf("schema = %#v, want object schema", jsonSchema["schema"])
	}
}

func TestOpenAICompatibleSendsToolsAndDecodesToolCalls(t *testing.T) {
	skipIfNoLoopback(t)
	var payload map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"","tool_calls":[{"id":"call_1","type":"function","function":{"name":"final.submit","arguments":"{\"message\":\"Done\"}"}}]}}]}`))
	}))
	defer server.Close()

	provider := NewOpenAICompatible("local", server.URL+"/v1", "")
	provider.client = server.Client()

	resp, err := provider.Complete(context.Background(), CompletionRequest{
		Model:    "local-model",
		Messages: []Message{{Role: "user", Content: "hi"}},
		Tools: []ToolSpec{{
			Name:        "final.submit",
			Description: "submit final answer",
			Schema:      json.RawMessage(`{"type":"object","required":["message"],"properties":{"message":{"type":"string"}}}`),
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	tools, ok := payload["tools"].([]any)
	if !ok || len(tools) != 1 {
		t.Fatalf("tools = %#v", payload["tools"])
	}
	toolDef, ok := tools[0].(map[string]any)
	if !ok || toolDef["type"] != "function" {
		t.Fatalf("tool def = %#v", tools[0])
	}
	function, ok := toolDef["function"].(map[string]any)
	if !ok || function["name"] != "final.submit" {
		t.Fatalf("function = %#v", toolDef["function"])
	}
	if payload["tool_choice"] != "auto" {
		t.Fatalf("tool_choice = %#v", payload["tool_choice"])
	}
	if len(resp.ToolCalls) != 1 || resp.ToolCalls[0].Name != "final.submit" || string(resp.ToolCalls[0].Args) != `{"message":"Done"}` {
		t.Fatalf("tool calls = %#v", resp.ToolCalls)
	}
}

func TestOpenAICompatibleDoesNotSendJSONSchemaToSelfHostedByDefault(t *testing.T) {
	skipIfNoLoopback(t)
	var payload map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`))
	}))
	defer server.Close()

	provider := NewOpenAICompatible("local", server.URL+"/v1", "")
	provider.client = server.Client()

	_, err := provider.Complete(context.Background(), CompletionRequest{
		Model:    "local-model",
		Messages: []Message{{Role: "user", Content: "hi"}},
		ResponseFormat: &ResponseFormat{
			Name:   "test_response",
			Schema: json.RawMessage(`{"type":"object"}`),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := payload["response_format"]; ok {
		t.Fatalf("self-hosted payload unexpectedly included response_format: %#v", payload)
	}
}

func skipIfNoLoopback(t *testing.T) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("loopback listener unavailable in this test environment: %v", err)
	}
	_ = ln.Close()
}
