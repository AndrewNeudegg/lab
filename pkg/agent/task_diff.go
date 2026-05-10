package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/andrewneudegg/lab/pkg/task"
)

type TaskDiff struct {
	TaskID      string          `json:"task_id"`
	Source      string          `json:"source,omitempty"`
	Snapshot    bool            `json:"snapshot,omitempty"`
	BaseRef     string          `json:"base_ref,omitempty"`
	BaseLabel   string          `json:"base_label,omitempty"`
	HeadRef     string          `json:"head_ref,omitempty"`
	HeadLabel   string          `json:"head_label,omitempty"`
	Workspace   string          `json:"workspace,omitempty"`
	RawDiff     string          `json:"raw_diff"`
	Summary     TaskDiffSummary `json:"summary"`
	Files       []TaskDiffFile  `json:"files"`
	CapturedAt  *time.Time      `json:"captured_at,omitempty"`
	SHA256      string          `json:"sha256,omitempty"`
	Warning     string          `json:"warning,omitempty"`
	GeneratedAt time.Time       `json:"generated_at"`
}

type TaskDiffSummary struct {
	Files     int `json:"files"`
	Additions int `json:"additions"`
	Deletions int `json:"deletions"`
}

type TaskDiffFile struct {
	Path      string `json:"path"`
	OldPath   string `json:"old_path,omitempty"`
	Status    string `json:"status"`
	Additions int    `json:"additions"`
	Deletions int    `json:"deletions"`
	Binary    bool   `json:"binary,omitempty"`
}

func (o *Orchestrator) TaskDiff(ctx context.Context, selector string) (TaskDiff, error) {
	taskID, err := o.resolveTaskID(selector)
	if err != nil {
		return TaskDiff{}, err
	}
	t, err := o.tasks.Load(taskID)
	if err != nil {
		return TaskDiff{}, err
	}
	if t.DiffSnapshot != nil {
		return taskDiffFromSnapshot(taskID, *t.DiffSnapshot), nil
	}
	if remoteTask(t) {
		raw := strings.TrimSpace(t.RemoteDiff)
		workspace := firstNonEmptyString(t.Workspace)
		headLabel := "remote task"
		if t.Target != nil {
			workspace = firstNonEmptyString(t.Target.Workdir, t.Workspace)
			headLabel = firstNonEmptyString(t.Target.AgentID, t.Target.Machine, "remote task")
		}
		baseLabel := "remote agent"
		source := "remote_diff_unavailable"
		warning := ""
		if raw == "" && workspaceHasGit(workspace) {
			raw, err = workingTreeDiff(ctx, workspace)
			if err != nil {
				return TaskDiff{}, err
			}
			source = "remote_live_worktree_fallback"
			warning = "This diff was generated from the current remote checkout because no immutable task snapshot is stored; it may include later or earlier work."
		} else if raw != "" {
			source = "remote_completion_snapshot_legacy"
			warning = "This legacy remote diff was captured before task-scoped baselines were available; it may include earlier uncommitted remote work."
		}
		if raw == "no diff" {
			raw = ""
		}
		if raw != "" {
			baseLabel = "remote base"
		}
		files := diffFileSummaries(raw)
		if files == nil {
			files = []TaskDiffFile{}
		}
		additions, deletions := diffLineStats(raw)
		return TaskDiff{
			TaskID:      taskID,
			Source:      source,
			Snapshot:    raw != "" && source == "remote_completion_snapshot_legacy",
			BaseLabel:   baseLabel,
			HeadLabel:   headLabel,
			Workspace:   workspace,
			RawDiff:     raw,
			Summary:     TaskDiffSummary{Files: len(files), Additions: additions, Deletions: deletions},
			Files:       files,
			Warning:     warning,
			GeneratedAt: time.Now().UTC(),
		}, nil
	}
	raw, baseRef, baseLabel, headRef, headLabel, err := o.taskDiffRaw(ctx, t)
	if err != nil {
		return TaskDiff{}, err
	}
	if raw == "no diff" {
		raw = ""
	}
	files := diffFileSummaries(raw)
	if files == nil {
		files = []TaskDiffFile{}
	}
	additions, deletions := diffLineStats(raw)
	return TaskDiff{
		TaskID:      taskID,
		BaseRef:     baseRef,
		BaseLabel:   baseLabel,
		HeadRef:     headRef,
		HeadLabel:   headLabel,
		Workspace:   t.Workspace,
		RawDiff:     raw,
		Summary:     TaskDiffSummary{Files: len(files), Additions: additions, Deletions: deletions},
		Files:       files,
		Source:      "local_live_branch_diff",
		Warning:     "This diff was generated live from the current task workspace; store a review snapshot before merging to preserve history.",
		GeneratedAt: time.Now().UTC(),
	}, nil
}

