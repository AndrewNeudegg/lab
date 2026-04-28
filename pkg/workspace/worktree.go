package workspace

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type Manager struct {
	RepoRoot      string
	WorkspaceRoot string
}

func (m Manager) Create(ctx context.Context, taskID string) (string, string, error) {
	if taskID == "" || strings.Contains(taskID, "..") {
		return "", "", fmt.Errorf("invalid task id")
	}
	if err := os.MkdirAll(m.WorkspaceRoot, 0o755); err != nil {
		return "", "", err
	}
	branch := "homelabd/" + taskID
	path := filepath.Join(m.WorkspaceRoot, taskID)
	if _, err := os.Stat(path); err == nil {
		return path, branch, nil
	}
	if err := m.preflight(ctx); err != nil {
		return "", "", err
	}
	cmd := exec.CommandContext(ctx, "git", "-C", m.RepoRoot, "worktree", "add", "-b", branch, path, "HEAD")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", "", fmt.Errorf("git worktree add: %w: %s%s", err, strings.TrimSpace(string(out)), explainWorktreeAddFailure(string(out)))
	}
	return path, branch, nil
}

func (m Manager) preflight(ctx context.Context) error {
	repoRoot := filepath.Clean(m.RepoRoot)
	if repoRoot == "/nix/store" || strings.HasPrefix(repoRoot, "/nix/store/") {
		return fmt.Errorf("git worktrees require a writable checkout; repo root %q is inside immutable /nix/store", m.RepoRoot)
	}
	if err := verifyDirectoryWritable(m.WorkspaceRoot, "workspace root"); err != nil {
		return err
	}
	gitDir, err := gitCommonDir(ctx, m.RepoRoot)
	if err != nil {
		return err
	}
	if err := verifyDirectoryWritable(gitDir, "git common dir"); err != nil {
		return fmt.Errorf("%w%s", err, worktreeWritableHint(gitDir))
	}
	return nil
}

func gitCommonDir(ctx context.Context, repoRoot string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", repoRoot, "rev-parse", "--git-common-dir")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git rev-parse --git-common-dir: %w: %s", err, strings.TrimSpace(string(out)))
	}
	gitDir := strings.TrimSpace(string(out))
	if gitDir == "" {
		return "", fmt.Errorf("git rev-parse --git-common-dir returned an empty path")
	}
	if !filepath.IsAbs(gitDir) {
		gitDir = filepath.Join(repoRoot, gitDir)
	}
	return filepath.Clean(gitDir), nil
}

func verifyDirectoryWritable(dir, label string) error {
	probe, err := os.CreateTemp(dir, ".homelabd-write-test-*")
	if err != nil {
		return fmt.Errorf("%s %q is not writable: %w", label, dir, err)
	}
	name := probe.Name()
	if err := probe.Close(); err != nil {
		_ = os.Remove(name)
		return fmt.Errorf("%s %q write probe failed: %w", label, dir, err)
	}
	if err := os.Remove(name); err != nil {
		return fmt.Errorf("%s %q cleanup failed: %w", label, dir, err)
	}
	return nil
}

func explainWorktreeAddFailure(output string) string {
	if !strings.Contains(strings.ToLower(output), "read-only file system") {
		return ""
	}
	return "\nGit metadata is read-only. On NixOS, run homelabd/worktree creation from a writable checkout outside /nix/store and do not mount .git read-only in the agent sandbox."
}

func worktreeWritableHint(gitDir string) string {
	return fmt.Sprintf("\nGit worktree creation must write refs and metadata under %s. If an agent sandbox uses bubblewrap, bind this .git/common dir writable for the host-side homelabd process, or create the worktree before entering the sandbox.", gitDir)
}

func (m Manager) Remove(ctx context.Context, path string, force bool) error {
	if path == "" || !strings.HasPrefix(filepath.Clean(path), filepath.Clean(m.WorkspaceRoot)) {
		return fmt.Errorf("workspace path escapes workspace root")
	}
	args := []string{"-C", m.RepoRoot, "worktree", "remove"}
	if force {
		args = append(args, "--force")
	}
	args = append(args, path)
	cmd := exec.CommandContext(ctx, "git", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git worktree remove: %w: %s%s", err, strings.TrimSpace(string(out)), explainWorktreeAddFailure(string(out)))
	}
	return nil
}
