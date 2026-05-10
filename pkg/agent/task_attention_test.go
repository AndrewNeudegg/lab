package agent

import (
	"testing"

	taskstore "github.com/andrewneudegg/lab/pkg/task"
	approvalstore "github.com/andrewneudegg/lab/pkg/tools/approval"
)

func TestTaskAttentionCountsForMatchesDashboardRules(t *testing.T) {
	tasks := []taskstore.Task{
		{ID: "blocked", Status: taskstore.StatusBlocked},
		{ID: "review", Status: taskstore.StatusReadyForReview},
		{ID: "parent", Status: taskstore.StatusBlocked, GraphPhase: "root"},
		{ID: "dependency", Status: taskstore.StatusBlocked, ParentID: "parent", GraphPhase: "phase", BlockedBy: []string{"other"}},
		{ID: "done", Status: taskstore.StatusDone},
		{ID: "queued", Status: taskstore.StatusQueued},
	}
	approvals := []approvalstore.Request{
		{ID: "app_task", TaskID: "queued", Status: approvalstore.StatusPending},
		{ID: "app_done", TaskID: "done", Status: approvalstore.StatusPending},
		{ID: "app_global", Status: approvalstore.StatusPending},
		{ID: "app_old", Status: approvalstore.StatusGranted},
	}

	counts := TaskAttentionCountsFor(tasks, approvals)
	if counts.Red != 1 || counts.Amber != 3 || counts.Total != 4 {
		t.Fatalf("counts = %#v, want 1 red, 3 amber, 4 total", counts)
	}
}
