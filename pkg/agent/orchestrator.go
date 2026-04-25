package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
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
	logger    *slog.Logger
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

type HandleResult struct {
	Reply  string
	Source string
}

func NewOrchestrator(cfg config.Config, events *eventlog.Store, tasks *taskstore.Store, approvals *approvalstore.Store, registry *tool.Registry, policy tool.Policy, provider llm.Provider, model string) *Orchestrator {
	return &Orchestrator{cfg: cfg, events: events, tasks: tasks, approvals: approvals, registry: registry, policy: policy, provider: provider, model: model, logger: slog.Default()}
}

func (o *Orchestrator) WithLogger(logger *slog.Logger) *Orchestrator {
	if logger != nil {
		o.logger = logger
	}
	return o
}

func (o *Orchestrator) log() *slog.Logger {
	if o.logger != nil {
		return o.logger
	}
	return slog.Default()
}

func (o *Orchestrator) Handle(ctx context.Context, from, message string) (string, error) {
	result, err := o.HandleDetailed(ctx, from, message)
	return result.Reply, err
}

func (o *Orchestrator) HandleDetailed(ctx context.Context, from, message string) (HandleResult, error) {
	message = strings.TrimSpace(message)
	if message == "" {
		return HandleResult{Reply: "empty message", Source: "program"}, nil
	}
	_ = o.events.Append(ctx, eventlog.Event{ID: id.New("evt"), Type: "user.message", Actor: from, Payload: eventlog.Payload(map[string]any{"message": message})})
	reply, source, err := o.handleMessage(ctx, message)
	if err != nil && isUserFacingCommandError(err) {
		reply = "I couldn't match that to a task. Use `tasks` to see current task IDs, or click one of the suggested action buttons."
		source = "program"
		err = nil
	}
	if err != nil && isOperationalCommandError(err) {
		reply = "error: " + err.Error()
		source = "program"
		err = nil
	}
	if err == nil {
		o.appendChatReply(ctx, from, reply)
	}
	return HandleResult{Reply: reply, Source: normalizeSource(source)}, err
}

func (o *Orchestrator) handleMessage(ctx context.Context, message string) (string, string, error) {
	fields := strings.Fields(message)
	cmd := strings.ToLower(fields[0])
	if isCasualMessage(message) {
		return programResult("I'm here. Use `help` for commands, or `new <goal>` to create a development task.", nil)
	}
	if isActiveTaskStatusRequest(message) || isInFlightQuery(message) {
		return programResult(o.listInFlight())
	}
	switch cmd {
	case "help":
		return programResult(help(), nil)
	case "status":
		return programResult(o.listInFlight())
	case "new", "task":
		return programResult(o.createTask(ctx, strings.TrimSpace(strings.TrimPrefix(message, fields[0]))))
	case "tasks":
		return programResult(o.listTasks())
	case "agents":
		return programResult(o.listAgents(ctx))
	case "cancel", "stop":
		if len(fields) < 2 {
			return programResult("usage: cancel <task_id>", nil)
		}
		return programResult(o.cancelTask(ctx, strings.Join(fields[1:], " ")))
	case "delete", "remove", "rm":
		if len(fields) < 2 {
			return programResult("usage: delete <task_id>", nil)
		}
		return programResult(o.deleteTask(ctx, strings.Join(fields[1:], " ")))
	case "show":
		if len(fields) < 2 {
			return programResult("usage: show <task_id>", nil)
		}
		return programResult(o.showTask(strings.Join(fields[1:], " ")))
	case "read":
		if len(fields) < 2 {
			return programResult("usage: read <repo_path>", nil)
		}
		return programResult(o.readRepo(ctx, strings.Join(fields[1:], " ")))
	case "web", "internet":
		query := webSearchQueryFromCommand(fields[1:])
		if query == "" {
			return programResult("usage: web <query>", nil)
		}
		return programResult(o.searchInternet(ctx, query, internetSearchSource(message)))
	case "search":
		if len(fields) < 2 {
			return programResult("usage: search <text>", nil)
		}
		if isWebSearchRequest(message) {
			query := webSearchQueryFromCommand(fields[1:])
			if query == "" {
				return programResult("usage: search the web for <query>", nil)
			}
			return programResult(o.searchInternet(ctx, query, internetSearchSource(message)))
		}
		return programResult(o.searchRepo(ctx, strings.Join(fields[1:], " ")))
	case "patch":
		if len(fields) < 3 {
			return programResult("usage: patch <task_id> <patch_file>", nil)
		}
		return programResult(o.patchTask(ctx, fields[1], fields[2]))
	case "test":
		if len(fields) < 2 {
			return programResult("usage: test <task_id>", nil)
		}
		return programResult(o.testTask(ctx, strings.Join(fields[1:], " ")))
	case "diff":
		if len(fields) < 2 {
			return programResult("usage: diff <task_id>", nil)
		}
		return programResult(o.diffTask(ctx, strings.Join(fields[1:], " ")))
	case "review":
		if len(fields) < 2 {
			return programResult("usage: review <task_id>", nil)
		}
		return programResult(o.reviewTask(ctx, strings.Join(fields[1:], " ")))
	case "accept", "verify":
		if len(fields) < 2 {
			return programResult("usage: accept <task_id>", nil)
		}
		return programResult(o.acceptTask(ctx, strings.Join(fields[1:], " ")))
	case "reopen":
		if len(fields) < 2 {
			return programResult("usage: reopen <task_id> [reason]", nil)
		}
		selector, reason := parseReopenCommand(fields[1:])
		return programResult(o.reopenTask(ctx, selector, reason))
	case "run", "work", "start":
		if len(fields) < 2 {
			return programResult("usage: run <task_id|task title>", nil)
		}
		return programResult(o.runCoderTask(ctx, strings.Join(fields[1:], " ")))
	case "delegate", "escalate":
		selector, backend, instruction, ok := parseDelegateCommand(fields)
		if !ok {
			return programResult("usage: delegate <task_id|task title> to <codex|claude|gemini> [instruction]", nil)
		}
		return programResult(o.delegateTask(ctx, selector, backend, instruction))
	case "codex", "claude", "gemini":
		if len(fields) < 2 {
			return programResult(fmt.Sprintf("usage: %s <task_id|task title> [instruction]", cmd), nil)
		}
		selector, instruction := splitSelectorAndInstruction(fields[1:])
		return programResult(o.delegateTask(ctx, selector, cmd, instruction))
	case "approvals":
		return programResult(o.listApprovals())
	case "approve":
		if len(fields) < 2 {
			return programResult("usage: approve <approval_id>", nil)
		}
		return programResult(o.resolveApproval(ctx, fields[1], true))
	case "deny":
		if len(fields) < 2 {
			return programResult("usage: deny <approval_id>", nil)
		}
		return programResult(o.resolveApproval(ctx, fields[1], false))
	default:
		if isPlainWorkRequest(message) {
			return programResult(o.startOneShotWork(ctx, message))
		}
		return o.handleWithLLM(ctx, message)
	}
}

func programResult(reply string, err error) (string, string, error) {
	return reply, "program", err
}

func normalizeSource(source string) string {
	source = strings.TrimSpace(source)
	if source == "" {
		return "program"
	}
	return source
}