func taskDiffFromSnapshot(taskID string, snapshot task.TaskDiffSnapshot) TaskDiff {
	raw := strings.TrimSpace(snapshot.RawDiff)
	if raw == "no diff" {
		raw = ""
	}
	files := make([]TaskDiffFile, 0, len(snapshot.Files))
	for _, file := range snapshot.Files {
		files = append(files, TaskDiffFile{
			Path:      file.Path,
			OldPath:   file.OldPath,
			Status:    file.Status,
			Additions: file.Additions,
			Deletions: file.Deletions,
			Binary:    file.Binary,
		})
	}
	if files == nil {
		files = []TaskDiffFile{}
	}
	capturedAt := snapshot.CapturedAt
	return TaskDiff{
		TaskID:      taskID,
		Source:      firstNonEmptyString(snapshot.Source, "task_diff_snapshot"),
		Snapshot:    true,
		BaseRef:     snapshot.BaseRef,
		BaseLabel:   snapshot.BaseLabel,
		HeadRef:     snapshot.HeadRef,
		HeadLabel:   snapshot.HeadLabel,
		Workspace:   snapshot.Workspace,
		RawDiff:     raw,
		Summary:     TaskDiffSummary{Files: snapshot.Summary.Files, Additions: snapshot.Summary.Additions, Deletions: snapshot.Summary.Deletions},
		Files:       files,
		CapturedAt:  &capturedAt,
		SHA256:      snapshot.SHA256,
		Warning:     snapshot.Warning,
		GeneratedAt: time.Now().UTC(),
	}
}

func buildTaskDiffSnapshot(raw, source, baseRef, baseLabel, headRef, headLabel, workspace, warning string, capturedAt time.Time) task.TaskDiffSnapshot {
	raw = strings.TrimSpace(raw)
	if raw == "no diff" {
		raw = ""
	}
	if capturedAt.IsZero() {
		capturedAt = time.Now().UTC()
	}
	files := diffFileSummaries(raw)
	if files == nil {
		files = []TaskDiffFile{}
	}
	additions, deletions := diffLineStats(raw)
	snapshotFiles := make([]task.TaskDiffSnapshotFile, 0, len(files))
	for _, file := range files {
		snapshotFiles = append(snapshotFiles, task.TaskDiffSnapshotFile{
			Path:      file.Path,
			OldPath:   file.OldPath,
			Status:    file.Status,
			Additions: file.Additions,
			Deletions: file.Deletions,
			Binary:    file.Binary,
		})
	}
	sum := sha256.Sum256([]byte(raw))
	return task.TaskDiffSnapshot{
		Source:    strings.TrimSpace(source),
		BaseRef:   strings.TrimSpace(baseRef),
		BaseLabel: strings.TrimSpace(baseLabel),
		HeadRef:   strings.TrimSpace(headRef),
		HeadLabel: strings.TrimSpace(headLabel),
		Workspace: strings.TrimSpace(workspace),
		RawDiff:   raw,
		Summary: task.TaskDiffSnapshotSummary{
			Files:     len(snapshotFiles),
			Additions: additions,
			Deletions: deletions,
		},
		Files:      snapshotFiles,
		CapturedAt: capturedAt,
		SHA256:     hex.EncodeToString(sum[:]),
		Warning:    strings.TrimSpace(warning),
	}
}

func (o *Orchestrator) taskDiffRaw(ctx context.Context, t task.Task) (raw, baseRef, baseLabel, headRef, headLabel string, err error) {
	if workspaceHasGit(t.Workspace) {
		baseRef, err = gitOutput(ctx, o.cfg.Repo.Root, "rev-parse", "HEAD")
		if err != nil {
			return "", "", "", "", "", fmt.Errorf("git rev-parse repo head: %w", err)
		}
		headRef, err = gitOutput(ctx, t.Workspace, "rev-parse", "HEAD")
		if err != nil {
			return "", "", "", "", "", fmt.Errorf("git rev-parse task head: %w", err)
		}
		baseLabel = gitLabel(ctx, o.cfg.Repo.Root, baseRef)
		headLabel = gitLabel(ctx, t.Workspace, headRef)
		raw, err = o.taskBranchDiff(ctx, t.Workspace)
		return raw, baseRef, baseLabel, headRef, headLabel, err
	}

	rawBytes, err := o.runTool(ctx, "CoderAgent", "repo.current_diff", map[string]any{"workspace": t.Workspace}, t.ID)
	if err != nil {
		return "", "", "", "", "", err
	}
	var out struct {
		Diff string `json:"diff"`
	}
	_ = json.Unmarshal(rawBytes, &out)
	return out.Diff, "", "workspace base", "", "workspace", nil
}

