package repo

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/andrewneudegg/lab/pkg/tool"
)

type Base struct {
	Root          string
	WorkspaceRoot string
	MaxFileBytes  int64
}

func Register(reg *tool.Registry, base Base) error {
	for _, t := range []tool.Tool{
		ListTool{base: base},
		ReadTool{base: base},
		SearchTool{base: base},
		WritePatchTool{base: base},
		CurrentDiffTool{base: base},
		ApplyPatchToMainTool{base: base},
		ResetWorkspaceTool{base: base},
	} {
		if err := reg.Register(t); err != nil {
			return err
		}
	}
	return nil
}

func schema(v string) json.RawMessage { return json.RawMessage(v) }

func safeJoin(root, rel string) (string, error) {
	if rel == "" {
		rel = "."
	}
	if filepath.IsAbs(rel) || strings.Contains(rel, "..") {
		return "", errors.New("unsafe repo path")
	}
	path := filepath.Clean(filepath.Join(root, rel))
	root = filepath.Clean(root)
	back, err := filepath.Rel(root, path)
	if err != nil || strings.HasPrefix(back, "..") {
		return "", errors.New("repo path escapes root")
	}
	return path, nil
}

func safeWorkspace(root, path string) error {
	if root == "" || path == "" {
		return errors.New("workspace root and path are required")
	}
	cleanRoot := filepath.Clean(root)
	cleanPath := filepath.Clean(path)
	rel, err := filepath.Rel(cleanRoot, cleanPath)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
		return errors.New("workspace path escapes workspace root")
	}
	return nil
}

func scopedRoot(base Base, workspace string) (string, error) {
	if workspace == "" {
		return base.Root, nil
	}
	if err := safeWorkspace(base.WorkspaceRoot, workspace); err != nil {
		return "", err
	}
	return filepath.Clean(workspace), nil
}

type ListTool struct{ base Base }

func (t ListTool) Name() string        { return "repo.list" }
func (t ListTool) Description() string { return "List files under the configured repository root." }
func (t ListTool) Schema() json.RawMessage {
	return schema(`{"type":"object","properties":{"path":{"type":"string"},"workspace":{"type":"string"}}}`)
}
func (t ListTool) Risk() tool.RiskLevel { return tool.RiskReadOnly }
func (t ListTool) Run(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
	var req struct {
		Path      string `json:"path"`
		Workspace string `json:"workspace"`
	}
	_ = json.Unmarshal(input, &req)
	rootBase, err := scopedRoot(t.base, req.Workspace)
	if err != nil {
		return nil, err
	}
	root, err := safeJoin(rootBase, req.Path)
	if err != nil {
		return nil, err
	}
	var files []string
	err = filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == root {
			return nil
		}
		if entry.IsDir() && (entry.Name() == ".git" || entry.Name() == "workspaces" || entry.Name() == "data") {
			return filepath.SkipDir
		}
		rel, _ := filepath.Rel(rootBase, path)
		files = append(files, rel)
		if len(files) >= 500 {
			return filepath.SkipAll
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			return nil
		}
	})
	if err != nil {
		return nil, err
	}
	return json.Marshal(map[string]any{"files": files, "workspace": req.Workspace})
}

type ReadTool struct{ base Base }