func (o *Orchestrator) appendChatReply(ctx context.Context, to, message string) {
	if o.events == nil || strings.TrimSpace(message) == "" {
		return
	}
	_ = o.events.Append(ctx, eventlog.Event{
		ID:      id.New("evt"),
		Type:    "chat.reply",
		Actor:   "OrchestratorAgent",
		Payload: eventlog.Payload(map[string]any{"message": message, "to": to}),
	})
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

func isUserFacingCommandError(err error) bool {
	if err == nil {
		return false
	}
	message := err.Error()
	return strings.HasPrefix(message, "no task matches ") ||
		strings.HasPrefix(message, "task selector ") ||
		strings.Contains(message, "task id or title is required")
}

func isOperationalCommandError(err error) bool {
	if err == nil {
		return false
	}
	message := err.Error()
	return strings.HasPrefix(message, "git merge:") ||
		strings.HasPrefix(message, "git commit:") ||
		strings.HasPrefix(message, "policy denied ") ||
		strings.HasPrefix(message, "approval required:")
}

func isInFlightQuery(message string) bool {
	normalized := strings.ToLower(strings.TrimSpace(strings.Trim(message, ".,!?")))
	if len(strings.Fields(normalized)) > 5 {
		return false
	}
	return strings.Contains(normalized, "in flight") ||
		strings.Contains(normalized, "cooking") ||
		strings.Contains(normalized, "in progress") ||
		strings.Contains(normalized, "progress") ||
		strings.Contains(normalized, "what work") ||
		strings.Contains(normalized, "what is running") ||
		strings.Contains(normalized, "what's running") ||
		strings.Contains(normalized, "whats running")
}

func isWebSearchRequest(message string) bool {
	normalized := " " + strings.ToLower(strings.Join(strings.Fields(message), " ")) + " "
	return strings.Contains(normalized, " web ") ||
		strings.Contains(normalized, " internet ") ||
		strings.Contains(normalized, " online ") ||
		strings.Contains(normalized, " current ") ||
		strings.Contains(normalized, " latest ") ||
		strings.Contains(normalized, " docs ") ||
		strings.Contains(normalized, " documentation ")
}

func webSearchQueryFromCommand(args []string) string {
	var kept []string
	skipNext := false
	for i, arg := range args {
		if skipNext {
			skipNext = false
			continue
		}
		normalized := strings.ToLower(strings.Trim(arg, ".,!?"))
		switch normalized {
		case "web", "internet", "online":
			continue
		case "the":
			if i+1 < len(args) {
				next := strings.ToLower(strings.Trim(args[i+1], ".,!?"))
				if next == "web" || next == "internet" {
					skipNext = true
					continue
				}
			}
		case "for", "about":
			if len(kept) == 0 {
				continue
			}
		}
		kept = append(kept, arg)
	}
	return strings.TrimSpace(strings.Join(kept, " "))
}

func internetSearchSource(message string) string {
	normalized := strings.ToLower(message)
	if strings.Contains(normalized, "academic") ||
		strings.Contains(normalized, "scholarly") ||
		strings.Contains(normalized, "paper") ||
		strings.Contains(normalized, "papers") ||
		strings.Contains(normalized, "research literature") {
		return "academic"
	}
	return "web"
}

func isActiveTaskStatusRequest(message string) bool {
	normalized := normalizeIntentText(message)
	if normalized == "" {
		return false
	}
	switch normalized {
	case "status", "task status", "tasks status", "active tasks", "active task", "list active tasks", "list all active tasks", "what tasks are active", "what task are active", "what work is in progress", "work in progress", "in progress", "whats in progress", "what is in progress":
		return true
	}
	if strings.Contains(normalized, "active") && strings.Contains(normalized, "task") {
		return true
	}
	if strings.Contains(normalized, "work") && strings.Contains(normalized, "in progress") {
		return true
	}
	if strings.Contains(normalized, "task") && strings.Contains(normalized, "in progress") {
		return true
	}
	return false
}

func isPlainWorkRequest(message string) bool {
	if strings.Contains(message, "?") {
		return false
	}
	normalized := normalizeIntentText(message)
	if normalized == "" || isActiveTaskStatusRequest(message) || isCasualMessage(message) {
		return false
	}
	if isDesiredBehaviorStatement(normalized) {
		return true
	}
	first := ""
	for _, field := range strings.Fields(normalized) {
		switch field {
		case "please", "can", "could", "you", "would", "we", "need", "to":
			continue
		default:
			first = field
		}
		break
	}
	switch first {
	case "add", "build", "change", "create", "debug", "fix", "implement", "improve", "make", "patch", "refactor", "remove", "repair", "replace", "update", "upgrade", "write":
		return true
	default:
		return false
	}
}

func isDesiredBehaviorStatement(normalized string) bool {
	if startsWithQuestionWord(normalized) {
		return false
	}
	return strings.Contains(normalized, " should ") ||
		strings.HasPrefix(normalized, "should ") ||
		strings.Contains(normalized, " needs to ") ||
		strings.Contains(normalized, " need to ")
}

func startsWithQuestionWord(normalized string) bool {
	fields := strings.Fields(normalized)
	if len(fields) == 0 {
		return false
	}
	switch fields[0] {
	case "what", "why", "how", "when", "where", "who", "which":
		return true
	default:
		return false
	}
}

func normalizeIntentText(message string) string {
	normalized := strings.ToLower(strings.TrimSpace(message))
	normalized = strings.Trim(normalized, " \t\r\n.,!?")
	normalized = strings.ReplaceAll(normalized, "'", "")
	return strings.Join(strings.Fields(normalized), " ")
}

func parseDelegateCommand(fields []string) (selector, backend, instruction string, ok bool) {
	args := fields[1:]
	if len(args) == 0 {
		return "", "", "", false
	}
	for i, arg := range args {
		if strings.EqualFold(arg, "to") && i+1 < len(args) && isExternalBackend(args[i+1]) {
			selector = strings.Join(args[:i], " ")
			backend = strings.ToLower(args[i+1])
			instruction = strings.Join(args[i+2:], " ")
			return strings.TrimSpace(selector), backend, strings.TrimSpace(instruction), strings.TrimSpace(selector) != ""
		}
	}
	if len(args) >= 2 && isExternalBackend(args[1]) {
		selector = args[0]
		backend = strings.ToLower(args[1])
		instruction = strings.Join(args[2:], " ")
		return selector, backend, strings.TrimSpace(instruction), true
	}
	if len(args) >= 2 && isExternalBackend(args[len(args)-1]) {
		selector = strings.Join(args[:len(args)-1], " ")
		backend = strings.ToLower(args[len(args)-1])
		return strings.TrimSpace(selector), backend, "", strings.TrimSpace(selector) != ""
	}
	return "", "", "", false
}

func splitSelectorAndInstruction(args []string) (string, string) {
	if len(args) == 0 {
		return "", ""
	}
	for i, arg := range args {
		if strings.EqualFold(arg, "with") || strings.EqualFold(arg, "and") || strings.EqualFold(arg, "because") {
			return strings.Join(args[:i], " "), strings.Join(args[i+1:], " ")
		}
	}
	return strings.Join(args, " "), ""
}

func parseReopenCommand(args []string) (string, string) {
	if len(args) == 0 {
		return "", ""
	}
	if isTaskPronoun(args[0]) || isLikelyTaskID(args[0]) {
		return args[0], strings.Join(args[1:], " ")
	}
	for i, arg := range args {
		if strings.EqualFold(arg, "because") || strings.EqualFold(arg, "needs") || strings.EqualFold(arg, "with") {
			return strings.Join(args[:i], " "), strings.Join(args[i+1:], " ")
		}
	}
	return strings.Join(args, " "), ""
}

func isLikelyTaskID(value string) bool {
	value = strings.TrimSpace(strings.Trim(value, ".,!?"))
	if strings.HasPrefix(value, "task_") {
		return true
	}
	if len(value) < 6 || len(value) > 12 {
		return false
	}
	for _, r := range value {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')) {
			return false
		}
	}
	return true
}

