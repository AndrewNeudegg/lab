package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/andrewneudegg/lab/pkg/config"
	"github.com/andrewneudegg/lab/pkg/eventlog"
	"github.com/andrewneudegg/lab/pkg/llm"
	taskstore "github.com/andrewneudegg/lab/pkg/task"
	"github.com/andrewneudegg/lab/pkg/tool"
	approvalstore "github.com/andrewneudegg/lab/pkg/tools/approval"
)

func TestExtractJSONUsesFirstBalancedObject(t *testing.T) {
	input := `{"message":"List files","done":false,"tool_calls":[{"tool":"repo.list","args":{"workspace":"/tmp/workspaces/task"}}]} trailing prose with {"other":true}`
	got := extractJSON(input)
	want := `{"message":"List files","done":false,"tool_calls":[{"tool":"repo.list","args":{"workspace":"/tmp/workspaces/task"}}]}`
	if got != want {
		t.Fatalf("extractJSON() = %q, want %q", got, want)
	}
}

func TestExtractJSONHandlesBracesInStrings(t *testing.T) {
	input := `prefix {"message":"brace } inside string","done":true,"tool_calls":[]} suffix {"ignored":true}`
	got := extractJSON(input)
	want := `{"message":"brace } inside string","done":true,"tool_calls":[]}`
	if got != want {
		t.Fatalf("extractJSON() = %q, want %q", got, want)
	}
}

func TestNormalizeTaskSelectorRemovesNaturalFiller(t *testing.T) {
	got := normalizeTaskSelector("the hi task")
	if got != "hi" {
		t.Fatalf("normalizeTaskSelector() = %q, want hi", got)
	}
}

func TestActiveTaskStatusIntent(t *testing.T) {
	for _, input := range []string{
		"list all active tasks",
		"active tasks",
		"what tasks are active?",
		"what work is in progress",
		"what tasks are in progress",
		"status",
	} {
		if !isActiveTaskStatusRequest(input) {
			t.Fatalf("isActiveTaskStatusRequest(%q) = false, want true", input)
		}
	}
}

func TestPlainWorkRequestIntent(t *testing.T) {
	for _, input := range []string{
		"implement the dashboard fix",
		"please fix the failing test",
		"we need to refactor the task parser",
	} {
		if !isPlainWorkRequest(input) {
			t.Fatalf("isPlainWorkRequest(%q) = false, want true", input)
		}
	}
	for _, input := range []string{
		"what tasks are active?",
		"how does the dashboard work?",
		"help",
		"hello",
	} {
		if isPlainWorkRequest(input) {
			t.Fatalf("isPlainWorkRequest(%q) = true, want false", input)
		}
	}
}

func TestParseDelegateCommandNaturalForm(t *testing.T) {
	selector, backend, instruction, ok := parseDelegateCommand([]string{"delegate", "the", "bun", "task", "to", "codex"})
	if !ok {
		t.Fatalf("expected delegate command to parse")
	}
	if selector != "the bun task" || backend != "codex" || instruction != "" {
		t.Fatalf("unexpected parse: selector=%q backend=%q instruction=%q", selector, backend, instruction)
	}
}

func TestParseDelegateCommandStrictForm(t *testing.T) {
	selector, backend, instruction, ok := parseDelegateCommand([]string{"delegate", "c26f013d", "codex", "finish", "it"})
	if !ok {
		t.Fatalf("expected delegate command to parse")
	}
	if selector != "c26f013d" || backend != "codex" || instruction != "finish it" {
		t.Fatalf("unexpected parse: selector=%q backend=%q instruction=%q", selector, backend, instruction)
	}
}

func TestNaturalActiveTasksDoesNotCreateTask(t *testing.T) {
	orch := newTestOrchestrator(t, nil)
	now := time.Now().UTC()
	for _, task := range []taskstore.Task{{
		ID:        "task_running",
		Title:     "running work",
		Goal:      "running work",
		Status:    taskstore.StatusRunning,
		CreatedAt: now,
	}, {
		ID:        "task_done",
		Title:     "done work",
		Goal:      "done work",
		Status:    taskstore.StatusDone,
		CreatedAt: now.Add(-time.Minute),
	}} {
		if err := orch.tasks.Save(task); err != nil {
			t.Fatal(err)
		}
	}

	reply, err := orch.Handle(context.Background(), "test", "what tasks are active?")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(reply, "running work") {
		t.Fatalf("reply = %q, want active task", reply)
	}
	if strings.Contains(reply, "task_done") {
		t.Fatalf("reply = %q, should not include done task", reply)
	}
	tasks, err := orch.tasks.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 2 {
		t.Fatalf("task count = %d, want 2", len(tasks))
	}
}

