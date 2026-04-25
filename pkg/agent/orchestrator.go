package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/andrewneudegg/lab/pkg/config"
	"github.com/andrewneudegg/lab/pkg/eventlog"
	"github.com/andrewneudegg/lab/pkg/id"
	"github.com/andrewneudegg/lab/pkg/llm"
	taskstore "github.com/andrewneudegg/lab/pkg/task"
	"github.com/andrewneudegg/lab/pkg/tool"
	approvalstore "github.com/andrewneudegg/lab/pkg/tools/approval"
)

type Orchestrator struct {
	cfg       config.Config
	events    *eventlog.Store
	tasks     *taskstore.Store
	approvals *approvalstore.Store
	registry  *tool.Registry
	policy    tool.Policy
	provider  llm.Provider
	model     string
}

type agentResponse struct {
	Message   string             `json:"message"`
	Done      bool               `json:"done"`
	ToolCalls []proposedToolCall `json:"tool_calls"`
}

type proposedToolCall struct {
	Tool string          `json:"tool"`
	Name string          `json:"name,omitempty"`
	Args json.RawMessage `json:"args"`
}

type toolExecution struct {
	Tool          string          `json:"tool"`
	Allowed       bool            `json:"allowed"`
	NeedsApproval bool            `json:"needs_approval"`
	ApprovalID    string          `json:"approval_id,omitempty"`
	Result        json.RawMessage `json:"result,omitempty"`
	Error         string          `json:"error,omitempty"`
	Reason        string          `json:"reason,omitempty"`
}

func NewOrchestrator(cfg config.Config, events *eventlog.Store, tasks *taskstore.Store, approvals *approvalstore.Store, registry *tool.Registry, policy tool.Policy, provider llm.Provider, model string) *Orchestrator {
	return &Orchestrator{cfg: cfg, events: events, tasks: tasks, approvals: approvals, registry: registry, policy: policy, provider: provider, model: model}
}

func (o *Orchestrator) Handle(ctx context.Context, from, message string) (string, error) {
	message = strings.TrimSpace(message)
	if message == "" {
		return "empty message", nil
	}
	_ = o.events.Append(ctx, eventlog.Event{ID: id.New("evt"), Type: "user.message", Actor: from, Payload: eventlog.Payload(map[string]any{"message": message})})
	fields := strings.Fields(message)
	cmd := strings.ToLower(fields[0])
	if isCasualMessage(message) {
		return "I'm here. Use `help` for commands, or `new <goal>` to create a development task.", nil
	}
	switch cmd {
	case "help":
		return help(), nil
	case "new", "task":
		return o.createTask(ctx, strings.TrimSpace(strings.TrimPrefix(message, fields[0])))
	case "tasks":
		return o.listTasks()
	case "agents":
		return o.listAgents(ctx)
	case "cancel", "stop":
		if len(fields) < 2 {
			return "usage: cancel <task_id>", nil
		}
		return o.cancelTask(ctx, strings.Join(fields[1:], " "))
	case "delete", "remove", "rm":
		if len(fields) < 2 {
			return "usage: delete <task_id>", nil
		}
		return o.deleteTask(ctx, strings.Join(fields[1:], " "))
	case "show":
		if len(fields) < 2 {
			return "usage: show <task_id>", nil
		}
		return o.showTask(fields[1])
	case "read":
		if len(fields) < 2 {
			return "usage: read <repo_path>", nil
		}
		return o.readRepo(ctx, strings.Join(fields[1:], " "))
	case "search":
		if len(fields) < 2 {
			return "usage: search <text>", nil
		}
		return o.searchRepo(ctx, strings.Join(fields[1:], " "))
	case "patch":
		if len(fields) < 3 {
			return "usage: patch <task_id> <patch_file>", nil
		}
		return o.patchTask(ctx, fields[1], fields[2])
	case "test":
		if len(fields) < 2 {
			return "usage: test <task_id>", nil
		}
		return o.testTask(ctx, fields[1])
	case "diff":
		if len(fields) < 2 {
			return "usage: diff <task_id>", nil
		}
		return o.diffTask(ctx, fields[1])
	case "review":
		if len(fields) < 2 {
			return "usage: review <task_id>", nil
		}
		return o.reviewTask(ctx, fields[1])
	case "run", "work":
		if len(fields) < 2 {
			return "usage: run <task_id>", nil
		}
		return o.runCoderTask(ctx, fields[1])
	case "delegate", "escalate":
		if len(fields) < 4 {
			return "usage: delegate <task_id> <backend> <instruction>", nil
		}
		instruction := strings.TrimSpace(strings.Join(fields[3:], " "))
		return o.delegateTask(ctx, fields[1], fields[2], instruction)
	case "codex", "claude", "gemini":
		if len(fields) < 3 {
			return fmt.Sprintf("usage: %s <task_id> <instruction>", cmd), nil
		}
		instruction := strings.TrimSpace(strings.Join(fields[2:], " "))
		return o.delegateTask(ctx, fields[1], cmd, instruction)
	case "approvals":
		return o.listApprovals()
	case "approve":
		if len(fields) < 2 {
			return "usage: approve <approval_id>", nil
		}
		return o.resolveApproval(ctx, fields[1], true)
	case "deny":
		if len(fields) < 2 {
			return "usage: deny <approval_id>", nil
		}
		return o.resolveApproval(ctx, fields[1], false)
	default:
		return o.handleWithLLM(ctx, message)
	}
}

func isCasualMessage(message string) bool {
	normalized := strings.ToLower(strings.TrimSpace(strings.Trim(message, ".,!?")))
	switch normalized {
	case "hi", "hello", "hey", "yo", "andrew", "element-bot", "eelement-bot", "ping":
		return true
	default:
		return false
	}
}

func (o *Orchestrator) CreateTask(ctx context.Context, goal string) (string, error) {
	return o.createTask(ctx, goal)
}

func (o *Orchestrator) ListTasks() ([]taskstore.Task, error) {
	tasks, err := o.tasks.List()
	if err != nil {
		return nil, err
	}
	if tasks == nil {
		tasks = []taskstore.Task{}
	}
	sort.Slice(tasks, func(i, j int) bool { return tasks[i].CreatedAt.After(tasks[j].CreatedAt) })
	return tasks, nil
}

