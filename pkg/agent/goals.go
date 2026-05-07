package agent

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	assistantstore "github.com/andrewneudegg/lab/pkg/assistant"
	"github.com/andrewneudegg/lab/pkg/eventlog"
	"github.com/andrewneudegg/lab/pkg/id"
	taskstore "github.com/andrewneudegg/lab/pkg/task"
	approvalstore "github.com/andrewneudegg/lab/pkg/tools/approval"
)

func (o *Orchestrator) assistantGoalStore() (*assistantstore.GoalStore, error) {
	if strings.TrimSpace(o.cfg.DataDir) == "" {
		return nil, fmt.Errorf("assistant goal store is not configured")
	}
	return assistantstore.NewGoalStore(filepath.Join(o.cfg.DataDir, "assistant_goals")), nil
}

func (o *Orchestrator) ListGoals() ([]assistantstore.Goal, error) {
	store, err := o.assistantGoalStore()
	if err != nil {
		return nil, err
	}
	return store.ListGoals()
}

func (o *Orchestrator) LoadGoal(goalID string) (assistantstore.GoalTimeline, error) {
	store, err := o.assistantGoalStore()
	if err != nil {
		return assistantstore.GoalTimeline{}, err
	}
	goal, err := store.LoadGoal(goalID)
	if err != nil {
		return assistantstore.GoalTimeline{}, err
	}
	watches, err := store.ListWatches(goal.ID)
	if err != nil {
		return assistantstore.GoalTimeline{}, err
	}
	signals, err := store.ListSignals(goal.ID)
	if err != nil {
		return assistantstore.GoalTimeline{}, err
	}
	notes, err := store.ListNotes(goal.ID)
	if err != nil {
		return assistantstore.GoalTimeline{}, err
	}
	assessments, err := store.ListAssessments(goal.ID)
	if err != nil {
		return assistantstore.GoalTimeline{}, err
	}
	return assistantstore.GoalTimeline{
		Goal:        goal,
		Watches:     watches,
		Signals:     signals,
		Notes:       notes,
		Assessments: assessments,
	}, nil
}

