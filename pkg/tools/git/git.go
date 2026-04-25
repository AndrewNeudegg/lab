package git

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/andrewneudegg/lab/pkg/tool"
	"github.com/andrewneudegg/lab/pkg/workspace"
)

type Base struct {
	RepoRoot      string
	WorkspaceRoot string
}

func Register(reg *tool.Registry, base Base) error {
	for _, t := range []tool.Tool{
		StatusTool{},
		DiffTool{},
		BranchTool{},
		WorktreeCreateTool{manager: workspace.Manager{RepoRoot: base.RepoRoot, WorkspaceRoot: base.WorkspaceRoot}},
		WorktreeRemoveTool{manager: workspace.Manager{RepoRoot: base.RepoRoot, WorkspaceRoot: base.WorkspaceRoot}},
		MergeApprovedTool{repoRoot: base.RepoRoot},
	} {
		if err := reg.Register(t); err != nil {
			return err
		}
	}
	return nil
}

func schema(v string) json.RawMessage { return json.RawMessage(v) }

func runGit(ctx context.Context, dir string, args ...string) (json.RawMessage, error) {
	out, err := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return json.Marshal(map[string]any{"output": string(out)})
}

type StatusTool struct{}

func (StatusTool) Name() string        { return "git.status" }
func (StatusTool) Description() string { return "Run git status --short in a repository or workspace." }
func (StatusTool) Schema() json.RawMessage {
	return schema(`{"type":"object","required":["dir"],"properties":{"dir":{"type":"string"}}}`)
}
func (StatusTool) Risk() tool.RiskLevel { return tool.RiskReadOnly }
func (StatusTool) Run(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
	var req struct {
		Dir string `json:"dir"`
	}
	_ = json.Unmarshal(input, &req)
	return runGit(ctx, req.Dir, "status", "--short")
}

type DiffTool struct{}

func (DiffTool) Name() string        { return "git.diff" }
func (DiffTool) Description() string { return "Run git diff in a repository or workspace." }
func (DiffTool) Schema() json.RawMessage {
	return schema(`{"type":"object","required":["dir"],"properties":{"dir":{"type":"string"}}}`)
}
func (DiffTool) Risk() tool.RiskLevel { return tool.RiskReadOnly }
func (DiffTool) Run(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
	var req struct {
		Dir string `json:"dir"`
	}
	_ = json.Unmarshal(input, &req)
	return runGit(ctx, req.Dir, "diff", "--", ".")
}

type BranchTool struct{}

func (BranchTool) Name() string        { return "git.branch" }
func (BranchTool) Description() string { return "Show current git branch." }
func (BranchTool) Schema() json.RawMessage {
	return schema(`{"type":"object","required":["dir"],"properties":{"dir":{"type":"string"}}}`)
}
func (BranchTool) Risk() tool.RiskLevel { return tool.RiskReadOnly }
func (BranchTool) Run(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
	var req struct {
		Dir string `json:"dir"`
	}
	_ = json.Unmarshal(input, &req)
	return runGit(ctx, req.Dir, "branch", "--show-current")
}

type WorktreeCreateTool struct{ manager workspace.Manager }

func (WorktreeCreateTool) Name() string { return "git.worktree_create" }
func (WorktreeCreateTool) Description() string {
	return "Create an isolated git worktree and task branch."
}
func (WorktreeCreateTool) Schema() json.RawMessage {
	return schema(`{"type":"object","required":["task_id"],"properties":{"task_id":{"type":"string"}}}`)
}
func (WorktreeCreateTool) Risk() tool.RiskLevel { return tool.RiskLow }
func (t WorktreeCreateTool) Run(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
	var req struct {
		TaskID string `json:"task_id"`
	}
	if err := json.Unmarshal(input, &req); err != nil {
		return nil, err
	}
	path, branch, err := t.manager.Create(ctx, req.TaskID)
	if err != nil {
		return nil, err
	}
	return json.Marshal(map[string]any{"workspace": path, "branch": branch})
}

type WorktreeRemoveTool struct{ manager workspace.Manager }

func (WorktreeRemoveTool) Name() string        { return "git.worktree_remove" }
func (WorktreeRemoveTool) Description() string { return "Remove an isolated git worktree." }
func (WorktreeRemoveTool) Schema() json.RawMessage {
	return schema(`{"type":"object","required":["workspace"],"properties":{"workspace":{"type":"string"},"force":{"type":"boolean"}}}`)
}
func (WorktreeRemoveTool) Risk() tool.RiskLevel { return tool.RiskMedium }
func (t WorktreeRemoveTool) Run(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
	var req struct {
		Workspace string `json:"workspace"`
		Force     bool   `json:"force"`
	}
	if err := json.Unmarshal(input, &req); err != nil {
		return nil, err
	}
	if err := t.manager.Remove(ctx, req.Workspace, req.Force); err != nil {
		return nil, err
	}
	return json.Marshal(map[string]any{"removed": filepath.Clean(req.Workspace)})
}

type MergeApprovedTool struct{ repoRoot string }

func (MergeApprovedTool) Name() string { return "git.merge_approved" }
func (MergeApprovedTool) Description() string {
	return "Merge an approved task branch into the configured repository."
}
func (MergeApprovedTool) Schema() json.RawMessage {
	return schema(`{"type":"object","required":["branch","target"],"properties":{"branch":{"type":"string"},"target":{"type":"string"},"workspace":{"type":"string"},"message":{"type":"string"}}}`)
}
func (MergeApprovedTool) Risk() tool.RiskLevel { return tool.RiskHigh }
func (t MergeApprovedTool) Run(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
	var req struct {
		Branch    string `json:"branch"`
		Target    string `json:"target"`
		Workspace string `json:"workspace"`
		Message   string `json:"message"`
	}
	if err := json.Unmarshal(input, &req); err != nil {
		return nil, err
	}
	if req.Target == "" || req.Branch == "" {
		return nil, fmt.Errorf("branch and target are required")
	}
	if filepath.Clean(req.Target) != filepath.Clean(t.repoRoot) {
		return nil, fmt.Errorf("merge target does not match configured repo root")
	}
	commitOutput := ""
	if req.Workspace != "" {
		out, err := commitWorkspaceChanges(ctx, req.Workspace, req.Message)
		commitOutput = out
		if err != nil {
			return nil, err
		}
	}
	out, err := exec.CommandContext(ctx, "git", "-C", t.repoRoot, "merge", "--no-ff", req.Branch).CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("git merge: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return json.Marshal(map[string]any{"merged": req.Branch, "commit_output": commitOutput, "output": string(out)})
}

func commitWorkspaceChanges(ctx context.Context, workspacePath, message string) (string, error) {
	statusOut, err := exec.CommandContext(ctx, "git", "-C", workspacePath, "status", "--porcelain").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git status workspace: %w: %s", err, strings.TrimSpace(string(statusOut)))
	}
	if strings.TrimSpace(string(statusOut)) == "" {
		return "workspace has no changes to commit", nil
	}
	if message == "" {
		message = "Apply approved task changes"
	}
	addOut, err := exec.CommandContext(ctx, "git", "-C", workspacePath, "add", "-A").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git add workspace: %w: %s", err, strings.TrimSpace(string(addOut)))
	}
	commitOut, err := exec.CommandContext(ctx, "git", "-C", workspacePath, "commit", "-m", message).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git commit workspace: %w: %s", err, strings.TrimSpace(string(commitOut)))
	}
	return string(commitOut), nil
}
