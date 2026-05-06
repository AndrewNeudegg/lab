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

func TestRunStoreArchivesAndRestoresRuns(t *testing.T) {
	store := NewRunStore(filepath.Join(t.TempDir(), "assistant_runs"))
	createdAt := time.Date(2026, 5, 6, 9, 0, 0, 0, time.UTC)
	archiveAt := createdAt.Add(time.Hour)

	if err := store.Save(Run{
		ID:        "arun_old_decision",
		Status:    RunStatusCompleted,
		Decision:  RunDecisionRecommend,
		Trigger:   RunTrigger{Kind: "event", Label: "Old decision"},
		Autonomy:  RunAutonomyPropose,
		Summary:   "Action recommended.",
		Snapshot:  RunSnapshot{GeneratedAt: createdAt},
		CreatedAt: createdAt,
		UpdatedAt: createdAt,
	}); err != nil {
		t.Fatal(err)
	}

	archived, err := store.SetArchived("arun_old_decision", true, "codex", "No longer required.", archiveAt)
	if err != nil {
		t.Fatal(err)
	}
	if !archived.Archived || archived.ArchivedBy != "codex" || archived.ArchivedReason != "No longer required." || archived.ArchivedAt == nil || !archived.ArchivedAt.Equal(archiveAt) {
		t.Fatalf("archived run = %#v, want archive metadata", archived)
	}

	restored, err := store.SetArchived("arun_old_decision", false, "codex", "", archiveAt.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if restored.Archived || restored.ArchivedAt != nil || restored.ArchivedBy != "" || restored.ArchivedReason != "" {
		t.Fatalf("restored run = %#v, want archive metadata cleared", restored)
	}
}

func TestNormalizeRunFillsDefaultsAndActionIDs(t *testing.T) {
	run := NormalizeRun(Run{
		ID:             " arun_1 ",
		ArchivedBy:     " codex ",
		ArchivedReason: " no longer needed ",
		Snapshot: RunSnapshot{Signals: []RunSignal{{
			Title:       " Review blocked task ",
			Fingerprint: "watchlist|tasks|blocked|task_1",
			Score:       120,
			SeenCount:   -3,
			UsefulCount: -2,
			Evidence: []RunSignalEvidence{
				{Source: " chat ", Kind: " quality ", Title: " Subpar response ", Detail: " Needs review ", Weight: 130},
				{Source: "empty"},
			},
			SafeActions:       []string{" create_task ", "snooze", "create_task", ""},
			SuggestedNextStep: " Review the source conversation. ",
		}}},
		RecommendedActions: []RunAction{
			{Kind: "task", Title: "Review findings", Rationale: "Needs attention."},
		},
	})

	if run.ID != "arun_1" {
		t.Fatalf("id = %q, want trimmed id", run.ID)
	}
	if run.ArchivedBy != "" || run.ArchivedReason != "" {
		t.Fatalf("archive metadata = %q/%q, want cleared when run is active", run.ArchivedBy, run.ArchivedReason)
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
	if run.Snapshot.Signals[0].Evidence[0].Source != "chat" || run.Snapshot.Signals[0].Evidence[0].Kind != "quality" || run.Snapshot.Signals[0].Evidence[0].Weight != 100 {
		t.Fatalf("signal evidence = %#v, want trimmed and clamped evidence", run.Snapshot.Signals[0].Evidence)
	}
	if len(run.Snapshot.Signals[0].Evidence) != 1 {
		t.Fatalf("signal evidence = %#v, want empty evidence omitted", run.Snapshot.Signals[0].Evidence)
	}
	if got := run.Snapshot.Signals[0].SafeActions; len(got) != 2 || got[0] != "create_task" || got[1] != "snooze" {
		t.Fatalf("safe actions = %#v, want trimmed deduped actions", got)
	}
	if run.Snapshot.Signals[0].SuggestedNextStep != "Review the source conversation." {
		t.Fatalf("suggested next step = %q, want trimmed text", run.Snapshot.Signals[0].SuggestedNextStep)
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

func TestSignalCandidateStoreUpsertsAndListsActiveCandidates(t *testing.T) {
	store := NewSignalCandidateStore(filepath.Join(t.TempDir(), "signal_candidates"))
	now := time.Date(2026, 5, 6, 12, 0, 0, 0, time.UTC)
	req := SignalSubmitRequest{
		Source:            "chat",
		Kind:              "chat_quality_feedback",
		Title:             "Review subpar chat answer",
		Detail:            "Operator feedback flagged a poor answer.",
		WhyNow:            "The operator said the answer was not useful.",
		Severity:          "warning",
		Surface:           "chat",
		ObjectID:          "evt_user",
		ObjectURL:         "/chat",
		Score:             88,
		ActionKind:        "task",
		Rationale:         "Poor answers are useful source-neutral signals.",
		TaskGoal:          "Review the exchange and improve the response path.",
		SafeActions:       []string{"create_task", "useful", "snooze", "dismiss"},
		SuggestedNextStep: "Create follow-up work to inspect the exchange.",
		TTLSeconds:        60,
		Evidence: []RunSignalEvidence{
			{Source: "chat", Kind: "user_feedback", Title: "Operator feedback", Detail: "That was wrong.", ObjectID: "evt_user", Weight: 88},
		},
	}

	first, err := store.Upsert(req, now)
	if err != nil {
		t.Fatal(err)
	}
	req.Evidence = append(req.Evidence, RunSignalEvidence{Source: "chat", Kind: "assistant_reply", Title: "Previous reply", Detail: "Old answer.", ObjectID: "evt_reply", Weight: 80})
	second, err := store.Upsert(req, now.Add(10*time.Second))
	if err != nil {
		t.Fatal(err)
	}

	if first.Fingerprint != second.Fingerprint {
		t.Fatalf("fingerprint changed from %q to %q", first.Fingerprint, second.Fingerprint)
	}
	if second.SeenCount != 2 || second.Source != "chat" || second.ActionKind != "task" {
		t.Fatalf("candidate = %#v, want second chat task sighting", second)
	}
	if len(second.Evidence) != 2 {
		t.Fatalf("evidence = %#v, want merged evidence", second.Evidence)
	}
	active, err := store.ListActive(now.Add(30 * time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != 1 || active[0].Fingerprint != second.Fingerprint {
		t.Fatalf("active = %#v, want stored candidate", active)
	}
	expired, err := store.ListActive(now.Add(2 * time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if len(expired) != 0 {
		t.Fatalf("expired = %#v, want no active candidates after ttl", expired)
	}
}
