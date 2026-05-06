package agent

import (
	"context"
	"strings"
	"testing"
	"time"

	assistantstore "github.com/andrewneudegg/lab/pkg/assistant"
	"github.com/andrewneudegg/lab/pkg/eventlog"
	"github.com/andrewneudegg/lab/pkg/id"
)

func TestAssistantRunCreateTasksAutonomyCreatesFollowUpTask(t *testing.T) {
	orch := newTestOrchestrator(t, nil)
	run := assistantstore.Run{
		Autonomy: assistantstore.RunAutonomyCreateTasks,
		Decision: assistantstore.RunDecisionRecommend,
		RecommendedActions: []assistantstore.RunAction{
			{
				ID:        "action_1",
				Kind:      "task",
				Title:     "Review restart gate",
				Rationale: "The task queue shows a failed restart gate.",
				TaskGoal:  "Review the dashboard restart gate and decide the recovery path.",
			},
			{
				ID:        "action_2",
				Kind:      "research",
				Title:     "Research restart patterns",
				Rationale: "This should stay a recommendation.",
			},
		},
	}

	orch.applyAssistantRunActions(context.Background(), &run)

	if run.Decision != assistantstore.RunDecisionCreated {
		t.Fatalf("decision = %q, want created_tasks", run.Decision)
	}
	if run.RecommendedActions[0].Status != assistantstore.SignalStatusCreatedTask || run.RecommendedActions[0].CreatedTaskID == "" {
		t.Fatalf("task action = %#v, want created task id", run.RecommendedActions[0])
	}
	if run.RecommendedActions[1].Status != "recommended" || run.RecommendedActions[1].CreatedTaskID != "" {
		t.Fatalf("research action = %#v, want recommendation only", run.RecommendedActions[1])
	}
	created, err := orch.tasks.Load(run.RecommendedActions[0].CreatedTaskID)
	if err != nil {
		t.Fatal(err)
	}
	if created.Goal != "Review the dashboard restart gate and decide the recovery path." {
		t.Fatalf("created task goal = %q", created.Goal)
	}
	if len(run.Receipts) != 1 || run.Receipts[0].Kind != "task_created" || run.Receipts[0].ObjectURL == "" {
		t.Fatalf("receipts = %#v, want task_created receipt", run.Receipts)
	}
}

func TestAssistantWatchlistSignalsScoreAndRespectFeedback(t *testing.T) {
	orch := newTestOrchestrator(t, nil)
	now := time.Date(2026, 5, 6, 9, 0, 0, 0, time.UTC)
	snapshot := assistantstore.RunSnapshot{
		AttentionTasks: []assistantstore.RunObjectRef{
			{
				ID:      "task_blocked",
				Title:   "Deploy dashboard",
				Status:  "blocked",
				Summary: "Waiting on operator decision.",
				URL:     "/tasks?task=task_blocked",
			},
		},
		PendingApprovals: 1,
		Supervisor: assistantstore.RunSystemSnapshot{Items: []assistantstore.RunObjectRef{
			{ID: "homelabd", Title: "homelabd", Status: "stopped", Summary: "desired running / crashed", URL: "/supervisord"},
			{ID: "homelab-agent", Title: "homelab-agent", Status: "stopped", Summary: "desired stopped / not started", URL: "/supervisord"},
		}},
	}
	candidates := assistantSignalCandidatesFromSnapshot(snapshot)
	var blocked assistantstore.RunSignal
	for _, signal := range candidates {
		if strings.Contains(signal.Title, "Deploy dashboard") {
			blocked = signal
			break
		}
	}
	if blocked.Fingerprint == "" {
		t.Fatalf("blocked task signal not found in %#v", candidates)
	}
	store, err := orch.assistantSignalStore()
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(assistantstore.SignalRecord{
		Fingerprint: blocked.Fingerprint,
		Status:      assistantstore.SignalStatusDismissed,
		Kind:        blocked.ActionKind,
		Title:       blocked.Title,
		Surface:     blocked.Surface,
		FirstSeenAt: now.Add(-time.Hour),
		LastSeenAt:  now.Add(-time.Hour),
		SeenCount:   2,
		DismissedAt: now.Add(-time.Minute),
		UpdatedAt:   now.Add(-time.Minute),
	}); err != nil {
		t.Fatal(err)
	}

	signals := orch.assistantWatchlistSignals(snapshot, now)

	var sawSuppressedBlocked, sawSupervisor, sawDesiredStopped bool
	for _, signal := range signals {
		if signal.Fingerprint == blocked.Fingerprint {
			sawSuppressedBlocked = signal.Suppressed && strings.Contains(signal.SuppressionReason, "Dismissed")
		}
		if strings.Contains(signal.Title, "homelabd") && signal.Score >= assistantSignalHighScore {
			sawSupervisor = true
		}
		if strings.Contains(signal.Title, "homelab-agent") {
			sawDesiredStopped = true
		}
	}
	if !sawSuppressedBlocked {
		t.Fatalf("signals = %#v, want blocked task suppressed by feedback", signals)
	}
	if !sawSupervisor {
		t.Fatalf("signals = %#v, want desired-running supervisor component scored high", signals)
	}
	if sawDesiredStopped {
		t.Fatalf("signals = %#v, did not want desired-stopped component treated as action", signals)
	}
}