func TestTaskViewsIncludeClickableNextActions(t *testing.T) {
	orch := newTestOrchestrator(t, nil)
	task := taskstore.Task{
		ID:         "task_20260425_174912_1db1c910",
		Title:      "retain chat history",
		Goal:       "retain chat history",
		Status:     taskstore.StatusReadyForReview,
		AssignedTo: "OrchestratorAgent",
		CreatedAt:  time.Now().UTC(),
		UpdatedAt:  time.Now().UTC(),
		Workspace:  filepath.Join(t.TempDir(), "workspace"),
	}
	if err := orch.tasks.Save(task); err != nil {
		t.Fatal(err)
	}

	for name, view := range map[string]func() (string, error){
		"tasks": func() (string, error) { return orch.listTasks() },
		"show":  func() (string, error) { return orch.showTask("1db1c910") },
	} {
		reply, err := view()
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(reply, "`review 1db1c910`") {
			t.Fatalf("%s reply = %q, want review action", name, reply)
		}
		if !strings.Contains(reply, "`delete 1db1c910`") {
			t.Fatalf("%s reply = %q, want delete action", name, reply)
		}
	}
}

func TestBadTaskSelectorReturnsChatErrorNotHTTPFailure(t *testing.T) {
	orch := newTestOrchestrator(t, nil)
	reply, err := orch.Handle(context.Background(), "test", "Diff summary:")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(reply, "I couldn't match that to a task") {
		t.Fatalf("reply = %q, want friendly task selector error", reply)
	}
}

func TestApprovalsIncludeClickableActions(t *testing.T) {
	orch := newTestOrchestrator(t, nil)
	req := approvalstore.Request{
		ID:     "approval_20260425_205243_caf98d74",
		TaskID: "task_20260425_174912_1db1c910",
		Tool:   "git.merge_approved",
		Reason: "merge reviewed task branch into repo root",
		Status: approvalstore.StatusPending,
	}
	if err := orch.approvals.Save(req); err != nil {
		t.Fatal(err)
	}

	reply, err := orch.listApprovals()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(reply, "`approve approval_20260425_205243_caf98d74`") {
		t.Fatalf("reply = %q, want approve action", reply)
	}
	if !strings.Contains(reply, "`deny approval_20260425_205243_caf98d74`") {
		t.Fatalf("reply = %q, want deny action", reply)
	}
}

func TestCreateTaskUsesFencedCommandBlock(t *testing.T) {
	orch := newTestOrchestrator(t, nil)

	reply, err := orch.Handle(context.Background(), "test", "new add fenced commands")
	if err != nil {
		t.Fatal(err)
	}
	tasks, err := orch.tasks.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 {
		t.Fatalf("task count = %d, want 1", len(tasks))
	}
	want := "Next:\n```\nrun " + tasks[0].ID + "\ndelegate " + tasks[0].ID + " <agent> <instruction>\nreview " + tasks[0].ID + "\n```"
	if !strings.Contains(reply, want) {
		t.Fatalf("reply = %q, want fenced commands %q", reply, want)
	}
	if strings.Contains(reply, "`run ") {
		t.Fatalf("reply = %q, should not inline command suggestions", reply)
	}
}

func TestReviewDoesNotRequestApprovalWhenChecksFail(t *testing.T) {
	orch := newTestOrchestrator(t, nil)
	if err := orch.registry.Register(currentDiffStub{}); err != nil {
		t.Fatal(err)
	}
	if err := orch.registry.Register(goTestFailStub{}); err != nil {
		t.Fatal(err)
	}
	workspace := filepath.Join(t.TempDir(), "workspace")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "go.mod"), []byte("module reviewtest\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	task := taskstore.Task{
		ID:        "task_20260425_205658_1e0b26b6",
		Title:     "add internet search",
		Goal:      "add internet search",
		Status:    taskstore.StatusReadyForReview,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
		Workspace: workspace,
	}
	if err := orch.tasks.Save(task); err != nil {
		t.Fatal(err)
	}

	reply, err := orch.reviewTask(context.Background(), "1e0b26b6")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(reply, "Approval requested") || strings.Contains(reply, "`approve ") {
		t.Fatalf("reply = %q, should not request approval", reply)
	}
	if !strings.Contains(reply, "No approval created because checks failed") {
		t.Fatalf("reply = %q, want no approval explanation", reply)
	}
	if !strings.Contains(reply, "`delegate 1e0b26b6 to codex fix the failing tests`") {
		t.Fatalf("reply = %q, want rework action", reply)
	}
	approvals, err := orch.approvals.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(approvals) != 0 {
		t.Fatalf("approval count = %d, want 0", len(approvals))
	}
	updated, err := orch.tasks.Load(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != taskstore.StatusBlocked {
		t.Fatalf("status = %q, want blocked", updated.Status)
	}
}