func (o *Orchestrator) LoadTask(taskID string) (taskstore.Task, error) {
	return o.tasks.Load(taskID)
}

func (o *Orchestrator) RunTask(ctx context.Context, taskID string) (string, error) {
	return o.runCoderTask(ctx, taskID)
}

func (o *Orchestrator) ReviewTask(ctx context.Context, taskID string) (string, error) {
	return o.reviewTask(ctx, taskID)
}

func (o *Orchestrator) ListApprovals() ([]approvalstore.Request, error) {
	requests, err := o.approvals.List()
	if err != nil {
		return nil, err
	}
	if requests == nil {
		requests = []approvalstore.Request{}
	}
	sort.Slice(requests, func(i, j int) bool { return requests[i].CreatedAt.After(requests[j].CreatedAt) })
	return requests, nil
}

func (o *Orchestrator) ResolveApproval(ctx context.Context, approvalID string, grant bool) (string, error) {
	return o.resolveApproval(ctx, approvalID, grant)
}

func (o *Orchestrator) ReadEvents(day time.Time) ([]eventlog.Event, error) {
	return o.events.ReadDay(day)
}

func help() string {
	return strings.Join([]string{
		"commands:",
		"  new <goal>                 create task and isolated worktree",
		"  tasks                      list tasks",
		"  show <task_id>             show task",
		"  cancel <task_id>           mark task cancelled, keep workspace",
		"  delete <task_id>           remove task record and worktree",
		"  read <repo_path>           inspect repo file",
		"  search <text>              search repo text",
		"  patch <task_id> <file>     apply unified diff to task worktree",
		"  test <task_id>             run go test ./... in worktree",
		"  diff <task_id>             show worktree diff",
		"  review <task_id>           run tests, show diff, request merge approval",
		"  run <task_id>              let CoderAgent work in the task worktree",
		"  agents                     list external worker backends",
		"  delegate <task_id> <agent> <instruction>",
		"                             run codex/claude/gemini in the task worktree",
		"  codex|claude|gemini <task_id> <instruction>",
		"                             shortcut for delegate <task_id> <agent> ...",
		"  approvals                  list approval requests",
		"  approve <approval_id>      execute approved action",
		"  deny <approval_id>         deny approved action",
	}, "\n")
}

func (o *Orchestrator) handleWithLLM(ctx context.Context, message string) (string, error) {
	if o.provider == nil || o.model == "" {
		return o.createTask(ctx, message)
	}
	maxToolCalls := o.cfg.Limits.MaxToolCallsPerTurn
	if maxToolCalls <= 0 {
		maxToolCalls = 12
	}
	messages := []llm.Message{
		{Role: "system", Content: o.llmToolPrompt()},
		{Role: "user", Content: message},
	}
	toolCallsUsed := 0
	lastMessage := ""
	for turn := 0; turn < 4; turn++ {
		req := llm.CompletionRequest{
			Model:       o.model,
			Temperature: 0,
			MaxTokens:   2048,
			Messages:    messages,
		}
		resp, err := o.provider.Complete(ctx, req)
		if err != nil {
			_ = o.events.Append(ctx, eventlog.Event{ID: id.New("evt"), Type: "agent.message", Actor: "OrchestratorAgent", Payload: eventlog.Payload(map[string]any{"provider": o.provider.Name(), "error": err.Error()})})
			return o.createTask(ctx, message)
		}
		_ = o.events.Append(ctx, eventlog.Event{ID: id.New("evt"), Type: "agent.message", Actor: "OrchestratorAgent", Payload: eventlog.Payload(map[string]any{"provider": o.provider.Name(), "message": resp.Message.Content, "usage": resp.Usage})})
		parsed, err := parseAgentResponse(resp.Message.Content)
		if err != nil {
			if strings.TrimSpace(resp.Message.Content) != "" {
				return strings.TrimSpace(resp.Message.Content), nil
			}
			return o.createTask(ctx, message)
		}
		if parsed.Message != "" {
			lastMessage = parsed.Message
		}
		if len(parsed.ToolCalls) == 0 || parsed.Done {
			if lastMessage == "" {
				lastMessage = "Done."
			}
			return lastMessage, nil
		}
		messages = append(messages, llm.Message{Role: "assistant", Content: mustJSON(parsed)})
		var results []toolExecution
		for _, call := range parsed.ToolCalls {
			if toolCallsUsed >= maxToolCalls {
				results = append(results, toolExecution{Tool: call.toolName(), Allowed: false, Error: "max tool calls reached"})
				continue
			}
			toolCallsUsed++
			result := o.executeProposedTool(ctx, "OrchestratorAgent", call, "")
			results = append(results, result)
			if result.NeedsApproval {
				return formatApprovalStop(lastMessage, result), nil
			}
		}
		messages = append(messages, llm.Message{Role: "user", Content: "Tool results:\n" + truncateForPrompt(mustJSON(results))})
	}
	if lastMessage == "" {
		lastMessage = "Stopped after reaching the turn limit."
	}
	return lastMessage, nil
}

func (o *Orchestrator) llmToolPrompt() string {
	return strings.Join([]string{
		"You are OrchestratorAgent for a local homelab development runtime.",
		"The Go runtime is the authority. You propose actions; tools execute only after policy validation.",
		"Respond with exactly one JSON object and no prose.",
		"Protocol:",
		`{"message":"short user-facing status","done":false,"tool_calls":[{"tool":"repo.search","args":{"query":"TODO"}}]}`,
		`{"message":"final answer","done":true,"tool_calls":[]}`,
		"Use tools for inspection before answering repo-specific questions.",
		"Create development work with task.create instead of pretending to edit files directly.",
		"Do not request dangerous or write tools unless the user clearly asked for that operation; approval may be required.",
		"Available tools:",
		o.toolCatalog(),
	}, "\n")
}

