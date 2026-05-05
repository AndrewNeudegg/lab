package agent

import (
	"context"
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
	if run.RecommendedActions[0].Status != "created" || run.RecommendedActions[0].CreatedTaskID == "" {
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