func TestOpenEndedChatRetainsHistory(t *testing.T) {
	provider := &recordingProvider{}
	orch := newTestOrchestrator(t, nil)
	orch.provider = provider
	orch.model = "test-model"

	reply, err := orch.Handle(context.Background(), "test", "what is this project?")
	if err != nil {
		t.Fatal(err)
	}
	if reply != "reply 1" {
		t.Fatalf("first reply = %q, want reply 1", reply)
	}
	reply, err = orch.Handle(context.Background(), "test", "what did I ask first?")
	if err != nil {
		t.Fatal(err)
	}
	if reply != "reply 2" {
		t.Fatalf("second reply = %q, want reply 2", reply)
	}
	if len(provider.requests) != 2 {
		t.Fatalf("request count = %d, want 2", len(provider.requests))
	}

	got := provider.requests[1].Messages
	assertContainsLLMMessage(t, got, llm.Message{Role: "user", Content: "what is this project?"})
	assertContainsLLMMessage(t, got, llm.Message{Role: "assistant", Content: "reply 1"})
	assertContainsLLMMessage(t, got, llm.Message{Role: "user", Content: "what did I ask first?"})
}

func TestOpenEndedChatReportsProviderSource(t *testing.T) {
	provider := &recordingProvider{}
	orch := newTestOrchestrator(t, nil)
	orch.provider = provider
	orch.model = "test-model"

	result, err := orch.HandleDetailed(context.Background(), "test", "what is this project?")
	if err != nil {
		t.Fatal(err)
	}
	if result.Source != "recording" {
		t.Fatalf("source = %q, want recording", result.Source)
	}
}

func TestPlainWorkRequestStartsCodexDelegationAsync(t *testing.T) {
	delegateStarted := make(chan struct{}, 1)
	releaseDelegate := make(chan struct{})
	orch := newTestOrchestrator(t, &delegateStub{
		started: delegateStarted,
		release: releaseDelegate,
	})

	reply, err := orch.Handle(context.Background(), "test", "implement the task parser")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(reply, "started codex") || !strings.Contains(reply, "cooking") {
		t.Fatalf("reply = %q, want cooking response", reply)
	}

	select {
	case <-delegateStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("delegate did not start")
	}

	tasks, err := orch.tasks.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 {
		t.Fatalf("task count = %d, want 1", len(tasks))
	}
	shortID := taskShortID(tasks[0].ID)
	want := "Next:\n```\nstatus\nshow " + shortID + "\nreview " + shortID + "\n```"
	if !strings.Contains(reply, want) {
		t.Fatalf("reply = %q, want fenced commands %q", reply, want)
	}
	if strings.Contains(reply, "`status`") {
		t.Fatalf("reply = %q, should not inline command suggestions", reply)
	}
	if tasks[0].AssignedTo != "codex" {
		t.Fatalf("AssignedTo = %q, want codex", tasks[0].AssignedTo)
	}
	close(releaseDelegate)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		tasks, err = orch.tasks.List()
		if err != nil {
			t.Fatal(err)
		}
		if len(tasks) == 1 && tasks[0].AssignedTo == "OrchestratorAgent" && tasks[0].Status == taskstore.StatusReadyForReview && strings.Contains(tasks[0].Result, "done") {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("delegate did not finish cleanly")
}

type recordingProvider struct {
	requests []llm.CompletionRequest
}

func (p *recordingProvider) Name() string { return "recording" }

func (p *recordingProvider) Complete(_ context.Context, req llm.CompletionRequest) (llm.CompletionResponse, error) {
	p.requests = append(p.requests, req)
	reply := "reply " + string(rune('0'+len(p.requests)))
	return llm.CompletionResponse{
		Message: llm.Message{
			Role:    "assistant",
			Content: `{"message":"` + reply + `","done":true,"tool_calls":[]}`,
		},
		Provider: p.Name(),
	}, nil
}

func assertContainsLLMMessage(t *testing.T, messages []llm.Message, want llm.Message) {
	t.Helper()
	for _, msg := range messages {
		if msg == want {
			return
		}
	}
	t.Fatalf("messages = %#v, want to contain %#v", messages, want)
}

type delegateStub struct {
	started chan struct{}
	release chan struct{}
}

func newTestOrchestrator(t *testing.T, delegate *delegateStub) *Orchestrator {
	t.Helper()
	dir := t.TempDir()
	registry := tool.NewRegistry()
	if err := registry.Register(worktreeCreateStub{root: filepath.Join(dir, "workspaces")}); err != nil {
		t.Fatal(err)
	}
	if err := registry.Register(agentListStub{}); err != nil {
		t.Fatal(err)
	}
	if delegate == nil {
		delegate = &delegateStub{started: make(chan struct{}, 1), release: make(chan struct{})}
		close(delegate.release)
	}
	if err := registry.Register(agentDelegateStub{stub: delegate}); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.DataDir = filepath.Join(dir, "data")
	cfg.Repo.Root = dir
	cfg.Repo.WorkspaceRoot = filepath.Join(dir, "workspaces")
	tasks := taskstore.NewStore(filepath.Join(cfg.DataDir, "tasks"))
	return NewOrchestrator(
		cfg,
		eventlog.NewStore(filepath.Join(cfg.DataDir, "events")),
		tasks,
		approvalstore.NewStore(filepath.Join(cfg.DataDir, "approvals")),
		registry,
		tool.NewPolicy(nil),
		nil,
		"",
	)
}

type worktreeCreateStub struct {
	root string
}

func (worktreeCreateStub) Name() string        { return "git.worktree_create" }
func (worktreeCreateStub) Description() string { return "" }
func (worktreeCreateStub) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object"}`)
}
func (worktreeCreateStub) Risk() tool.RiskLevel { return tool.RiskLow }
func (s worktreeCreateStub) Run(_ context.Context, raw json.RawMessage) (json.RawMessage, error) {
	var req struct {
		TaskID string `json:"task_id"`
	}
	_ = json.Unmarshal(raw, &req)
	return json.Marshal(map[string]any{
		"workspace": filepath.Join(s.root, req.TaskID),
		"branch":    "homelabd/" + req.TaskID,
	})
}