func (t ReadTool) Name() string { return "repo.read" }
func (t ReadTool) Description() string {
	return "Read a bounded file from the configured repository root or an isolated workspace."
}
func (t ReadTool) Schema() json.RawMessage {
	return schema(`{"type":"object","required":["path"],"properties":{"path":{"type":"string"},"workspace":{"type":"string"}}}`)
}
func (t ReadTool) Risk() tool.RiskLevel { return tool.RiskReadOnly }
func (t ReadTool) Run(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
	var req struct {
		Path      string `json:"path"`
		Workspace string `json:"workspace"`
	}
	if err := json.Unmarshal(input, &req); err != nil {
		return nil, err
	}
	rootBase, err := scopedRoot(t.base, req.Workspace)
	if err != nil {
		return nil, err
	}
	path, err := safeJoin(rootBase, req.Path)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	max := t.base.MaxFileBytes
	if max <= 0 {
		max = 1 << 20
	}
	if info.Size() > max {
		return nil, fmt.Errorf("file exceeds max read bytes")
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return json.Marshal(map[string]any{"path": req.Path, "workspace": req.Workspace, "content": string(b)})
}

type SearchTool struct{ base Base }

func (t SearchTool) Name() string { return "repo.search" }
func (t SearchTool) Description() string {
	return "Search text in repository or workspace files without invoking a shell."
}
func (t SearchTool) Schema() json.RawMessage {
	return schema(`{"type":"object","required":["query"],"properties":{"query":{"type":"string"},"path":{"type":"string"},"workspace":{"type":"string"}}}`)
}
func (t SearchTool) Risk() tool.RiskLevel { return tool.RiskReadOnly }
func (t SearchTool) Run(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
	var req struct {
		Query     string `json:"query"`
		Path      string `json:"path"`
		Workspace string `json:"workspace"`
	}
	if err := json.Unmarshal(input, &req); err != nil {
		return nil, err
	}
	if req.Query == "" {
		return nil, fmt.Errorf("query is required")
	}
	rootBase, err := scopedRoot(t.base, req.Workspace)
	if err != nil {
		return nil, err
	}
	root, err := safeJoin(rootBase, req.Path)
	if err != nil {
		return nil, err
	}
	var matches []map[string]any
	err = filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if entry.Name() == ".git" || entry.Name() == "workspaces" || entry.Name() == "data" {
				return filepath.SkipDir
			}
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil || bytes.IndexByte(b, 0) >= 0 {
			return nil
		}
		for i, line := range strings.Split(string(b), "\n") {
			if strings.Contains(line, req.Query) {
				rel, _ := filepath.Rel(rootBase, path)
				matches = append(matches, map[string]any{"path": rel, "line": i + 1, "text": line})
				if len(matches) >= 100 {
					return filepath.SkipAll
				}
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			return nil
		}
	})
	if err != nil {
		return nil, err
	}
	return json.Marshal(map[string]any{"matches": matches, "workspace": req.Workspace})
}

type WritePatchTool struct{ base Base }

func (WritePatchTool) Name() string { return "repo.write_patch" }
func (WritePatchTool) Description() string {
	return "Apply a unified diff to an isolated task workspace."
}
func (WritePatchTool) Schema() json.RawMessage {
	return schema(`{"type":"object","required":["workspace","patch"],"properties":{"workspace":{"type":"string"},"patch":{"type":"string"}}}`)
}
func (WritePatchTool) Risk() tool.RiskLevel { return tool.RiskMedium }
func (t WritePatchTool) Run(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
	var req struct {
		Workspace string `json:"workspace"`
		Patch     string `json:"patch"`
	}
	if err := json.Unmarshal(input, &req); err != nil {
		return nil, err
	}
	if req.Workspace == "" || req.Patch == "" {
		return nil, fmt.Errorf("workspace and patch are required")
	}
	if err := safeWorkspace(t.base.WorkspaceRoot, req.Workspace); err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, "git", "-C", req.Workspace, "apply", "--whitespace=nowarn", "-")
	cmd.Stdin = strings.NewReader(req.Patch)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("git apply: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return json.Marshal(map[string]any{"applied": true})
}

type CurrentDiffTool struct{ base Base }

