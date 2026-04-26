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
		DescribeTool{},
		LogTool{},
		ShowTool{},
		CommitTool{},
		RevertTool{},
		MergeTool{},
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

func (DiffTool) Name() string { return "git.diff" }
func (DiffTool) Description() string {
	return "Run git diff in a repository or workspace, optionally for a ref range, staged changes, or summary output."
}
func (DiffTool) Schema() json.RawMessage {
	return schema(`{"type":"object","required":["dir"],"properties":{"dir":{"type":"string"},"base":{"type":"string"},"head":{"type":"string"},"staged":{"type":"boolean"},"stat":{"type":"boolean"},"name_status":{"type":"boolean"},"context_lines":{"type":"integer"},"paths":{"type":"array","items":{"type":"string"}}}}`)
}
func (DiffTool) Risk() tool.RiskLevel { return tool.RiskReadOnly }
func (DiffTool) Run(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
	var req struct {
		Dir          string   `json:"dir"`
		Base         string   `json:"base"`
		Head         string   `json:"head"`
		Staged       bool     `json:"staged"`
		Stat         bool     `json:"stat"`
		NameStatus   bool     `json:"name_status"`
		ContextLines int      `json:"context_lines"`
		Paths        []string `json:"paths"`
	}
	if err := json.Unmarshal(input, &req); err != nil {
		return nil, err
	}
	args := []string{"diff"}
	if req.Staged {
		args = append(args, "--cached")
	}
	if req.Stat {
		args = append(args, "--stat")
	}
	if req.NameStatus {
		args = append(args, "--name-status")
	}
	if req.ContextLines > 0 {
		args = append(args, fmt.Sprintf("--unified=%d", req.ContextLines))
	}
	if req.Base != "" {
		args = append(args, req.Base)
	}
	if req.Head != "" {
		args = append(args, req.Head)
	}
	paths, err := pathspecArgs(req.Paths, true)
	if err != nil {
		return nil, err
	}
	args = append(args, paths...)
	return runGit(ctx, req.Dir, args...)
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

type DescribeTool struct{}

func (DescribeTool) Name() string { return "git.describe" }
func (DescribeTool) Description() string {
	return "Run git describe to identify a ref by tag or abbreviated commit, with optional dirty marker."
}
func (DescribeTool) Schema() json.RawMessage {
	return schema(`{"type":"object","required":["dir"],"properties":{"dir":{"type":"string"},"ref":{"type":"string"},"tags":{"type":"boolean"},"dirty":{"type":"boolean"},"always":{"type":"boolean"}}}`)
}
func (DescribeTool) Risk() tool.RiskLevel { return tool.RiskReadOnly }
func (DescribeTool) Run(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
	var req struct {
		Dir    string `json:"dir"`
		Ref    string `json:"ref"`
		Tags   bool   `json:"tags"`
		Dirty  bool   `json:"dirty"`
		Always bool   `json:"always"`
	}
	if err := json.Unmarshal(input, &req); err != nil {
		return nil, err
	}
	args := []string{"describe"}
	if req.Tags {
		args = append(args, "--tags")
	}
	if req.Dirty {
		args = append(args, "--dirty")
	}
	if req.Always {
		args = append(args, "--always")
	}
	if req.Ref != "" {
		args = append(args, req.Ref)
	}
	return runGit(ctx, req.Dir, args...)
}

type LogTool struct{}

func (LogTool) Name() string { return "git.log" }
func (LogTool) Description() string {
	return "Show recent commits with one-line summaries, decorations, and optional ref selection."
}
func (LogTool) Schema() json.RawMessage {
	return schema(`{"type":"object","required":["dir"],"properties":{"dir":{"type":"string"},"ref":{"type":"string"},"max_count":{"type":"integer"},"paths":{"type":"array","items":{"type":"string"}}}}`)
}
func (LogTool) Risk() tool.RiskLevel { return tool.RiskReadOnly }
func (LogTool) Run(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
	var req struct {
		Dir      string   `json:"dir"`
		Ref      string   `json:"ref"`
		MaxCount int      `json:"max_count"`
		Paths    []string `json:"paths"`
	}
	if err := json.Unmarshal(input, &req); err != nil {
		return nil, err
	}
	maxCount := req.MaxCount
	if maxCount <= 0 {
		maxCount = 20
	}
	if maxCount > 100 {
		maxCount = 100
	}
	args := []string{"log", "--oneline", "--decorate", fmt.Sprintf("--max-count=%d", maxCount)}
	if req.Ref != "" {
		args = append(args, req.Ref)
	}
	paths, err := pathspecArgs(req.Paths, false)
	if err != nil {
		return nil, err
	}
	args = append(args, paths...)
	return runGit(ctx, req.Dir, args...)
}

type ShowTool struct{}

func (ShowTool) Name() string { return "git.show" }
func (ShowTool) Description() string {
	return "Show a commit, tag, or tree object with optional stat-only output."
}
func (ShowTool) Schema() json.RawMessage {
	return schema(`{"type":"object","required":["dir"],"properties":{"dir":{"type":"string"},"ref":{"type":"string"},"stat":{"type":"boolean"},"name_status":{"type":"boolean"}}}`)
}
func (ShowTool) Risk() tool.RiskLevel { return tool.RiskReadOnly }
func (ShowTool) Run(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
	var req struct {
		Dir        string `json:"dir"`
		Ref        string `json:"ref"`
		Stat       bool   `json:"stat"`
		NameStatus bool   `json:"name_status"`
	}
	if err := json.Unmarshal(input, &req); err != nil {
		return nil, err
	}
	ref := req.Ref
	if ref == "" {
		ref = "HEAD"
	}
	args := []string{"show"}
	if req.Stat {
		args = append(args, "--stat")
	}
	if req.NameStatus {
		args = append(args, "--name-status")
	}
	args = append(args, ref)
	return runGit(ctx, req.Dir, args...)
}

type CommitTool struct{}

func (CommitTool) Name() string { return "git.commit" }
func (CommitTool) Description() string {
	return "Create a git commit after optionally staging all changes or selected paths."
}
func (CommitTool) Schema() json.RawMessage {
	return schema(`{"type":"object","required":["dir","message"],"properties":{"dir":{"type":"string"},"message":{"type":"string"},"all":{"type":"boolean"},"allow_empty":{"type":"boolean"},"paths":{"type":"array","items":{"type":"string"}}}}`)
}
func (CommitTool) Risk() tool.RiskLevel { return tool.RiskHigh }
func (CommitTool) Run(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
	var req struct {
		Dir        string   `json:"dir"`
		Message    string   `json:"message"`
		All        bool     `json:"all"`
		AllowEmpty bool     `json:"allow_empty"`
		Paths      []string `json:"paths"`
	}
	if err := json.Unmarshal(input, &req); err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.Message) == "" {
		return nil, fmt.Errorf("message is required")
	}
	result := map[string]any{}
	if req.All || len(req.Paths) > 0 {
		addArgs := []string{"add"}
		if req.All {
			addArgs = append(addArgs, "-A")
		}
		paths, err := pathspecArgs(req.Paths, true)
		if err != nil {
			return nil, err
		}
		addArgs = append(addArgs, paths...)
		out, err := runGitOutput(ctx, req.Dir, addArgs...)
		result["add_output"] = out
		if err != nil {
			return nil, err
		}
	}
	commitArgs := []string{"commit"}
	if req.AllowEmpty {
		commitArgs = append(commitArgs, "--allow-empty")
	}
	commitArgs = append(commitArgs, "-m", req.Message)
	out, err := runGitOutput(ctx, req.Dir, commitArgs...)
	result["output"] = out
	if err != nil {
		return nil, err
	}
	return json.Marshal(result)
}

