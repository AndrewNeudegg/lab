package agent

import (
	"context"
	"encoding/json"
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
	BaseRef     string          `json:"base_ref,omitempty"`
	BaseLabel   string          `json:"base_label,omitempty"`
	HeadRef     string          `json:"head_ref,omitempty"`
	HeadLabel   string          `json:"head_label,omitempty"`
	Workspace   string          `json:"workspace,omitempty"`
	RawDiff     string          `json:"raw_diff"`
	Summary     TaskDiffSummary `json:"summary"`
	Files       []TaskDiffFile  `json:"files"`
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
	if remoteTask(t) {
		return TaskDiff{
			TaskID:      taskID,
			BaseLabel:   "remote agent",
			HeadLabel:   firstNonEmptyString(t.Target.AgentID, t.Target.Machine, "remote task"),
			Workspace:   t.Workspace,
			Files:       []TaskDiffFile{},
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
		GeneratedAt: time.Now().UTC(),
	}, nil
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
