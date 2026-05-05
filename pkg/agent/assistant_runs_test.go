package agent

import (
	"context"
	"testing"

	assistantstore "github.com/andrewneudegg/lab/pkg/assistant"
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