func (o *Orchestrator) CreateGoal(ctx context.Context, req assistantstore.GoalCreateRequest) (assistantstore.GoalTimeline, error) {
	store, err := o.assistantGoalStore()
	if err != nil {
		return assistantstore.GoalTimeline{}, err
	}
	now := time.Now().UTC()
	nextCheckAt, err := assistantstore.ParseGoalTime(req.NextCheckAt)
	if err != nil {
		return assistantstore.GoalTimeline{}, fmt.Errorf("next_check_at must be RFC3339: %w", err)
	}
	title := strings.TrimSpace(req.Title)
	objective := strings.TrimSpace(req.Objective)
	if title == "" {
		title = objective
	}
	if objective == "" {
		objective = title
	}
	if title == "" {
		return assistantstore.GoalTimeline{}, fmt.Errorf("goal title or objective is required")
	}
	if nextCheckAt == nil {
		next := now
		nextCheckAt = &next
	}
	goal := assistantstore.Goal{
		ID:              id.New("goal"),
		Title:           title,
		Objective:       objective,
		Details:         req.Details,
		Status:          assistantstore.GoalStatusActive,
		Kind:            req.Kind,
		ExecutionMode:   req.ExecutionMode,
		Autopilot:       req.Autopilot,
		Priority:        req.Priority,
		Autonomy:        req.Autonomy,
		Cadence:         req.Cadence,
		NextCheckAt:     nextCheckAt,
		SuccessCriteria: req.SuccessCriteria,
		Constraints:     req.Constraints,
		OpenQuestions:   req.OpenQuestions,
		CreatedBy:       firstNonEmptyString(strings.TrimSpace(req.CreatedBy), "human"),
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	goal = assistantstore.NormalizeGoal(goal)
	if err := store.SaveGoal(goal); err != nil {
		return assistantstore.GoalTimeline{}, err
	}
	note := assistantstore.GoalNote{
		ID:        id.New("gnote"),
		GoalID:    goal.ID,
		Kind:      "created",
		Title:     "Goal created",
		Body:      "Initial objective: " + goal.Objective,
		CreatedBy: goal.CreatedBy,
		CreatedAt: now,
	}
	if err := store.SaveNote(note); err != nil {
		o.log().Warn("goal creation note failed", "error", err, "goal", goal.ID)
	}
	if goal.Cadence != "" {
		watch := assistantstore.GoalWatch{
			ID:              id.New("gwatch"),
			GoalID:          goal.ID,
			Title:           "Review cadence for " + goal.Title,
			Condition:       "goal_due",
			Source:          "assistant_goal",
			Cadence:         goal.Cadence,
			Severity:        "info",
			Status:          assistantstore.GoalWatchStatusActive,
			OnTrigger:       "create_signal",
			SuggestedAction: "Run a Goal assessment and decide whether a task, question, or progress update is needed.",
			CreatedAt:       now,
			UpdatedAt:       now,
		}
		if err := store.SaveWatch(watch); err != nil {
			o.log().Warn("goal cadence watch failed", "error", err, "goal", goal.ID)
		}
	}
	o.appendGoalEvent(ctx, "assistant.goal.created", goal, map[string]any{"goal": goal})
	return o.LoadGoal(goal.ID)
}

func (o *Orchestrator) UpdateGoal(ctx context.Context, goalID string, req assistantstore.GoalUpdateRequest) (assistantstore.GoalTimeline, error) {
	store, err := o.assistantGoalStore()
	if err != nil {
		return assistantstore.GoalTimeline{}, err
	}
	goal, err := store.LoadGoal(goalID)
	if err != nil {
		return assistantstore.GoalTimeline{}, err
	}
	now := time.Now().UTC()
	if value := strings.TrimSpace(req.Title); value != "" {
		goal.Title = value
	}
	if value := strings.TrimSpace(req.Objective); value != "" {
		goal.Objective = value
	}
	if strings.TrimSpace(req.Details) != "" {
		goal.Details = req.Details
	}
	if strings.TrimSpace(req.Status) != "" {
		goal.Status = req.Status
		if assistantstore.NormalizeGoal(goal).Status == assistantstore.GoalStatusArchived && goal.ArchivedAt == nil {
			archived := now
			goal.ArchivedAt = &archived
		}
	}
	if strings.TrimSpace(req.Kind) != "" {
		goal.Kind = req.Kind
	}
	if strings.TrimSpace(req.ExecutionMode) != "" {
		goal.ExecutionMode = req.ExecutionMode
	}
	if req.Autopilot != nil {
		goal.Autopilot = req.Autopilot
		if strings.TrimSpace(req.ExecutionMode) == "" {
			goal.ExecutionMode = assistantstore.GoalExecutionModeAutopilot
		}
	}
	if strings.TrimSpace(req.Priority) != "" {
		goal.Priority = req.Priority
	}
	if strings.TrimSpace(req.Autonomy) != "" {
		goal.Autonomy = req.Autonomy
	}
	if strings.TrimSpace(req.Cadence) != "" {
		goal.Cadence = req.Cadence
	}
	if strings.TrimSpace(req.NextCheckAt) != "" {
		nextCheckAt, err := assistantstore.ParseGoalTime(req.NextCheckAt)
		if err != nil {
			return assistantstore.GoalTimeline{}, fmt.Errorf("next_check_at must be RFC3339: %w", err)
		}
		goal.NextCheckAt = nextCheckAt
	}
	if len(req.SuccessCriteria) > 0 {
		goal.SuccessCriteria = req.SuccessCriteria
	}
	if len(req.Constraints) > 0 {
		goal.Constraints = req.Constraints
	}
	if strings.TrimSpace(req.ProgressSummary) != "" {
		goal.ProgressSummary = req.ProgressSummary
	}
	if len(req.OpenQuestions) > 0 {
		goal.OpenQuestions = req.OpenQuestions
	}
	goal.UpdatedAt = now
	goal = assistantstore.NormalizeGoal(goal)
	if goal.Status != assistantstore.GoalStatusArchived {
		goal.ArchivedAt = nil
	}
	if err := store.SaveGoal(goal); err != nil {
		return assistantstore.GoalTimeline{}, err
	}
	o.appendGoalEvent(ctx, "assistant.goal.updated", goal, map[string]any{"goal": goal})
	return o.LoadGoal(goal.ID)
}

func (o *Orchestrator) AddGoalWatch(ctx context.Context, goalID string, req assistantstore.GoalWatchRequest) (assistantstore.GoalTimeline, error) {
	store, err := o.assistantGoalStore()
	if err != nil {
		return assistantstore.GoalTimeline{}, err
	}
	goal, err := store.LoadGoal(goalID)
	if err != nil {
		return assistantstore.GoalTimeline{}, err
	}
	expiresAt, err := assistantstore.ParseGoalTime(req.ExpiresAt)
	if err != nil {
		return assistantstore.GoalTimeline{}, fmt.Errorf("expires_at must be RFC3339: %w", err)
	}
	title := strings.TrimSpace(req.Title)
	if title == "" {
		return assistantstore.GoalTimeline{}, fmt.Errorf("watch title is required")
	}
	now := time.Now().UTC()
	watch := assistantstore.GoalWatch{
		ID:              id.New("gwatch"),
		GoalID:          goal.ID,
		Title:           title,
		Condition:       req.Condition,
		Source:          req.Source,
		Cadence:         req.Cadence,
		Severity:        req.Severity,
		Status:          assistantstore.GoalWatchStatusActive,
		ExpiresAt:       expiresAt,
		OnTrigger:       req.OnTrigger,
		SuggestedAction: req.SuggestedAction,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	if err := store.SaveWatch(watch); err != nil {
		return assistantstore.GoalTimeline{}, err
	}
	_ = store.SaveNote(assistantstore.GoalNote{
		ID:        id.New("gnote"),
		GoalID:    goal.ID,
		Kind:      "watch",
		Title:     "Watch added",
		Body:      watch.Title,
		CreatedBy: "assistant",
		CreatedAt: now,
	})
	o.appendGoalEvent(ctx, "assistant.goal.watch.created", goal, map[string]any{"goal_id": goal.ID, "watch": watch})
	return o.LoadGoal(goal.ID)
}

func (o *Orchestrator) AddGoalNote(ctx context.Context, goalID string, req assistantstore.GoalNoteRequest) (assistantstore.GoalTimeline, error) {
	store, err := o.assistantGoalStore()
	if err != nil {
		return assistantstore.GoalTimeline{}, err
	}
	goal, err := store.LoadGoal(goalID)
	if err != nil {
		return assistantstore.GoalTimeline{}, err
	}
	body := strings.TrimSpace(req.Body)
	if body == "" {
		return assistantstore.GoalTimeline{}, fmt.Errorf("note body is required")
	}
	now := time.Now().UTC()
	note := assistantstore.GoalNote{
		ID:        id.New("gnote"),
		GoalID:    goal.ID,
		Kind:      req.Kind,
		Title:     req.Title,
		Body:      body,
		TaskID:    req.TaskID,
		RunID:     req.RunID,
		CreatedBy: firstNonEmptyString(strings.TrimSpace(req.CreatedBy), "human"),
		CreatedAt: now,
	}
	if err := store.SaveNote(note); err != nil {
		return assistantstore.GoalTimeline{}, err
	}
	o.appendGoalEvent(ctx, "assistant.goal.note.created", goal, map[string]any{"goal_id": goal.ID, "note": note})
	return o.LoadGoal(goal.ID)
}

func (o *Orchestrator) CheckGoal(ctx context.Context, goalID string) (assistantstore.Run, string, error) {
	store, err := o.assistantGoalStore()
	if err != nil {
		return assistantstore.Run{}, "", err
	}
	goal, err := store.LoadGoal(goalID)
	if err != nil {
		return assistantstore.Run{}, "", err
	}
	return o.StartAssistantRun(ctx, assistantstore.RunRequest{
		TriggerKind:  "goal",
		TriggerLabel: "Goal check: " + goal.Title,
		GoalID:       goal.ID,
		Goal:         goalRunRequestText(goal),
		Autonomy:     goal.Autonomy,
	})
}

func (o *Orchestrator) StartGoalAutopilot(ctx context.Context, goalID string, req assistantstore.GoalAutopilotRequest) (assistantstore.GoalTimeline, string, error) {
	return o.updateGoalAutopilotLifecycle(ctx, goalID, "start", req)
}

func (o *Orchestrator) PauseGoalAutopilot(ctx context.Context, goalID string) (assistantstore.GoalTimeline, string, error) {
	return o.updateGoalAutopilotLifecycle(ctx, goalID, "pause", assistantstore.GoalAutopilotRequest{})
}

func (o *Orchestrator) StopGoalAutopilot(ctx context.Context, goalID string) (assistantstore.GoalTimeline, string, error) {
	return o.updateGoalAutopilotLifecycle(ctx, goalID, "stop", assistantstore.GoalAutopilotRequest{})
}

func (o *Orchestrator) ResumeGoalAutopilot(ctx context.Context, goalID string, req assistantstore.GoalAutopilotRequest) (assistantstore.GoalTimeline, string, error) {
	return o.updateGoalAutopilotLifecycle(ctx, goalID, "resume", req)
}

func (o *Orchestrator) updateGoalAutopilotLifecycle(ctx context.Context, goalID, action string, req assistantstore.GoalAutopilotRequest) (assistantstore.GoalTimeline, string, error) {
	store, err := o.assistantGoalStore()
	if err != nil {
		return assistantstore.GoalTimeline{}, "", err
	}
	goal, err := store.LoadGoal(goalID)
	if err != nil {
		return assistantstore.GoalTimeline{}, "", err
	}
	if goal.Status == assistantstore.GoalStatusArchived {
		return assistantstore.GoalTimeline{}, "", fmt.Errorf("archived Goals cannot run Autopilot")
	}
	now := time.Now().UTC()
	goal.ExecutionMode = assistantstore.GoalExecutionModeAutopilot
	autopilot := assistantstore.NormalizeGoalAutopilot(goal.Autopilot)
	applyGoalAutopilotRequest(&autopilot, req)
	switch action {
	case "start":
		if autopilot.Status != assistantstore.GoalAutopilotStatusRunning && autopilot.Status != assistantstore.GoalAutopilotStatusPaused {
			autopilot.TasksStarted = 0
			autopilot.CurrentTaskID = ""
			autopilot.StopReasons = nil
			started := now
			autopilot.StartedAt = &started
		} else if autopilot.StartedAt == nil {
			started := now
			autopilot.StartedAt = &started
		}
		autopilot.Status = assistantstore.GoalAutopilotStatusRunning
		if goal.Status == assistantstore.GoalStatusPaused || goal.Status == assistantstore.GoalStatusBlocked {
			goal.Status = assistantstore.GoalStatusActive
		}
	case "resume":
		if autopilot.Status == assistantstore.GoalAutopilotStatusBudgetExhausted && autopilot.BudgetTasks <= autopilot.TasksStarted {
			return assistantstore.GoalTimeline{}, "", fmt.Errorf("resume needs budget_tasks greater than tasks_started (%d)", autopilot.TasksStarted)
		}
		if autopilot.StartedAt == nil {
			started := now
			autopilot.StartedAt = &started
		}
		autopilot.Status = assistantstore.GoalAutopilotStatusRunning
		autopilot.StopReasons = nil
		if goal.Status == assistantstore.GoalStatusPaused || goal.Status == assistantstore.GoalStatusBlocked {
			goal.Status = assistantstore.GoalStatusActive
		}
	case "pause":
		autopilot.Status = assistantstore.GoalAutopilotStatusPaused
	case "stop":
		autopilot.Status = assistantstore.GoalAutopilotStatusStopped
		autopilot.StopReasons = appendGoalStopReason(autopilot.StopReasons, "stopped by operator")
		autopilot.CurrentTaskID = ""
	default:
		return assistantstore.GoalTimeline{}, "", fmt.Errorf("unknown Autopilot action %q", action)
	}
	lastStep := now
	autopilot.LastStepAt = &lastStep
	goal.Autopilot = &autopilot
	goal.UpdatedAt = now
	goal = assistantstore.NormalizeGoal(goal)
	if err := store.SaveGoal(goal); err != nil {
		return assistantstore.GoalTimeline{}, "", err
	}
	noteBody := goalAutopilotActionNote(action, goal)
	_ = store.SaveNote(assistantstore.GoalNote{
		ID:        id.New("gnote"),
		GoalID:    goal.ID,
		Kind:      "autopilot",
		Title:     "Autopilot " + action,
		Body:      noteBody,
		CreatedBy: "assistant",
		CreatedAt: now,
	})
	o.appendGoalEvent(ctx, "assistant.goal.autopilot."+action, goal, map[string]any{"goal": goal, "autopilot": goal.Autopilot})
	reply := noteBody
	if action == "start" || action == "resume" {
		changed, reconcileReply, err := o.reconcileGoalAutopilot(ctx, store, goal)
		if err != nil {
			return assistantstore.GoalTimeline{}, "", err
		}
		if strings.TrimSpace(reconcileReply) != "" {
			reply = reconcileReply
		} else if changed {
			reply = "Autopilot advanced Goal `" + goal.ID + "`."
		}
	}
	timeline, err := o.LoadGoal(goal.ID)
	if err != nil {
		return assistantstore.GoalTimeline{}, "", err
	}
	return timeline, reply, nil
}

func applyGoalAutopilotRequest(autopilot *assistantstore.GoalAutopilot, req assistantstore.GoalAutopilotRequest) {
	if req.BudgetTasks > 0 {
		autopilot.BudgetTasks = req.BudgetTasks
	}
	if req.MaxRuntimeMinutes > 0 {
		autopilot.MaxRuntimeMinutes = req.MaxRuntimeMinutes
	}
	if len(req.AllowedActions) > 0 {
		autopilot.AllowedActions = req.AllowedActions
	}
	normalized := assistantstore.NormalizeGoalAutopilot(autopilot)
	*autopilot = normalized
}

func goalAutopilotActionNote(action string, goal assistantstore.Goal) string {
	goal = assistantstore.NormalizeGoal(goal)
	budget := 0
	tasksStarted := 0
	status := assistantstore.GoalAutopilotStatusReady
	if goal.Autopilot != nil {
		budget = goal.Autopilot.BudgetTasks
		tasksStarted = goal.Autopilot.TasksStarted
		status = goal.Autopilot.Status
	}
	switch action {
	case "start":
		return fmt.Sprintf("Autopilot started for this %s Goal with a %d task budget.", goal.Kind, budget)
	case "resume":
		return fmt.Sprintf("Autopilot resumed for this %s Goal. Tasks started: %d/%d.", goal.Kind, tasksStarted, budget)
	case "pause":
		return "Autopilot paused. Existing linked tasks are not cancelled, but no new Goal task will be created while paused."
	case "stop":
		return "Autopilot stopped. Existing linked tasks are left alone, and this Goal will not create new Autopilot tasks until started again."
	default:
		return "Autopilot state changed to " + status + "."
	}
}

func (o *Orchestrator) ReconcileGoalAutopilots(ctx context.Context) (int, error) {
	store, err := o.assistantGoalStore()
	if err != nil {
		return 0, err
	}
	goals, err := store.ListGoals()
	if err != nil {
		return 0, err
	}
	changed := 0
	for _, goal := range goals {
		goal = assistantstore.NormalizeGoal(goal)
		if goal.ExecutionMode != assistantstore.GoalExecutionModeAutopilot || goal.Autopilot == nil || goal.Autopilot.Status != assistantstore.GoalAutopilotStatusRunning {
			continue
		}
		advanced, _, err := o.reconcileGoalAutopilot(ctx, store, goal)
		if err != nil {
			o.log().Error("goal autopilot reconcile failed", "goal_id", goal.ID, "error", err)
			continue
		}
		if advanced {
			changed++
		}
	}
	return changed, nil
}

func (o *Orchestrator) reconcileGoalAutopilot(ctx context.Context, store *assistantstore.GoalStore, goal assistantstore.Goal) (bool, string, error) {
	goal = assistantstore.NormalizeGoal(goal)
	if goal.ExecutionMode != assistantstore.GoalExecutionModeAutopilot || goal.Autopilot == nil || goal.Autopilot.Status != assistantstore.GoalAutopilotStatusRunning {
		return false, "", nil
	}
	now := time.Now().UTC()
	if goal.Status == assistantstore.GoalStatusCompleted {
		return o.blockOrStopGoalAutopilot(ctx, store, goal, assistantstore.GoalAutopilotStatusCompleted, "Goal is marked completed.")
	}
	if goal.Status == assistantstore.GoalStatusArchived || goal.Status == assistantstore.GoalStatusPaused {
		return false, "Autopilot is waiting because the Goal is " + goal.Status + ".", nil
	}
	if goal.Autopilot.MaxRuntimeMinutes > 0 && goal.Autopilot.StartedAt != nil && now.Sub(*goal.Autopilot.StartedAt) > time.Duration(goal.Autopilot.MaxRuntimeMinutes)*time.Minute {
		return o.blockOrStopGoalAutopilot(ctx, store, goal, assistantstore.GoalAutopilotStatusBudgetExhausted, "Autopilot runtime budget was exhausted.")
	}
	if len(goal.OpenQuestions) > 0 {
		goal.Status = assistantstore.GoalStatusBlocked
		return o.blockOrStopGoalAutopilot(ctx, store, goal, assistantstore.GoalAutopilotStatusBlocked, "Autopilot is blocked by open Goal questions.")
	}
	if current, ok := o.currentGoalAutopilotTask(goal); ok {
		return o.reconcileGoalAutopilotTask(ctx, store, goal, current)
	}
	if goal.Autopilot.TasksStarted >= goal.Autopilot.BudgetTasks {
		return o.blockOrStopGoalAutopilot(ctx, store, goal, assistantstore.GoalAutopilotStatusBudgetExhausted, fmt.Sprintf("Autopilot task budget exhausted (%d/%d).", goal.Autopilot.TasksStarted, goal.Autopilot.BudgetTasks))
	}
	if !goalAutopilotAllows(goal, "create_task") {
		return o.blockOrStopGoalAutopilot(ctx, store, goal, assistantstore.GoalAutopilotStatusBlocked, "Autopilot policy does not allow creating tasks.")
	}
	return o.createGoalAutopilotTask(ctx, store, goal)
}

func (o *Orchestrator) currentGoalAutopilotTask(goal assistantstore.Goal) (taskstore.Task, bool) {
	goal = assistantstore.NormalizeGoal(goal)
	seen := map[string]bool{}
	ids := make([]string, 0, len(goal.LinkedTasks)+1)
	if goal.Autopilot != nil && strings.TrimSpace(goal.Autopilot.CurrentTaskID) != "" {
		id := strings.TrimSpace(goal.Autopilot.CurrentTaskID)
		ids = append(ids, id)
		seen[id] = true
	}
	for i := len(goal.LinkedTasks) - 1; i >= 0; i-- {
		taskID := strings.TrimSpace(goal.LinkedTasks[i])
		if taskID == "" || seen[taskID] {
			continue
		}
		ids = append(ids, taskID)
		seen[taskID] = true
	}
	for i, taskID := range ids {
		t, err := o.tasks.Load(taskID)
		if err != nil {
			continue
		}
		if t.GoalID != "" && t.GoalID != goal.ID {
			continue
		}
		if i == 0 && goal.Autopilot != nil && taskID == goal.Autopilot.CurrentTaskID {
			return t, true
		}
		if !taskTerminal(t.Status) {
			return t, true
		}
	}
	return taskstore.Task{}, false
}

func (o *Orchestrator) reconcileGoalAutopilotTask(ctx context.Context, store *assistantstore.GoalStore, goal assistantstore.Goal, t taskstore.Task) (bool, string, error) {
	switch t.Status {
	case taskstore.StatusQueued, taskstore.StatusRunning, taskstore.StatusAwaitingRestart:
		return false, fmt.Sprintf("Autopilot is waiting for task %s (%s).", taskShortID(t.ID), t.Status), nil
	case taskstore.StatusReadyForReview:
		if o.taskActive(t.ID) {
			return false, fmt.Sprintf("Autopilot is waiting for active review on task %s.", taskShortID(t.ID)), nil
		}
		if !goalAutopilotAllows(goal, "review_task") {
			return o.blockOrStopGoalAutopilot(ctx, store, goal, assistantstore.GoalAutopilotStatusBlocked, "Autopilot policy does not allow reviewing tasks.")
		}
		if _, head, err := o.mergeQueuePosition(ctx, t.ID); err != nil {
			return false, "", err
		} else if !head {
			return false, fmt.Sprintf("Autopilot is waiting for task %s to reach the head of the merge queue.", taskShortID(t.ID)), nil
		}
		queued, err := o.processMergeQueueHead(ctx, "Autopilot Goal task ready for review")
		if err != nil {
			return false, "", err
		}
		if queued {
			return true, fmt.Sprintf("Autopilot queued review for task %s.", taskShortID(t.ID)), nil
		}
		return false, fmt.Sprintf("Autopilot is waiting for task %s to reach the head of the merge queue.", taskShortID(t.ID)), nil
	case taskstore.StatusAwaitingApproval:
		if !goalAutopilotAllows(goal, "approve_merge") {
			return o.blockOrStopGoalAutopilot(ctx, store, goal, assistantstore.GoalAutopilotStatusBlocked, "Autopilot policy does not allow approving merges.")
		}
		merged, reply, err := o.processGoalAutopilotMergeApproval(ctx, t, "Autopilot Goal task awaiting merge approval")
		if err != nil {
			return false, "", err
		}
		if merged {
			return true, reply, nil
		}
		return false, fmt.Sprintf("Autopilot is waiting for merge approval readiness on task %s.", taskShortID(t.ID)), nil
	case taskstore.StatusAwaitingVerification, taskstore.StatusNoChangeRequired:
		if !goalAutopilotAllows(goal, "accept_task") {
			return o.blockOrStopGoalAutopilot(ctx, store, goal, assistantstore.GoalAutopilotStatusBlocked, "Autopilot policy does not allow accepting completed tasks.")
		}
		acceptReply, err := o.acceptTaskWithActor(ctx, t.ID, "Assistant Autopilot")
		if err != nil {
			return false, "", err
		}
		latest, loadErr := store.LoadGoal(goal.ID)
		if loadErr != nil {
			return false, "", loadErr
		}
		_, reconcileReply, err := o.reconcileGoalAutopilot(ctx, store, latest)
		if strings.TrimSpace(reconcileReply) == "" {
			reconcileReply = acceptReply
		}
		return true, reconcileReply, err
	case taskstore.StatusDone:
		goal.Autopilot.CurrentTaskID = ""
		goal.Autopilot.LastStepAt = goalTimePtr(time.Now().UTC())
		goal.UpdatedAt = time.Now().UTC()
		if err := store.SaveGoal(goal); err != nil {
			return false, "", err
		}
		_ = store.SaveNote(assistantstore.GoalNote{
			ID:        id.New("gnote"),
			GoalID:    goal.ID,
			Kind:      "autopilot",
			Title:     "Autopilot task accepted",
			Body:      "Autopilot accepted linked task " + taskShortID(t.ID) + " and is checking the next step.",
			TaskID:    t.ID,
			CreatedBy: "assistant",
			CreatedAt: time.Now().UTC(),
		})
		latest, loadErr := store.LoadGoal(goal.ID)
		if loadErr != nil {
			return false, "", loadErr
		}
		_, reconcileReply, err := o.reconcileGoalAutopilot(ctx, store, latest)
		return true, reconcileReply, err
	case taskstore.StatusBlocked, taskstore.StatusConflictResolution:
		if o.taskActive(t.ID) {
			return false, fmt.Sprintf("Autopilot is waiting for automatic recovery on task %s.", taskShortID(t.ID)), nil
		}
		if ok, _ := automaticTaskRecoveryCandidate(t, time.Now().UTC()); ok {
			return false, fmt.Sprintf("Autopilot is waiting for task supervisor recovery on task %s.", taskShortID(t.ID)), nil
		}
		return o.blockOrStopGoalAutopilot(ctx, store, goal, assistantstore.GoalAutopilotStatusBlocked, "Linked task "+taskShortID(t.ID)+" is "+t.Status+".")
	case taskstore.StatusFailed, taskstore.StatusCancelled:
		return o.blockOrStopGoalAutopilot(ctx, store, goal, assistantstore.GoalAutopilotStatusBlocked, "Linked task "+taskShortID(t.ID)+" is "+t.Status+".")
	default:
		return false, fmt.Sprintf("Autopilot is waiting for task %s (%s).", taskShortID(t.ID), firstNonEmptyString(t.Status, "unknown")), nil
	}
}

func (o *Orchestrator) processGoalAutopilotMergeApproval(ctx context.Context, t taskstore.Task, reason string) (bool, string, error) {
	if t.Status != taskstore.StatusAwaitingApproval || o.taskActive(t.ID) {
		return false, "", nil
	}
	if _, head, err := o.mergeQueuePosition(ctx, t.ID); err != nil {
		return false, "", err
	} else if !head {
		return false, "", nil
	}
	approval, ok, err := o.latestApprovalForTask(t.ID, "git.merge_approved", approvalstore.StatusPending)
	if err != nil {
		return false, "", err
	}
	if !ok {
		return false, "", nil
	}
	_ = o.events.Append(ctx, eventlog.Event{ID: id.New("evt"), Type: "approval.autopilot_merge.queued", Actor: "Assistant Autopilot", TaskID: t.ID, Payload: eventlog.Payload(map[string]any{
		"approval": approval.ID,
		"goal_id":  t.GoalID,
		"reason":   reason,
	})})
	reply, err := o.resolveApprovalWithActor(ctx, approval.ID, true, "Assistant Autopilot")
	if err != nil {
		return false, "", err
	}
	_ = o.events.Append(ctx, eventlog.Event{ID: id.New("evt"), Type: "approval.autopilot_merge.completed", Actor: "Assistant Autopilot", TaskID: t.ID, Payload: eventlog.Payload(map[string]any{
		"approval": approval.ID,
		"goal_id":  t.GoalID,
		"reply":    reply,
	})})
	return true, reply, nil
}

func (o *Orchestrator) createGoalAutopilotTask(ctx context.Context, store *assistantstore.GoalStore, goal assistantstore.Goal) (bool, string, error) {
	goal = assistantstore.NormalizeGoal(goal)
	if _, ok := o.preferredWorkerBackend(); !ok {
		return o.blockOrStopGoalAutopilot(ctx, store, goal, assistantstore.GoalAutopilotStatusBlocked, "No local worker backend is configured for Autopilot tasks.")
	}
	created, err := o.createTaskRecordForGoal(ctx, goalAutopilotTaskGoal(goal), goal)
	if err != nil {
		return false, "", err
	}
	if created.Task.ID == "" {
		return false, "Autopilot did not create a task because the Goal task prompt was empty.", nil
	}
	now := time.Now().UTC()
	goal, err = store.LoadGoal(goal.ID)
	if err != nil {
		return false, "", err
	}
	goal = assistantstore.NormalizeGoal(goal)
	if goal.Autopilot == nil {
		autopilot := assistantstore.NormalizeGoalAutopilot(nil)
		goal.Autopilot = &autopilot
	}
	goal.ExecutionMode = assistantstore.GoalExecutionModeAutopilot
	goal.Autopilot.Status = assistantstore.GoalAutopilotStatusRunning
	goal.Autopilot.TasksStarted++
	goal.Autopilot.CurrentTaskID = created.Task.ID
	goal.Autopilot.LastStepAt = &now
	goal.LastActionAt = &now
	goal.UpdatedAt = now
	if !stringSliceContains(goal.LinkedTasks, created.Task.ID) {
		goal.LinkedTasks = append(goal.LinkedTasks, created.Task.ID)
	}
	if err := store.SaveGoal(goal); err != nil {
		return false, "", err
	}
	_ = store.SaveNote(assistantstore.GoalNote{
		ID:        id.New("gnote"),
		GoalID:    goal.ID,
		Kind:      "autopilot",
		Title:     "Autopilot task created",
		Body:      fmt.Sprintf("Created %s task %s for this %s Goal (%d/%d task budget).", goal.ExecutionMode, taskShortID(created.Task.ID), goal.Kind, goal.Autopilot.TasksStarted, goal.Autopilot.BudgetTasks),
		TaskID:    created.Task.ID,
		CreatedBy: "assistant",
		CreatedAt: now,
	})
	o.appendGoalEvent(ctx, "assistant.goal.autopilot.task.created", goal, map[string]any{"goal_id": goal.ID, "task_id": created.Task.ID, "autopilot": goal.Autopilot})
	startReply := o.startGoalAutopilotTaskIfPossible(ctx, created.Task)
	reply := fmt.Sprintf("Autopilot created task %s for Goal `%s`.", taskShortID(created.Task.ID), goal.ID)
	if strings.TrimSpace(startReply) != "" {
		reply += " " + startReply
	}
	return true, reply, nil
}

func (o *Orchestrator) startGoalAutopilotTaskIfPossible(ctx context.Context, t taskstore.Task) string {
	if t.Status != taskstore.StatusQueued || o.taskActive(t.ID) {
		return ""
	}
	if o.activeTaskCount() >= o.maxConcurrentTasks() {
		return "Worker capacity is full; the task supervisor will start it when capacity is available."
	}
	backend, ok := o.preferredWorkerBackend()
	if !ok {
		return "No worker backend is configured, so the task is queued."
	}
	_ = o.events.Append(ctx, eventlog.Event{ID: id.New("evt"), Type: "task.supervisor.queued", Actor: "Assistant Autopilot", TaskID: t.ID, Payload: eventlog.Payload(map[string]any{
		"backend": backend,
		"reason":  "Autopilot created Goal task",
	})})
	if err := o.startDelegationForTask(ctx, t.ID, backend, defaultDelegationInstruction(t)); err != nil {
		o.markRecoveryBlocked(ctx, t.ID, err)
		return "The task was created, but the worker could not start: " + err.Error()
	}
	return "Worker started with " + backend + "."
}

func (o *Orchestrator) blockOrStopGoalAutopilot(ctx context.Context, store *assistantstore.GoalStore, goal assistantstore.Goal, status, reason string) (bool, string, error) {
	goal = assistantstore.NormalizeGoal(goal)
	now := time.Now().UTC()
	if goal.Autopilot == nil {
		autopilot := assistantstore.NormalizeGoalAutopilot(nil)
		goal.Autopilot = &autopilot
	}
	goal.Autopilot.Status = status
	goal.Autopilot.StopReasons = appendGoalStopReason(goal.Autopilot.StopReasons, reason)
	goal.Autopilot.LastStepAt = &now
	if status == assistantstore.GoalAutopilotStatusBlocked {
		goal.Status = assistantstore.GoalStatusBlocked
	}
	goal.UpdatedAt = now
	if err := store.SaveGoal(goal); err != nil {
		return false, "", err
	}
	_ = store.SaveNote(assistantstore.GoalNote{
		ID:        id.New("gnote"),
		GoalID:    goal.ID,
		Kind:      "autopilot",
		Title:     "Autopilot " + status,
		Body:      reason,
		CreatedBy: "assistant",
		CreatedAt: now,
	})
	o.appendGoalEvent(ctx, "assistant.goal.autopilot."+status, goal, map[string]any{"goal_id": goal.ID, "reason": reason, "autopilot": goal.Autopilot})
	return true, reason, nil
}

func appendGoalStopReason(reasons []string, reason string) []string {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return reasons
	}
	for _, existing := range reasons {
		if strings.EqualFold(strings.TrimSpace(existing), reason) {
			return reasons
		}
	}
	reasons = append(reasons, reason)
	if len(reasons) > 16 {
		reasons = reasons[len(reasons)-16:]
	}
	return reasons
}

func goalAutopilotAllows(goal assistantstore.Goal, action string) bool {
	goal = assistantstore.NormalizeGoal(goal)
	action = strings.TrimSpace(action)
	if action == "" || goal.Autopilot == nil {
		return false
	}
	for _, allowed := range goal.Autopilot.AllowedActions {
		if strings.EqualFold(strings.TrimSpace(allowed), action) {
			return true
		}
	}
	return false
}

func goalTimePtr(value time.Time) *time.Time {
	value = value.UTC()
	return &value
}

func goalAutopilotTaskGoal(goal assistantstore.Goal) string {
	goal = assistantstore.NormalizeGoal(goal)
	kindInstruction := "Pick the next bounded implementation slice that measurably advances the objective."
	switch goal.Kind {
	case assistantstore.GoalKindRoutine:
		kindInstruction = "Run one complete routine cycle, update the durable state, and create or change code only when that is required by the objective."
	case assistantstore.GoalKindWatch:
		kindInstruction = "Inspect the watched condition, gather evidence, and make only the bounded change or follow-up needed by the condition."
	case assistantstore.GoalKindMaintenance:
		kindInstruction = "Pick one bounded upkeep issue that improves reliability, clarity, tests, docs, or operator ergonomics."
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Autopilot task for %s Goal `%s`: %s\n\nObjective: %s", goal.Kind, goal.ID, goal.Title, goal.Objective)
	if goal.Details != "" {
		fmt.Fprintf(&b, "\n\nDetails:\n%s", goal.Details)
	}
	if goal.ProgressSummary != "" {
		fmt.Fprintf(&b, "\n\nCurrent progress:\n%s", goal.ProgressSummary)
	}
	b.WriteString("\n\nAutopilot work mode:")
	fmt.Fprintf(&b, "\n- %s", kindInstruction)
	b.WriteString("\n- Work the selected slice to completion in this task, including implementation, docs, focused tests, and required browser UAT for changed UI.")
	b.WriteString("\n- Do not split the work into a daily drip. If the Goal needs one large feature, build the largest coherent safe slice this task can complete.")
	b.WriteString("\n- Use existing approval, merge, restart, and verification gates. Autopilot may pass those gates for this Goal when checks succeed.")
	b.WriteString("\n- If the next step needs credentials, private operator judgement, destructive action, or unresolved product direction, stop and report the blocker instead of guessing.")
	if len(goal.SuccessCriteria) > 0 {
		b.WriteString("\n\nGoal success criteria:")
		for _, criterion := range goal.SuccessCriteria {
			fmt.Fprintf(&b, "\n- %s", criterion)
		}
	}
	if len(goal.Constraints) > 0 {
		b.WriteString("\n\nGoal constraints:")
		for _, constraint := range goal.Constraints {
			fmt.Fprintf(&b, "\n- %s", constraint)
		}
	}
	return b.String()
}

func (o *Orchestrator) handleGoalCommand(ctx context.Context, fields []string, message string) (string, error) {
	if len(fields) == 0 {
		return "usage: goal <objective>|list|show <goal_id>|check <goal_id>|autopilot <start|pause|resume|stop> <goal_id>|pause <goal_id>|archive <goal_id>", nil
	}
	if commandWord(fields[0]) == "goals" {
		if len(fields) == 1 || commandWord(fields[1]) == "list" {
			return o.formatGoalsReply()
		}
		fields = append([]string{"goal"}, fields[1:]...)
	}
	if len(fields) == 1 {
		return "usage: goal <objective>|list|show <goal_id>|check <goal_id>|autopilot <start|pause|resume|stop> <goal_id>|pause <goal_id>|archive <goal_id>", nil
	}
	action := commandWord(fields[1])
	switch action {
	case "list", "ls":
		return o.formatGoalsReply()
	case "show", "get":
		if len(fields) < 3 {
			return "usage: goal show <goal_id>", nil
		}
		return o.formatGoalReply(fields[2])
	case "check":
		if len(fields) < 3 {
			return "usage: goal check <goal_id>", nil
		}
		run, _, err := o.CheckGoal(ctx, fields[2])
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("Checked Goal `%s` with Assistant run `%s`: %s", fields[2], run.ID, firstNonEmptyString(run.Summary, run.Decision)), nil
	case "autopilot", "auto":
		if len(fields) < 4 {
			return "usage: goal autopilot <start|pause|resume|stop> <goal_id>", nil
		}
		return o.handleGoalAutopilotCommand(ctx, commandWord(fields[2]), fields[3])
	case "pause":
		if len(fields) < 3 {
			return "usage: goal pause <goal_id>", nil
		}
		timeline, err := o.UpdateGoal(ctx, fields[2], assistantstore.GoalUpdateRequest{Status: assistantstore.GoalStatusPaused})
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("Paused Goal `%s`: %s", timeline.Goal.ID, timeline.Goal.Title), nil
	case "archive":
		if len(fields) < 3 {
			return "usage: goal archive <goal_id>", nil
		}
		timeline, err := o.UpdateGoal(ctx, fields[2], assistantstore.GoalUpdateRequest{Status: assistantstore.GoalStatusArchived})
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("Archived Goal `%s`: %s", timeline.Goal.ID, timeline.Goal.Title), nil
	case "new", "create", "add":
		objective := strings.TrimSpace(strings.Join(fields[2:], " "))
		if objective == "" {
			return "usage: goal create <objective>", nil
		}
		return o.createGoalFromChat(ctx, assistantstore.GoalCreateRequest{Title: goalTitleFromObjective(objective), Objective: objective, CreatedBy: "chat"})
	default:
		objective := strings.TrimSpace(strings.TrimPrefix(message, fields[0]))
		if objective == "" {
			return "usage: goal <objective>", nil
		}
		return o.createGoalFromChat(ctx, assistantstore.GoalCreateRequest{Title: goalTitleFromObjective(objective), Objective: objective, CreatedBy: "chat"})
	}
}

func (o *Orchestrator) createGoalFromChat(ctx context.Context, req assistantstore.GoalCreateRequest) (string, error) {
	if req.Title == "" {
		req.Title = goalTitleFromObjective(req.Objective)
	}
	if req.CreatedBy == "" {
		req.CreatedBy = "chat"
	}
	timeline, err := o.CreateGoal(ctx, req)
	if err != nil {
		return "", err
	}
	goal := timeline.Goal
	return fmt.Sprintf("Created %s Goal `%s`: %s\nMode: %s. Next: `goal check %s` for a guided check, or `goal autopilot start %s` to let it drive linked tasks.", goal.Kind, goal.ID, goal.Title, goal.ExecutionMode, goal.ID, goal.ID), nil
}

func (o *Orchestrator) handleGoalAutopilotCommand(ctx context.Context, action, goalID string) (string, error) {
	var (
		timeline assistantstore.GoalTimeline
		reply    string
		err      error
	)
	switch action {
	case "start":
		timeline, reply, err = o.StartGoalAutopilot(ctx, goalID, assistantstore.GoalAutopilotRequest{})
	case "pause":
		timeline, reply, err = o.PauseGoalAutopilot(ctx, goalID)
	case "resume":
		timeline, reply, err = o.ResumeGoalAutopilot(ctx, goalID, assistantstore.GoalAutopilotRequest{})
	case "stop":
		timeline, reply, err = o.StopGoalAutopilot(ctx, goalID)
	default:
		return "usage: goal autopilot <start|pause|resume|stop> <goal_id>", nil
	}
	if err != nil {
		return "", err
	}
	status := assistantstore.GoalAutopilotStatusReady
	if timeline.Goal.Autopilot != nil {
		status = timeline.Goal.Autopilot.Status
	}
	if strings.TrimSpace(reply) == "" {
		reply = "Autopilot " + action + " complete."
	}
	return fmt.Sprintf("%s\nGoal `%s` Autopilot status: %s.", reply, timeline.Goal.ID, status), nil
}

func (o *Orchestrator) formatGoalsReply() (string, error) {
	goals, err := o.ListGoals()
	if err != nil {
		return "", err
	}
	if len(goals) == 0 {
		return "No Assistant Goals yet. Use `goal <objective>` to create one.", nil
	}
	var b strings.Builder
	b.WriteString("Assistant Goals:")
	for _, goal := range goals {
		mode := goal.ExecutionMode
		if mode == "" {
			mode = assistantstore.GoalExecutionModeGuided
		}
		fmt.Fprintf(&b, "\n- `%s` %s [%s %s %s]", goal.ID, goal.Title, goal.Kind, mode, goal.Status)
		if goal.Autopilot != nil && goal.ExecutionMode == assistantstore.GoalExecutionModeAutopilot {
			fmt.Fprintf(&b, " Autopilot: %s %d/%d", goal.Autopilot.Status, goal.Autopilot.TasksStarted, goal.Autopilot.BudgetTasks)
		}
		if goal.ProgressSummary != "" {
			fmt.Fprintf(&b, ": %s", truncateAssistantRunText(goal.ProgressSummary, 120))
		}
		if goal.NextCheckAt != nil {
			fmt.Fprintf(&b, " Next check: %s", goal.NextCheckAt.UTC().Format(time.RFC3339))
		}
	}
	return b.String(), nil
}

func (o *Orchestrator) formatGoalReply(goalID string) (string, error) {
	timeline, err := o.LoadGoal(goalID)
	if err != nil {
		return "", err
	}
	goal := timeline.Goal
	var b strings.Builder
	fmt.Fprintf(&b, "Goal `%s`: %s\nType: %s\nMode: %s\nStatus: %s\nObjective: %s", goal.ID, goal.Title, goal.Kind, goal.ExecutionMode, goal.Status, goal.Objective)
	if goal.Autopilot != nil {
		fmt.Fprintf(&b, "\nAutopilot: %s (%d/%d tasks)", goal.Autopilot.Status, goal.Autopilot.TasksStarted, goal.Autopilot.BudgetTasks)
		if goal.Autopilot.CurrentTaskID != "" {
			fmt.Fprintf(&b, "\nCurrent Autopilot task: %s", goal.Autopilot.CurrentTaskID)
		}
	}
	if goal.ProgressSummary != "" {
		fmt.Fprintf(&b, "\nProgress: %s", goal.ProgressSummary)
	}
	if goal.NextCheckAt != nil {
		fmt.Fprintf(&b, "\nNext check: %s", goal.NextCheckAt.UTC().Format(time.RFC3339))
	}
	if len(goal.LinkedTasks) > 0 {
		fmt.Fprintf(&b, "\nLinked tasks: %s", strings.Join(goal.LinkedTasks, ", "))
	}
	if len(timeline.Watches) > 0 {
		fmt.Fprintf(&b, "\nWatches: %d", len(timeline.Watches))
	}
	if len(timeline.Assessments) > 0 {
		fmt.Fprintf(&b, "\nLatest assessment: %s", firstNonEmptyString(timeline.Assessments[0].Summary, timeline.Assessments[0].Decision))
	}
	return b.String(), nil
}

func goalCreationIntent(message string) (assistantstore.GoalCreateRequest, bool) {
	trimmed := strings.TrimSpace(message)
	if trimmed == "" {
		return assistantstore.GoalCreateRequest{}, false
	}
	fields := strings.Fields(trimmed)
	if len(fields) == 0 {
		return assistantstore.GoalCreateRequest{}, false
	}
	first := commandWord(fields[0])
	if first == "goal" {
		if len(fields) >= 2 {
			switch commandWord(fields[1]) {
			case "list", "ls", "show", "get", "check", "autopilot", "auto", "pause", "archive", "new", "create", "add":
				return assistantstore.GoalCreateRequest{}, false
			}
		}
		objective := strings.TrimSpace(strings.TrimPrefix(trimmed, fields[0]))
		return assistantstore.GoalCreateRequest{Title: goalTitleFromObjective(objective), Objective: objective, CreatedBy: "chat"}, objective != ""
	}
	normalized := normalizeIntentText(trimmed)
	prefixes := []string{
		"new goal",
		"create goal",
		"create a goal",
		"add goal",
		"add a goal",
		"my goal is",
		"my goal is that",
		"i want a goal to",
		"i want the assistant to keep",
	}
	for _, prefix := range prefixes {
		if strings.HasPrefix(normalized, prefix+" ") {
			objective := wordsAfterPrefix(trimmed, len(strings.Fields(prefix)))
			objective = strings.TrimSpace(strings.Trim(objective, " :"))
			if objective == "" {
				continue
			}
			return assistantstore.GoalCreateRequest{Title: goalTitleFromObjective(objective), Objective: objective, CreatedBy: "chat"}, true
		}
	}
	return assistantstore.GoalCreateRequest{}, false
}

func goalTitleFromObjective(objective string) string {
	objective = strings.TrimSpace(strings.Trim(objective, " \t\r\n:"))
	if objective == "" {
		return ""
	}
	title := objective
	for _, sep := range []string{".", "\n"} {
		if idx := strings.Index(title, sep); idx > 0 {
			title = strings.TrimSpace(title[:idx])
		}
	}
	title = strings.Join(strings.Fields(title), " ")
	if len(title) > 80 {
		title = strings.TrimSpace(title[:80])
	}
	return title
}

func goalNextCheckLabel(goal assistantstore.Goal) string {
	if goal.NextCheckAt == nil {
		return "the next scheduled proactive check"
	}
	return goal.NextCheckAt.UTC().Format(time.RFC3339)
}

func (o *Orchestrator) assistantGoalSnapshotRefs(now time.Time, selectedGoalID string) []assistantstore.GoalSnapshotRef {
	store, err := o.assistantGoalStore()
	if err != nil {
		return nil
	}
	goals, err := store.ListGoals()
	if err != nil {
		o.log().Warn("assistant goals unavailable", "error", err)
		return nil
	}
	selectedGoalID = strings.TrimSpace(selectedGoalID)
	refs := make([]assistantstore.GoalSnapshotRef, 0, len(goals))
	for _, goal := range goals {
		goal = assistantstore.NormalizeGoal(goal)
		include := goal.ID == selectedGoalID ||
			goal.Status == assistantstore.GoalStatusActive ||
			goal.Status == assistantstore.GoalStatusBlocked ||
			assistantstore.GoalIsDue(goal, now)
		if !include {
			continue
		}
		refs = append(refs, assistantstore.GoalToSnapshotRef(goal, now))
		if selectedGoalID == "" && len(refs) >= 12 {
			break
		}
	}
	return refs
}

func (o *Orchestrator) createTaskRecordForGoal(ctx context.Context, taskGoal string, goal assistantstore.Goal) (createdTask, error) {
	taskGoal = strings.TrimSpace(taskGoal)
	if taskGoal == "" {
		return createdTask{}, nil
	}
	goal = assistantstore.NormalizeGoal(goal)
	return o.createTaskRecordWithOptions(ctx, taskGoalWithGoalContext(taskGoal, goal), nil, taskCreateOptions{
		GoalID:        goal.ID,
		ExecutionMode: goal.ExecutionMode,
		GoalKind:      goal.Kind,
	})
}

func (o *Orchestrator) recordGoalRunAssessment(ctx context.Context, run *assistantstore.Run) {
	if run == nil || run.Status == "" {
		return
	}
	store, err := o.assistantGoalStore()
	if err != nil {
		return
	}
	now := firstNonZeroTime(run.FinishedAt, run.UpdatedAt, time.Now().UTC())
	goalIDs := goalIDsForRun(*run)
	for _, goalID := range goalIDs {
		goal, err := store.LoadGoal(goalID)
		if err != nil {
			continue
		}
		goal.LastCheckedAt = &now
		goal.UpdatedAt = now
		if assistantRunHasCreatedTaskForGoal(*run, goal.ID) {
			goal.LastActionAt = &now
		}
		if strings.TrimSpace(run.Summary) != "" {
			goal.ProgressSummary = truncateAssistantRunText(run.Summary, 500)
		}
		if goal.Status == assistantstore.GoalStatusActive || goal.Status == assistantstore.GoalStatusBlocked {
			goal.NextCheckAt = assistantstore.GoalNextCheckTime(goal, now)
		}
		goal = assistantstore.NormalizeGoal(goal)
		if err := store.SaveGoal(goal); err != nil {
			o.log().Warn("goal assessment update failed", "error", err, "goal", goal.ID)
			continue
		}
		actions := goalActionSummaries(*run, goal.ID)
		assessment := assistantstore.GoalAssessment{
			ID:          id.New("gassess"),
			GoalID:      goal.ID,
			RunID:       run.ID,
			Trigger:     run.Trigger.Label,
			Decision:    run.Decision,
			Summary:     run.Summary,
			Actions:     actions,
			NextCheckAt: goal.NextCheckAt,
			CreatedAt:   now,
		}
		if err := store.SaveAssessment(assessment); err != nil {
			o.log().Warn("goal assessment save failed", "error", err, "goal", goal.ID)
		}
		noteBody := "Assistant checked this Goal."
		if run.Summary != "" {
			noteBody = run.Summary
		}
		_ = store.SaveNote(assistantstore.GoalNote{
			ID:        id.New("gnote"),
			GoalID:    goal.ID,
			Kind:      "assessment",
			Title:     "Assistant assessment",
			Body:      noteBody,
			RunID:     run.ID,
			CreatedBy: "assistant",
			CreatedAt: now,
		})
	}
}

func (o *Orchestrator) linkTaskToGoal(ctx context.Context, goalID, taskID, runID, summary string) {
	goalID = strings.TrimSpace(goalID)
	taskID = strings.TrimSpace(taskID)
	if goalID == "" || taskID == "" {
		return
	}
	store, err := o.assistantGoalStore()
	if err != nil {
		return
	}
	goal, err := store.LoadGoal(goalID)
	if err != nil {
		return
	}
	now := time.Now().UTC()
	if !stringSliceContains(goal.LinkedTasks, taskID) {
		goal.LinkedTasks = append(goal.LinkedTasks, taskID)
	}
	goal.LastActionAt = &now
	goal.UpdatedAt = now
	if summary != "" {
		goal.ProgressSummary = truncateAssistantRunText(summary, 500)
	}
	if err := store.SaveGoal(goal); err != nil {
		o.log().Warn("goal task link failed", "error", err, "goal", goalID, "task", taskID)
		return
	}
	_ = store.SaveNote(assistantstore.GoalNote{
		ID:        id.New("gnote"),
		GoalID:    goal.ID,
		Kind:      "task",
		Title:     "Linked task created",
		Body:      "Created linked task " + taskID + ".",
		TaskID:    taskID,
		RunID:     runID,
		CreatedBy: "assistant",
		CreatedAt: now,
	})
	o.appendGoalEvent(ctx, "assistant.goal.task.linked", goal, map[string]any{"goal_id": goal.ID, "task_id": taskID, "run_id": runID})
}

func (o *Orchestrator) reflectGoalTaskCompletion(ctx context.Context, t taskstore.Task) {
	goalID := strings.TrimSpace(t.GoalID)
	if goalID == "" {
		return
	}
	store, err := o.assistantGoalStore()
	if err != nil {
		return
	}
	goal, err := store.LoadGoal(goalID)
	if err != nil {
		return
	}
	now := time.Now().UTC()
	result := strings.TrimSpace(t.Result)
	if result == "" {
		result = "Task completed."
	}
	goal.ProgressSummary = truncateAssistantRunText("Latest linked task completed: "+friendlyTaskTitle(t)+". "+result, 600)
	goal.LastActionAt = &now
	if goal.Status == assistantstore.GoalStatusActive || goal.Status == assistantstore.GoalStatusBlocked {
		goal.NextCheckAt = assistantstore.GoalNextCheckTime(goal, now)
	}
	if !stringSliceContains(goal.LinkedTasks, t.ID) {
		goal.LinkedTasks = append(goal.LinkedTasks, t.ID)
	}
	goal.UpdatedAt = now
	if err := store.SaveGoal(goal); err != nil {
		o.log().Warn("goal task completion update failed", "error", err, "goal", goalID, "task", t.ID)
		return
	}
	_ = store.SaveNote(assistantstore.GoalNote{
		ID:        id.New("gnote"),
		GoalID:    goal.ID,
		Kind:      "reflection",
		Title:     "Linked task completed",
		Body:      goal.ProgressSummary,
		TaskID:    t.ID,
		CreatedBy: "assistant",
		CreatedAt: now,
	})
	for _, recommendation := range extractGoalWatchRecommendations(result) {
		watch := assistantstore.GoalWatch{
			ID:              id.New("gwatch"),
			GoalID:          goal.ID,
			Title:           recommendation,
			Condition:       "worker_recommended_watch",
			Source:          "task_reflection",
			Severity:        "info",
			Status:          assistantstore.GoalWatchStatusActive,
			OnTrigger:       "create_signal",
			SuggestedAction: "Review this watch during the next Goal assessment.",
			CreatedAt:       now,
			UpdatedAt:       now,
		}
		if err := store.SaveWatch(watch); err != nil {
			o.log().Warn("goal watch recommendation save failed", "error", err, "goal", goal.ID, "task", t.ID)
		}
	}
	o.appendGoalEvent(ctx, "assistant.goal.task.reflected", goal, map[string]any{"goal_id": goal.ID, "task_id": t.ID})
}

func (o *Orchestrator) appendGoalEvent(ctx context.Context, eventType string, goal assistantstore.Goal, payload map[string]any) {
	if o.events == nil {
		return
	}
	if payload == nil {
		payload = map[string]any{}
	}
	payload["goal_id"] = goal.ID
	payload["goal_title"] = goal.Title
	_ = o.events.Append(ctx, eventlog.Event{
		ID:      id.New("evt"),
		Type:    eventType,
		Actor:   "Assistant",
		Payload: eventlog.Payload(payload),
	})
}

func goalRunRequestText(goal assistantstore.Goal) string {
	goal = assistantstore.NormalizeGoal(goal)
	var b strings.Builder
	fmt.Fprintf(&b, "Assess Goal %s: %s\n\nObjective: %s", goal.ID, goal.Title, goal.Objective)
	if goal.Details != "" {
		fmt.Fprintf(&b, "\n\nDetails:\n%s", goal.Details)
	}
	if goal.ProgressSummary != "" {
		fmt.Fprintf(&b, "\n\nCurrent progress:\n%s", goal.ProgressSummary)
	}
	if len(goal.SuccessCriteria) > 0 {
		b.WriteString("\n\nSuccess criteria:")
		for _, criterion := range goal.SuccessCriteria {
			fmt.Fprintf(&b, "\n- %s", criterion)
		}
	}
	if len(goal.Constraints) > 0 {
		b.WriteString("\n\nConstraints:")
		for _, constraint := range goal.Constraints {
			fmt.Fprintf(&b, "\n- %s", constraint)
		}
	}
	if len(goal.OpenQuestions) > 0 {
		b.WriteString("\n\nOpen questions:")
		for _, question := range goal.OpenQuestions {
			fmt.Fprintf(&b, "\n- %s", question)
		}
	}
	return b.String()
}

func taskGoalWithGoalContext(taskGoal string, goal assistantstore.Goal) string {
	goal = assistantstore.NormalizeGoal(goal)
	var b strings.Builder
	b.WriteString(strings.TrimSpace(taskGoal))
	b.WriteString("\n\nLinked Assistant Goal:")
	fmt.Fprintf(&b, "\n- Goal ID: %s", goal.ID)
	fmt.Fprintf(&b, "\n- Title: %s", goal.Title)
	fmt.Fprintf(&b, "\n- Objective: %s", goal.Objective)
	if goal.Details != "" {
		fmt.Fprintf(&b, "\n\nGoal details:\n%s", goal.Details)
	}
	if goal.ProgressSummary != "" {
		fmt.Fprintf(&b, "\n\nCurrent progress:\n%s", goal.ProgressSummary)
	}
	if len(goal.SuccessCriteria) > 0 {
		b.WriteString("\n\nGoal success criteria:")
		for _, criterion := range goal.SuccessCriteria {
			fmt.Fprintf(&b, "\n- %s", criterion)
		}
	}
	if len(goal.Constraints) > 0 {
		b.WriteString("\n\nGoal constraints:")
		for _, constraint := range goal.Constraints {
			fmt.Fprintf(&b, "\n- %s", constraint)
		}
	}
	b.WriteString("\n\nReport-back contract:")
	b.WriteString("\n- State whether the task moved the Goal forward.")
	b.WriteString("\n- Report blockers as Goal blockers or follow-up recommendations.")
	b.WriteString("\n- Recommend any watch that should keep monitoring the Goal after this task.")
	b.WriteString("\n- Do not perform external side effects outside the task's normal approval gates.")
	return b.String()
}

func goalIDsForRun(run assistantstore.Run) []string {
	seen := map[string]bool{}
	var ids []string
	add := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			return
		}
		seen[value] = true
		ids = append(ids, value)
	}
	add(run.GoalID)
	for _, signal := range run.Snapshot.Signals {
		add(signal.GoalID)
	}
	for _, action := range run.RecommendedActions {
		add(action.GoalID)
	}
	for _, finding := range run.Concerns {
		add(finding.GoalID)
	}
	for _, finding := range run.Opportunities {
		add(finding.GoalID)
	}
	return ids
}

