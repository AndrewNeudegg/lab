package repo

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestSearchToolReturnsGrepLikeContextByDefault(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "pkg", "sample.go"), stringsJoinLines(
		"package sample",
		"",
		"func before() {}",
		"func target() {}",
		"func after() {}",
		"",
	))

	raw, err := SearchTool{base: Base{Root: root}}.Run(context.Background(), json.RawMessage(`{"query":"target"}`))
	if err != nil {
		t.Fatal(err)
	}

	var out struct {
		Matches []struct {
			Path    string `json:"path"`
			Line    int    `json:"line"`
			Text    string `json:"text"`
			Context []struct {
				Line  int    `json:"line"`
				Text  string `json:"text"`
				Match bool   `json:"match"`
			} `json:"context"`
		} `json:"matches"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Matches) != 1 {
		t.Fatalf("matches = %d, want 1: %s", len(out.Matches), raw)
	}
	match := out.Matches[0]
	if match.Path != filepath.Join("pkg", "sample.go") || match.Line != 4 || match.Text != "func target() {}" {
		t.Fatalf("match = %#v, want target line", match)
	}
	if len(match.Context) != 5 {
		t.Fatalf("context lines = %d, want 5: %#v", len(match.Context), match.Context)
	}
	if !match.Context[2].Match || match.Context[2].Line != 4 || match.Context[2].Text != "func target() {}" {
		t.Fatalf("middle context = %#v, want matched target line", match.Context[2])
	}
}

func TestSearchToolHonoursContextAndResultLimits(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "sample.txt"), stringsJoinLines(
		"needle one",
		"between",
		"needle two",
	))

	raw, err := SearchTool{base: Base{Root: root}}.Run(context.Background(), json.RawMessage(`{"query":"needle","context_lines":0,"max_results":1}`))
	if err != nil {
		t.Fatal(err)
	}

	var out struct {
		Matches []struct {
			Line    int `json:"line"`
			Context []struct {
				Line int `json:"line"`
			} `json:"context"`
		} `json:"matches"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Matches) != 1 {
		t.Fatalf("matches = %d, want max_results limit of 1", len(out.Matches))
	}
	if len(out.Matches[0].Context) != 0 {
		t.Fatalf("context lines = %d, want explicit zero", len(out.Matches[0].Context))
	}
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func stringsJoinLines(lines ...string) string {
	out := ""
	for _, line := range lines {
		out += line + "\n"
	}
	return out
}
