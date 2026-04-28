package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/andrewneudegg/lab/pkg/config"
	"github.com/andrewneudegg/lab/pkg/eventlog"
	"github.com/andrewneudegg/lab/pkg/id"
	"github.com/andrewneudegg/lab/pkg/llm"
	memstore "github.com/andrewneudegg/lab/pkg/memory"
	"github.com/andrewneudegg/lab/pkg/remoteagent"
	taskstore "github.com/andrewneudegg/lab/pkg/task"
	"github.com/andrewneudegg/lab/pkg/tool"
	approvalstore "github.com/andrewneudegg/lab/pkg/tools/approval"
	workflowstore "github.com/andrewneudegg/lab/pkg/workflow"
)

type Orchestrator struct {
	cfg          config.Config
	events       *eventlog.Store
	tasks        *taskstore.Store
	approvals    *approvalstore.Store
	workflows    *workflowstore.Store
	registry     *tool.Registry
	policy       tool.Policy
	provider     llm.Provider
	model        string
	memory       *memstore.Store
	remoteAgents *remoteagent.Store
	logger       *slog.Logger
	activeMu     sync.Mutex
	active       map[string]activeTaskRun
}

func (o *Orchestrator) WithRemoteAgents(store *remoteagent.Store) *Orchestrator {
	o.remoteAgents = store
	return o
}

func (o *Orchestrator) WithMemory(store *memstore.Store) *Orchestrator {
	o.memory = store
	return o
}