func isExternalBackend(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "codex", "claude", "gemini":
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

func (o *Orchestrator) AcceptTask(ctx context.Context, taskID string) (string, error) {
	return o.acceptTask(ctx, taskID)
}

func (o *Orchestrator) ReopenTask(ctx context.Context, taskID, reason string) (string, error) {
	return o.reopenTask(ctx, taskID, reason)
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

type recoveryStrategy string

const (
	recoveryCoder    recoveryStrategy = "coder"
	recoveryDelegate recoveryStrategy = "delegate"
)

func (o *Orchestrator) RecoverRunningTasks(ctx context.Context) (int, error) {
	tasks, err := o.tasks.List()
	if err != nil {
		o.log().Error("task recovery list failed", "error", err)
		return 0, err
	}
	maxConcurrent := o.cfg.Limits.MaxConcurrentTasks
	if maxConcurrent <= 0 {
		maxConcurrent = 1
	}
	sem := make(chan struct{}, maxConcurrent)
	recovered := 0
	for _, t := range tasks {
		if t.Status != taskstore.StatusRunning {
			continue
		}
		recovered++
		strategy, backend := o.recoveryPlan(t)
		o.log().Warn("recovering persisted running task",
			"task_id", t.ID,
			"task_short_id", taskShortID(t.ID),
			"title", friendlyTaskTitle(t),
			"assigned_to", t.AssignedTo,
			"workspace", t.Workspace,
			"strategy", string(strategy),
			"backend", backend,
			"updated_at", t.UpdatedAt,
		)
		_ = o.events.Append(ctx, eventlog.Event{ID: id.New("evt"), Type: "task.recovery.queued", Actor: "homelabd", TaskID: t.ID, Payload: eventlog.Payload(map[string]any{
			"assigned_to": t.AssignedTo,
			"strategy":    string(strategy),
			"backend":     backend,
			"reason":      "homelabd started with task persisted as running",
		})})
		go o.resumeRecoveredTask(ctx, sem, t, strategy, backend)
	}
	if recovered == 0 {
		o.log().Info("task recovery found no persisted running tasks")
	} else {
		o.log().Info("task recovery queued persisted running tasks", "count", recovered, "max_concurrent", maxConcurrent)
	}
	return recovered, nil
}

func (o *Orchestrator) recoveryPlan(t taskstore.Task) (recoveryStrategy, string) {
	assigned := strings.ToLower(strings.TrimSpace(t.AssignedTo))
	if isExternalBackend(assigned) {
		return recoveryDelegate, assigned
	}
	if assigned == "" || assigned == "orchestratoragent" {
		if cfg, ok := o.cfg.ExternalAgents["codex"]; ok && cfg.Enabled && strings.TrimSpace(cfg.Command) != "" {
			return recoveryDelegate, "codex"
		}
	}
	return recoveryCoder, ""
}

func (o *Orchestrator) resumeRecoveredTask(ctx context.Context, sem chan struct{}, t taskstore.Task, strategy recoveryStrategy, backend string) {
	select {
	case <-ctx.Done():
		o.log().Info("task recovery skipped because context ended", "task_id", t.ID, "error", ctx.Err())
		return
	case sem <- struct{}{}:
	}
	defer func() { <-sem }()

	o.log().Info("task recovery started",
		"task_id", t.ID,
		"task_short_id", taskShortID(t.ID),
		"strategy", string(strategy),
		"backend", backend,
	)
	_ = o.events.Append(ctx, eventlog.Event{ID: id.New("evt"), Type: "task.recovery.started", Actor: "homelabd", TaskID: t.ID, Payload: eventlog.Payload(map[string]any{
		"strategy": string(strategy),
		"backend":  backend,
	})})

	switch strategy {
	case recoveryDelegate:
		run, err := o.prepareDelegationForTask(ctx, t.ID, backend, recoveredDelegationInstruction(t))
		if err != nil {
			o.markRecoveryBlocked(ctx, t.ID, err)
			return
		}
		o.runDelegation(ctx, run.ID, run.TaskID, run.Backend, run.Workspace, run.Instruction)
	case recoveryCoder:
		_, err := o.runCoderTask(ctx, t.ID)
		if err != nil {
			o.log().Error("task recovery coder run failed", "task_id", t.ID, "error", err)
			if ctx.Err() == nil {
				o.markRecoveryBlocked(ctx, t.ID, err)
				return
			}
			_ = o.events.Append(ctx, eventlog.Event{ID: id.New("evt"), Type: "task.recovery.failed", Actor: "homelabd", TaskID: t.ID, Payload: eventlog.Payload(map[string]any{
				"strategy": string(strategy),
				"error":    err.Error(),
			})})
			return
		}
	default:
		o.markRecoveryBlocked(ctx, t.ID, fmt.Errorf("unknown recovery strategy %q", strategy))
		return
	}
	o.log().Info("task recovery finished", "task_id", t.ID, "strategy", string(strategy), "backend", backend)
	_ = o.events.Append(ctx, eventlog.Event{ID: id.New("evt"), Type: "task.recovery.finished", Actor: "homelabd", TaskID: t.ID, Payload: eventlog.Payload(map[string]any{
		"strategy": string(strategy),
		"backend":  backend,
	})})
}

func recoveredDelegationInstruction(t taskstore.Task) string {
	return strings.Join([]string{
		"Resume this task after homelabd restarted while it was marked running.",
		"Do not assume prior in-memory worker state survived the restart.",
		defaultDelegationInstruction(t),
	}, " ")
}

func (o *Orchestrator) markRecoveryBlocked(ctx context.Context, taskID string, err error) {
	o.log().Error("task recovery failed", "task_id", taskID, "error", err)
	t, loadErr := o.tasks.Load(taskID)
	if loadErr == nil {
		t.Status = taskstore.StatusBlocked
		t.AssignedTo = "OrchestratorAgent"
		t.Result = "automatic recovery failed: " + err.Error()
		_ = o.tasks.Save(t)
	}
	_ = o.events.Append(ctx, eventlog.Event{ID: id.New("evt"), Type: "task.recovery.failed", Actor: "homelabd", TaskID: taskID, Payload: eventlog.Payload(map[string]any{"error": err.Error()})})
}

func help() string {
	return strings.Join([]string{
		"commands:",
		"  new <goal>                 create task and isolated worktree",
		"  tasks                      list tasks",
		"  status                     show active tasks and next actions",
		"  what's cooking             show active tasks and next actions",
		"  show <task_id>             show task",
		"  cancel <task_id>           mark task cancelled, keep workspace",
		"  delete <task_id>           remove task record and worktree",
		"  start <task_id|title>      run a task by id, short id, or title",
		"  read <repo_path>           inspect repo file",
		"  search <text>              search repo text",
		"  web <query>                search the public web",
		"  search web for <query>     search the public web",
		"  patch <task_id> <file>     apply unified diff to task worktree",
		"  test <task_id>             run project checks in worktree",
		"  diff <task_id>             show worktree diff",
		"  review <task_id>           run tests, show diff, request merge approval",
		"  accept <task_id>           mark merged task verified and done",
		"  reopen <task_id> [reason]  mark task not done and continue work",
		"  run <task_id>              let CoderAgent work in the task worktree",
		"  agents                     list external worker backends",
		"  delegate <task_id> <agent> <instruction>",
		"                             run codex/claude/gemini in the task worktree",
		"  delegate <task title> to <agent>",
		"                             natural form, e.g. delegate the bun task to codex",
		"  codex|claude|gemini <task_id> <instruction>",
		"                             shortcut for delegate <task_id> <agent> ...",
		"  approvals                  list approval requests",
		"  approve <approval_id>      execute approved action",
		"  deny <approval_id>         deny approved action",
	}, "\n")
}

func (o *Orchestrator) handleWithLLM(ctx context.Context, message string) (string, string, error) {
	if o.provider == nil || o.model == "" {
		return programResult("I do not have an LLM provider configured for open-ended chat. Use `help`, `status`, or describe development work to start a task.", nil)
	}
	maxToolCalls := o.cfg.Limits.MaxToolCallsPerTurn
	if maxToolCalls <= 0 {
		maxToolCalls = 12
	}
	messages := []llm.Message{
		{Role: "system", Content: o.llmToolPrompt()},
	}
	history := o.recentChatHistory(time.Now().UTC(), 24)
	messages = append(messages, history...)
	if !chatHistoryEndsWith(history, "user", message) {
		messages = append(messages, llm.Message{Role: "user", Content: message})
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
		source := responseSource(resp, o.provider.Name())
		if err != nil {
			_ = o.events.Append(ctx, eventlog.Event{ID: id.New("evt"), Type: "agent.message", Actor: "OrchestratorAgent", Payload: eventlog.Payload(map[string]any{"provider": o.provider.Name(), "error": err.Error()})})
			return programResult("I couldn't reach the configured LLM provider. I did not create a task. Use `status` for active work, or describe development work to start a task.", nil)
		}
		_ = o.events.Append(ctx, eventlog.Event{ID: id.New("evt"), Type: "agent.message", Actor: "OrchestratorAgent", Payload: eventlog.Payload(map[string]any{"provider": source, "model": resp.Model, "message": resp.Message.Content, "usage": resp.Usage})})
		parsed, err := parseAgentResponse(resp.Message.Content)
		if err != nil {
			if strings.TrimSpace(resp.Message.Content) != "" {
				return strings.TrimSpace(resp.Message.Content), source, nil
			}
			return programResult("I could not parse the model response. Use `help`, `status`, or describe development work to start a task.", nil)
		}
		if parsed.Message != "" {
			lastMessage = parsed.Message
		}
		if len(parsed.ToolCalls) == 0 || parsed.Done {
			if lastMessage == "" {
				lastMessage = "Done."
			}
			return lastMessage, source, nil
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
				return formatApprovalStop(lastMessage, result), "program", nil
			}
		}
		messages = append(messages, llm.Message{Role: "user", Content: "Tool results:\n" + truncateForPrompt(mustJSON(results))})
	}
	if lastMessage == "" {
		lastMessage = "Stopped after reaching the turn limit."
	}
	return lastMessage, "program", nil
}

func responseSource(resp llm.CompletionResponse, fallback string) string {
	if strings.TrimSpace(resp.Provider) != "" {
		return strings.TrimSpace(resp.Provider)
	}
	return normalizeSource(fallback)
}

func (o *Orchestrator) recentChatHistory(day time.Time, limit int) []llm.Message {
	if o.events == nil || limit <= 0 {
		return nil
	}
	events, err := o.events.ReadDay(day)
	if err != nil {
		return nil
	}
	history := make([]llm.Message, 0, limit)
	for _, event := range events {
		msg, ok := chatHistoryMessage(event)
		if !ok {
			continue
		}
		history = append(history, msg)
	}
	if len(history) > limit {
		history = history[len(history)-limit:]
	}
	return history
}

func chatHistoryMessage(event eventlog.Event) (llm.Message, bool) {
	var role string
	switch event.Type {
	case "user.message":
		role = "user"
	case "chat.reply":
		role = "assistant"
	default:
		return llm.Message{}, false
	}
	var payload struct {
		Message string `json:"message"`
		Content string `json:"content"`
		Reply   string `json:"reply"`
	}
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		return llm.Message{}, false
	}
	content := strings.TrimSpace(payload.Message)
	if content == "" {
		content = strings.TrimSpace(payload.Content)
	}
	if content == "" {
		content = strings.TrimSpace(payload.Reply)
	}
	if content == "" {
		return llm.Message{}, false
	}
	return llm.Message{Role: role, Content: content}, true
}

