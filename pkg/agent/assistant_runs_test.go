package agent

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	assistantstore "github.com/andrewneudegg/lab/pkg/assistant"
	"github.com/andrewneudegg/lab/pkg/config"
	"github.com/andrewneudegg/lab/pkg/eventlog"
	"github.com/andrewneudegg/lab/pkg/id"
	"github.com/andrewneudegg/lab/pkg/llm"
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

func TestAssistantRunCreateTaskBudgetCapsExecuteSafeActions(t *testing.T) {
	orch := newTestOrchestrator(t, nil)
	orch.cfg.Assistant.CreateTasksMaxPerRun = 1
	run := assistantstore.Run{
		Autonomy: assistantstore.RunAutonomyExecuteSafe,
		Decision: assistantstore.RunDecisionRecommend,
		RecommendedActions: []assistantstore.RunAction{
			{
				ID:        "action_1",
				Kind:      "task",
				Title:     "Review first signal",
				Rationale: "The first signal is safe to turn into work.",
				TaskGoal:  "Review the first proactive signal.",
			},
			{
				ID:        "action_2",
				Kind:      "task",
				Title:     "Review second signal",
				Rationale: "This should wait for the next run budget.",
				TaskGoal:  "Review the second proactive signal.",
			},
		},
	}

	orch.applyAssistantRunActions(context.Background(), &run)

	if run.Decision != assistantstore.RunDecisionCreated {
		t.Fatalf("decision = %q, want created_tasks", run.Decision)
	}
	if run.RecommendedActions[0].Status != assistantstore.SignalStatusCreatedTask || run.RecommendedActions[0].CreatedTaskID == "" {
		t.Fatalf("first action = %#v, want created task", run.RecommendedActions[0])
	}
	if run.RecommendedActions[1].Status != "skipped" || run.RecommendedActions[1].CreatedTaskID != "" {
		t.Fatalf("second action = %#v, want skipped by budget", run.RecommendedActions[1])
	}
	var budgetReceipt bool
	for _, receipt := range run.Receipts {
		if receipt.Kind == "task_budget_exhausted" && receipt.ObjectID == "action_2" {
			budgetReceipt = true
		}
	}
	if !budgetReceipt {
		t.Fatalf("receipts = %#v, want task_budget_exhausted receipt", run.Receipts)
	}
	tasks, err := orch.ListTasks()
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 {
		t.Fatalf("tasks = %d, want one task created within budget", len(tasks))
	}
}

