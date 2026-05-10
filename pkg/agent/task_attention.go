package agent

import (
	taskstore "github.com/andrewneudegg/lab/pkg/task"
	approvalstore "github.com/andrewneudegg/lab/pkg/tools/approval"
)

type TaskAttentionCounts struct {
	Red   int `json:"red"`
	Amber int `json:"amber"`
	Total int `json:"total"`
}

func TaskAttentionCountsFor(tasks []taskstore.Task, approvals []approvalstore.Request) TaskAttentionCounts {
	actionableTaskIDs := map[string]struct{}{}
	counts := TaskAttentionCounts{}
	for _, task := range tasks {
		if !taskNeedsDashboardAttention(task, approvals) {
			continue
		}
		actionableTaskIDs[task.ID] = struct{}{}
		if taskNeedsCriticalAttention(task) {
			counts.Red++
		} else {
			counts.Amber++
		}
	}
	for _, approval := range pendingActionableApprovals(approvals, tasks) {
		if approval.TaskID == "" {
			counts.Amber++
			continue
		}
		if _, seen := actionableTaskIDs[approval.TaskID]; !seen {
			counts.Amber++
		}
	}
	counts.Total = counts.Red + counts.Amber
	return counts
}

func taskNeedsDashboardAttention(task taskstore.Task, approvals []approvalstore.Request) bool {
	if taskHasActionableApproval(task, approvals) {
		return true
	}
	if taskIsGraphParent(task) || taskIsBlockedByGraphDependency(task) {
		return false
	}
	return taskNeedsAttention(task)
}

func taskNeedsAttention(task taskstore.Task) bool {
	switch task.Status {
	case taskstore.StatusBlocked,
		taskstore.StatusTimedOut,
		taskstore.StatusConflictResolution,
		taskstore.StatusFailed,
		taskstore.StatusReadyForReview,
		taskstore.StatusAwaitingApproval,
		taskstore.StatusAwaitingRestart,
		taskstore.StatusAwaitingVerification,
		taskstore.StatusNoChangeRequired:
		return true
	default:
		return false
	}
}

func taskNeedsCriticalAttention(task taskstore.Task) bool {
	switch task.Status {
	case taskstore.StatusBlocked,
		taskstore.StatusTimedOut,
		taskstore.StatusConflictResolution,
		taskstore.StatusFailed:
		return true
	default:
		return false
	}
}

func taskHasActionableApproval(task taskstore.Task, approvals []approvalstore.Request) bool {
	if taskIsTerminal(task) {
		return false
	}
	for _, approval := range approvals {
		if approval.Status == approvalstore.StatusPending && approval.TaskID == task.ID {
			return true
		}
	}
	return false
}

func pendingActionableApprovals(approvals []approvalstore.Request, tasks []taskstore.Task) []approvalstore.Request {
	tasksByID := map[string]taskstore.Task{}
	for _, task := range tasks {
		tasksByID[task.ID] = task
	}
	out := []approvalstore.Request{}
	for _, approval := range approvals {
		if approval.Status != approvalstore.StatusPending {
			continue
		}
		if approval.TaskID == "" {
			out = append(out, approval)
			continue
		}
		task, ok := tasksByID[approval.TaskID]
		if !ok || !taskIsTerminal(task) {
			out = append(out, approval)
		}
	}
	return out
}

func taskIsTerminal(task taskstore.Task) bool {
	switch task.Status {
	case taskstore.StatusDone, taskstore.StatusCancelled:
		return true
	default:
		return false
	}
}

func taskIsGraphParent(task taskstore.Task) bool {
	return task.GraphPhase == "root" && task.ParentID == ""
}

func taskIsBlockedByGraphDependency(task taskstore.Task) bool {
	return task.Status == taskstore.StatusBlocked &&
		task.ParentID != "" &&
		task.GraphPhase != "" &&
		len(task.BlockedBy) > 0
}