func (o *Orchestrator) toolCatalog() string {
	type catalogTool struct {
		Name        string          `json:"name"`
		Description string          `json:"description"`
		Risk        tool.RiskLevel  `json:"risk"`
		Schema      json.RawMessage `json:"schema"`
	}
	catalog := []catalogTool{{
		Name:        "task.create",
		Description: "Create a development task with an isolated git worktree. Args: {\"goal\":\"...\"}.",
		Risk:        tool.RiskLow,
		Schema:      json.RawMessage(`{"type":"object","required":["goal"],"properties":{"goal":{"type":"string"}}}`),
	}, {
		Name:        "task.run",
		Description: "Run CoderAgent on an existing task. Args: {\"task_id\":\"...\"}.",
		Risk:        tool.RiskLow,
		Schema:      json.RawMessage(`{"type":"object","required":["task_id"],"properties":{"task_id":{"type":"string"}}}`),
	}}
	for _, t := range o.registry.List() {
		catalog = append(catalog, catalogTool{Name: t.Name(), Description: t.Description(), Risk: t.Risk(), Schema: t.Schema()})
	}
	sort.Slice(catalog, func(i, j int) bool { return catalog[i].Name < catalog[j].Name })
	return mustJSON(catalog)
}

func parseAgentResponse(content string) (agentResponse, error) {
	var response agentResponse
	err := json.Unmarshal([]byte(extractJSON(content)), &response)
	return response, err
}

func (c proposedToolCall) toolName() string {
	if c.Tool != "" {
		return c.Tool
	}
	return c.Name
}

func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return `{"error":"json marshal failed"}`
	}
	return string(b)
}

func truncateForPrompt(s string) string {
	const max = 12000
	if len(s) <= max {
		return s
	}
	return s[:max] + "...[truncated]"
}

func formatApprovalStop(message string, result toolExecution) string {
	if message == "" {
		message = "A tool call requires approval."
	}
	return fmt.Sprintf("%s\nApproval requested: %s\nTool: %s\nReason: %s\nApprove with `approve %s` or deny with `deny %s`.", message, result.ApprovalID, result.Tool, result.Reason, result.ApprovalID, result.ApprovalID)
}

func extractJSON(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```") {
		s = strings.TrimPrefix(s, "```json")
		s = strings.TrimPrefix(s, "```")
		s = strings.TrimSuffix(s, "```")
		s = strings.TrimSpace(s)
	}
	if object := firstJSONObject(s); object != "" {
		return object
	}
	return s
}

func firstJSONObject(s string) string {
	start := strings.Index(s, "{")
	if start < 0 {
		return ""
	}
	depth := 0
	inString := false
	escaped := false
	for i := start; i < len(s); i++ {
		ch := s[i]
		if inString {
			if escaped {
				escaped = false
				continue
			}
			switch ch {
			case '\\':
				escaped = true
			case '"':
				inString = false
			}
			continue
		}
		switch ch {
		case '"':
			inString = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return s[start : i+1]
			}
		}
	}
	return ""
}

func (o *Orchestrator) createTask(ctx context.Context, goal string) (string, error) {
	if goal == "" {
		return "usage: new <goal>", nil
	}
	now := time.Now().UTC()
	t := taskstore.Task{ID: id.New("task"), Title: firstLine(goal), Goal: goal, Status: taskstore.StatusRunning, AssignedTo: "CoderAgent", Priority: 5, CreatedAt: now, UpdatedAt: now}
	raw, err := o.runTool(ctx, "OrchestratorAgent", "git.worktree_create", map[string]any{"task_id": t.ID}, t.ID)
	if err != nil {
		t.Status = taskstore.StatusFailed
		t.Result = err.Error()
		_ = o.tasks.Save(t)
		return "", err
	}
	var out struct {
		Workspace string `json:"workspace"`
		Branch    string `json:"branch"`
	}
	_ = json.Unmarshal(raw, &out)
	t.Workspace = out.Workspace
	if err := o.tasks.Save(t); err != nil {
		return "", err
	}
	_ = o.events.Append(ctx, eventlog.Event{ID: id.New("evt"), Type: "task.created", Actor: "OrchestratorAgent", TaskID: t.ID, Payload: eventlog.Payload(t)})
	_ = o.events.Append(ctx, eventlog.Event{ID: id.New("evt"), Type: "task.assigned", Actor: "OrchestratorAgent", TaskID: t.ID, Payload: eventlog.Payload(map[string]any{"agent": "CoderAgent"})})
	return fmt.Sprintf("Created task %s.\nWorkspace: %s\nBranch: %s\nNext: `run %s`, `delegate %s <agent> <instruction>`, or `review %s`.", t.ID, t.Workspace, out.Branch, t.ID, t.ID, t.ID), nil
}

func (o *Orchestrator) listTasks() (string, error) {
	tasks, err := o.tasks.List()
	if err != nil {
		return "", err
	}
	sort.Slice(tasks, func(i, j int) bool { return tasks[i].CreatedAt.After(tasks[j].CreatedAt) })
	if len(tasks) == 0 {
		return "no tasks", nil
	}
	var b strings.Builder
	for _, t := range tasks {
		fmt.Fprintf(&b, "%s [%s] %s workspace=%s\n", t.ID, t.Status, t.Title, t.Workspace)
	}
	return strings.TrimSpace(b.String()), nil
}

func (o *Orchestrator) showTask(taskID string) (string, error) {
	t, err := o.tasks.Load(taskID)
	if err != nil {
		return "", err
	}
	b, _ := json.MarshalIndent(t, "", "  ")
	return string(b), nil
}

func (o *Orchestrator) cancelTask(ctx context.Context, selector string) (string, error) {
	taskID, err := o.resolveTaskID(selector)
	if err != nil {
		return "", err
	}
	t, err := o.tasks.Load(taskID)
	if err != nil {
		return "", err
	}
	if t.Status == taskstore.StatusDone || t.Status == taskstore.StatusCancelled {
		return fmt.Sprintf("Task %s is already %s.", taskID, t.Status), nil
	}
	t.Status = taskstore.StatusCancelled
	t.AssignedTo = "OrchestratorAgent"
	t.Result = "cancelled by human"
	if err := o.tasks.Save(t); err != nil {
		return "", err
	}
	_ = o.events.Append(ctx, eventlog.Event{ID: id.New("evt"), Type: "task.cancelled", Actor: "human", TaskID: taskID, Payload: eventlog.Payload(map[string]any{"workspace": t.Workspace})})
	return fmt.Sprintf("Cancelled %s. Workspace kept at %s.", taskID, t.Workspace), nil
}