func chatHistoryEndsWith(history []llm.Message, role, content string) bool {
	if len(history) == 0 {
		return false
	}
	last := history[len(history)-1]
	return last.Role == role && strings.TrimSpace(last.Content) == strings.TrimSpace(content)
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
		"Use internet.search when current external documentation, public web context, or academic papers are required.",
		"Use internet.fetch on promising search result URLs before relying on page details; prefer official, primary, or scholarly sources.",
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

func truncateForChat(s string) string {
	const max = 12000
	if len(s) <= max {
		return s
	}
	return s[:max] + fmt.Sprintf("\n...[truncated %d bytes]", len(s)-max)
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
	created, err := o.createTaskRecord(ctx, goal)
	if err != nil {
		return "", err
	}
	if created.Task.ID == "" {
		return "usage: new <goal>", nil
	}
	t := created.Task
	return fmt.Sprintf("Created task %s.\nWorkspace: %s\nBranch: %s\nNext: `run %s`, `delegate %s <agent> <instruction>`, or `review %s`.", t.ID, t.Workspace, created.Branch, t.ID, t.ID, t.ID), nil
}

type createdTask struct {
	Task   taskstore.Task
	Branch string
}

func (o *Orchestrator) createTaskRecord(ctx context.Context, goal string) (createdTask, error) {
	if goal == "" {
		return createdTask{}, nil
	}
	now := time.Now().UTC()
	t := taskstore.Task{ID: id.New("task"), Title: firstLine(goal), Goal: goal, Status: taskstore.StatusRunning, AssignedTo: "CoderAgent", Priority: 5, CreatedAt: now, UpdatedAt: now}
	raw, err := o.runTool(ctx, "OrchestratorAgent", "git.worktree_create", map[string]any{"task_id": t.ID}, t.ID)
	if err != nil {
		t.Status = taskstore.StatusFailed
		t.Result = err.Error()
		_ = o.tasks.Save(t)
		return createdTask{}, err
	}
	var out struct {
		Workspace string `json:"workspace"`
		Branch    string `json:"branch"`
	}
	_ = json.Unmarshal(raw, &out)
	t.Workspace = out.Workspace
	if err := o.tasks.Save(t); err != nil {
		return createdTask{}, err
	}
	_ = o.events.Append(ctx, eventlog.Event{ID: id.New("evt"), Type: "task.created", Actor: "OrchestratorAgent", TaskID: t.ID, Payload: eventlog.Payload(t)})
	_ = o.events.Append(ctx, eventlog.Event{ID: id.New("evt"), Type: "task.assigned", Actor: "OrchestratorAgent", TaskID: t.ID, Payload: eventlog.Payload(map[string]any{"agent": "CoderAgent"})})
	return createdTask{Task: t, Branch: out.Branch}, nil
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
		fmt.Fprintf(&b, "%s [%s] %s\n  id: %s\n  workspace: %s\n", taskShortID(t.ID), t.Status, friendlyTaskTitle(t), t.ID, t.Workspace)
		if !taskTerminal(t.Status) {
			fmt.Fprintf(&b, "  next: %s\n", nextActionsForTask(t))
		}
	}
	return strings.TrimSpace(b.String()), nil
}