func TestAssistantRunDecisionUsesScoredSignalsWhenModelMisses(t *testing.T) {
	orch := newTestOrchestrator(t, nil)
	provider := &staticProvider{content: `{
		"decision":"no_op",
		"summary":"Nothing new.",
		"changed":[],
		"concerns":[],
		"opportunities":[],
		"recommended_actions":[]
	}`}
	orch.provider = provider
	signal := newAssistantRunSignal(assistantstore.RunSignal{
		Kind:        "task_blocked",
		Title:       "Unblock task: Deploy dashboard",
		Detail:      "The task is blocked on an operator decision.",
		Severity:    "warning",
		Surface:     "tasks",
		ObjectID:    "task_blocked",
		ObjectURL:   "/tasks?task=task_blocked",
		Score:       90,
		Confidence:  "high",
		Priority:    "high",
		ActionKind:  "task",
		Rationale:   "The task is in an operator attention state.",
		TaskGoal:    "Review task_blocked and decide the next step.",
		Fingerprint: assistantSignalFingerprint("task", "blocked", "task_blocked", "Unblock task: Deploy dashboard"),
	})
	run := assistantstore.Run{
		Trigger:  assistantstore.RunTrigger{Kind: "manual", Label: "Manual proactive check"},
		Autonomy: assistantstore.RunAutonomyPropose,
		Goal:     "Review current state.",
		Snapshot: assistantstore.RunSnapshot{Signals: []assistantstore.RunSignal{signal}},
	}

	decision, _, err := orch.evaluateAssistantRun(context.Background(), run)
	if err != nil {
		t.Fatal(err)
	}

	if decision.Decision != assistantstore.RunDecisionRecommend {
		t.Fatalf("decision = %q, want recommend", decision.Decision)
	}
	if len(decision.RecommendedActions) != 1 {
		t.Fatalf("actions = %#v, want one signal-derived action", decision.RecommendedActions)
	}
	if decision.RecommendedActions[0].Fingerprint != signal.Fingerprint || decision.RecommendedActions[0].Priority != "high" {
		t.Fatalf("action = %#v, want signal fingerprint and priority", decision.RecommendedActions[0])
	}
	if len(decision.Concerns) != 1 || decision.Concerns[0].ObjectID != "task_blocked" {
		t.Fatalf("concerns = %#v, want signal-derived concern", decision.Concerns)
	}
	if len(provider.requests) != 1 || !strings.Contains(provider.requests[0].Messages[1].Content, `"signals"`) {
		t.Fatalf("provider request did not include scored signals: %#v", provider.requests)
	}
}

func TestAssistantRunPrunesSuppressedRecommendationsBeforeActions(t *testing.T) {
	run := assistantstore.Run{
		Decision: assistantstore.RunDecisionRecommend,
		RecommendedActions: []assistantstore.RunAction{
			{ID: "dismissed", Kind: "task", Title: "Dismissed", Status: assistantstore.SignalStatusDismissed},
			{ID: "active", Kind: "task", Title: "Active", Status: "recommended"},
			{ID: "created", Kind: "task", Title: "Created", Status: assistantstore.SignalStatusCreatedTask, CreatedTaskID: "task_existing"},
		},
	}

	suppressed := pruneAssistantSuppressedRunActions(&run)

	if suppressed != 2 {
		t.Fatalf("suppressed = %d, want 2", suppressed)
	}
	if len(run.RecommendedActions) != 1 || run.RecommendedActions[0].ID != "active" {
		t.Fatalf("actions = %#v, want only active recommendation", run.RecommendedActions)
	}
}