func (o *Orchestrator) deleteTask(ctx context.Context, selector string) (string, error) {
	taskID, err := o.resolveTaskID(selector)
	if err != nil {
		return "", err
	}
	t, err := o.tasks.Load(taskID)
	if err != nil {
		return "", err
	}
	removedWorkspace := false
	if t.Workspace != "" {
		if _, err := o.runTool(ctx, "OrchestratorAgent", "git.worktree_remove", map[string]any{"workspace": t.Workspace, "force": true}, taskID); err != nil {
			return "", err
		}
		removedWorkspace = true
	}
	if err := o.tasks.Delete(taskID); err != nil {
		return "", err
	}
	_ = o.events.Append(ctx, eventlog.Event{ID: id.New("evt"), Type: "task.deleted", Actor: "human", TaskID: taskID, Payload: eventlog.Payload(map[string]any{"workspace": t.Workspace, "removed_workspace": removedWorkspace})})
	if removedWorkspace {
		return fmt.Sprintf("Deleted %s and removed workspace %s.", taskID, t.Workspace), nil
	}
	return "Deleted " + taskID + ".", nil
}

func (o *Orchestrator) resolveTaskID(selector string) (string, error) {
	selector = strings.TrimSpace(selector)
	if selector == "" {
		return "", fmt.Errorf("task id or title is required")
	}
	if _, err := o.tasks.Load(selector); err == nil {
		return selector, nil
	}
	tasks, err := o.tasks.List()
	if err != nil {
		return "", err
	}
	needle := normalizeTaskSelector(selector)
	var matches []taskstore.Task
	for _, t := range tasks {
		if normalizeTaskSelector(t.ID) == needle || normalizeTaskSelector(t.Title) == needle {
			matches = append(matches, t)
			continue
		}
		if needle != "" && strings.Contains(normalizeTaskSelector(t.Title), needle) {
			matches = append(matches, t)
		}
	}
	if len(matches) == 1 {
		return matches[0].ID, nil
	}
	if len(matches) > 1 {
		var ids []string
		for _, t := range matches {
			ids = append(ids, t.ID)
		}
		sort.Strings(ids)
		return "", fmt.Errorf("task selector %q is ambiguous: %s", selector, strings.Join(ids, ", "))
	}
	return "", fmt.Errorf("no task matches %q", selector)
}

func normalizeTaskSelector(s string) string {
	words := strings.Fields(strings.ToLower(strings.TrimSpace(strings.Trim(s, ".,!?"))))
	filtered := words[:0]
	for _, word := range words {
		switch word {
		case "the", "a", "an", "task":
			continue
		default:
			filtered = append(filtered, word)
		}
	}
	return strings.Join(filtered, " ")
}

func (o *Orchestrator) listAgents(ctx context.Context) (string, error) {
	raw, err := o.runTool(ctx, "OrchestratorAgent", "agent.list", map[string]any{}, "")
	if err != nil {
		return "", err
	}
	var out struct {
		Agents []struct {
			Name        string   `json:"name"`
			Enabled     bool     `json:"enabled"`
			Available   bool     `json:"available"`
			Command     string   `json:"command"`
			Args        []string `json:"args"`
			Description string   `json:"description"`
		} `json:"agents"`
	}
	_ = json.Unmarshal(raw, &out)
	if len(out.Agents) == 0 {
		return "no external agents configured", nil
	}
	var b strings.Builder
	for _, a := range out.Agents {
		status := "unavailable"
		if a.Available {
			status = "available"
		} else if !a.Enabled {
			status = "disabled"
		}
		command := strings.TrimSpace(strings.Join(append([]string{a.Command}, a.Args...), " "))
		if command == "" {
			command = "not configured"
		}
		fmt.Fprintf(&b, "%s [%s] %s\n  command: %s\n", a.Name, status, a.Description, command)
	}
	return strings.TrimSpace(b.String()), nil
}

func (o *Orchestrator) delegateTask(ctx context.Context, taskID, backend, instruction string) (string, error) {
	t, err := o.tasks.Load(taskID)
	if err != nil {
		return "", err
	}
	if t.Workspace == "" {
		return "", fmt.Errorf("task %s has no workspace", taskID)
	}
	if strings.TrimSpace(instruction) == "" {
		return "usage: delegate <task_id> <backend> <instruction>", nil
	}
	t.Status = taskstore.StatusRunning
	t.AssignedTo = backend
	if err := o.tasks.Save(t); err != nil {
		return "", err
	}
	_ = o.events.Append(ctx, eventlog.Event{ID: id.New("evt"), Type: "task.assigned", Actor: "OrchestratorAgent", TaskID: taskID, Payload: eventlog.Payload(map[string]any{"agent": backend})})
	raw, err := o.runTool(ctx, "OrchestratorAgent", "agent.delegate", map[string]any{
		"backend":     backend,
		"task_id":     taskID,
		"workspace":   t.Workspace,
		"instruction": instruction,
	}, taskID)
	var out struct {
		ID       string   `json:"id"`
		Backend  string   `json:"backend"`
		Command  []string `json:"command"`
		Output   string   `json:"output"`
		Error    string   `json:"error"`
		Duration int64    `json:"duration"`
	}
	_ = json.Unmarshal(raw, &out)
	t.AssignedTo = "OrchestratorAgent"
	t.Result = strings.TrimSpace(out.Output)
	if err != nil {
		t.Status = taskstore.StatusBlocked
		if out.Error != "" {
			t.Result = out.Error
		} else {
			t.Result = err.Error()
		}
		_ = o.tasks.Save(t)
		if strings.TrimSpace(out.Output) != "" {
			return strings.TrimSpace(out.Output), err
		}
		return "", err
	}
	t.Status = taskstore.StatusRunning
	_ = o.tasks.Save(t)
	output := strings.TrimSpace(out.Output)
	if output == "" {
		output = "external agent returned no output"
	}
	if len(output) > 6000 {
		output = output[:6000] + "\n...[truncated]"
	}
	return fmt.Sprintf("%s finished for %s.\nNext: `diff %s`, `test %s`, or `review %s`.\n\n%s", out.Backend, taskID, taskID, taskID, taskID, output), nil
}