func (o *Orchestrator) listInFlight() (string, error) {
	tasks, err := o.tasks.List()
	if err != nil {
		return "", err
	}
	sort.Slice(tasks, func(i, j int) bool { return tasks[i].UpdatedAt.After(tasks[j].UpdatedAt) })
	var b strings.Builder
	for _, t := range tasks {
		if taskTerminal(t.Status) {
			continue
		}
		next := nextActionForTask(t)
		fmt.Fprintf(&b, "- %s [%s] %s\n  next: %s\n", taskShortID(t.ID), t.Status, friendlyTaskTitle(t), nextActionsForTaskWithPrimary(t, next))
	}
	if b.Len() == 0 {
		return "Nothing active. Use `new <goal>` to create a task.", nil
	}
	return "In flight:\n" + strings.TrimSpace(b.String()), nil
}

func nextActionForTask(t taskstore.Task) string {
	shortID := taskShortID(t.ID)
	switch t.Status {
	case taskstore.StatusAwaitingApproval:
		return "approvals"
	case taskstore.StatusAwaitingVerification:
		return fmt.Sprintf("accept %s", shortID)
	case taskstore.StatusReadyForReview:
		return fmt.Sprintf("review %s", shortID)
	case taskstore.StatusBlocked:
		result := strings.ToLower(t.Result)
		if strings.Contains(result, "external agent returned") || strings.Contains(result, "finished") || strings.Contains(result, "diff") {
			return fmt.Sprintf("review %s", shortID)
		}
		return fmt.Sprintf("start %s", shortID)
	case taskstore.StatusFailed:
		return fmt.Sprintf("start %s", shortID)
	default:
		if t.AssignedTo != "" && t.AssignedTo != "OrchestratorAgent" && t.AssignedTo != "CoderAgent" {
			return fmt.Sprintf("show %s", shortID)
		}
		return fmt.Sprintf("run %s", shortID)
	}
}

func nextActionsForTask(t taskstore.Task) string {
	return nextActionsForTaskWithPrimary(t, nextActionForTask(t))
}

func nextActionsForTaskWithPrimary(t taskstore.Task, primary string) string {
	shortID := taskShortID(t.ID)
	if t.Status == taskstore.StatusAwaitingVerification {
		return fmt.Sprintf("`%s`, `reopen %s needs rework`, or `delete %s`", primary, shortID, shortID)
	}
	return fmt.Sprintf("`%s`, `delegate %s to codex`, or `delete %s`", primary, shortID, shortID)
}

