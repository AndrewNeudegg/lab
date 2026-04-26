package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
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
	if len(tasks) != 7 {
		t.Fatalf("task count = %d, want parent plus 6 child phases", len(tasks))
	}
	var parent taskstore.Task
	var children []taskstore.Task
	for _, task := range tasks {
		if task.ParentID == "" {
			parent = task
			continue
		}
		children = append(children, task)
	}
	if parent.ID == "" {
		t.Fatalf("parent task not found in %#v", tasks)
	}
	if parent.Status != taskstore.StatusBlocked || parent.GraphPhase != "root" {
		t.Fatalf("parent status/phase = %q/%q, want blocked/root", parent.Status, parent.GraphPhase)
	}
	if len(children) != 6 {
		t.Fatalf("child count = %d, want 6", len(children))
	}
	sort.Slice(children, func(i, j int) bool { return children[i].Priority < children[j].Priority })
	if children[0].Status != taskstore.StatusQueued || children[0].GraphPhase != "inspect" {
		t.Fatalf("first child = %#v, want queued inspect phase", children[0])
	}
	for i, child := range children[1:] {
		if child.Status != taskstore.StatusBlocked {
			t.Fatalf("child %d status = %q, want blocked", i+1, child.Status)
		}
		if len(child.DependsOn) != 1 || child.DependsOn[0] != children[i].ID {
			t.Fatalf("child %d depends_on = %#v, want previous child %s", i+1, child.DependsOn, children[i].ID)
		}
	}
	if parent.Plan == nil {
		t.Fatal("task plan is nil, want reviewed plan")
	}
	if parent.Plan.Status != "reviewed" {
		t.Fatalf("plan status = %q, want reviewed", parent.Plan.Status)
	}
	if len(parent.Plan.Steps) != 4 {
		t.Fatalf("plan step count = %d, want 4", len(parent.Plan.Steps))
	}
	if !strings.Contains(reply, "Plan created and reviewed before execution") {
		t.Fatalf("reply = %q, want plan creation note", reply)
	}
	if !strings.Contains(reply, "Created task graph") || !strings.Contains(reply, "Child phases:") {
		t.Fatalf("reply = %q, want task graph summary", reply)
	}
	want := "Next:\n```\nstatus\nshow " + parent.ID + "\nrun " + children[0].ID + "\n```"
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
		if event.TaskID != parent.ID {
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

func TestAcceptingGraphPhaseReleasesNextChild(t *testing.T) {
	orch := newTestOrchestrator(t, nil)
	if _, err := orch.Handle(context.Background(), "test", "new improve task graph execution"); err != nil {
		t.Fatal(err)
	}
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
	if _, err := orch.Handle(context.Background(), "test", "new build graph parent completion"); err != nil {
		t.Fatal(err)
	}
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
	if _, err := orch.Handle(context.Background(), "test", "new enforce graph dependencies"); err != nil {
		t.Fatal(err)
	}
	tasks, err := orch.tasks.List()
	if err != nil {
		t.Fatal(err)
	}
	design := taskByPhase(tasks, "design")
	if design.ID == "" {
		t.Fatalf("design task not found in %#v", tasks)
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
	task := taskstore.Task{
		ID:         "task_20260426_220000_deadbeef",
		Title:      "add planner",
		Goal:       "add planner",
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
	if !strings.Contains(run.Instruction, "Reviewed task plan:") || !strings.Contains(run.Instruction, "Inspect scope") {
		t.Fatalf("instruction = %q, want reviewed plan context", run.Instruction)
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
	if !strings.Contains(reply, "Created task graph") {
		t.Fatalf("reply = %q, want created task graph", reply)
	}
	tasks, err := orch.tasks.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 8 {
		t.Fatalf("task count = %d, want existing task plus parent and 6 child phases", len(tasks))
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
	if created.Status != taskstore.StatusBlocked || created.GraphPhase != "root" {
		t.Fatalf("status/phase = %q/%q, want blocked root graph", created.Status, created.GraphPhase)
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

func TestReviewPremergeFailureBlocksWithoutAutoDelegating(t *testing.T) {
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
	if !strings.Contains(reply, "ready_for_review -> blocked") || !strings.Contains(reply, "no worker was restarted automatically") {
		t.Fatalf("reply = %q, want explicit blocked transition without auto restart", reply)
	}
	select {
	case <-delegateStarted:
		t.Fatal("review premerge failure should not auto-delegate")
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
	if len(tasks) != 7 {
		t.Fatalf("task count = %d, want parent plus 6 child phases", len(tasks))
	}
	parent := taskByPhase(tasks, "root")
	if parent.Goal != goal {
		t.Fatalf("task goal = %q, want %q", parent.Goal, goal)
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
	if len(tasks) != 7 {
		t.Fatalf("task count = %d, want parent plus 6 child phases", len(tasks))
	}
	parent := taskByPhase(tasks, "root")
	inspect := taskByPhase(tasks, "inspect")
	shortID := taskShortID(parent.ID)
	inspectShortID := taskShortID(inspect.ID)
	want := "Next:\n```\nstatus\nshow " + shortID + "\nshow " + inspectShortID + "\n```"
	if !strings.Contains(reply, want) {
		t.Fatalf("reply = %q, want fenced commands %q", reply, want)
	}
	if strings.Contains(reply, "`status`") {
		t.Fatalf("reply = %q, should not inline command suggestions", reply)
	}
	if inspect.AssignedTo != "codex" {
		t.Fatalf("AssignedTo = %q, want codex", inspect.AssignedTo)
	}
	close(releaseDelegate)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		tasks, err = orch.tasks.List()
		if err != nil {
			t.Fatal(err)
		}
		inspect = taskByPhase(tasks, "inspect")
		if len(tasks) == 7 && inspect.AssignedTo == "OrchestratorAgent" && inspect.Status == taskstore.StatusReadyForReview && strings.Contains(inspect.Result, "done") {
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
			if len(tasks) != 7 {
				t.Fatalf("task count = %d, want parent plus 6 child phases", len(tasks))
			}
			task := taskByPhase(tasks, "inspect")
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