func (o *Orchestrator) readRepo(ctx context.Context, path string) (string, error) {
	raw, err := o.runTool(ctx, "OrchestratorAgent", "repo.read", map[string]any{"path": path}, "")
	if err != nil {
		return "", err
	}
	var out struct {
		Content string `json:"content"`
	}
	_ = json.Unmarshal(raw, &out)
	return out.Content, nil
}

func (o *Orchestrator) searchRepo(ctx context.Context, query string) (string, error) {
	raw, err := o.runTool(ctx, "OrchestratorAgent", "repo.search", map[string]any{"query": query}, "")
	if err != nil {
		return "", err
	}
	var out struct {
		Matches []map[string]any `json:"matches"`
	}
	_ = json.Unmarshal(raw, &out)
	if len(out.Matches) == 0 {
		return "no matches", nil
	}
	var b strings.Builder
	for _, m := range out.Matches {
		fmt.Fprintf(&b, "%s:%v: %s\n", m["path"], m["line"], m["text"])
	}
	return strings.TrimSpace(b.String()), nil
}

func (o *Orchestrator) patchTask(ctx context.Context, taskID, patchFile string) (string, error) {
	t, err := o.tasks.Load(taskID)
	if err != nil {
		return "", err
	}
	patchBytes, err := os.ReadFile(patchFile)
	if err != nil {
		return "", err
	}
	if _, err := o.runTool(ctx, "CoderAgent", "repo.write_patch", map[string]any{"workspace": t.Workspace, "patch": string(patchBytes)}, taskID); err != nil {
		return "", err
	}
	_ = o.events.Append(ctx, eventlog.Event{ID: id.New("evt"), Type: "repo.patch.created", Actor: "CoderAgent", TaskID: taskID, Payload: eventlog.Payload(map[string]any{"patch_file": patchFile})})
	return "Patch applied to workspace " + t.Workspace, nil
}

func (o *Orchestrator) testTask(ctx context.Context, taskID string) (string, error) {
	t, err := o.tasks.Load(taskID)
	if err != nil {
		return "", err
	}
	raw, err := o.runTool(ctx, "CoderAgent", "go.test", map[string]any{"dir": t.Workspace}, taskID)
	var out struct {
		Output string `json:"output"`
	}
	_ = json.Unmarshal(raw, &out)
	if err != nil {
		return out.Output, err
	}
	return strings.TrimSpace(out.Output), nil
}

func (o *Orchestrator) diffTask(ctx context.Context, taskID string) (string, error) {
	t, err := o.tasks.Load(taskID)
	if err != nil {
		return "", err
	}
	raw, err := o.runTool(ctx, "CoderAgent", "repo.current_diff", map[string]any{"workspace": t.Workspace}, taskID)
	if err != nil {
		return "", err
	}
	var out struct {
		Diff string `json:"diff"`
	}
	_ = json.Unmarshal(raw, &out)
	if out.Diff == "" {
		return "no diff", nil
	}
	return out.Diff, nil
}

func (o *Orchestrator) reviewTask(ctx context.Context, taskID string) (string, error) {
	testOut, testErr := o.testTask(ctx, taskID)
	diffOut, diffErr := o.diffTask(ctx, taskID)
	if diffErr != nil {
		return "", diffErr
	}
	t, err := o.tasks.Load(taskID)
	if err != nil {
		return "", err
	}
	if diffOut == "no diff" {
		return "ReviewerAgent: no diff to approve.", nil
	}
	status := "pass"
	if testErr != nil {
		status = "fail"
	}
	approvalID := id.New("approval")
	args := eventlog.Payload(map[string]any{"branch": "homelabd/" + taskID, "target": o.cfg.Repo.Root, "workspace": t.Workspace, "message": "Apply " + taskID})
	req := approvalstore.Request{ID: approvalID, TaskID: taskID, Tool: "git.merge_approved", Args: args, Reason: "merge reviewed task branch into repo root", Status: approvalstore.StatusPending}
	if err := o.approvals.Save(req); err != nil {
		return "", err
	}
	t.Status = taskstore.StatusAwaitingApproval
	t.Result = "ReviewerAgent test status: " + status
	_ = o.tasks.Save(t)
	_ = o.events.Append(ctx, eventlog.Event{ID: id.New("evt"), Type: "approval.requested", Actor: "ReviewerAgent", TaskID: taskID, Payload: eventlog.Payload(req)})
	return fmt.Sprintf("ReviewerAgent:\nTests: %s\n%s\nDiff:\n%s\nApproval requested: %s\nApprove merge with `approve %s`.", status, strings.TrimSpace(testOut), diffOut, approvalID, approvalID), nil
}