func workingTreeDiff(ctx context.Context, workspace string) (string, error) {
	workspace = strings.TrimSpace(workspace)
	if workspace == "" {
		return "", nil
	}
	diffOut, err := exec.CommandContext(ctx, "git", "-C", workspace, "diff", "--binary", "HEAD", "--", ".").CombinedOutput()
	if err != nil {
		diffOut, err = exec.CommandContext(ctx, "git", "-C", workspace, "diff", "--binary", "--", ".").CombinedOutput()
	}
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
		if skipUntrackedDiffPath(rel) {
			continue
		}
		out, diffErr := exec.CommandContext(ctx, "git", "-C", workspace, "diff", "--no-index", "--binary", "--", "/dev/null", rel).CombinedOutput()
		if diffErr != nil {
			var exitErr *exec.ExitError
			if !errors.As(diffErr, &exitErr) || exitErr.ExitCode() != 1 {
				return "", fmt.Errorf("git diff untracked %s: %w: %s", rel, diffErr, strings.TrimSpace(string(out)))
			}
		}
		b.Write(out)
	}
	if strings.TrimSpace(b.String()) == "" {
		return "no diff", nil
	}
	return b.String(), nil
}

func skipUntrackedDiffPath(rel string) bool {
	rel = filepath.ToSlash(strings.TrimSpace(rel))
	return rel == "" ||
		rel == ".codex" ||
		strings.HasPrefix(rel, ".codex/") ||
		strings.HasPrefix(rel, ".agent-")
}

func gitOutput(ctx context.Context, dir string, args ...string) (string, error) {
	out, err := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}

func gitLabel(ctx context.Context, dir, fallback string) string {
	label, err := gitOutput(ctx, dir, "branch", "--show-current")
	if err == nil && label != "" {
		return label
	}
	if len(fallback) > 12 {
		return fallback[:12]
	}
	return fallback
}

func diffFileSummaries(diff string) []TaskDiffFile {
	if strings.TrimSpace(diff) == "" {
		return nil
	}
	var files []TaskDiffFile
	current := TaskDiffFile{Status: "modified"}
	inFile := false
	inHunk := false

	finish := func() {
		if !inFile {
			return
		}
		if current.Path == "" {
			current.Path = current.OldPath
		}
		if current.Status == "" {
			current.Status = "modified"
		}
		files = append(files, current)
	}

	for _, line := range strings.Split(diff, "\n") {
		switch {
		case strings.HasPrefix(line, "diff --git "):
			finish()
			current = TaskDiffFile{Status: "modified"}
			inFile = true
			inHunk = false
			parts := strings.Fields(line)
			if len(parts) >= 4 {
				current.OldPath = cleanDiffPath(parts[2])
				current.Path = cleanDiffPath(parts[3])
			}
		case !inFile:
			continue
		case strings.HasPrefix(line, "new file mode"):
			current.Status = "added"
		case strings.HasPrefix(line, "deleted file mode"):
			current.Status = "deleted"
		case strings.HasPrefix(line, "rename from "):
			current.Status = "renamed"
			current.OldPath = strings.TrimSpace(strings.TrimPrefix(line, "rename from "))
		case strings.HasPrefix(line, "rename to "):
			current.Status = "renamed"
			current.Path = strings.TrimSpace(strings.TrimPrefix(line, "rename to "))
		case strings.HasPrefix(line, "Binary files ") || strings.HasPrefix(line, "GIT binary patch"):
			current.Binary = true
		case strings.HasPrefix(line, "--- "):
			oldPath := cleanDiffPath(strings.TrimSpace(strings.TrimPrefix(line, "--- ")))
			if oldPath == "/dev/null" {
				current.Status = "added"
			} else if oldPath != "" {
				current.OldPath = oldPath
			}
			inHunk = false
		case strings.HasPrefix(line, "+++ "):
			newPath := cleanDiffPath(strings.TrimSpace(strings.TrimPrefix(line, "+++ ")))
			if newPath == "/dev/null" {
				current.Status = "deleted"
			} else if newPath != "" {
				current.Path = newPath
			}
			inHunk = false
		case strings.HasPrefix(line, "@@ "):
			inHunk = true
		case inHunk && strings.HasPrefix(line, "+"):
			current.Additions++
		case inHunk && strings.HasPrefix(line, "-"):
			current.Deletions++
		}
	}
	finish()
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files
}

func cleanDiffPath(path string) string {
	path = strings.Trim(path, "\"")
	path = strings.TrimPrefix(path, "a/")
	path = strings.TrimPrefix(path, "b/")
	if path == "" {
		return ""
	}
	return filepath.Clean(path)
}