type agentListStub struct{}

func (agentListStub) Name() string        { return "agent.list" }
func (agentListStub) Description() string { return "" }
func (agentListStub) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object"}`)
}
func (agentListStub) Risk() tool.RiskLevel { return tool.RiskReadOnly }
func (agentListStub) Run(context.Context, json.RawMessage) (json.RawMessage, error) {
	return json.Marshal(map[string]any{"agents": []map[string]any{{
		"name":      "codex",
		"enabled":   true,
		"available": true,
	}}})
}

type agentDelegateStub struct {
	stub *delegateStub
}

func (agentDelegateStub) Name() string        { return "agent.delegate" }
func (agentDelegateStub) Description() string { return "" }
func (agentDelegateStub) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object"}`)
}
func (agentDelegateStub) Risk() tool.RiskLevel { return tool.RiskMedium }
func (s agentDelegateStub) Run(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
	var req struct {
		Backend   string `json:"backend"`
		TaskID    string `json:"task_id"`
		Workspace string `json:"workspace"`
	}
	_ = json.Unmarshal(raw, &req)
	select {
	case s.stub.started <- struct{}{}:
	default:
	}
	select {
	case <-s.stub.release:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return json.Marshal(map[string]any{
		"id":        "external_run_test",
		"backend":   req.Backend,
		"task_id":   req.TaskID,
		"workspace": req.Workspace,
		"output":    "done",
	})
}

type currentDiffStub struct{}

func (currentDiffStub) Name() string        { return "repo.current_diff" }
func (currentDiffStub) Description() string { return "" }
func (currentDiffStub) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object"}`)
}
func (currentDiffStub) Risk() tool.RiskLevel { return tool.RiskReadOnly }
func (currentDiffStub) Run(context.Context, json.RawMessage) (json.RawMessage, error) {
	return json.Marshal(map[string]any{
		"diff": "diff --git a/main.go b/main.go\n--- a/main.go\n+++ b/main.go\n@@ -1 +1 @@\n-old\n+new\n",
	})
}

type goTestFailStub struct{}

func (goTestFailStub) Name() string        { return "go.test" }
func (goTestFailStub) Description() string { return "" }
func (goTestFailStub) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object"}`)
}
func (goTestFailStub) Risk() tool.RiskLevel { return tool.RiskLow }
func (goTestFailStub) Run(context.Context, json.RawMessage) (json.RawMessage, error) {
	raw, err := json.Marshal(map[string]any{
		"command": "go test ./...",
		"output":  "FAIL\n",
	})
	if err != nil {
		return nil, err
	}
	return raw, fmt.Errorf("go test failed")
}