func (o *Orchestrator) runCoderTask(ctx context.Context, taskID string) (string, error) {
	t, err := o.tasks.Load(taskID)
	if err != nil {
		return "", err
	}
	if t.Workspace == "" {
		return "", fmt.Errorf("task %s has no workspace", taskID)
	}
	if o.provider == nil || o.model == "" {
		return "", fmt.Errorf("no LLM provider configured")
	}
	t.Status = taskstore.StatusRunning
	t.AssignedTo = "CoderAgent"
	if err := o.tasks.Save(t); err != nil {
		return "", err
	}
	_ = o.events.Append(ctx, eventlog.Event{ID: id.New("evt"), Type: "task.assigned", Actor: "OrchestratorAgent", TaskID: taskID, Payload: eventlog.Payload(map[string]any{"agent": "CoderAgent"})})

	maxToolCalls := o.cfg.Limits.MaxToolCallsPerTurn
	if maxToolCalls <= 0 {
		maxToolCalls = 12
	}
	maxToolCalls *= 2
	messages := []llm.Message{
		{Role: "system", Content: o.coderPrompt(t)},
		{Role: "user", Content: "Work this task to completion if possible. Inspect the workspace, apply a minimal patch, run formatting/tests, then summarize the diff."},
	}
	toolCallsUsed := 0
	lastMessage := ""
	var allResults []toolExecution
	for turn := 0; turn < 8; turn++ {
		resp, err := o.provider.Complete(ctx, llm.CompletionRequest{
			Model:       o.model,
			Temperature: 0,
			MaxTokens:   4096,
			Messages:    messages,
		})
		if err != nil {
			t.Status = taskstore.StatusFailed
			t.Result = err.Error()
			_ = o.tasks.Save(t)
			_ = o.writeRunArtifact(taskID, "failed", lastMessage, allResults, err.Error())
			return "", err
		}
		_ = o.events.Append(ctx, eventlog.Event{ID: id.New("evt"), Type: "agent.message", Actor: "CoderAgent", TaskID: taskID, Payload: eventlog.Payload(map[string]any{"provider": o.provider.Name(), "message": resp.Message.Content, "usage": resp.Usage})})
		parsed, err := parseAgentResponse(resp.Message.Content)
		if err != nil {
			t.Result = strings.TrimSpace(resp.Message.Content)
			_ = o.tasks.Save(t)
			_ = o.writeRunArtifact(taskID, "non_json_response", t.Result, allResults, err.Error())
			return t.Result, nil
		}
		if parsed.Message != "" {
			lastMessage = parsed.Message
		}
		if len(parsed.ToolCalls) == 0 || parsed.Done {
			break
		}
		messages = append(messages, llm.Message{Role: "assistant", Content: mustJSON(parsed)})
		var turnResults []toolExecution
		for _, call := range parsed.ToolCalls {
			if toolCallsUsed >= maxToolCalls {
				turnResults = append(turnResults, toolExecution{Tool: call.toolName(), Allowed: false, Error: "max tool calls reached"})
				continue
			}
			toolCallsUsed++
			result := o.executeProposedTool(ctx, "CoderAgent", call, taskID)
			turnResults = append(turnResults, result)
			allResults = append(allResults, result)
			if result.NeedsApproval {
				t.Status = taskstore.StatusAwaitingApproval
				t.Result = "CoderAgent is waiting for approval: " + result.ApprovalID
				_ = o.tasks.Save(t)
				_ = o.writeRunArtifact(taskID, "awaiting_approval", lastMessage, allResults, result.Reason)
				return formatApprovalStop(lastMessage, result), nil
			}
		}
		messages = append(messages, llm.Message{Role: "user", Content: "Tool results:\n" + truncateForPrompt(mustJSON(turnResults))})
	}

	t.Result = lastMessage
	if err := o.tasks.Save(t); err != nil {
		return "", err
	}
	review, err := o.reviewTask(ctx, taskID)
	status := "reviewed"
	errorText := ""
	if err != nil {
		status = "review_failed"
		errorText = err.Error()
	}
	_ = o.writeRunArtifact(taskID, status, lastMessage, allResults, errorText)
	if err != nil {
		return lastMessage, err
	}
	if lastMessage == "" {
		return review, nil
	}
	return lastMessage + "\n\n" + review, nil
}

func (o *Orchestrator) coderPrompt(t taskstore.Task) string {
	return strings.Join([]string{
		"You are CoderAgent. You inspect and modify only the isolated task workspace.",
		"The Go runtime is the authority. You propose tool calls; tools execute only after policy validation.",
		"Respond with exactly one JSON object and no prose.",
		"Protocol:",
		`{"message":"short status","done":false,"tool_calls":[{"tool":"repo.search","args":{"workspace":"` + t.Workspace + `","query":"symbol"}}]}`,
		`{"message":"summary of completed work","done":true,"tool_calls":[]}`,
		"Task:",
		mustJSON(t),
		"Rules:",
		"- Use repo.list/repo.search/repo.read with the workspace argument before editing.",
		"- Every repo tool call that supports workspace must include this exact workspace: " + t.Workspace,
		"- Apply edits only with repo.write_patch using a unified diff against repository-relative paths.",
		"- Prefer small, targeted patches. Do not rewrite unrelated files.",
		"- After editing Go code, run go.fmt, go.test, and repo.current_diff.",
		"- Do not call git.merge_approved, repo.apply_patch_to_main, service.*, shell.run_approved, or memory.commit_write.",
		"Available CoderAgent tools:",
		o.filteredToolCatalog(map[string]bool{
			"repo.list": true, "repo.search": true, "repo.read": true, "repo.write_patch": true, "repo.current_diff": true,
			"git.status": true, "git.diff": true, "go.fmt": true, "go.test": true, "go.build": true,
		}),
	}, "\n")
}

func (o *Orchestrator) filteredToolCatalog(allowed map[string]bool) string {
	type catalogTool struct {
		Name        string          `json:"name"`
		Description string          `json:"description"`
		Risk        tool.RiskLevel  `json:"risk"`
		Schema      json.RawMessage `json:"schema"`
	}
	var catalog []catalogTool
	for _, t := range o.registry.List() {
		if allowed[t.Name()] {
			catalog = append(catalog, catalogTool{Name: t.Name(), Description: t.Description(), Risk: t.Risk(), Schema: t.Schema()})
		}
	}
	sort.Slice(catalog, func(i, j int) bool { return catalog[i].Name < catalog[j].Name })
	return mustJSON(catalog)
}

func (o *Orchestrator) writeRunArtifact(taskID, status, summary string, results []toolExecution, errorText string) error {
	dir := filepath.Join(o.cfg.DataDir, "runs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	payload := map[string]any{
		"id":           id.New("run"),
		"task_id":      taskID,
		"status":       status,
		"summary":      summary,
		"tool_results": results,
		"error":        errorText,
		"time":         time.Now().UTC(),
	}
	b, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, payload["id"].(string)+".json"), append(b, '\n'), 0o644)
}

