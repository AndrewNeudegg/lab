package assistant

import (
	"path/filepath"
	"testing"
	"time"
)

func TestRunStoreListsRunsNewestFirst(t *testing.T) {
	store := NewRunStore(filepath.Join(t.TempDir(), "assistant_runs"))
	oldTime := time.Date(2026, 4, 30, 20, 0, 0, 0, time.UTC)
	newTime := oldTime.Add(time.Minute)

	if err := store.Save(Run{
		ID:        "arun_old",
		Status:    RunStatusCompleted,
		Decision:  RunDecisionNoop,
		Trigger:   RunTrigger{Kind: "manual", Label: "Old run"},
		Autonomy:  RunAutonomyObserve,
		Summary:   "No action.",
		Snapshot:  RunSnapshot{GeneratedAt: oldTime},
		CreatedAt: oldTime,
		UpdatedAt: oldTime,
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(Run{
		ID:        "arun_new",
		Status:    RunStatusCompleted,
		Decision:  RunDecisionRecommend,
		Trigger:   RunTrigger{Kind: "manual", Label: "New run"},
		Autonomy:  RunAutonomyPropose,
		Summary:   "Action recommended.",
		Snapshot:  RunSnapshot{GeneratedAt: newTime},
		CreatedAt: newTime,
		UpdatedAt: newTime,
	}); err != nil {
		t.Fatal(err)
	}

	runs, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 2 {
		t.Fatalf("runs = %d, want 2", len(runs))
	}
	if runs[0].ID != "arun_new" || runs[1].ID != "arun_old" {
		t.Fatalf("runs order = %#v, want newest first", []string{runs[0].ID, runs[1].ID})
	}
}

func TestNormalizeRunFillsDefaultsAndActionIDs(t *testing.T) {
	run := NormalizeRun(Run{
		ID: " arun_1 ",
		Snapshot: RunSnapshot{Signals: []RunSignal{{
			Title:       " Review blocked task ",
			Fingerprint: "watchlist|tasks|blocked|task_1",
			Score:       120,
			SeenCount:   -3,
			UsefulCount: -2,
		}}},
		RecommendedActions: []RunAction{
			{Kind: "task", Title: "Review findings", Rationale: "Needs attention."},
		},
	})

	if run.ID != "arun_1" {
		t.Fatalf("id = %q, want trimmed id", run.ID)
	}
	if run.Status != RunStatusCompleted || run.Decision != RunDecisionNoop {
		t.Fatalf("status/decision = %q/%q, want completed/no-op", run.Status, run.Decision)
	}
	if run.Trigger.Kind != "manual" || run.Trigger.Label != "Manual proactive check" {
		t.Fatalf("trigger = %#v, want manual defaults", run.Trigger)
	}
	if run.Autonomy != RunAutonomyObserve {
		t.Fatalf("autonomy = %q, want observe", run.Autonomy)
	}
	if run.RecommendedActions[0].ID != "action_1" {
		t.Fatalf("action id = %q, want action_1", run.RecommendedActions[0].ID)
	}
	if run.RecommendedActions[0].Fingerprint == "" {
		t.Fatal("action fingerprint was not generated")
	}
	if run.Snapshot.TaskCounts == nil || run.Snapshot.WorkflowCounts == nil || run.Snapshot.RemoteAgentCounts == nil {
		t.Fatalf("snapshot maps were not initialised: %#v", run.Snapshot)
	}
	if run.Snapshot.Signals[0].Title != "Review blocked task" || run.Snapshot.Signals[0].Score != 100 {
		t.Fatalf("signal = %#v, want trimmed and clamped signal", run.Snapshot.Signals[0])
	}
	if run.Snapshot.Signals[0].SeenCount != 0 || run.Snapshot.Signals[0].UsefulCount != 0 {
		t.Fatalf("signal counts = %#v, want non-negative counts", run.Snapshot.Signals[0])
	}
}

func TestSignalStoreTracksRecommendationState(t *testing.T) {
	store := NewSignalStore(filepath.Join(t.TempDir(), "signals"))
	now := time.Now().UTC()
	action := NormalizeRun(Run{RecommendedActions: []RunAction{{
		Kind:          "task",
		Title:         "Review restart gate",
		Rationale:     "Restart failed.",
		TargetSurface: "tasks",
		TaskGoal:      "Review the dashboard restart gate.",
	}}}).RecommendedActions[0]

	first, err := store.UpsertFromAction("arun_1", action, now)
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.UpsertFromAction("arun_2", action, now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}

	if first.Fingerprint != second.Fingerprint {
		t.Fatalf("fingerprint changed: %q then %q", first.Fingerprint, second.Fingerprint)
	}
	if second.SeenCount != 2 || second.LastRunID != "arun_2" || second.Status != SignalStatusActive {
		t.Fatalf("signal = %#v, want active second sighting", second)
	}
	second.Status = SignalStatusSnoozed
	second.SnoozedUntil = now.Add(time.Hour)
	if err := store.Save(second); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Load(action.Fingerprint)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Status != SignalStatusSnoozed {
		t.Fatalf("loaded status = %q, want snoozed", loaded.Status)
	}
}
