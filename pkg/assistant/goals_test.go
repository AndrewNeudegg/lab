package assistant

import (
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