func (o *Orchestrator) listApprovals() (string, error) {
	requests, err := o.approvals.List()
	if err != nil {
		return "", err
	}
	if len(requests) == 0 {
		return "no approvals", nil
	}
	var b strings.Builder
	for _, r := range requests {
		fmt.Fprintf(&b, "%s [%s] task=%s tool=%s reason=%s\n", r.ID, r.Status, r.TaskID, r.Tool, r.Reason)
	}
	return strings.TrimSpace(b.String()), nil
}

func (o *Orchestrator) resolveApproval(ctx context.Context, approvalID string, grant bool) (string, error) {
	req, err := o.approvals.Load(approvalID)
	if err != nil {
		return "", err
	}
	if req.Status != approvalstore.StatusPending {
		return "approval is already " + req.Status, nil
	}
	if !grant {
		req.Status = approvalstore.StatusDenied
		if err := o.approvals.Save(req); err != nil {
			return "", err
		}
		_ = o.events.Append(ctx, eventlog.Event{ID: id.New("evt"), Type: "approval.denied", Actor: "human", TaskID: req.TaskID, Payload: eventlog.Payload(req)})
		return "Denied " + approvalID, nil
	}
	if _, err := o.runApprovedTool(ctx, req.Tool, req.Args, req.TaskID); err != nil {
		return "", err
	}
	req.Status = approvalstore.StatusGranted
	if err := o.approvals.Save(req); err != nil {
		return "", err
	}
	if req.TaskID != "" {
		if t, err := o.tasks.Load(req.TaskID); err == nil {
			if req.Tool == "git.merge_approved" {
				t.Status = taskstore.StatusDone
				t.Result = "merged after approval " + approvalID
			} else {
				t.Status = taskstore.StatusRunning
				t.Result = "approved " + req.Tool + " via " + approvalID
			}
			_ = o.tasks.Save(t)
		}
	}
	_ = o.events.Append(ctx, eventlog.Event{ID: id.New("evt"), Type: "approval.granted", Actor: "human", TaskID: req.TaskID, Payload: eventlog.Payload(req)})
	_ = o.events.Append(ctx, eventlog.Event{ID: id.New("evt"), Type: "task.completed", Actor: "OrchestratorAgent", TaskID: req.TaskID, Payload: eventlog.Payload(map[string]any{"approval": approvalID})})
	return "Approved and executed " + approvalID, nil
}

func (o *Orchestrator) executeProposedTool(ctx context.Context, actor string, call proposedToolCall, fallbackTaskID string) toolExecution {
	name := call.toolName()
	if name == "" {
		return toolExecution{Allowed: false, Error: "tool name is required"}
	}
	raw := call.Args
	if len(raw) == 0 {
		raw = json.RawMessage(`{}`)
	}
	taskID := taskIDFromArgs(raw)
	if taskID == "" {
		taskID = fallbackTaskID
	}
	_ = o.events.Append(ctx, eventlog.Event{ID: id.New("evt"), Type: "tool.call.requested", Actor: actor, TaskID: taskID, Payload: eventlog.Payload(map[string]any{"tool": name, "args": json.RawMessage(raw)})})
	if name == "task.create" {
		decision := o.policy.DecideNamed(actor, name, raw)
		if !decision.Allowed || decision.NeedsApproval {
			return o.handlePolicyDecision(ctx, actor, taskID, name, raw, decision)
		}
		return o.executeTaskCreate(ctx, actor, raw)
	}
	if name == "task.run" {
		decision := o.policy.DecideNamed(actor, name, raw)
		if !decision.Allowed || decision.NeedsApproval {
			return o.handlePolicyDecision(ctx, actor, taskID, name, raw, decision)
		}
		return o.executeTaskRun(ctx, actor, raw)
	}
	t, ok := o.registry.Get(name)
	if !ok {
		result := toolExecution{Tool: name, Allowed: false, Error: "tool not registered"}
		_ = o.events.Append(ctx, eventlog.Event{ID: id.New("evt"), Type: "tool.call.denied", Actor: "policy", TaskID: taskID, Payload: eventlog.Payload(result)})
		return result
	}
	decision := o.policy.Decide(actor, t, raw)
	if !decision.Allowed {
		return o.handlePolicyDecision(ctx, actor, taskID, name, raw, decision)
	}
	if decision.NeedsApproval {
		return o.handlePolicyDecision(ctx, actor, taskID, name, raw, decision)
	}
	_ = o.events.Append(ctx, eventlog.Event{ID: id.New("evt"), Type: "tool.call.allowed", Actor: "policy", TaskID: taskID, Payload: eventlog.Payload(map[string]any{"tool": name, "decision": decision})})
	rawResult, err := t.Run(ctx, raw)
	result := toolExecution{Tool: name, Allowed: true, Result: rawResult}
	payload := map[string]any{"tool": name, "result": json.RawMessage(rawResult)}
	if err != nil {
		result.Error = err.Error()
		payload["error"] = err.Error()
	}
	_ = o.events.Append(ctx, eventlog.Event{ID: id.New("evt"), Type: "tool.result", Actor: actor, TaskID: taskID, Payload: eventlog.Payload(payload)})
	return result
}

func (o *Orchestrator) handlePolicyDecision(ctx context.Context, actor, taskID, name string, raw json.RawMessage, decision tool.PolicyDecision) toolExecution {
	if !decision.Allowed {
		result := toolExecution{Tool: name, Allowed: false, Error: "policy denied", Reason: decision.Reason}
		_ = o.events.Append(ctx, eventlog.Event{ID: id.New("evt"), Type: "tool.call.denied", Actor: "policy", TaskID: taskID, Payload: eventlog.Payload(map[string]any{"actor": actor, "result": result})})
		return result
	}
	if decision.NeedsApproval {
		approvalID := id.New("approval")
		req := approvalstore.Request{ID: approvalID, TaskID: taskID, Tool: name, Args: raw, Reason: decision.Reason, Status: approvalstore.StatusPending}
		if err := o.approvals.Save(req); err != nil {
			return toolExecution{Tool: name, Allowed: true, NeedsApproval: true, Error: err.Error(), Reason: decision.Reason}
		}
		_ = o.events.Append(ctx, eventlog.Event{ID: id.New("evt"), Type: "approval.requested", Actor: "policy", TaskID: taskID, Payload: eventlog.Payload(req)})
		return toolExecution{Tool: name, Allowed: true, NeedsApproval: true, ApprovalID: approvalID, Reason: decision.Reason}
	}
	return toolExecution{Tool: name, Allowed: true, Reason: decision.Reason}
}