func (CurrentDiffTool) Name() string { return "repo.current_diff" }
func (CurrentDiffTool) Description() string {
	return "Return git diff for a workspace, including untracked files."
}
func (CurrentDiffTool) Schema() json.RawMessage {
	return schema(`{"type":"object","required":["workspace"],"properties":{"workspace":{"type":"string"}}}`)
}
func (CurrentDiffTool) Risk() tool.RiskLevel { return tool.RiskReadOnly }
func (t CurrentDiffTool) Run(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
	var req struct {
		Workspace string `json:"workspace"`
	}
	if err := json.Unmarshal(input, &req); err != nil {
		return nil, err
	}
	if err := safeWorkspace(t.base.WorkspaceRoot, req.Workspace); err != nil {
		return nil, err
	}
	out, err := workspaceDiff(ctx, req.Workspace)
	if err != nil {
		return nil, err
	}
	return json.Marshal(map[string]any{"diff": out})
}

func workspaceDiff(ctx context.Context, workspace string) (string, error) {
	diffOut, err := exec.CommandContext(ctx, "git", "-C", workspace, "diff", "--", ".").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git diff: %w: %s", err, strings.TrimSpace(string(diffOut)))
	}
	var b strings.Builder
	b.Write(diffOut)
	untrackedOut, err := exec.CommandContext(ctx, "git", "-C", workspace, "ls-files", "--others", "--exclude-standard").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git ls-files untracked: %w: %s", err, strings.TrimSpace(string(untrackedOut)))
	}
	for _, rel := range strings.Split(string(untrackedOut), "\n") {
		rel = strings.TrimSpace(rel)
		if rel == "" || strings.HasPrefix(rel, ".codex") {
			continue
		}
		out, diffErr := exec.CommandContext(ctx, "git", "-C", workspace, "diff", "--no-index", "--", "/dev/null", rel).CombinedOutput()
		if diffErr != nil {
			var exitErr *exec.ExitError
			if !errors.As(diffErr, &exitErr) || exitErr.ExitCode() != 1 {
				return "", fmt.Errorf("git diff untracked %s: %w: %s", rel, diffErr, strings.TrimSpace(string(out)))
			}
		}
		b.Write(out)
	}
	return b.String(), nil
}

type ApplyPatchToMainTool struct{ base Base }

func (t ApplyPatchToMainTool) Name() string { return "repo.apply_patch_to_main" }
func (t ApplyPatchToMainTool) Description() string {
	return "Apply an approved patch to the configured repository root."
}
func (t ApplyPatchToMainTool) Schema() json.RawMessage {
	return schema(`{"type":"object","required":["patch"],"properties":{"patch":{"type":"string"},"target":{"type":"string"}}}`)
}
func (t ApplyPatchToMainTool) Risk() tool.RiskLevel { return tool.RiskHigh }
func (t ApplyPatchToMainTool) Run(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
	var req struct {
		Patch string `json:"patch"`
	}
	if err := json.Unmarshal(input, &req); err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, "git", "-C", t.base.Root, "apply", "--whitespace=nowarn", "-")
	cmd.Stdin = strings.NewReader(req.Patch)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("git apply main: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return json.Marshal(map[string]any{"applied": true})
}

type ResetWorkspaceTool struct{ base Base }

func (ResetWorkspaceTool) Name() string        { return "repo.reset_workspace" }
func (ResetWorkspaceTool) Description() string { return "Reset a task workspace to HEAD." }
func (ResetWorkspaceTool) Schema() json.RawMessage {
	return schema(`{"type":"object","required":["workspace"],"properties":{"workspace":{"type":"string"}}}`)
}
func (ResetWorkspaceTool) Risk() tool.RiskLevel { return tool.RiskMedium }
func (t ResetWorkspaceTool) Run(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
	var req struct {
		Workspace string `json:"workspace"`
	}
	if err := json.Unmarshal(input, &req); err != nil {
		return nil, err
	}
	if err := safeWorkspace(t.base.WorkspaceRoot, req.Workspace); err != nil {
		return nil, err
	}
	out, err := exec.CommandContext(ctx, "git", "-C", req.Workspace, "reset", "--hard", "HEAD").CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("git reset: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return json.Marshal(map[string]any{"reset": true})
}
