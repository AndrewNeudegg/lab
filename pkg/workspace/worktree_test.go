package workspace

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCreateRejectsNixStoreRepoRootBeforeGitMutation(t *testing.T) {
	manager := Manager{RepoRoot: "/nix/store/example-source", WorkspaceRoot: t.TempDir()}
	_, _, err := manager.Create(context.Background(), "task_123")
	if err == nil || !strings.Contains(err.Error(), "immutable /nix/store") {
		t.Fatalf("Create error = %v, want immutable /nix/store guidance", err)
	}
}

func TestExplainWorktreeAddFailureMentionsReadOnlyGitMetadata(t *testing.T) {
	hint := explainWorktreeAddFailure("fatal: cannot lock ref: Read-only file system")
	if !strings.Contains(hint, "Git metadata is read-only") || !strings.Contains(hint, "do not mount .git read-only") {
		t.Fatalf("hint = %q, want read-only .git guidance", hint)
	}
	if got := explainWorktreeAddFailure("fatal: branch already exists"); got != "" {
		t.Fatalf("hint = %q, want empty for unrelated git failure", got)
	}
}

func TestCreateAddsWorktreeFromWritableRepo(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is required for worktree integration coverage")
	}
	repo := t.TempDir()
	runGit(t, repo, "init")
	runGit(t, repo, "config", "user.email", "test@example.invalid")
	runGit(t, repo, "config", "user.name", "Test User")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("ok\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", "README.md")
	runGit(t, repo, "commit", "-m", "init")

	manager := Manager{RepoRoot: repo, WorkspaceRoot: filepath.Join(repo, "workspaces")}
	path, branch, err := manager.Create(context.Background(), "task_123")
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if branch != "homelabd/task_123" {
		t.Fatalf("branch = %q, want homelabd/task_123", branch)
	}
	if _, err := os.Stat(filepath.Join(path, "README.md")); err != nil {
		t.Fatalf("worktree missing README.md: %v", err)
	}
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
}
