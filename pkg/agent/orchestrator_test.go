package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
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

func TestSearchTheWebUsesInternetTool(t *testing.T) {
	orch := newTestOrchestrator(t, nil)
	search := &internetSearchStub{}
	if err := orch.registry.Register(search); err != nil {
		t.Fatal(err)
	}

	reply, err := orch.Handle(context.Background(), "test", "search the web for current SvelteKit adapter-auto production deployment guidance")
	if err != nil {
		t.Fatal(err)
	}
	if search.query != "current SvelteKit adapter-auto production deployment guidance" {
		t.Fatalf("query = %q, want cleaned web query", search.query)
	}
	if search.source != "web" {
		t.Fatalf("source = %q, want web", search.source)
	}
	if !strings.Contains(reply, "Internet search") || !strings.Contains(reply, "SvelteKit docs") {
		t.Fatalf("reply = %q, want formatted internet result", reply)
	}
}

func TestPlainSearchStillUsesRepoSearch(t *testing.T) {
	orch := newTestOrchestrator(t, nil)
	search := &repoSearchStub{}
	if err := orch.registry.Register(search); err != nil {
		t.Fatal(err)
	}

	reply, err := orch.Handle(context.Background(), "test", "search orchestrator")
	if err != nil {
		t.Fatal(err)
	}
	if search.query != "orchestrator" {
		t.Fatalf("query = %q, want repo query", search.query)
	}
	if !strings.Contains(reply, "pkg/agent/orchestrator.go") {
		t.Fatalf("reply = %q, want repo match", reply)
	}
}