func TestAssistantCapabilityRouterLabelsSurfaceAndApprovalNeed(t *testing.T) {
	proposeTask := assistantCapabilityRouteForRun(assistantstore.Run{
		Status:   assistantstore.RunStatusCompleted,
		Autonomy: assistantstore.RunAutonomyPropose,
		Summary:  "Task follow-up recommended.",
		RecommendedActions: []assistantstore.RunAction{{
			ID:        "action_1",
			Kind:      "task",
			Title:     "Review task",
			Rationale: "A task needs operator review.",
			Status:    "recommended",
		}},
	})
	if proposeTask.Capability != "tasks" || proposeTask.Decision != "propose_task" || !proposeTask.RequiresApproval {
		t.Fatalf("task route = %#v, want task proposal requiring approval", proposeTask)
	}

	runWorkflow := assistantCapabilityRouteForRun(assistantstore.Run{
		Status:   assistantstore.RunStatusCompleted,
		Autonomy: assistantstore.RunAutonomyRunWorkflows,
		RecommendedActions: []assistantstore.RunAction{{
			ID:           "action_2",
			Kind:         "workflow",
			Title:        "Run workflow",
			WorkflowHint: "Run the approved workflow template.",
			Status:       "recommended",
		}},
	})
	if runWorkflow.Capability != "workflows" || runWorkflow.Decision != "prepare_workflow" || runWorkflow.RequiresApproval {
		t.Fatalf("workflow route = %#v, want workflow route without extra approval", runWorkflow)
	}

	failed := assistantCapabilityRouteForRun(assistantstore.Run{
		Status: assistantstore.RunStatusFailed,
		Error:  "provider returned invalid content",
	})
	if failed.Capability != "diagnose" || failed.Decision != "review_error" || !strings.Contains(failed.Reason, "invalid content") {
		t.Fatalf("failed route = %#v, want diagnose route", failed)
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
	if blocked.WhyNow == "" || len(blocked.Evidence) == 0 || len(blocked.SafeActions) == 0 || blocked.SuggestedNextStep == "" {
		t.Fatalf("blocked task signal = %#v, want generic evidence packet", blocked)
	}
	if blocked.Evidence[0].Source != "tasks" || blocked.Evidence[0].Kind == "" {
		t.Fatalf("blocked task evidence = %#v, want source-neutral evidence", blocked.Evidence)
	}
	if !assistantStringSliceContains(blocked.SafeActions, "create_task") || !assistantStringSliceContains(blocked.SafeActions, "snooze") {
		t.Fatalf("blocked task safe actions = %#v, want create_task and snooze", blocked.SafeActions)
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

func TestAssistantWatchlistSignalsIncludesSubmittedCandidates(t *testing.T) {
	orch := newTestOrchestrator(t, nil)
	now := time.Now().UTC()
	candidate, err := orch.SubmitAssistantSignal(context.Background(), assistantstore.SignalSubmitRequest{
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
		Evidence:          []assistantstore.RunSignalEvidence{{Source: "chat", Kind: "user_feedback", Title: "Operator feedback", Detail: "That was wrong.", ObjectID: "evt_user", Weight: 88}},
		SafeActions:       []string{"create_task", "useful", "snooze", "dismiss"},
		SuggestedNextStep: "Create follow-up work to inspect the exchange.",
	})
	if err != nil {
		t.Fatal(err)
	}

	signals := orch.assistantWatchlistSignals(assistantstore.RunSnapshot{}, now)

	var found assistantstore.RunSignal
	for _, signal := range signals {
		if signal.Fingerprint == candidate.Fingerprint {
			found = signal
			break
		}
	}
	if found.Fingerprint == "" {
		t.Fatalf("signals = %#v, want submitted chat candidate", signals)
	}
	if found.Surface != "chat" || found.Score < 80 || len(found.Evidence) == 0 || !assistantStringSliceContains(found.SafeActions, "create_task") {
		t.Fatalf("signal = %#v, want rich generic chat signal", found)
	}
}

func TestSubmitAssistantSignalAppliesSourceControlsAndCooldown(t *testing.T) {
	orch := newTestOrchestrator(t, nil)
	allowed := true
	orch.cfg.Assistant.SignalSources = map[string]config.AssistantSignalSourceConfig{
		"chat": {
			Enabled:         &allowed,
			MinScore:        70,
			CooldownSeconds: 300,
			SafeActions:     []string{"useful", "snooze", "dismiss"},
		},
	}
	req := assistantstore.SignalSubmitRequest{
		Source:      "chat",
		Kind:        "chat_tool_light_response",
		Title:       "Check tool-light chat response",
		Surface:     "chat",
		ObjectID:    "evt_user",
		Score:       69,
		ActionKind:  "task",
		SafeActions: []string{"create_task", "useful"},
	}
	if _, err := orch.SubmitAssistantSignal(context.Background(), req); err == nil {
		t.Fatal("SubmitAssistantSignal accepted score below min_score")
	}

	req.Score = 74
	first, err := orch.SubmitAssistantSignal(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	second, err := orch.SubmitAssistantSignal(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if first.Fingerprint != second.Fingerprint || second.SeenCount != first.SeenCount {
		t.Fatalf("cooldown candidate = %#v after %#v, want existing candidate without increment", second, first)
	}
	if assistantStringSliceContains(first.SafeActions, "create_task") || !assistantStringSliceContains(first.SafeActions, "useful") {
		t.Fatalf("safe actions = %#v, want source-controlled actions", first.SafeActions)
	}
}

func TestUpdateAssistantSignalCandidateStoresFeedbackAndCreatesTask(t *testing.T) {
	orch := newTestOrchestrator(t, nil)
	candidate, err := orch.SubmitAssistantSignal(context.Background(), assistantstore.SignalSubmitRequest{
		Source:            "chat",
		Kind:              "chat_quality_feedback",
		Title:             "Review subpar chat answer",
		Surface:           "chat",
		ObjectID:          "evt_chat",
		Score:             88,
		ActionKind:        "task",
		Rationale:         "Operator feedback flagged a poor answer.",
		TaskGoal:          "Review the chat exchange and improve the response path.",
		SafeActions:       []string{"create_task", "useful", "snooze", "dismiss"},
		SuggestedNextStep: "Create follow-up work for the chat exchange.",
	})
	if err != nil {
		t.Fatal(err)
	}

	useful, reply, err := orch.UpdateAssistantSignalCandidate(context.Background(), candidate.Fingerprint, assistantstore.SignalFeedbackRequest{Feedback: assistantstore.SignalFeedbackUseful})
	if err != nil {
		t.Fatal(err)
	}
	if reply != "Marked signal as useful." || useful.UsefulCount != 1 {
		t.Fatalf("useful signal = %#v reply %q, want useful count", useful, reply)
	}
	if !useful.Suppressed || !strings.Contains(useful.SuppressionReason, "cleared") {
		t.Fatalf("useful signal = %#v, want cleared from active inbox", useful)
	}

	created, reply, err := orch.UpdateAssistantSignalCandidate(context.Background(), candidate.Fingerprint, assistantstore.SignalFeedbackRequest{Feedback: assistantstore.SignalFeedbackCreateTask})
	if err != nil {
		t.Fatal(err)
	}
	if reply != "Created task from signal." || created.CreatedTaskID == "" || !created.Suppressed {
		t.Fatalf("created signal = %#v reply %q, want created task suppression", created, reply)
	}
	tasks, err := orch.ListTasks()
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 || tasks[0].ID != created.CreatedTaskID {
		t.Fatalf("tasks = %#v, want one created signal task", tasks)
	}
}

func TestAssistantSignalUsefulFeedbackClearsInboxUntilNewObservation(t *testing.T) {
	orch := newTestOrchestrator(t, nil)
	allowed := true
	orch.cfg.Assistant.SignalSources = map[string]config.AssistantSignalSourceConfig{
		"chat": {Enabled: &allowed, SafeActions: []string{"create_task", "useful", "snooze", "dismiss"}},
	}
	now := time.Date(2026, 5, 6, 12, 0, 0, 0, time.UTC)
	candidate, err := orch.SubmitAssistantSignal(context.Background(), assistantstore.SignalSubmitRequest{
		Source:      "chat",
		Kind:        "chat_quality_feedback",
		Title:       "Review subpar chat answer",
		Surface:     "chat",
		ObjectID:    "evt_chat",
		Score:       88,
		ActionKind:  "task",
		TaskGoal:    "Review the chat exchange.",
		SafeActions: []string{"create_task", "useful", "snooze", "dismiss"},
		ObservedAt:  now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := orch.UpdateAssistantSignalCandidate(context.Background(), candidate.Fingerprint, assistantstore.SignalFeedbackRequest{Feedback: assistantstore.SignalFeedbackUseful}); err != nil {
		t.Fatal(err)
	}
	cleared, err := orch.ListAssistantSignalCandidates()
	if err != nil {
		t.Fatal(err)
	}
	if len(cleared) != 0 {
		t.Fatalf("signals = %#v, want useful signal cleared from active inbox", cleared)
	}

	if _, err := orch.SubmitAssistantSignal(context.Background(), assistantstore.SignalSubmitRequest{
		Source:      "chat",
		Kind:        "chat_quality_feedback",
		Title:       "Review subpar chat answer",
		Surface:     "chat",
		ObjectID:    "evt_chat",
		Score:       88,
		ActionKind:  "task",
		TaskGoal:    "Review the chat exchange again.",
		SafeActions: []string{"create_task", "useful", "snooze", "dismiss"},
		ObservedAt:  time.Now().UTC().Add(time.Second),
	}); err != nil {
		t.Fatal(err)
	}
	reopened, err := orch.ListAssistantSignalCandidates()
	if err != nil {
		t.Fatal(err)
	}
	if len(reopened) != 1 || reopened[0].UsefulCount != 1 || reopened[0].Suppressed {
		t.Fatalf("signals = %#v, want new sighting reopened with useful memory", reopened)
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
		Kind:       "task_blocked",
		Title:      "Unblock task: Deploy dashboard",
		Detail:     "The task is blocked on an operator decision.",
		Severity:   "warning",
		Surface:    "tasks",
		ObjectID:   "task_blocked",
		ObjectURL:  "/tasks?task=task_blocked",
		Score:      90,
		Confidence: "high",
		Priority:   "high",
		ActionKind: "task",
		Rationale:  "The task is in an operator attention state.",
		TaskGoal:   "Review task_blocked and decide the next step.",
		WhyNow:     "The task crossed the proactive attention threshold.",
		Evidence: []assistantstore.RunSignalEvidence{
			{Source: "tasks", Kind: "task_status", Title: "Blocked deploy", Detail: "Status: blocked", ObjectID: "task_blocked", ObjectURL: "/tasks?task=task_blocked", Weight: 90},
		},
		SafeActions:       []string{"create_task", "useful", "snooze", "dismiss"},
		SuggestedNextStep: "Review the blocked task and choose a recovery path.",
		Fingerprint:       assistantSignalFingerprint("task", "blocked", "task_blocked", "Unblock task: Deploy dashboard"),
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
	if !strings.Contains(provider.requests[0].Messages[1].Content, `"evidence"`) || !strings.Contains(provider.requests[0].Messages[1].Content, `"safe_actions"`) {
		t.Fatalf("provider request did not include generic signal evidence fields: %s", provider.requests[0].Messages[1].Content)
	}
	if !strings.Contains(provider.requests[0].Messages[0].Content, "any source can plug in") {
		t.Fatalf("provider system prompt did not describe generic signal sources: %s", provider.requests[0].Messages[0].Content)
	}
}

func TestAssistantRunFallsBackWhenModelReturnsInvalidJSON(t *testing.T) {
	cases := []struct {
		name    string
		content string
	}{
		{name: "empty", content: ""},
		{name: "partial", content: `{"decision":`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			orch := newTestOrchestrator(t, nil)
			orch.provider = &staticProvider{content: tc.content}
			orch.model = "assistant-model"

			run, reply, err := orch.StartAssistantRun(context.Background(), assistantstore.RunRequest{
				TriggerLabel: "Regression invalid JSON check",
			})
			if err != nil {
				t.Fatalf("StartAssistantRun returned error: %v", err)
			}

			if run.Status != assistantstore.RunStatusCompleted || run.Error != "" {
				t.Fatalf("run status/error = %q/%q, want completed without error", run.Status, run.Error)
			}
			if !strings.Contains(reply, "completed") {
				t.Fatalf("reply = %q, want completed", reply)
			}
			if len(run.Changed) == 0 || !strings.Contains(run.Changed[0], "model output was not valid JSON") {
				t.Fatalf("changed = %#v, want invalid JSON fallback note", run.Changed)
			}
			for _, receipt := range run.Receipts {
				if receipt.Kind == "error" {
					t.Fatalf("receipts = %#v, want no error receipt for fallback", run.Receipts)
				}
			}
			if len(run.Receipts) == 0 || run.Receipts[len(run.Receipts)-1].Kind != "decision" {
				t.Fatalf("receipts = %#v, want decision receipt", run.Receipts)
			}
			stored, err := orch.LoadAssistantRun(run.ID)
			if err != nil {
				t.Fatal(err)
			}
			if stored.Status != assistantstore.RunStatusCompleted || len(stored.Changed) == 0 {
				t.Fatalf("stored run = %#v, want persisted fallback decision", stored)
			}
		})
	}
}

func TestAssistantRunFallsBackWhenModelCallFails(t *testing.T) {
	orch := newTestOrchestrator(t, nil)
	orch.provider = failingAssistantProvider{err: llm.Retryable(errors.New("gemini provider returned empty content: MAX_TOKENS"))}
	orch.model = "assistant-model"

	run, _, err := orch.StartAssistantRun(context.Background(), assistantstore.RunRequest{
		TriggerLabel: "Regression provider failure check",
	})
	if err != nil {
		t.Fatalf("StartAssistantRun returned error: %v", err)
	}

	if run.Status != assistantstore.RunStatusCompleted || run.Error != "" {
		t.Fatalf("run status/error = %q/%q, want completed without error", run.Status, run.Error)
	}
	if run.Provider != "failing-assistant-provider" || run.Model != "assistant-model" {
		t.Fatalf("provider/model = %q/%q, want fallback provenance", run.Provider, run.Model)
	}
	if len(run.Changed) == 0 || !strings.Contains(run.Changed[0], "model call failed") {
		t.Fatalf("changed = %#v, want model call fallback note", run.Changed)
	}
}

func TestAssistantRunDecisionAcceptsGenericSignalSource(t *testing.T) {
	signal := newAssistantRunSignal(assistantstore.RunSignal{
		Kind:              "chat_quality_regression",
		Title:             "Review subpar chat answer",
		Detail:            "A chat message was marked as not useful after a tool-light response.",
		WhyNow:            "Operator feedback identified a poor answer that may need follow-up work.",
		Severity:          "warning",
		Surface:           "chat",
		ObjectID:          "message_assistant_42",
		ObjectURL:         "/chat#message-assistant-42",
		Score:             88,
		ActionKind:        "task",
		Rationale:         "Subpar assistant responses are useful signals for improving the harness.",
		Evidence:          []assistantstore.RunSignalEvidence{{Source: "chat", Kind: "feedback", Title: "Negative chat feedback", Detail: "Marked not useful", ObjectID: "message_assistant_42", ObjectURL: "/chat#message-assistant-42", Weight: 88}},
		SafeActions:       []string{"create_task", "useful", "snooze", "dismiss"},
		SuggestedNextStep: "Create follow-up work to review the conversation and improve the response path.",
		Fingerprint:       assistantSignalFingerprint("chat", "quality", "message_assistant_42", "Review subpar chat answer"),
	})
	run := assistantstore.Run{
		Snapshot: assistantstore.RunSnapshot{Signals: []assistantstore.RunSignal{signal}},
	}

	decision := assistantRunDecisionWithSignals(run, assistantRunDecision{
		Decision:           assistantstore.RunDecisionNoop,
		Summary:            "Nothing new.",
		Changed:            []string{},
		Concerns:           []assistantstore.RunFinding{},
		Opportunities:      []assistantstore.RunFinding{},
		RecommendedActions: []assistantstore.RunAction{},
	})

	if decision.Decision != assistantstore.RunDecisionRecommend {
		t.Fatalf("decision = %q, want recommend", decision.Decision)
	}
	if len(decision.RecommendedActions) != 1 {
		t.Fatalf("actions = %#v, want one generic-source action", decision.RecommendedActions)
	}
	action := decision.RecommendedActions[0]
	if action.Fingerprint != signal.Fingerprint || action.TargetSurface != "chat" {
		t.Fatalf("action = %#v, want chat signal fingerprint and surface", action)
	}
	if action.TaskGoal != signal.SuggestedNextStep {
		t.Fatalf("task goal = %q, want suggested next step", action.TaskGoal)
	}
	if len(decision.Concerns) != 1 || decision.Concerns[0].ObjectURL != "/chat#message-assistant-42" {
		t.Fatalf("concerns = %#v, want generic chat concern", decision.Concerns)
	}
}

func TestAssistantRunDecisionRespectsSignalSafeActions(t *testing.T) {
	signal := newAssistantRunSignal(assistantstore.RunSignal{
		Kind:              "email_invoice_question",
		Title:             "Review email before creating work",
		Detail:            "An email may need follow-up, but the source only allows observation right now.",
		Surface:           "email",
		ObjectID:          "email_42",
		ObjectURL:         "/email/email_42",
		Score:             91,
		ActionKind:        "task",
		Rationale:         "Email context is not yet approved for task creation.",
		Evidence:          []assistantstore.RunSignalEvidence{{Source: "email", Kind: "message", Title: "Email message", Detail: "Potential invoice follow-up", ObjectID: "email_42", Weight: 91}},
		SafeActions:       []string{"useful", "snooze", "dismiss"},
		SuggestedNextStep: "Observe the email signal until task creation is allowed for this source.",
		Fingerprint:       assistantSignalFingerprint("email", "invoice", "email_42", "Review email before creating work"),
	})
	run := assistantstore.Run{
		Snapshot: assistantstore.RunSnapshot{Signals: []assistantstore.RunSignal{signal}},
	}

	decision := assistantRunDecisionWithSignals(run, assistantRunDecision{
		Decision:           assistantstore.RunDecisionNoop,
		Summary:            "Nothing new.",
		Changed:            []string{},
		Concerns:           []assistantstore.RunFinding{},
		Opportunities:      []assistantstore.RunFinding{},
		RecommendedActions: []assistantstore.RunAction{},
	})

	if len(decision.RecommendedActions) != 0 {
		t.Fatalf("actions = %#v, want no task action without create_task safe action", decision.RecommendedActions)
	}
	if len(decision.Concerns) != 1 || decision.Concerns[0].Surface != "email" {
		t.Fatalf("concerns = %#v, want visible email concern", decision.Concerns)
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

func TestAssistantRunUsefulFeedbackArchivesSettledDecision(t *testing.T) {
	orch := newTestOrchestrator(t, nil)
	now := time.Date(2026, 5, 5, 12, 0, 0, 0, time.UTC)
	run := assistantstore.NormalizeRun(assistantstore.Run{
		ID:        "arun_useful",
		Status:    assistantstore.RunStatusCompleted,
		Decision:  assistantstore.RunDecisionRecommend,
		Trigger:   assistantstore.RunTrigger{Kind: "manual", Label: "Manual proactive check"},
		Autonomy:  assistantstore.RunAutonomyPropose,
		Summary:   "Action recommended.",
		CreatedAt: now,
		UpdatedAt: now,
		RecommendedActions: []assistantstore.RunAction{{
			ID:        "action_1",
			Kind:      "task",
			Title:     "Review useful signal",
			Rationale: "The signal was useful training feedback.",
			TaskGoal:  "Review the useful signal.",
		}},
	})
	store, err := orch.assistantRunStore()
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(run); err != nil {
		t.Fatal(err)
	}

	updated, reply, err := orch.UpdateAssistantRunAction(context.Background(), run.ID, "action_1", assistantstore.SignalFeedbackRequest{Feedback: assistantstore.SignalFeedbackUseful})
	if err != nil {
		t.Fatal(err)
	}
	if reply != "Marked recommendation as useful." {
		t.Fatalf("reply = %q, want useful reply", reply)
	}
	if !updated.Archived || updated.ArchivedBy != "assistant-lifecycle" || !strings.Contains(updated.ArchivedReason, "resolved") {
		t.Fatalf("updated run = %#v, want lifecycle archive after useful feedback", updated)
	}
	if updated.RecommendedActions[0].Status != assistantstore.SignalStatusUseful {
		t.Fatalf("action = %#v, want useful status", updated.RecommendedActions[0])
	}
	runs, err := orch.ListAssistantRuns()
	if err != nil {
		t.Fatal(err)
	}
	for _, listed := range runs {
		if listed.ID == run.ID && !listed.Archived {
			t.Fatalf("runs = %#v, useful-settled run should not remain active", runs)
		}
	}
}

func TestAssistantRunLifecycleAutoArchivesOldResolvedDecisions(t *testing.T) {
	orch := newTestOrchestrator(t, nil)
	orch.cfg.Assistant.DecisionNoopAutoArchiveSeconds = 60
	orch.cfg.Assistant.DecisionSettledAutoArchiveSeconds = 60
	store, err := orch.assistantRunStore()
	if err != nil {
		t.Fatal(err)
	}
	old := time.Date(2026, 5, 6, 8, 0, 0, 0, time.UTC)
	if err := store.Save(assistantstore.Run{
		ID:         "arun_noop",
		Status:     assistantstore.RunStatusCompleted,
		Decision:   assistantstore.RunDecisionNoop,
		Trigger:    assistantstore.RunTrigger{Kind: "schedule", Label: "Old no-op"},
		Autonomy:   assistantstore.RunAutonomyObserve,
		Summary:    "No action.",
		Snapshot:   assistantstore.RunSnapshot{GeneratedAt: old},
		CreatedAt:  old,
		FinishedAt: old,
		UpdatedAt:  old,
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(assistantstore.Run{
		ID:       "arun_settled",
		Status:   assistantstore.RunStatusCompleted,
		Decision: assistantstore.RunDecisionRecommend,
		Trigger:  assistantstore.RunTrigger{Kind: "manual", Label: "Old settled"},
		Autonomy: assistantstore.RunAutonomyPropose,
		Summary:  "Action settled.",
		RecommendedActions: []assistantstore.RunAction{{
			ID:        "action_1",
			Kind:      "task",
			Title:     "Review old item",
			Rationale: "Already dismissed.",
			Status:    assistantstore.SignalStatusDismissed,
		}},
		Snapshot:   assistantstore.RunSnapshot{GeneratedAt: old},
		CreatedAt:  old,
		FinishedAt: old,
		UpdatedAt:  old,
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(assistantstore.Run{
		ID:         "arun_failed",
		Status:     assistantstore.RunStatusFailed,
		Decision:   assistantstore.RunDecisionNoop,
		Trigger:    assistantstore.RunTrigger{Kind: "manual", Label: "Failed without decision"},
		Autonomy:   assistantstore.RunAutonomyPropose,
		Summary:    "Failed before decision.",
		Error:      "invalid JSON",
		Snapshot:   assistantstore.RunSnapshot{GeneratedAt: old},
		CreatedAt:  old,
		FinishedAt: old,
		UpdatedAt:  old,
	}); err != nil {
		t.Fatal(err)
	}

	archived, err := orch.maintainAssistantRuns(context.Background(), store, old.Add(2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if archived != 3 {
		t.Fatalf("archived = %d, want 3", archived)
	}
	for _, id := range []string{"arun_noop", "arun_settled", "arun_failed"} {
		run, err := store.Load(id)
		if err != nil {
			t.Fatal(err)
		}
		if !run.Archived || run.ArchivedBy != "assistant-lifecycle" || len(run.Receipts) == 0 {
			t.Fatalf("run %s = %#v, want lifecycle archive receipt", id, run)
		}
	}
}

func TestAssistantRunLifecycleKeepsOnlyLatestNoopStatusActive(t *testing.T) {
	orch := newTestOrchestrator(t, nil)
	orch.cfg.Assistant.DecisionNoopAutoArchiveSeconds = 24 * 60 * 60
	store, err := orch.assistantRunStore()
	if err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 5, 7, 8, 0, 0, 0, time.UTC)
	for index, id := range []string{"arun_nominal_old", "arun_nominal_middle", "arun_nominal_latest"} {
		finished := base.Add(time.Duration(index) * time.Minute)
		if err := store.Save(assistantstore.Run{
			ID:         id,
			Status:     assistantstore.RunStatusCompleted,
			Decision:   assistantstore.RunDecisionNoop,
			Trigger:    assistantstore.RunTrigger{Kind: "schedule", Label: "All systems nominal"},
			Autonomy:   assistantstore.RunAutonomyObserve,
			Summary:    "All systems nominal.",
			Snapshot:   assistantstore.RunSnapshot{GeneratedAt: finished},
			CreatedAt:  finished,
			StartedAt:  finished,
			FinishedAt: finished,
			UpdatedAt:  finished,
		}); err != nil {
			t.Fatal(err)
		}
	}

	archived, err := orch.maintainAssistantRuns(context.Background(), store, base.Add(3*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if archived != 2 {
		t.Fatalf("archived = %d, want 2 older nominal runs", archived)
	}
	latest, err := store.Load("arun_nominal_latest")
	if err != nil {
		t.Fatal(err)
	}
	if latest.Archived {
		t.Fatalf("latest run = %#v, want active", latest)
	}
	for _, id := range []string{"arun_nominal_old", "arun_nominal_middle"} {
		run, err := store.Load(id)
		if err != nil {
			t.Fatal(err)
		}
		if !run.Archived || run.ArchivedBy != "assistant-lifecycle" || !strings.Contains(run.ArchivedReason, "newer no-action") {
			t.Fatalf("run %s = %#v, want archived as superseded by newer nominal status", id, run)
		}
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

func assistantStringSliceContains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

type failingAssistantProvider struct {
	err error
}

func (p failingAssistantProvider) Name() string { return "failing-assistant-provider" }

func (p failingAssistantProvider) Complete(context.Context, llm.CompletionRequest) (llm.CompletionResponse, error) {
	return llm.CompletionResponse{}, p.err
}
