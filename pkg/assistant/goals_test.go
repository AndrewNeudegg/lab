package assistant

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

func TestGoalStorePersistsTimelineObjects(t *testing.T) {
	store := NewGoalStore(filepath.Join(t.TempDir(), "assistant_goals"))
	now := time.Date(2026, 5, 7, 8, 0, 0, 0, time.UTC)
	next := now.Add(time.Hour)
	goal := Goal{
		ID:              "goal_email",
		Title:           "Daily email brief",
		Objective:       "Keep important email read and responded to.",
		Status:          GoalStatusActive,
		Autonomy:        RunAutonomyPropose,
		Cadence:         "hourly",
		NextCheckAt:     &next,
		SuccessCriteria: []string{"Brief is ready"},
		Constraints:     []string{"Do not send without approval"},
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	if err := store.SaveGoal(goal); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveWatch(GoalWatch{ID: "watch_email", GoalID: goal.ID, Title: "Unread important email", Status: GoalWatchStatusActive, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveNote(GoalNote{ID: "note_email", GoalID: goal.ID, Body: "Created from operator desire.", CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveAssessment(GoalAssessment{ID: "assess_email", GoalID: goal.ID, Decision: RunDecisionNoop, CreatedAt: now}); err != nil {
		t.Fatal(err)
	}

	loaded, err := store.LoadGoal(goal.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Title != goal.Title || loaded.Autonomy != RunAutonomyPropose {
		t.Fatalf("loaded goal = %#v", loaded)
	}
	watches, err := store.ListWatches(goal.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(watches) != 1 || watches[0].ID != "watch_email" {
		t.Fatalf("watches = %#v", watches)
	}
	notes, err := store.ListNotes(goal.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(notes) != 1 || notes[0].ID != "note_email" {
		t.Fatalf("notes = %#v", notes)
	}
	assessments, err := store.ListAssessments(goal.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(assessments) != 1 || assessments[0].ID != "assess_email" {
		t.Fatalf("assessments = %#v", assessments)
	}
}

func TestGoalDueAndCadenceHelpers(t *testing.T) {
	now := time.Date(2026, 5, 7, 8, 0, 0, 0, time.UTC)
	next := now.Add(-time.Minute)
	goal := Goal{ID: "goal_due", Title: "Due", Status: GoalStatusActive, Cadence: "daily", NextCheckAt: &next}

	if !GoalIsDue(goal, now) {
		t.Fatal("GoalIsDue = false, want true")
	}
	nextCheck := GoalNextCheckTime(goal, now)
	if nextCheck == nil || !nextCheck.Equal(now.Add(24*time.Hour)) {
		t.Fatalf("next check = %v, want tomorrow", nextCheck)
	}
}

func TestNormalizeGoalAutopilotDefaults(t *testing.T) {
	now := time.Date(2026, 5, 7, 8, 0, 0, 0, time.UTC)
	goal := NormalizeGoal(Goal{
		ID:            "goal_build",
		Title:         "Build feature",
		Status:        GoalStatusActive,
		Kind:          "project",
		ExecutionMode: "auto",
		Autopilot: &GoalAutopilot{
			Status:       "budget-exhausted",
			BudgetTasks:  0,
			TasksStarted: -1,
			StartedAt:    &now,
		},
		CreatedAt: now,
		UpdatedAt: now,
	})
	if goal.Kind != GoalKindBuild || goal.ExecutionMode != GoalExecutionModeAutopilot {
		t.Fatalf("goal type/mode = %s/%s, want build/autopilot", goal.Kind, goal.ExecutionMode)
	}
	if goal.Autopilot == nil {
		t.Fatal("autopilot = nil, want normalised state")
	}
	if goal.Autopilot.Status != GoalAutopilotStatusBudgetExhausted || goal.Autopilot.BudgetTasks != 1 || goal.Autopilot.TasksStarted != 0 {
		t.Fatalf("autopilot = %#v, want budget exhausted with default budget", goal.Autopilot)
	}
	if len(goal.Autopilot.AllowedActions) == 0 {
		t.Fatalf("allowed actions = %#v, want defaults", goal.Autopilot.AllowedActions)
	}
	unlimited := NormalizeGoalAutopilot(&GoalAutopilot{BudgetTasks: -50})
	if unlimited.BudgetTasks != GoalAutopilotUnlimitedBudget {
		t.Fatalf("unlimited budget = %d, want %d", unlimited.BudgetTasks, GoalAutopilotUnlimitedBudget)
	}

	guided := NormalizeGoal(Goal{ID: "goal_guided", Title: "Guided", Autopilot: &GoalAutopilot{Status: GoalAutopilotStatusRunning}, CreatedAt: now, UpdatedAt: now})
	if guided.ExecutionMode != GoalExecutionModeAutopilot || guided.Autopilot == nil {
		t.Fatalf("guided with autopilot payload = %#v, want autopilot mode preserved", guided)
	}
	guided.ExecutionMode = GoalExecutionModeGuided
	guided = NormalizeGoal(guided)
	if guided.Autopilot != nil {
		t.Fatalf("guided autopilot = %#v, want nil", guided.Autopilot)
	}
}

func TestNormalizeGoalKeepsRecentLinkedTasks(t *testing.T) {
	linkedTasks := make([]string, 0, 65)
	for i := 1; i <= 64; i++ {
		linkedTasks = append(linkedTasks, fmt.Sprintf("task_%02d", i))
	}
	linkedTasks = append(linkedTasks, "task_65")

	goal := NormalizeGoal(Goal{
		ID:          "goal_build",
		Title:       "Build feature",
		LinkedTasks: linkedTasks,
		CreatedAt:   time.Date(2026, 5, 7, 8, 0, 0, 0, time.UTC),
		UpdatedAt:   time.Date(2026, 5, 7, 8, 0, 0, 0, time.UTC),
	})

	if len(goal.LinkedTasks) != 64 {
		t.Fatalf("linked tasks count = %d, want 64", len(goal.LinkedTasks))
	}
	if goal.LinkedTasks[0] != "task_02" || goal.LinkedTasks[63] != "task_65" {
		t.Fatalf("linked tasks bounds = %s..%s, want task_02..task_65", goal.LinkedTasks[0], goal.LinkedTasks[63])
	}

	goal = NormalizeGoal(Goal{
		ID:          "goal_build",
		Title:       "Build feature",
		LinkedTasks: []string{" task_a ", "task_b", "task_a", "task_c"},
		CreatedAt:   time.Date(2026, 5, 7, 8, 0, 0, 0, time.UTC),
		UpdatedAt:   time.Date(2026, 5, 7, 8, 0, 0, 0, time.UTC),
	})
	if got := goal.LinkedTasks; len(got) != 3 || got[0] != "task_b" || got[1] != "task_a" || got[2] != "task_c" {
		t.Fatalf("deduped recent linked tasks = %#v, want task_b/task_a/task_c", got)
	}
}

func TestGoalUpdateRequestTracksExplicitFields(t *testing.T) {
	var req GoalUpdateRequest
	if err := json.Unmarshal([]byte(`{"details":"","success_criteria":[],"autopilot":{"budget_tasks":-1}}`), &req); err != nil {
		t.Fatal(err)
	}
	if !req.HasField("details") || !req.HasField("success_criteria") || !req.HasField("autopilot") {
		t.Fatalf("present fields not tracked: %#v", req)
	}
	if req.Details != "" || len(req.SuccessCriteria) != 0 || req.Autopilot == nil || req.Autopilot.BudgetTasks != -1 {
		t.Fatalf("decoded request = %#v", req)
	}
	if req.HasField("title") {
		t.Fatalf("title unexpectedly present in %#v", req)
	}
}
