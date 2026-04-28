package memory

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	memstore "github.com/andrewneudegg/lab/pkg/memory"
	"github.com/andrewneudegg/lab/pkg/tool"
)

func TestMemoryToolsRememberListAndUnlearnLessons(t *testing.T) {
	store := memstore.NewStore(t.TempDir())
	registry := tool.NewRegistry()
	if err := Register(registry, store); err != nil {
		t.Fatal(err)
	}

	remember, ok := registry.Get("memory.remember")
	if !ok {
		t.Fatal("memory.remember not registered")
	}
	raw, err := remember.Run(context.Background(), json.RawMessage(`{"content":"Prefer explicit validation summaries.","kind":"preference"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "Prefer explicit validation summaries.") {
		t.Fatalf("remember result = %s, want lesson content", raw)
	}

	list, ok := registry.Get("memory.list")
	if !ok {
		t.Fatal("memory.list not registered")
	}
	raw, err = list.Run(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "Prefer explicit validation summaries.") {
		t.Fatalf("list result = %s, want remembered lesson", raw)
	}

	unlearn, ok := registry.Get("memory.unlearn")
	if !ok {
		t.Fatal("memory.unlearn not registered")
	}
	raw, err = unlearn.Run(context.Background(), json.RawMessage(`{"selector":"validation summaries"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "Prefer explicit validation summaries.") {
		t.Fatalf("unlearn result = %s, want removed lesson", raw)
	}
}