func TestAssistantRunActionCreateTaskFeedbackDedupesBySignal(t *testing.T) {
	orch := newTestOrchestrator(t, nil)
	now := time.Date(2026, 5, 5, 12, 0, 0, 0, time.UTC)
	run := assistantstore.NormalizeRun(assistantstore.Run{
		ID:        "arun_feedback",
		Status:    assistantstore.RunStatusCompleted,
		Decision:  assistantstore.RunDecisionRecommend,
		Trigger:   assistantstore.RunTrigger{Kind: "manual", Label: "Manual proactive check"},
		Autonomy:  assistantstore.RunAutonomyPropose,
		Summary:   "Action recommended.",
		CreatedAt: now,
		UpdatedAt: now,
		RecommendedActions: []assistantstore.RunAction{
			{
				ID:        "action_1",
				Kind:      "task",
				Title:     "Review restart gate",
				Rationale: "The task queue shows a failed restart gate.",
				TaskGoal:  "Review the dashboard restart gate and decide the recovery path.",
			},
		},
	})
	store, err := orch.assistantRunStore()
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(run); err != nil {
		t.Fatal(err)
	}

	updated, _, err := orch.UpdateAssistantRunAction(context.Background(), run.ID, "action_1", assistantstore.SignalFeedbackRequest{Feedback: assistantstore.SignalFeedbackCreateTask})
	if err != nil {
		t.Fatal(err)
	}
	createdTaskID := updated.RecommendedActions[0].CreatedTaskID
	if updated.RecommendedActions[0].Status != assistantstore.SignalStatusCreatedTask || createdTaskID == "" {
		t.Fatalf("updated action = %#v, want created task", updated.RecommendedActions[0])
	}
	updatedAgain, _, err := orch.UpdateAssistantRunAction(context.Background(), run.ID, "action_1", assistantstore.SignalFeedbackRequest{Feedback: assistantstore.SignalFeedbackCreateTask})
	if err != nil {
		t.Fatal(err)
	}
	if updatedAgain.RecommendedActions[0].CreatedTaskID != createdTaskID {
		t.Fatalf("created task id changed from %q to %q", createdTaskID, updatedAgain.RecommendedActions[0].CreatedTaskID)
	}
	tasks, err := orch.ListTasks()
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 {
		t.Fatalf("tasks = %d, want one deduped task", len(tasks))
	}
}

func TestAssistantTriggerFromEventsChoosesLatestActionableEvent(t *testing.T) {
	base := time.Date(2026, 5, 5, 12, 0, 0, 0, time.UTC)
	trigger, ok := assistantTriggerFromEvents([]eventlog.Event{
		{ID: "evt_info", Time: base, Type: "task.created", TaskID: "task_new"},
		{ID: "evt_blocked", Time: base.Add(time.Second), Type: "task.blocked", TaskID: "task_20260505_120000_blocked1"},
		{ID: "evt_review", Time: base.Add(2 * time.Second), Type: "task.review.failed", TaskID: "task_20260505_120000_review99"},
	})

	if !ok {
		t.Fatal("assistantTriggerFromEvents did not find an actionable event")
	}
	if trigger.Label != "Task review failed: review99" {
		t.Fatalf("label = %q, want latest actionable review failure", trigger.Label)
	}
	if trigger.Goal == "" || trigger.Goal == "Task review failed: review99" {
		t.Fatalf("goal = %q, want explanatory event goal", trigger.Goal)
	}
}

func TestAssistantEventsAfterReadsAcrossMidnight(t *testing.T) {
	orch := newTestOrchestrator(t, nil)
	beforeMidnight := time.Date(2026, 5, 5, 23, 59, 59, 0, time.UTC)
	afterMidnight := beforeMidnight.Add(2 * time.Second)
	events := []eventlog.Event{
		{ID: id.New("evt"), Time: beforeMidnight, Type: "task.blocked", TaskID: "task_before"},
		{ID: id.New("evt"), Time: afterMidnight, Type: "approval.requested", TaskID: "task_after"},
	}
	for _, event := range events {
		if err := orch.events.Append(context.Background(), event); err != nil {
			t.Fatal(err)
		}
	}

	got := orch.assistantEventsAfter(beforeMidnight.Add(-time.Second), afterMidnight.Add(time.Second))

	if len(got) != 2 {
		t.Fatalf("events = %d, want 2", len(got))
	}
	if got[0].TaskID != "task_before" || got[1].TaskID != "task_after" {
		t.Fatalf("events = %#v, want chronological cross-day events", got)
	}
}