func TestPlainWorkRequestIntent(t *testing.T) {
	for _, input := range []string{
		"implement the dashboard fix",
		"please fix the failing test",
		"we need to refactor the task parser",
		"The chat window should scroll to the bottom when a new message is received",
		"the task list needs to show the next action",
	} {
		if !isPlainWorkRequest(input) {
			t.Fatalf("isPlainWorkRequest(%q) = false, want true", input)
		}
	}
	for _, input := range []string{
		"what tasks are active?",
		"how does the dashboard work?",
		"how should I use the dashboard?",
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

func TestParseReopenCommandWithBareReason(t *testing.T) {
	selector, reason := parseReopenCommand([]string{"28493611", "needs", "rework"})
	if selector != "28493611" || reason != "needs rework" {
		t.Fatalf("unexpected parse: selector=%q reason=%q", selector, reason)
	}
}

func TestParseReopenCommandWithNaturalTitle(t *testing.T) {
	selector, reason := parseReopenCommand([]string{"the", "bun", "task", "needs", "rework"})
	if selector != "the bun task" || reason != "rework" {
		t.Fatalf("unexpected parse: selector=%q reason=%q", selector, reason)
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

func TestAwaitingVerificationTaskShowsAcceptAction(t *testing.T) {
	orch := newTestOrchestrator(t, nil)
	task := taskstore.Task{
		ID:         "task_20260425_212733_4f4b79fd",
		Title:      "fix chat scroll",
		Goal:       "fix chat scroll",
		Status:     taskstore.StatusAwaitingVerification,
		AssignedTo: "OrchestratorAgent",
		CreatedAt:  time.Now().UTC(),
		UpdatedAt:  time.Now().UTC(),
		Workspace:  filepath.Join(t.TempDir(), "workspace"),
	}
	if err := orch.tasks.Save(task); err != nil {
		t.Fatal(err)
	}

	reply, err := orch.showTask("4f4b79fd")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(reply, "`accept 4f4b79fd`") {
		t.Fatalf("reply = %q, want accept action", reply)
	}
	if !strings.Contains(reply, "`reopen 4f4b79fd needs rework`") {
		t.Fatalf("reply = %q, want reopen action", reply)
	}
	if !strings.Contains(reply, "`delete 4f4b79fd`") {
		t.Fatalf("reply = %q, want delete action", reply)
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

func TestApprovedMergeAwaitsVerificationUntilAccepted(t *testing.T) {
	orch := newTestOrchestrator(t, nil)
	if err := orch.registry.Register(mergeApprovedStub{}); err != nil {
		t.Fatal(err)
	}
	task := taskstore.Task{
		ID:         "task_20260425_212733_4f4b79fd",
		Title:      "fix chat scroll",
		Goal:       "fix chat scroll",
		Status:     taskstore.StatusAwaitingApproval,
		AssignedTo: "OrchestratorAgent",
		CreatedAt:  time.Now().UTC(),
		UpdatedAt:  time.Now().UTC(),
		Workspace:  filepath.Join(t.TempDir(), "workspace"),
	}
	if err := orch.tasks.Save(task); err != nil {
		t.Fatal(err)
	}
	req := approvalstore.Request{
		ID:     "approval_20260425_213114_4eb24ba1",
		TaskID: task.ID,
		Tool:   "git.merge_approved",
		Args:   json.RawMessage(`{"branch":"homelabd/task_20260425_212733_4f4b79fd"}`),
		Reason: "merge reviewed task branch into repo root",
		Status: approvalstore.StatusPending,
	}
	if err := orch.approvals.Save(req); err != nil {
		t.Fatal(err)
	}

	reply, err := orch.resolveApproval(context.Background(), req.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(reply, "awaiting verification") || !strings.Contains(reply, "`accept 4f4b79fd`") {
		t.Fatalf("reply = %q, want verification guidance", reply)
	}
	updated, err := orch.tasks.Load(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != taskstore.StatusAwaitingVerification {
		t.Fatalf("status = %q, want awaiting_verification", updated.Status)
	}

	reply, err = orch.acceptTask(context.Background(), "4f4b79fd")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(reply, "now done") {
		t.Fatalf("reply = %q, want done confirmation", reply)
	}
	updated, err = orch.tasks.Load(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != taskstore.StatusDone {
		t.Fatalf("status = %q, want done", updated.Status)
	}
}

func TestApprovalMergeFailureReturnsChatErrorNotHTTPFailure(t *testing.T) {
	orch := newTestOrchestrator(t, nil)
	if err := orch.registry.Register(mergeFailStub{}); err != nil {
		t.Fatal(err)
	}
	task := taskstore.Task{
		ID:         "task_20260425_213021_28493611",
		Title:      "add internet search",
		Goal:       "add internet search",
		Status:     taskstore.StatusAwaitingApproval,
		AssignedTo: "OrchestratorAgent",
		CreatedAt:  time.Now().UTC(),
		UpdatedAt:  time.Now().UTC(),
	}
	if err := orch.tasks.Save(task); err != nil {
		t.Fatal(err)
	}
	req := approvalstore.Request{
		ID:     "approval_20260425_214114_a300d8be",
		TaskID: task.ID,
		Tool:   "git.merge_approved",
		Args:   json.RawMessage(`{"branch":"homelabd/task_20260425_213021_28493611"}`),
		Reason: "merge reviewed task branch into repo root",
		Status: approvalstore.StatusPending,
	}
	if err := orch.approvals.Save(req); err != nil {
		t.Fatal(err)
	}

	reply, err := orch.Handle(context.Background(), "test", "approve approval_20260425_214114_a300d8be")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(reply, "error: git merge:") {
		t.Fatalf("reply = %q, want git merge error", reply)
	}
	updatedApproval, err := orch.approvals.Load(req.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updatedApproval.Status != approvalstore.StatusPending {
		t.Fatalf("approval status = %q, want pending", updatedApproval.Status)
	}
}

func TestReopenTaskMovesBackToRunning(t *testing.T) {
	orch := newTestOrchestrator(t, nil)
	task := taskstore.Task{
		ID:         "task_20260425_212733_4f4b79fd",
		Title:      "fix chat scroll",
		Goal:       "fix chat scroll",
		Status:     taskstore.StatusAwaitingVerification,
		AssignedTo: "OrchestratorAgent",
		CreatedAt:  time.Now().UTC(),
		UpdatedAt:  time.Now().UTC(),
	}
	if err := orch.tasks.Save(task); err != nil {
		t.Fatal(err)
	}

	reply, err := orch.Handle(context.Background(), "test", "reopen 4f4b79fd because scroll still jumps")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(reply, "Reopened 4f4b79fd") {
		t.Fatalf("reply = %q, want reopened confirmation", reply)
	}
	updated, err := orch.tasks.Load(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != taskstore.StatusRunning {
		t.Fatalf("status = %q, want running", updated.Status)
	}
	if updated.AssignedTo != "CoderAgent" {
		t.Fatalf("AssignedTo = %q, want CoderAgent", updated.AssignedTo)
	}
	if !strings.Contains(updated.Result, "scroll still jumps") {
		t.Fatalf("Result = %q, want reopen reason", updated.Result)
	}
}

func TestReopenTaskAcceptsBareReasonAfterID(t *testing.T) {
	orch := newTestOrchestrator(t, nil)
	task := taskstore.Task{
		ID:         "task_20260425_213021_28493611",
		Title:      "add internet search",
		Goal:       "add internet search",
		Status:     taskstore.StatusAwaitingVerification,
		AssignedTo: "OrchestratorAgent",
		CreatedAt:  time.Now().UTC(),
		UpdatedAt:  time.Now().UTC(),
	}
	if err := orch.tasks.Save(task); err != nil {
		t.Fatal(err)
	}

	reply, err := orch.Handle(context.Background(), "test", "reopen 28493611 needs rework")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(reply, "Reopened 28493611") {
		t.Fatalf("reply = %q, want reopened confirmation", reply)
	}
	updated, err := orch.tasks.Load(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != taskstore.StatusRunning {
		t.Fatalf("status = %q, want running", updated.Status)
	}
	if !strings.Contains(updated.Result, "needs rework") {
		t.Fatalf("Result = %q, want reopen reason", updated.Result)
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

func TestRecoverRunningTasksRestartsExternalWorker(t *testing.T) {
	delegateStarted := make(chan struct{}, 1)
	releaseDelegate := make(chan struct{})
	orch := newTestOrchestrator(t, &delegateStub{
		started: delegateStarted,
		release: releaseDelegate,
	})
	var logs bytes.Buffer
	orch.WithLogger(slog.New(slog.NewTextHandler(&logs, nil)))
	now := time.Now().UTC()
	taskID := "task_20260425_213611_73d51bee"
	if err := orch.tasks.Save(taskstore.Task{
		ID:         taskID,
		Title:      "move chat to /chat",
		Goal:       "move chat to /chat",
		Status:     taskstore.StatusRunning,
		AssignedTo: "codex",
		Workspace:  filepath.Join(t.TempDir(), "workspaces", taskID),
		CreatedAt:  now,
		UpdatedAt:  now,
	}); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	count, err := orch.RecoverRunningTasks(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("recovered count = %d, want 1", count)
	}
	if !strings.Contains(logs.String(), "recovering persisted running task") {
		t.Fatalf("logs = %q, want recovery log", logs.String())
	}
	select {
	case <-delegateStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("recovered delegate did not start")
	}
	close(releaseDelegate)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		updated, err := orch.tasks.Load(taskID)
		if err != nil {
			t.Fatal(err)
		}
		if updated.Status == taskstore.StatusReadyForReview && strings.Contains(updated.Result, "done") {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("recovered delegate did not finish cleanly")
}

func TestRecoverRunningTasksSkipsNonRunningTasks(t *testing.T) {
	delegateStarted := make(chan struct{}, 1)
	orch := newTestOrchestrator(t, &delegateStub{
		started: delegateStarted,
		release: make(chan struct{}),
	})
	if err := orch.tasks.Save(taskstore.Task{
		ID:         "task_done",
		Title:      "done work",
		Goal:       "done work",
		Status:     taskstore.StatusAwaitingVerification,
		AssignedTo: "OrchestratorAgent",
		Workspace:  filepath.Join(t.TempDir(), "workspace"),
		CreatedAt:  time.Now().UTC(),
		UpdatedAt:  time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}

	count, err := orch.RecoverRunningTasks(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("recovered count = %d, want 0", count)
	}
	select {
	case <-delegateStarted:
		t.Fatal("delegate started for non-running task")
	default:
	}
}

func TestReviewNoDiffBlocksTask(t *testing.T) {
	orch := newTestOrchestrator(t, nil)
	if err := orch.registry.Register(noDiffStub{}); err != nil {
		t.Fatal(err)
	}
	taskID := "task_20260425_213611_73d51bee"
	if err := orch.tasks.Save(taskstore.Task{
		ID:         taskID,
		Title:      "move chat to /chat",
		Goal:       "move chat to /chat",
		Status:     taskstore.StatusRunning,
		AssignedTo: "CoderAgent",
		Workspace:  filepath.Join(t.TempDir(), "workspaces", taskID),
		CreatedAt:  time.Now().UTC(),
		UpdatedAt:  time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}

	reply, err := orch.reviewTask(context.Background(), taskID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(reply, "no diff to approve") {
		t.Fatalf("reply = %q, want no diff message", reply)
	}
	updated, err := orch.tasks.Load(taskID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != taskstore.StatusBlocked {
		t.Fatalf("status = %q, want blocked", updated.Status)
	}
	if !strings.Contains(updated.Result, "no diff") {
		t.Fatalf("result = %q, want no diff reason", updated.Result)
	}
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

type repoSearchStub struct {
	query string
}

func (repoSearchStub) Name() string        { return "repo.search" }
func (repoSearchStub) Description() string { return "" }
func (repoSearchStub) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object"}`)
}
func (repoSearchStub) Risk() tool.RiskLevel { return tool.RiskReadOnly }
func (s *repoSearchStub) Run(_ context.Context, raw json.RawMessage) (json.RawMessage, error) {
	var req struct {
		Query string `json:"query"`
	}
	_ = json.Unmarshal(raw, &req)
	s.query = req.Query
	return json.Marshal(map[string]any{"matches": []map[string]any{{
		"path": "pkg/agent/orchestrator.go",
		"line": 148,
		"text": "case search",
	}}})
}

type internetSearchStub struct {
	query  string
	source string
}

func (internetSearchStub) Name() string        { return "internet.search" }
func (internetSearchStub) Description() string { return "" }
func (internetSearchStub) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object"}`)
}
func (internetSearchStub) Risk() tool.RiskLevel { return tool.RiskReadOnly }
func (s *internetSearchStub) Run(_ context.Context, raw json.RawMessage) (json.RawMessage, error) {
	var req struct {
		Query  string `json:"query"`
		Source string `json:"source"`
	}
	_ = json.Unmarshal(raw, &req)
	s.query = req.Query
	s.source = req.Source
	return json.Marshal(map[string]any{
		"query":  req.Query,
		"source": "duckduckgo",
		"results": []map[string]any{{
			"title":   "SvelteKit docs",
			"url":     "https://svelte.dev/docs/kit/adapter-auto",
			"snippet": "Adapter-auto detects supported production environments.",
		}},
	})
}

type noDiffStub struct{}

func (noDiffStub) Name() string        { return "repo.current_diff" }
func (noDiffStub) Description() string { return "" }
func (noDiffStub) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object"}`)
}
func (noDiffStub) Risk() tool.RiskLevel { return tool.RiskReadOnly }
func (noDiffStub) Run(context.Context, json.RawMessage) (json.RawMessage, error) {
	return json.Marshal(map[string]any{"diff": ""})
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

type mergeApprovedStub struct{}

func (mergeApprovedStub) Name() string        { return "git.merge_approved" }
func (mergeApprovedStub) Description() string { return "" }
func (mergeApprovedStub) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object"}`)
}
func (mergeApprovedStub) Risk() tool.RiskLevel { return tool.RiskHigh }
func (mergeApprovedStub) Run(context.Context, json.RawMessage) (json.RawMessage, error) {
	return json.Marshal(map[string]any{"merged": true})
}

type mergeFailStub struct{}

func (mergeFailStub) Name() string        { return "git.merge_approved" }
func (mergeFailStub) Description() string { return "" }
func (mergeFailStub) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object"}`)
}
func (mergeFailStub) Risk() tool.RiskLevel { return tool.RiskHigh }
func (mergeFailStub) Run(context.Context, json.RawMessage) (json.RawMessage, error) {
	return nil, fmt.Errorf("git merge: exit status 2: local changes would be overwritten")
}