type RevertTool struct{}

func (RevertTool) Name() string { return "git.revert" }
func (RevertTool) Description() string {
	return "Revert a commit, optionally leaving the revert staged in the working tree."
}
func (RevertTool) Schema() json.RawMessage {
	return schema(`{"type":"object","required":["dir","commit"],"properties":{"dir":{"type":"string"},"commit":{"type":"string"},"no_commit":{"type":"boolean"},"mainline":{"type":"integer"}}}`)
}
func (RevertTool) Risk() tool.RiskLevel { return tool.RiskHigh }
func (RevertTool) Run(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
	var req struct {
		Dir      string `json:"dir"`
		Commit   string `json:"commit"`
		NoCommit bool   `json:"no_commit"`
		Mainline int    `json:"mainline"`
	}
	if err := json.Unmarshal(input, &req); err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.Commit) == "" {
		return nil, fmt.Errorf("commit is required")
	}
	args := []string{"revert"}
	if req.NoCommit {
		args = append(args, "--no-commit")
	}
	if req.Mainline > 0 {
		args = append(args, "-m", fmt.Sprint(req.Mainline))
	}
	args = append(args, req.Commit)
	return runGit(ctx, req.Dir, args...)
}

type MergeTool struct{}

func (MergeTool) Name() string { return "git.merge" }
func (MergeTool) Description() string {
	return "Merge a branch or commit into the current branch, with options for no-ff, squash, no-commit, and commit message."
}
func (MergeTool) Schema() json.RawMessage {
	return schema(`{"type":"object","required":["dir","branch"],"properties":{"dir":{"type":"string"},"branch":{"type":"string"},"no_ff":{"type":"boolean"},"squash":{"type":"boolean"},"no_commit":{"type":"boolean"},"message":{"type":"string"}}}`)
}
func (MergeTool) Risk() tool.RiskLevel { return tool.RiskHigh }
func (MergeTool) Run(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
	var req struct {
		Dir      string `json:"dir"`
		Branch   string `json:"branch"`
		NoFF     bool   `json:"no_ff"`
		Squash   bool   `json:"squash"`
		NoCommit bool   `json:"no_commit"`
		Message  string `json:"message"`
	}
	if err := json.Unmarshal(input, &req); err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.Branch) == "" {
		return nil, fmt.Errorf("branch is required")
	}
	args := []string{"merge"}
	if req.NoFF {
		args = append(args, "--no-ff")
	}
	if req.Squash {
		args = append(args, "--squash")
	}
	if req.NoCommit {
		args = append(args, "--no-commit")
	}
	if strings.TrimSpace(req.Message) != "" && !req.NoCommit && !req.Squash {
		args = append(args, "-m", req.Message)
	}
	args = append(args, req.Branch)
	return runGit(ctx, req.Dir, args...)
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
	addOut, err := exec.CommandContext(ctx, "git", "-C", workspacePath, "add", "-A", "--", ".").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git add workspace: %w: %s", err, strings.TrimSpace(string(addOut)))
	}
	cleanupOut, err := exec.CommandContext(ctx, "git", "-C", workspacePath, "reset", "--", ".codex").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git reset workspace metadata: %w: %s", err, strings.TrimSpace(string(cleanupOut)))
	}
	commitOut, err := exec.CommandContext(ctx, "git", "-C", workspacePath, "commit", "-m", message).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git commit workspace: %w: %s", err, strings.TrimSpace(string(commitOut)))
	}
	return string(commitOut), nil
}

func runGitOutput(ctx context.Context, dir string, args ...string) (string, error) {
	out, err := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

func pathspecArgs(paths []string, defaultDot bool) ([]string, error) {
	if len(paths) == 0 {
		if defaultDot {
			return []string{"--", "."}, nil
		}
		return nil, nil
	}
	args := []string{"--"}
	for _, path := range paths {
		if filepath.IsAbs(path) || strings.Contains(path, "..") {
			return nil, fmt.Errorf("unsafe pathspec %q", path)
		}
		args = append(args, path)
	}
	return args, nil
}
