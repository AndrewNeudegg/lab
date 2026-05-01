package agent

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
)

func (o *Orchestrator) validateToolArgSemantics(actor, taskID, name string, raw json.RawMessage) []string {
	if !taskWorkerActor(actor) || strings.TrimSpace(taskID) == "" {
		return nil
	}
	task, err := o.tasks.Load(taskID)
	if err != nil || strings.TrimSpace(task.Workspace) == "" {
		return nil
	}
	var args map[string]json.RawMessage
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil
	}
	var issues []string
	if schema, ok := o.toolSchema(name); ok {
		if _, ok := args["workspace"]; ok || workspaceRequiredForTaskTool(name, schema) {
			workspace, ok := rawStringArg(args, "workspace")
			if !ok || strings.TrimSpace(workspace) == "" {
				issues = append(issues, "args.workspace must be the task workspace")
			} else if !sameCleanPath(workspace, task.Workspace) {
				issues = append(issues, fmt.Sprintf("args.workspace must be the task workspace %q", task.Workspace))
			}
		}
		if _, ok := args["dir"]; ok || dirRequiredForTaskTool(name, schema) {
			dir, ok := rawStringArg(args, "dir")
			if !ok || strings.TrimSpace(dir) == "" {
				issues = append(issues, "args.dir must be inside the task workspace")
			} else if !pathInside(task.Workspace, dir) {
				issues = append(issues, fmt.Sprintf("args.dir must be inside the task workspace %q", task.Workspace))
			}
		}
	}
	return issues
}

func taskWorkerActor(actor string) bool {
	switch strings.TrimSpace(actor) {
	case "CoderAgent", "UXAgent":
		return true
	default:
		return false
	}
}

func workspaceRequiredForTaskTool(name string, schema json.RawMessage) bool {
	return strings.HasPrefix(name, "repo.") && schemaDeclaresProperty(schema, "workspace")
}

func dirRequiredForTaskTool(name string, schema json.RawMessage) bool {
	if strings.HasPrefix(name, "git.") || strings.HasPrefix(name, "go.") || strings.HasPrefix(name, "bun.") || name == "shell.run_limited" || name == "shell.run_chain" {
		return schemaDeclaresProperty(schema, "dir")
	}
	return false
}

func rawStringArg(args map[string]json.RawMessage, key string) (string, bool) {
	raw, ok := args[key]
	if !ok {
		return "", false
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", false
	}
	return value, true
}

func schemaDeclaresProperty(schema json.RawMessage, key string) bool {
	var root struct {
		Properties map[string]json.RawMessage `json:"properties"`
	}
	if err := json.Unmarshal(schema, &root); err != nil {
		return false
	}
	_, ok := root.Properties[key]
	return ok
}

func sameCleanPath(left, right string) bool {
	leftAbs, leftErr := filepath.Abs(strings.TrimSpace(left))
	rightAbs, rightErr := filepath.Abs(strings.TrimSpace(right))
	if leftErr != nil || rightErr != nil {
		return false
	}
	return filepath.Clean(leftAbs) == filepath.Clean(rightAbs)
}

func pathInside(root, candidate string) bool {
	rootAbs, rootErr := filepath.Abs(strings.TrimSpace(root))
	candidateAbs, candidateErr := filepath.Abs(strings.TrimSpace(candidate))
	if rootErr != nil || candidateErr != nil {
		return false
	}
	rootClean := filepath.Clean(rootAbs)
	candidateClean := filepath.Clean(candidateAbs)
	if rootClean == candidateClean {
		return true
	}
	rel, err := filepath.Rel(rootClean, candidateClean)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false
	}
	return true
}
