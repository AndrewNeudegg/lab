package git

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/andrewneudegg/lab/pkg/tool"
)

func TestRegisterIncludesFullWorkflowTools(t *testing.T) {
	reg := tool.NewRegistry()
	if err := Register(reg, Base{}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{
		"git.status",
		"git.diff",
		"git.branch",
		"git.describe",
		"git.log",
		"git.show",
		"git.commit",
		"git.revert",
		"git.merge",
		"git.worktree_create",
		"git.worktree_remove",
		"git.merge_approved",
	} {
		if _, ok := reg.Get(name); !ok {
			t.Fatalf("expected registered tool %s", name)
		}
	}
}

func TestCommitDiffLogShowDescribeTools(t *testing.T) {
	ctx := context.Background()
	dir := initTestRepo(t)
	writeFile(t, dir, "app.txt", "one\n")

	raw, err := CommitTool{}.Run(ctx, mustRaw(t, map[string]any{
		"dir":     dir,
		"message": "initial commit",
		"all":     true,
	}))
	if err != nil {
		t.Fatalf("commit tool: %v", err)
	}
	if !strings.Contains(string(raw), "initial commit") {
		t.Fatalf("commit output = %s, want commit subject", raw)
	}

	writeFile(t, dir, "app.txt", "one\ntwo\n")
	raw, err = DiffTool{}.Run(ctx, mustRaw(t, map[string]any{
		"dir":   dir,
		"stat":  true,
		"paths": []string{"app.txt"},
	}))
	if err != nil {
		t.Fatalf("diff tool: %v", err)
	}
	if !strings.Contains(string(raw), "app.txt") {
		t.Fatalf("diff output = %s, want app.txt stat", raw)
	}

	if _, err := (CommitTool{}).Run(ctx, mustRaw(t, map[string]any{
		"dir":     dir,
		"message": "expand app",
		"all":     true,
	})); err != nil {
		t.Fatalf("second commit tool: %v", err)
	}
	raw, err = LogTool{}.Run(ctx, mustRaw(t, map[string]any{"dir": dir, "max_count": 2}))
	if err != nil {
		t.Fatalf("log tool: %v", err)
	}
	if !strings.Contains(string(raw), "expand app") || !strings.Contains(string(raw), "initial commit") {
		t.Fatalf("log output = %s, want recent commits", raw)
	}
	raw, err = ShowTool{}.Run(ctx, mustRaw(t, map[string]any{"dir": dir, "ref": "HEAD", "stat": true}))
	if err != nil {
		t.Fatalf("show tool: %v", err)
	}
	if !strings.Contains(string(raw), "expand app") || !strings.Contains(string(raw), "app.txt") {
		t.Fatalf("show output = %s, want commit stat", raw)
	}
	raw, err = DescribeTool{}.Run(ctx, mustRaw(t, map[string]any{"dir": dir, "always": true, "dirty": true}))
	if err != nil {
		t.Fatalf("describe tool: %v", err)
	}
	if strings.TrimSpace(string(raw)) == `{"output":""}` {
		t.Fatalf("describe output = %s, want ref description", raw)
	}
}

func TestMergeAndRevertTools(t *testing.T) {
	ctx := context.Background()
	dir := initTestRepo(t)
	writeFile(t, dir, "app.txt", "base\n")
	gitRun(t, dir, "add", "app.txt")
	gitRun(t, dir, "commit", "-m", "base")

	gitRun(t, dir, "checkout", "-b", "feature")
	writeFile(t, dir, "app.txt", "feature\n")
	gitRun(t, dir, "commit", "-am", "feature change")
	featureCommit := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "HEAD"))
	gitRun(t, dir, "checkout", "main")

	raw, err := MergeTool{}.Run(ctx, mustRaw(t, map[string]any{
		"dir":     dir,
		"branch":  "feature",
		"no_ff":   true,
		"message": "merge feature",
	}))
	if err != nil {
		t.Fatalf("merge tool: %v\n%s", err, raw)
	}
	if got := readFile(t, dir, "app.txt"); got != "feature\n" {
		t.Fatalf("merged content = %q, want feature", got)
	}

	raw, err = RevertTool{}.Run(ctx, mustRaw(t, map[string]any{
		"dir":       dir,
		"commit":    featureCommit,
		"no_commit": true,
	}))
	if err != nil {
		t.Fatalf("revert tool: %v\n%s", err, raw)
	}
	if got := readFile(t, dir, "app.txt"); got != "base\n" {
		t.Fatalf("reverted content = %q, want base", got)
	}
	status := gitOutput(t, dir, "status", "--short")
	if !strings.Contains(status, "M  app.txt") && !strings.Contains(status, "M app.txt") {
		t.Fatalf("status = %q, want staged or modified app.txt", status)
	}
}

func initTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	gitRun(t, dir, "init", "--initial-branch=main")
	gitRun(t, dir, "config", "user.email", "test@example.com")
	gitRun(t, dir, "config", "user.name", "Test User")
	return dir
}

func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	_ = gitOutput(t, dir, args...)
}

func gitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return string(out)
}

func writeFile(t *testing.T, dir, rel, content string) {
	t.Helper()
	path := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, dir, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, rel))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func mustRaw(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
