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
	cmd := exec.CommandContext(ctx, "git", "-C", m.RepoRoot, "worktree", "add", "-b", branch, path, "HEAD")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", "", fmt.Errorf("git worktree add: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return path, branch, nil
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
		return fmt.Errorf("git worktree remove: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}