func assistantRunHasCreatedTaskForGoal(run assistantstore.Run, goalID string) bool {
	for _, action := range run.RecommendedActions {
		if action.GoalID == goalID && action.CreatedTaskID != "" {
			return true
		}
	}
	return false
}

func goalActionSummaries(run assistantstore.Run, goalID string) []string {
	var out []string
	for _, action := range run.RecommendedActions {
		if goalID != "" && action.GoalID != goalID {
			continue
		}
		summary := strings.TrimSpace(action.Title)
		if action.CreatedTaskID != "" {
			summary += " -> task " + action.CreatedTaskID
		} else if action.Status != "" {
			summary += " -> " + action.Status
		}
		if summary != "" {
			out = append(out, summary)
		}
	}
	return out
}

type taskCreateOptions struct {
	GoalID        string
	ExecutionMode string
	GoalKind      string
}

func stringSliceContains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func extractGoalWatchRecommendations(result string) []string {
	var out []string
	seen := map[string]bool{}
	for _, line := range strings.Split(result, "\n") {
		trimmed := strings.TrimSpace(strings.Trim(line, "-* "))
		lower := strings.ToLower(trimmed)
		var value string
		for _, marker := range []string{"goal.watch.recommend:", "watch_recommendation:", "watch recommendation:", "recommend watch:", "watch:"} {
			if idx := strings.Index(lower, marker); idx >= 0 {
				value = strings.TrimSpace(trimmed[idx+len(marker):])
				break
			}
		}
		if value == "" {
			continue
		}
		value = truncateAssistantRunText(value, 160)
		key := strings.ToLower(value)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, value)
		if len(out) >= 4 {
			break
		}
	}
	return out
}