func (o *Orchestrator) showTask(taskID string) (string, error) {
	resolved, err := o.resolveTaskID(taskID)
	if err != nil {
		return "", err
	}
	t, err := o.tasks.Load(resolved)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s [%s] %s\n", taskShortID(t.ID), t.Status, friendlyTaskTitle(t))
	fmt.Fprintf(&b, "id: %s\n", t.ID)
	if t.AssignedTo != "" {
		fmt.Fprintf(&b, "assigned: %s\n", t.AssignedTo)
	}
	if t.Workspace != "" {
		fmt.Fprintf(&b, "workspace: %s\n", t.Workspace)
	}
	if strings.TrimSpace(t.Result) != "" {
		fmt.Fprintf(&b, "result: %s\n", strings.TrimSpace(t.Result))
	}
	if !taskTerminal(t.Status) {
		fmt.Fprintf(&b, "next: %s", nextActionsForTask(t))
	}
	return strings.TrimSpace(b.String()), nil
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

func (o *Orchestrator) acceptTask(ctx context.Context, selector string) (string, error) {
	taskID, err := o.resolveTaskID(selector)
	if err != nil {
		return "", err
	}
	t, err := o.tasks.Load(taskID)
	if err != nil {
		return "", err
	}
	if t.Status == taskstore.StatusDone {
		return fmt.Sprintf("Task %s is already done.", taskID), nil
	}
	if t.Status == taskstore.StatusCancelled {
		return fmt.Sprintf("Task %s is cancelled; reopen it before accepting.", taskID), nil
	}
	t.Status = taskstore.StatusDone
	t.AssignedTo = "OrchestratorAgent"
	if strings.TrimSpace(t.Result) == "" {
		t.Result = "accepted by human"
	} else {
		t.Result = strings.TrimSpace(t.Result) + "\naccepted by human"
	}
	if err := o.tasks.Save(t); err != nil {
		return "", err
	}
	_ = o.events.Append(ctx, eventlog.Event{ID: id.New("evt"), Type: "task.completed", Actor: "human", TaskID: taskID, Payload: eventlog.Payload(map[string]any{"result": t.Result})})
	return fmt.Sprintf("Accepted %s. Task is now done.\nUsage/docs notes:\n%s", taskShortID(taskID), usageNotesFromResult(t.Result)), nil
}

func (o *Orchestrator) reopenTask(ctx context.Context, selector, reason string) (string, error) {
	taskID, err := o.resolveTaskID(selector)
	if err != nil {
		return "", err
	}
	t, err := o.tasks.Load(taskID)
	if err != nil {
		return "", err
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "reopened by human"
	} else {
		reason = "reopened by human: " + reason
	}
	t.Status = taskstore.StatusRunning
	t.AssignedTo = "CoderAgent"
	if strings.TrimSpace(t.Result) == "" {
		t.Result = reason
	} else {
		t.Result = strings.TrimSpace(t.Result) + "\n" + reason
	}
	if err := o.tasks.Save(t); err != nil {
		return "", err
	}
	_ = o.events.Append(ctx, eventlog.Event{ID: id.New("evt"), Type: "task.reopened", Actor: "human", TaskID: taskID, Payload: eventlog.Payload(map[string]any{"reason": reason})})
	shortID := taskShortID(taskID)
	instruction := strings.TrimPrefix(reason, "reopened by human: ")
	instruction = strings.TrimPrefix(instruction, "reopened by human")
	instruction = strings.TrimSpace(instruction)
	if instruction == "" {
		instruction = "continue the reopened task"
	}
	return fmt.Sprintf("Reopened %s.\nNext: `delegate %s to codex %s`, `run %s`, or `review %s`.", shortID, shortID, instruction, shortID, shortID), nil
}

func usageNotesFromResult(result string) string {
	result = strings.TrimSpace(result)
	if result == "" {
		return "No usage notes were recorded. Reopen the task if this needs follow-up."
	}
	lines := strings.Split(result, "\n")
	var selected []string
	for i, line := range lines {
		normalized := strings.ToLower(strings.TrimSpace(line))
		if strings.Contains(normalized, "how to use") ||
			strings.Contains(normalized, "usage") ||
			strings.Contains(normalized, "docs") ||
			strings.Contains(normalized, "documentation") {
			start := i
			end := i + 4
			if end > len(lines) {
				end = len(lines)
			}
			selected = append(selected, strings.Join(lines[start:end], "\n"))
		}
	}
	if len(selected) == 0 {
		return "No explicit usage/docs notes were recorded. Reopen the task if this needs follow-up."
	}
	return strings.TrimSpace(strings.Join(selected, "\n"))
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
	if isTaskPronoun(selector) {
		return o.latestActionableTask()
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
		shortID := normalizeTaskSelector(taskShortID(t.ID))
		fullID := normalizeTaskSelector(t.ID)
		title := normalizeTaskSelector(t.Title)
		goal := normalizeTaskSelector(t.Goal)
		if fullID == needle || shortID == needle || title == needle || goal == needle {
			matches = append(matches, t)
			continue
		}
		if needle != "" && (strings.Contains(title, needle) || strings.Contains(goal, needle)) {
			matches = append(matches, t)
		}
	}
	if len(matches) == 1 {
		return matches[0].ID, nil
	}
	if len(matches) > 1 {
		sort.Slice(matches, func(i, j int) bool {
			if taskTerminal(matches[i].Status) != taskTerminal(matches[j].Status) {
				return !taskTerminal(matches[i].Status)
			}
			return matches[i].UpdatedAt.After(matches[j].UpdatedAt)
		})
		if !taskTerminal(matches[0].Status) && (len(matches) == 1 || taskTerminal(matches[1].Status)) {
			return matches[0].ID, nil
		}
		var ids []string
		for _, t := range matches {
			ids = append(ids, taskShortID(t.ID)+"="+friendlyTaskTitle(t))
		}
		sort.Strings(ids)
		return "", fmt.Errorf("task selector %q is ambiguous: %s", selector, strings.Join(ids, ", "))
	}
	return "", fmt.Errorf("no task matches %q", selector)
}

func (o *Orchestrator) latestActionableTask() (string, error) {
	tasks, err := o.tasks.List()
	if err != nil {
		return "", err
	}
	sort.Slice(tasks, func(i, j int) bool { return tasks[i].UpdatedAt.After(tasks[j].UpdatedAt) })
	for _, t := range tasks {
		if !taskTerminal(t.Status) {
			return t.ID, nil
		}
	}
	if len(tasks) > 0 {
		return tasks[0].ID, nil
	}
	return "", fmt.Errorf("no tasks exist")
}

func normalizeTaskSelector(s string) string {
	words := strings.Fields(strings.ToLower(strings.TrimSpace(strings.Trim(s, ".,!?"))))
	filtered := words[:0]
	for _, word := range words {
		switch word {
		case "the", "a", "an", "task", "please":
			continue
		default:
			filtered = append(filtered, word)
		}
	}
	return strings.Join(filtered, " ")
}

func isTaskPronoun(selector string) bool {
	switch normalizeTaskSelector(selector) {
	case "it", "this", "that", "current", "latest", "last":
		return true
	default:
		return false
	}
}

func taskTerminal(status string) bool {
	switch status {
	case taskstore.StatusDone, taskstore.StatusCancelled:
		return true
	default:
		return false
	}
}

func taskShortID(taskID string) string {
	parts := strings.Split(taskID, "_")
	if len(parts) > 0 && len(parts[len(parts)-1]) >= 6 {
		return parts[len(parts)-1]
	}
	if len(taskID) <= 8 {
		return taskID
	}
	return taskID[len(taskID)-8:]
}

func friendlyTaskTitle(t taskstore.Task) string {
	title := strings.TrimSpace(t.Title)
	if title == "" {
		title = strings.TrimSpace(t.Goal)
	}
	if len(title) > 72 {
		title = title[:72] + "..."
	}
	return title
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

func (o *Orchestrator) delegateTask(ctx context.Context, selector, backend, instruction string) (string, error) {
	taskID, err := o.resolveTaskID(selector)
	if err != nil {
		return "", err
	}
	if err := o.startDelegationForTask(context.Background(), taskID, backend, instruction); err != nil {
		return "", err
	}
	return fmt.Sprintf("Started %s on %s.\nThis is running in the background; chat is not blocked.\nNext: `status`, `show %s`, or `review %s` after it finishes.", backend, taskShortID(taskID), taskShortID(taskID), taskShortID(taskID)), nil
}

type delegationRun struct {
	ID          string
	TaskID      string
	Backend     string
	Workspace   string
	Instruction string
}

func (o *Orchestrator) runDelegation(ctx context.Context, runID, taskID, backend, workspace, instruction string) {
	raw, err := o.runTool(ctx, "OrchestratorAgent", "agent.delegate", map[string]any{
		"backend":     backend,
		"task_id":     taskID,
		"workspace":   workspace,
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
	t, loadErr := o.tasks.Load(taskID)
	if loadErr != nil {
		_ = o.events.Append(ctx, eventlog.Event{ID: id.New("evt"), Type: "agent.delegate.failed", Actor: backend, TaskID: taskID, Payload: eventlog.Payload(map[string]any{"id": runID, "error": loadErr.Error()})})
		return
	}
	t.AssignedTo = "OrchestratorAgent"
	output := strings.TrimSpace(out.Output)
	t.Result = output
	if err != nil {
		t.Status = taskstore.StatusBlocked
		if out.Error != "" {
			t.Result = out.Error
		} else {
			t.Result = err.Error()
		}
		_ = o.tasks.Save(t)
		_ = o.events.Append(ctx, eventlog.Event{ID: id.New("evt"), Type: "agent.delegate.failed", Actor: backend, TaskID: taskID, Payload: eventlog.Payload(map[string]any{"id": runID, "backend": backend, "error": err.Error(), "result": out})})
		return
	}
	t.Status = taskstore.StatusBlocked
	if t.Result == "" {
		t.Result = "external agent returned no output"
	}
	t.Result = "external agent finished; ready for review.\n" + t.Result
	t.Status = taskstore.StatusReadyForReview
	_ = o.tasks.Save(t)
	_ = o.events.Append(ctx, eventlog.Event{ID: id.New("evt"), Type: "agent.delegate.completed", Actor: backend, TaskID: taskID, Payload: eventlog.Payload(map[string]any{"id": runID, "backend": backend, "result": out})})
}

func (o *Orchestrator) startOneShotWork(ctx context.Context, goal string) (string, error) {
	created, err := o.createTaskRecord(ctx, goal)
	if err != nil {
		return "", err
	}
	if created.Task.ID == "" {
		return "usage: new <goal>", nil
	}
	taskID := created.Task.ID
	if err := o.startDelegationForTask(context.Background(), taskID, "codex", strings.Join([]string{
		goal,
		"",
		"Work only in this task worktree. Inspect first, make the smallest practical patch, update relevant docs/help text when behavior or commands change, run relevant formatting and tests, and leave a concise summary with how to use the change.",
	}, "\n")); err != nil {
		return fmt.Sprintf("Created task %s, but could not start codex: %v\nNext: `run %s` or `delegate %s to codex`.", taskShortID(taskID), err, taskShortID(taskID), taskShortID(taskID)), nil
	}
	return fmt.Sprintf("Created task %s and started codex. It is cooking in the background.\nNext: `status`, `show %s`, or `review %s` when ready.", taskShortID(taskID), taskShortID(taskID), taskShortID(taskID)), nil
}

func (o *Orchestrator) startDelegationForTask(ctx context.Context, taskID, backend, instruction string) error {
	run, err := o.prepareDelegationForTask(ctx, taskID, backend, instruction)
	if err != nil {
		return err
	}
	go o.runDelegation(ctx, run.ID, run.TaskID, run.Backend, run.Workspace, run.Instruction)
	return nil
}

func (o *Orchestrator) prepareDelegationForTask(ctx context.Context, taskID, backend, instruction string) (delegationRun, error) {
	t, err := o.tasks.Load(taskID)
	if err != nil {
		return delegationRun{}, err
	}
	if t.Workspace == "" {
		return delegationRun{}, fmt.Errorf("task %s has no workspace", taskID)
	}
	if strings.TrimSpace(instruction) == "" {
		instruction = defaultDelegationInstruction(t)
	}
	t.Status = taskstore.StatusRunning
	t.AssignedTo = backend
	t.Result = fmt.Sprintf("delegated to %s; external worker is running", backend)
	if err := o.tasks.Save(t); err != nil {
		return delegationRun{}, err
	}
	_ = o.events.Append(ctx, eventlog.Event{ID: id.New("evt"), Type: "task.assigned", Actor: "OrchestratorAgent", TaskID: taskID, Payload: eventlog.Payload(map[string]any{"agent": backend})})
	runID := id.New("delegate")
	_ = o.events.Append(ctx, eventlog.Event{ID: id.New("evt"), Type: "agent.delegate.started", Actor: "OrchestratorAgent", TaskID: taskID, Payload: eventlog.Payload(map[string]any{"id": runID, "backend": backend, "workspace": t.Workspace})})
	return delegationRun{ID: runID, TaskID: taskID, Backend: backend, Workspace: t.Workspace, Instruction: instruction}, nil
}

func defaultDelegationInstruction(t taskstore.Task) string {
	return strings.Join([]string{
		"Work this task to completion if possible.",
		"Inspect the task workspace before editing.",
		"Make a minimal patch that satisfies the task goal.",
		"If behavior, commands, UI, configuration, tools, or workflow changed, update relevant docs/help text in the same patch.",
		"Run relevant formatting and tests when available.",
		"Final summary must include: changed files, validation run, how to use the change, and docs updated or why no docs change was needed.",
		"Task goal: " + t.Goal,
	}, " ")
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

func (o *Orchestrator) searchInternet(ctx context.Context, query, source string) (string, error) {
	raw, err := o.runTool(ctx, "OrchestratorAgent", "internet.search", map[string]any{"query": query, "source": source, "max_results": 5}, "")
	if err != nil {
		return "", err
	}
	return formatInternetSearchResult(raw), nil
}

func formatInternetSearchResult(raw json.RawMessage) string {
	var out struct {
		Query       string           `json:"query"`
		Source      string           `json:"source"`
		Answer      string           `json:"answer"`
		Abstract    string           `json:"abstract"`
		AbstractURL string           `json:"abstract_url"`
		Results     []map[string]any `json:"results"`
		Web         *json.RawMessage `json:"web"`
		Academic    []map[string]any `json:"academic"`
		WebError    string           `json:"web_error"`
		AcademicErr string           `json:"academic_error"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return string(raw)
	}
	var b strings.Builder
	query := strings.TrimSpace(out.Query)
	if query == "" {
		query = "query"
	}
	source := strings.TrimSpace(out.Source)
	if source == "" {
		source = "internet"
	}
	fmt.Fprintf(&b, "Internet search (%s): %s\n", source, query)
	if out.Answer != "" {
		fmt.Fprintf(&b, "Answer: %s\n", out.Answer)
	}
	if out.Abstract != "" {
		fmt.Fprintf(&b, "Abstract: %s\n", out.Abstract)
		if out.AbstractURL != "" {
			fmt.Fprintf(&b, "Source: %s\n", out.AbstractURL)
		}
	}
	appendSearchResults(&b, out.Results)
	if out.Web != nil {
		var web struct {
			Results []map[string]any `json:"results"`
		}
		if json.Unmarshal(*out.Web, &web) == nil {
			appendSearchResults(&b, web.Results)
		}
	}
	appendSearchResults(&b, out.Academic)
	if out.WebError != "" {
		fmt.Fprintf(&b, "Web error: %s\n", out.WebError)
	}
	if out.AcademicErr != "" {
		fmt.Fprintf(&b, "Academic error: %s\n", out.AcademicErr)
	}
	result := strings.TrimSpace(b.String())
	if result == fmt.Sprintf("Internet search (%s): %s", source, query) {
		return result + "\nNo results returned."
	}
	return result
}

func appendSearchResults(b *strings.Builder, results []map[string]any) {
	if len(results) == 0 {
		return
	}
	for i, result := range results {
		title := stringFromAny(result["title"])
		url := stringFromAny(result["url"])
		snippet := stringFromAny(result["snippet"])
		if title == "" {
			title = stringFromAny(result["text"])
		}
		if title == "" && url == "" && snippet == "" {
			continue
		}
		fmt.Fprintf(b, "%d. %s", i+1, firstNonEmptyString(title, url, snippet))
		if url != "" && url != title {
			fmt.Fprintf(b, " — %s", url)
		}
		if snippet != "" && snippet != title {
			fmt.Fprintf(b, "\n   %s", snippet)
		}
		fmt.Fprintln(b)
	}
}

func stringFromAny(value any) string {
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v)
	case fmt.Stringer:
		return strings.TrimSpace(v.String())
	default:
		return ""
	}
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
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

func (o *Orchestrator) testTask(ctx context.Context, selector string) (string, error) {
	taskID, err := o.resolveTaskID(selector)
	if err != nil {
		return "", err
	}
	t, err := o.tasks.Load(taskID)
	if err != nil {
		return "", err
	}
	return o.runProjectChecks(ctx, taskID, t.Workspace, "CoderAgent")
}

func (o *Orchestrator) diffTask(ctx context.Context, selector string) (string, error) {
	taskID, err := o.resolveTaskID(selector)
	if err != nil {
		return "", err
	}
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

func (o *Orchestrator) reviewTask(ctx context.Context, selector string) (string, error) {
	taskID, err := o.resolveTaskID(selector)
	if err != nil {
		return "", err
	}
	diffOut, diffErr := o.diffTask(ctx, taskID)
	if diffErr != nil {
		return "", diffErr
	}
	t, err := o.tasks.Load(taskID)
	if err != nil {
		return "", err
	}
	shortID := taskShortID(taskID)
	if diffOut == "no diff" {
		t.Status = taskstore.StatusBlocked
		t.AssignedTo = "OrchestratorAgent"
		t.Result = "ReviewerAgent found no diff to approve."
		_ = o.tasks.Save(t)
		_ = o.events.Append(ctx, eventlog.Event{ID: id.New("evt"), Type: "task.blocked", Actor: "ReviewerAgent", TaskID: taskID, Payload: eventlog.Payload(map[string]any{"reason": t.Result})})
		return fmt.Sprintf("ReviewerAgent: no diff to approve.\nTask %s is blocked because the worker produced no changes.\nNext: `delegate %s to codex finish the task`, `run %s`, or `delete %s`.", shortID, shortID, shortID, shortID), nil
	}
	testOut, testErr := o.runProjectChecks(ctx, taskID, t.Workspace, "ReviewerAgent")
	status := "pass"
	if testErr != nil {
		status = "fail"
		t.Status = taskstore.StatusBlocked
		t.AssignedTo = "OrchestratorAgent"
		t.Result = "ReviewerAgent checks failed: " + testErr.Error()
		_ = o.tasks.Save(t)
		_ = o.events.Append(ctx, eventlog.Event{ID: id.New("evt"), Type: "task.blocked", Actor: "ReviewerAgent", TaskID: taskID, Payload: eventlog.Payload(map[string]any{"reason": t.Result})})
		return fmt.Sprintf("ReviewerAgent:\nChecks: %s\n%s\nDiff summary:\n%s\nNo approval created because checks failed.\nNext: `delegate %s to codex fix the failing tests`, `diff %s`, or `delete %s`.", status, strings.TrimSpace(testOut), summarizeDiffForChat(diffOut), shortID, shortID, shortID), nil
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
	return fmt.Sprintf("ReviewerAgent:\nChecks: %s\n%s\nDiff summary:\n%s\nMerge approval requested: %s\nApprove merge with `approve %s`.\nAfter merge, verify the running app and use `accept %s` or `reopen %s <reason>`.", status, strings.TrimSpace(testOut), summarizeDiffForChat(diffOut), approvalID, approvalID, shortID, shortID), nil
}

func (o *Orchestrator) runProjectChecks(ctx context.Context, taskID, workspace, actor string) (string, error) {
	var outputs []string
	var firstErr error
	if exists(filepath.Join(workspace, "go.mod")) {
		out, err := o.runCheckTool(ctx, actor, "go.test", workspace, taskID)
		outputs = append(outputs, "go.test:\n"+strings.TrimSpace(out))
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}
	webDir := filepath.Join(workspace, "web")
	if exists(filepath.Join(webDir, "package.json")) {
		out, err := o.runCheckTool(ctx, actor, "bun.check", webDir, taskID)
		outputs = append(outputs, "bun.check:\n"+strings.TrimSpace(out))
		if err != nil && firstErr == nil {
			firstErr = err
		}
		out, err = o.runCheckTool(ctx, actor, "bun.build", webDir, taskID)
		outputs = append(outputs, "bun.build:\n"+strings.TrimSpace(out))
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if len(outputs) == 0 {
		return "no configured checks found", nil
	}
	return truncateForChat(strings.Join(outputs, "\n\n")), firstErr
}

func (o *Orchestrator) runCheckTool(ctx context.Context, actor, name, dir, taskID string) (string, error) {
	raw, err := o.runTool(ctx, actor, name, map[string]any{"dir": dir}, taskID)
	var out struct {
		Output   string `json:"output"`
		Command  string `json:"command"`
		TimedOut bool   `json:"timed_out"`
	}
	_ = json.Unmarshal(raw, &out)
	output := out.Output
	if out.Command != "" {
		output = "$ " + out.Command + "\n" + output
	}
	if out.TimedOut {
		output += "\n[timed out]"
	}
	return output, err
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func summarizeDiffForChat(diff string) string {
	diff = strings.TrimSpace(diff)
	if diff == "" {
		return "no diff"
	}
	files := diffFileList(diff)
	additions, deletions := diffLineStats(diff)
	header := fmt.Sprintf("%d changed file(s), +%d/-%d", len(files), additions, deletions)
	if len(files) > 0 {
		limit := len(files)
		if limit > 20 {
			limit = 20
		}
		header += ":\n- " + strings.Join(files[:limit], "\n- ")
		if len(files) > limit {
			header += fmt.Sprintf("\n- ... %d more", len(files)-limit)
		}
	}
	return header + "\n\nFull diff is available with `diff <task_id>`."
}

func diffFileList(diff string) []string {
	seen := map[string]bool{}
	var files []string
	for _, line := range strings.Split(diff, "\n") {
		var path string
		switch {
		case strings.HasPrefix(line, "diff --git "):
			parts := strings.Fields(line)
			if len(parts) >= 4 {
				path = strings.TrimPrefix(parts[3], "b/")
			}
		case strings.HasPrefix(line, "+++ b/"):
			path = strings.TrimPrefix(strings.TrimPrefix(line, "+++ "), "b/")
		case strings.HasPrefix(line, "## "):
			continue
		}
		if path != "" && path != "/dev/null" && !seen[path] {
			seen[path] = true
			files = append(files, path)
		}
	}
	sort.Strings(files)
	return files
}

func diffLineStats(diff string) (int, int) {
	additions := 0
	deletions := 0
	for _, line := range strings.Split(diff, "\n") {
		switch {
		case strings.HasPrefix(line, "+++") || strings.HasPrefix(line, "---"):
			continue
		case strings.HasPrefix(line, "+"):
			additions++
		case strings.HasPrefix(line, "-"):
			deletions++
		}
	}
	return additions, deletions
}

func (o *Orchestrator) runCoderTask(ctx context.Context, selector string) (string, error) {
	taskID, err := o.resolveTaskID(selector)
	if err != nil {
		return "", err
	}
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
		"- Use internet.search when current external documentation, public web context, or academic papers are required.",
		"- Use internet.search with source academic for papers or source all when both web and scholarly context matter.",
		"- Use internet.fetch on promising result URLs before relying on details; prefer official, primary, or scholarly sources.",
		"- Every repo tool call that supports workspace must include this exact workspace: " + t.Workspace,
		"- Apply edits only with repo.write_patch using a unified diff against repository-relative paths.",
		"- Prefer small, targeted patches. Do not rewrite unrelated files.",
		"- If behavior, commands, UI, configuration, tools, or workflow changed, update relevant docs/help text in the same patch.",
		"- After editing Go code, run go.fmt, go.test, and repo.current_diff.",
		"- Final done=true message must include: changed files, validation run, how to use the change, and docs updated or why no docs change was needed.",
		"- Do not call git.merge_approved, repo.apply_patch_to_main, service.*, shell.run_approved, or memory.commit_write.",
		"Available CoderAgent tools:",
		o.filteredToolCatalog(map[string]bool{
			"internet.search": true, "internet.fetch": true,
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
		if r.Status == approvalstore.StatusPending {
			fmt.Fprintf(&b, "  next: `approve %s` or `deny %s`\n", r.ID, r.ID)
		}
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
				t.Status = taskstore.StatusAwaitingVerification
				t.AssignedTo = "OrchestratorAgent"
				t.Result = "merged after approval " + approvalID + "; awaiting human verification"
			} else {
				t.Status = taskstore.StatusRunning
				t.Result = "approved " + req.Tool + " via " + approvalID
			}
			_ = o.tasks.Save(t)
		}
	}
	_ = o.events.Append(ctx, eventlog.Event{ID: id.New("evt"), Type: "approval.granted", Actor: "human", TaskID: req.TaskID, Payload: eventlog.Payload(req)})
	if req.Tool == "git.merge_approved" {
		_ = o.events.Append(ctx, eventlog.Event{ID: id.New("evt"), Type: "task.awaiting_verification", Actor: "OrchestratorAgent", TaskID: req.TaskID, Payload: eventlog.Payload(map[string]any{"approval": approvalID})})
		if req.TaskID != "" {
			return fmt.Sprintf("Approved and merged %s.\nTask %s is awaiting verification.\nNext: check the running app, then `accept %s` or `reopen %s <reason>`.", approvalID, taskShortID(req.TaskID), taskShortID(req.TaskID), taskShortID(req.TaskID)), nil
		}
	}
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
