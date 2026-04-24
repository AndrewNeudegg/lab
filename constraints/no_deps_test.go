package tests

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestGoModContainsNoRequireReplaceOrExclude(t *testing.T) {
	root := moduleRoot(t)
	b, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		t.Fatalf("read go.mod: %v", err)
	}
	lines := strings.Split(string(b), "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "require") {
			t.Fatalf("go.mod contains require on line %d", i+1)
		}
		if strings.HasPrefix(trimmed, "replace") {
			t.Fatalf("go.mod contains replace on line %d", i+1)
		}
		if strings.HasPrefix(trimmed, "exclude") {
			t.Fatalf("go.mod contains exclude on line %d", i+1)
		}
		if strings.HasPrefix(trimmed, "tool") {
			t.Fatalf("go.mod contains tool on line %d", i+1)
		}
	}
}

func TestGoListMAllReturnsOnlyMainModule(t *testing.T) {
	root := moduleRoot(t)
	cmd := exec.Command("go", "list", "-m", "all")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list -m all: %v", err)
	}
	lines := strings.Fields(strings.TrimSpace(string(out)))
	if len(lines) != 1 {
		t.Fatalf("expected 1 module, got %d: %q", len(lines), string(out))
	}
}

func moduleRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	cur := wd
	for {
		if _, err := os.Stat(filepath.Join(cur, "go.mod")); err == nil {
			return cur
		}
		next := filepath.Dir(cur)
		if next == cur {
			t.Fatalf("module root with go.mod not found from %s", wd)
		}
		cur = next
	}
}