type activeTaskRun struct {
	Worker string
	RunID  string
	Cancel context.CancelFunc
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
	return &Orchestrator{cfg: cfg, events: events, tasks: tasks, approvals: approvals, registry: registry, policy: policy, provider: provider, model: model, logger: slog.Default(), active: make(map[string]activeTaskRun)}
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

func (o *Orchestrator) markTaskActive(taskID, worker string) bool {
	o.activeMu.Lock()
	defer o.activeMu.Unlock()
	if o.active == nil {
		o.active = make(map[string]activeTaskRun)
	}
	if _, ok := o.active[taskID]; ok {
		return false
	}
	o.active[taskID] = activeTaskRun{Worker: worker}
	return true
}

func (o *Orchestrator) setTaskActiveRun(taskID, runID string, cancel context.CancelFunc) {
	o.activeMu.Lock()
	defer o.activeMu.Unlock()
	if o.active == nil {
		o.active = make(map[string]activeTaskRun)
	}
	run := o.active[taskID]
	run.RunID = runID
	run.Cancel = cancel
	o.active[taskID] = run
}

func (o *Orchestrator) cancelTaskActiveRun(taskID string) (activeTaskRun, bool) {
	o.activeMu.Lock()
	defer o.activeMu.Unlock()
	run, ok := o.active[taskID]
	if !ok {
		return activeTaskRun{}, false
	}
	if run.Cancel != nil {
		run.Cancel()
	}
	return run, true
}

func (o *Orchestrator) clearTaskActive(taskID string) {
	o.activeMu.Lock()
	defer o.activeMu.Unlock()
	delete(o.active, taskID)
}

func (o *Orchestrator) taskActive(taskID string) bool {
	o.activeMu.Lock()
	defer o.activeMu.Unlock()
	_, ok := o.active[taskID]
	return ok
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
		reply = userFacingCommandErrorReply(err)
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
	message = strings.TrimSpace(message)
	if message == "" {
		return programResult("I'm here. Use `help` for commands, or `new <goal>` to create a development task.", nil)
	}
	fields := strings.Fields(message)
	cmd := commandWord(fields[0])
	if isCasualMessage(message) {
		return programResult("I'm here. Use `help` for commands, or `new <goal>` to create a development task.", nil)
	}
	if action, selector, reason, ok := parseTaskStateCommand(message); ok {
		switch action {
		case "accept":
			return programResult(o.acceptTask(ctx, selector))
		case "reopen":
			return programResult(o.reopenTask(ctx, selector, reason))
		case "delete":
			return programResult(o.deleteTask(ctx, selector))
		}
	}
	if goal, ok := taskCreationGoal(message); ok {
		return programResult(o.createTask(ctx, goal))
	}
	if isActiveTaskStatusRequest(message) || isInFlightQuery(message) {
		return programResult(o.listInFlight())
	}
	if selector, ok := taskDiffQuestionSelector(message); ok {
		return programResult(o.describeTaskDiff(ctx, selector))
	}
	switch cmd {
	case "help":
		return programResult(help(), nil)
	case "reflect":
		return o.reflectOnInteraction(ctx, message)
	case "deep":
		if len(fields) >= 3 && strings.EqualFold(fields[1], "research") {
			query := webSearchQueryFromCommand(fields[2:])
			if query == "" {
				return programResult("usage: deep research <query>", nil)
			}
			return programResult(o.researchInternet(ctx, query, internetSearchSource(message), "deep"))
		}
		return programResult("usage: deep research <query>", nil)
	case "status":
		return programResult(o.listInFlight())
	case "memory", "memories":
		return programResult(o.listMemoryLessons())
	case "remember", "learn":
		return programResult(o.rememberInteractionLesson(ctx, strings.TrimSpace(strings.TrimPrefix(message, fields[0])), cmd))
	case "forget", "unlearn":
		return programResult(o.unlearnInteractionLesson(strings.TrimSpace(strings.TrimPrefix(message, fields[0]))))
	case "new", "task":
		return programResult(o.createTask(ctx, strings.TrimSpace(strings.TrimPrefix(message, fields[0]))))
	case "tasks":
		return programResult(o.listTasks())
	case "workflow", "workflows":
		return programResult(o.handleWorkflowCommand(ctx, fields, message))
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
		if isDeepResearchRequest(message) {
			return programResult(o.researchInternet(ctx, query, internetSearchSource(message), researchDepth(message)))
		}
		return programResult(o.searchInternet(ctx, query, internetSearchSource(message)))
	case "research":
		query := webSearchQueryFromCommand(fields[1:])
		if query == "" {
			return programResult("usage: research <query>", nil)
		}
		return programResult(o.researchInternet(ctx, query, internetSearchSource(message), researchDepth(message)))
	case "search":
		if len(fields) < 2 {
			return programResult("usage: search <text>", nil)
		}
		if isWebSearchRequest(message) {
			query := webSearchQueryFromCommand(fields[1:])
			if query == "" {
				return programResult("usage: search the web for <query>", nil)
			}
			if isDeepResearchRequest(message) {
				return programResult(o.researchInternet(ctx, query, internetSearchSource(message), researchDepth(message)))
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
	case "retry":
		if len(fields) < 2 {
			return programResult("usage: retry <task_id> [codex|claude|gemini] [instruction]", nil)
		}
		selector, backend, instruction := parseRetryCommand(fields[1:])
		return programResult(o.retryTask(ctx, selector, backend, instruction))
	case "refresh", "rebase", "sync":
		if len(fields) < 2 {
			return programResult("usage: refresh <task_id>", nil)
		}
		return programResult(o.refreshTaskWorkspace(ctx, strings.Join(fields[1:], " ")))
	case "run", "work", "start":
		if len(fields) < 2 {
			return programResult("usage: run <task_id|task title>", nil)
		}
		return programResult(o.runCoderTask(ctx, strings.Join(fields[1:], " ")))
	case "ux":
		if len(fields) < 2 {
			return programResult("usage: ux <task_id|task title> [instruction]", nil)
		}
		selector, instruction := parseSpecialistRunCommand(fields[1:])
		return programResult(o.runUXTask(ctx, selector, instruction))
	case "delegate", "escalate":
		selector, backend, instruction, ok := parseDelegateCommand(fields)
		if !ok {
			return programResult("usage: delegate <task_id|task title> to <codex|claude|gemini|ux> [instruction]", nil)
		}
		if backend == "ux" {
			return programResult(o.runUXTask(ctx, selector, instruction))
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
		if isReflectionRequest(message) {
			return o.reflectOnInteraction(ctx, message)
		}
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

type memoryDistillation struct {
	Lesson string `json:"lesson"`
	Kind   string `json:"kind"`
}

func (o *Orchestrator) listMemoryLessons() (string, error) {
	if o.memory == nil {
		return "memory store is not configured", nil
	}
	lessons, err := o.memory.ListLessons(memstore.DefaultLessonFile)
	if err != nil {
		return "", err
	}
	if len(lessons) == 0 {
		return "No durable chat lessons recorded.", nil
	}
	var b strings.Builder
	b.WriteString("Durable chat lessons:")
	for _, lesson := range lessons {
		fmt.Fprintf(&b, "\n- %s: %s", lesson.ID, lesson.Content)
	}
	return b.String(), nil
}

func (o *Orchestrator) rememberInteractionLesson(ctx context.Context, text, command string) (string, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return fmt.Sprintf("usage: %s <lesson>", command), nil
	}
	if o.memory == nil {
		return "memory store is not configured", nil
	}

	lesson, err := o.distillMemoryLesson(ctx, text)
	if err != nil {
		return err.Error(), nil
	}
	saved, err := o.memory.RememberLesson(memstore.DefaultLessonFile, lesson)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Remembered %s: %s", saved.ID, saved.Content), nil
}

func (o *Orchestrator) unlearnInteractionLesson(selector string) (string, error) {
	selector = strings.TrimSpace(selector)
	if selector == "" {
		return "usage: unlearn <memory_id|text>", nil
	}
	if o.memory == nil {
		return "memory store is not configured", nil
	}
	removed, err := o.memory.UnlearnLesson(memstore.DefaultLessonFile, selector)
	if err != nil {
		return err.Error(), nil
	}
	if len(removed) == 1 {
		return fmt.Sprintf("Unlearned %s: %s", removed[0].ID, removed[0].Content), nil
	}
	return fmt.Sprintf("Unlearned %d lessons.", len(removed)), nil
}

func (o *Orchestrator) distillMemoryLesson(ctx context.Context, text string) (memstore.Lesson, error) {
	fromRecentInteraction := wantsRecentInteractionMemory(text)
	if fromRecentInteraction && (o.provider == nil || o.model == "") {
		return memstore.Lesson{}, errors.New("I need an LLM provider configured to distil recent interactions. Use `remember <specific lesson>` instead.")
	}
	if o.provider == nil || o.model == "" {
		return memstore.Lesson{Content: text, Kind: "lesson", Source: "chat"}, nil
	}

	input := "Memory request:\n" + text
	if fromRecentInteraction {
		input = "Recent chat history:\n" + o.chatHistoryForMemoryPrompt(time.Now().UTC(), 24)
	}
	resp, err := o.provider.Complete(ctx, llm.CompletionRequest{
		Model:       o.model,
		Temperature: 0,
		MaxTokens:   512,
		Messages: []llm.Message{{
			Role: "system",
			Content: strings.Join([]string{
				"You distil durable memory for OrchestratorAgent.",
				"Return exactly one JSON object and no prose.",
				`Schema: {"lesson":"one concise future-facing lesson","kind":"preference|procedure|principle|fact|lesson"}`,
				"The lesson must inform future decisions without mirroring the user's language, slang, or mood.",
				"Do not store secrets, raw transcripts, transient task state, or facts likely to go stale.",
				"If there is no durable lesson, return an empty lesson string.",
			}, "\n"),
		}, {
			Role:    "user",
			Content: input,
		}},
	})
	if err != nil {
		if fromRecentInteraction {
			return memstore.Lesson{}, fmt.Errorf("I could not distil recent interactions: %w", err)
		}
		return memstore.Lesson{Content: text, Kind: "lesson", Source: "chat"}, nil
	}
	var parsed memoryDistillation
	if err := json.Unmarshal([]byte(extractJSON(resp.Message.Content)), &parsed); err != nil {
		if fromRecentInteraction {
			return memstore.Lesson{}, fmt.Errorf("I could not parse the distilled lesson")
		}
		return memstore.Lesson{Content: text, Kind: "lesson", Source: "chat"}, nil
	}
	if strings.TrimSpace(parsed.Lesson) == "" {
		return memstore.Lesson{}, errors.New("I did not find a durable lesson to remember.")
	}
	return memstore.Lesson{Content: parsed.Lesson, Kind: parsed.Kind, Source: "chat"}, nil
}

func (o *Orchestrator) chatHistoryForMemoryPrompt(day time.Time, limit int) string {
	history := o.recentChatHistory(day, limit)
	var b strings.Builder
	for _, msg := range history {
		content := strings.TrimSpace(msg.Content)
		if content == "" {
			continue
		}
		fmt.Fprintf(&b, "%s: %s\n", msg.Role, truncateForPrompt(content))
	}
	return strings.TrimSpace(b.String())
}

func (o *Orchestrator) memoryContextPrompt() string {
	if o.memory == nil {
		return "No durable memory store configured."
	}
	prompt, err := o.memory.LessonPrompt(memstore.DefaultLessonFile, 12)
	if err != nil {
		return "Durable memory unavailable: " + err.Error()
	}
	return prompt
}

func wantsRecentInteractionMemory(value string) bool {
	normalised := strings.ToLower(strings.Join(strings.Fields(strings.Trim(value, " .,!?:;")), " "))
	switch normalised {
	case "that", "this", "from that", "from this", "from what just happened",
		"from our interaction", "from our interactions", "from recent interaction",
		"from the recent interaction", "from our recent interaction", "from our recent interactions":
		return true
	default:
		return false
	}
}

func isCasualMessage(message string) bool {
	normalized := strings.ToLower(strings.TrimSpace(strings.Trim(message, ".,!?")))
	switch normalized {
	case "hi", "hello", "hey", "yo", "andrew", "ping":
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
		strings.HasPrefix(message, "no workflow matches ") ||
		strings.HasPrefix(message, "workflow selector ") ||
		strings.Contains(message, "workflow id or name is required") ||
		strings.Contains(message, "task id or title is required")
}

func userFacingCommandErrorReply(err error) string {
	if err == nil {
		return ""
	}
	message := err.Error()
	if strings.HasPrefix(message, "no workflow matches ") ||
		strings.HasPrefix(message, "workflow selector ") ||
		strings.Contains(message, "workflow id or name is required") {
		return "I couldn't match that to a workflow: " + message + ". Use `workflows` to see current workflow IDs."
	}
	if strings.HasPrefix(message, "task selector ") {
		return "I found more than one matching task: " + message + ". Use the exact task ID from `tasks`, or click one of the suggested action buttons."
	}
	return "I couldn't match that to a task: " + message + ". Use `tasks` to see current task IDs, or click one of the suggested action buttons."
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

func commandWord(field string) string {
	return strings.ToLower(strings.Trim(field, " \t\r\n:.,!?"))
}

func taskCreationGoal(message string) (string, bool) {
	trimmed := strings.TrimSpace(message)
	if trimmed == "" {
		return "", false
	}
	fields := strings.Fields(trimmed)
	if len(fields) == 0 {
		return "", false
	}
	first := commandWord(fields[0])
	switch first {
	case "new", "task":
		goal := strings.TrimSpace(strings.TrimPrefix(trimmed, fields[0]))
		if !isCreatableTaskGoal(goal) {
			return "", false
		}
		return cleanupTaskCreationGoal(goal), true
	case "tasks":
		return "", false
	}

	normalized := normalizeIntentText(trimmed)
	for _, prefix := range taskCreationPrefixes() {
		if strings.HasPrefix(normalized, prefix+" ") {
			goal := wordsAfterPrefix(trimmed, len(strings.Fields(prefix)))
			if !isCreatableTaskGoal(goal) {
				return "", false
			}
			return cleanupTaskCreationGoal(goal), true
		}
	}
	for _, marker := range []string{" task to ", " task that ", " task which "} {
		if idx := strings.Index(normalized, marker); idx >= 0 {
			before := strings.TrimSpace(normalized[:idx])
			if !taskCreationLeadIn(before) {
				continue
			}
			goal := wordsAfterPrefix(trimmed, len(strings.Fields(normalized[:idx+len(marker)])))
			if !isCreatableTaskGoal(goal) {
				return "", false
			}
			return cleanupTaskCreationGoal(goal), true
		}
	}
	return "", false
}

func taskCreationPrefixes() []string {
	return []string{
		"create task",
		"create a task",
		"create a new task",
		"add task",
		"add a task",
		"make task",
		"make a task",
		"please create task",
		"please create a task",
		"please create a new task",
		"can you create task",
		"can you create a task",
		"could you create task",
		"could you create a task",
		"i want to create task",
		"i want to create a task",
		"i need to create task",
		"i need to create a task",
	}
}

func taskCreationLeadIn(value string) bool {
	switch value {
	case "create", "please create", "can you create", "could you create", "i want", "i want to create", "i need", "i need to create", "make", "please make", "add", "please add":
		return true
	default:
		return false
	}
}

func wordsAfterPrefix(message string, wordCount int) string {
	fields := strings.Fields(message)
	if wordCount >= len(fields) {
		return ""
	}
	return strings.Join(fields[wordCount:], " ")
}

func cleanupTaskCreationGoal(goal string) string {
	goal = strings.TrimSpace(strings.Trim(goal, " \t\r\n:"))
	lower := strings.ToLower(goal)
	for _, prefix := range []string{"to ", "that ", "which ", "for "} {
		if strings.HasPrefix(lower, prefix) {
			return strings.TrimSpace(goal[len(prefix):])
		}
	}
	return goal
}

func isCreatableTaskGoal(goal string) bool {
	goal = cleanupTaskCreationGoal(goal)
	if goal == "" {
		return false
	}
	normalized := normalizeIntentText(goal)
	switch normalized {
	case "", "status", "task status", "tasks", "active tasks", "list active tasks", "list all active tasks", "in flight", "whats cooking", "what is cooking":
		return false
	}
	if isInFlightQuery(goal) || isActiveTaskStatusRequest(goal) {
		return false
	}
	return true
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

func taskDiffQuestionSelector(message string) (string, bool) {
	normalized := normalizeIntentText(message)
	if !strings.Contains(normalized, "diff") {
		return "", false
	}
	if !strings.Contains(" "+normalized+" ", " main ") && !strings.Contains(" "+normalized+" ", " master ") {
		return "", false
	}
	if !strings.Contains(normalized, "between") && !strings.Contains(normalized, " vs ") && !strings.Contains(normalized, "against") {
		return "", false
	}
	for _, field := range strings.Fields(message) {
		token := taskDiffSelectorToken(field)
		if isTaskRefLike(token) {
			return token, true
		}
	}
	return "", false
}

func taskDiffSelectorToken(value string) string {
	value = strings.Trim(value, " \t\r\n`'\".,?!:;()[]{}")
	if strings.Contains(value, "/") {
		parts := strings.Split(value, "/")
		value = parts[len(parts)-1]
	}
	return strings.Trim(value, " \t\r\n`'\".,?!:;()[]{}")
}

func isTaskRefLike(value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	if strings.HasPrefix(value, "task_") {
		return true
	}
	if len(value) < 6 || len(value) > 16 {
		return false
	}
	for _, r := range value {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')) {
			return false
		}
	}
	return true
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

func isDeepResearchRequest(message string) bool {
	normalized := strings.ToLower(message)
	return strings.Contains(normalized, "deep research") ||
		strings.Contains(normalized, "research online") ||
		strings.Contains(normalized, "research the web") ||
		strings.HasPrefix(strings.TrimSpace(normalized), "research ")
}

func researchDepth(message string) string {
	normalized := strings.ToLower(message)
	if strings.Contains(normalized, "quick") || strings.Contains(normalized, "roughly") {
		return "quick"
	}
	if strings.Contains(normalized, "deep") || strings.Contains(normalized, "comprehensive") || strings.Contains(normalized, "thorough") {
		return "deep"
	}
	return "standard"
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
	if statusQuestionLead(normalized) && strings.Contains(normalized, "active") && strings.Contains(normalized, "task") {
		return true
	}
	if statusQuestionLead(normalized) && strings.Contains(normalized, "work") && strings.Contains(normalized, "in progress") {
		return true
	}
	if statusQuestionLead(normalized) && strings.Contains(normalized, "task") && strings.Contains(normalized, "in progress") {
		return true
	}
	return false
}

func statusQuestionLead(normalized string) bool {
	fields := strings.Fields(normalized)
	if len(fields) == 0 {
		return false
	}
	switch fields[0] {
	case "list", "show", "status", "what", "whats", "which":
		return true
	default:
		return false
	}
}

func isReflectionRequest(message string) bool {
	normalized := normalizeIntentText(message)
	for _, field := range strings.Fields(normalized) {
		switch field {
		case "please", "can", "could", "you", "would":
			continue
		case "reflect":
			return true
		default:
			return false
		}
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
	normalized = strings.ReplaceAll(normalized, ":", " ")
	return strings.Join(strings.Fields(normalized), " ")
}

func parseDelegateCommand(fields []string) (selector, backend, instruction string, ok bool) {
	args := fields[1:]
	if len(args) == 0 {
		return "", "", "", false
	}
	for i, arg := range args {
		if strings.EqualFold(arg, "to") && i+1 < len(args) && isDelegationTarget(args[i+1]) {
			selector = strings.Join(args[:i], " ")
			backend = strings.ToLower(args[i+1])
			instruction = strings.Join(args[i+2:], " ")
			return strings.TrimSpace(selector), backend, strings.TrimSpace(instruction), strings.TrimSpace(selector) != ""
		}
	}
	if len(args) >= 2 && isDelegationTarget(args[1]) {
		selector = args[0]
		backend = strings.ToLower(args[1])
		instruction = strings.Join(args[2:], " ")
		return selector, backend, strings.TrimSpace(instruction), true
	}
	if len(args) >= 2 && isDelegationTarget(args[len(args)-1]) {
		selector = strings.Join(args[:len(args)-1], " ")
		backend = strings.ToLower(args[len(args)-1])
		return strings.TrimSpace(selector), backend, "", strings.TrimSpace(selector) != ""
	}
	return "", "", "", false
}

func parseSpecialistRunCommand(args []string) (selector, instruction string) {
	if len(args) == 0 {
		return "", ""
	}
	if isLikelyTaskID(args[0]) {
		return args[0], strings.TrimSpace(strings.Join(args[1:], " "))
	}
	return splitSelectorAndInstruction(args)
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

func parseRetryCommand(args []string) (selector, backend, instruction string) {
	if len(args) == 0 {
		return "", "", ""
	}
	selector = args[0]
	rest := args[1:]
	if len(rest) > 0 && isExternalBackend(rest[0]) {
		return selector, strings.ToLower(rest[0]), strings.Join(rest[1:], " ")
	}
	if len(rest) > 1 && (strings.EqualFold(rest[0], "with") || strings.EqualFold(rest[0], "on")) && isExternalBackend(rest[1]) {
		return selector, strings.ToLower(rest[1]), strings.Join(rest[2:], " ")
	}
	return selector, "", strings.Join(rest, " ")
}

func parseTaskStateCommand(message string) (action, selector, reason string, ok bool) {
	fields := strings.Fields(strings.TrimSpace(message))
	if len(fields) == 0 {
		return "", "", "", false
	}
	start := 0
	for start < len(fields) && isCommandFiller(fields[start]) {
		start++
	}
	if start >= len(fields) {
		return "", "", "", false
	}
	verb := commandWord(fields[start])
	args := fields[start+1:]
	switch verb {
	case "accept", "verify":
		selector = strings.Join(dropTrailingStateWords(args), " ")
		return "accept", strings.TrimSpace(selector), "", strings.TrimSpace(selector) != ""
	case "delete", "remove", "rm":
		selector = strings.Join(dropTrailingStateWords(args), " ")
		return "delete", strings.TrimSpace(selector), "", strings.TrimSpace(selector) != ""
	case "reopen":
		selector, reason = parseReopenCommand(args)
		return "reopen", selector, reason, strings.TrimSpace(selector) != ""
	case "mark":
		if len(args) < 2 || !isAcceptStateWord(args[len(args)-1]) {
			return "", "", "", false
		}
		selector = strings.Join(dropTrailingStateWords(args[:len(args)-1]), " ")
		return "accept", strings.TrimSpace(selector), "", strings.TrimSpace(selector) != ""
	case "send":
		for i, arg := range args {
			if strings.EqualFold(arg, "back") {
				selector = strings.Join(args[:i], " ")
				reason = strings.Join(args[i+1:], " ")
				return "reopen", strings.TrimSpace(selector), strings.TrimSpace(reason), strings.TrimSpace(selector) != ""
			}
		}
	}
	return "", "", "", false
}

func isCommandFiller(word string) bool {
	switch strings.ToLower(strings.Trim(word, ".,!?")) {
	case "please", "pls", "can", "could", "would", "you":
		return true
	default:
		return false
	}
}

func dropTrailingStateWords(args []string) []string {
	for len(args) > 0 && isAcceptStateWord(args[len(args)-1]) {
		args = args[:len(args)-1]
	}
	return args
}

func isAcceptStateWord(word string) bool {
	switch strings.ToLower(strings.Trim(word, ".,!?")) {
	case "done", "complete", "completed", "verified", "accepted":
		return true
	default:
		return false
	}
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

func isDelegationTarget(name string) bool {
	if isExternalBackend(name) {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(name), "ux")
}

func (o *Orchestrator) CreateTask(ctx context.Context, goal string) (string, error) {
	return o.createTask(ctx, goal)
}

func (o *Orchestrator) CreateTaskWithTarget(ctx context.Context, goal string, target *taskstore.ExecutionTarget) (string, error) {
	if target == nil || strings.TrimSpace(target.Mode) == "" || strings.EqualFold(target.Mode, "local") {
		return o.createTask(ctx, goal)
	}
	if !strings.EqualFold(target.Mode, "remote") {
		return "", fmt.Errorf("unsupported task target mode %q", target.Mode)
	}
	task, err := o.createRemoteTaskRecord(ctx, goal, *target)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Created remote task %s for %s on %s.\nThe remote agent will claim it on its next poll.\nNext:\n%s", task.ID, task.Target.AgentID, task.Target.Workdir, commandBlock(
		"status",
		"show "+task.ID,
	)), nil
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

func (o *Orchestrator) AssignTaskTarget(ctx context.Context, selector string, target *taskstore.ExecutionTarget) (string, error) {
	if target == nil {
		return "", fmt.Errorf("target is required")
	}
	taskID, err := o.resolveTaskID(selector)
	if err != nil {
		return "", err
	}
	t, err := o.tasks.Load(taskID)
	if err != nil {
		return "", err
	}
	if taskTerminal(t.Status) {
		return "", fmt.Errorf("task %s is %s and cannot be reassigned", taskShortID(taskID), t.Status)
	}
	if !strings.EqualFold(target.Mode, "remote") {
		return "", fmt.Errorf("only remote assignment is supported")
	}
	target.AgentID = strings.TrimSpace(target.AgentID)
	target.WorkdirID = strings.TrimSpace(target.WorkdirID)
	target.Workdir = strings.TrimSpace(target.Workdir)
	target.Backend = strings.TrimSpace(target.Backend)
	if target.AgentID == "" {
		return "", fmt.Errorf("remote agent id is required")
	}
	if o.remoteAgents != nil {
		agent, err := o.remoteAgents.Load(target.AgentID)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return "", fmt.Errorf("remote agent %q is not registered", target.AgentID)
			}
			return "", err
		}
		if err := resolveAdvertisedWorkdir(agent, target); err != nil {
			return "", err
		}
	}
	if target.Workdir == "" {
		return "", fmt.Errorf("remote working directory is required")
	}
	if err := o.stalePendingTaskApprovals(ctx, taskID, "task assigned to a remote target"); err != nil {
		return "", err
	}
	target.Mode = "remote"
	t.Target = target
	t.Workspace = ""
	t.Status = taskstore.StatusQueued
	t.AssignedTo = "remote:" + target.AgentID
	t.Result = "queued for remote agent " + target.AgentID
	if err := o.tasks.Save(t); err != nil {
		return "", err
	}
	_ = o.events.Append(ctx, eventlog.Event{ID: id.New("evt"), Type: "remote_agent.task.assigned", Actor: "OrchestratorAgent", TaskID: taskID, Payload: eventlog.Payload(map[string]any{
		"agent_id": target.AgentID,
		"machine":  target.Machine,
		"workdir":  target.Workdir,
		"backend":  target.Backend,
	})})
	return fmt.Sprintf("Assigned %s to remote agent %s in %s.", taskShortID(taskID), target.AgentID, target.Workdir), nil
}

func (o *Orchestrator) CancelTask(ctx context.Context, taskID string) (string, error) {
	return o.cancelTask(ctx, taskID)
}

func (o *Orchestrator) RetryTask(ctx context.Context, taskID, backend, instruction string) (string, error) {
	return o.retryTask(ctx, taskID, backend, instruction)
}

func (o *Orchestrator) DeleteTask(ctx context.Context, taskID string) (string, error) {
	return o.deleteTask(ctx, taskID)
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

func (o *Orchestrator) ClaimRemoteTask(ctx context.Context, agent remoteagent.Agent, backend string) (*remoteagent.Assignment, error) {
	tasks, err := o.tasks.List()
	if err != nil {
		return nil, err
	}
	sort.Slice(tasks, func(i, j int) bool {
		if tasks[i].Priority != tasks[j].Priority {
			return tasks[i].Priority < tasks[j].Priority
		}
		return tasks[i].CreatedAt.Before(tasks[j].CreatedAt)
	})
	for _, t := range tasks {
		if !remoteTaskForAgent(t, agent.ID) || t.Status != taskstore.StatusQueued {
			continue
		}
		target := *t.Target
		if err := resolveAdvertisedWorkdir(agent, &target); err != nil {
			t.Status = taskstore.StatusBlocked
			t.AssignedTo = "OrchestratorAgent"
			t.Result = err.Error()
			_ = o.tasks.Save(t)
			return nil, fmt.Errorf("remote task %s target is invalid: %w", t.ID, err)
		}
		if backend == "" {
			backend = firstNonEmptyString(target.Backend, "codex")
		}
		t.Status = taskstore.StatusRunning
		t.AssignedTo = agent.ID
		target.Backend = backend
		t.Target = &target
		t.Result = "claimed by remote agent " + agent.ID
		if err := o.tasks.Save(t); err != nil {
			return nil, err
		}
		if o.remoteAgents != nil {
			_ = o.remoteAgents.SetCurrentTask(agent.ID, t.ID)
		}
		_ = o.events.Append(ctx, eventlog.Event{ID: id.New("evt"), Type: "remote_agent.task.claimed", Actor: agent.ID, TaskID: t.ID, Payload: eventlog.Payload(map[string]any{
			"agent_id": agent.ID,
			"machine":  agent.Machine,
			"workdir":  target.Workdir,
			"backend":  backend,
		})})
		return &remoteagent.Assignment{
			TaskID:      t.ID,
			Title:       t.Title,
			Goal:        t.Goal,
			Workdir:     target.Workdir,
			WorkdirID:   target.WorkdirID,
			Backend:     backend,
			Instruction: defaultRemoteAgentInstruction(t, agent),
		}, nil
	}
	return nil, nil
}

func (o *Orchestrator) CompleteRemoteTask(ctx context.Context, agentID, taskID, result, status string) (string, error) {
	t, err := o.tasks.Load(taskID)
	if err != nil {
		return "", err
	}
	if !remoteTaskForAgent(t, agentID) {
		return "", fmt.Errorf("task %s is not assigned to remote agent %s", taskID, agentID)
	}
	if t.Status != taskstore.StatusRunning {
		return "", fmt.Errorf("task %s is %s, not running", taskID, t.Status)
	}
	status = strings.ToLower(strings.TrimSpace(status))
	t.AssignedTo = "OrchestratorAgent"
	t.Result = strings.TrimSpace(result)
	if status == "failed" || status == "blocked" {
		t.Status = taskstore.StatusBlocked
		if t.Result == "" {
			t.Result = "remote agent reported failure"
		}
	} else {
		t.Status = taskstore.StatusReadyForReview
		if t.Result == "" {
			t.Result = "remote agent finished; ready for review"
		} else {
			t.Result = "remote agent finished; ready for review.\n" + t.Result
		}
	}
	if err := o.tasks.Save(t); err != nil {
		return "", err
	}
	if o.remoteAgents != nil {
		_ = o.remoteAgents.ClearCurrentTask(agentID, taskID)
	}
	_ = o.events.Append(ctx, eventlog.Event{ID: id.New("evt"), Type: "remote_agent.task.completed", Actor: agentID, TaskID: taskID, Payload: eventlog.Payload(map[string]any{
		"agent_id": agentID,
		"status":   t.Status,
	})})
	return fmt.Sprintf("Recorded remote result for %s.", taskShortID(taskID)), nil
}

func (o *Orchestrator) ListTaskRuns(taskID string) ([]ExternalRunArtifact, error) {
	dir := filepath.Join(o.cfg.DataDir, "runs")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []ExternalRunArtifact{}, nil
		}
		return nil, err
	}
	runs := []ExternalRunArtifact{}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, err
		}
		var run ExternalRunArtifact
		if err := json.Unmarshal(b, &run); err != nil {
			return nil, err
		}
		if run.Kind != "external_agent" || run.TaskID != taskID {
			continue
		}
		run.Path = filepath.Join(dir, entry.Name())
		runs = append(runs, run)
	}
	sort.Slice(runs, func(i, j int) bool {
		left := runs[i].Time
		if left.IsZero() {
			left = runs[i].FinishedAt
		}
		right := runs[j].Time
		if right.IsZero() {
			right = runs[j].FinishedAt
		}
		return left.After(right)
	})
	return runs, nil
}

type recoveryStrategy string

const (
	recoveryCoder    recoveryStrategy = "coder"
	recoveryDelegate recoveryStrategy = "delegate"
	recoveryUX       recoveryStrategy = "ux"
)

func (o *Orchestrator) StartTaskSupervisor(ctx context.Context) {
	interval := time.Duration(o.cfg.Limits.TaskWatchdogSeconds) * time.Second
	if interval <= 0 {
		interval = 30 * time.Second
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				o.log().Info("task supervisor stopped", "error", ctx.Err())
				return
			case <-ticker.C:
				if _, err := o.ReconcileTasks(ctx); err != nil {
					o.log().Error("task supervisor reconcile failed", "error", err)
				}
			}
		}
	}()
}

func (o *Orchestrator) RecoverRunningTasks(ctx context.Context) (int, error) {
	return o.reconcileTasks(ctx, true)
}

func (o *Orchestrator) ReconcileTasks(ctx context.Context) (int, error) {
	return o.reconcileTasks(ctx, false)
}

func (o *Orchestrator) reconcileTasks(ctx context.Context, recoverAllRunning bool) (int, error) {
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
	now := time.Now().UTC()
	for _, t := range tasks {
		if taskTerminal(t.Status) || o.taskActive(t.ID) {
			continue
		}
		if remoteTask(t) {
			continue
		}
		if t.Status == taskstore.StatusRunning {
			if approval, ok, err := o.latestApprovalForTask(t.ID, "git.merge_approved", approvalstore.StatusGranted); err != nil {
				o.log().Error("task approval reconciliation failed", "task_id", t.ID, "error", err)
				continue
			} else if ok && approvalGrantedDuringCurrentRun(approval, t) {
				t.Status = taskstore.StatusAwaitingVerification
				t.AssignedTo = "OrchestratorAgent"
				t.Result = appendResultLine(t.Result, "merge approval "+approval.ID+" was already granted; awaiting human verification")
				if err := o.tasks.Save(t); err != nil {
					o.log().Error("task approval reconciliation save failed", "task_id", t.ID, "error", err)
					continue
				}
				recovered++
				_ = o.events.Append(ctx, eventlog.Event{ID: id.New("evt"), Type: "task.awaiting_verification", Actor: "homelabd", TaskID: t.ID, Payload: eventlog.Payload(map[string]any{
					"approval": approval.ID,
					"reason":   "granted merge approval found while task was still marked running",
				})})
				continue
			}
		}
		if len(t.DependsOn) > 0 {
			unresolved, err := o.unresolvedDependencies(t)
			if err != nil {
				o.log().Error("task dependency check failed", "task_id", t.ID, "error", err)
				continue
			}
			if len(unresolved) > 0 {
				if t.Status == taskstore.StatusQueued {
					t.Status = taskstore.StatusBlocked
					t.BlockedBy = unresolved
					t.AssignedTo = "OrchestratorAgent"
					t.Result = "blocked by graph dependencies: " + strings.Join(shortTaskIDs(unresolved), ", ")
					_ = o.tasks.Save(t)
					_ = o.events.Append(ctx, eventlog.Event{ID: id.New("evt"), Type: "task.graph.blocked", Actor: "homelabd", TaskID: t.ID, ParentID: t.ParentID, Payload: eventlog.Payload(map[string]any{"blocked_by": unresolved})})
				}
				continue
			}
			if t.Status == taskstore.StatusBlocked {
				t.Status = taskstore.StatusQueued
				t.BlockedBy = nil
				t.AssignedTo = "OrchestratorAgent"
				t.Result = appendResultLine(t.Result, "dependencies satisfied; queued graph phase")
				if err := o.tasks.Save(t); err != nil {
					o.log().Error("task dependency release failed", "task_id", t.ID, "error", err)
					continue
				}
				_ = o.events.Append(ctx, eventlog.Event{ID: id.New("evt"), Type: "task.graph.released", Actor: "homelabd", TaskID: t.ID, ParentID: t.ParentID, Payload: eventlog.Payload(map[string]any{"depends_on": t.DependsOn})})
			}
		}
		if t.Status == taskstore.StatusQueued {
			backend, ok := o.preferredWorkerBackend()
			if !ok {
				o.log().Warn("queued task has no configured worker", "task_id", t.ID, "task_short_id", taskShortID(t.ID), "title", friendlyTaskTitle(t))
				continue
			}
			recovered++
			o.log().Info("task supervisor starting queued task",
				"task_id", t.ID,
				"task_short_id", taskShortID(t.ID),
				"title", friendlyTaskTitle(t),
				"backend", backend,
				"workspace", t.Workspace,
			)
			_ = o.events.Append(ctx, eventlog.Event{ID: id.New("evt"), Type: "task.supervisor.queued", Actor: "homelabd", TaskID: t.ID, Payload: eventlog.Payload(map[string]any{
				"backend": backend,
				"reason":  "queued task picked up by task supervisor",
			})})
			if err := o.startDelegationForTask(ctx, t.ID, backend, defaultDelegationInstruction(t)); err != nil {
				o.markRecoveryBlocked(ctx, t.ID, err)
			}
			continue
		}
		if t.Status != taskstore.StatusRunning {
			continue
		}
		if !recoverAllRunning && !o.runningTaskStale(t, now) {
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
			"recover_all_running", recoverAllRunning,
		)
		_ = o.events.Append(ctx, eventlog.Event{ID: id.New("evt"), Type: "task.recovery.queued", Actor: "homelabd", TaskID: t.ID, Payload: eventlog.Payload(map[string]any{
			"assigned_to": t.AssignedTo,
			"strategy":    string(strategy),
			"backend":     backend,
			"reason":      recoveryReason(recoverAllRunning),
		})})
		go o.resumeRecoveredTask(ctx, sem, t, strategy, backend)
	}
	if recovered == 0 {
		o.log().Info("task supervisor found no tasks requiring recovery")
	} else {
		o.log().Info("task recovery queued persisted running tasks", "count", recovered, "max_concurrent", maxConcurrent)
	}
	return recovered, nil
}

func recoveryReason(recoverAllRunning bool) string {
	if recoverAllRunning {
		return "homelabd started with task persisted as running"
	}
	return "running task is stale and no in-memory worker owns it"
}

func (o *Orchestrator) runningTaskStale(t taskstore.Task, now time.Time) bool {
	threshold := time.Duration(o.cfg.Limits.TaskStaleSeconds) * time.Second
	if threshold <= 0 {
		threshold = 5 * time.Minute
	}
	if t.UpdatedAt.IsZero() {
		return true
	}
	return now.Sub(t.UpdatedAt) >= threshold
}

func (o *Orchestrator) unresolvedDependencies(t taskstore.Task) ([]string, error) {
	var unresolved []string
	for _, depID := range uniqueStrings(t.DependsOn) {
		dep, err := o.tasks.Load(depID)
		if err != nil {
			unresolved = append(unresolved, depID)
			continue
		}
		if dep.Status != taskstore.StatusDone {
			unresolved = append(unresolved, dep.ID)
		}
	}
	return unresolved, nil
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]bool, len(values))
	var out []string
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func shortTaskIDs(ids []string) []string {
	short := make([]string, 0, len(ids))
	for _, taskID := range ids {
		short = append(short, taskShortID(taskID))
	}
	return short
}

func appendResultLine(result, line string) string {
	result = strings.TrimSpace(result)
	line = strings.TrimSpace(line)
	if line == "" || strings.Contains(result, line) {
		return result
	}
	if result == "" {
		return line
	}
	return result + "\n" + line
}

func (o *Orchestrator) preferredWorkerBackend() (string, bool) {
	if cfg, ok := o.cfg.ExternalAgents["codex"]; ok && cfg.Enabled && strings.TrimSpace(cfg.Command) != "" {
		return "codex", true
	}
	for _, name := range []string{"claude", "gemini"} {
		if cfg, ok := o.cfg.ExternalAgents[name]; ok && cfg.Enabled && strings.TrimSpace(cfg.Command) != "" {
			return name, true
		}
	}
	return "", false
}

func (o *Orchestrator) recoveryPlan(t taskstore.Task) (recoveryStrategy, string) {
	assigned := strings.ToLower(strings.TrimSpace(t.AssignedTo))
	if isExternalBackend(assigned) {
		return recoveryDelegate, assigned
	}
	if assigned == "" || assigned == "orchestratoragent" {
		if backend, ok := o.preferredWorkerBackend(); ok {
			return recoveryDelegate, backend
		}
	}
	if assigned == "coderagent" {
		if backend, ok := o.preferredWorkerBackend(); ok {
			return recoveryDelegate, backend
		}
	}
	if assigned == "uxagent" {
		return recoveryUX, ""
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
		if !o.markTaskActive(t.ID, backend) {
			o.log().Info("task recovery skipped because task is already active", "task_id", t.ID, "backend", backend)
			return
		}
		run, err := o.prepareDelegationForTask(ctx, t.ID, backend, recoveredDelegationInstruction(t))
		if err != nil {
			o.clearTaskActive(t.ID)
			o.markRecoveryBlocked(ctx, t.ID, err)
			return
		}
		defer o.clearTaskActive(run.TaskID)
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
	case recoveryUX:
		_, err := o.runUXTask(ctx, t.ID, recoveredUXInstruction(t))
		if err != nil {
			o.log().Error("task recovery UX run failed", "task_id", t.ID, "error", err)
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

func recoveredUXInstruction(t taskstore.Task) string {
	return strings.Join([]string{
		"Resume this UX task after homelabd restarted while it was marked running.",
		"Do not assume prior in-memory worker state survived the restart.",
		"Continue the UX research, implementation, regression tests, and browser-level verification from the current workspace state.",
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
		"  workflows                  list durable LLM/tool workflows",
		"  workflow new <name>: <goal> create a simple workflow",
		"  workflow show <workflow_id> show workflow steps, status, and cost estimate",
		"  workflow run <workflow_id> run a workflow through policy-bound steps",
		"  status                     show active tasks and next actions",
		"  what's cooking             show active tasks and next actions",
		"  memories                   list durable chat lessons",
		"  remember <lesson>          distil and store a durable chat lesson",
		"  unlearn <id|text>          remove a durable chat lesson",
		"  show <task_id>             show task",
		"  cancel <task_id>           mark task cancelled, keep workspace",
		"  delete <task_id>           remove task record and worktree",
		"  start <task_id|title>      run a task by id, short id, or title",
		"  read <repo_path>           inspect repo file",
		"  search <text>              search repo text",
		"  web <query>                search the public web",
		"  search web for <query>     search the public web",
		"  research <query>           fan out web research and fetch sources",
		"  deep research <query>      broader multi-query research bundle",
		"  reflect [prompt]           reflect and suggest a new-task action",
		"  patch <task_id> <file>     apply unified diff to task worktree",
		"  test <task_id>             run project checks in worktree",
		"  diff <task_id>             show worktree diff",
		"  review <task_id>           run tests, show diff, request merge approval",
		"  accept <task_id>           mark merged task verified and done",
		"  reopen <task_id> [reason]  mark task not done and continue work",
		"  retry <task_id> [agent] [instruction]",
		"                             rerun blocked or conflict work with preserved failure context",
		"  refresh <task_id>          reset task worktree branch to current main",
		"  run <task_id>              let CoderAgent work in the task worktree",
		"  ux <task_id> [instruction] let UXAgent audit, research, improve, and test UI/UX",
		"  agents                     list external worker backends",
		"  delegate <task_id> <agent> <instruction>",
		"                             run codex/claude/gemini or UXAgent in the task worktree",
		"  delegate <task title> to <agent>",
		"                             natural form, e.g. delegate the bun task to codex",
		"  codex|claude|gemini <task_id> <instruction>",
		"                             shortcut for delegate <task_id> <agent> ...",
		"  approvals                  list approval requests",
		"  approve <approval_id>      execute approved action; merge approvals auto-reconcile with main first",
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

type reflectionResult struct {
	Reflection string `json:"reflection"`
	TaskGoal   string `json:"task_goal"`
}

func (o *Orchestrator) reflectOnInteraction(ctx context.Context, message string) (string, string, error) {
	if o.provider == nil || o.model == "" {
		return programResult("I do not have an LLM provider configured for reflection. Use `new <goal>` to create a task directly.", nil)
	}
	history := o.recentChatHistory(time.Now().UTC(), 24)
	messages := []llm.Message{{
		Role: "system",
		Content: strings.Join([]string{
			"You are OrchestratorAgent reflecting on the recent homelabd interaction.",
			"Return exactly one JSON object and no prose.",
			`Schema: {"reflection":"one concise observation or improvement","task_goal":"one concrete development task goal the user can create with new <goal>"}`,
			"The task_goal must be actionable, scoped, and phrased as implementation work.",
		}, "\n"),
	}}
	messages = append(messages, history...)
	if !chatHistoryEndsWith(history, "user", message) {
		messages = append(messages, llm.Message{Role: "user", Content: message})
	}
	resp, err := o.provider.Complete(ctx, llm.CompletionRequest{
		Model:       o.model,
		Temperature: 0,
		MaxTokens:   512,
		Messages:    messages,
	})
	source := responseSource(resp, o.provider.Name())
	if err != nil {
		_ = o.events.Append(ctx, eventlog.Event{ID: id.New("evt"), Type: "agent.message", Actor: "OrchestratorAgent", Payload: eventlog.Payload(map[string]any{"provider": o.provider.Name(), "error": err.Error()})})
		return programResult("I couldn't reach the configured LLM provider. I did not create a task. Use `new <goal>` to create one directly.", nil)
	}
	_ = o.events.Append(ctx, eventlog.Event{ID: id.New("evt"), Type: "agent.message", Actor: "OrchestratorAgent", Payload: eventlog.Payload(map[string]any{"provider": source, "model": resp.Model, "message": resp.Message.Content, "usage": resp.Usage})})

	var result reflectionResult
	if err := json.Unmarshal([]byte(extractJSON(resp.Message.Content)), &result); err != nil {
		result.Reflection = strings.TrimSpace(resp.Message.Content)
	}
	result.Reflection = cleanReflectionText(result.Reflection)
	result.TaskGoal = cleanNewTaskGoal(result.TaskGoal)
	if result.Reflection == "" {
		result.Reflection = "I could not extract a specific reflection, but this can still be turned into a follow-up task."
	}
	if result.TaskGoal == "" {
		result.TaskGoal = cleanNewTaskGoal("Follow up on this reflection: " + result.Reflection)
	}
	if result.TaskGoal == "" {
		result.TaskGoal = "Improve the homelabd workflow based on the latest reflection"
	}
	return fmt.Sprintf("Reflection: %s\n\nAction: `new %s`", result.Reflection, result.TaskGoal), source, nil
}

func cleanReflectionText(value string) string {
	value = strings.TrimSpace(value)
	value = strings.ReplaceAll(value, "`", "'")
	return strings.Join(strings.Fields(value), " ")
}

func cleanNewTaskGoal(value string) string {
	value = strings.TrimSpace(value)
	value = strings.ReplaceAll(value, "`", "'")
	value = strings.ReplaceAll(value, "<", "")
	value = strings.ReplaceAll(value, ">", "")
	value = strings.Join(strings.Fields(value), " ")
	if len(value) > 220 {
		value = strings.TrimSpace(value[:220])
	}
	return strings.TrimSpace(strings.TrimPrefix(value, "new "))
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
		"Personality: be direct, pragmatic, and decision-oriented. Do not mirror the user's wording, slang, or emotional tone.",
		"Learning: distil durable feedback into concise lessons that improve future decisions; treat memory as soft guidance below current instructions and repo facts.",
		"The Go runtime is the authority. You propose actions; tools execute only after policy validation.",
		"Respond with exactly one JSON object and no prose.",
		"Protocol:",
		`{"message":"short user-facing status","done":false,"tool_calls":[{"tool":"repo.search","args":{"query":"TODO"}}]}`,
		`{"message":"final answer","done":true,"tool_calls":[]}`,
		"Use tools for inspection before answering repo-specific questions.",
		"Use internet.research for broad, current, multi-source questions before synthesizing advice.",
		"Use text.correct to fix likely spelling or grammar issues before internet.search when the user query looks typo-prone; preserve exact code symbols and quoted strings.",
		"Use text.summarize when a long user task or note needs a compact label.",
		"Use memory.remember only when the user explicitly asks you to remember or learn something, or gives clear future-facing feedback. Store distilled lessons, not transcripts.",
		"Use memory.unlearn when the user asks you to forget, remove, correct, or stop using a stored lesson.",
		"Use internet.search when current external documentation, public web context, or academic papers are required.",
		"Use internet.fetch on promising search result URLs before relying on page details; prefer official, primary, or scholarly sources.",
		"Create development work with task.create instead of pretending to edit files directly.",
		"Create or reuse workflows when repeatable LLM/tool/wait logic should be monitored outside this chat turn.",
		"Do not request dangerous or write tools unless the user clearly asked for that operation; approval may be required.",
		"Current durable memory:",
		o.memoryContextPrompt(),
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
		Description: "Create a development task with an isolated local worktree or explicit remote target. Args: {\"goal\":\"...\",\"target\":{...}}.",
		Risk:        tool.RiskLow,
		Schema:      json.RawMessage(`{"type":"object","required":["goal"],"properties":{"goal":{"type":"string"},"target":{"type":"object"}}}`),
	}, {
		Name:        "task.run",
		Description: "Run CoderAgent on an existing task. Args: {\"task_id\":\"...\"}.",
		Risk:        tool.RiskLow,
		Schema:      json.RawMessage(`{"type":"object","required":["task_id"],"properties":{"task_id":{"type":"string"}}}`),
	}, {
		Name:        "workflow.create",
		Description: "Create a durable workflow made of LLM, tool, wait, or workflow steps. Args: {\"name\":\"...\",\"goal\":\"...\",\"steps\":[{\"kind\":\"llm|tool|wait|workflow\",...}]}",
		Risk:        tool.RiskLow,
		Schema:      json.RawMessage(`{"type":"object","required":["name"],"properties":{"name":{"type":"string"},"description":{"type":"string"},"goal":{"type":"string"},"steps":{"type":"array","items":{"type":"object"}}}}`),
	}, {
		Name:        "workflow.list",
		Description: "List durable workflow definitions, current status, and cost estimates. Args: {}.",
		Risk:        tool.RiskReadOnly,
		Schema:      json.RawMessage(`{"type":"object","properties":{}}`),
	}, {
		Name:        "workflow.show",
		Description: "Show one workflow's steps, latest run, and cost estimate. Args: {\"workflow_id\":\"...\"}.",
		Risk:        tool.RiskReadOnly,
		Schema:      json.RawMessage(`{"type":"object","required":["workflow_id"],"properties":{"workflow_id":{"type":"string"}}}`),
	}, {
		Name:        "workflow.run",
		Description: "Run a durable workflow through policy-bound LLM/tool/wait/workflow steps. Args: {\"workflow_id\":\"...\"}.",
		Risk:        tool.RiskLow,
		Schema:      json.RawMessage(`{"type":"object","required":["workflow_id"],"properties":{"workflow_id":{"type":"string"}}}`),
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
	return fmt.Sprintf("Created queued task %s.\nPlan created and reviewed before execution.\nWorkspace: %s\nBranch: %s\nThe task supervisor will start an available worker automatically.\nNext:\n%s", t.ID, t.Workspace, created.Branch, commandBlock(
		"status",
		"run "+t.ID,
		"delegate "+t.ID+" to codex",
	)), nil
}

func (c createdTask) firstChildID() string {
	if len(c.Children) == 0 {
		return c.Task.ID
	}
	return c.Children[0].ID
}

func formatCreatedGraphChildren(children []taskstore.Task) string {
	if len(children) == 0 {
		return "No child phases were created."
	}
	var b strings.Builder
	b.WriteString("Child phases:")
	for _, child := range children {
		fmt.Fprintf(&b, "\n- %s [%s] %s", taskShortID(child.ID), child.Status, child.GraphPhase)
		if len(child.DependsOn) > 0 {
			fmt.Fprintf(&b, " after %s", strings.Join(shortTaskIDs(child.DependsOn), ", "))
		}
	}
	return b.String()
}

func commandBlock(commands ...string) string {
	return "```\n" + strings.Join(commands, "\n") + "\n```"
}

type createdTask struct {
	Task     taskstore.Task
	Branch   string
	Children []taskstore.Task
}

const taskTitleMaxCharacters = 84

func (o *Orchestrator) createTaskRecord(ctx context.Context, goal string) (createdTask, error) {
	goal = strings.TrimSpace(goal)
	if goal == "" {
		return createdTask{}, nil
	}
	taskID := id.New("task")
	now := time.Now().UTC()
	t := taskstore.Task{
		ID:         taskID,
		Title:      o.summarizeTaskTitle(ctx, taskID, goal),
		Goal:       goal,
		Status:     taskstore.StatusQueued,
		AssignedTo: "OrchestratorAgent",
		Priority:   5,
		CreatedAt:  now,
		UpdatedAt:  now,
		Result:     "queued for task supervisor",
	}
	o.ensureTaskPlan(ctx, &t)
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
	return createdTask{Task: t, Branch: out.Branch}, nil
}

func (o *Orchestrator) createRemoteTaskRecord(ctx context.Context, goal string, target taskstore.ExecutionTarget) (taskstore.Task, error) {
	goal = strings.TrimSpace(goal)
	if goal == "" {
		return taskstore.Task{}, fmt.Errorf("goal is required")
	}
	target.Mode = "remote"
	target.AgentID = strings.TrimSpace(target.AgentID)
	target.WorkdirID = strings.TrimSpace(target.WorkdirID)
	target.Workdir = strings.TrimSpace(target.Workdir)
	target.Backend = strings.TrimSpace(target.Backend)
	if target.AgentID == "" {
		return taskstore.Task{}, fmt.Errorf("remote agent id is required")
	}
	if o.remoteAgents != nil {
		agent, err := o.remoteAgents.Load(target.AgentID)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return taskstore.Task{}, fmt.Errorf("remote agent %q is not registered", target.AgentID)
			}
			return taskstore.Task{}, err
		}
		if agent.ID == "" {
			return taskstore.Task{}, fmt.Errorf("remote agent %q is not registered", target.AgentID)
		}
		if err := resolveAdvertisedWorkdir(agent, &target); err != nil {
			return taskstore.Task{}, err
		}
	}
	if target.Workdir == "" {
		return taskstore.Task{}, fmt.Errorf("remote working directory is required")
	}
	taskID := id.New("task")
	now := time.Now().UTC()
	task := taskstore.Task{
		ID:                 taskID,
		Title:              o.summarizeTaskTitle(ctx, taskID, goal),
		Goal:               goal,
		Status:             taskstore.StatusQueued,
		AssignedTo:         "remote:" + target.AgentID,
		Priority:           5,
		CreatedAt:          now,
		UpdatedAt:          now,
		Target:             &target,
		Result:             "queued for remote agent " + target.AgentID,
		AcceptanceCriteria: remoteAcceptanceCriteria(target),
	}
	o.ensureTaskPlan(ctx, &task)
	if err := o.tasks.Save(task); err != nil {
		return taskstore.Task{}, err
	}
	_ = o.events.Append(ctx, eventlog.Event{ID: id.New("evt"), Type: "task.created", Actor: "OrchestratorAgent", TaskID: task.ID, Payload: eventlog.Payload(task)})
	_ = o.events.Append(ctx, eventlog.Event{ID: id.New("evt"), Type: "remote_agent.task.queued", Actor: "OrchestratorAgent", TaskID: task.ID, Payload: eventlog.Payload(map[string]any{
		"agent_id": target.AgentID,
		"machine":  target.Machine,
		"workdir":  target.Workdir,
		"backend":  target.Backend,
	})})
	return task, nil
}

func (o *Orchestrator) summarizeTaskTitle(ctx context.Context, taskID, goal string) string {
	fallback := fallbackTaskTitle(goal, taskTitleMaxCharacters)
	if o.registry == nil {
		return fallback
	}
	if _, ok := o.registry.Get("text.summarize"); !ok {
		return fallback
	}
	raw, err := o.runTool(ctx, "OrchestratorAgent", "text.summarize", map[string]any{
		"text":           goal,
		"purpose":        "task_title",
		"max_characters": taskTitleMaxCharacters,
	}, taskID)
	if err != nil {
		o.log().Warn("task title summarisation failed", "task_id", taskID, "error", err)
		return fallback
	}
	var out struct {
		Summary string `json:"summary"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		o.log().Warn("task title summarisation returned invalid JSON", "task_id", taskID, "error", err)
		return fallback
	}
	title := cleanTaskTitle(out.Summary, taskTitleMaxCharacters)
	if title == "" {
		return fallback
	}
	return title
}

func fallbackTaskTitle(goal string, maxCharacters int) string {
	title := firstLine(goal)
	if title == "" {
		title = "untitled task"
	}
	return cleanTaskTitle(title, maxCharacters)
}

func cleanTaskTitle(value string, maxCharacters int) string {
	value = strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
	value = strings.Trim(value, " \t\r\n\"'`")
	for {
		lower := strings.ToLower(value)
		switch {
		case strings.HasPrefix(lower, "summary:"):
			value = strings.TrimSpace(value[len("summary:"):])
		case strings.HasPrefix(lower, "title:"):
			value = strings.TrimSpace(value[len("title:"):])
		default:
			return clipTaskTitle(value, maxCharacters)
		}
	}
}

func clipTaskTitle(value string, maxCharacters int) string {
	if maxCharacters <= 0 || len([]rune(value)) <= maxCharacters {
		return value
	}
	runes := []rune(value)
	if maxCharacters <= 3 {
		return string(runes[:maxCharacters])
	}
	limit := maxCharacters - 3
	clipped := strings.TrimSpace(string(runes[:limit]))
	if boundary := strings.LastIndex(clipped, " "); boundary >= limit*3/5 {
		clipped = clipped[:boundary]
	}
	clipped = strings.TrimRight(strings.TrimSpace(clipped), ".,;:-")
	if clipped == "" {
		return strings.TrimSpace(string(runes[:maxCharacters]))
	}
	return clipped + "..."
}

func (o *Orchestrator) createTaskGraphChildren(ctx context.Context, parent taskstore.Task, now time.Time) ([]taskstore.Task, string, error) {
	phases := defaultTaskGraphPhases(parent.Goal)
	children := make([]taskstore.Task, 0, len(phases))
	firstBranch := ""
	var previousID string
	for i, phase := range phases {
		child := taskstore.Task{
			ID:                 id.New("task"),
			Title:              phase.Title,
			Goal:               phase.Goal,
			Status:             taskstore.StatusBlocked,
			AssignedTo:         "OrchestratorAgent",
			Priority:           parent.Priority + i + 1,
			CreatedAt:          now,
			UpdatedAt:          now,
			ParentID:           parent.ID,
			ContextIDs:         []string{parent.ID},
			GraphPhase:         phase.Name,
			AcceptanceCriteria: phase.AcceptanceCriteria,
			Result:             "blocked on earlier graph phase",
		}
		if previousID == "" {
			child.Status = taskstore.StatusQueued
			child.Result = "queued as first graph phase"
		} else {
			child.DependsOn = []string{previousID}
			child.BlockedBy = []string{previousID}
			child.ContextIDs = append(child.ContextIDs, previousID)
		}
		o.ensureTaskPlan(ctx, &child)
		raw, err := o.runTool(ctx, "OrchestratorAgent", "git.worktree_create", map[string]any{"task_id": child.ID}, child.ID)
		if err != nil {
			child.Status = taskstore.StatusFailed
			child.Result = err.Error()
			_ = o.tasks.Save(child)
			return children, firstBranch, err
		}
		var out struct {
			Workspace string `json:"workspace"`
			Branch    string `json:"branch"`
		}
		_ = json.Unmarshal(raw, &out)
		child.Workspace = out.Workspace
		if firstBranch == "" {
			firstBranch = out.Branch
		}
		if err := o.tasks.Save(child); err != nil {
			return children, firstBranch, err
		}
		_ = o.events.Append(ctx, eventlog.Event{ID: id.New("evt"), Type: "task.created", Actor: "OrchestratorAgent", TaskID: child.ID, ParentID: parent.ID, Payload: eventlog.Payload(child)})
		children = append(children, child)
		previousID = child.ID
	}
	return children, firstBranch, nil
}

type taskGraphPhase struct {
	Name               string
	Title              string
	Goal               string
	AcceptanceCriteria []taskstore.AcceptanceCriterion
}

func defaultTaskGraphPhases(goal string) []taskGraphPhase {
	shortGoal := firstLine(goal)
	return []taskGraphPhase{
		{
			Name:  "inspect",
			Title: "inspect: " + shortGoal,
			Goal:  "Inspect the repository and current task context for: " + goal,
			AcceptanceCriteria: criteria(
				"Relevant files, commands, docs, and existing behavior are identified.",
				"Unknowns, risks, and likely validation needs are recorded in the task result.",
			),
		},
		{
			Name:  "design",
			Title: "design: " + shortGoal,
			Goal:  "Design the smallest implementation approach for: " + goal,
			AcceptanceCriteria: criteria(
				"Implementation boundaries, affected modules, and data contracts are described.",
				"Validation commands and expected user-facing behavior are listed before coding starts.",
			),
		},
		{
			Name:  "implement",
			Title: "implement: " + shortGoal,
			Goal:  "Implement the approved design for: " + goal,
			AcceptanceCriteria: criteria(
				"Code changes are made in the task workspace and match the design scope.",
				"Changed behavior is summarized with changed files and remaining risks.",
			),
		},
		{
			Name:  "test",
			Title: "test: " + shortGoal,
			Goal:  "Validate the implementation for: " + goal,
			AcceptanceCriteria: criteria(
				"Focused automated checks run and results are recorded.",
				"Browser/UAT validation is run for UI changes or explicitly marked not applicable.",
			),
		},
		{
			Name:  "docs",
			Title: "docs: " + shortGoal,
			Goal:  "Update operator docs, help text, or handoff notes for: " + goal,
			AcceptanceCriteria: criteria(
				"Relevant docs/help text are updated, or the result explains why no docs changed.",
				"Usage notes are ready for the final review handoff.",
			),
		},
		{
			Name:  "review",
			Title: "review: " + shortGoal,
			Goal:  "Review the completed task graph for: " + goal,
			AcceptanceCriteria: criteria(
				"All prior graph phases are accepted or explicitly waived.",
				"Final result states what changed, how it was validated, and what remains open.",
			),
		},
	}
}

func rootAcceptanceCriteria() []taskstore.AcceptanceCriterion {
	return criteria(
		"Inspect, design, implement, test, docs, and review child phases are completed in order.",
		"Final review confirms the original goal is satisfied or records remaining follow-up work.",
	)
}

func remoteAcceptanceCriteria(target taskstore.ExecutionTarget) []taskstore.AcceptanceCriterion {
	return criteria(
		"Remote agent runs in the selected directory and reports the commands or tools used.",
		"Final result states changed files, validation performed, and any follow-up needed on "+firstNonEmptyString(target.Machine, target.AgentID)+".",
	)
}

func criteria(descriptions ...string) []taskstore.AcceptanceCriterion {
	out := make([]taskstore.AcceptanceCriterion, 0, len(descriptions))
	for i, description := range descriptions {
		out = append(out, taskstore.AcceptanceCriterion{
			ID:          fmt.Sprintf("ac-%d", i+1),
			Description: description,
			Status:      "pending",
		})
	}
	return out
}

func taskGraphPhaseNames() []string {
	phases := defaultTaskGraphPhases("")
	names := make([]string, 0, len(phases))
	for _, phase := range phases {
		names = append(names, phase.Name)
	}
	return names
}

func taskIDs(tasks []taskstore.Task) []string {
	ids := make([]string, 0, len(tasks))
	for _, t := range tasks {
		ids = append(ids, t.ID)
	}
	return ids
}

func (o *Orchestrator) ensureTaskPlan(ctx context.Context, t *taskstore.Task) bool {
	if taskPlanReviewed(t.Plan) {
		return false
	}
	now := time.Now().UTC()
	plan := defaultTaskPlan(*t, now, o.taskPlanContext(ctx, *t))
	t.Plan = &plan
	_ = o.events.Append(ctx, eventlog.Event{ID: id.New("evt"), Type: "task.plan.created", Actor: "OrchestratorAgent", TaskID: t.ID, Payload: eventlog.Payload(plan)})
	_ = o.events.Append(ctx, eventlog.Event{ID: id.New("evt"), Type: "task.plan.reviewed", Actor: "ReviewerAgent", TaskID: t.ID, Payload: eventlog.Payload(map[string]any{"status": plan.Status, "review": plan.Review})})
	return true
}

func taskPlanReviewed(plan *taskstore.TaskPlan) bool {
	return plan != nil && strings.TrimSpace(plan.Summary) != "" && len(plan.Steps) > 0 && strings.EqualFold(plan.Status, "reviewed") && !legacyDefaultTaskPlan(plan)
}

const repoAwareTaskPlanReview = "OrchestratorAgent inspected repository context, generated a task-specific plan inside the default execution stages, and ReviewerAgent checked it before execution."

const legacyTaskSpecificDefaultPlanReview = "OrchestratorAgent generated this task-specific default plan and ReviewerAgent checked it includes the required execution stages before execution."

const legacyDefaultTaskPlanReview = "OrchestratorAgent generated this default plan and ReviewerAgent checked it includes inspect, change, validate, and handoff stages before execution."

func legacyDefaultTaskPlan(plan *taskstore.TaskPlan) bool {
	if plan == nil {
		return false
	}
	review := strings.TrimSpace(plan.Review)
	return review == legacyDefaultTaskPlanReview ||
		review == legacyTaskSpecificDefaultPlanReview ||
		(strings.Contains(review, "generated this default plan") && strings.Contains(review, "inspect, change, validate, and handoff")) ||
		strings.Contains(review, "generated this task-specific default plan")
}

func defaultTaskPlan(t taskstore.Task, now time.Time, planContext taskPlanContext) taskstore.TaskPlan {
	reviewedAt := now
	return taskstore.TaskPlan{
		Status:     "reviewed",
		Summary:    taskPlanSummary(t, planContext),
		Steps:      taskPlanSteps(t, planContext),
		Risks:      taskPlanRisks(t, planContext),
		Review:     repoAwareTaskPlanReview,
		CreatedAt:  now,
		ReviewedAt: &reviewedAt,
	}
}

type taskPlanContext struct {
	Files    []string
	Docs     []string
	Tests    []string
	Commands []string
}

func (c taskPlanContext) Empty() bool {
	return len(c.Files) == 0 && len(c.Docs) == 0 && len(c.Tests) == 0 && len(c.Commands) == 0
}

func (o *Orchestrator) taskPlanContext(ctx context.Context, t taskstore.Task) taskPlanContext {
	if t.Target != nil && strings.EqualFold(t.Target.Mode, "remote") {
		return taskPlanContext{}
	}
	root := strings.TrimSpace(t.Workspace)
	if root == "" || !pathExists(root) {
		root = strings.TrimSpace(o.cfg.Repo.Root)
	}
	if root == "" || !pathExists(root) {
		return taskPlanContext{}
	}
	terms := taskPlanSearchTerms(t)
	if len(terms) == 0 {
		return taskPlanContext{}
	}
	scores := map[string]int{}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if entry.IsDir() {
			if skipTaskPlanDir(root, path, entry.Name(), o.cfg.Repo.WorkspaceRoot, o.cfg.DataDir) {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil || strings.HasPrefix(rel, "..") {
			return nil
		}
		rel = filepath.ToSlash(rel)
		score := scoreTaskPlanPath(rel, "", terms)
		info, err := entry.Info()
		if err == nil && planTextFile(rel, info.Size()) {
			if b, err := os.ReadFile(path); err == nil {
				score += scoreTaskPlanPath(rel, strings.ToLower(string(b)), terms)
			}
		}
		if score > 0 {
			scores[rel] = score
		}
		return nil
	})
	if err != nil {
		o.log().Warn("task plan repository scan failed", "task_id", t.ID, "error", err)
	}
	planContext := taskPlanContext{
		Files: rankedTaskPlanPaths(scores, func(path string) bool {
			return !taskPlanDocPath(path) && !taskPlanTestPath(path)
		}, 4),
		Docs: rankedTaskPlanPaths(scores, taskPlanDocPath, 3),
		Tests: rankedTaskPlanPaths(scores, func(path string) bool {
			return taskPlanTestPath(path)
		}, 3),
	}
	planContext.Commands = taskPlanValidationCommands(planContext)
	return planContext
}

func pathExists(path string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	_, err := os.Stat(path)
	return err == nil
}

func skipTaskPlanDir(root, path, name, workspaceRoot, dataDir string) bool {
	switch name {
	case ".git", "node_modules", "dist", "build", ".svelte-kit", "coverage", ".cache", "vendor":
		return true
	}
	for _, external := range []string{workspaceRoot, dataDir} {
		if external == "" {
			continue
		}
		rel, err := filepath.Rel(root, external)
		if err != nil || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
			continue
		}
		externalInRoot := filepath.Join(root, rel)
		if samePath(path, externalInRoot) {
			return true
		}
	}
	return false
}

func samePath(a, b string) bool {
	aa, errA := filepath.Abs(a)
	bb, errB := filepath.Abs(b)
	if errA != nil || errB != nil {
		return filepath.Clean(a) == filepath.Clean(b)
	}
	return aa == bb
}

func planTextFile(path string, size int64) bool {
	if size <= 0 || size > 128*1024 {
		return false
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".go", ".ts", ".js", ".mjs", ".svelte", ".css", ".html", ".md", ".json", ".nix", ".sh", ".yaml", ".yml", ".toml", ".txt":
		return true
	default:
		return false
	}
}

func scoreTaskPlanPath(path, content string, terms []string) int {
	lowerPath := strings.ToLower(path)
	score := 0
	for _, term := range terms {
		term = strings.ToLower(strings.TrimSpace(term))
		if term == "" {
			continue
		}
		if strings.Contains(lowerPath, term) {
			if strings.Contains(term, "/") || strings.Contains(term, ".") {
				score += 14
			} else {
				score += 7
			}
		}
		if content != "" && strings.Contains(content, term) {
			if strings.Contains(term, " ") || strings.Contains(term, ".") {
				score += 8
			} else {
				score += 3
			}
		}
	}
	if score > 0 && strings.HasPrefix(path, "docs/") {
		score += 2
	}
	if score > 0 && taskPlanTestPath(path) {
		score += 2
	}
	return score
}

func taskPlanSearchTerms(t taskstore.Task) []string {
	var raw strings.Builder
	raw.WriteString(t.Title)
	raw.WriteByte(' ')
	raw.WriteString(t.Goal)
	for _, criterion := range t.AcceptanceCriteria {
		raw.WriteByte(' ')
		raw.WriteString(criterion.Description)
	}
	text := strings.ToLower(raw.String())
	seen := map[string]bool{}
	var terms []string
	add := func(term string) {
		term = strings.ToLower(strings.TrimSpace(term))
		if term == "" || seen[term] {
			return
		}
		seen[term] = true
		terms = append(terms, term)
	}
	for _, token := range strings.FieldsFunc(text, func(r rune) bool {
		return !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9') && r != '/' && r != '.' && r != '_' && r != '-'
	}) {
		token = strings.Trim(token, ".,:;()[]{}'\"`")
		if taskPlanStopWord(token) {
			continue
		}
		add(token)
		if len(token) > 5 && strings.HasSuffix(token, "s") {
			add(strings.TrimSuffix(token, "s"))
		}
	}
	if strings.Contains(text, "task plan") || strings.Contains(text, "planner") || strings.Contains(text, "planning") || strings.Contains(text, " plan") {
		add("taskplan")
		add("task plan")
		add("ensuretaskplan")
		add("defaulttaskplan")
		add("taskplansteps")
		add("planning gate")
	}
	if strings.Contains(text, "agent") || strings.Contains(text, "orchestrator") {
		add("orchestratoragent")
		add("pkg/agent")
		add("prompts/orchestrator")
	}
	if strings.Contains(text, "repo") || strings.Contains(text, "repository") {
		add("repo.search")
		add("repo.read")
		add("repository agent tools")
		add("pkg/tools/repo")
	}
	if strings.Contains(text, "internet") || strings.Contains(text, "online") || strings.Contains(text, "research") {
		add("internet.search")
		add("internet.research")
	}
	if strings.Contains(text, "dashboard") || strings.Contains(text, "ui") || strings.Contains(text, "browser") {
		add("web/dashboard")
		add("uat:tasks")
		add("svelte")
	}
	if strings.Contains(text, "homelabctl") || strings.Contains(text, "cli") || strings.Contains(text, "command") {
		add("cmd/homelabctl")
		add("docs/homelabctl.md")
	}
	if len(terms) > 32 {
		return terms[:32]
	}
	return terms
}

func taskPlanStopWord(token string) bool {
	if len(token) < 4 {
		return true
	}
	switch token {
	case "about", "after", "also", "because", "before", "best", "build", "change", "completion", "context", "default", "does", "doing", "done", "find", "fine", "from", "have", "into", "like", "look", "looks", "make", "mostly", "need", "possible", "really", "same", "seem", "seems", "sensible", "some", "task", "that", "their", "them", "then", "there", "this", "want", "within", "with", "work", "would":
		return true
	default:
		return false
	}
}

func rankedTaskPlanPaths(scores map[string]int, include func(string) bool, limit int) []string {
	type scoredPath struct {
		Path  string
		Score int
	}
	var paths []scoredPath
	for path, score := range scores {
		if score <= 0 || !include(path) {
			continue
		}
		paths = append(paths, scoredPath{Path: path, Score: score})
	}
	sort.Slice(paths, func(i, j int) bool {
		if paths[i].Score == paths[j].Score {
			return paths[i].Path < paths[j].Path
		}
		return paths[i].Score > paths[j].Score
	})
	if len(paths) > limit {
		paths = paths[:limit]
	}
	out := make([]string, 0, len(paths))
	for _, path := range paths {
		out = append(out, path.Path)
	}
	return out
}

func taskPlanDocPath(path string) bool {
	return strings.HasPrefix(path, "docs/") && strings.EqualFold(filepath.Ext(path), ".md")
}

func taskPlanTestPath(path string) bool {
	lower := strings.ToLower(path)
	return strings.HasSuffix(lower, "_test.go") ||
		strings.HasSuffix(lower, ".test.ts") ||
		strings.HasSuffix(lower, ".spec.ts") ||
		strings.Contains(lower, "/e2e/")
}

func taskPlanValidationCommands(planContext taskPlanContext) []string {
	seen := map[string]bool{}
	var commands []string
	add := func(command string) {
		if command == "" || seen[command] {
			return
		}
		seen[command] = true
		commands = append(commands, command)
	}
	for _, path := range append(append([]string{}, planContext.Files...), planContext.Tests...) {
		if strings.HasSuffix(path, ".go") {
			if pkg := goTestPackageForPath(path); pkg != "" {
				add("go test " + pkg)
			}
		}
		if strings.HasPrefix(path, "web/") {
			add("nix develop -c bun run --cwd web check")
			if strings.Contains(path, "routes/tasks") {
				add("nix develop -c bun run --cwd web uat:tasks")
			} else if strings.Contains(path, "routes/") || strings.Contains(path, "dashboard/src/") {
				add("nix develop -c bun run --cwd web uat:site")
			}
		}
	}
	if len(planContext.Docs) > 0 {
		add("review docs diff")
	}
	if len(commands) > 4 {
		return commands[:4]
	}
	return commands
}

func goTestPackageForPath(path string) string {
	dir := filepath.ToSlash(filepath.Dir(path))
	if dir == "." || dir == "" {
		return "./..."
	}
	return "./" + dir
}

func taskPlanSummary(t taskstore.Task, planContext taskPlanContext) string {
	subject := firstLine(t.Goal)
	if subject == "" {
		subject = "the task goal"
	}
	context := taskPlanContextLabel(planContext)
	switch strings.ToLower(strings.TrimSpace(t.GraphPhase)) {
	case "root":
		return "Plan to coordinate the task graph for: " + subject
	case "inspect":
		return appendTaskPlanContextLabel("Plan for the inspect phase: "+subject, context)
	case "design":
		return appendTaskPlanContextLabel("Plan for the design phase: "+subject, context)
	case "implement":
		return appendTaskPlanContextLabel("Plan for the implement phase: "+subject, context)
	case "test":
		return appendTaskPlanContextLabel("Plan for the test phase: "+subject, context)
	case "docs":
		return appendTaskPlanContextLabel("Plan for the docs phase: "+subject, context)
	case "review":
		return appendTaskPlanContextLabel("Plan for the review phase: "+subject, context)
	}
	if t.Target != nil && strings.EqualFold(t.Target.Mode, "remote") {
		target := firstNonEmptyString(t.Target.Workdir, t.Target.WorkdirID, t.Target.AgentID)
		if target != "" {
			return "Plan to complete the remote task on " + target + ": " + subject
		}
		return "Plan to complete the remote task: " + subject
	}
	return appendTaskPlanContextLabel("Plan to satisfy: "+subject, context)
}

func taskPlanContextLabel(planContext taskPlanContext) string {
	for _, group := range [][]string{planContext.Files, planContext.Docs, planContext.Tests} {
		if len(group) > 0 {
			if len(group) == 1 {
				return group[0]
			}
			return group[0] + ", " + group[1]
		}
	}
	return ""
}

func appendTaskPlanContextLabel(summary, context string) string {
	if context == "" {
		return summary
	}
	return summary + " (repo scan: " + context + ")"
}

func taskPlanSteps(t taskstore.Task, planContext taskPlanContext) []taskstore.TaskPlanStep {
	switch strings.ToLower(strings.TrimSpace(t.GraphPhase)) {
	case "root":
		return []taskstore.TaskPlanStep{
			{Title: "Coordinate task graph", Detail: "Keep the original goal and acceptance criteria on the parent task while child phases drive execution."},
			{Title: "Release the inspect phase", Detail: "Create isolated child worktrees and start only the first unblocked phase."},
			{Title: "Track phase acceptance", Detail: "Advance later phases only after dependencies are accepted or explicitly waived."},
			{Title: "Prepare final handoff", Detail: "Confirm validation, documentation, and remaining risks across the completed graph."},
		}
	case "inspect":
		return []taskstore.TaskPlanStep{
			{Title: "Map affected surface", Detail: "Identify relevant files, commands, docs, and current behaviour before proposing edits."},
			{Title: "Check task context", Detail: "Read the task goal, graph context, dependencies, and recent repository state."},
			{Title: "Record findings", Detail: "Capture unknowns, risks, and likely validation needs in the task result."},
			{Title: "Handoff inspection", Detail: "State whether the next phase should change code, docs, tests, or workflow."},
		}
	case "design":
		return []taskstore.TaskPlanStep{
			{Title: "Define implementation boundary", Detail: "Name the modules, data contracts, and behaviours the smallest fix should touch."},
			{Title: "Choose the approach", Detail: "Prefer existing repository patterns and avoid broad refactors unless required."},
			{Title: "Plan validation", Detail: "List targeted tests, formatting, builds, and any browser/UAT checks needed before coding starts."},
			{Title: "Surface open questions", Detail: "Record blockers, assumptions, and risks for the implementation phase."},
		}
	case "implement":
		return []taskstore.TaskPlanStep{
			{Title: "Apply scoped changes", Detail: "Edit only the files needed for the approved design inside the task worktree."},
			{Title: "Keep docs in sync", Detail: "Update docs or help text when behaviour, commands, UI, configuration, tools, or workflow changes."},
			{Title: "Add regression coverage", Detail: "Include focused automated coverage for fixed bugs or changed contracts."},
			{Title: "Prepare implementation notes", Detail: "Summarize changed files, user impact, and remaining risks for validation."},
		}
	case "test":
		return []taskstore.TaskPlanStep{
			{Title: "Run targeted checks", Detail: "Execute formatting, builds, and automated tests that match the files changed."},
			{Title: "Exercise UI when relevant", Detail: "Run browser/UAT checks for changed pages and verify the reported interaction."},
			{Title: "Record results", Detail: "Capture commands, outcomes, skipped checks, and any failures in the task result."},
			{Title: "Identify residual risk", Detail: "Call out untested surfaces or environment blockers before review."},
		}
	case "docs":
		return []taskstore.TaskPlanStep{
			{Title: "Find affected docs", Detail: "Locate operator docs, help text, examples, and workflow notes tied to the behaviour changed."},
			{Title: "Update discoverable guidance", Detail: "Use concise British English with clear titles, searchable terms, related links, and current examples."},
			{Title: "Check consistency", Detail: "Keep docs, commands, UI, configuration, tools, and workflows aligned with the implementation."},
			{Title: "Report docs status", Detail: "State exactly what changed or why no documentation update was needed."},
		}
	case "review":
		return []taskstore.TaskPlanStep{
			{Title: "Review phase outcomes", Detail: "Confirm prior graph phases are accepted, waived, or clearly blocked."},
			{Title: "Inspect diff and tests", Detail: "Check correctness, regression coverage, docs, and validation evidence before approval."},
			{Title: "Verify handoff quality", Detail: "Ensure changed files, validation, usage notes, docs status, and risks are clear."},
			{Title: "Choose final action", Detail: "Approve, reopen, or record follow-up work based on the evidence."},
		}
	}
	if t.Target != nil && strings.EqualFold(t.Target.Mode, "remote") {
		return []taskstore.TaskPlanStep{
			{Title: "Confirm remote target", Detail: "Verify the selected agent, machine, backend, and working directory before work starts."},
			{Title: "Run in remote checkout", Detail: "Execute the requested change only in the advertised remote directory."},
			{Title: "Validate remotely", Detail: "Run relevant checks available on that machine and report any environment limits."},
			{Title: "Report remote result", Detail: "Return changed files, validation, usage notes, docs status, and follow-up needs."},
		}
	}
	return []taskstore.TaskPlanStep{
		{Title: "Inspect repo-grounded scope", Detail: taskPlanInspectDetail(planContext)},
		{Title: "Make a minimal workspace change", Detail: taskPlanChangeDetail(planContext)},
		{Title: "Validate the change", Detail: taskPlanValidationDetail(planContext)},
		{Title: "Summarize and hand off", Detail: "Report changed files, validation results, how to use the change, docs updates, and remaining risks before merge approval."},
	}
}

func taskPlanInspectDetail(planContext taskPlanContext) string {
	if planContext.Empty() {
		return "Read the task goal, current task state, repository search results, and recent context before editing."
	}
	return "Start with the repository scan: " + taskPlanPathSentence(planContext.Files, "likely code") + taskPlanPathSentence(planContext.Docs, "docs") + taskPlanPathSentence(planContext.Tests, "tests") + " Confirm the actual affected surface before editing."
}

func taskPlanChangeDetail(planContext taskPlanContext) string {
	if len(planContext.Files) == 0 {
		return "Apply the smallest practical patch inside the isolated task worktree after confirming the affected files."
	}
	return "Keep edits centred on " + strings.Join(planContext.Files, ", ") + " unless inspection proves another path owns the behaviour."
}

func taskPlanValidationDetail(planContext taskPlanContext) string {
	if len(planContext.Commands) == 0 {
		return "Run targeted formatting, build, tests, or browser checks that match the files touched."
	}
	return "Start with " + strings.Join(planContext.Commands, "; ") + ", then add any checks required by the final diff."
}

func taskPlanPathSentence(paths []string, label string) string {
	if len(paths) == 0 {
		return ""
	}
	return " " + label + ": " + strings.Join(paths, ", ") + "."
}

func taskPlanRisks(t taskstore.Task, planContext taskPlanContext) []string {
	var risks []string
	switch strings.ToLower(strings.TrimSpace(t.GraphPhase)) {
	case "root":
		risks = []string{
			"Child phases can drift from the original goal if dependencies or acceptance criteria are unclear.",
			"The graph parent must remain blocked until phase results are accepted or explicitly waived.",
		}
	case "inspect":
		risks = []string{
			"Affected files and components may be broader than the initial task wording suggests.",
			"Inspection should avoid speculative edits unless a minimal fix is clearly required.",
		}
	case "design":
		risks = []string{
			"The design may miss hidden contracts if inspection findings are incomplete.",
			"Validation needs must be explicit before implementation starts.",
		}
	case "implement":
		risks = []string{
			"Unrelated workspace changes must be preserved while applying the patch.",
			"Behaviour changes need matching tests and docs/help text updates.",
		}
	case "test":
		risks = []string{
			"Environment limits can block full validation and must be recorded clearly.",
			"UI changes require browser-level checks, not compile checks alone.",
		}
	case "docs":
		risks = []string{
			"Docs can become misleading if examples or command names drift from the implementation.",
			"Operator-facing workflow changes need discoverable help text as well as prose docs.",
		}
	case "review":
		risks = []string{
			"Approval should not proceed if checks, docs, or merge readiness are unclear.",
			"Remaining risks must be visible before final acceptance.",
		}
	default:
		if t.Target != nil && strings.EqualFold(t.Target.Mode, "remote") {
			risks = []string{
				"Remote execution can affect a checkout outside the local control-plane repository.",
				"Local review acknowledges the remote result but cannot compare or merge that checkout.",
			}
		} else {
			risks = []string{
				"Affected files and components are unknown until inspection finishes.",
				"The task must stay inside its workspace until review and approval gates pass.",
			}
		}
	}
	if !planContext.Empty() && (t.Target == nil || !strings.EqualFold(t.Target.Mode, "remote")) {
		risks = append(risks, "The repository scan is only a starting point; imports, callers, generated code, and UI flows still need verification before editing.")
	}
	return risks
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
		if t.Target != nil && t.Target.Mode != "" {
			fmt.Fprintf(&b, "  target: %s\n", formatTaskTarget(*t.Target))
		}
		if t.GraphPhase != "" {
			fmt.Fprintf(&b, "  graph: %s%s\n", t.GraphPhase, graphRelationSummary(t))
		}
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
		fmt.Fprintf(&b, "- %s [%s] %s\n  state: %s\n  next: %s\n", taskShortID(t.ID), t.Status, friendlyTaskTitle(t), taskStateDescription(t.Status), nextActionsForTaskWithPrimary(t, next))
	}
	if b.Len() == 0 {
		return "Nothing active. Use `new <goal>` to create a task.", nil
	}
	return "In flight:\n" + strings.TrimSpace(b.String()), nil
}

func nextActionForTask(t taskstore.Task) string {
	shortID := taskShortID(t.ID)
	if t.GraphPhase == "root" {
		return fmt.Sprintf("show %s", shortID)
	}
	switch t.Status {
	case taskstore.StatusAwaitingApproval:
		return "approvals"
	case taskstore.StatusAwaitingVerification:
		return fmt.Sprintf("accept %s", shortID)
	case taskstore.StatusReadyForReview:
		return fmt.Sprintf("review %s", shortID)
	case taskstore.StatusConflictResolution:
		return fmt.Sprintf("retry %s codex resolve the main-branch conflict", shortID)
	case taskstore.StatusBlocked:
		if len(t.BlockedBy) > 0 {
			return fmt.Sprintf("show %s", shortID)
		}
		result := strings.ToLower(t.Result)
		if strings.Contains(result, "external agent returned") || strings.Contains(result, "finished") || strings.Contains(result, "diff") {
			return fmt.Sprintf("review %s", shortID)
		}
		return fmt.Sprintf("start %s", shortID)
	case taskstore.StatusFailed:
		return fmt.Sprintf("start %s", shortID)
	default:
		if t.AssignedTo != "" && t.AssignedTo != "OrchestratorAgent" && t.AssignedTo != "CoderAgent" && t.AssignedTo != "UXAgent" {
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
	if t.GraphPhase == "root" {
		return fmt.Sprintf("`%s` or `delete %s`", primary, shortID)
	}
	if len(t.BlockedBy) > 0 {
		return fmt.Sprintf("`%s` after blockers %s, or `delete %s`", primary, strings.Join(shortTaskIDs(t.BlockedBy), ", "), shortID)
	}
	if t.Status == taskstore.StatusAwaitingVerification {
		return fmt.Sprintf("`%s`, `reopen %s needs rework`, or `delete %s`", primary, shortID, shortID)
	}
	return fmt.Sprintf("`%s`, `ux %s`, `delegate %s to codex`, or `delete %s`", primary, shortID, shortID, shortID)
}

func taskStateDescription(status string) string {
	switch status {
	case taskstore.StatusQueued:
		return "waiting for the task supervisor to assign a worker"
	case taskstore.StatusRunning:
		return "a worker owns this task; wait for completion or inspect progress"
	case taskstore.StatusReadyForReview:
		return "worker finished; review gate has not passed yet"
	case taskstore.StatusConflictResolution:
		return "task branch conflicts with current main; manual conflict resolution is required"
	case taskstore.StatusBlocked:
		return "review or execution stopped; a human or worker must choose the next action"
	case taskstore.StatusAwaitingApproval:
		return "review gate passed; merge approval is pending"
	case taskstore.StatusAwaitingVerification:
		return "merge landed; verify the running app before accepting"
	case taskstore.StatusDone:
		return "accepted by the human"
	case taskstore.StatusFailed:
		return "runtime failure; rerun or delegate with failure context"
	case taskstore.StatusCancelled:
		return "intentionally stopped"
	default:
		return "unknown state"
	}
}

func taskStateTransitions(status string) string {
	switch status {
	case taskstore.StatusQueued:
		return "queued -> running"
	case taskstore.StatusRunning:
		return "running -> ready_for_review or blocked"
	case taskstore.StatusReadyForReview:
		return "ready_for_review -> awaiting_approval, conflict_resolution, or blocked"
	case taskstore.StatusConflictResolution:
		return "conflict_resolution -> running, ready_for_review, cancelled, or deleted"
	case taskstore.StatusBlocked:
		return "blocked -> running, cancelled, or deleted"
	case taskstore.StatusAwaitingApproval:
		return "awaiting_approval -> awaiting_verification, conflict_resolution, blocked, or running"
	case taskstore.StatusAwaitingVerification:
		return "awaiting_verification -> done or queued"
	case taskstore.StatusDone, taskstore.StatusCancelled:
		return "terminal"
	case taskstore.StatusFailed:
		return "failed -> running, cancelled, or deleted"
	default:
		return "unknown"
	}
}

func remoteTask(t taskstore.Task) bool {
	return t.Target != nil && strings.EqualFold(t.Target.Mode, "remote")
}

func remoteTaskForAgent(t taskstore.Task, agentID string) bool {
	return remoteTask(t) && strings.EqualFold(strings.TrimSpace(t.Target.AgentID), strings.TrimSpace(agentID))
}

func resolveAdvertisedWorkdir(agent remoteagent.Agent, target *taskstore.ExecutionTarget) error {
	if target == nil {
		return fmt.Errorf("remote target is required")
	}
	target.Machine = firstNonEmptyString(target.Machine, agent.Machine)
	target.WorkdirID = strings.TrimSpace(target.WorkdirID)
	target.Workdir = strings.TrimSpace(target.Workdir)
	if target.WorkdirID == "" && target.Workdir == "" {
		return fmt.Errorf("remote working directory is required")
	}
	if len(agent.Workdirs) == 0 {
		return fmt.Errorf("remote working directory is required; remote agent %q has no advertised working directories", agent.ID)
	}
	var matched *remoteagent.Workdir
	for _, workdir := range agent.Workdirs {
		workdir.ID = strings.TrimSpace(workdir.ID)
		workdir.Path = strings.TrimSpace(workdir.Path)
		if workdir.Path == "" {
			continue
		}
		if target.WorkdirID != "" && workdir.ID == target.WorkdirID {
			copy := workdir
			matched = &copy
			break
		}
		if target.WorkdirID == "" && target.Workdir != "" && workdir.Path == target.Workdir {
			copy := workdir
			matched = &copy
			break
		}
	}
	if matched == nil {
		value := firstNonEmptyString(target.WorkdirID, target.Workdir)
		return fmt.Errorf("remote working directory %q is not advertised by agent %s", value, agent.ID)
	}
	if target.Workdir != "" && target.Workdir != matched.Path {
		return fmt.Errorf("remote working directory %q does not match advertised path %q for agent %s", target.Workdir, matched.Path, agent.ID)
	}
	target.WorkdirID = matched.ID
	target.Workdir = matched.Path
	return nil
}

func formatTaskTarget(target taskstore.ExecutionTarget) string {
	parts := []string{target.Mode}
	if target.AgentID != "" {
		parts = append(parts, "agent="+target.AgentID)
	}
	if target.Machine != "" {
		parts = append(parts, "machine="+target.Machine)
	}
	if target.Workdir != "" {
		parts = append(parts, "dir="+target.Workdir)
	}
	if target.Backend != "" {
		parts = append(parts, "backend="+target.Backend)
	}
	return strings.Join(parts, " ")
}

func defaultRemoteAgentInstruction(t taskstore.Task, agent remoteagent.Agent) string {
	return strings.Join([]string{
		"Work this task in the selected remote directory.",
		"Agent: " + agent.ID + " on " + firstNonEmptyString(agent.Machine, "unknown machine") + ".",
		"Goal: " + t.Goal,
		"",
		"Inspect the directory first, make the smallest practical change, run relevant validation, and report changed files plus commands run.",
	}, "\n")
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
	fmt.Fprintf(&b, "state: %s\n", taskStateDescription(t.Status))
	fmt.Fprintf(&b, "transitions: %s\n", taskStateTransitions(t.Status))
	fmt.Fprintf(&b, "id: %s\n", t.ID)
	if t.AssignedTo != "" {
		fmt.Fprintf(&b, "assigned: %s\n", t.AssignedTo)
	}
	if t.Workspace != "" {
		fmt.Fprintf(&b, "workspace: %s\n", t.Workspace)
	}
	if t.Target != nil && t.Target.Mode != "" {
		fmt.Fprintf(&b, "target: %s\n", formatTaskTarget(*t.Target))
	}
	if t.Plan != nil {
		fmt.Fprintf(&b, "plan: %s - %s\n", t.Plan.Status, t.Plan.Summary)
	}
	if t.GraphPhase != "" {
		fmt.Fprintf(&b, "graph: %s%s\n", t.GraphPhase, graphRelationSummary(t))
	}
	if len(t.DependsOn) > 0 {
		fmt.Fprintf(&b, "depends on: %s\n", strings.Join(shortTaskIDs(t.DependsOn), ", "))
	}
	if len(t.BlockedBy) > 0 {
		fmt.Fprintf(&b, "blocked by: %s\n", strings.Join(shortTaskIDs(t.BlockedBy), ", "))
	}
	if len(t.AcceptanceCriteria) > 0 {
		fmt.Fprintf(&b, "acceptance criteria:\n%s\n", formatAcceptanceCriteria(t.AcceptanceCriteria))
	}
	if t.GraphPhase == "root" {
		if children, err := o.graphChildren(t.ID); err == nil && len(children) > 0 {
			fmt.Fprintf(&b, "child phases:\n%s\n", formatGraphChildren(children))
		}
	}
	if strings.TrimSpace(t.Result) != "" {
		fmt.Fprintf(&b, "result: %s\n", strings.TrimSpace(t.Result))
	}
	if !taskTerminal(t.Status) {
		fmt.Fprintf(&b, "next: %s", nextActionsForTask(t))
	}
	return strings.TrimSpace(b.String()), nil
}

func graphRelationSummary(t taskstore.Task) string {
	var parts []string
	if t.ParentID != "" {
		parts = append(parts, "parent="+taskShortID(t.ParentID))
	}
	if len(t.DependsOn) > 0 {
		parts = append(parts, "depends_on="+strings.Join(shortTaskIDs(t.DependsOn), ","))
	}
	if len(t.BlockedBy) > 0 {
		parts = append(parts, "blocked_by="+strings.Join(shortTaskIDs(t.BlockedBy), ","))
	}
	if len(parts) == 0 {
		return ""
	}
	return " (" + strings.Join(parts, " ") + ")"
}

func formatAcceptanceCriteria(criteria []taskstore.AcceptanceCriterion) string {
	var b strings.Builder
	for _, criterion := range criteria {
		status := criterion.Status
		if status == "" {
			status = "pending"
		}
		fmt.Fprintf(&b, "- [%s] %s %s\n", status, criterion.ID, criterion.Description)
	}
	return strings.TrimSpace(b.String())
}

func (o *Orchestrator) graphChildren(parentID string) ([]taskstore.Task, error) {
	tasks, err := o.tasks.List()
	if err != nil {
		return nil, err
	}
	var children []taskstore.Task
	for _, task := range tasks {
		if task.ParentID == parentID {
			children = append(children, task)
		}
	}
	sort.Slice(children, func(i, j int) bool { return children[i].Priority < children[j].Priority })
	return children, nil
}

func formatGraphChildren(children []taskstore.Task) string {
	var b strings.Builder
	for _, child := range children {
		fmt.Fprintf(&b, "- %s [%s] %s", taskShortID(child.ID), child.Status, child.GraphPhase)
		if len(child.DependsOn) > 0 {
			fmt.Fprintf(&b, " after %s", strings.Join(shortTaskIDs(child.DependsOn), ", "))
		}
		b.WriteByte('\n')
	}
	return strings.TrimSpace(b.String())
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
	if err := o.stalePendingTaskApprovals(ctx, taskID, "task cancelled by human"); err != nil {
		return "", err
	}
	t.Status = taskstore.StatusCancelled
	t.AssignedTo = "OrchestratorAgent"
	t.Result = "cancelled by human"
	if err := o.tasks.Save(t); err != nil {
		return "", err
	}
	activeRun, stopped := o.cancelTaskActiveRun(taskID)
	payload := map[string]any{"workspace": t.Workspace, "stopped_active_worker": stopped}
	if activeRun.RunID != "" {
		payload["run_id"] = activeRun.RunID
	}
	if activeRun.Worker != "" {
		payload["worker"] = activeRun.Worker
	}
	_ = o.events.Append(ctx, eventlog.Event{ID: id.New("evt"), Type: "task.cancelled", Actor: "human", TaskID: taskID, Payload: eventlog.Payload(payload)})
	return fmt.Sprintf("Cancelled %s. Workspace kept at %s.", taskID, t.Workspace), nil
}

func (o *Orchestrator) retryTask(ctx context.Context, selector, backend, instruction string) (string, error) {
	taskID, err := o.resolveTaskID(selector)
	if err != nil {
		return "", err
	}
	t, err := o.tasks.Load(taskID)
	if err != nil {
		return "", err
	}
	if o.taskActive(taskID) {
		return "", fmt.Errorf("task %s is already running", taskShortID(taskID))
	}
	if strings.TrimSpace(backend) == "" {
		backend = externalBackendForTask(t)
	}
	if strings.TrimSpace(instruction) == "" {
		instruction = strings.Join([]string{
			"Retry this task from the current workspace state.",
			"Inspect the latest task result, worker run trace, and git diff before editing.",
			"Fix or complete the task with the smallest useful patch.",
			"Run relevant validation and summarize changed files, validation, and any remaining risk.",
		}, "\n")
	}
	if err := o.startDelegationForTask(ctx, taskID, backend, instruction); err != nil {
		return "", err
	}
	return fmt.Sprintf("Retried %s on %s. The worker is running in the background.", taskShortID(taskID), backend), nil
}

func externalBackendForTask(t taskstore.Task) string {
	switch strings.ToLower(strings.TrimSpace(t.AssignedTo)) {
	case "codex", "claude", "gemini":
		return strings.ToLower(strings.TrimSpace(t.AssignedTo))
	default:
		return "codex"
	}
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
	if err := o.stalePendingTaskApprovals(ctx, taskID, "task accepted by human"); err != nil {
		return "", err
	}
	t.Status = taskstore.StatusDone
	t.AssignedTo = "OrchestratorAgent"
	t.AcceptanceCriteria = markAcceptanceCriteria(t.AcceptanceCriteria, "accepted")
	if strings.TrimSpace(t.Result) == "" {
		t.Result = "accepted by human"
	} else {
		t.Result = strings.TrimSpace(t.Result) + "\naccepted by human"
	}
	if err := o.tasks.Save(t); err != nil {
		return "", err
	}
	_ = o.events.Append(ctx, eventlog.Event{ID: id.New("evt"), Type: "task.completed", Actor: "human", TaskID: taskID, Payload: eventlog.Payload(map[string]any{"result": t.Result})})
	released, releaseErr := o.releaseGraphDependents(ctx, taskID)
	if releaseErr != nil {
		return "", releaseErr
	}
	parentDone := false
	if t.ParentID != "" {
		parentDone, releaseErr = o.completeGraphParentIfReady(ctx, t.ParentID)
		if releaseErr != nil {
			return "", releaseErr
		}
	}
	graphLine := ""
	if released > 0 {
		graphLine = fmt.Sprintf("\nReleased %d dependent graph phase(s).", released)
	}
	if parentDone {
		graphLine += "\nAll child phases are done; parent task is now done."
	}
	return fmt.Sprintf("Accepted %s. Task is now done.%s\nUsage/docs notes:\n%s", taskShortID(taskID), graphLine, usageNotesFromResult(t.Result)), nil
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
	if err := o.stalePendingTaskApprovals(ctx, taskID, reason); err != nil {
		return "", err
	}
	t.Status = taskstore.StatusQueued
	t.AssignedTo = "OrchestratorAgent"
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
	return fmt.Sprintf("Reopened %s and queued it for the task supervisor.\nNext: `status`, `delegate %s to codex %s`, or `run %s`.", shortID, shortID, instruction, shortID), nil
}

func (o *Orchestrator) refreshTaskWorkspace(ctx context.Context, selector string) (string, error) {
	taskID, err := o.resolveTaskID(selector)
	if err != nil {
		return "", err
	}
	t, err := o.tasks.Load(taskID)
	if err != nil {
		return "", err
	}
	if err := safeTaskWorkspace(o.cfg.Repo.WorkspaceRoot, t.Workspace); err != nil {
		return "", err
	}
	headOut, err := exec.CommandContext(ctx, "git", "-C", o.cfg.Repo.Root, "rev-parse", "HEAD").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git rev-parse repo head: %w: %s", err, strings.TrimSpace(string(headOut)))
	}
	head := strings.TrimSpace(string(headOut))
	_ = exec.CommandContext(context.Background(), "git", "-C", t.Workspace, "merge", "--abort").Run()
	_ = exec.CommandContext(context.Background(), "git", "-C", t.Workspace, "rebase", "--abort").Run()
	resetOut, err := exec.CommandContext(ctx, "git", "-C", t.Workspace, "reset", "--hard", head).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git reset task workspace: %w: %s", err, strings.TrimSpace(string(resetOut)))
	}
	cleanOut, err := exec.CommandContext(ctx, "git", "-C", t.Workspace, "clean", "-fd", "-e", ".codex", "-e", ".git-local", "-e", ".artifacts", "--", ".").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git clean task workspace: %w: %s", err, strings.TrimSpace(string(cleanOut)))
	}
	if err := o.stalePendingTaskApprovals(ctx, taskID, "workspace refreshed to current main"); err != nil {
		return "", err
	}
	t.Status = taskstore.StatusBlocked
	t.AssignedTo = "OrchestratorAgent"
	t.Result = fmt.Sprintf("workspace refreshed to current main %s; delegate work from scratch", head)
	if err := o.tasks.Save(t); err != nil {
		return "", err
	}
	_ = o.events.Append(ctx, eventlog.Event{ID: id.New("evt"), Type: "task.workspace.refreshed", Actor: "OrchestratorAgent", TaskID: taskID, Payload: eventlog.Payload(map[string]any{"head": head, "workspace": t.Workspace})})
	shortID := taskShortID(taskID)
	return fmt.Sprintf("Refreshed %s to current main %s.\nNext: `delegate %s to codex <instruction>`.", shortID, head, shortID), nil
}

func safeTaskWorkspace(workspaceRoot, workspace string) error {
	if strings.TrimSpace(workspace) == "" {
		return fmt.Errorf("task has no workspace")
	}
	root, err := filepath.Abs(workspaceRoot)
	if err != nil {
		return err
	}
	path, err := filepath.Abs(workspace)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return err
	}
	if rel == "." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rel == ".." || filepath.IsAbs(rel) {
		return fmt.Errorf("workspace %q is outside workspace root %q", workspace, workspaceRoot)
	}
	if !workspaceHasGit(path) {
		return fmt.Errorf("workspace %q is not a git worktree", workspace)
	}
	return nil
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
			strings.Contains(normalized, "documentation") ||
			strings.HasPrefix(normalized, "restart plan:") {
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

func markAcceptanceCriteria(criteria []taskstore.AcceptanceCriterion, status string) []taskstore.AcceptanceCriterion {
	if len(criteria) == 0 {
		return criteria
	}
	out := append([]taskstore.AcceptanceCriterion(nil), criteria...)
	for i := range out {
		out[i].Status = status
	}
	return out
}

func (o *Orchestrator) releaseGraphDependents(ctx context.Context, completedTaskID string) (int, error) {
	tasks, err := o.tasks.List()
	if err != nil {
		return 0, err
	}
	released := 0
	for _, candidate := range tasks {
		if taskTerminal(candidate.Status) || !containsString(candidate.DependsOn, completedTaskID) {
			continue
		}
		unresolved, err := o.unresolvedDependencies(candidate)
		if err != nil {
			return released, err
		}
		candidate.BlockedBy = unresolved
		if len(unresolved) > 0 {
			if err := o.tasks.Save(candidate); err != nil {
				return released, err
			}
			continue
		}
		if candidate.Status == taskstore.StatusBlocked {
			candidate.Status = taskstore.StatusQueued
			candidate.AssignedTo = "OrchestratorAgent"
			candidate.Result = appendResultLine(candidate.Result, "dependencies satisfied; queued graph phase")
			if err := o.tasks.Save(candidate); err != nil {
				return released, err
			}
			_ = o.events.Append(ctx, eventlog.Event{ID: id.New("evt"), Type: "task.graph.released", Actor: "OrchestratorAgent", TaskID: candidate.ID, ParentID: candidate.ParentID, Payload: eventlog.Payload(map[string]any{
				"completed_dependency": completedTaskID,
				"depends_on":           candidate.DependsOn,
			})})
			released++
		}
	}
	return released, nil
}

func (o *Orchestrator) completeGraphParentIfReady(ctx context.Context, parentID string) (bool, error) {
	parent, err := o.tasks.Load(parentID)
	if err != nil {
		return false, err
	}
	if parent.Status == taskstore.StatusDone || parent.GraphPhase != "root" {
		return false, nil
	}
	tasks, err := o.tasks.List()
	if err != nil {
		return false, err
	}
	childCount := 0
	for _, child := range tasks {
		if child.ParentID != parentID {
			continue
		}
		childCount++
		if child.Status != taskstore.StatusDone {
			return false, nil
		}
	}
	if childCount == 0 {
		return false, nil
	}
	parent.Status = taskstore.StatusDone
	parent.AssignedTo = "OrchestratorAgent"
	parent.AcceptanceCriteria = markAcceptanceCriteria(parent.AcceptanceCriteria, "accepted")
	parent.Result = appendResultLine(parent.Result, "all child graph phases accepted by human")
	if err := o.tasks.Save(parent); err != nil {
		return false, err
	}
	_ = o.events.Append(ctx, eventlog.Event{ID: id.New("evt"), Type: "task.graph.completed", Actor: "OrchestratorAgent", TaskID: parentID, Payload: eventlog.Payload(map[string]any{"children": childCount})})
	return true, nil
}

func containsString(values []string, needle string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}

func restartPlanForDiff(diff string) string {
	components := touchedRestartComponents(diffFileList(diff))
	if len(components) == 0 {
		return ""
	}
	return "restart " + strings.Join(components, ", ") + " after merge before final acceptance"
}

func restartPlanFromResult(result string) string {
	for _, line := range strings.Split(result, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(strings.ToLower(line), "restart plan:") {
			return strings.TrimSpace(line[len("Restart plan:"):])
		}
	}
	return ""
}

func touchedRestartComponents(files []string) []string {
	seen := make(map[string]bool)
	var components []string
	add := func(component string) {
		if component == "" || seen[component] {
			return
		}
		seen[component] = true
		components = append(components, component)
	}
	for _, file := range files {
		switch {
		case strings.HasPrefix(file, "cmd/homelabd/"),
			strings.HasPrefix(file, "pkg/agent/"),
			strings.HasPrefix(file, "pkg/chat/"),
			strings.HasPrefix(file, "pkg/config/"),
			strings.HasPrefix(file, "pkg/control/"),
			strings.HasPrefix(file, "pkg/eventlog/"),
			strings.HasPrefix(file, "pkg/externalagent/"),
			strings.HasPrefix(file, "pkg/llm/"),
			strings.HasPrefix(file, "pkg/memory/"),
			strings.HasPrefix(file, "pkg/task/"),
			strings.HasPrefix(file, "pkg/tool/"),
			strings.HasPrefix(file, "pkg/tools/"),
			strings.HasPrefix(file, "pkg/workspace/"):
			add("homelabd")
		case strings.HasPrefix(file, "cmd/healthd/"), strings.HasPrefix(file, "pkg/healthd/"):
			add("healthd")
		case strings.HasPrefix(file, "cmd/supervisord/"), strings.HasPrefix(file, "pkg/supervisor/"):
			add("supervisord")
		case strings.HasPrefix(file, "web/"):
			add("dashboard")
		}
	}
	return components
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
	var exactIDMatches []taskstore.Task
	var exactTextMatches []taskstore.Task
	var fuzzyMatches []taskstore.Task
	for _, t := range tasks {
		shortID := normalizeTaskSelector(taskShortID(t.ID))
		fullID := normalizeTaskSelector(t.ID)
		title := normalizeTaskSelector(t.Title)
		goal := normalizeTaskSelector(t.Goal)
		if fullID == needle || shortID == needle {
			exactIDMatches = append(exactIDMatches, t)
			continue
		}
		if title == needle || goal == needle {
			exactTextMatches = append(exactTextMatches, t)
			continue
		}
		if needle != "" && (strings.Contains(title, needle) || strings.Contains(goal, needle)) {
			fuzzyMatches = append(fuzzyMatches, t)
		}
	}
	if taskID, ok, err := resolveTaskMatches(selector, exactIDMatches); ok || err != nil {
		return taskID, err
	}
	if taskID, ok, err := resolveTaskMatches(selector, exactTextMatches); ok || err != nil {
		return taskID, err
	}
	if taskID, ok, err := resolveTaskMatches(selector, fuzzyMatches); ok || err != nil {
		return taskID, err
	}
	return "", fmt.Errorf("no task matches %q", selector)
}

func resolveTaskMatches(selector string, matches []taskstore.Task) (string, bool, error) {
	if len(matches) == 1 {
		return matches[0].ID, true, nil
	}
	if len(matches) > 1 {
		sort.Slice(matches, func(i, j int) bool {
			if taskTerminal(matches[i].Status) != taskTerminal(matches[j].Status) {
				return !taskTerminal(matches[i].Status)
			}
			return matches[i].UpdatedAt.After(matches[j].UpdatedAt)
		})
		if !taskTerminal(matches[0].Status) && (len(matches) == 1 || taskTerminal(matches[1].Status)) {
			return matches[0].ID, true, nil
		}
		var ids []string
		for _, t := range matches {
			ids = append(ids, taskShortID(t.ID)+"="+friendlyTaskTitle(t))
		}
		sort.Strings(ids)
		return "", true, fmt.Errorf("task selector %q is ambiguous: %s", selector, strings.Join(ids, ", "))
	}
	return "", false, nil
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
		if strings.Contains(err.Error(), "already running") {
			shortID := taskShortID(taskID)
			return fmt.Sprintf("Task %s is already running.\nNext:\n%s", shortID, commandBlock(
				"status",
				"show "+shortID,
			)), nil
		}
		return "", err
	}
	shortID := taskShortID(taskID)
	return fmt.Sprintf("Started %s on %s.\nThis is running in the background; chat is not blocked.\nNext after it finishes:\n%s", backend, shortID, commandBlock(
		"status",
		"show "+shortID,
		"review "+shortID,
	)), nil
}

type delegationRun struct {
	ID          string
	TaskID      string
	Backend     string
	Workspace   string
	Instruction string
}

type externalDelegateResult struct {
	ID         string    `json:"id"`
	Backend    string    `json:"backend"`
	TaskID     string    `json:"task_id"`
	Workspace  string    `json:"workspace"`
	Command    []string  `json:"command"`
	Output     string    `json:"output"`
	Error      string    `json:"error"`
	Duration   int64     `json:"duration"`
	StartedAt  time.Time `json:"started_at"`
	FinishedAt time.Time `json:"finished_at"`
}

type ExternalRunArtifact struct {
	ID         string    `json:"id"`
	Kind       string    `json:"kind"`
	Path       string    `json:"path,omitempty"`
	TaskID     string    `json:"task_id"`
	Backend    string    `json:"backend"`
	Workspace  string    `json:"workspace"`
	Status     string    `json:"status"`
	Command    []string  `json:"command"`
	Output     string    `json:"output"`
	Error      string    `json:"error"`
	Duration   int64     `json:"duration"`
	StartedAt  time.Time `json:"started_at"`
	FinishedAt time.Time `json:"finished_at"`
	Time       time.Time `json:"time"`
}

func (o *Orchestrator) runDelegation(ctx context.Context, runID, taskID, backend, workspace, instruction string) {
	raw, err := o.runTool(ctx, "OrchestratorAgent", "agent.delegate", map[string]any{
		"backend":     backend,
		"run_id":      runID,
		"task_id":     taskID,
		"workspace":   workspace,
		"instruction": instruction,
	}, taskID)
	var out externalDelegateResult
	_ = json.Unmarshal(raw, &out)
	if out.ID == "" {
		out.ID = runID
	}
	t, loadErr := o.tasks.Load(taskID)
	if loadErr != nil {
		_ = o.writeExternalRunArtifact(runID, taskID, backend, workspace, "failed", out, loadErr.Error())
		_ = o.events.Append(ctx, eventlog.Event{ID: id.New("evt"), Type: "agent.delegate.failed", Actor: backend, TaskID: taskID, Payload: eventlog.Payload(map[string]any{"id": runID, "error": loadErr.Error()})})
		return
	}
	if shouldIgnoreStaleDelegationResult(t) {
		_ = o.writeExternalRunArtifact(runID, taskID, backend, workspace, "ignored", out, "task state advanced while worker was running")
		_ = o.events.Append(ctx, eventlog.Event{ID: id.New("evt"), Type: "agent.delegate.ignored", Actor: backend, TaskID: taskID, Payload: eventlog.Payload(map[string]any{"id": runID, "status": t.Status, "reason": "task state advanced while worker was running"})})
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
		_ = o.writeExternalRunArtifact(runID, taskID, backend, workspace, "failed", out, err.Error())
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
	_ = o.writeExternalRunArtifact(runID, taskID, backend, workspace, "completed", out, "")
	_ = o.events.Append(ctx, eventlog.Event{ID: id.New("evt"), Type: "agent.delegate.completed", Actor: backend, TaskID: taskID, Payload: eventlog.Payload(map[string]any{"id": runID, "backend": backend, "result": out})})
	o.clearTaskActive(taskID)
	review, reviewErr := o.reviewTask(context.Background(), taskID)
	payload := map[string]any{"id": runID, "backend": backend, "review": review}
	if reviewErr != nil {
		payload["error"] = reviewErr.Error()
		_ = o.events.Append(ctx, eventlog.Event{ID: id.New("evt"), Type: "task.review.failed", Actor: "ReviewerAgent", TaskID: taskID, Payload: eventlog.Payload(payload)})
		return
	}
	_ = o.events.Append(ctx, eventlog.Event{ID: id.New("evt"), Type: "task.review.completed", Actor: "ReviewerAgent", TaskID: taskID, Payload: eventlog.Payload(payload)})
}

func shouldIgnoreStaleDelegationResult(t taskstore.Task) bool {
	switch t.Status {
	case taskstore.StatusAwaitingApproval, taskstore.StatusAwaitingVerification, taskstore.StatusDone, taskstore.StatusCancelled:
		return true
	default:
		return false
	}
}

func (o *Orchestrator) startOneShotWork(ctx context.Context, goal string) (string, error) {
	created, err := o.createTaskRecord(ctx, goal)
	if err != nil {
		return "", err
	}
	if created.Task.ID == "" {
		return "usage: new <goal>", nil
	}
	taskID := created.firstChildID()
	if err := o.startDelegationForTask(context.Background(), taskID, "codex", strings.Join([]string{
		goal,
		"",
		"Work only in this task worktree. Inspect first, make the smallest practical patch, update relevant docs/help text when behavior or commands change, run relevant formatting and tests, and leave a concise summary with how to use the change.",
	}, "\n")); err != nil {
		shortID := taskShortID(created.Task.ID)
		return fmt.Sprintf("Created queued task %s, but could not start codex: %v\nNext:\n%s", shortID, err, commandBlock(
			"run "+shortID,
			"delegate "+shortID+" to codex",
		)), nil
	}
	shortID := taskShortID(created.Task.ID)
	return fmt.Sprintf("Created queued task %s and started codex. It is cooking in the background.\nNext:\n%s", shortID, commandBlock(
		"status",
		"show "+shortID,
	)), nil
}

func (o *Orchestrator) startDelegationForTask(ctx context.Context, taskID, backend, instruction string) error {
	if !o.markTaskActive(taskID, backend) {
		return fmt.Errorf("task %s is already running", taskID)
	}
	run, err := o.prepareDelegationForTask(ctx, taskID, backend, instruction)
	if err != nil {
		o.clearTaskActive(taskID)
		return err
	}
	runCtx, cancel := context.WithCancel(context.Background())
	o.setTaskActiveRun(run.TaskID, run.ID, cancel)
	go func() {
		defer o.clearTaskActive(run.TaskID)
		o.runDelegation(runCtx, run.ID, run.TaskID, run.Backend, run.Workspace, run.Instruction)
	}()
	return nil
}

func (o *Orchestrator) prepareDelegationForTask(ctx context.Context, taskID, backend, instruction string) (delegationRun, error) {
	t, err := o.tasks.Load(taskID)
	if err != nil {
		return delegationRun{}, err
	}
	previousStatus := t.Status
	previousAssignedTo := t.AssignedTo
	previousResult := t.Result
	if t.Workspace == "" {
		return delegationRun{}, fmt.Errorf("task %s has no workspace", taskID)
	}
	if len(t.DependsOn) > 0 {
		unresolved, err := o.unresolvedDependencies(t)
		if err != nil {
			return delegationRun{}, err
		}
		if len(unresolved) > 0 {
			t.Status = taskstore.StatusBlocked
			t.BlockedBy = unresolved
			t.AssignedTo = "OrchestratorAgent"
			t.Result = "blocked by graph dependencies: " + strings.Join(shortTaskIDs(unresolved), ", ")
			_ = o.tasks.Save(t)
			return delegationRun{}, fmt.Errorf("task %s is blocked by graph dependencies: %s", taskShortID(taskID), strings.Join(shortTaskIDs(unresolved), ", "))
		}
	}
	if o.ensureTaskPlan(ctx, &t) {
		if err := o.tasks.Save(t); err != nil {
			return delegationRun{}, err
		}
	}
	if strings.TrimSpace(instruction) == "" {
		instruction = defaultDelegationInstruction(t)
	}
	if err := o.stalePendingTaskApprovals(ctx, taskID, "superseded by a new worker run"); err != nil {
		return delegationRun{}, err
	}
	conflictContext := ""
	if shouldPrepareConflictWorkspace(previousStatus, previousResult) {
		prepared, err := o.prepareConflictResolutionWorkspace(ctx, t)
		if err != nil {
			return delegationRun{}, err
		}
		conflictContext = strings.TrimSpace(prepared)
		if conflictContext != "" {
			_ = o.events.Append(ctx, eventlog.Event{ID: id.New("evt"), Type: "task.conflict_resolution.prepared", Actor: "OrchestratorAgent", TaskID: taskID, Payload: eventlog.Payload(map[string]any{
				"status":  previousStatus,
				"context": truncateForChat(conflictContext),
			})})
		}
	}
	instruction = contextualDelegationInstruction(t, instruction, previousStatus, previousAssignedTo, previousResult, conflictContext)
	t.Status = taskstore.StatusRunning
	t.AssignedTo = backend
	t.Result = runningDelegationResult(backend, previousStatus, previousResult, conflictContext)
	if err := o.tasks.Save(t); err != nil {
		return delegationRun{}, err
	}
	_ = o.events.Append(ctx, eventlog.Event{ID: id.New("evt"), Type: "task.assigned", Actor: "OrchestratorAgent", TaskID: taskID, Payload: eventlog.Payload(map[string]any{"agent": backend})})
	runID := id.New("delegate")
	_ = o.events.Append(ctx, eventlog.Event{ID: id.New("evt"), Type: "agent.delegate.started", Actor: "OrchestratorAgent", TaskID: taskID, Payload: eventlog.Payload(map[string]any{"id": runID, "backend": backend, "workspace": t.Workspace})})
	return delegationRun{ID: runID, TaskID: taskID, Backend: backend, Workspace: t.Workspace, Instruction: instruction}, nil
}

func shouldPrepareConflictWorkspace(status, result string) bool {
	if status == taskstore.StatusConflictResolution {
		return true
	}
	result = strings.ToLower(result)
	return strings.Contains(result, "premerge check failed") ||
		strings.Contains(result, "could not reconcile task branch") ||
		strings.Contains(result, "auto-rebase failed") ||
		strings.Contains(result, "merge conflict") ||
		strings.Contains(result, "must be rebased or conflict-resolved")
}

func (o *Orchestrator) prepareConflictResolutionWorkspace(ctx context.Context, t taskstore.Task) (string, error) {
	if !workspaceHasGit(t.Workspace) {
		return "", nil
	}
	statusOut, err := exec.CommandContext(ctx, "git", "-C", t.Workspace, "status", "--porcelain").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git status task workspace: %w: %s", err, strings.TrimSpace(string(statusOut)))
	}
	if strings.TrimSpace(string(statusOut)) != "" {
		return "Workspace already has uncommitted or conflicted changes; inspect and resolve this state before review.\nGit status:\n" + strings.TrimSpace(string(statusOut)), nil
	}
	headOut, err := exec.CommandContext(ctx, "git", "-C", o.cfg.Repo.Root, "rev-parse", "HEAD").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git rev-parse repo head: %w: %s", err, strings.TrimSpace(string(headOut)))
	}
	head := strings.TrimSpace(string(headOut))
	mergeOut, mergeErr := exec.CommandContext(ctx, "git", "-C", t.Workspace, "merge", "--no-edit", head).CombinedOutput()
	statusAfterOut, statusErr := exec.CommandContext(ctx, "git", "-C", t.Workspace, "status", "--porcelain").CombinedOutput()
	if statusErr != nil {
		return "", fmt.Errorf("git status task workspace after merge attempt: %w: %s", statusErr, strings.TrimSpace(string(statusAfterOut)))
	}
	if mergeErr != nil {
		return fmt.Sprintf("Started merge of current main %s into the task workspace and left conflicts for the worker to resolve.\nMerge output:\n%s\nGit status:\n%s", head, strings.TrimSpace(string(mergeOut)), strings.TrimSpace(string(statusAfterOut))), nil
	}
	return fmt.Sprintf("Merged current main %s into the task workspace before retry.\nMerge output:\n%s\nGit status:\n%s", head, strings.TrimSpace(string(mergeOut)), strings.TrimSpace(string(statusAfterOut))), nil
}

func contextualDelegationInstruction(t taskstore.Task, instruction, previousStatus, previousAssignedTo, previousResult, conflictContext string) string {
	var sections []string
	if strings.TrimSpace(instruction) != "" {
		sections = append(sections, strings.TrimSpace(instruction))
	}
	sections = append(sections, fmt.Sprintf("Task state before this worker run: status=%s assigned_to=%s workspace=%s.", previousStatus, firstNonEmptyString(previousAssignedTo, "unassigned"), t.Workspace))
	if strings.TrimSpace(previousResult) != "" {
		sections = append(sections, "Latest task result before this worker run:\n"+truncateForPrompt(strings.TrimSpace(previousResult)))
	}
	if strings.TrimSpace(conflictContext) != "" {
		sections = append(sections, "Conflict-resolution workspace context:\n"+truncateForPrompt(strings.TrimSpace(conflictContext)))
	}
	if previousStatus == taskstore.StatusConflictResolution || strings.TrimSpace(conflictContext) != "" {
		sections = append(sections, strings.Join([]string{
			"Conflict-resolution requirements:",
			"- Inspect `git status --short` first.",
			"- If conflict markers or unmerged paths are present, resolve them, stage the resolved files, and commit the merge/resolution in the task branch.",
			"- If the workspace is clean but the latest task result reports a main-branch conflict, merge current main into the task branch, resolve conflicts, and commit.",
			"- Do not reset the task branch to main unless the operator explicitly asked for a fresh restart.",
			"- Run relevant validation, then summarise the resolved conflict files, validation, and remaining risk.",
		}, "\n"))
	}
	return strings.Join(sections, "\n\n")
}

func runningDelegationResult(backend, previousStatus, previousResult, conflictContext string) string {
	var lines []string
	lines = append(lines, fmt.Sprintf("delegated to %s; external worker is running", backend))
	if strings.TrimSpace(previousStatus) != "" {
		lines = append(lines, "previous status: "+previousStatus)
	}
	if strings.TrimSpace(previousResult) != "" {
		lines = append(lines, "previous task result before this run:\n"+truncateForChat(strings.TrimSpace(previousResult)))
	}
	if strings.TrimSpace(conflictContext) != "" {
		lines = append(lines, "conflict retry context:\n"+truncateForChat(strings.TrimSpace(conflictContext)))
	}
	return strings.Join(lines, "\n")
}

func defaultDelegationInstruction(t taskstore.Task) string {
	return strings.Join([]string{
		"Work this task to completion if possible.",
		"Follow the reviewed task plan before executing; if the plan is wrong, explain the mismatch in the final summary.",
		"Inspect the task workspace before editing.",
		"Make a minimal patch that satisfies the task goal.",
		"If behavior, commands, UI, configuration, tools, or workflow changed, update relevant docs/help text in the same patch.",
		"Run relevant formatting and tests when available.",
		"For UI changes, run browser UAT from this task workspace with an isolated dev server, for example `nix develop -c bun run --cwd web uat:tasks` for task-page changes or `nix develop -c bun run --cwd web uat:site` for site-wide changes. If Chromium launch fails, run `nix develop -c bun run --cwd web browser:preflight` and report the browser infrastructure failure; do not stop or restart production dashboard, homelabd, healthd, or supervisord.",
		"For remote tasks, run validation on the remote worker in the selected remote workdir and report the exact commands, ports, and URLs used.",
		"Final summary must include: changed files, validation run, how to use the change, and docs updated or why no docs change was needed.",
		"Reviewed task plan: " + formatTaskPlanForPrompt(t.Plan),
		"Task graph context: " + formatGraphContextForPrompt(t),
		"Task goal: " + t.Goal,
	}, " ")
}

func formatGraphContextForPrompt(t taskstore.Task) string {
	if t.GraphPhase == "" && t.ParentID == "" && len(t.DependsOn) == 0 && len(t.AcceptanceCriteria) == 0 {
		return "none"
	}
	var b strings.Builder
	if t.GraphPhase != "" {
		fmt.Fprintf(&b, "phase=%s. ", t.GraphPhase)
	}
	if t.ParentID != "" {
		fmt.Fprintf(&b, "parent=%s. ", t.ParentID)
	}
	if len(t.DependsOn) > 0 {
		fmt.Fprintf(&b, "depends_on=%s. ", strings.Join(t.DependsOn, ","))
	}
	if len(t.AcceptanceCriteria) > 0 {
		b.WriteString("acceptance_criteria=")
		for i, criterion := range t.AcceptanceCriteria {
			if i > 0 {
				b.WriteString("; ")
			}
			b.WriteString(criterion.Description)
		}
	}
	return strings.TrimSpace(b.String())
}

func formatTaskPlanForPrompt(plan *taskstore.TaskPlan) string {
	if plan == nil {
		return "none"
	}
	var b strings.Builder
	if strings.TrimSpace(plan.Summary) != "" {
		fmt.Fprintf(&b, "%s. ", strings.TrimSpace(plan.Summary))
	}
	for i, step := range plan.Steps {
		if strings.TrimSpace(step.Title) == "" {
			continue
		}
		fmt.Fprintf(&b, "%d) %s", i+1, strings.TrimSpace(step.Title))
		if strings.TrimSpace(step.Detail) != "" {
			fmt.Fprintf(&b, " - %s", strings.TrimSpace(step.Detail))
		}
		b.WriteString(". ")
	}
	if strings.TrimSpace(plan.Review) != "" {
		fmt.Fprintf(&b, "Review: %s.", strings.TrimSpace(plan.Review))
	}
	return strings.TrimSpace(b.String())
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
		Matches []struct {
			Path    string `json:"path"`
			Line    int    `json:"line"`
			Text    string `json:"text"`
			Context []struct {
				Line  int    `json:"line"`
				Text  string `json:"text"`
				Match bool   `json:"match"`
			} `json:"context"`
		} `json:"matches"`
	}
	_ = json.Unmarshal(raw, &out)
	if len(out.Matches) == 0 {
		return "no matches", nil
	}
	var b strings.Builder
	for _, m := range out.Matches {
		if len(m.Context) == 0 {
			fmt.Fprintf(&b, "%s:%d: %s\n", m.Path, m.Line, m.Text)
			continue
		}
		fmt.Fprintf(&b, "%s:%d:\n", m.Path, m.Line)
		for _, line := range m.Context {
			marker := " "
			if line.Match {
				marker = ">"
			}
			fmt.Fprintf(&b, "%s %d: %s\n", marker, line.Line, line.Text)
		}
	}
	return strings.TrimSpace(b.String()), nil
}

func (o *Orchestrator) searchInternet(ctx context.Context, query, source string) (string, error) {
	correction := o.correctExternalSearchQuery(ctx, query)
	raw, err := o.runTool(ctx, "OrchestratorAgent", "internet.search", map[string]any{"query": correction.Query, "source": source, "max_results": 8}, "")
	if err != nil {
		return "", err
	}
	result := formatInternetSearchResult(raw)
	if correction.Note != "" {
		return correction.Note + "\n" + result, nil
	}
	return result, nil
}

func (o *Orchestrator) researchInternet(ctx context.Context, query, source, depth string) (string, error) {
	if source == "" || source == "web" {
		source = "all"
	}
	correction := o.correctExternalSearchQuery(ctx, query)
	raw, err := o.runTool(ctx, "OrchestratorAgent", "internet.research", map[string]any{"query": correction.Query, "source": source, "depth": depth}, "")
	if err != nil {
		return "", err
	}
	result := formatInternetResearchResult(raw)
	if correction.Note != "" {
		return correction.Note + "\n" + result, nil
	}
	return result, nil
}

type externalSearchCorrection struct {
	Query string
	Note  string
}

func (o *Orchestrator) correctExternalSearchQuery(ctx context.Context, query string) externalSearchCorrection {
	out := externalSearchCorrection{Query: query}
	if _, ok := o.registry.Get("text.correct"); !ok {
		return out
	}
	raw, err := o.runTool(ctx, "OrchestratorAgent", "text.correct", map[string]any{"text": query, "mode": "search_query", "max_variants": 3}, "")
	if err != nil {
		return out
	}
	var corrected struct {
		Corrected     string   `json:"corrected_text"`
		Changed       bool     `json:"changed"`
		SearchQueries []string `json:"search_queries"`
	}
	if err := json.Unmarshal(raw, &corrected); err != nil {
		return out
	}
	correctedText := strings.TrimSpace(corrected.Corrected)
	if !corrected.Changed || correctedText == "" || strings.EqualFold(correctedText, query) {
		return out
	}
	out.Query = correctedText
	var variants []string
	for _, variant := range corrected.SearchQueries {
		variant = strings.TrimSpace(variant)
		if variant == "" || strings.EqualFold(variant, query) || strings.EqualFold(variant, correctedText) {
			continue
		}
		variants = append(variants, variant)
		if len(variants) >= 2 {
			break
		}
	}
	out.Note = fmt.Sprintf("Corrected search query: %s -> %s", query, correctedText)
	if len(variants) > 0 {
		out.Note += "\nOther query variants: " + strings.Join(variants, "; ")
	}
	return out
}

func formatInternetResearchResult(raw json.RawMessage) string {
	var out struct {
		Query           string           `json:"query"`
		Source          string           `json:"source"`
		Depth           string           `json:"depth"`
		Method          string           `json:"method"`
		Provider        string           `json:"search_provider"`
		Plan            []string         `json:"plan"`
		Subqueries      []string         `json:"subqueries"`
		Sources         []map[string]any `json:"sources"`
		SearchErrors    []string         `json:"search_errors"`
		FollowUpQueries []string         `json:"follow_up_queries"`
		Notes           []string         `json:"notes"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return string(raw)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Research bundle (%s/%s): %s\n", firstNonEmptyString(out.Source, "all"), firstNonEmptyString(out.Depth, "standard"), firstNonEmptyString(out.Query, "query"))
	if out.Provider != "" {
		fmt.Fprintf(&b, "Search provider: %s\n", out.Provider)
	}
	if out.Method != "" {
		fmt.Fprintf(&b, "Method: %s\n", out.Method)
	}
	if len(out.Subqueries) > 0 {
		fmt.Fprintln(&b, "Fan-out queries:")
		for i, query := range out.Subqueries {
			fmt.Fprintf(&b, "%d. %s\n", i+1, query)
		}
	}
	if len(out.Sources) > 0 {
		fmt.Fprintln(&b, "Sources:")
		for i, source := range out.Sources {
			title := firstNonEmptyString(stringFromAny(source["title"]), stringFromAny(source["url"]))
			url := stringFromAny(source["url"])
			domain := stringFromAny(source["domain"])
			snippet := firstNonEmptyString(stringFromAny(source["snippet"]), stringFromAny(source["text"]))
			fmt.Fprintf(&b, "%d. %s", i+1, title)
			if domain != "" {
				fmt.Fprintf(&b, " (%s)", domain)
			}
			if url != "" {
				fmt.Fprintf(&b, " — %s", url)
			}
			if snippet != "" {
				fmt.Fprintf(&b, "\n   %s", truncateString(snippet, 260))
			}
			fmt.Fprintln(&b)
		}
	}
	if len(out.SearchErrors) > 0 {
		fmt.Fprintln(&b, "Search errors:")
		for _, err := range out.SearchErrors {
			fmt.Fprintf(&b, "- %s\n", err)
		}
	}
	if len(out.FollowUpQueries) > 0 {
		fmt.Fprintln(&b, "Useful follow-up queries:")
		for _, query := range out.FollowUpQueries {
			fmt.Fprintf(&b, "- %s\n", query)
		}
	}
	result := strings.TrimSpace(b.String())
	if result == "" {
		return string(raw)
	}
	return result
}

func truncateString(value string, max int) string {
	value = strings.TrimSpace(value)
	if len(value) <= max {
		return value
	}
	return strings.TrimSpace(value[:max]) + "..."
}

func formatInternetSearchResult(raw json.RawMessage) string {
	var out struct {
		Query       string           `json:"query"`
		Source      string           `json:"source"`
		Provider    string           `json:"provider"`
		Answer      string           `json:"answer"`
		Answers     []string         `json:"answers"`
		Abstract    string           `json:"abstract"`
		AbstractURL string           `json:"abstract_url"`
		Results     []map[string]any `json:"results"`
		Suggestions []string         `json:"suggestions"`
		Errors      []string         `json:"errors"`
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
	if out.Provider != "" && out.Provider != source {
		fmt.Fprintf(&b, "Provider: %s\n", out.Provider)
	}
	if out.Answer != "" {
		fmt.Fprintf(&b, "Answer: %s\n", out.Answer)
	}
	for _, answer := range out.Answers {
		if answer != "" && answer != out.Answer {
			fmt.Fprintf(&b, "Answer: %s\n", answer)
		}
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
	if len(out.Errors) > 0 {
		fmt.Fprintln(&b, "Search errors:")
		for _, err := range out.Errors {
			fmt.Fprintf(&b, "- %s\n", err)
		}
	}
	if len(out.Suggestions) > 0 {
		fmt.Fprintln(&b, "Suggestions:")
		for _, suggestion := range out.Suggestions {
			fmt.Fprintf(&b, "- %s\n", suggestion)
		}
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
	if remoteTask(t) {
		return "Remote task checks run on the remote agent. Review the agent result and recorded validation instead of running local repo checks.", nil
	}
	browserUAT := ""
	if workspaceHasGit(t.Workspace) {
		diffOut, err := o.taskBranchDiff(ctx, t.Workspace)
		if err != nil {
			return "", err
		}
		browserUAT = browserUATForDiff(diffOut)
	}
	return o.runProjectChecks(ctx, taskID, t.Workspace, "CoderAgent", browserUAT)
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
	if remoteTask(t) {
		return "Remote task diffs are not read from homelabd's repo. Inspect the remote agent result for changed files and validation.", nil
	}
	diff, err := o.TaskDiff(ctx, taskID)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(diff.RawDiff) == "" {
		return "no diff", nil
	}
	return diff.RawDiff, nil
}

func (o *Orchestrator) describeTaskDiff(ctx context.Context, selector string) (string, error) {
	diff, err := o.TaskDiff(ctx, selector)
	if err != nil {
		return "", err
	}
	shortID := taskShortID(diff.TaskID)
	base := diff.BaseLabel
	if base == "" {
		base = "main"
	}
	if strings.EqualFold(diff.BaseLabel, "remote agent") && strings.TrimSpace(diff.RawDiff) == "" {
		return "Remote task diffs are not read from homelabd's repo. Inspect the remote agent result for changed files and validation.", nil
	}
	if strings.TrimSpace(diff.RawDiff) == "" {
		return fmt.Sprintf("Task %s has no diff against %s.\nOpen the task in the dashboard to inspect the highlighted Changes panel, or run `homelabctl task diff %s`.", shortID, base, shortID), nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Diff for %s against %s: %d changed file(s), +%d/-%d.", shortID, base, diff.Summary.Files, diff.Summary.Additions, diff.Summary.Deletions)
	limit := len(diff.Files)
	if limit > 8 {
		limit = 8
	}
	for i := 0; i < limit; i++ {
		file := diff.Files[i]
		fmt.Fprintf(&b, "\n- %s (%s, +%d/-%d)", file.Path, file.Status, file.Additions, file.Deletions)
	}
	if len(diff.Files) > limit {
		fmt.Fprintf(&b, "\n- ... %d more", len(diff.Files)-limit)
	}
	fmt.Fprintf(&b, "\nOpen Tasks and use the Changes vs main panel for highlighted unified/split review, or run `homelabctl task diff %s` for the raw patch.", shortID)
	return b.String(), nil
}

func (o *Orchestrator) reviewTask(ctx context.Context, selector string) (string, error) {
	taskID, err := o.resolveTaskID(selector)
	if err != nil {
		return "", err
	}
	shortID := taskShortID(taskID)
	if !o.markTaskActive(taskID, "ReviewerAgent") {
		return fmt.Sprintf("ReviewerAgent: task %s already has an active worker or review. No checks run and no state changed.\nNext: `status` or `show %s`.", shortID, shortID), nil
	}
	defer o.clearTaskActive(taskID)
	t, err := o.tasks.Load(taskID)
	if err != nil {
		return "", err
	}
	if remoteTask(t) {
		return o.reviewRemoteTask(ctx, t)
	}
	if taskBlockedByReviewChecks(t) {
		previousResult := t.Result
		t.Status = taskstore.StatusReadyForReview
		t.AssignedTo = "OrchestratorAgent"
		if err := o.tasks.Save(t); err != nil {
			return "", err
		}
		_ = o.events.Append(ctx, eventlog.Event{ID: id.New("evt"), Type: "task.review.requeued", Actor: "ReviewerAgent", TaskID: taskID, Payload: eventlog.Payload(map[string]any{"previous_result": previousResult})})
	}
	if t.Status != taskstore.StatusReadyForReview {
		return reviewNotReadyReply(t), nil
	}
	diffOut := ""
	if workspaceHasGit(t.Workspace) {
		if _, err := commitReviewWorkspaceChanges(ctx, t.Workspace, taskID); err != nil {
			if latest, ok, reply, currentErr := o.currentReviewTask(taskID); currentErr != nil {
				return "", currentErr
			} else if !ok {
				return reply, nil
			} else {
				t = latest
			}
			t.Status = taskstore.StatusBlocked
			t.AssignedTo = "OrchestratorAgent"
			t.Result = "ReviewerAgent could not commit workspace changes before review: " + err.Error()
			_ = o.tasks.Save(t)
			_ = o.events.Append(ctx, eventlog.Event{ID: id.New("evt"), Type: "task.blocked", Actor: "ReviewerAgent", TaskID: taskID, Payload: eventlog.Payload(map[string]any{"reason": t.Result})})
			return fmt.Sprintf("ReviewerAgent:\nState: %s -> %s\nPre-review commit: fail\n%s\nNo approval created.\nNext: `delegate %s to codex fix the workspace git state`, `diff %s`, or `delete %s`.", taskstore.StatusReadyForReview, taskstore.StatusBlocked, err.Error(), shortID, shortID, shortID), nil
		}
		if mergeOut, err := o.reconcileTaskWorkspaceWithMain(ctx, t.Workspace); err != nil {
			if latest, ok, reply, currentErr := o.currentReviewTask(taskID); currentErr != nil {
				return "", currentErr
			} else if !ok {
				return reply, nil
			} else {
				t = latest
			}
			t.Status = taskstore.StatusConflictResolution
			t.AssignedTo = "OrchestratorAgent"
			t.Result = "ReviewerAgent could not reconcile task branch with current main before checks: " + err.Error()
			_ = o.tasks.Save(t)
			_ = o.events.Append(ctx, eventlog.Event{ID: id.New("evt"), Type: "task.conflict_resolution", Actor: "ReviewerAgent", TaskID: taskID, Payload: eventlog.Payload(map[string]any{"reason": t.Result, "from_status": taskstore.StatusReadyForReview, "to_status": taskstore.StatusConflictResolution})})
			return fmt.Sprintf("ReviewerAgent:\nState: %s -> %s\nPre-review rebase: fail\n%s\nNo checks or approval created.\nNext: `delegate %s to codex resolve the main-branch conflict`, `diff %s`, or `delete %s`.", taskstore.StatusReadyForReview, taskstore.StatusConflictResolution, err.Error(), shortID, shortID, shortID), nil
		} else if strings.TrimSpace(mergeOut) != "" {
			_ = o.events.Append(ctx, eventlog.Event{ID: id.New("evt"), Type: "task.workspace.reconciled", Actor: "ReviewerAgent", TaskID: taskID, Payload: eventlog.Payload(map[string]any{"output": strings.TrimSpace(mergeOut)})})
		}
		diffOut, err = o.taskBranchDiff(ctx, t.Workspace)
		if err != nil {
			return "", err
		}
	} else {
		var diffErr error
		diffOut, diffErr = o.diffTask(ctx, taskID)
		if diffErr != nil {
			return "", diffErr
		}
	}
	if diffOut == "no diff" {
		if latest, ok, reply, currentErr := o.currentReviewTask(taskID); currentErr != nil {
			return "", currentErr
		} else if !ok {
			return reply, nil
		} else {
			t = latest
		}
		t.Status = taskstore.StatusBlocked
		t.AssignedTo = "OrchestratorAgent"
		t.Result = "ReviewerAgent found no diff to approve."
		_ = o.tasks.Save(t)
		_ = o.events.Append(ctx, eventlog.Event{ID: id.New("evt"), Type: "task.blocked", Actor: "ReviewerAgent", TaskID: taskID, Payload: eventlog.Payload(map[string]any{"reason": t.Result})})
		return fmt.Sprintf("ReviewerAgent: no diff to approve.\nTask %s is blocked because the worker produced no changes.\nNext: `delegate %s to codex finish the task`, `run %s`, or `delete %s`.", shortID, shortID, shortID, shortID), nil
	}
	browserUAT := browserUATForDiff(diffOut)
	testOut, testErr := o.runProjectChecks(ctx, taskID, t.Workspace, "ReviewerAgent", browserUAT)
	status := "pass"
	if testErr != nil {
		if latest, ok, reply, currentErr := o.currentReviewTask(taskID); currentErr != nil {
			return "", currentErr
		} else if !ok {
			return reply, nil
		} else {
			t = latest
		}
		status = "fail"
		t.Status = taskstore.StatusBlocked
		t.AssignedTo = "OrchestratorAgent"
		t.Result = "ReviewerAgent checks failed: " + testErr.Error()
		_ = o.tasks.Save(t)
		_ = o.events.Append(ctx, eventlog.Event{ID: id.New("evt"), Type: "task.blocked", Actor: "ReviewerAgent", TaskID: taskID, Payload: eventlog.Payload(map[string]any{"reason": t.Result})})
		return fmt.Sprintf("ReviewerAgent:\nChecks: %s\n%s\nDiff summary:\n%s\nNo approval created because checks failed.\nNext: `delegate %s to codex fix the failing tests`, `diff %s`, or `delete %s`.", status, strings.TrimSpace(testOut), summarizeDiffForChat(diffOut), shortID, shortID, shortID), nil
	}
	if _, ok := o.registry.Get("git.merge_check"); ok {
		if _, err := o.runTool(ctx, "ReviewerAgent", "git.merge_check", map[string]any{"branch": "homelabd/" + taskID, "target": o.cfg.Repo.Root}, taskID); err != nil {
			if latest, ok, reply, currentErr := o.currentReviewTask(taskID); currentErr != nil {
				return "", currentErr
			} else if !ok {
				return reply, nil
			} else {
				t = latest
			}
			t.Status = taskstore.StatusBlocked
			t.AssignedTo = "OrchestratorAgent"
			t.Result = "ReviewerAgent premerge check failed: " + err.Error()
			_ = o.tasks.Save(t)
			_ = o.events.Append(ctx, eventlog.Event{ID: id.New("evt"), Type: "task.blocked", Actor: "ReviewerAgent", TaskID: taskID, Payload: eventlog.Payload(map[string]any{"reason": t.Result, "from_status": taskstore.StatusReadyForReview, "to_status": taskstore.StatusBlocked})})
			return fmt.Sprintf("ReviewerAgent:\nState: %s -> %s\nChecks: %s\n%s\nPremerge: fail\n%s\nNo approval created and no worker was restarted automatically.\nNext: `delegate %s to codex rebase and resolve merge conflicts`, `diff %s`, or `delete %s`.", taskstore.StatusReadyForReview, taskstore.StatusBlocked, status, strings.TrimSpace(testOut), err.Error(), shortID, shortID, shortID), nil
		}
	}
	restartPlan := restartPlanForDiff(diffOut)
	approvalID := id.New("approval")
	args := eventlog.Payload(map[string]any{"branch": "homelabd/" + taskID, "target": o.cfg.Repo.Root, "workspace": t.Workspace, "message": "Apply " + taskID})
	req := approvalstore.Request{ID: approvalID, TaskID: taskID, Tool: "git.merge_approved", Args: args, Reason: "merge reviewed task branch into repo root", Status: approvalstore.StatusPending}
	if latest, ok, reply, currentErr := o.currentReviewTask(taskID); currentErr != nil {
		return "", currentErr
	} else if !ok {
		return reply, nil
	} else {
		t = latest
	}
	if err := o.stalePendingTaskApprovals(ctx, taskID, "superseded by a new review approval"); err != nil {
		return "", err
	}
	if err := o.approvals.Save(req); err != nil {
		return "", err
	}
	t.Status = taskstore.StatusAwaitingApproval
	t.AssignedTo = "OrchestratorAgent"
	t.Result = "ReviewerAgent test status: " + status
	if restartPlan != "" {
		t.Result += "\nRestart plan: " + restartPlan
	}
	_ = o.tasks.Save(t)
	_ = o.events.Append(ctx, eventlog.Event{ID: id.New("evt"), Type: "approval.requested", Actor: "ReviewerAgent", TaskID: taskID, Payload: eventlog.Payload(req)})
	restartLine := "Restart impact: no supervised component restart detected."
	if restartPlan != "" {
		restartLine = "Restart impact: " + restartPlan
	}
	return fmt.Sprintf("ReviewerAgent:\nChecks: %s\n%s\nDiff summary:\n%s\n%s\nMerge approval requested: %s\nApprove merge with `approve %s`.\nAfter merge, verify the running app and use `accept %s` or `reopen %s <reason>`.", status, strings.TrimSpace(testOut), summarizeDiffForChat(diffOut), restartLine, approvalID, approvalID, shortID, shortID), nil
}

func reviewNotReadyReply(t taskstore.Task) string {
	shortID := taskShortID(t.ID)
	switch t.Status {
	case taskstore.StatusRunning:
		return fmt.Sprintf("ReviewerAgent: task %s is still running on %s. No checks run and no state changed.\nNext: `status` or `show %s`.", shortID, firstNonEmptyString(t.AssignedTo, "a worker"), shortID)
	case taskstore.StatusQueued:
		return fmt.Sprintf("ReviewerAgent: task %s is still queued. No checks run and no state changed.\nNext: `run %s`, `delegate %s to codex`, or `show %s`.", shortID, shortID, shortID, shortID)
	case taskstore.StatusAwaitingApproval:
		return fmt.Sprintf("ReviewerAgent: task %s has already passed review and is awaiting approval. No checks run and no state changed.\nNext: `approvals` or `show %s`.", shortID, shortID)
	case taskstore.StatusAwaitingVerification:
		return fmt.Sprintf("ReviewerAgent: task %s has already merged and is awaiting verification. No checks run and no state changed.\nNext: `accept %s` or `reopen %s <reason>`.", shortID, shortID, shortID)
	case taskstore.StatusBlocked, taskstore.StatusFailed, taskstore.StatusConflictResolution:
		return fmt.Sprintf("ReviewerAgent: task %s is %s, not ready for review. No checks run and no state changed.\nNext: `retry %s codex <instruction>`, `diff %s`, or `delete %s`.", shortID, t.Status, shortID, shortID, shortID)
	case taskstore.StatusDone, taskstore.StatusCancelled:
		return fmt.Sprintf("ReviewerAgent: task %s is %s. No checks run and no state changed.", shortID, t.Status)
	default:
		return fmt.Sprintf("ReviewerAgent: task %s is %s, not %s. No checks run and no state changed.\nNext: `show %s`.", shortID, firstNonEmptyString(t.Status, "unknown"), taskstore.StatusReadyForReview, shortID)
	}
}

func taskBlockedByReviewChecks(t taskstore.Task) bool {
	return t.Status == taskstore.StatusBlocked && strings.HasPrefix(strings.TrimSpace(t.Result), "ReviewerAgent checks failed:")
}

func (o *Orchestrator) currentReviewTask(taskID string) (taskstore.Task, bool, string, error) {
	t, err := o.tasks.Load(taskID)
	if err != nil {
		return taskstore.Task{}, false, "", err
	}
	if t.Status != taskstore.StatusReadyForReview {
		shortID := taskShortID(taskID)
		return t, false, fmt.Sprintf("ReviewerAgent: review result for %s was ignored because the task changed to %s while checks were running. No task state changed.\nNext: `status` or `show %s`.", shortID, firstNonEmptyString(t.Status, "unknown"), shortID), nil
	}
	return t, true, "", nil
}

func (o *Orchestrator) reviewRemoteTask(ctx context.Context, t taskstore.Task) (string, error) {
	shortID := taskShortID(t.ID)
	if t.Status == taskstore.StatusAwaitingVerification {
		return fmt.Sprintf("Remote task %s is already awaiting verification.\nVerify the result on %s in %s, then use `accept %s` or `reopen %s <reason>`.", shortID, t.Target.Machine, t.Target.Workdir, shortID, shortID), nil
	}
	if t.Status != taskstore.StatusReadyForReview {
		return fmt.Sprintf("Remote task %s is %s. Wait for the agent to finish before review.", shortID, t.Status), nil
	}
	t.Status = taskstore.StatusAwaitingVerification
	t.AssignedTo = "OrchestratorAgent"
	t.Result = appendResultLine(t.Result, "ReviewerAgent acknowledged remote result; no local git merge was attempted.")
	if err := o.tasks.Save(t); err != nil {
		return "", err
	}
	_ = o.events.Append(ctx, eventlog.Event{ID: id.New("evt"), Type: "remote_agent.review.acknowledged", Actor: "ReviewerAgent", TaskID: t.ID, Payload: eventlog.Payload(map[string]any{
		"agent_id": t.Target.AgentID,
		"machine":  t.Target.Machine,
		"workdir":  t.Target.Workdir,
	})})
	return fmt.Sprintf("ReviewerAgent:\nRemote result acknowledged for %s.\nNo local workspace, main-branch comparison, or merge approval was attempted.\nExecution context: %s on %s in %s.\nNext: verify that remote checkout, then use `accept %s` or `reopen %s <reason>`.", shortID, t.Target.AgentID, t.Target.Machine, t.Target.Workdir, shortID, shortID), nil
}

func (o *Orchestrator) reconcileTaskWorkspaceWithMain(ctx context.Context, workspace string) (string, error) {
	headOut, err := exec.CommandContext(ctx, "git", "-C", o.cfg.Repo.Root, "rev-parse", "HEAD").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git rev-parse repo head: %w: %s", err, strings.TrimSpace(string(headOut)))
	}
	statusOut, err := exec.CommandContext(ctx, "git", "-C", workspace, "status", "--porcelain").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git status task workspace: %w: %s", err, strings.TrimSpace(string(statusOut)))
	}
	if strings.TrimSpace(string(statusOut)) != "" {
		return "", fmt.Errorf("task workspace has uncommitted changes after pre-review commit: %s", strings.TrimSpace(string(statusOut)))
	}
	head := strings.TrimSpace(string(headOut))
	mergeOut, err := exec.CommandContext(ctx, "git", "-C", workspace, "merge", "--no-edit", head).CombinedOutput()
	if err != nil {
		_ = exec.CommandContext(context.Background(), "git", "-C", workspace, "merge", "--abort").Run()
		return "", fmt.Errorf("git merge current main into task workspace: %w: %s", err, strings.TrimSpace(string(mergeOut)))
	}
	return string(mergeOut), nil
}

func (o *Orchestrator) runProjectChecks(ctx context.Context, taskID, workspace, actor string, browserUAT string) (string, error) {
	var outputs []string
	var failedOutputs []string
	var firstErr error
	if exists(filepath.Join(workspace, "go.mod")) {
		out, err := o.runCheckTool(ctx, actor, "go.test", workspace, taskID)
		output := "go.test:\n" + strings.TrimSpace(out)
		outputs = append(outputs, output)
		firstErr = recordProjectCheckFailure("go.test", output, err, firstErr, &failedOutputs)
	}
	webDir := filepath.Join(workspace, "web")
	if exists(filepath.Join(webDir, "package.json")) {
		out, err := o.runCheckTool(ctx, actor, "bun.check", webDir, taskID)
		output := "bun.check:\n" + strings.TrimSpace(out)
		outputs = append(outputs, output)
		firstErr = recordProjectCheckFailure("bun.check", output, err, firstErr, &failedOutputs)
		out, err = o.runCheckTool(ctx, actor, "bun.build", webDir, taskID)
		output = "bun.build:\n" + strings.TrimSpace(out)
		outputs = append(outputs, output)
		firstErr = recordProjectCheckFailure("bun.build", output, err, firstErr, &failedOutputs)
		out, err = o.runCheckTool(ctx, actor, "bun.test", webDir, taskID)
		output = "bun.test:\n" + strings.TrimSpace(out)
		outputs = append(outputs, output)
		firstErr = recordProjectCheckFailure("bun.test", output, err, firstErr, &failedOutputs)
		switch browserUAT {
		case "site":
			out, err = o.runCheckTool(ctx, actor, "bun.uat.site", webDir, taskID)
			output = "bun.uat.site:\n" + strings.TrimSpace(out)
			outputs = append(outputs, output)
			firstErr = recordProjectCheckFailure("bun.uat.site", output, err, firstErr, &failedOutputs)
		case "tasks":
			out, err = o.runCheckTool(ctx, actor, "bun.uat.tasks", webDir, taskID)
			output = "bun.uat.tasks:\n" + strings.TrimSpace(out)
			outputs = append(outputs, output)
			firstErr = recordProjectCheckFailure("bun.uat.tasks", output, err, firstErr, &failedOutputs)
		}
	}
	if len(outputs) == 0 {
		return "no configured checks found", nil
	}
	if len(failedOutputs) > 0 {
		return truncateForChat(strings.Join(failedOutputs, "\n\n")), firstErr
	}
	return truncateForChat(strings.Join(outputs, "\n\n")), firstErr
}

func recordProjectCheckFailure(name, output string, err error, firstErr error, failedOutputs *[]string) error {
	if err == nil {
		return firstErr
	}
	*failedOutputs = append(*failedOutputs, truncateFailureForChat(output))
	if firstErr == nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	return firstErr
}

func truncateFailureForChat(s string) string {
	const max = 12000
	if len(s) <= max {
		return s
	}
	const head = 3000
	marker := fmt.Sprintf("\n...[truncated %d bytes before failing tail]\n", len(s)-max)
	tail := max - head - len(marker)
	if tail < 0 {
		tail = 0
	}
	return s[:head] + marker + s[len(s)-tail:]
}

func browserUATForDiff(diff string) string {
	if diffRequiresSiteUAT(diff) {
		return "site"
	}
	if diffRequiresTaskPageUAT(diff) {
		return "tasks"
	}
	return ""
}

func diffRequiresTaskPageUAT(diff string) bool {
	for _, path := range diffFileList(diff) {
		if path == "web/dashboard/e2e/chat-tasks-mobile.spec.ts" {
			return true
		}
		if strings.HasPrefix(path, "web/dashboard/src/routes/tasks/") ||
			strings.HasPrefix(path, "web/dashboard/scripts/tasks-page-uat") {
			return true
		}
	}
	return false
}

func diffRequiresSiteUAT(diff string) bool {
	for _, path := range diffFileList(diff) {
		switch path {
		case "web/package.json",
			"web/dashboard/package.json",
			"web/dashboard/playwright.config.ts":
			return true
		}
		if strings.HasPrefix(path, "web/shared/src/") ||
			strings.HasPrefix(path, "web/dashboard/src/lib/") ||
			strings.HasPrefix(path, "web/dashboard/src/app.") ||
			strings.HasPrefix(path, "web/dashboard/e2e/") ||
			strings.HasPrefix(path, "web/dashboard/scripts/browser-preflight") {
			return true
		}
		if strings.HasPrefix(path, "web/dashboard/src/routes/") &&
			!strings.HasPrefix(path, "web/dashboard/src/routes/tasks/") {
			return true
		}
	}
	return false
}

func workspaceHasGit(workspace string) bool {
	if strings.TrimSpace(workspace) == "" {
		return false
	}
	_, err := os.Stat(filepath.Join(workspace, ".git"))
	return err == nil
}

func commitReviewWorkspaceChanges(ctx context.Context, workspace, taskID string) (string, error) {
	statusOut, err := exec.CommandContext(ctx, "git", "-C", workspace, "status", "--porcelain").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git status workspace: %w: %s", err, strings.TrimSpace(string(statusOut)))
	}
	if strings.TrimSpace(string(statusOut)) == "" {
		return "workspace has no uncommitted changes", nil
	}
	addOut, err := exec.CommandContext(ctx, "git", "-C", workspace, "add", "-A", "--", ".").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git add workspace: %w: %s", err, strings.TrimSpace(string(addOut)))
	}
	resetOut, err := exec.CommandContext(ctx, "git", "-C", workspace, "reset", "--", ".codex", ".git-local", ".artifacts", "data").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git reset workspace metadata: %w: %s", err, strings.TrimSpace(string(resetOut)))
	}
	diffCmd := exec.CommandContext(ctx, "git", "-C", workspace, "diff", "--cached", "--quiet")
	if err := diffCmd.Run(); err == nil {
		return "workspace has no committable changes", nil
	} else if exitErr, ok := err.(*exec.ExitError); !ok || exitErr.ExitCode() != 1 {
		return "", fmt.Errorf("git diff cached workspace: %w", err)
	}
	commitOut, err := exec.CommandContext(ctx, "git", "-C", workspace, "commit", "-m", "Apply "+taskID+" for review").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git commit workspace: %w: %s", err, strings.TrimSpace(string(commitOut)))
	}
	return string(commitOut), nil
}

func (o *Orchestrator) taskBranchDiff(ctx context.Context, workspace string) (string, error) {
	baseOut, err := exec.CommandContext(ctx, "git", "-C", o.cfg.Repo.Root, "rev-parse", "HEAD").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git rev-parse repo head: %w: %s", err, strings.TrimSpace(string(baseOut)))
	}
	base := strings.TrimSpace(string(baseOut))
	diffOut, err := exec.CommandContext(ctx, "git", "-C", workspace, "diff", "--binary", base+"...HEAD", "--", ".").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git diff task branch: %w: %s", err, strings.TrimSpace(string(diffOut)))
	}
	if strings.TrimSpace(string(diffOut)) == "" {
		return "no diff", nil
	}
	return string(diffOut), nil
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
	return o.runSpecialistTask(ctx, selector, specialistTaskAgent{
		Name: "CoderAgent",
		Prompt: func(t taskstore.Task) string {
			return o.coderPrompt(t)
		},
		InitialUserMessage: "Work this task to completion if possible. Inspect the workspace, apply a minimal patch, run formatting/tests, then summarize the diff.",
	})
}

func (o *Orchestrator) runUXTask(ctx context.Context, selector, instruction string) (string, error) {
	initial := "Work this task as UXAgent. Research applicable UI, UX, and accessibility guidance, inspect the live UI code path, improve the experience, add regression tests, run automated validation, and report the browser-level UAT needed or completed."
	if strings.TrimSpace(instruction) != "" {
		initial += "\nHuman instruction:\n" + strings.TrimSpace(instruction)
	}
	return o.runSpecialistTask(ctx, selector, specialistTaskAgent{
		Name: "UXAgent",
		Prompt: func(t taskstore.Task) string {
			return o.uxPrompt(t)
		},
		InitialUserMessage: initial,
	})
}

type specialistTaskAgent struct {
	Name               string
	Prompt             func(taskstore.Task) string
	InitialUserMessage string
}

func (o *Orchestrator) runSpecialistTask(ctx context.Context, selector string, agent specialistTaskAgent) (string, error) {
	if agent.Name == "" {
		agent.Name = "CoderAgent"
	}
	taskID, err := o.resolveTaskID(selector)
	if err != nil {
		return "", err
	}
	if !o.markTaskActive(taskID, agent.Name) {
		return fmt.Sprintf("Task %s is already running. Use `show %s` or `status` for progress.", taskShortID(taskID), taskShortID(taskID)), nil
	}
	defer o.clearTaskActive(taskID)
	t, err := o.tasks.Load(taskID)
	if err != nil {
		return "", err
	}
	if t.Workspace == "" {
		return "", fmt.Errorf("task %s has no workspace", taskID)
	}
	if len(t.DependsOn) > 0 {
		unresolved, err := o.unresolvedDependencies(t)
		if err != nil {
			return "", err
		}
		if len(unresolved) > 0 {
			t.Status = taskstore.StatusBlocked
			t.BlockedBy = unresolved
			t.AssignedTo = "OrchestratorAgent"
			t.Result = "blocked by graph dependencies: " + strings.Join(shortTaskIDs(unresolved), ", ")
			_ = o.tasks.Save(t)
			return fmt.Sprintf("Task %s is blocked by graph dependencies: %s.", taskShortID(taskID), strings.Join(shortTaskIDs(unresolved), ", ")), nil
		}
	}
	if o.provider == nil || o.model == "" {
		return "", fmt.Errorf("no LLM provider configured")
	}
	o.ensureTaskPlan(ctx, &t)
	if err := o.stalePendingTaskApprovals(ctx, taskID, "superseded by a new worker run"); err != nil {
		return "", err
	}
	t.Status = taskstore.StatusRunning
	t.AssignedTo = agent.Name
	if err := o.tasks.Save(t); err != nil {
		return "", err
	}
	_ = o.events.Append(ctx, eventlog.Event{ID: id.New("evt"), Type: "task.assigned", Actor: "OrchestratorAgent", TaskID: taskID, Payload: eventlog.Payload(map[string]any{"agent": agent.Name})})

	maxToolCalls := o.cfg.Limits.MaxToolCallsPerTurn
	if maxToolCalls <= 0 {
		maxToolCalls = 12
	}
	maxToolCalls *= 2
	prompt := ""
	if agent.Prompt != nil {
		prompt = agent.Prompt(t)
	}
	if prompt == "" {
		prompt = o.coderPrompt(t)
	}
	initialUserMessage := strings.TrimSpace(agent.InitialUserMessage)
	if initialUserMessage == "" {
		initialUserMessage = "Work this task to completion if possible. Inspect the workspace, apply a minimal patch, run formatting/tests, then summarize the diff."
	}
	messages := []llm.Message{
		{Role: "system", Content: prompt},
		{Role: "user", Content: initialUserMessage},
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
		_ = o.events.Append(ctx, eventlog.Event{ID: id.New("evt"), Type: "agent.message", Actor: agent.Name, TaskID: taskID, Payload: eventlog.Payload(map[string]any{"provider": o.provider.Name(), "message": resp.Message.Content, "usage": resp.Usage})})
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
			result := o.executeProposedTool(ctx, agent.Name, call, taskID)
			turnResults = append(turnResults, result)
			allResults = append(allResults, result)
			if result.NeedsApproval {
				t.Status = taskstore.StatusAwaitingApproval
				t.Result = agent.Name + " is waiting for approval: " + result.ApprovalID
				_ = o.tasks.Save(t)
				_ = o.writeRunArtifact(taskID, "awaiting_approval", lastMessage, allResults, result.Reason)
				return formatApprovalStop(lastMessage, result), nil
			}
		}
		messages = append(messages, llm.Message{Role: "user", Content: "Tool results:\n" + truncateForPrompt(mustJSON(turnResults))})
	}

	t.Result = lastMessage
	t.Status = taskstore.StatusReadyForReview
	t.AssignedTo = "OrchestratorAgent"
	if err := o.tasks.Save(t); err != nil {
		return "", err
	}
	o.clearTaskActive(taskID)
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
		"- repo.search returns grep-like context by default; use path, context_lines, and max_results to keep searches focused.",
		"- Use internet.research for broad, current, multi-source questions before implementation choices.",
		"- Use text.correct before internet.search when a natural-language query appears misspelled or grammatically ambiguous; preserve exact code symbols.",
		"- Use internet.search when current external documentation, public web context, or academic papers are required.",
		"- Use internet.search with source academic for papers or source all when both web and scholarly context matter.",
		"- Use internet.fetch on promising result URLs before relying on details; prefer official, primary, or scholarly sources.",
		"- Every repo tool call that supports workspace must include this exact workspace: " + t.Workspace,
		"- Apply edits only with repo.write_patch using a unified diff against repository-relative paths.",
		"- Use shell.run_limited with dir set to the task workspace for allowlisted command arrays when a dedicated repo or test tool is too narrow.",
		"- Prefer small, targeted patches. Do not rewrite unrelated files.",
		"- If behavior, commands, UI, configuration, tools, or workflow changed, update relevant docs/help text in the same patch.",
		"- After editing Go code, run go.fmt, go.test, and repo.current_diff.",
		"- After editing web code, run bun.check, bun.build, bun.test, and a targeted isolated browser UAT when UI changed; use bun.uat.site for broad dashboard shell, navigation, theme, or multi-page changes.",
		"- If Chromium launch fails, run `nix develop -c bun run --cwd web browser:preflight` and report the browser infrastructure failure.",
		"- Browser UAT must run from the task workspace with an isolated dev server. Do not stop or restart production dashboard, homelabd, healthd, or supervisord.",
		"- Final done=true message must include: changed files, validation run, how to use the change, and docs updated or why no docs change was needed.",
		"- Do not call git.merge_approved, repo.apply_patch_to_main, service.*, shell.run_approved, or memory.commit_write.",
		"Available CoderAgent tools:",
		o.filteredToolCatalog(map[string]bool{
			"text.correct": true, "text.summarize": true,
			"internet.search": true, "internet.fetch": true, "internet.research": true,
			"repo.list": true, "repo.search": true, "repo.read": true, "repo.write_patch": true, "repo.current_diff": true,
			"git.status": true, "git.diff": true, "git.branch": true, "git.describe": true, "git.log": true, "git.show": true,
			"go.fmt": true, "go.test": true, "go.build": true, "bun.check": true, "bun.build": true, "bun.test": true, "bun.uat.tasks": true, "bun.uat.site": true,
			"shell.run_limited": true,
		}),
	}, "\n")
}

func (o *Orchestrator) uxPrompt(t taskstore.Task) string {
	return strings.Join([]string{
		"You are UXAgent. You inspect, research, design, implement, and verify UI/UX improvements only in the isolated task workspace.",
		"The Go runtime is the authority. You propose tool calls; tools execute only after policy validation.",
		"Respond with exactly one JSON object and no prose.",
		"Protocol:",
		`{"message":"short UX status","done":false,"tool_calls":[{"tool":"internet.research","args":{"query":"WCAG 2.2 focus target size WAI-ARIA APG keyboard UX heuristics","source":"web","depth":"standard"}}]}`,
		`{"message":"summary of completed UX work","done":true,"tool_calls":[]}`,
		"Task:",
		mustJSON(t),
		"UX standards to apply:",
		"- Start with the user's task, context, and visible state; avoid decorative changes that do not improve task success.",
		"- Use current external evidence before UX implementation choices: WCAG 2.2, WAI-ARIA APG patterns, platform or framework docs, design-system guidance, and reputable usability research such as NN/g heuristics.",
		"- Prioritise semantic HTML, accessible names, keyboard operation, visible focus, target size and spacing, colour contrast, predictable states, clear errors, responsive layouts, and content that matches user language.",
		"- Check loading, empty, error, disabled, selected, hover/focus, long-content, and mobile states when relevant.",
		"- Ensure text does not overlap, truncate badly, or depend on colour alone; UI changes must be usable at narrow and desktop viewports.",
		"Rules:",
		"- Use repo.list/repo.search/repo.read with the workspace argument before editing.",
		"- Use internet.research for broad or current UX/accessibility questions before implementation choices.",
		"- Use text.correct before internet.search when a natural-language query appears misspelled or grammatically ambiguous; preserve exact code symbols.",
		"- Use internet.search for precise current documentation lookups and internet.fetch before citing or relying on details.",
		"- Prefer official standards, primary framework docs, design-system docs, and reputable UX research. Mention source URLs in the final message.",
		"- Every repo tool call that supports workspace must include this exact workspace: " + t.Workspace,
		"- Apply edits only with repo.write_patch using a unified diff against repository-relative paths.",
		"- Add automated regression coverage for fixed UX bugs. Prefer testable view/state logic plus browser-level tests where interaction matters.",
		"- For changed UI, perform browser-level UAT against the page served from the isolated task workspace. Exercise the reported interaction, visible data, state changes, selected items, and mobile viewport behaviour when relevant.",
		"- If the dashboard task page changed, run bun.uat.tasks or `nix develop -c bun run --cwd web uat:tasks`; it starts a per-worktree Playwright/Vite server and mocks homelabd APIs.",
		"- For site-wide dashboard shell, navigation, theme, terminal, docs, workflow, health, or supervisor changes, run bun.uat.site or `nix develop -c bun run --cwd web uat:site`; review desktop and mobile screenshots for visual artefacts, not just pass/fail output.",
		"- If Chromium launch fails, run `nix develop -c bun run --cwd web browser:preflight` and report the browser infrastructure failure.",
		"- Do not stop or restart production dashboard, homelabd, healthd, or supervisord for UAT. Report restart impact for explicit operator verification after merge.",
		"- If browser UAT is not possible, say exactly why and what automated coverage ran instead.",
		"- If behaviour, commands, UI, configuration, tools, or workflow changed, update relevant docs/help text in the same patch.",
		"- Final done=true message must include: source URLs consulted, changed files, automated tests, browser/UAT command and interaction verified, how to use the change, and docs updated or why no docs change was needed.",
		"- Do not call git.merge_approved, repo.apply_patch_to_main, service.*, shell.run_approved, or memory.commit_write.",
		"Available UXAgent tools:",
		o.filteredToolCatalog(map[string]bool{
			"text.correct": true, "text.summarize": true,
			"internet.search": true, "internet.fetch": true, "internet.research": true,
			"repo.list": true, "repo.search": true, "repo.read": true, "repo.write_patch": true, "repo.current_diff": true,
			"git.status": true, "git.diff": true, "git.branch": true, "git.describe": true, "git.log": true, "git.show": true,
			"go.fmt": true, "go.test": true, "go.build": true, "test.run": true, "bun.check": true, "bun.build": true, "bun.test": true, "bun.uat.tasks": true, "bun.uat.site": true,
			"shell.run_limited": true,
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

func (o *Orchestrator) writeExternalRunArtifact(runID, taskID, backend, workspace, status string, result externalDelegateResult, errorText string) error {
	dir := filepath.Join(o.cfg.DataDir, "runs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	runID = firstNonEmptyString(runID, result.ID, id.New("external_run"))
	taskID = firstNonEmptyString(taskID, result.TaskID)
	payload := map[string]any{
		"id":          runID,
		"kind":        "external_agent",
		"task_id":     taskID,
		"backend":     firstNonEmptyString(backend, result.Backend),
		"workspace":   firstNonEmptyString(workspace, result.Workspace),
		"status":      status,
		"command":     result.Command,
		"output":      result.Output,
		"error":       firstNonEmptyString(errorText, result.Error),
		"duration":    result.Duration,
		"started_at":  result.StartedAt,
		"finished_at": result.FinishedAt,
		"time":        time.Now().UTC(),
	}
	b, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, runID+".json"), append(b, '\n'), 0o644)
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
	task, hasTask, err := o.loadApprovalTask(req)
	if err != nil {
		return "", err
	}
	if req.Tool == "git.merge_approved" {
		if !hasTask {
			req.Status = approvalstore.StatusStale
			req.Reason = appendApprovalReason(req.Reason, "stale: task record is missing")
			if err := o.approvals.Save(req); err != nil {
				return "", err
			}
			_ = o.events.Append(ctx, eventlog.Event{ID: id.New("evt"), Type: "approval.stale", Actor: "OrchestratorAgent", TaskID: req.TaskID, Payload: eventlog.Payload(map[string]any{"approval": req, "reason": "task record is missing"})})
			return fmt.Sprintf("Approval %s is stale: task record %q is missing. No merge was attempted.", approvalID, req.TaskID), nil
		}
		if task.Status != taskstore.StatusAwaitingApproval {
			req.Status = approvalstore.StatusStale
			req.Reason = appendApprovalReason(req.Reason, fmt.Sprintf("stale: task is %s", task.Status))
			if err := o.approvals.Save(req); err != nil {
				return "", err
			}
			_ = o.events.Append(ctx, eventlog.Event{ID: id.New("evt"), Type: "approval.stale", Actor: "OrchestratorAgent", TaskID: req.TaskID, Payload: eventlog.Payload(map[string]any{"approval": req, "task_status": task.Status})})
			return fmt.Sprintf("Approval %s is stale: task %s is %s, not %s. No merge was attempted.", approvalID, taskShortID(req.TaskID), task.Status, taskstore.StatusAwaitingApproval), nil
		}
		if newer, ok, err := o.newerPendingApprovalForTask(req); err != nil {
			return "", err
		} else if ok {
			req.Status = approvalstore.StatusStale
			req.Reason = appendApprovalReason(req.Reason, "stale: superseded by "+newer.ID)
			if err := o.approvals.Save(req); err != nil {
				return "", err
			}
			_ = o.events.Append(ctx, eventlog.Event{ID: id.New("evt"), Type: "approval.stale", Actor: "OrchestratorAgent", TaskID: req.TaskID, Payload: eventlog.Payload(map[string]any{"approval": req, "superseded_by": newer.ID})})
			return fmt.Sprintf("Approval %s is stale: task %s has a newer pending merge approval %s. No merge was attempted.", approvalID, taskShortID(req.TaskID), newer.ID), nil
		}
		if reconcileOut, err := o.reconcileApprovedTaskBranch(ctx, task); err != nil {
			req.Status = approvalstore.StatusFailed
			req.Reason = appendApprovalReason(req.Reason, "auto-rebase failed: "+err.Error())
			if saveErr := o.approvals.Save(req); saveErr != nil {
				return "", saveErr
			}
			task.Status = taskstore.StatusConflictResolution
			task.AssignedTo = "OrchestratorAgent"
			task.Result = "Approval auto-rebase failed; manual conflict resolution required: " + err.Error()
			if saveErr := o.tasks.Save(task); saveErr != nil {
				return "", saveErr
			}
			_ = o.events.Append(ctx, eventlog.Event{ID: id.New("evt"), Type: "task.conflict_resolution", Actor: "OrchestratorAgent", TaskID: req.TaskID, Payload: eventlog.Payload(map[string]any{"approval": approvalID, "reason": task.Result, "from_status": taskstore.StatusAwaitingApproval, "to_status": taskstore.StatusConflictResolution})})
			_ = o.events.Append(ctx, eventlog.Event{ID: id.New("evt"), Type: "approval.failed", Actor: "OrchestratorAgent", TaskID: req.TaskID, Payload: eventlog.Payload(map[string]any{"approval": req, "error": err.Error()})})
			shortID := taskShortID(req.TaskID)
			return fmt.Sprintf("Approval %s could not auto-rebase task %s onto current main.\nReason: %s\nTask moved to %s; no merge was applied.\nNext: `delegate %s to codex resolve the main-branch conflict`, `review %s`, or `delete %s`.", approvalID, shortID, err.Error(), taskstore.StatusConflictResolution, shortID, shortID, shortID), nil
		} else if strings.TrimSpace(reconcileOut) != "" {
			_ = o.events.Append(ctx, eventlog.Event{ID: id.New("evt"), Type: "task.workspace.reconciled", Actor: "OrchestratorAgent", TaskID: req.TaskID, Payload: eventlog.Payload(map[string]any{"approval": approvalID, "output": strings.TrimSpace(reconcileOut)})})
		}
	}
	if _, err := o.runApprovedTool(ctx, req.Tool, req.Args, req.TaskID); err != nil {
		req.Status = approvalstore.StatusFailed
		req.Reason = appendApprovalReason(req.Reason, "failed: "+err.Error())
		if saveErr := o.approvals.Save(req); saveErr != nil {
			return "", saveErr
		}
		if req.Tool == "git.merge_approved" && hasTask && !taskTerminal(task.Status) {
			task.Status = taskstore.StatusBlocked
			task.AssignedTo = "OrchestratorAgent"
			task.Result = "Approved merge failed: " + err.Error()
			if saveErr := o.tasks.Save(task); saveErr != nil {
				return "", saveErr
			}
			_ = o.events.Append(ctx, eventlog.Event{ID: id.New("evt"), Type: "task.blocked", Actor: "OrchestratorAgent", TaskID: req.TaskID, Payload: eventlog.Payload(map[string]any{"approval": approvalID, "reason": task.Result})})
		}
		_ = o.events.Append(ctx, eventlog.Event{ID: id.New("evt"), Type: "approval.failed", Actor: "human", TaskID: req.TaskID, Payload: eventlog.Payload(map[string]any{"approval": req, "error": err.Error()})})
		if req.Tool == "git.merge_approved" && req.TaskID != "" {
			return fmt.Sprintf("Approval %s failed while merging task %s.\nReason: %s\nTask moved to blocked; no merge was applied.\nNext: `delegate %s to codex rebase and resolve merge conflicts`, `review %s`, or `delete %s`.", approvalID, taskShortID(req.TaskID), err.Error(), taskShortID(req.TaskID), taskShortID(req.TaskID), taskShortID(req.TaskID)), nil
		}
		return fmt.Sprintf("Approval %s failed while executing %s.\nReason: %s", approvalID, req.Tool, err.Error()), nil
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
				restartPlan := restartPlanFromResult(t.Result)
				t.Result = "merged after approval " + approvalID + "; awaiting human verification"
				if restartPlan != "" {
					t.Result += "\nRestart plan: " + restartPlan
				}
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
			restartPlan := ""
			if t, err := o.tasks.Load(req.TaskID); err == nil {
				restartPlan = restartPlanFromResult(t.Result)
			}
			restartLine := ""
			if restartPlan != "" {
				restartLine = "\nRestart plan: " + restartPlan
			}
			return fmt.Sprintf("Approved and merged %s.\nTask %s is awaiting verification.%s\nNext: check the running app, then `accept %s` or `reopen %s <reason>`.", approvalID, taskShortID(req.TaskID), restartLine, taskShortID(req.TaskID), taskShortID(req.TaskID)), nil
		}
	}
	return "Approved and executed " + approvalID, nil
}

func (o *Orchestrator) stalePendingTaskApprovals(ctx context.Context, taskID, reason string) error {
	if strings.TrimSpace(taskID) == "" {
		return nil
	}
	requests, err := o.approvals.List()
	if err != nil {
		return err
	}
	for _, req := range requests {
		if req.TaskID != taskID || req.Status != approvalstore.StatusPending {
			continue
		}
		req.Status = approvalstore.StatusStale
		req.Reason = appendApprovalReason(req.Reason, "stale: "+reason)
		if err := o.approvals.Save(req); err != nil {
			return err
		}
		_ = o.events.Append(ctx, eventlog.Event{ID: id.New("evt"), Type: "approval.stale", Actor: "OrchestratorAgent", TaskID: taskID, Payload: eventlog.Payload(map[string]any{"approval": req, "reason": reason})})
	}
	return nil
}

func (o *Orchestrator) newerPendingApprovalForTask(req approvalstore.Request) (approvalstore.Request, bool, error) {
	if req.TaskID == "" || req.Tool == "" {
		return approvalstore.Request{}, false, nil
	}
	requests, err := o.approvals.List()
	if err != nil {
		return approvalstore.Request{}, false, err
	}
	for _, candidate := range requests {
		if candidate.ID == req.ID ||
			candidate.TaskID != req.TaskID ||
			candidate.Tool != req.Tool ||
			candidate.Status != approvalstore.StatusPending {
			continue
		}
		if approvalCreatedAfter(candidate, req) {
			return candidate, true, nil
		}
	}
	return approvalstore.Request{}, false, nil
}

func (o *Orchestrator) latestApprovalForTask(taskID, toolName, status string) (approvalstore.Request, bool, error) {
	requests, err := o.approvals.List()
	if err != nil {
		return approvalstore.Request{}, false, err
	}
	var latest approvalstore.Request
	for _, candidate := range requests {
		if candidate.TaskID != taskID || candidate.Tool != toolName || candidate.Status != status {
			continue
		}
		if latest.ID == "" || approvalCreatedAfter(candidate, latest) {
			latest = candidate
		}
	}
	return latest, latest.ID != "", nil
}

func approvalCreatedAfter(left, right approvalstore.Request) bool {
	if left.CreatedAt.Equal(right.CreatedAt) {
		return left.ID > right.ID
	}
	if right.CreatedAt.IsZero() {
		return !left.CreatedAt.IsZero() || left.ID > right.ID
	}
	if left.CreatedAt.IsZero() {
		return false
	}
	return left.CreatedAt.After(right.CreatedAt)
}

func approvalGrantedDuringCurrentRun(approval approvalstore.Request, t taskstore.Task) bool {
	if t.StartedAt == nil {
		return true
	}
	grantedAt := approval.UpdatedAt
	if grantedAt.IsZero() {
		grantedAt = approval.CreatedAt
	}
	if grantedAt.IsZero() {
		return false
	}
	return !grantedAt.Before(*t.StartedAt)
}

func (o *Orchestrator) loadApprovalTask(req approvalstore.Request) (taskstore.Task, bool, error) {
	if req.TaskID == "" {
		return taskstore.Task{}, false, nil
	}
	task, err := o.tasks.Load(req.TaskID)
	if err != nil {
		if os.IsNotExist(err) {
			return taskstore.Task{}, false, nil
		}
		return taskstore.Task{}, false, err
	}
	return task, true, nil
}

func (o *Orchestrator) reconcileApprovedTaskBranch(ctx context.Context, task taskstore.Task) (string, error) {
	if !workspaceHasGit(task.Workspace) {
		return "", nil
	}
	commitOut, err := commitReviewWorkspaceChanges(ctx, task.Workspace, task.ID)
	if err != nil {
		return "", err
	}
	mergeOut, err := o.reconcileTaskWorkspaceWithMain(ctx, task.Workspace)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(strings.Join([]string{strings.TrimSpace(commitOut), strings.TrimSpace(mergeOut)}, "\n")), nil
}

func appendApprovalReason(reason, suffix string) string {
	reason = strings.TrimSpace(reason)
	suffix = strings.TrimSpace(suffix)
	if reason == "" {
		return suffix
	}
	if suffix == "" || strings.Contains(reason, suffix) {
		return reason
	}
	return reason + "; " + suffix
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
	if strings.HasPrefix(name, "workflow.") {
		decision := o.policy.DecideNamed(actor, name, raw)
		if !decision.Allowed || decision.NeedsApproval {
			return o.handlePolicyDecision(ctx, actor, taskID, name, raw, decision)
		}
		return o.executeWorkflowTool(ctx, actor, name, raw)
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
		Goal   string                     `json:"goal"`
		Target *taskstore.ExecutionTarget `json:"target,omitempty"`
	}
	if err := json.Unmarshal(raw, &req); err != nil {
		return toolExecution{Tool: "task.create", Allowed: false, Error: err.Error()}
	}
	if req.Goal == "" {
		return toolExecution{Tool: "task.create", Allowed: false, Error: "goal is required"}
	}
	resultText, err := o.CreateTaskWithTarget(ctx, req.Goal, req.Target)
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