func (o *Orchestrator) executeTaskCreate(ctx context.Context, actor string, raw json.RawMessage) toolExecution {
	var req struct {
		Goal string `json:"goal"`
	}
	if err := json.Unmarshal(raw, &req); err != nil {
		return toolExecution{Tool: "task.create", Allowed: false, Error: err.Error()}
	}
	if req.Goal == "" {
		return toolExecution{Tool: "task.create", Allowed: false, Error: "goal is required"}
	}
	resultText, err := o.createTask(ctx, req.Goal)
	result := toolExecution{Tool: "task.create", Allowed: true, Result: eventlog.Payload(map[string]any{"message": resultText})}
	if err != nil {
		result.Error = err.Error()
	}
	_ = o.events.Append(ctx, eventlog.Event{ID: id.New("evt"), Type: "tool.result", Actor: actor, Payload: eventlog.Payload(result)})
	return result
}

func (o *Orchestrator) executeTaskRun(ctx context.Context, actor string, raw json.RawMessage) toolExecution {
	var req struct {
		TaskID string `json:"task_id"`
	}
	if err := json.Unmarshal(raw, &req); err != nil {
		return toolExecution{Tool: "task.run", Allowed: false, Error: err.Error()}
	}
	if req.TaskID == "" {
		return toolExecution{Tool: "task.run", Allowed: false, Error: "task_id is required"}
	}
	resultText, err := o.runCoderTask(ctx, req.TaskID)
	result := toolExecution{Tool: "task.run", Allowed: true, Result: eventlog.Payload(map[string]any{"message": resultText})}
	if err != nil {
		result.Error = err.Error()
	}
	_ = o.events.Append(ctx, eventlog.Event{ID: id.New("evt"), Type: "tool.result", Actor: actor, TaskID: req.TaskID, Payload: eventlog.Payload(result)})
	return result
}

func taskIDFromArgs(raw json.RawMessage) string {
	var args map[string]any
	if json.Unmarshal(raw, &args) != nil {
		return ""
	}
	for _, key := range []string{"task_id", "taskID"} {
		value, ok := args[key].(string)
		if ok {
			return value
		}
	}
	return ""
}

func (o *Orchestrator) runTool(ctx context.Context, actor, name string, args any, taskID string) (json.RawMessage, error) {
	raw := eventlog.Payload(args)
	t, ok := o.registry.Get(name)
	if !ok {
		return nil, fmt.Errorf("tool %s not registered", name)
	}
	_ = o.events.Append(ctx, eventlog.Event{ID: id.New("evt"), Type: "tool.call.requested", Actor: actor, TaskID: taskID, Payload: eventlog.Payload(map[string]any{"tool": name, "args": args})})
	decision := o.policy.Decide(actor, t, raw)
	if !decision.Allowed {
		_ = o.events.Append(ctx, eventlog.Event{ID: id.New("evt"), Type: "tool.call.denied", Actor: "policy", TaskID: taskID, Payload: eventlog.Payload(map[string]any{"actor": actor, "tool": name, "decision": decision})})
		return nil, fmt.Errorf("policy denied %s: %s", name, decision.Reason)
	}
	if decision.NeedsApproval {
		approvalID := id.New("approval")
		req := approvalstore.Request{ID: approvalID, TaskID: taskID, Tool: name, Args: raw, Reason: decision.Reason, Status: approvalstore.StatusPending}
		if err := o.approvals.Save(req); err != nil {
			return nil, err
		}
		_ = o.events.Append(ctx, eventlog.Event{ID: id.New("evt"), Type: "approval.requested", Actor: "policy", TaskID: taskID, Payload: eventlog.Payload(req)})
		return nil, fmt.Errorf("approval required: %s", approvalID)
	}
	_ = o.events.Append(ctx, eventlog.Event{ID: id.New("evt"), Type: "tool.call.allowed", Actor: "policy", TaskID: taskID, Payload: eventlog.Payload(map[string]any{"tool": name, "decision": decision})})
	rawResult, err := t.Run(ctx, raw)
	payload := map[string]any{"tool": name, "result": json.RawMessage(rawResult)}
	if err != nil {
		payload["error"] = err.Error()
	}
	_ = o.events.Append(ctx, eventlog.Event{ID: id.New("evt"), Type: "tool.result", Actor: actor, TaskID: taskID, Payload: eventlog.Payload(payload)})
	return rawResult, err
}

func (o *Orchestrator) runApprovedTool(ctx context.Context, name string, raw json.RawMessage, taskID string) (json.RawMessage, error) {
	t, ok := o.registry.Get(name)
	if !ok {
		return nil, fmt.Errorf("tool %s not registered", name)
	}
	_ = o.events.Append(ctx, eventlog.Event{ID: id.New("evt"), Type: "tool.call.allowed", Actor: "human", TaskID: taskID, Payload: eventlog.Payload(map[string]any{"tool": name, "reason": "human approval"})})
	rawResult, err := t.Run(ctx, raw)
	payload := map[string]any{"tool": name, "result": json.RawMessage(rawResult)}
	if err != nil {
		payload["error"] = err.Error()
	}
	_ = o.events.Append(ctx, eventlog.Event{ID: id.New("evt"), Type: "tool.result", Actor: "human", TaskID: taskID, Payload: eventlog.Payload(payload)})
	return rawResult, err
}

func firstLine(s string) string {
	s = strings.TrimSpace(strings.Split(s, "\n")[0])
	if len(s) > 80 {
		return s[:80]
	}
	return s
}

func DataPath(dataDir, name string) string {
	return filepath.Join(dataDir, name)
}
