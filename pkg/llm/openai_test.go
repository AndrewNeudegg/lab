package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOpenAICompatibleUsesMaxCompletionTokensForOpenAI(t *testing.T) {
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
