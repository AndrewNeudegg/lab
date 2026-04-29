package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/andrewneudegg/lab/pkg/config"
	"github.com/andrewneudegg/lab/pkg/eventlog"
	"github.com/andrewneudegg/lab/pkg/llm"
	memstore "github.com/andrewneudegg/lab/pkg/memory"
	"github.com/andrewneudegg/lab/pkg/remoteagent"
	taskstore "github.com/andrewneudegg/lab/pkg/task"
	"github.com/andrewneudegg/lab/pkg/tool"
	approvalstore "github.com/andrewneudegg/lab/pkg/tools/approval"
	workflowstore "github.com/andrewneudegg/lab/pkg/workflow"
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
	for _, input := range []string{
		"Task: add active task parser coverage",
		"create a task to fix running task creation parsing",
		"Resolve parsing of task creation messages where the goal mentions active tasks",
	} {
		if isActiveTaskStatusRequest(input) {
			t.Fatalf("isActiveTaskStatusRequest(%q) = true, want false", input)
		}
	}
}

func TestTaskCreationIntentParsesNaturalForms(t *testing.T) {
	for _, tt := range []struct {
		input string
		want  string
	}{
		{
			input: "Task: Add Playwright end-to-end tests for the chat and task components covering queue filters",
			want:  "Add Playwright end-to-end tests for the chat and task components covering queue filters",
		},
		{
			input: "task the chat markdown code blocks are exceeding the bounds of their message bubble",
			want:  "the chat markdown code blocks are exceeding the bounds of their message bubble",
		},
		{
			input: "create a task to Resolve parsing of task creation messages in chat where the task to be created contains words like running or active tasks",
			want:  "Resolve parsing of task creation messages in chat where the task to be created contains words like running or active tasks",
		},
		{
			input: "please create a task: Implement a pre-review rebase worker that automatically attempts to recover",
			want:  "Implement a pre-review rebase worker that automatically attempts to recover",
		},
		{
			input: "can you create a task that when a task is complete and a component was touched homelabd restarts it",
			want:  "when a task is complete and a component was touched homelabd restarts it",
		},
	} {
		got, ok := taskCreationGoal(tt.input)
		if !ok {
			t.Fatalf("taskCreationGoal(%q) ok = false, want true", tt.input)
		}
		if got != tt.want {
			t.Fatalf("taskCreationGoal(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestTaskCreationIntentRejectsStatusAndExistingTaskCommands(t *testing.T) {
	for _, input := range []string{
		"tasks",
		"task status",
		"Task: status",
		"list all active tasks",
		"what tasks are active?",
		"start the bun task",
		"delegate the bun task to codex",
	} {
		if goal, ok := taskCreationGoal(input); ok {
			t.Fatalf("taskCreationGoal(%q) = %q, true; want false", input, goal)
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

func TestSearchTheWebCorrectsQueryWhenTextToolAvailable(t *testing.T) {
	orch := newTestOrchestrator(t, nil)
	correct := &textCorrectStub{corrected: "kittens in pajamas", variants: []string{"kittens in pyjamas", "kittens in pijamas"}}
	search := &internetSearchStub{}
	if err := orch.registry.Register(correct); err != nil {
		t.Fatal(err)
	}
	if err := orch.registry.Register(search); err != nil {
		t.Fatal(err)
	}

	reply, err := orch.Handle(context.Background(), "test", "search the web for kittens in pijamas")
	if err != nil {
		t.Fatal(err)
	}
	if correct.text != "kittens in pijamas" {
		t.Fatalf("text.correct text = %q", correct.text)
	}
	if search.query != "kittens in pajamas" {
		t.Fatalf("search query = %q, want corrected query", search.query)
	}
	if !strings.Contains(reply, "Corrected search query: kittens in pijamas -> kittens in pajamas") {
		t.Fatalf("reply = %q, want correction note", reply)
	}
}

func TestResearchCommandUsesInternetResearchTool(t *testing.T) {
	orch := newTestOrchestrator(t, nil)
	research := &internetResearchStub{}
	if err := orch.registry.Register(research); err != nil {
		t.Fatal(err)
	}

	reply, err := orch.Handle(context.Background(), "test", "deep research local LLM agent web research architecture")
	if err != nil {
		t.Fatal(err)
	}
	if research.query != "local LLM agent research architecture" {
		t.Fatalf("query = %q, want cleaned research query", research.query)
	}
	if research.depth != "deep" {
		t.Fatalf("depth = %q, want deep", research.depth)
	}
	if !strings.Contains(reply, "Research bundle") || !strings.Contains(reply, "Agent search architecture") {
		t.Fatalf("reply = %q, want formatted research result", reply)
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

func TestParseDelegateCommandAcceptsUXAgent(t *testing.T) {
	selector, backend, instruction, ok := parseDelegateCommand([]string{"delegate", "the", "task", "to", "ux", "audit", "mobile"})
	if !ok {
		t.Fatalf("expected delegate command to parse")
	}
	if selector != "the task" || backend != "ux" || instruction != "audit mobile" {
		t.Fatalf("unexpected parse: selector=%q backend=%q instruction=%q", selector, backend, instruction)
	}
}

func TestParseRetryCommand(t *testing.T) {
	tests := []struct {
		input           []string
		wantSelector    string
		wantBackend     string
		wantInstruction string
	}{
		{[]string{"6d41996e"}, "6d41996e", "", ""},
		{[]string{"6d41996e", "codex", "resolve", "the", "conflict"}, "6d41996e", "codex", "resolve the conflict"},
		{[]string{"6d41996e", "with", "claude", "rebase", "it"}, "6d41996e", "claude", "rebase it"},
		{[]string{"6d41996e", "unstick", "the", "review"}, "6d41996e", "", "unstick the review"},
	}
	for _, tt := range tests {
		selector, backend, instruction := parseRetryCommand(tt.input)
		if selector != tt.wantSelector || backend != tt.wantBackend || instruction != tt.wantInstruction {
			t.Fatalf("parseRetryCommand(%v) = (%q, %q, %q), want (%q, %q, %q)", tt.input, selector, backend, instruction, tt.wantSelector, tt.wantBackend, tt.wantInstruction)
		}
	}
}

func TestParseSpecialistRunCommandWithTaskIDInstruction(t *testing.T) {
	selector, instruction := parseSpecialistRunCommand([]string{"1e0b26b6", "check", "mobile", "states"})
	if selector != "1e0b26b6" || instruction != "check mobile states" {
		t.Fatalf("unexpected parse: selector=%q instruction=%q", selector, instruction)
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

func TestResolveTaskIDExactShortIDBeatsFuzzyGoalMention(t *testing.T) {
	orch := newTestOrchestrator(t, nil)
	now := time.Now().UTC()
	target := taskstore.Task{
		ID:         "task_20260425_235939_a927493f",
		Title:      "action reflection result into a new task",
		Goal:       "action reflection result into a new task",
		Status:     taskstore.StatusReadyForReview,
		AssignedTo: "OrchestratorAgent",
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	mentioning := taskstore.Task{
		ID:         "task_20260426_000306_09a4b60d",
		Title:      "investigate failed review command",
		Goal:       "review a927493f failed even though the task exists",
		Status:     taskstore.StatusRunning,
		AssignedTo: "codex",
		CreatedAt:  now.Add(time.Minute),
		UpdatedAt:  now.Add(time.Minute),
	}
	for _, task := range []taskstore.Task{target, mentioning} {
		if err := orch.tasks.Save(task); err != nil {
			t.Fatal(err)
		}
	}

	taskID, err := orch.resolveTaskID("a927493f")
	if err != nil {
		t.Fatal(err)
	}
	if taskID != target.ID {
		t.Fatalf("taskID = %q, want %q", taskID, target.ID)
	}
	reply, err := orch.Handle(context.Background(), "test", "show a927493f")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(reply, target.ID) {
		t.Fatalf("reply = %q, want exact short-id task", reply)
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

func TestAmbiguousTaskSelectorExplainsAmbiguity(t *testing.T) {
	orch := newTestOrchestrator(t, nil)
	now := time.Now().UTC()
	for _, task := range []taskstore.Task{{
		ID:        "task_20260425_174912_1db1c910",
		Title:     "chat cleanup",
		Goal:      "chat cleanup",
		Status:    taskstore.StatusRunning,
		CreatedAt: now,
		UpdatedAt: now,
	}, {
		ID:        "task_20260425_213021_28493611",
		Title:     "chat cleanup",
		Goal:      "chat cleanup",
		Status:    taskstore.StatusRunning,
		CreatedAt: now.Add(time.Minute),
		UpdatedAt: now.Add(time.Minute),
	}} {
		if err := orch.tasks.Save(task); err != nil {
			t.Fatal(err)
		}
	}

	reply, err := orch.Handle(context.Background(), "test", "show chat cleanup")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(reply, "more than one matching task") || !strings.Contains(reply, "1db1c910") || !strings.Contains(reply, "28493611") {
		t.Fatalf("reply = %q, want actionable ambiguity detail", reply)
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
		t.Fatalf("task count = %d, want one queued task", len(tasks))
	}
	created := tasks[0]
	if created.Status != taskstore.StatusQueued || created.GraphPhase != "" || created.ParentID != "" {
		t.Fatalf("created task = %#v, want standalone queued task", created)
	}
	if created.Workspace == "" {
		t.Fatalf("workspace = %q, want isolated task workspace", created.Workspace)
	}
	if created.Plan == nil {
		t.Fatal("task plan is nil, want reviewed plan")
	}
	if created.Plan.Status != "reviewed" {
		t.Fatalf("plan status = %q, want reviewed", created.Plan.Status)
	}
	if len(created.Plan.Steps) != 4 {
		t.Fatalf("plan step count = %d, want 4", len(created.Plan.Steps))
	}
	if !strings.Contains(reply, "Plan created and reviewed before execution") {
		t.Fatalf("reply = %q, want plan creation note", reply)
	}
	if !strings.Contains(reply, "Created queued task") || strings.Contains(reply, "Child phases:") {
		t.Fatalf("reply = %q, want single queued task summary", reply)
	}
	want := "Next:\n```\nstatus\nrun " + created.ID + "\ndelegate " + created.ID + " to codex\n```"
	if !strings.Contains(reply, want) {
		t.Fatalf("reply = %q, want fenced commands %q", reply, want)
	}
	if strings.Contains(reply, "`run ") {
		t.Fatalf("reply = %q, should not inline command suggestions", reply)
	}
	events, err := orch.events.ReadDay(time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	var sawCreated, sawReviewed bool
	for _, event := range events {
		if event.TaskID != created.ID {
			continue
		}
		switch event.Type {
		case "task.plan.created":
			sawCreated = true
		case "task.plan.reviewed":
			sawReviewed = true
		}
	}
	if !sawCreated || !sawReviewed {
		t.Fatalf("plan events created=%v reviewed=%v, want both", sawCreated, sawReviewed)
	}
}

func TestWorkflowChatCommandsCreateListAndRun(t *testing.T) {
	orch := newTestOrchestrator(t, nil)
	orch.provider = &staticProvider{content: "Research summary ready."}
	orch.model = "test-model"

	reply, err := orch.Handle(context.Background(), "test", "workflow new Research releases: Find release notes and summarise risk")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(reply, "Created workflow") || !strings.Contains(reply, "workflow run") {
		t.Fatalf("reply = %q, want workflow creation with run command", reply)
	}
	workflows, err := orch.ListWorkflows()
	if err != nil {
		t.Fatal(err)
	}
	if len(workflows) != 1 {
		t.Fatalf("workflow count = %d, want 1", len(workflows))
	}

	reply, err = orch.Handle(context.Background(), "test", "workflows")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(reply, "Research releases") || !strings.Contains(reply, "tool call") {
		t.Fatalf("reply = %q, want workflow list with estimate", reply)
	}

	reply, err = orch.Handle(context.Background(), "test", "workflow run "+workflowShortID(workflows[0].ID))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(reply, "completed") || !strings.Contains(reply, "Research summary ready") {
		t.Fatalf("reply = %q, want completed workflow run", reply)
	}
	updated, err := orch.LoadWorkflow(workflows[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != workflowstore.StatusCompleted || updated.LastRun == nil || len(updated.LastRun.Outputs) != 1 {
		t.Fatalf("workflow = %#v, want completed run output", updated)
	}
}

func TestWorkflowToolStepRunsThroughPolicyBoundTool(t *testing.T) {
	orch := newTestOrchestrator(t, nil)
	if err := orch.registry.Register(workflowTextCorrectStub{}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(orch.toolCatalog(), "workflow.run") {
		t.Fatal("workflow.run missing from LLM tool catalog")
	}
	item, _, err := orch.CreateWorkflow(context.Background(), workflowstore.CreateRequest{
		Name: "Correct query",
		Steps: []workflowstore.Step{{
			Name: "Correct text",
			Kind: workflowstore.StepKindTool,
			Tool: "text.correct",
			Args: json.RawMessage(`{"text":"kittens in pijamas","mode":"search_query"}`),
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	updated, reply, err := orch.RunWorkflow(context.Background(), item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != workflowstore.StatusCompleted {
		t.Fatalf("status = %q, reply = %q, want completed", updated.Status, reply)
	}
	if updated.LastRun == nil || len(updated.LastRun.Outputs) != 1 || updated.LastRun.Outputs[0].Tool != "text.correct" {
		t.Fatalf("last run = %#v, want text.correct output", updated.LastRun)
	}
}

func TestWorkflowWaitStepCompletesForHomelabdHealthCondition(t *testing.T) {
	orch := newTestOrchestrator(t, nil)
	item, _, err := orch.CreateWorkflow(context.Background(), workflowstore.CreateRequest{
		Name: "Health gate",
		Steps: []workflowstore.Step{{
			Name:           "Homelabd health",
			Kind:           workflowstore.StepKindWait,
			Condition:      "homelabd health is reachable",
			TimeoutSeconds: 60,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}

	updated, reply, err := orch.RunWorkflow(context.Background(), item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != workflowstore.StatusCompleted || !strings.Contains(reply, "Condition met: homelabd health is reachable") {
		t.Fatalf("status = %q, reply = %q, want completed health wait", updated.Status, reply)
	}
	if updated.LastRun == nil || len(updated.LastRun.Outputs) != 1 || updated.LastRun.Outputs[0].FinishedAt == nil {
		t.Fatalf("last run = %#v, want one finished wait output", updated.LastRun)
	}
}

func TestWorkflowRunResumesTimedWait(t *testing.T) {
	orch := newTestOrchestrator(t, nil)
	item, _, err := orch.CreateWorkflow(context.Background(), workflowstore.CreateRequest{
		Name: "Delay gate",
		Steps: []workflowstore.Step{{
			Name:           "Delay",
			Kind:           workflowstore.StepKindWait,
			TimeoutSeconds: 5,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}

	waiting, _, err := orch.RunWorkflow(context.Background(), item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if waiting.Status != workflowstore.StatusWaiting || waiting.LastRun == nil || len(waiting.LastRun.Outputs) != 1 {
		t.Fatalf("workflow = %#v, want waiting run", waiting)
	}
	runID := waiting.LastRun.ID

	store, err := orch.workflowStore()
	if err != nil {
		t.Fatal(err)
	}
	waiting.LastRun.StartedAt = time.Now().UTC().Add(-6 * time.Second)
	waiting.LastRun.Outputs[0].StartedAt = waiting.LastRun.StartedAt
	if err := store.Save(waiting); err != nil {
		t.Fatal(err)
	}

	updated, reply, err := orch.RunWorkflow(context.Background(), item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != workflowstore.StatusCompleted || !strings.Contains(reply, "Wait completed") {
		t.Fatalf("status = %q, reply = %q, want completed resumed wait", updated.Status, reply)
	}
	if updated.LastRun == nil || updated.LastRun.ID != runID || len(updated.LastRun.Outputs) != 1 {
		t.Fatalf("last run = %#v, want same resumed run with one output", updated.LastRun)
	}
}

func TestWorkflowRunFailsExpiredConditionalWait(t *testing.T) {
	orch := newTestOrchestrator(t, nil)
	item, _, err := orch.CreateWorkflow(context.Background(), workflowstore.CreateRequest{
		Name: "Manual gate",
		Steps: []workflowstore.Step{{
			Name:           "Manual approval",
			Kind:           workflowstore.StepKindWait,
			Condition:      "manual deployment gate",
			TimeoutSeconds: 1,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}

	waiting, _, err := orch.RunWorkflow(context.Background(), item.ID)
	if err != nil {
		t.Fatal(err)
	}
	waiting.LastRun.StartedAt = time.Now().UTC().Add(-2 * time.Second)
	waiting.LastRun.Outputs[0].StartedAt = waiting.LastRun.StartedAt
	store, err := orch.workflowStore()
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(waiting); err != nil {
		t.Fatal(err)
	}

	updated, reply, err := orch.RunWorkflow(context.Background(), item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != workflowstore.StatusFailed || !strings.Contains(reply, "timed out after 1s waiting for condition: manual deployment gate") {
		t.Fatalf("status = %q, reply = %q, want timed-out wait failure", updated.Status, reply)
	}
}

func TestCreateTaskUsesSummarizedTitle(t *testing.T) {
	orch := newTestOrchestrator(t, nil)
	summarizer := &taskTitleSummaryStub{summary: "Summarise task creation titles"}
	if err := orch.registry.Register(summarizer); err != nil {
		t.Fatal(err)
	}
	goal := "Work this task to completion if possible. Inspect the task workspace before editing. Task goal: when tasks are created their title should be derived from an automatic LLM summarisation of the user's input"

	if _, err := orch.Handle(context.Background(), "test", "new "+goal); err != nil {
		t.Fatal(err)
	}
	tasks, err := orch.tasks.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 {
		t.Fatalf("task count = %d, want 1", len(tasks))
	}
	if tasks[0].Title != "Summarise task creation titles" {
		t.Fatalf("title = %q, want summarized title", tasks[0].Title)
	}
	if summarizer.text != goal {
		t.Fatalf("summarizer text = %q, want original goal", summarizer.text)
	}
	if summarizer.purpose != "task_title" || summarizer.maxCharacters != taskTitleMaxCharacters {
		t.Fatalf("summarizer request purpose=%q max=%d", summarizer.purpose, summarizer.maxCharacters)
	}
}

func TestCreateTaskClipsSummarizedTitleToTaskPaneLimit(t *testing.T) {
	orch := newTestOrchestrator(t, nil)
	summarizer := &taskTitleSummaryStub{summary: strings.Repeat("title ", 40)}
	if err := orch.registry.Register(summarizer); err != nil {
		t.Fatal(err)
	}

	if _, err := orch.Handle(context.Background(), "test", "new make task titles fit in the task pane"); err != nil {
		t.Fatal(err)
	}
	tasks, err := orch.tasks.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 {
		t.Fatalf("task count = %d, want 1", len(tasks))
	}
	if got := len([]rune(tasks[0].Title)); got > taskTitleMaxCharacters {
		t.Fatalf("title length = %d, want <= %d: %q", got, taskTitleMaxCharacters, tasks[0].Title)
	}
}

func TestRemoteTaskUsesSummarizedTitle(t *testing.T) {
	orch := newTestOrchestrator(t, nil)
	summarizer := &taskTitleSummaryStub{summary: "Fix remote service"}
	if err := orch.registry.Register(summarizer); err != nil {
		t.Fatal(err)
	}
	store := remoteagent.NewStore(filepath.Join(t.TempDir(), "agents"))
	orch.WithRemoteAgents(store)
	if _, err := store.UpsertHeartbeat(remoteagent.Heartbeat{
		ID:      "workstation",
		Name:    "Workstation",
		Machine: "desk",
		Workdirs: []remoteagent.Workdir{{
			ID:    "repo",
			Path:  "/home/me/project",
			Label: "Project",
		}},
	}, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}

	if _, err := orch.CreateTaskWithTarget(context.Background(), "fix the remote service and update its startup docs", &taskstore.ExecutionTarget{
		Mode:      "remote",
		AgentID:   "workstation",
		WorkdirID: "repo",
		Backend:   "codex",
	}); err != nil {
		t.Fatal(err)
	}
	tasks, err := orch.tasks.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 || tasks[0].Title != "Fix remote service" {
		t.Fatalf("tasks = %#v, want summarized remote title", tasks)
	}
}

func TestRemoteTaskLifecycleUsesAgentClaimAndCompletion(t *testing.T) {
	orch := newTestOrchestrator(t, nil)
	store := remoteagent.NewStore(filepath.Join(t.TempDir(), "agents"))
	orch.WithRemoteAgents(store)
	agent, err := store.UpsertHeartbeat(remoteagent.Heartbeat{
		ID:      "workstation",
		Name:    "Workstation",
		Machine: "desk",
		Workdirs: []remoteagent.Workdir{{
			ID:    "repo",
			Path:  "/home/me/project",
			Label: "Project",
		}},
	}, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}

	reply, err := orch.CreateTaskWithTarget(context.Background(), "fix the remote service", &taskstore.ExecutionTarget{
		Mode:      "remote",
		AgentID:   "workstation",
		WorkdirID: "repo",
		Backend:   "codex",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(reply, "Created remote task") {
		t.Fatalf("reply = %q, want remote task creation", reply)
	}
	tasks, err := orch.tasks.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 {
		t.Fatalf("task count = %d, want 1 remote task", len(tasks))
	}
	task := tasks[0]
	if task.Target == nil || task.Target.AgentID != "workstation" || task.Target.Workdir != "/home/me/project" {
		t.Fatalf("target = %#v, want registered remote target", task.Target)
	}

	assignment, err := orch.ClaimRemoteTask(context.Background(), agent, "codex")
	if err != nil {
		t.Fatal(err)
	}
	if assignment == nil {
		t.Fatal("assignment is nil, want queued task")
	}
	if assignment.TaskID != task.ID || assignment.Workdir != "/home/me/project" {
		t.Fatalf("assignment = %#v, want task in remote workdir", assignment)
	}
	running, err := orch.tasks.Load(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if running.Status != taskstore.StatusRunning || running.AssignedTo != "workstation" {
		t.Fatalf("running task = %#v, want running assigned to remote agent", running)
	}

	if _, err := orch.CompleteRemoteTask(context.Background(), "workstation", task.ID, "changed files: service.go", "completed"); err != nil {
		t.Fatal(err)
	}
	completed, err := orch.tasks.Load(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if completed.Status != taskstore.StatusReadyForReview {
		t.Fatalf("status = %q, want ready_for_review", completed.Status)
	}
	if !strings.Contains(completed.Result, "changed files") {
		t.Fatalf("result = %q, want remote result", completed.Result)
	}

	review, err := orch.reviewTask(context.Background(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(review, "Merge approval requested") {
		t.Fatalf("review = %q, remote review must not request local merge approval", review)
	}
	if !strings.Contains(review, "No local workspace, main-branch comparison, or merge approval was attempted") {
		t.Fatalf("review = %q, want explicit remote review semantics", review)
	}
	verified, err := orch.tasks.Load(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if verified.Status != taskstore.StatusAwaitingVerification {
		t.Fatalf("status = %q, want awaiting_verification", verified.Status)
	}
}

func TestCreateRemoteTaskRequiresRegisteredAgentAndWorkdir(t *testing.T) {
	orch := newTestOrchestrator(t, nil)
	store := remoteagent.NewStore(filepath.Join(t.TempDir(), "agents"))
	orch.WithRemoteAgents(store)

	if _, err := orch.CreateTaskWithTarget(context.Background(), "bad remote task", &taskstore.ExecutionTarget{
		Mode:      "remote",
		AgentID:   "missing",
		WorkdirID: "repo",
	}); err == nil || !strings.Contains(err.Error(), "remote agent") {
		t.Fatalf("missing agent error = %v, want remote agent error", err)
	}

	if _, err := store.UpsertHeartbeat(remoteagent.Heartbeat{ID: "desk", Workdirs: []remoteagent.Workdir{}}, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if _, err := orch.CreateTaskWithTarget(context.Background(), "bad remote task", &taskstore.ExecutionTarget{
		Mode:      "remote",
		AgentID:   "desk",
		WorkdirID: "repo",
	}); err == nil || !strings.Contains(err.Error(), "remote working directory") {
		t.Fatalf("missing workdir error = %v, want remote working directory error", err)
	}

	if _, err := store.UpsertHeartbeat(remoteagent.Heartbeat{ID: "desk", Workdirs: []remoteagent.Workdir{{ID: "repo", Path: "/srv/desk/repo"}}}, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if _, err := orch.CreateTaskWithTarget(context.Background(), "bad remote task", &taskstore.ExecutionTarget{
		Mode:      "remote",
		AgentID:   "desk",
		WorkdirID: "wrong-repo",
	}); err == nil || !strings.Contains(err.Error(), "not advertised") {
		t.Fatalf("unknown workdir id error = %v, want advertised workdir error", err)
	}
	if _, err := orch.CreateTaskWithTarget(context.Background(), "bad remote task", &taskstore.ExecutionTarget{
		Mode:    "remote",
		AgentID: "desk",
		Workdir: "/tmp/wrong-repo",
	}); err == nil || !strings.Contains(err.Error(), "not advertised") {
		t.Fatalf("unknown workdir path error = %v, want advertised workdir error", err)
	}
	if _, err := orch.CreateTaskWithTarget(context.Background(), "bad remote task", &taskstore.ExecutionTarget{
		Mode:      "remote",
		AgentID:   "desk",
		WorkdirID: "repo",
		Workdir:   "/tmp/wrong-repo",
	}); err == nil || !strings.Contains(err.Error(), "does not match advertised path") {
		t.Fatalf("mismatched workdir error = %v, want mismatch error", err)
	}
}

func TestRemoteQueuedTaskIsSkippedByLocalTaskSupervisor(t *testing.T) {
	orch := newTestOrchestrator(t, nil)
	store := remoteagent.NewStore(filepath.Join(t.TempDir(), "agents"))
	orch.WithRemoteAgents(store)
	if _, err := store.UpsertHeartbeat(remoteagent.Heartbeat{
		ID:       "desk",
		Workdirs: []remoteagent.Workdir{{ID: "repo", Path: "/srv/desk/repo"}},
	}, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if _, err := orch.CreateTaskWithTarget(context.Background(), "remote task must not be locally started", &taskstore.ExecutionTarget{
		Mode:      "remote",
		AgentID:   "desk",
		WorkdirID: "repo",
	}); err != nil {
		t.Fatal(err)
	}
	tasks, err := orch.tasks.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 {
		t.Fatalf("task count = %d, want 1", len(tasks))
	}

	reconciled, err := orch.ReconcileTasks(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if reconciled != 0 {
		t.Fatalf("reconciled = %d, want 0 because remote task waits for remote claim", reconciled)
	}
	queued, err := orch.tasks.Load(tasks[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if queued.Status != taskstore.StatusQueued || queued.AssignedTo != "remote:desk" || queued.Workspace != "" {
		t.Fatalf("queued task = %#v, want untouched remote queue item", queued)
	}
}

func TestRemoteClaimOnlyReturnsTasksForMatchingAgent(t *testing.T) {
	orch := newTestOrchestrator(t, nil)
	store := remoteagent.NewStore(filepath.Join(t.TempDir(), "agents"))
	orch.WithRemoteAgents(store)
	desk, err := store.UpsertHeartbeat(remoteagent.Heartbeat{
		ID:       "desk",
		Workdirs: []remoteagent.Workdir{{ID: "repo", Path: "/srv/desk/repo"}},
	}, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	nuc, err := store.UpsertHeartbeat(remoteagent.Heartbeat{
		ID:       "nuc",
		Workdirs: []remoteagent.Workdir{{ID: "repo", Path: "/srv/nuc/repo"}},
	}, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := orch.CreateTaskWithTarget(context.Background(), "desk only", &taskstore.ExecutionTarget{
		Mode:      "remote",
		AgentID:   "desk",
		WorkdirID: "repo",
	}); err != nil {
		t.Fatal(err)
	}

	assignment, err := orch.ClaimRemoteTask(context.Background(), nuc, "codex")
	if err != nil {
		t.Fatal(err)
	}
	if assignment != nil {
		t.Fatalf("nuc claimed desk task: %#v", assignment)
	}
	assignment, err = orch.ClaimRemoteTask(context.Background(), desk, "codex")
	if err != nil {
		t.Fatal(err)
	}
	if assignment == nil || assignment.Workdir != "/srv/desk/repo" {
		t.Fatalf("desk assignment = %#v", assignment)
	}
}

func TestAcceptingGraphPhaseReleasesNextChild(t *testing.T) {
	orch := newTestOrchestrator(t, nil)
	seedTaskGraph(t, orch, "improve task graph execution")
	tasks, err := orch.tasks.List()
	if err != nil {
		t.Fatal(err)
	}
	inspect := taskByPhase(tasks, "inspect")
	design := taskByPhase(tasks, "design")
	if inspect.ID == "" || design.ID == "" {
		t.Fatalf("missing graph children in %#v", tasks)
	}
	reply, err := orch.acceptTask(context.Background(), inspect.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(reply, "Released 1 dependent graph phase") {
		t.Fatalf("reply = %q, want released child note", reply)
	}
	updatedDesign, err := orch.tasks.Load(design.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updatedDesign.Status != taskstore.StatusQueued || len(updatedDesign.BlockedBy) != 0 {
		t.Fatalf("design task = %#v, want queued with no blockers", updatedDesign)
	}
	updatedInspect, err := orch.tasks.Load(inspect.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, criterion := range updatedInspect.AcceptanceCriteria {
		if criterion.Status != "accepted" {
			t.Fatalf("criterion = %#v, want accepted", criterion)
		}
	}
}

func TestGraphParentCompletesAfterAllChildrenAccepted(t *testing.T) {
	orch := newTestOrchestrator(t, nil)
	seedTaskGraph(t, orch, "build graph parent completion")
	tasks, err := orch.tasks.List()
	if err != nil {
		t.Fatal(err)
	}
	parent := taskByPhase(tasks, "root")
	var children []taskstore.Task
	for _, task := range tasks {
		if task.ParentID == parent.ID {
			task.Status = taskstore.StatusDone
			task.AcceptanceCriteria = markAcceptanceCriteria(task.AcceptanceCriteria, "accepted")
			if err := orch.tasks.Save(task); err != nil {
				t.Fatal(err)
			}
			children = append(children, task)
		}
	}
	if len(children) != 6 {
		t.Fatalf("child count = %d, want 6", len(children))
	}
	ok, err := orch.completeGraphParentIfReady(context.Background(), parent.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("completeGraphParentIfReady returned false, want true")
	}
	updatedParent, err := orch.tasks.Load(parent.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updatedParent.Status != taskstore.StatusDone {
		t.Fatalf("parent status = %q, want done", updatedParent.Status)
	}
}

func TestBlockedGraphChildCannotDelegateUntilDependencyAccepted(t *testing.T) {
	orch := newTestOrchestrator(t, nil)
	seedTaskGraph(t, orch, "enforce graph dependencies")
	tasks, err := orch.tasks.List()
	if err != nil {
		t.Fatal(err)
	}
	design := taskByPhase(tasks, "design")
	if design.ID == "" {
		t.Fatalf("design task not found in %#v", tasks)
	}
	design.Workspace = filepath.Join(t.TempDir(), "workspace")
	if err := orch.tasks.Save(design); err != nil {
		t.Fatal(err)
	}
	err = orch.startDelegationForTask(context.Background(), design.ID, "codex", "start early")
	if err == nil || !strings.Contains(err.Error(), "blocked by graph dependencies") {
		t.Fatalf("err = %v, want blocked by graph dependencies", err)
	}
	updated, err := orch.tasks.Load(design.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != taskstore.StatusBlocked || len(updated.BlockedBy) == 0 {
		t.Fatalf("task = %#v, want blocked with blockers", updated)
	}
}

func TestDelegationCreatesReviewedPlanBeforeExecution(t *testing.T) {
	orch := newTestOrchestrator(t, nil)
	writeTestRepoFile(t, orch.cfg.Repo.Root, "pkg/agent/orchestrator.go", "func ensureTaskPlan() {}\nfunc defaultTaskPlan() {}\n")
	writeTestRepoFile(t, orch.cfg.Repo.Root, "pkg/agent/orchestrator_test.go", "func TestTaskPlan(t *testing.T) {}\n")
	writeTestRepoFile(t, orch.cfg.Repo.Root, "docs/task-workflow.md", "## Planning Gate\nTaskPlan records explain task plans.\n")
	task := taskstore.Task{
		ID:         "task_20260426_220000_deadbeef",
		Title:      "add planner",
		Goal:       "add repo-aware task planner",
		Status:     taskstore.StatusQueued,
		AssignedTo: "OrchestratorAgent",
		CreatedAt:  time.Now().UTC(),
		UpdatedAt:  time.Now().UTC(),
		Workspace:  filepath.Join(t.TempDir(), "workspace"),
	}
	if err := orch.tasks.Save(task); err != nil {
		t.Fatal(err)
	}

	run, err := orch.prepareDelegationForTask(context.Background(), task.ID, "codex", "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(run.Instruction, "Reviewed task plan:") || !strings.Contains(run.Instruction, "Inspect repo-grounded scope") {
		t.Fatalf("instruction = %q, want reviewed plan context", run.Instruction)
	}
	for _, want := range []string{"pkg/agent/orchestrator.go", "pkg/agent/orchestrator_test.go", "docs/task-workflow.md", "go test ./pkg/agent"} {
		if !strings.Contains(run.Instruction, want) {
			t.Fatalf("instruction = %q, want repo-aware plan detail %q", run.Instruction, want)
		}
	}
	updated, err := orch.tasks.Load(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != taskstore.StatusRunning {
		t.Fatalf("status = %q, want running", updated.Status)
	}
	if updated.Plan == nil || updated.Plan.Status != "reviewed" {
		t.Fatalf("plan = %#v, want reviewed plan", updated.Plan)
	}
	if updated.Plan.Review != repoAwareTaskPlanReview {
		t.Fatalf("review = %q, want repo-aware review", updated.Plan.Review)
	}
}

func TestDelegationRefreshesLegacyGenericPlanBeforeExecution(t *testing.T) {
	orch := newTestOrchestrator(t, nil)
	now := time.Now().UTC()
	reviewedAt := now
	task := taskstore.Task{
		ID:         "task_20260427_001343_deadbeef",
		Title:      "inspect plans",
		Goal:       "Inspect the repository and current task context for: all task plans look generic",
		Status:     taskstore.StatusQueued,
		AssignedTo: "OrchestratorAgent",
		CreatedAt:  now,
		UpdatedAt:  now,
		Workspace:  filepath.Join(t.TempDir(), "workspace"),
		GraphPhase: "inspect",
		Plan: &taskstore.TaskPlan{
			Status:     "reviewed",
			Summary:    "Plan to satisfy: all task plans look generic",
			Steps:      []taskstore.TaskPlanStep{{Title: "Inspect scope"}},
			Review:     legacyDefaultTaskPlanReview,
			CreatedAt:  now,
			ReviewedAt: &reviewedAt,
		},
	}
	if err := orch.tasks.Save(task); err != nil {
		t.Fatal(err)
	}

	run, err := orch.prepareDelegationForTask(context.Background(), task.ID, "codex", "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(run.Instruction, "Map affected surface") {
		t.Fatalf("instruction = %q, want refreshed phase-specific plan", run.Instruction)
	}
	updated, err := orch.tasks.Load(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Plan == nil || updated.Plan.Review != repoAwareTaskPlanReview {
		t.Fatalf("plan = %#v, want refreshed task-specific plan", updated.Plan)
	}
	if !strings.Contains(updated.Plan.Summary, "inspect phase") {
		t.Fatalf("summary = %q, want inspect phase", updated.Plan.Summary)
	}
}

func TestDelegationRefreshesLegacyTaskSpecificPlanBeforeExecution(t *testing.T) {
	orch := newTestOrchestrator(t, nil)
	writeTestRepoFile(t, orch.cfg.Repo.Root, "pkg/agent/orchestrator.go", "func ensureTaskPlan() {}\nfunc taskPlanSteps() {}\n")
	now := time.Now().UTC()
	reviewedAt := now
	task := taskstore.Task{
		ID:         "task_20260427_001344_deadbeef",
		Title:      "smarter task plans",
		Goal:       "make task plans inspect the repo first",
		Status:     taskstore.StatusQueued,
		AssignedTo: "OrchestratorAgent",
		CreatedAt:  now,
		UpdatedAt:  now,
		Workspace:  filepath.Join(t.TempDir(), "workspace"),
		Plan: &taskstore.TaskPlan{
			Status:     "reviewed",
			Summary:    "Plan to satisfy: make task plans inspect the repo first",
			Steps:      []taskstore.TaskPlanStep{{Title: "Inspect scope"}},
			Review:     legacyTaskSpecificDefaultPlanReview,
			CreatedAt:  now,
			ReviewedAt: &reviewedAt,
		},
	}
	if err := orch.tasks.Save(task); err != nil {
		t.Fatal(err)
	}

	run, err := orch.prepareDelegationForTask(context.Background(), task.ID, "codex", "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(run.Instruction, "pkg/agent/orchestrator.go") {
		t.Fatalf("instruction = %q, want refreshed repo-aware plan", run.Instruction)
	}
	updated, err := orch.tasks.Load(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Plan == nil || updated.Plan.Review != repoAwareTaskPlanReview {
		t.Fatalf("plan = %#v, want repo-aware plan", updated.Plan)
	}
}

func TestTaskColonCreationDoesNotListInFlight(t *testing.T) {
	orch := newTestOrchestrator(t, nil)
	now := time.Now().UTC()
	if err := orch.tasks.Save(taskstore.Task{
		ID:         "task_20260426_014212_653ddcbc",
		Title:      "existing running task",
		Goal:       "existing running task",
		Status:     taskstore.StatusRunning,
		AssignedTo: "codex",
		CreatedAt:  now,
		UpdatedAt:  now,
	}); err != nil {
		t.Fatal(err)
	}

	goal := "Add Playwright end-to-end tests for the chat and task components covering queue filters and active tasks"
	reply, err := orch.Handle(context.Background(), "test", "Task: "+goal)
	if err != nil {
		t.Fatal(err)
	}
	if strings.HasPrefix(reply, "In flight:") {
		t.Fatalf("reply = %q, want task creation not status", reply)
	}
	if !strings.Contains(reply, "Created queued task") {
		t.Fatalf("reply = %q, want created queued task", reply)
	}
	tasks, err := orch.tasks.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 2 {
		t.Fatalf("task count = %d, want existing task plus new queued task", len(tasks))
	}
	var created taskstore.Task
	for _, task := range tasks {
		if task.Goal == goal {
			created = task
			break
		}
	}
	if created.ID == "" {
		t.Fatalf("created task with goal %q not found in %#v", goal, tasks)
	}
	if created.Status != taskstore.StatusQueued || created.GraphPhase != "" || created.ParentID != "" {
		t.Fatalf("created task = %#v, want standalone queued task", created)
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
	if !strings.Contains(updated.Result, "go.test: go test failed") {
		t.Fatalf("result = %q, want failing check name", updated.Result)
	}
}

func TestReviewRerunsBlockedReviewCheckFailure(t *testing.T) {
	orch := newTestOrchestrator(t, nil)
	if err := orch.registry.Register(currentDiffStub{}); err != nil {
		t.Fatal(err)
	}
	workspace := filepath.Join(t.TempDir(), "workspace")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatal(err)
	}
	task := taskstore.Task{
		ID:         "task_20260428_214200_recheck1",
		Title:      "recheck review infra",
		Goal:       "recheck review infra",
		Status:     taskstore.StatusBlocked,
		AssignedTo: "OrchestratorAgent",
		Result:     "ReviewerAgent checks failed: bun.uat.site: bun: exit status 1",
		CreatedAt:  time.Now().UTC(),
		UpdatedAt:  time.Now().UTC(),
		Workspace:  workspace,
	}
	if err := orch.tasks.Save(task); err != nil {
		t.Fatal(err)
	}

	reply, err := orch.reviewTask(context.Background(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(reply, "Merge approval requested") {
		t.Fatalf("reply = %q, want blocked check failure to be reviewed again", reply)
	}
	updated, err := orch.tasks.Load(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != taskstore.StatusAwaitingApproval {
		t.Fatalf("status = %q, want awaiting approval", updated.Status)
	}
	approvals, err := orch.approvals.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(approvals) != 1 || approvals[0].TaskID != task.ID {
		t.Fatalf("approvals = %#v, want one approval for rechecked task", approvals)
	}
}

func TestReviewRunningTaskDoesNotRunChecksOrBlock(t *testing.T) {
	orch := newTestOrchestrator(t, nil)
	taskID := "task_20260428_204200_running1"
	task := taskstore.Task{
		ID:         taskID,
		Title:      "running worker",
		Goal:       "running worker",
		Status:     taskstore.StatusRunning,
		AssignedTo: "codex",
		Result:     "delegated to codex; external worker is running",
		CreatedAt:  time.Now().UTC(),
		UpdatedAt:  time.Now().UTC(),
		Workspace:  filepath.Join(t.TempDir(), "workspace"),
	}
	if err := orch.tasks.Save(task); err != nil {
		t.Fatal(err)
	}

	reply, err := orch.reviewTask(context.Background(), taskID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(reply, "still running") || strings.Contains(reply, "Checks:") {
		t.Fatalf("reply = %q, want running no-op review", reply)
	}
	updated, err := orch.tasks.Load(taskID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != taskstore.StatusRunning || updated.Result != task.Result {
		t.Fatalf("task = %#v, want unchanged running task", updated)
	}
}

func TestReviewIgnoresResultWhenTaskChangesDuringChecks(t *testing.T) {
	orch := newTestOrchestrator(t, nil)
	if err := orch.registry.Register(currentDiffStub{}); err != nil {
		t.Fatal(err)
	}
	taskID := "task_20260428_204201_raced001"
	if err := orch.registry.Register(statusChangingGoTestStub{orch: orch, taskID: taskID}); err != nil {
		t.Fatal(err)
	}
	workspace := filepath.Join(t.TempDir(), "workspace")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "go.mod"), []byte("module reviewrace\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	task := taskstore.Task{
		ID:         taskID,
		Title:      "review race",
		Goal:       "review race",
		Status:     taskstore.StatusReadyForReview,
		AssignedTo: "OrchestratorAgent",
		CreatedAt:  time.Now().UTC(),
		UpdatedAt:  time.Now().UTC(),
		Workspace:  workspace,
	}
	if err := orch.tasks.Save(task); err != nil {
		t.Fatal(err)
	}

	reply, err := orch.reviewTask(context.Background(), taskID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(reply, "ignored because the task changed to running") {
		t.Fatalf("reply = %q, want stale review ignored", reply)
	}
	updated, err := orch.tasks.Load(taskID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != taskstore.StatusRunning || updated.AssignedTo != "codex" || updated.Result != "worker restarted while review was running" {
		t.Fatalf("task = %#v, want worker state preserved", updated)
	}
}

func TestProjectCheckFailureKeepsFailingToolTail(t *testing.T) {
	orch := newTestOrchestrator(t, nil)
	if err := orch.registry.Register(longGoTestFailStub{}); err != nil {
		t.Fatal(err)
	}
	workspace := filepath.Join(t.TempDir(), "workspace")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "go.mod"), []byte("module longfail\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := orch.runProjectChecks(context.Background(), "task_longfail", workspace, "ReviewerAgent", "")
	if err == nil || !strings.Contains(err.Error(), "go.test: go test failed") {
		t.Fatalf("err = %v, want named go.test failure", err)
	}
	if !strings.Contains(out, "FINAL FAILURE LINE") || !strings.Contains(out, "truncated") {
		t.Fatalf("output = %q, want failing tail preserved", out)
	}
}

func TestReviewPremergeFailureBlocksForSupervisorRecovery(t *testing.T) {
	delegateStarted := make(chan struct{}, 1)
	releaseDelegate := make(chan struct{})
	defer close(releaseDelegate)
	orch := newTestOrchestrator(t, &delegateStub{
		started: delegateStarted,
		release: releaseDelegate,
	})
	if err := orch.registry.Register(currentDiffStub{}); err != nil {
		t.Fatal(err)
	}
	if err := orch.registry.Register(mergeCheckFailStub{}); err != nil {
		t.Fatal(err)
	}
	task := taskstore.Task{
		ID:         "task_20260426_012457_899298ac",
		Title:      "restart touched components",
		Goal:       "restart touched components",
		Status:     taskstore.StatusReadyForReview,
		AssignedTo: "OrchestratorAgent",
		CreatedAt:  time.Now().UTC(),
		UpdatedAt:  time.Now().UTC(),
		Workspace:  filepath.Join(t.TempDir(), "workspace"),
	}
	if err := orch.tasks.Save(task); err != nil {
		t.Fatal(err)
	}

	reply, err := orch.reviewTask(context.Background(), "899298ac")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(reply, "ready_for_review -> blocked") || !strings.Contains(reply, "task supervisor will queue automatic recovery") {
		t.Fatalf("reply = %q, want explicit blocked transition with supervisor recovery", reply)
	}
	select {
	case <-delegateStarted:
		t.Fatal("review premerge failure should be recovered by supervisor, not inline review")
	default:
	}
	updated, err := orch.tasks.Load(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != taskstore.StatusBlocked || updated.AssignedTo != "OrchestratorAgent" {
		t.Fatalf("task = %#v, want blocked OrchestratorAgent", updated)
	}
	if !strings.Contains(updated.Result, "premerge check failed") {
		t.Fatalf("result = %q, want premerge failure reason", updated.Result)
	}
}

func TestReviewSuccessMovesOwnershipBackToOrchestrator(t *testing.T) {
	orch := newTestOrchestrator(t, nil)
	if err := orch.registry.Register(currentDiffStub{}); err != nil {
		t.Fatal(err)
	}
	if err := orch.registry.Register(mergeCheckPassStub{}); err != nil {
		t.Fatal(err)
	}
	task := taskstore.Task{
		ID:         "task_20260426_012457_899298ac",
		Title:      "restart touched components",
		Goal:       "restart touched components",
		Status:     taskstore.StatusReadyForReview,
		AssignedTo: "codex",
		CreatedAt:  time.Now().UTC(),
		UpdatedAt:  time.Now().UTC(),
		Workspace:  filepath.Join(t.TempDir(), "workspace"),
	}
	if err := orch.tasks.Save(task); err != nil {
		t.Fatal(err)
	}

	reply, err := orch.reviewTask(context.Background(), "899298ac")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(reply, "Merge approval requested") {
		t.Fatalf("reply = %q, want merge approval", reply)
	}
	updated, err := orch.tasks.Load(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != taskstore.StatusAwaitingApproval {
		t.Fatalf("status = %q, want awaiting_approval", updated.Status)
	}
	if updated.AssignedTo != "OrchestratorAgent" {
		t.Fatalf("AssignedTo = %q, want OrchestratorAgent", updated.AssignedTo)
	}
}

func TestReviewSuccessStalesPreviousPendingMergeApproval(t *testing.T) {
	orch := newTestOrchestrator(t, nil)
	if err := orch.registry.Register(currentDiffStub{}); err != nil {
		t.Fatal(err)
	}
	if err := orch.registry.Register(mergeCheckPassStub{}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	task := taskstore.Task{
		ID:         "task_20260427_120000_6d41996e",
		Title:      "retry stale approval",
		Goal:       "retry stale approval",
		Status:     taskstore.StatusReadyForReview,
		AssignedTo: "codex",
		CreatedAt:  now,
		UpdatedAt:  now,
		Workspace:  filepath.Join(t.TempDir(), "workspace"),
	}
	if err := orch.tasks.Save(task); err != nil {
		t.Fatal(err)
	}
	oldApproval := approvalstore.Request{
		ID:        "approval_20260427_120000_old00001",
		TaskID:    task.ID,
		Tool:      "git.merge_approved",
		Args:      json.RawMessage(`{"branch":"homelabd/task_20260427_120000_6d41996e"}`),
		Reason:    "merge reviewed task branch into repo root",
		Status:    approvalstore.StatusPending,
		CreatedAt: now.Add(-time.Minute),
	}
	if err := orch.approvals.Save(oldApproval); err != nil {
		t.Fatal(err)
	}

	reply, err := orch.reviewTask(context.Background(), "6d41996e")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(reply, "Merge approval requested") {
		t.Fatalf("reply = %q, want merge approval", reply)
	}
	updatedOldApproval, err := orch.approvals.Load(oldApproval.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updatedOldApproval.Status != approvalstore.StatusStale {
		t.Fatalf("old approval status = %q, want stale", updatedOldApproval.Status)
	}
	approvals, err := orch.approvals.List()
	if err != nil {
		t.Fatal(err)
	}
	pending := 0
	for _, approval := range approvals {
		if approval.TaskID == task.ID && approval.Status == approvalstore.StatusPending {
			pending++
		}
	}
	if pending != 1 {
		t.Fatalf("pending approvals = %d, want exactly the new review approval", pending)
	}
}

func TestReviewWorkspaceCommitFeedsBranchDiff(t *testing.T) {
	ctx := context.Background()
	tempDir := t.TempDir()
	root := filepath.Join(tempDir, "repo")
	workspace := filepath.Join(tempDir, "workspace")
	gitTestRun(t, "", "init", "--initial-branch=main", root)
	gitTestRun(t, root, "config", "user.email", "test@example.com")
	gitTestRun(t, root, "config", "user.name", "Test User")
	if err := os.WriteFile(filepath.Join(root, "app.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitTestRun(t, root, "add", "app.txt")
	gitTestRun(t, root, "commit", "-m", "base")
	gitTestRun(t, root, "worktree", "add", "-b", "homelabd/task_test", workspace)
	if err := os.WriteFile(filepath.Join(workspace, "app.txt"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(workspace, ".git-local"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, ".git-local", "config"), []byte("metadata\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := commitReviewWorkspaceChanges(ctx, workspace, "task_test"); err != nil {
		t.Fatal(err)
	}
	tree := gitTestOutput(t, workspace, "ls-tree", "-r", "--name-only", "HEAD")
	if strings.Contains(tree, ".git-local") {
		t.Fatalf("committed tree = %q, should not include workspace metadata", tree)
	}
	orch := newTestOrchestrator(t, nil)
	orch.cfg.Repo.Root = root
	diff, err := orch.taskBranchDiff(ctx, workspace)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(diff, "+changed") {
		t.Fatalf("diff = %q, want committed workspace change", diff)
	}
}

func TestNaturalDiffQuestionUsesProgramDiffSummary(t *testing.T) {
	orch := newTestOrchestrator(t, nil)
	if err := orch.registry.Register(currentDiffStub{}); err != nil {
		t.Fatal(err)
	}
	task := taskstore.Task{
		ID:         "task_20260426_204322_c01777ee",
		Title:      "change main",
		Goal:       "change main",
		Status:     taskstore.StatusConflictResolution,
		AssignedTo: "codex",
		Workspace:  filepath.Join(t.TempDir(), "workspace"),
		CreatedAt:  time.Now().UTC(),
		UpdatedAt:  time.Now().UTC(),
	}
	if err := orch.tasks.Save(task); err != nil {
		t.Fatal(err)
	}

	result, err := orch.HandleDetailed(context.Background(), "dashboard", "what is the diff between c01777ee and main?")
	if err != nil {
		t.Fatal(err)
	}
	if result.Source != "program" {
		t.Fatalf("source = %q, want program", result.Source)
	}
	if !strings.Contains(result.Reply, "Diff for c01777ee") ||
		!strings.Contains(result.Reply, "main.go") ||
		!strings.Contains(result.Reply, "homelabctl task diff c01777ee") {
		t.Fatalf("reply = %q, want diff summary and CLI hint", result.Reply)
	}
}

func TestNaturalDiffQuestionForRemoteTaskExplainsRemoteCheckout(t *testing.T) {
	orch := newTestOrchestrator(t, nil)
	task := taskstore.Task{
		ID:         "task_20260426_204322_c01777ee",
		Title:      "change remote",
		Goal:       "change remote",
		Status:     taskstore.StatusReadyForReview,
		AssignedTo: "desk",
		Target: &taskstore.ExecutionTarget{
			Mode:    "remote",
			AgentID: "desk",
			Workdir: "/srv/repo",
		},
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	if err := orch.tasks.Save(task); err != nil {
		t.Fatal(err)
	}

	result, err := orch.HandleDetailed(context.Background(), "dashboard", "what is the diff between c01777ee and main?")
	if err != nil {
		t.Fatal(err)
	}
	if result.Source != "program" {
		t.Fatalf("source = %q, want program", result.Source)
	}
	if !strings.Contains(result.Reply, "Remote task diffs are not read from homelabd's repo") {
		t.Fatalf("reply = %q, want remote diff guidance", result.Reply)
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

func TestApprovedMergeEnforcesRestartBeforeVerification(t *testing.T) {
	orch := newTestOrchestrator(t, nil)
	if err := orch.registry.Register(mergeApprovedStub{}); err != nil {
		t.Fatal(err)
	}
	supervisor, calls := newRestartGateSupervisor(t, false)
	defer supervisor.Close()
	orch.cfg.Supervisord.Addr = supervisor.URL
	setSupervisorAppHealthURL(t, &orch.cfg, "dashboard", supervisor.URL+"/health/dashboard")

	task := taskstore.Task{
		ID:         "task_20260429_083000_abcd1234",
		Title:      "fix dashboard SSR",
		Goal:       "fix dashboard SSR",
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
		ID:     "approval_20260429_083000_restart",
		TaskID: task.ID,
		Tool:   "git.merge_approved",
		Args:   json.RawMessage(`{"branch":"homelabd/task_20260429_083000_abcd1234","restart_required":["dashboard"]}`),
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
	if !strings.Contains(reply, "enforcing post-merge restarts before verification") {
		t.Fatalf("reply = %q, want restart gate guidance", reply)
	}
	awaiting, err := orch.tasks.Load(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if awaiting.Status != taskstore.StatusAwaitingRestart || awaiting.RestartStatus != taskstore.RestartStatusPending {
		t.Fatalf("task after approval = %#v, want awaiting restart pending", awaiting)
	}
	blockedReply, err := orch.acceptTask(context.Background(), "abcd1234")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(blockedReply, "still enforcing post-merge restarts") {
		t.Fatalf("accept reply = %q, want restart gate block", blockedReply)
	}

	verified := waitForTask(t, orch, task.ID, func(item taskstore.Task) bool {
		return item.Status == taskstore.StatusAwaitingVerification && item.RestartStatus == taskstore.RestartStatusComplete
	})
	if !containsString(verified.RestartCompleted, "dashboard") {
		t.Fatalf("restart_completed = %#v, want dashboard", verified.RestartCompleted)
	}
	if got := calls.snapshot(); !containsString(got, "POST /supervisord/apps/dashboard/restart") {
		t.Fatalf("supervisor calls = %#v, want dashboard restart", got)
	}
}

func TestPostMergeRestartFailureKeepsTaskAwaitingRestart(t *testing.T) {
	orch := newTestOrchestrator(t, nil)
	supervisor, _ := newRestartGateSupervisor(t, true)
	defer supervisor.Close()
	orch.cfg.Supervisord.Addr = supervisor.URL
	setSupervisorAppHealthURL(t, &orch.cfg, "dashboard", supervisor.URL+"/health/dashboard")

	task := taskstore.Task{
		ID:              "task_20260429_090000_deadbeef",
		Title:           "restart fails",
		Goal:            "restart fails",
		Status:          taskstore.StatusAwaitingRestart,
		AssignedTo:      "OrchestratorAgent",
		RestartRequired: []string{"dashboard"},
		RestartStatus:   taskstore.RestartStatusPending,
		CreatedAt:       time.Now().UTC(),
		UpdatedAt:       time.Now().UTC(),
	}
	if err := orch.tasks.Save(task); err != nil {
		t.Fatal(err)
	}

	err := orch.continuePostMergeRestart(context.Background(), task.ID)
	if err == nil {
		t.Fatal("restart gate succeeded, want failure")
	}
	updated, loadErr := orch.tasks.Load(task.ID)
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if updated.Status != taskstore.StatusAwaitingRestart || updated.RestartStatus != taskstore.RestartStatusFailed || updated.RestartCurrent != "dashboard" {
		t.Fatalf("task = %#v, want awaiting_restart failed on dashboard", updated)
	}
	if !strings.Contains(updated.RestartLastError, "500") {
		t.Fatalf("restart_last_error = %q, want HTTP 500 detail", updated.RestartLastError)
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
	if !strings.Contains(reply, "Approval approval_20260425_214114_a300d8be failed while merging task 28493611") {
		t.Fatalf("reply = %q, want approval failure explanation", reply)
	}
	if !strings.Contains(reply, "Task moved to blocked") {
		t.Fatalf("reply = %q, want blocked transition explanation", reply)
	}
	updatedApproval, err := orch.approvals.Load(req.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updatedApproval.Status != approvalstore.StatusFailed {
		t.Fatalf("approval status = %q, want failed", updatedApproval.Status)
	}
	updatedTask, err := orch.tasks.Load(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updatedTask.Status != taskstore.StatusBlocked {
		t.Fatalf("task status = %q, want blocked", updatedTask.Status)
	}
	if !strings.Contains(updatedTask.Result, "Approved merge failed") {
		t.Fatalf("task result = %q, want merge failure context", updatedTask.Result)
	}
}

func TestApprovalAutoReconcilesTaskBranchWithCurrentMain(t *testing.T) {
	orch := newTestOrchestrator(t, nil)
	tempDir := t.TempDir()
	root := filepath.Join(tempDir, "repo")
	workspace := filepath.Join(tempDir, "workspaces", "task_20260426_184630_f03ee9d3")
	orch.cfg.Repo.Root = root
	orch.cfg.Repo.WorkspaceRoot = filepath.Join(tempDir, "workspaces")
	gitTestRun(t, "", "init", "--initial-branch=main", root)
	gitTestRun(t, root, "config", "user.email", "test@example.com")
	gitTestRun(t, root, "config", "user.name", "Test User")
	if err := os.WriteFile(filepath.Join(root, "app.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitTestRun(t, root, "add", ".")
	gitTestRun(t, root, "commit", "-m", "base")
	gitTestRun(t, root, "worktree", "add", "-b", "homelabd/task_20260426_184630_f03ee9d3", workspace)
	if err := os.WriteFile(filepath.Join(workspace, "task.txt"), []byte("task\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitTestRun(t, workspace, "add", ".")
	gitTestRun(t, workspace, "commit", "-m", "task change")
	if err := os.WriteFile(filepath.Join(root, "main.txt"), []byte("main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitTestRun(t, root, "add", ".")
	gitTestRun(t, root, "commit", "-m", "main change")
	if err := orch.registry.Register(mergeObservesWorkspaceStub{workspace: workspace, path: "main.txt"}); err != nil {
		t.Fatal(err)
	}
	task := taskstore.Task{
		ID:         "task_20260426_184630_f03ee9d3",
		Title:      "approve with fresh main",
		Goal:       "approve with fresh main",
		Status:     taskstore.StatusAwaitingApproval,
		AssignedTo: "OrchestratorAgent",
		Workspace:  workspace,
		CreatedAt:  time.Now().UTC(),
		UpdatedAt:  time.Now().UTC(),
	}
	if err := orch.tasks.Save(task); err != nil {
		t.Fatal(err)
	}
	req := approvalstore.Request{
		ID:     "approval_20260426_184630_c0ffee00",
		TaskID: task.ID,
		Tool:   "git.merge_approved",
		Args:   json.RawMessage(`{"branch":"homelabd/task_20260426_184630_f03ee9d3","target":"` + root + `","workspace":"` + workspace + `"}`),
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
	if !strings.Contains(reply, "Approved and merged") {
		t.Fatalf("reply = %q, want successful approval", reply)
	}
	if got := readTestFile(t, filepath.Join(workspace, "main.txt")); got != "main\n" {
		t.Fatalf("main.txt = %q, want main content reconciled into task branch", got)
	}
	status := gitTestOutput(t, workspace, "status", "--porcelain")
	if strings.TrimSpace(status) != "" {
		t.Fatalf("workspace status = %q, want clean reconciled branch", status)
	}
	updated, err := orch.tasks.Load(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != taskstore.StatusAwaitingVerification {
		t.Fatalf("status = %q, want awaiting_verification", updated.Status)
	}
}

func TestApprovalAutoRebaseConflictMovesToConflictResolution(t *testing.T) {
	orch := newTestOrchestrator(t, nil)
	orch.cfg.ExternalAgents = nil
	tempDir := t.TempDir()
	root := filepath.Join(tempDir, "repo")
	workspace := filepath.Join(tempDir, "workspaces", "task_20260426_184630_badf00d")
	orch.cfg.Repo.Root = root
	orch.cfg.Repo.WorkspaceRoot = filepath.Join(tempDir, "workspaces")
	gitTestRun(t, "", "init", "--initial-branch=main", root)
	gitTestRun(t, root, "config", "user.email", "test@example.com")
	gitTestRun(t, root, "config", "user.name", "Test User")
	if err := os.WriteFile(filepath.Join(root, "app.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitTestRun(t, root, "add", ".")
	gitTestRun(t, root, "commit", "-m", "base")
	gitTestRun(t, root, "worktree", "add", "-b", "homelabd/task_20260426_184630_badf00d", workspace)
	if err := os.WriteFile(filepath.Join(workspace, "app.txt"), []byte("task\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitTestRun(t, workspace, "commit", "-am", "task conflict")
	if err := os.WriteFile(filepath.Join(root, "app.txt"), []byte("main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitTestRun(t, root, "commit", "-am", "main conflict")
	if err := orch.registry.Register(mergeShouldNotRunStub{}); err != nil {
		t.Fatal(err)
	}
	task := taskstore.Task{
		ID:         "task_20260426_184630_badf00d",
		Title:      "approve conflict",
		Goal:       "approve conflict",
		Status:     taskstore.StatusAwaitingApproval,
		AssignedTo: "OrchestratorAgent",
		Workspace:  workspace,
		CreatedAt:  time.Now().UTC(),
		UpdatedAt:  time.Now().UTC(),
	}
	if err := orch.tasks.Save(task); err != nil {
		t.Fatal(err)
	}
	req := approvalstore.Request{
		ID:     "approval_20260426_184630_badf00d0",
		TaskID: task.ID,
		Tool:   "git.merge_approved",
		Args:   json.RawMessage(`{"branch":"homelabd/task_20260426_184630_badf00d","target":"` + root + `","workspace":"` + workspace + `"}`),
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
	if !strings.Contains(reply, "could not auto-rebase") || !strings.Contains(reply, taskstore.StatusConflictResolution) || !strings.Contains(reply, "Automatic conflict recovery could not start yet") {
		t.Fatalf("reply = %q, want auto-rebase conflict guidance", reply)
	}
	updatedApproval, err := orch.approvals.Load(req.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updatedApproval.Status != approvalstore.StatusFailed {
		t.Fatalf("approval status = %q, want failed", updatedApproval.Status)
	}
	updatedTask, err := orch.tasks.Load(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updatedTask.Status != taskstore.StatusConflictResolution {
		t.Fatalf("task status = %q, want conflict_resolution", updatedTask.Status)
	}
	status := gitTestOutput(t, workspace, "status", "--porcelain")
	if strings.TrimSpace(status) != "" {
		t.Fatalf("workspace status = %q, want merge aborted and clean", status)
	}
}

func TestFailedMergeApprovalQueuesAutomaticRecovery(t *testing.T) {
	delegateStarted := make(chan struct{}, 1)
	releaseDelegate := make(chan struct{})
	orch := newTestOrchestrator(t, &delegateStub{
		started: delegateStarted,
		release: releaseDelegate,
	})
	orch.cfg.ExternalAgents = map[string]config.ExternalAgentConfig{"codex": {Enabled: true, Command: "codex"}}
	workspace := filepath.Join(orch.cfg.Repo.WorkspaceRoot, "task_20260428_200514_0d62653b")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatal(err)
	}
	task := taskstore.Task{
		ID:         "task_20260428_200514_0d62653b",
		Title:      "pwa",
		Goal:       "pwa",
		Status:     taskstore.StatusAwaitingApproval,
		AssignedTo: "OrchestratorAgent",
		Workspace:  workspace,
		Result:     "Approval auto-rebase failed; automatic conflict recovery required: merge conflict",
		CreatedAt:  time.Now().UTC(),
		UpdatedAt:  time.Now().UTC(),
	}
	if err := orch.tasks.Save(task); err != nil {
		t.Fatal(err)
	}
	req := approvalstore.Request{
		ID:     "approval_20260428_221022_241942df",
		TaskID: task.ID,
		Tool:   "git.merge_approved",
		Args:   json.RawMessage(`{"branch":"homelabd/task_20260428_200514_0d62653b"}`),
		Reason: "merge reviewed task branch into repo root; auto-rebase failed: merge conflict",
		Status: approvalstore.StatusFailed,
	}
	if err := orch.approvals.Save(req); err != nil {
		t.Fatal(err)
	}

	reply, err := orch.resolveApproval(context.Background(), req.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(reply, "already failed") || !strings.Contains(reply, "automatic conflict recovery") {
		t.Fatalf("reply = %q, want failed approval recovery guidance", reply)
	}
	select {
	case <-delegateStarted:
	case <-time.After(time.Second):
		t.Fatal("automatic recovery worker did not start")
	}
	updated, err := orch.tasks.Load(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != taskstore.StatusRunning || updated.AssignedTo != "codex" || updated.AutoRecoveryAttempts != 1 {
		t.Fatalf("task = %#v, want running codex recovery attempt", updated)
	}
	close(releaseDelegate)
	waitForDelegationReviewEvent(t, orch, task.ID)
}

func TestReconcileConflictResolutionQueuesAutomaticRecovery(t *testing.T) {
	delegateStarted := make(chan struct{}, 1)
	releaseDelegate := make(chan struct{})
	orch := newTestOrchestrator(t, &delegateStub{
		started: delegateStarted,
		release: releaseDelegate,
	})
	orch.cfg.ExternalAgents = map[string]config.ExternalAgentConfig{"codex": {Enabled: true, Command: "codex"}}
	workspace := filepath.Join(orch.cfg.Repo.WorkspaceRoot, "task_20260429_065348_8f7391fb")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatal(err)
	}
	task := taskstore.Task{
		ID:         "task_20260429_065348_8f7391fb",
		Title:      "chat failed send UI",
		Goal:       "chat failed send UI",
		Status:     taskstore.StatusConflictResolution,
		AssignedTo: "OrchestratorAgent",
		Workspace:  workspace,
		Result:     "ReviewerAgent could not reconcile task branch with current main before checks: merge conflict",
		CreatedAt:  time.Now().UTC(),
		UpdatedAt:  time.Now().Add(-10 * time.Minute).UTC(),
	}
	if err := orch.tasks.Save(task); err != nil {
		t.Fatal(err)
	}

	count, err := orch.ReconcileTasks(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("recovered = %d, want 1", count)
	}
	select {
	case <-delegateStarted:
	case <-time.After(time.Second):
		t.Fatal("automatic recovery worker did not start")
	}
	updated, err := orch.tasks.Load(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != taskstore.StatusRunning || updated.AssignedTo != "codex" || updated.AutoRecoveryAttempts != 1 {
		t.Fatalf("task = %#v, want running codex recovery attempt", updated)
	}
	close(releaseDelegate)
	waitForDelegationReviewEvent(t, orch, task.ID)
}

func TestPrepareDelegationForConflictResolutionPreservesFailureContext(t *testing.T) {
	orch := newTestOrchestrator(t, nil)
	tempDir := t.TempDir()
	root := filepath.Join(tempDir, "repo")
	workspaceRoot := filepath.Join(tempDir, "workspaces")
	workspace := filepath.Join(workspaceRoot, "task_20260428_090000_c0ffee00")
	orch.cfg.Repo.Root = root
	orch.cfg.Repo.WorkspaceRoot = workspaceRoot
	gitTestRun(t, "", "init", "--initial-branch=main", root)
	gitTestRun(t, root, "config", "user.email", "test@example.com")
	gitTestRun(t, root, "config", "user.name", "Test User")
	if err := os.WriteFile(filepath.Join(root, "app.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitTestRun(t, root, "add", ".")
	gitTestRun(t, root, "commit", "-m", "base")
	gitTestRun(t, root, "worktree", "add", "-b", "homelabd/task_20260428_090000_c0ffee00", workspace)
	if err := os.WriteFile(filepath.Join(workspace, "app.txt"), []byte("task\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitTestRun(t, workspace, "commit", "-am", "task conflict")
	if err := os.WriteFile(filepath.Join(root, "app.txt"), []byte("main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitTestRun(t, root, "commit", "-am", "main conflict")
	previousResult := "ReviewerAgent could not reconcile task branch with current main before checks: CONFLICT (content): Merge conflict in app.txt"
	task := taskstore.Task{
		ID:         "task_20260428_090000_c0ffee00",
		Title:      "conflict retry",
		Goal:       "conflict retry",
		Status:     taskstore.StatusConflictResolution,
		AssignedTo: "OrchestratorAgent",
		Workspace:  workspace,
		Result:     previousResult,
		CreatedAt:  time.Now().UTC(),
		UpdatedAt:  time.Now().UTC(),
	}
	if err := orch.tasks.Save(task); err != nil {
		t.Fatal(err)
	}

	run, err := orch.prepareDelegationForTask(context.Background(), task.ID, "codex", "resolve the main branch conflict")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(run.Instruction, previousResult) ||
		!strings.Contains(run.Instruction, "Conflict-resolution workspace context") ||
		!strings.Contains(run.Instruction, "resolve them, stage the resolved files, and commit") {
		t.Fatalf("instruction = %q, want prior failure and conflict guidance", run.Instruction)
	}
	status := gitTestOutput(t, workspace, "status", "--porcelain")
	if !strings.Contains(status, "UU app.txt") {
		t.Fatalf("status = %q, want unmerged conflict prepared for worker", status)
	}
	if got := readTestFile(t, filepath.Join(workspace, "app.txt")); !strings.Contains(got, "<<<<<<<") {
		t.Fatalf("app.txt = %q, want conflict markers left for worker", got)
	}
	updated, err := orch.tasks.Load(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != taskstore.StatusRunning || updated.AssignedTo != "codex" {
		t.Fatalf("task = %#v, want running codex task", updated)
	}
	if !strings.Contains(updated.Result, previousResult) || !strings.Contains(updated.Result, "conflict retry context") {
		t.Fatalf("result = %q, want preserved failure and conflict context", updated.Result)
	}
}

func TestStaleMergeApprovalDoesNotRunForDoneTask(t *testing.T) {
	orch := newTestOrchestrator(t, nil)
	if err := orch.registry.Register(mergeApprovedStub{}); err != nil {
		t.Fatal(err)
	}
	task := taskstore.Task{
		ID:         "task_20260425_225253_6eb67bb4",
		Title:      "add theme toggle",
		Goal:       "add theme toggle",
		Status:     taskstore.StatusDone,
		AssignedTo: "OrchestratorAgent",
		Result:     "accepted by human",
		CreatedAt:  time.Now().UTC(),
		UpdatedAt:  time.Now().UTC(),
	}
	if err := orch.tasks.Save(task); err != nil {
		t.Fatal(err)
	}
	req := approvalstore.Request{
		ID:     "approval_20260425_230054_037ea577",
		TaskID: task.ID,
		Tool:   "git.merge_approved",
		Args:   json.RawMessage(`{"branch":"homelabd/task_20260425_225253_6eb67bb4"}`),
		Reason: "merge reviewed task branch into repo root",
		Status: approvalstore.StatusPending,
	}
	if err := orch.approvals.Save(req); err != nil {
		t.Fatal(err)
	}

	reply, err := orch.ResolveApproval(context.Background(), req.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(reply, "is stale") || !strings.Contains(reply, "No merge was attempted") {
		t.Fatalf("reply = %q, want stale approval explanation", reply)
	}
	updatedApproval, err := orch.approvals.Load(req.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updatedApproval.Status != approvalstore.StatusStale {
		t.Fatalf("approval status = %q, want stale", updatedApproval.Status)
	}
	updatedTask, err := orch.tasks.Load(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updatedTask.Status != taskstore.StatusDone {
		t.Fatalf("task status = %q, want done", updatedTask.Status)
	}
}

func TestStaleMergeApprovalDoesNotRunForMissingTask(t *testing.T) {
	orch := newTestOrchestrator(t, nil)
	if err := orch.registry.Register(mergeApprovedStub{}); err != nil {
		t.Fatal(err)
	}
	req := approvalstore.Request{
		ID:     "approval_20260426_103333_deadbeef",
		TaskID: "task_20260426_000000_missing",
		Tool:   "git.merge_approved",
		Args:   json.RawMessage(`{"branch":"homelabd/task_20260426_000000_missing"}`),
		Reason: "merge reviewed task branch into repo root",
		Status: approvalstore.StatusPending,
	}
	if err := orch.approvals.Save(req); err != nil {
		t.Fatal(err)
	}

	reply, err := orch.ResolveApproval(context.Background(), req.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(reply, "task record") || !strings.Contains(reply, "No merge was attempted") {
		t.Fatalf("reply = %q, want missing task explanation", reply)
	}
	updatedApproval, err := orch.approvals.Load(req.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updatedApproval.Status != approvalstore.StatusStale {
		t.Fatalf("approval status = %q, want stale", updatedApproval.Status)
	}
}

func TestOlderMergeApprovalDoesNotRunWhenNewerApprovalExists(t *testing.T) {
	orch := newTestOrchestrator(t, nil)
	if err := orch.registry.Register(mergeShouldNotRunStub{}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	task := taskstore.Task{
		ID:         "task_20260427_120000_6d41996e",
		Title:      "retry stale approval",
		Goal:       "retry stale approval",
		Status:     taskstore.StatusAwaitingApproval,
		AssignedTo: "OrchestratorAgent",
		CreatedAt:  now,
		UpdatedAt:  now,
		Workspace:  filepath.Join(t.TempDir(), "workspace"),
	}
	if err := orch.tasks.Save(task); err != nil {
		t.Fatal(err)
	}
	oldApproval := approvalstore.Request{
		ID:        "approval_20260427_120000_old00001",
		TaskID:    task.ID,
		Tool:      "git.merge_approved",
		Args:      json.RawMessage(`{"branch":"homelabd/task_20260427_120000_6d41996e"}`),
		Reason:    "merge reviewed task branch into repo root",
		Status:    approvalstore.StatusPending,
		CreatedAt: now,
	}
	newApproval := approvalstore.Request{
		ID:        "approval_20260427_120100_new00001",
		TaskID:    task.ID,
		Tool:      "git.merge_approved",
		Args:      json.RawMessage(`{"branch":"homelabd/task_20260427_120000_6d41996e"}`),
		Reason:    "merge reviewed retry result into repo root",
		Status:    approvalstore.StatusPending,
		CreatedAt: now.Add(time.Minute),
	}
	if err := orch.approvals.Save(oldApproval); err != nil {
		t.Fatal(err)
	}
	if err := orch.approvals.Save(newApproval); err != nil {
		t.Fatal(err)
	}

	reply, err := orch.ResolveApproval(context.Background(), oldApproval.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(reply, "is stale") || !strings.Contains(reply, newApproval.ID) || !strings.Contains(reply, "No merge was attempted") {
		t.Fatalf("reply = %q, want stale older approval explanation", reply)
	}
	updatedOldApproval, err := orch.approvals.Load(oldApproval.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updatedOldApproval.Status != approvalstore.StatusStale {
		t.Fatalf("old approval status = %q, want stale", updatedOldApproval.Status)
	}
	updatedNewApproval, err := orch.approvals.Load(newApproval.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updatedNewApproval.Status != approvalstore.StatusPending {
		t.Fatalf("new approval status = %q, want pending", updatedNewApproval.Status)
	}
	updatedTask, err := orch.tasks.Load(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updatedTask.Status != taskstore.StatusAwaitingApproval {
		t.Fatalf("task status = %q, want awaiting_approval", updatedTask.Status)
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
	if updated.Status != taskstore.StatusQueued {
		t.Fatalf("status = %q, want queued", updated.Status)
	}
	if updated.AssignedTo != "OrchestratorAgent" {
		t.Fatalf("AssignedTo = %q, want OrchestratorAgent", updated.AssignedTo)
	}
	if !strings.Contains(updated.Result, "scroll still jumps") {
		t.Fatalf("Result = %q, want reopen reason", updated.Result)
	}
}

func TestRefreshTaskWorkspaceResetsBranchToCurrentMain(t *testing.T) {
	orch := newTestOrchestrator(t, nil)
	tempDir := t.TempDir()
	root := filepath.Join(tempDir, "repo")
	workspaceRoot := filepath.Join(tempDir, "workspaces")
	workspace := filepath.Join(workspaceRoot, "task_20260426_014005_793f04ec")
	orch.cfg.Repo.Root = root
	orch.cfg.Repo.WorkspaceRoot = workspaceRoot
	gitTestRun(t, "", "init", "--initial-branch=main", root)
	gitTestRun(t, root, "config", "user.email", "test@example.com")
	gitTestRun(t, root, "config", "user.name", "Test User")
	if err := os.WriteFile(filepath.Join(root, "app.txt"), []byte("main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitTestRun(t, root, "add", "app.txt")
	gitTestRun(t, root, "commit", "-m", "main")
	gitTestRun(t, root, "worktree", "add", "-b", "homelabd/task_20260426_014005_793f04ec", workspace)
	if err := os.WriteFile(filepath.Join(workspace, "app.txt"), []byte("stale\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitTestRun(t, workspace, "commit", "-am", "stale")
	task := taskstore.Task{
		ID:         "task_20260426_014005_793f04ec",
		Title:      "parse task state commands",
		Goal:       "parse task state commands",
		Status:     taskstore.StatusBlocked,
		AssignedTo: "OrchestratorAgent",
		Workspace:  workspace,
		CreatedAt:  time.Now().UTC(),
		UpdatedAt:  time.Now().UTC(),
	}
	if err := orch.tasks.Save(task); err != nil {
		t.Fatal(err)
	}

	reply, err := orch.Handle(context.Background(), "test", "refresh 793f04ec")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(reply, "Refreshed 793f04ec") {
		t.Fatalf("reply = %q, want refresh confirmation", reply)
	}
	if got := readTestFile(t, filepath.Join(workspace, "app.txt")); got != "main\n" {
		t.Fatalf("app.txt = %q, want main content", got)
	}
	updated, err := orch.tasks.Load(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != taskstore.StatusBlocked || !strings.Contains(updated.Result, "workspace refreshed") {
		t.Fatalf("task = %#v, want blocked refreshed task", updated)
	}
}

func TestRetryConflictResolutionPreparesWorkspaceAndKeepsContext(t *testing.T) {
	orch := newTestOrchestrator(t, nil)
	tempDir := t.TempDir()
	root := filepath.Join(tempDir, "repo")
	workspace := filepath.Join(tempDir, "workspaces", "task_20260428_090000_badf00d")
	orch.cfg.Repo.Root = root
	orch.cfg.Repo.WorkspaceRoot = filepath.Join(tempDir, "workspaces")
	gitTestRun(t, "", "init", "--initial-branch=main", root)
	gitTestRun(t, root, "config", "user.email", "test@example.com")
	gitTestRun(t, root, "config", "user.name", "Test User")
	if err := os.WriteFile(filepath.Join(root, "app.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitTestRun(t, root, "add", ".")
	gitTestRun(t, root, "commit", "-m", "base")
	gitTestRun(t, root, "worktree", "add", "-b", "homelabd/task_20260428_090000_badf00d", workspace)
	if err := os.WriteFile(filepath.Join(workspace, "app.txt"), []byte("task\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitTestRun(t, workspace, "commit", "-am", "task conflict")
	if err := os.WriteFile(filepath.Join(root, "app.txt"), []byte("main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitTestRun(t, root, "commit", "-am", "main conflict")
	previousResult := "ReviewerAgent could not reconcile task branch with current main before checks: CONFLICT (content): Merge conflict in app.txt"
	task := taskstore.Task{
		ID:         "task_20260428_090000_badf00d",
		Title:      "resolve conflict",
		Goal:       "resolve conflict",
		Status:     taskstore.StatusConflictResolution,
		AssignedTo: "OrchestratorAgent",
		Workspace:  workspace,
		Result:     previousResult,
		CreatedAt:  time.Now().UTC(),
		UpdatedAt:  time.Now().UTC(),
	}
	if err := orch.tasks.Save(task); err != nil {
		t.Fatal(err)
	}

	run, err := orch.prepareDelegationForTask(context.Background(), task.ID, "codex", "resolve the rebase issue")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"resolve the rebase issue",
		"Task state before this worker run: status=conflict_resolution",
		"Latest task result before this worker run",
		previousResult,
		"Conflict-resolution workspace context",
		"Started merge of current main",
		"Conflict-resolution requirements",
	} {
		if !strings.Contains(run.Instruction, want) {
			t.Fatalf("instruction missing %q:\n%s", want, run.Instruction)
		}
	}
	status := gitTestOutput(t, workspace, "status", "--porcelain")
	if !strings.Contains(status, "UU app.txt") {
		t.Fatalf("status = %q, want prepared unresolved conflict", status)
	}
	if got := readTestFile(t, filepath.Join(workspace, "app.txt")); !strings.Contains(got, "<<<<<<<") {
		t.Fatalf("app.txt = %q, want conflict markers for worker", got)
	}
	updated, err := orch.tasks.Load(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != taskstore.StatusRunning || updated.AssignedTo != "codex" {
		t.Fatalf("task = %#v, want running codex retry", updated)
	}
	if !strings.Contains(updated.Result, "previous task result before this run") ||
		!strings.Contains(updated.Result, previousResult) ||
		!strings.Contains(updated.Result, "conflict retry context") {
		t.Fatalf("result = %q, want preserved retry context", updated.Result)
	}
}

func TestRetryBlockedPremergeFailureIsTreatedAsConflictWork(t *testing.T) {
	result := "ReviewerAgent premerge check failed: branch must be rebased or conflict-resolved before merge"
	if !shouldPrepareConflictWorkspace(taskstore.StatusBlocked, result) {
		t.Fatalf("blocked premerge failure should prepare conflict workspace")
	}
	if shouldPrepareConflictWorkspace(taskstore.StatusBlocked, "ReviewerAgent checks failed: go test failed") {
		t.Fatalf("ordinary check failure should not prepare conflict workspace")
	}
}

func TestReconcileTaskWorkspaceWithMainMergesNonConflictingMainChanges(t *testing.T) {
	orch := newTestOrchestrator(t, nil)
	tempDir := t.TempDir()
	root := filepath.Join(tempDir, "repo")
	workspace := filepath.Join(tempDir, "workspaces", "task_20260426_094828_3936a7ef")
	orch.cfg.Repo.Root = root
	gitTestRun(t, "", "init", "--initial-branch=main", root)
	gitTestRun(t, root, "config", "user.email", "test@example.com")
	gitTestRun(t, root, "config", "user.name", "Test User")
	if err := os.WriteFile(filepath.Join(root, "app.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitTestRun(t, root, "add", ".")
	gitTestRun(t, root, "commit", "-m", "base")
	gitTestRun(t, root, "worktree", "add", "-b", "homelabd/task_20260426_094828_3936a7ef", workspace)
	if err := os.WriteFile(filepath.Join(workspace, "task.txt"), []byte("task\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitTestRun(t, workspace, "add", ".")
	gitTestRun(t, workspace, "commit", "-m", "task change")
	if err := os.WriteFile(filepath.Join(root, "main.txt"), []byte("main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitTestRun(t, root, "add", ".")
	gitTestRun(t, root, "commit", "-m", "main change")

	out, err := orch.reconcileTaskWorkspaceWithMain(context.Background(), workspace)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Merge made") && !strings.Contains(out, "Fast-forward") && !strings.Contains(out, "Already up to date") {
		t.Fatalf("merge output = %q, want successful merge output", out)
	}
	if got := readTestFile(t, filepath.Join(workspace, "main.txt")); got != "main\n" {
		t.Fatalf("main.txt = %q, want main content", got)
	}
	if got := readTestFile(t, filepath.Join(workspace, "task.txt")); got != "task\n" {
		t.Fatalf("task.txt = %q, want task content", got)
	}
	status := gitTestOutput(t, workspace, "status", "--porcelain")
	if strings.TrimSpace(status) != "" {
		t.Fatalf("workspace status = %q, want clean", status)
	}
}

func TestReconcileTaskWorkspaceWithMainReportsConflicts(t *testing.T) {
	orch := newTestOrchestrator(t, nil)
	tempDir := t.TempDir()
	root := filepath.Join(tempDir, "repo")
	workspace := filepath.Join(tempDir, "workspaces", "task_20260426_094828_3936a7ef")
	orch.cfg.Repo.Root = root
	gitTestRun(t, "", "init", "--initial-branch=main", root)
	gitTestRun(t, root, "config", "user.email", "test@example.com")
	gitTestRun(t, root, "config", "user.name", "Test User")
	if err := os.WriteFile(filepath.Join(root, "app.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitTestRun(t, root, "add", ".")
	gitTestRun(t, root, "commit", "-m", "base")
	gitTestRun(t, root, "worktree", "add", "-b", "homelabd/task_20260426_094828_3936a7ef", workspace)
	if err := os.WriteFile(filepath.Join(workspace, "app.txt"), []byte("task\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitTestRun(t, workspace, "commit", "-am", "task conflict")
	if err := os.WriteFile(filepath.Join(root, "app.txt"), []byte("main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitTestRun(t, root, "commit", "-am", "main conflict")

	_, err := orch.reconcileTaskWorkspaceWithMain(context.Background(), workspace)
	if err == nil {
		t.Fatal("reconcile succeeded, want conflict error")
	}
	if !strings.Contains(err.Error(), "git merge current main") {
		t.Fatalf("err = %v, want merge context", err)
	}
	status := gitTestOutput(t, workspace, "status", "--porcelain")
	if strings.TrimSpace(status) != "" {
		t.Fatalf("workspace status = %q, want merge aborted and clean", status)
	}
}

func TestRestartPlanForDiffDetectsTouchedComponents(t *testing.T) {
	diff := strings.Join([]string{
		"diff --git a/cmd/homelabd/main.go b/cmd/homelabd/main.go",
		"diff --git a/web/dashboard/src/routes/tasks/+page.svelte b/web/dashboard/src/routes/tasks/+page.svelte",
		"diff --git a/docs/task-workflow.md b/docs/task-workflow.md",
	}, "\n")
	got := restartPlanForDiff(diff)
	if !strings.Contains(got, "homelabd") || !strings.Contains(got, "dashboard") {
		t.Fatalf("restartPlanForDiff() = %q, want homelabd and dashboard", got)
	}
	if strings.Contains(got, "docs") {
		t.Fatalf("restartPlanForDiff() = %q, should not restart docs", got)
	}
}

func TestUsageNotesIncludeRestartPlan(t *testing.T) {
	result := "ReviewerAgent test status: pass\nRestart plan: restart homelabd after merge before final acceptance"
	got := usageNotesFromResult(result)
	if !strings.Contains(got, "Restart plan: restart homelabd") {
		t.Fatalf("usageNotesFromResult() = %q, want restart plan", got)
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
	if updated.Status != taskstore.StatusQueued {
		t.Fatalf("status = %q, want queued", updated.Status)
	}
	if !strings.Contains(updated.Result, "needs rework") {
		t.Fatalf("Result = %q, want reopen reason", updated.Result)
	}
}

func TestNaturalTaskStateCommands(t *testing.T) {
	orch := newTestOrchestrator(t, nil)
	now := time.Now().UTC()
	for _, task := range []taskstore.Task{
		{
			ID:         "task_20260426_000000_aaaa1111",
			Title:      "chat markdown code blocks",
			Goal:       "chat markdown code blocks",
			Status:     taskstore.StatusAwaitingVerification,
			AssignedTo: "OrchestratorAgent",
			CreatedAt:  now,
			UpdatedAt:  now,
		},
		{
			ID:         "task_20260426_000001_bbbb2222",
			Title:      "search internet",
			Goal:       "search internet",
			Status:     taskstore.StatusAwaitingVerification,
			AssignedTo: "OrchestratorAgent",
			CreatedAt:  now,
			UpdatedAt:  now,
		},
		{
			ID:         "task_20260426_000002_cccc3333",
			Title:      "obsolete placeholder",
			Goal:       "obsolete placeholder",
			Status:     taskstore.StatusBlocked,
			AssignedTo: "OrchestratorAgent",
			CreatedAt:  now,
			UpdatedAt:  now,
		},
	} {
		if err := orch.tasks.Save(task); err != nil {
			t.Fatal(err)
		}
	}

	reply, err := orch.Handle(context.Background(), "test", "please accept the chat markdown code blocks task")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(reply, "Accepted aaaa1111") {
		t.Fatalf("reply = %q, want accepted confirmation", reply)
	}

	reply, err = orch.Handle(context.Background(), "test", "can you reopen bbbb2222 because the provider fallback is still wrong")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(reply, "Reopened bbbb2222") {
		t.Fatalf("reply = %q, want reopened confirmation", reply)
	}
	updated, err := orch.tasks.Load("task_20260426_000001_bbbb2222")
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != taskstore.StatusQueued || !strings.Contains(updated.Result, "provider fallback is still wrong") {
		t.Fatalf("updated task = %#v, want queued with reopen reason", updated)
	}

	reply, err = orch.Handle(context.Background(), "test", "please delete the obsolete placeholder task")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(reply, "Deleted task_20260426_000002_cccc3333") {
		t.Fatalf("reply = %q, want delete confirmation", reply)
	}
	if _, err := orch.tasks.Load("task_20260426_000002_cccc3333"); err == nil {
		t.Fatal("deleted task still loads")
	}
}

func TestParseTaskStateCommandVariants(t *testing.T) {
	tests := []struct {
		input        string
		wantAction   string
		wantSelector string
		wantReason   string
	}{
		{"please accept the search internet task", "accept", "the search internet task", ""},
		{"mark 28493611 done", "accept", "28493611", ""},
		{"can you reopen 28493611 because tests failed", "reopen", "28493611", "because tests failed"},
		{"send the bun task back for rework", "reopen", "the bun task", "for rework"},
		{"please delete the hi task", "delete", "the hi task", ""},
	}
	for _, tt := range tests {
		action, selector, reason, ok := parseTaskStateCommand(tt.input)
		if !ok {
			t.Fatalf("parseTaskStateCommand(%q) ok = false, want true", tt.input)
		}
		if action != tt.wantAction || selector != tt.wantSelector || reason != tt.wantReason {
			t.Fatalf("parseTaskStateCommand(%q) = (%q, %q, %q), want (%q, %q, %q)", tt.input, action, selector, reason, tt.wantAction, tt.wantSelector, tt.wantReason)
		}
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

func TestRememberCommandStoresDistilledLesson(t *testing.T) {
	provider := &staticProvider{content: `{"lesson":"Prefer durable decision rules over style mimicry.","kind":"preference"}`}
	orch := newTestOrchestrator(t, nil)
	orch.provider = provider
	orch.model = "test-model"

	reply, err := orch.Handle(context.Background(), "test", "remember that feedback should become decision guidance, not copied wording")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(reply, "Remembered mem_") ||
		!strings.Contains(reply, "Prefer durable decision rules over style mimicry.") {
		t.Fatalf("reply = %q, want remembered distilled lesson", reply)
	}

	list, err := orch.Handle(context.Background(), "test", "memories")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(list, "Prefer durable decision rules over style mimicry.") {
		t.Fatalf("memories = %q, want stored lesson", list)
	}
	if !strings.Contains(orch.llmToolPrompt(), "Prefer durable decision rules over style mimicry.") {
		t.Fatalf("prompt does not include stored lesson")
	}
}

func TestUnlearnCommandRemovesLesson(t *testing.T) {
	orch := newTestOrchestrator(t, nil)
	reply, err := orch.Handle(context.Background(), "test", "remember prefer short direct handoffs")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(reply, "Remembered mem_") {
		t.Fatalf("reply = %q, want remembered", reply)
	}

	reply, err = orch.Handle(context.Background(), "test", "unlearn short direct handoffs")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(reply, "Unlearned mem_") {
		t.Fatalf("reply = %q, want unlearned", reply)
	}

	list, err := orch.Handle(context.Background(), "test", "memories")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(list, "short direct handoffs") {
		t.Fatalf("memories = %q, want lesson removed", list)
	}
}

func TestOpenEndedChatIncludesDurableMemory(t *testing.T) {
	provider := &recordingProvider{}
	orch := newTestOrchestrator(t, nil)
	orch.provider = provider
	orch.model = "test-model"
	if _, err := orch.memory.RememberLesson(memstore.DefaultLessonFile, memstore.Lesson{
		Content: "Prefer distilled lessons over language mimicry.",
		Kind:    "preference",
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := orch.Handle(context.Background(), "test", "what should you optimise for?"); err != nil {
		t.Fatal(err)
	}
	if len(provider.requests) != 1 {
		t.Fatalf("request count = %d, want 1", len(provider.requests))
	}
	system := provider.requests[0].Messages[0].Content
	if !strings.Contains(system, "Prefer distilled lessons over language mimicry.") {
		t.Fatalf("system prompt missing durable memory: %s", system)
	}
}

func TestReflectIncludesActionableNewTaskCommand(t *testing.T) {
	goal := "Add task-ready action buttons for reflection results"
	provider := &staticProvider{content: `{"reflection":"Keep improvement ideas task-ready.","task_goal":"` + goal + `"}`}
	orch := newTestOrchestrator(t, nil)
	orch.provider = provider
	orch.model = "test-model"

	reply, err := orch.Handle(context.Background(), "test", "please reflect on our recent interaction and suggest one improvement")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(reply, "Reflection: Keep improvement ideas task-ready.") {
		t.Fatalf("reply = %q, want reflection", reply)
	}
	action := "`new " + goal + "`"
	if !strings.Contains(reply, action) {
		t.Fatalf("reply = %q, want actionable command %q", reply, action)
	}

	if _, err := orch.Handle(context.Background(), "test", "new "+goal); err != nil {
		t.Fatal(err)
	}
	tasks, err := orch.tasks.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 {
		t.Fatalf("task count = %d, want one queued task", len(tasks))
	}
	if tasks[0].Goal != goal || tasks[0].ParentID != "" || tasks[0].GraphPhase != "" {
		t.Fatalf("task = %#v, want standalone task with goal %q", tasks[0], goal)
	}
}

func TestUXCommandRunsUXAgentWithResearchPrompt(t *testing.T) {
	provider := &staticProvider{content: `{"message":"UX pass complete","done":true,"tool_calls":[]}`}
	orch := newTestOrchestrator(t, nil)
	orch.provider = provider
	orch.model = "test-model"
	if err := orch.registry.Register(noDiffStub{}); err != nil {
		t.Fatal(err)
	}
	taskID := "task_20260426_010101_a927493f"
	if err := orch.tasks.Save(taskstore.Task{
		ID:        taskID,
		Title:     "improve task page ergonomics",
		Goal:      "improve task page ergonomics",
		Status:    taskstore.StatusBlocked,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
		Workspace: t.TempDir(),
	}); err != nil {
		t.Fatal(err)
	}

	reply, err := orch.Handle(context.Background(), "test", "ux a927493f focus on touch targets")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(reply, "UX pass complete") {
		t.Fatalf("reply = %q, want UX completion", reply)
	}
	if len(provider.requests) != 1 {
		t.Fatalf("provider request count = %d, want 1", len(provider.requests))
	}
	system := provider.requests[0].Messages[0].Content
	for _, want := range []string{"You are UXAgent", "WCAG 2.2", "WAI-ARIA APG", "browser-level UAT", "bun.uat.tasks", "bun.uat.site", "Do not stop or restart production", "Mermaid fenced diagrams", "docs/diagramming-and-brand-colours.md"} {
		if !strings.Contains(system, want) {
			t.Fatalf("UX prompt missing %q:\n%s", want, system)
		}
	}
	user := provider.requests[0].Messages[1].Content
	if !strings.Contains(user, "Human instruction") || !strings.Contains(user, "focus on touch targets") {
		t.Fatalf("UX user prompt = %q, want human instruction", user)
	}
}

func TestDefaultDelegationInstructionRequiresIsolatedBrowserUAT(t *testing.T) {
	instruction := defaultDelegationInstruction(taskstore.Task{
		ID:        "task_20260428_100000_12345678",
		Goal:      "fix dashboard task page",
		Workspace: "/tmp/workspaces/task_123",
	})
	for _, want := range []string{
		"isolated dev server",
		"nix develop -c bun run --cwd web uat:tasks",
		"nix develop -c bun run --cwd web uat:site",
		"nix develop -c bun run --cwd web browser:preflight",
		"do not stop or restart production",
		"For remote tasks",
		"Mermaid fenced diagrams",
		"docs/diagramming-and-brand-colours.md",
		"#2563eb",
		"#60a5fa",
	} {
		if !strings.Contains(instruction, want) {
			t.Fatalf("delegation instruction missing %q:\n%s", want, instruction)
		}
	}
}

func TestDiffRequiresTaskPageUAT(t *testing.T) {
	diff := strings.Join([]string{
		"diff --git a/web/dashboard/src/routes/tasks/+page.svelte b/web/dashboard/src/routes/tasks/+page.svelte",
		"--- a/web/dashboard/src/routes/tasks/+page.svelte",
		"+++ b/web/dashboard/src/routes/tasks/+page.svelte",
		"@@",
		"+change",
	}, "\n")
	if !diffRequiresTaskPageUAT(diff) {
		t.Fatalf("task-page diff should require isolated task UAT")
	}
	if diffRequiresTaskPageUAT("diff --git a/pkg/task/store.go b/pkg/task/store.go\n+++ b/pkg/task/store.go") {
		t.Fatalf("backend-only diff should not require task-page UAT")
	}
}

func TestBrowserUATForDiffSelectsSiteUATForBroadDashboardChanges(t *testing.T) {
	diff := strings.Join([]string{
		"diff --git a/web/shared/src/lib/Navbar.svelte b/web/shared/src/lib/Navbar.svelte",
		"--- a/web/shared/src/lib/Navbar.svelte",
		"+++ b/web/shared/src/lib/Navbar.svelte",
		"@@",
		"+change",
	}, "\n")
	if got := browserUATForDiff(diff); got != "site" {
		t.Fatalf("browserUATForDiff(shared nav) = %q, want site", got)
	}

	diff = strings.Join([]string{
		"diff --git a/web/bun.lock b/web/bun.lock",
		"--- a/web/bun.lock",
		"+++ b/web/bun.lock",
		"@@",
		"+    \"mermaid\": [\"mermaid@11.14.0\"]",
	}, "\n")
	if got := browserUATForDiff(diff); got != "site" {
		t.Fatalf("browserUATForDiff(web lockfile) = %q, want site", got)
	}

	diff = strings.Join([]string{
		"diff --git a/pkg/supervisor/manager.go b/pkg/supervisor/manager.go",
		"--- a/pkg/supervisor/manager.go",
		"+++ b/pkg/supervisor/manager.go",
		"@@",
		"+change",
	}, "\n")
	if got := browserUATForDiff(diff); got != "site" {
		t.Fatalf("browserUATForDiff(supervisor) = %q, want site", got)
	}

	diff = strings.Join([]string{
		"diff --git a/web/dashboard/src/routes/terminal/+page.svelte b/web/dashboard/src/routes/terminal/+page.svelte",
		"--- a/web/dashboard/src/routes/terminal/+page.svelte",
		"+++ b/web/dashboard/src/routes/terminal/+page.svelte",
		"@@",
		"+change",
	}, "\n")
	if got := browserUATForDiff(diff); got != "site" {
		t.Fatalf("browserUATForDiff(terminal route) = %q, want site", got)
	}

	diff = strings.Join([]string{
		"diff --git a/web/dashboard/src/routes/tasks/+page.svelte b/web/dashboard/src/routes/tasks/+page.svelte",
		"--- a/web/dashboard/src/routes/tasks/+page.svelte",
		"+++ b/web/dashboard/src/routes/tasks/+page.svelte",
		"@@",
		"+change",
	}, "\n")
	if got := browserUATForDiff(diff); got != "tasks" {
		t.Fatalf("browserUATForDiff(task route) = %q, want tasks", got)
	}
}

func TestDelegateToUXRunsInternalUXAgent(t *testing.T) {
	delegateStarted := make(chan struct{}, 1)
	provider := &staticProvider{content: `{"message":"UX delegated","done":true,"tool_calls":[]}`}
	orch := newTestOrchestrator(t, &delegateStub{
		started: delegateStarted,
		release: make(chan struct{}),
	})
	orch.provider = provider
	orch.model = "test-model"
	if err := orch.registry.Register(noDiffStub{}); err != nil {
		t.Fatal(err)
	}
	taskID := "task_20260426_010102_bbbb2222"
	if err := orch.tasks.Save(taskstore.Task{
		ID:        taskID,
		Title:     "audit dashboard empty state",
		Goal:      "audit dashboard empty state",
		Status:    taskstore.StatusBlocked,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
		Workspace: t.TempDir(),
	}); err != nil {
		t.Fatal(err)
	}

	reply, err := orch.Handle(context.Background(), "test", "delegate bbbb2222 to ux audit empty and mobile states")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(reply, "UX delegated") {
		t.Fatalf("reply = %q, want UX delegated message", reply)
	}
	if len(provider.requests) != 1 {
		t.Fatalf("provider request count = %d, want 1", len(provider.requests))
	}
	select {
	case <-delegateStarted:
		t.Fatal("delegate to UX should use the internal UXAgent, not the external delegate tool")
	default:
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
		t.Fatalf("task count = %d, want one delegated task", len(tasks))
	}
	task := tasks[0]
	shortID := taskShortID(task.ID)
	want := "Next:\n```\nstatus\nshow " + shortID + "\n```"
	if !strings.Contains(reply, want) {
		t.Fatalf("reply = %q, want fenced commands %q", reply, want)
	}
	if strings.Contains(reply, "`status`") {
		t.Fatalf("reply = %q, should not inline command suggestions", reply)
	}
	if task.AssignedTo != "codex" {
		t.Fatalf("AssignedTo = %q, want codex", task.AssignedTo)
	}
	close(releaseDelegate)
	deadline := time.Now().Add(2 * time.Second)
	sawDelegateResult := false
	for time.Now().Before(deadline) {
		tasks, err = orch.tasks.List()
		if err != nil {
			t.Fatal(err)
		}
		task = tasks[0]
		if len(tasks) == 1 && task.AssignedTo == "OrchestratorAgent" && task.Status == taskstore.StatusReadyForReview && strings.Contains(task.Result, "done") {
			sawDelegateResult = true
		}
		if sawDelegateResult && !orch.taskActive(task.ID) {
			time.Sleep(20 * time.Millisecond)
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("delegate did not finish cleanly")
}

func TestWriteExternalRunArtifact(t *testing.T) {
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	orch := &Orchestrator{cfg: cfg}

	err := orch.writeExternalRunArtifact("delegate_test", "task_test", "codex", "/tmp/work", "completed", externalDelegateResult{
		ID:        "delegate_test",
		Backend:   "codex",
		TaskID:    "task_test",
		Workspace: "/tmp/work",
		Command:   []string{"codex", "exec"},
		Output:    "done",
		Duration:  42,
	}, "")
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(cfg.DataDir, "runs", "delegate_test.json"))
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]string{
		"id":        "delegate_test",
		"kind":      "external_agent",
		"task_id":   "task_test",
		"backend":   "codex",
		"workspace": "/tmp/work",
		"status":    "completed",
		"output":    "done",
	} {
		if got[key] != want {
			t.Fatalf("%s = %v, want %q", key, got[key], want)
		}
	}
	runs, err := orch.ListTaskRuns("task_test")
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 || runs[0].ID != "delegate_test" {
		t.Fatalf("runs = %#v, want delegate_test", runs)
	}
	if !strings.HasSuffix(runs[0].Path, filepath.Join("runs", "delegate_test.json")) {
		t.Fatalf("run path = %q, want artifact path", runs[0].Path)
	}
}

func TestCancelTaskStopsActiveDelegation(t *testing.T) {
	delegateStarted := make(chan struct{}, 1)
	orch := newTestOrchestrator(t, &delegateStub{
		started: delegateStarted,
		release: make(chan struct{}),
	})
	now := time.Now().UTC()
	taskID := "task_20260426_120000_cancel01"
	workspace := filepath.Join(t.TempDir(), "workspaces", taskID)
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := orch.tasks.Save(taskstore.Task{
		ID:         taskID,
		Title:      "cancel running worker",
		Goal:       "cancel running worker",
		Status:     taskstore.StatusQueued,
		AssignedTo: "OrchestratorAgent",
		Workspace:  workspace,
		CreatedAt:  now,
		UpdatedAt:  now,
	}); err != nil {
		t.Fatal(err)
	}
	if err := orch.startDelegationForTask(context.Background(), taskID, "codex", "work"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-delegateStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("delegate did not start")
	}
	reply, err := orch.CancelTask(context.Background(), taskID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(reply, "Cancelled") {
		t.Fatalf("reply = %q, want cancellation", reply)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if !orch.taskActive(taskID) {
			task, err := orch.tasks.Load(taskID)
			if err != nil {
				t.Fatal(err)
			}
			if task.Status != taskstore.StatusCancelled {
				t.Fatalf("status = %q, want cancelled", task.Status)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("active delegation was not cleared after cancellation")
}

func TestStaleDelegateCompletionDoesNotChangeMergedOrDoneTask(t *testing.T) {
	cases := []struct {
		name   string
		status string
		result string
	}{
		{
			name:   "merged awaiting verification",
			status: taskstore.StatusAwaitingVerification,
			result: "merged after approval approval_test; awaiting human verification",
		},
		{
			name:   "accepted done",
			status: taskstore.StatusDone,
			result: "accepted by human",
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			delegateStarted := make(chan struct{}, 1)
			releaseDelegate := make(chan struct{})
			orch := newTestOrchestrator(t, &delegateStub{
				started: delegateStarted,
				release: releaseDelegate,
			})

			reply, err := orch.Handle(context.Background(), "test", "implement the stale worker guard")
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(reply, "started codex") {
				t.Fatalf("reply = %q, want started codex", reply)
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
				t.Fatalf("task count = %d, want one delegated task", len(tasks))
			}
			task := tasks[0]
			task.Status = tt.status
			task.AssignedTo = "OrchestratorAgent"
			task.Result = tt.result
			if err := orch.tasks.Save(task); err != nil {
				t.Fatal(err)
			}

			close(releaseDelegate)
			deadline := time.Now().Add(2 * time.Second)
			for time.Now().Before(deadline) {
				updated, err := orch.tasks.Load(task.ID)
				if err != nil {
					t.Fatal(err)
				}
				if !orch.taskActive(task.ID) {
					if updated.Status != tt.status || updated.Result != tt.result {
						t.Fatalf("updated task = %#v, want stale completion ignored", updated)
					}
					return
				}
				time.Sleep(10 * time.Millisecond)
			}
			t.Fatal("delegate did not finish")
		})
	}
}

func TestRetryStalesPendingApprovalBeforeStartingWorker(t *testing.T) {
	delegateStarted := make(chan struct{}, 1)
	releaseDelegate := make(chan struct{})
	orch := newTestOrchestrator(t, &delegateStub{
		started: delegateStarted,
		release: releaseDelegate,
	})
	now := time.Now().UTC()
	taskID := "task_20260427_120000_6d41996e"
	if err := orch.tasks.Save(taskstore.Task{
		ID:         taskID,
		Title:      "retry stale approval",
		Goal:       "retry stale approval",
		Status:     taskstore.StatusAwaitingApproval,
		AssignedTo: "OrchestratorAgent",
		Workspace:  filepath.Join(t.TempDir(), "workspaces", taskID),
		CreatedAt:  now,
		UpdatedAt:  now,
	}); err != nil {
		t.Fatal(err)
	}
	approval := approvalstore.Request{
		ID:        "approval_20260427_120000_old00001",
		TaskID:    taskID,
		Tool:      "git.merge_approved",
		Args:      json.RawMessage(`{"branch":"homelabd/task_20260427_120000_6d41996e"}`),
		Reason:    "merge reviewed task branch into repo root",
		Status:    approvalstore.StatusPending,
		CreatedAt: now,
	}
	if err := orch.approvals.Save(approval); err != nil {
		t.Fatal(err)
	}

	reply, err := orch.RetryTask(context.Background(), taskID, "codex", "rebase and retry")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(reply, "Retried 6d41996e on codex") {
		t.Fatalf("reply = %q, want retry confirmation", reply)
	}
	select {
	case <-delegateStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("delegate did not start")
	}
	updatedApproval, err := orch.approvals.Load(approval.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updatedApproval.Status != approvalstore.StatusStale || !strings.Contains(updatedApproval.Reason, "superseded by a new worker run") {
		t.Fatalf("approval = %#v, want stale worker-run reason", updatedApproval)
	}
	updatedTask, err := orch.tasks.Load(taskID)
	if err != nil {
		t.Fatal(err)
	}
	if updatedTask.Status != taskstore.StatusRunning || updatedTask.AssignedTo != "codex" {
		t.Fatalf("task = %#v, want running codex task", updatedTask)
	}
	close(releaseDelegate)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if !orch.taskActive(taskID) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("delegate did not finish after release")
}

func TestReconcileRunningTaskWithGrantedMergeApprovalAwaitsVerification(t *testing.T) {
	delegateStarted := make(chan struct{}, 1)
	orch := newTestOrchestrator(t, &delegateStub{
		started: delegateStarted,
		release: make(chan struct{}),
	})
	now := time.Now().UTC()
	taskID := "task_20260427_120000_6d41996e"
	if err := orch.tasks.Save(taskstore.Task{
		ID:         taskID,
		Title:      "merged but running",
		Goal:       "merged but running",
		Status:     taskstore.StatusRunning,
		AssignedTo: "codex",
		Workspace:  filepath.Join(t.TempDir(), "workspaces", taskID),
		Result:     "delegated to codex; external worker is running",
		CreatedAt:  now,
		UpdatedAt:  now,
	}); err != nil {
		t.Fatal(err)
	}
	approval := approvalstore.Request{
		ID:        "approval_20260427_120000_granted1",
		TaskID:    taskID,
		Tool:      "git.merge_approved",
		Args:      json.RawMessage(`{"branch":"homelabd/task_20260427_120000_6d41996e"}`),
		Reason:    "merge reviewed task branch into repo root",
		Status:    approvalstore.StatusGranted,
		CreatedAt: now,
	}
	if err := orch.approvals.Save(approval); err != nil {
		t.Fatal(err)
	}

	count, err := orch.ReconcileTasks(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("reconciled count = %d, want 1", count)
	}
	select {
	case <-delegateStarted:
		t.Fatal("granted merge recovery should not restart the worker")
	default:
	}
	updated, err := orch.tasks.Load(taskID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != taskstore.StatusAwaitingVerification || updated.AssignedTo != "OrchestratorAgent" {
		t.Fatalf("task = %#v, want awaiting verification owned by orchestrator", updated)
	}
	if !strings.Contains(updated.Result, approval.ID) {
		t.Fatalf("result = %q, want granted approval context", updated.Result)
	}
}

func TestReconcileRunningTaskIgnoresGrantedMergeApprovalFromPreviousRun(t *testing.T) {
	delegateStarted := make(chan struct{}, 1)
	orch := newTestOrchestrator(t, &delegateStub{
		started: delegateStarted,
		release: make(chan struct{}),
	})
	now := time.Now().UTC()
	taskID := "task_20260427_120000_6d41996e"
	if err := orch.tasks.Save(taskstore.Task{
		ID:         taskID,
		Title:      "reopened after merge",
		Goal:       "reopened after merge",
		Status:     taskstore.StatusRunning,
		AssignedTo: "codex",
		Workspace:  filepath.Join(t.TempDir(), "workspaces", taskID),
		Result:     "delegated to codex; external worker is running",
		CreatedAt:  now.Add(-time.Hour),
		UpdatedAt:  now,
	}); err != nil {
		t.Fatal(err)
	}
	running, err := orch.tasks.Load(taskID)
	if err != nil {
		t.Fatal(err)
	}
	if running.StartedAt == nil {
		t.Fatal("running task has no started_at")
	}
	approval := approvalstore.Request{
		ID:        "approval_20260427_110000_granted1",
		TaskID:    taskID,
		Tool:      "git.merge_approved",
		Args:      json.RawMessage(`{"branch":"homelabd/task_20260427_120000_6d41996e"}`),
		Reason:    "previous run merge approval",
		Status:    approvalstore.StatusGranted,
		CreatedAt: running.StartedAt.Add(-time.Hour),
		UpdatedAt: running.StartedAt.Add(-time.Hour),
	}
	if err := writeApprovalRecord(orch, approval); err != nil {
		t.Fatal(err)
	}

	count, err := orch.ReconcileTasks(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("reconciled count = %d, want 0", count)
	}
	select {
	case <-delegateStarted:
		t.Fatal("fresh running task should not restart before stale threshold")
	default:
	}
	updated, err := orch.tasks.Load(taskID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != taskstore.StatusRunning {
		t.Fatalf("task status = %q, want running", updated.Status)
	}
}

func TestRecoverRunningTasksRestartsExternalWorker(t *testing.T) {
	delegateStarted := make(chan struct{}, 1)
	releaseDelegate := make(chan struct{})
	orch := newTestOrchestrator(t, &delegateStub{
		started: delegateStarted,
		release: releaseDelegate,
	})
	var logs lockedBuffer
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
		logText := logs.String()
		if strings.Contains(logText, "task recovery finished") && !orch.taskActive(taskID) && updated.Status != taskstore.StatusRunning {
			if !strings.Contains(logText, "recovering persisted running task") {
				t.Fatalf("logs = %q, want recovery log", logText)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("recovered delegate did not finish cleanly")
}

type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
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

func TestReconcileQueuedTasksStartsWorker(t *testing.T) {
	delegateStarted := make(chan struct{}, 1)
	releaseDelegate := make(chan struct{})
	orch := newTestOrchestrator(t, &delegateStub{
		started: delegateStarted,
		release: releaseDelegate,
	})
	now := time.Now().UTC()
	taskID := "task_20260426_010000_a927493f"
	if err := orch.tasks.Save(taskstore.Task{
		ID:         taskID,
		Title:      "action reflection into task",
		Goal:       "action reflection into task",
		Status:     taskstore.StatusQueued,
		AssignedTo: "OrchestratorAgent",
		Workspace:  filepath.Join(t.TempDir(), "workspaces", taskID),
		CreatedAt:  now,
		UpdatedAt:  now,
	}); err != nil {
		t.Fatal(err)
	}

	count, err := orch.ReconcileTasks(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("reconciled count = %d, want 1", count)
	}
	select {
	case <-delegateStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("queued task was not delegated")
	}
	updated, err := orch.tasks.Load(taskID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != taskstore.StatusRunning || updated.AssignedTo != "codex" {
		t.Fatalf("task = %#v, want running codex task", updated)
	}
	close(releaseDelegate)
	waitForTaskStatus(t, orch, taskID, taskstore.StatusReadyForReview)
}

func TestReconcileStaleCoderTaskDelegatesToCodex(t *testing.T) {
	delegateStarted := make(chan struct{}, 1)
	releaseDelegate := make(chan struct{})
	orch := newTestOrchestrator(t, &delegateStub{
		started: delegateStarted,
		release: releaseDelegate,
	})
	orch.cfg.Limits.TaskStaleSeconds = 1
	taskID := "task_20260425_235939_a927493f"
	staleTask := taskstore.Task{
		ID:         taskID,
		Title:      "reflection task action",
		Goal:       "reflection task action",
		Status:     taskstore.StatusRunning,
		AssignedTo: "CoderAgent",
		Workspace:  filepath.Join(t.TempDir(), "workspaces", taskID),
		CreatedAt:  time.Now().UTC().Add(-time.Hour),
		UpdatedAt:  time.Now().UTC().Add(-time.Hour),
	}
	if err := orch.tasks.Save(staleTask); err != nil {
		t.Fatal(err)
	}
	b, err := json.MarshalIndent(staleTask, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(orch.cfg.DataDir, "tasks", taskID+".json"), append(b, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}

	count, err := orch.ReconcileTasks(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("reconciled count = %d, want 1", count)
	}
	select {
	case <-delegateStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("stale task was not delegated")
	}
	updated, err := orch.tasks.Load(taskID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != taskstore.StatusRunning || updated.AssignedTo != "codex" {
		t.Fatalf("task = %#v, want stale task reassigned to codex", updated)
	}
	close(releaseDelegate)
	waitForTaskStatus(t, orch, taskID, taskstore.StatusReadyForReview)
}

func TestRecoveryPlanPreservesUXAgent(t *testing.T) {
	orch := newTestOrchestrator(t, nil)
	strategy, backend := orch.recoveryPlan(taskstore.Task{AssignedTo: "UXAgent"})
	if strategy != recoveryUX || backend != "" {
		t.Fatalf("recoveryPlan() = (%q, %q), want UX recovery", strategy, backend)
	}
}

func TestCoderPromptExposesLimitedShellAndContextSearch(t *testing.T) {
	orch := newTestOrchestrator(t, nil)
	if err := orch.registry.Register(shellRunLimitedStub{}); err != nil {
		t.Fatal(err)
	}

	prompt := orch.coderPrompt(taskstore.Task{
		ID:        "task_123",
		Workspace: "/tmp/workspaces/task_123",
	})
	for _, want := range []string{"shell.run_limited", "allowlisted command arrays", "grep-like context", "context_lines", "Mermaid fenced diagrams", "docs/diagramming-and-brand-colours.md", "#2563eb", "#60a5fa"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("coder prompt missing %q:\n%s", want, prompt)
		}
	}
}

func TestWorkflowStepPromptIncludesDiagramGuidance(t *testing.T) {
	prompt := workflowStepPrompt(workflowstore.Workflow{
		Name: "Release flow",
		Goal: "Explain deployment states",
	}, workflowstore.Step{
		Name:   "Summarise",
		Prompt: "Map the state machine",
	}, nil)
	for _, want := range []string{"Mermaid fenced diagrams", "#2563eb", "#60a5fa"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("workflow step prompt missing %q:\n%s", want, prompt)
		}
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
		Status:     taskstore.StatusReadyForReview,
		AssignedTo: "OrchestratorAgent",
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

type staticProvider struct {
	content  string
	requests []llm.CompletionRequest
}

func (p *staticProvider) Name() string { return "static" }

func (p *staticProvider) Complete(_ context.Context, req llm.CompletionRequest) (llm.CompletionResponse, error) {
	p.requests = append(p.requests, req)
	return llm.CompletionResponse{
		Message: llm.Message{
			Role:    "assistant",
			Content: p.content,
		},
		Provider: p.Name(),
	}, nil
}

type workflowTextCorrectStub struct{}

func (workflowTextCorrectStub) Name() string        { return "text.correct" }
func (workflowTextCorrectStub) Description() string { return "correct text" }
func (workflowTextCorrectStub) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"text":{"type":"string"}}}`)
}
func (workflowTextCorrectStub) Risk() tool.RiskLevel { return tool.RiskReadOnly }
func (workflowTextCorrectStub) Run(context.Context, json.RawMessage) (json.RawMessage, error) {
	return json.RawMessage(`{"corrected_text":"kittens in pajamas"}`), nil
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

func waitForTaskStatus(t *testing.T, orch *Orchestrator, taskID, status string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		updated, err := orch.tasks.Load(taskID)
		if err != nil {
			t.Fatal(err)
		}
		if updated.Status == status {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	updated, err := orch.tasks.Load(taskID)
	if err != nil {
		t.Fatal(err)
	}
	t.Fatalf("task status = %q, want %q", updated.Status, status)
}

func taskByPhase(tasks []taskstore.Task, phase string) taskstore.Task {
	for _, task := range tasks {
		if task.GraphPhase == phase {
			return task
		}
	}
	return taskstore.Task{}
}

func seedTaskGraph(t *testing.T, orch *Orchestrator, goal string) (taskstore.Task, []taskstore.Task) {
	t.Helper()
	now := time.Now().UTC()
	parent := taskstore.Task{
		ID:                 "task_graph_root",
		Title:              firstLine(goal),
		Goal:               goal,
		Status:             taskstore.StatusBlocked,
		AssignedTo:         "OrchestratorAgent",
		Priority:           5,
		CreatedAt:          now,
		UpdatedAt:          now,
		GraphPhase:         "root",
		Result:             "task graph parent; child phases drive execution",
		AcceptanceCriteria: rootAcceptanceCriteria(),
	}
	if err := orch.tasks.Save(parent); err != nil {
		t.Fatal(err)
	}
	var children []taskstore.Task
	var previousID string
	for i, phase := range defaultTaskGraphPhases(goal) {
		child := taskstore.Task{
			ID:                 "task_graph_" + phase.Name,
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
			Result:             "blocked on earlier graph phase",
			AcceptanceCriteria: phase.AcceptanceCriteria,
		}
		if previousID == "" {
			child.Status = taskstore.StatusQueued
			child.Result = "queued as first graph phase"
		} else {
			child.DependsOn = []string{previousID}
			child.BlockedBy = []string{previousID}
			child.ContextIDs = append(child.ContextIDs, previousID)
		}
		if err := orch.tasks.Save(child); err != nil {
			t.Fatal(err)
		}
		children = append(children, child)
		previousID = child.ID
	}
	return parent, children
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
	).
		WithMemory(memstore.NewStore(filepath.Join(cfg.DataDir, "memory"))).
		WithWorkflows(workflowstore.NewStore(filepath.Join(cfg.DataDir, "workflows")))
}

func gitTestRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	_ = gitTestOutput(t, dir, args...)
}

func gitTestOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return string(out)
}

func readTestFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func waitForDelegationReviewEvent(t *testing.T, orch *Orchestrator, taskID string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		events, err := orch.events.ReadDay(time.Now().UTC())
		if err == nil {
			for _, event := range events {
				if event.TaskID != taskID {
					continue
				}
				switch event.Type {
				case "task.review.completed", "task.review.failed":
					return
				}
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("delegation review event was not written")
}

type restartGateCalls struct {
	mu    sync.Mutex
	calls []string
}

func (c *restartGateCalls) append(call string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls = append(c.calls, call)
}

func (c *restartGateCalls) snapshot() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.calls...)
}

func newRestartGateSupervisor(t *testing.T, failRestart bool) (*httptest.Server, *restartGateCalls) {
	t.Helper()
	calls := &restartGateCalls{}
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		calls.append(req.Method + " " + req.URL.Path)
		switch {
		case req.Method == http.MethodPost && req.URL.Path == "/supervisord/apps/dashboard/restart":
			if failRestart {
				http.Error(rw, "restart failed", http.StatusInternalServerError)
				return
			}
			rw.WriteHeader(http.StatusOK)
			_, _ = rw.Write([]byte(`{"ok":true}`))
		case req.Method == http.MethodGet && req.URL.Path == "/health/dashboard":
			rw.WriteHeader(http.StatusOK)
			_, _ = rw.Write([]byte(`{"ok":true}`))
		case req.Method == http.MethodGet && req.URL.Path == "/supervisord":
			rw.WriteHeader(http.StatusOK)
			_, _ = rw.Write([]byte(`{"status":"running"}`))
		default:
			http.NotFound(rw, req)
		}
	}))
	return server, calls
}

func setSupervisorAppHealthURL(t *testing.T, cfg *config.Config, appName, healthURL string) {
	t.Helper()
	for i := range cfg.Supervisord.Apps {
		if cfg.Supervisord.Apps[i].Name == appName {
			cfg.Supervisord.Apps[i].HealthURL = healthURL
			return
		}
	}
	t.Fatalf("supervisor app %q not found", appName)
}

func waitForTask(t *testing.T, orch *Orchestrator, taskID string, done func(taskstore.Task) bool) taskstore.Task {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	var last taskstore.Task
	for time.Now().Before(deadline) {
		item, err := orch.tasks.Load(taskID)
		if err != nil {
			t.Fatal(err)
		}
		last = item
		if done(item) {
			return item
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for task %s; last = %#v", taskID, last)
	return taskstore.Task{}
}

func writeTestRepoFile(t *testing.T, root, rel, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeApprovalRecord(orch *Orchestrator, approval approvalstore.Request) error {
	dir := filepath.Join(orch.cfg.DataDir, "approvals")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(approval, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, approval.ID+".json"), append(b, '\n'), 0o644)
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

type taskTitleSummaryStub struct {
	summary       string
	text          string
	purpose       string
	maxCharacters int
}

func (taskTitleSummaryStub) Name() string        { return "text.summarize" }
func (taskTitleSummaryStub) Description() string { return "" }
func (taskTitleSummaryStub) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object"}`)
}
func (taskTitleSummaryStub) Risk() tool.RiskLevel { return tool.RiskReadOnly }
func (s *taskTitleSummaryStub) Run(_ context.Context, raw json.RawMessage) (json.RawMessage, error) {
	var req struct {
		Text          string `json:"text"`
		Purpose       string `json:"purpose"`
		MaxCharacters int    `json:"max_characters"`
	}
	_ = json.Unmarshal(raw, &req)
	s.text = req.Text
	s.purpose = req.Purpose
	s.maxCharacters = req.MaxCharacters
	return json.Marshal(map[string]any{"summary": s.summary})
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
		RunID     string `json:"run_id"`
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
		"id":        req.RunID,
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

type shellRunLimitedStub struct{}

func (shellRunLimitedStub) Name() string        { return "shell.run_limited" }
func (shellRunLimitedStub) Description() string { return "Run allowlisted command arrays." }
func (shellRunLimitedStub) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object"}`)
}
func (shellRunLimitedStub) Risk() tool.RiskLevel { return tool.RiskLow }
func (shellRunLimitedStub) Run(context.Context, json.RawMessage) (json.RawMessage, error) {
	return json.RawMessage(`{"output":""}`), nil
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

type textCorrectStub struct {
	text      string
	corrected string
	variants  []string
}

func (textCorrectStub) Name() string        { return "text.correct" }
func (textCorrectStub) Description() string { return "" }
func (textCorrectStub) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object"}`)
}
func (textCorrectStub) Risk() tool.RiskLevel { return tool.RiskReadOnly }
func (s *textCorrectStub) Run(_ context.Context, raw json.RawMessage) (json.RawMessage, error) {
	var req struct {
		Text string `json:"text"`
	}
	_ = json.Unmarshal(raw, &req)
	s.text = req.Text
	corrected := s.corrected
	if corrected == "" {
		corrected = req.Text
	}
	queries := append([]string{corrected}, s.variants...)
	return json.Marshal(map[string]any{
		"text":           req.Text,
		"corrected_text": corrected,
		"changed":        corrected != req.Text,
		"search_queries": queries,
	})
}

type internetResearchStub struct {
	query string
	depth string
}

func (internetResearchStub) Name() string        { return "internet.research" }
func (internetResearchStub) Description() string { return "" }
func (internetResearchStub) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object"}`)
}
func (internetResearchStub) Risk() tool.RiskLevel { return tool.RiskReadOnly }
func (s *internetResearchStub) Run(_ context.Context, raw json.RawMessage) (json.RawMessage, error) {
	var req struct {
		Query string `json:"query"`
		Depth string `json:"depth"`
	}
	_ = json.Unmarshal(raw, &req)
	s.query = req.Query
	s.depth = req.Depth
	return json.Marshal(map[string]any{
		"query":           req.Query,
		"source":          "all",
		"depth":           req.Depth,
		"method":          "plan -> fan-out search -> fetch",
		"search_provider": "brave",
		"subqueries":      []string{req.Query, req.Query + " official docs"},
		"sources": []map[string]any{{
			"title":   "Agent search architecture",
			"url":     "https://example.com/agent-search",
			"domain":  "example.com",
			"snippet": "Fan-out search and fetched evidence.",
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
		"command": "go test ./cmd/... ./pkg/... ./constraints",
		"output":  "FAIL\n",
	})
	if err != nil {
		return nil, err
	}
	return raw, fmt.Errorf("go test failed")
}

type statusChangingGoTestStub struct {
	orch   *Orchestrator
	taskID string
}

func (statusChangingGoTestStub) Name() string        { return "go.test" }
func (statusChangingGoTestStub) Description() string { return "" }
func (statusChangingGoTestStub) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object"}`)
}
func (statusChangingGoTestStub) Risk() tool.RiskLevel { return tool.RiskLow }
func (s statusChangingGoTestStub) Run(context.Context, json.RawMessage) (json.RawMessage, error) {
	t, err := s.orch.tasks.Load(s.taskID)
	if err != nil {
		return nil, err
	}
	t.Status = taskstore.StatusRunning
	t.AssignedTo = "codex"
	t.Result = "worker restarted while review was running"
	if err := s.orch.tasks.Save(t); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(map[string]any{
		"command": "go test ./cmd/... ./pkg/... ./constraints",
		"output":  "FAIL\n",
	})
	if err != nil {
		return nil, err
	}
	return raw, fmt.Errorf("go test failed")
}

type longGoTestFailStub struct{}

func (longGoTestFailStub) Name() string        { return "go.test" }
func (longGoTestFailStub) Description() string { return "" }
func (longGoTestFailStub) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object"}`)
}
func (longGoTestFailStub) Risk() tool.RiskLevel { return tool.RiskLow }
func (longGoTestFailStub) Run(context.Context, json.RawMessage) (json.RawMessage, error) {
	raw, err := json.Marshal(map[string]any{
		"command": "go test ./cmd/... ./pkg/... ./constraints",
		"output":  strings.Repeat("passing package output\n", 900) + "FINAL FAILURE LINE\n",
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

type mergeObservesWorkspaceStub struct {
	workspace string
	path      string
}

func (mergeObservesWorkspaceStub) Name() string        { return "git.merge_approved" }
func (mergeObservesWorkspaceStub) Description() string { return "" }
func (mergeObservesWorkspaceStub) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object"}`)
}
func (mergeObservesWorkspaceStub) Risk() tool.RiskLevel { return tool.RiskHigh }
func (s mergeObservesWorkspaceStub) Run(_ context.Context, _ json.RawMessage) (json.RawMessage, error) {
	if _, err := os.Stat(filepath.Join(s.workspace, s.path)); err != nil {
		return nil, err
	}
	return json.Marshal(map[string]any{"merged": true})
}

type mergeShouldNotRunStub struct{}

func (mergeShouldNotRunStub) Name() string        { return "git.merge_approved" }
func (mergeShouldNotRunStub) Description() string { return "" }
func (mergeShouldNotRunStub) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object"}`)
}
func (mergeShouldNotRunStub) Risk() tool.RiskLevel { return tool.RiskHigh }
func (mergeShouldNotRunStub) Run(context.Context, json.RawMessage) (json.RawMessage, error) {
	return nil, fmt.Errorf("merge tool should not run after auto-rebase conflict")
}

type mergeCheckFailStub struct{}

func (mergeCheckFailStub) Name() string        { return "git.merge_check" }
func (mergeCheckFailStub) Description() string { return "" }
func (mergeCheckFailStub) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object"}`)
}
func (mergeCheckFailStub) Risk() tool.RiskLevel { return tool.RiskReadOnly }
func (mergeCheckFailStub) Run(context.Context, json.RawMessage) (json.RawMessage, error) {
	return nil, fmt.Errorf("git merge-tree: conflict in web/dashboard/src/routes/tasks/+page.svelte")
}

type mergeCheckPassStub struct{}

func (mergeCheckPassStub) Name() string        { return "git.merge_check" }
func (mergeCheckPassStub) Description() string { return "" }
func (mergeCheckPassStub) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object"}`)
}
func (mergeCheckPassStub) Risk() tool.RiskLevel { return tool.RiskReadOnly }
func (mergeCheckPassStub) Run(context.Context, json.RawMessage) (json.RawMessage, error) {
	return json.Marshal(map[string]any{"mergeable": true})
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
