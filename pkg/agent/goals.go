package agent

import (
	"context"
	"encoding/json"
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
	goals, err := store.ListGoals()
	if err != nil {
		return nil, err
	}
	for index := range goals {
		goal, err := o.ensureGoalPlan(store, goals[index])
		if err != nil {
			return nil, err
		}
		goals[index] = goal
	}
	return goals, nil
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
	goal, err = o.ensureGoalPlan(store, goal)
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
	decisions, err := store.ListDecisions(goal.ID)
	if err != nil {
		return assistantstore.GoalTimeline{}, err
	}
	reports, err := store.ListTaskReports(goal.ID)
	if err != nil {
		return assistantstore.GoalTimeline{}, err
	}
	return assistantstore.GoalTimeline{
		Goal:        goal,
		Watches:     watches,
		Signals:     signals,
		Notes:       notes,
		Assessments: assessments,
		Decisions:   decisions,
		TaskReports: reports,
	}, nil
}

func (o *Orchestrator) ensureGoalPlan(store *assistantstore.GoalStore, goal assistantstore.Goal) (assistantstore.Goal, error) {
	goal = assistantstore.NormalizeGoal(goal)
	if goal.Plan != nil {
		return goal, nil
	}
	now := time.Now().UTC()
	plan := buildGoalPlan(goal, now)
	goal.Plan = &plan
	goal.UpdatedAt = now
	if err := store.SaveGoal(goal); err != nil {
		return assistantstore.Goal{}, err
	}
	return goal, nil
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
		Target:          req.Target,
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
	if goal.Plan == nil {
		plan := buildGoalPlan(goal, now)
		goal.Plan = &plan
	}
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
	if req.HasField("title") {
		value := strings.TrimSpace(req.Title)
		if value == "" {
			return assistantstore.GoalTimeline{}, fmt.Errorf("goal title is required")
		}
		goal.Title = value
	}
	if req.HasField("objective") {
		value := strings.TrimSpace(req.Objective)
		if value == "" {
			return assistantstore.GoalTimeline{}, fmt.Errorf("goal objective is required")
		}
		goal.Objective = value
	}
	if req.HasField("details") {
		goal.Details = req.Details
	}
	if req.HasField("status") {
		goal.Status = req.Status
		if assistantstore.NormalizeGoal(goal).Status == assistantstore.GoalStatusArchived && goal.ArchivedAt == nil {
			archived := now
			goal.ArchivedAt = &archived
		}
	}
	if req.HasField("kind") {
		goal.Kind = req.Kind
	}
	if req.HasField("execution_mode") {
		goal.ExecutionMode = req.ExecutionMode
	}
	if req.HasField("target") {
		goal.Target = req.Target
	}
	if req.HasField("autopilot") && req.Autopilot != nil {
		goal.Autopilot = mergeGoalAutopilotPatch(goal.Autopilot, req.Autopilot)
		if !req.HasField("execution_mode") {
			goal.ExecutionMode = assistantstore.GoalExecutionModeAutopilot
		}
	}
	if req.HasField("priority") {
		goal.Priority = req.Priority
	}
	if req.HasField("autonomy") {
		goal.Autonomy = req.Autonomy
	}
	if req.HasField("cadence") {
		goal.Cadence = req.Cadence
	}
	if req.HasField("next_check_at") {
		nextCheckAt, err := assistantstore.ParseGoalTime(req.NextCheckAt)
		if err != nil {
			return assistantstore.GoalTimeline{}, fmt.Errorf("next_check_at must be RFC3339: %w", err)
		}
		goal.NextCheckAt = nextCheckAt
	}
	if req.HasField("success_criteria") {
		goal.SuccessCriteria = req.SuccessCriteria
	}
	if req.HasField("constraints") {
		goal.Constraints = req.Constraints
	}
	if req.HasField("progress_summary") {
		goal.ProgressSummary = req.ProgressSummary
	}
	if req.HasField("open_questions") {
		goal.OpenQuestions = req.OpenQuestions
	}
	shouldReconcile := goalUpdateShouldReconcile(goal, req)
	planNeedsRevision := goalUpdateShouldRevisePlan(req)
	goal.UpdatedAt = now
	goal = assistantstore.NormalizeGoal(goal)
	if planNeedsRevision || goal.Plan == nil {
		plan := buildGoalPlan(goal, now)
		goal.Plan = &plan
	}
	if goalUpdateShouldUnblockPlan(req, goal) {
		unblockGoalPlan(&goal, now)
		if goal.Status == assistantstore.GoalStatusBlocked && len(goal.OpenQuestions) == 0 {
			goal.Status = assistantstore.GoalStatusActive
		}
	}
	if goal.Autopilot != nil && goal.Autopilot.Status == assistantstore.GoalAutopilotStatusBudgetExhausted && goalAutopilotBudgetAllowsMore(*goal.Autopilot) {
		goal.Autopilot.Status = assistantstore.GoalAutopilotStatusRunning
		goal.Autopilot.StopReasons = nil
		goal.Autopilot.LastStepAt = &now
		shouldReconcile = true
		if goal.Status == assistantstore.GoalStatusBlocked {
			goal.Status = assistantstore.GoalStatusActive
		}
	}
	if goal.Status != assistantstore.GoalStatusArchived {
		goal.ArchivedAt = nil
	}
	if err := store.SaveGoal(goal); err != nil {
		return assistantstore.GoalTimeline{}, err
	}
	if planNeedsRevision {
		o.saveGoalSupervisorDecision(ctx, store, &goal, assistantstore.GoalSupervisorDecision{
			Decision:  assistantstore.GoalSupervisorDecisionRevisePlan,
			Summary:   "Goal plan revised after operator-edited Goal context.",
			Rationale: "The objective, details, success criteria, constraints, or Goal type changed; stale phase assumptions were replaced before creating more work.",
		})
	}
	o.appendGoalEvent(ctx, "assistant.goal.updated", goal, map[string]any{"goal": goal})
	_ = store.SaveNote(assistantstore.GoalNote{
		ID:        id.New("gnote"),
		GoalID:    goal.ID,
		Kind:      "updated",
		Title:     "Goal updated",
		Body:      goalUpdateNote(req, goal),
		CreatedBy: "assistant",
		CreatedAt: now,
	})
	if shouldReconcile && goal.ExecutionMode == assistantstore.GoalExecutionModeAutopilot && goal.Autopilot != nil && goal.Autopilot.Status == assistantstore.GoalAutopilotStatusRunning {
		_, _, err = o.reconcileGoalAutopilot(ctx, store, goal)
		if err != nil {
			return assistantstore.GoalTimeline{}, err
		}
	}
	return o.LoadGoal(goal.ID)
}

func mergeGoalAutopilotPatch(current *assistantstore.GoalAutopilot, patch *assistantstore.GoalAutopilot) *assistantstore.GoalAutopilot {
	if patch == nil {
		return current
	}
	merged := assistantstore.NormalizeGoalAutopilot(current)
	if strings.TrimSpace(patch.Status) != "" {
		merged.Status = patch.Status
	}
	if patch.BudgetTasks != 0 {
		merged.BudgetTasks = patch.BudgetTasks
	}
	if patch.MaxRuntimeMinutes != 0 {
		merged.MaxRuntimeMinutes = patch.MaxRuntimeMinutes
	}
	if patch.StartedAt != nil {
		merged.StartedAt = patch.StartedAt
	}
	if patch.LastStepAt != nil {
		merged.LastStepAt = patch.LastStepAt
	}
	if len(patch.StopReasons) > 0 {
		merged.StopReasons = append([]string(nil), patch.StopReasons...)
	}
	if len(patch.AllowedActions) > 0 {
		merged.AllowedActions = append([]string(nil), patch.AllowedActions...)
	}
	if strings.TrimSpace(patch.CurrentTaskID) != "" {
		merged.CurrentTaskID = patch.CurrentTaskID
	}
	normalized := assistantstore.NormalizeGoalAutopilot(&merged)
	return &normalized
}

func goalUpdateShouldReconcile(goal assistantstore.Goal, req assistantstore.GoalUpdateRequest) bool {
	if goal.ExecutionMode != assistantstore.GoalExecutionModeAutopilot || goal.Autopilot == nil {
		return false
	}
	if req.HasField("status") && assistantstore.NormalizeGoal(goal).Status != assistantstore.GoalStatusActive {
		return false
	}
	for _, field := range []string{"title", "objective", "details", "status", "kind", "target", "autopilot", "success_criteria", "constraints", "open_questions"} {
		if req.HasField(field) {
			return true
		}
	}
	return false
}

func goalUpdateShouldRevisePlan(req assistantstore.GoalUpdateRequest) bool {
	for _, field := range []string{"title", "objective", "details", "kind", "success_criteria", "constraints"} {
		if req.HasField(field) {
			return true
		}
	}
	return false
}

func goalUpdateShouldUnblockPlan(req assistantstore.GoalUpdateRequest, goal assistantstore.Goal) bool {
	if goal.Plan == nil || goal.ExecutionMode != assistantstore.GoalExecutionModeAutopilot || goal.Autopilot == nil {
		return false
	}
	if assistantstore.NormalizeGoalPlan(*goal.Plan).Status != assistantstore.GoalPlanStatusBlocked {
		return false
	}
	if req.HasField("open_questions") && len(goal.OpenQuestions) == 0 {
		return true
	}
	if req.HasField("status") && goal.Status == assistantstore.GoalStatusActive && len(goal.OpenQuestions) == 0 {
		return true
	}
	if req.HasField("autopilot") && goal.Autopilot.Status == assistantstore.GoalAutopilotStatusRunning && len(goal.OpenQuestions) == 0 {
		return true
	}
	return false
}

func unblockGoalPlan(goal *assistantstore.Goal, now time.Time) {
	if goal == nil || goal.Plan == nil {
		return
	}
	plan := assistantstore.NormalizeGoalPlan(*goal.Plan)
	if plan.Status != assistantstore.GoalPlanStatusBlocked {
		return
	}
	plan.Status = assistantstore.GoalPlanStatusActive
	for index := range plan.Phases {
		if plan.Phases[index].Status != assistantstore.GoalPlanPhaseStatusBlocked {
			continue
		}
		if len(plan.Phases[index].TaskIDs) > 0 {
			plan.Phases[index].Status = assistantstore.GoalPlanPhaseStatusInProgress
		} else {
			plan.Phases[index].Status = assistantstore.GoalPlanPhaseStatusPending
		}
	}
	if next, ok := selectGoalPlanPhase(&plan); ok {
		plan.CurrentPhaseID = next.ID
	}
	plan.UpdatedAt = now
	goal.Plan = &plan
	if goal.Autopilot != nil {
		goal.Autopilot.CurrentPhaseID = plan.CurrentPhaseID
	}
}

func goalUpdateNote(req assistantstore.GoalUpdateRequest, goal assistantstore.Goal) string {
	labels := []string{}
	fields := []struct {
		key   string
		label string
	}{
		{"title", "title"},
		{"objective", "objective"},
		{"details", "details"},
		{"status", "status"},
		{"kind", "type"},
		{"execution_mode", "execution mode"},
		{"target", "target"},
		{"autopilot", "Autopilot settings"},
		{"priority", "priority"},
		{"autonomy", "autonomy"},
		{"cadence", "cadence"},
		{"next_check_at", "next check"},
		{"success_criteria", "success criteria"},
		{"constraints", "constraints"},
		{"progress_summary", "progress summary"},
		{"open_questions", "open questions"},
	}
	for _, field := range fields {
		if req.HasField(field.key) {
			labels = append(labels, field.label)
		}
	}
	if len(labels) == 0 {
		return "Goal metadata refreshed."
	}
	return fmt.Sprintf("Updated %s for `%s`.", strings.Join(labels, ", "), goal.Title)
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
		if autopilot.Status == assistantstore.GoalAutopilotStatusBudgetExhausted && !goalAutopilotBudgetAllowsMore(autopilot) {
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
	if goal.Plan == nil {
		plan := buildGoalPlan(goal, now)
		goal.Plan = &plan
	}
	if (action == "start" || action == "resume") && len(goal.OpenQuestions) == 0 {
		unblockGoalPlan(&goal, now)
	}
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
	if req.BudgetTasks != 0 {
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
		return fmt.Sprintf("Autopilot started for this %s Goal with %s.", goal.Kind, goalAutopilotTaskLimitLabel(budget))
	case "resume":
		return fmt.Sprintf("Autopilot resumed for this %s Goal. Tasks started: %s.", goal.Kind, goalAutopilotProgressLabel(tasksStarted, budget))
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
	if goal.Plan == nil {
		plan := buildGoalPlan(goal, now)
		goal.Plan = &plan
		goal.UpdatedAt = now
		if err := store.SaveGoal(goal); err != nil {
			return false, "", err
		}
		o.saveGoalSupervisorDecision(ctx, store, &goal, assistantstore.GoalSupervisorDecision{
			Decision:  assistantstore.GoalSupervisorDecisionRevisePlan,
			Summary:   "Created a Goal plan before Autopilot selected more work.",
			Rationale: "Autopilot needs a durable phase plan so each task advances an explicit part of the objective.",
		})
	}
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
	if !goalAutopilotBudgetAllowsMore(*goal.Autopilot) {
		return o.blockOrStopGoalAutopilot(ctx, store, goal, assistantstore.GoalAutopilotStatusBudgetExhausted, fmt.Sprintf("Autopilot task limit exhausted (%s).", goalAutopilotProgressLabel(goal.Autopilot.TasksStarted, goal.Autopilot.BudgetTasks)))
	}
	if !goalAutopilotAllows(goal, "create_task") {
		return o.blockOrStopGoalAutopilot(ctx, store, goal, assistantstore.GoalAutopilotStatusBlocked, "Autopilot policy does not allow creating tasks.")
	}
	decision, err := o.goalSupervisorDecision(ctx, store, goal)
	if err != nil {
		return false, "", err
	}
	switch decision.Decision {
	case assistantstore.GoalSupervisorDecisionCreateTask:
		return o.createGoalAutopilotTask(ctx, store, goal, decision)
	case assistantstore.GoalSupervisorDecisionAskQuestion:
		goal.OpenQuestions = appendUniqueStrings(goal.OpenQuestions, decision.Questions...)
		if goal.Autopilot != nil {
			goal.Autopilot.LastDecisionID = decision.ID
		}
		goal.Status = assistantstore.GoalStatusBlocked
		return o.blockOrStopGoalAutopilot(ctx, store, goal, assistantstore.GoalAutopilotStatusBlocked, firstNonEmptyString(decision.StopReason, "Autopilot needs operator input before creating another task."))
	case assistantstore.GoalSupervisorDecisionMarkComplete:
		return o.completeGoalAutopilot(ctx, store, goal, decision)
	case assistantstore.GoalSupervisorDecisionPauseBlocked:
		if goal.Autopilot != nil {
			goal.Autopilot.LastDecisionID = decision.ID
		}
		return o.blockOrStopGoalAutopilot(ctx, store, goal, assistantstore.GoalAutopilotStatusBlocked, firstNonEmptyString(decision.StopReason, decision.Summary, "Autopilot blocked before creating another task."))
	default:
		return false, firstNonEmptyString(decision.Summary, "Autopilot is waiting for more evidence before creating another task."), nil
	}
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
		if remoteTask(t) {
			reply, err := o.reviewRemoteTask(ctx, t)
			if err != nil {
				return false, "", err
			}
			return true, fmt.Sprintf("Autopilot acknowledged remote review for task %s.\n%s", taskShortID(t.ID), reply), nil
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
	case taskstore.StatusTimedOut, taskstore.StatusFailed, taskstore.StatusCancelled:
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

func (o *Orchestrator) createGoalAutopilotTask(ctx context.Context, store *assistantstore.GoalStore, goal assistantstore.Goal, decision assistantstore.GoalSupervisorDecision) (bool, string, error) {
	goal = assistantstore.NormalizeGoal(goal)
	reports, _ := store.ListTaskReports(goal.ID)
	taskGoal := strings.TrimSpace(decision.TaskGoal)
	if taskGoal == "" {
		taskGoal = goalAutopilotTaskGoalForDecision(goal, decision, reports)
	}
	created, err := o.createTaskRecordForGoalPhase(ctx, taskGoal, goal, decision.PhaseID)
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
	goal.Autopilot.CurrentPhaseID = strings.TrimSpace(decision.PhaseID)
	goal.Autopilot.LastStepAt = &now
	goal.Autopilot.LastDecisionID = strings.TrimSpace(decision.ID)
	goal.LastActionAt = &now
	goal.UpdatedAt = now
	if !stringSliceContains(goal.LinkedTasks, created.Task.ID) {
		goal.LinkedTasks = append(goal.LinkedTasks, created.Task.ID)
	}
	markGoalPlanTaskCreated(&goal, decision.PhaseID, created.Task.ID, now)
	if err := store.SaveGoal(goal); err != nil {
		return false, "", err
	}
	decision.TaskID = created.Task.ID
	decision.TaskGoal = taskGoal
	o.saveGoalSupervisorDecision(ctx, store, &goal, decision)
	_ = store.SaveNote(assistantstore.GoalNote{
		ID:        id.New("gnote"),
		GoalID:    goal.ID,
		Kind:      "autopilot",
		Title:     "Autopilot task created",
		Body:      fmt.Sprintf("Created %s task %s for this %s Goal phase `%s` (%s task limit).", goal.ExecutionMode, taskShortID(created.Task.ID), goal.Kind, firstNonEmptyString(decision.PhaseID, "unplanned"), goalAutopilotProgressLabel(goal.Autopilot.TasksStarted, goal.Autopilot.BudgetTasks)),
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
	if remoteTask(t) && t.Target != nil {
		return fmt.Sprintf("Remote agent %s will claim it from %s on the next poll.", t.Target.AgentID, t.Target.Workdir)
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

func markGoalPlanTaskCreated(goal *assistantstore.Goal, phaseID, taskID string, now time.Time) {
	if goal == nil || goal.Plan == nil {
		return
	}
	phaseID = strings.TrimSpace(phaseID)
	taskID = strings.TrimSpace(taskID)
	if phaseID == "" || taskID == "" {
		return
	}
	plan := assistantstore.NormalizeGoalPlan(*goal.Plan)
	plan.CurrentPhaseID = phaseID
	plan.UpdatedAt = now
	for index := range plan.Phases {
		if plan.Phases[index].ID != phaseID {
			continue
		}
		if plan.Phases[index].Status == assistantstore.GoalPlanPhaseStatusPending {
			plan.Phases[index].Status = assistantstore.GoalPlanPhaseStatusInProgress
		}
		if !stringSliceContains(plan.Phases[index].TaskIDs, taskID) {
			plan.Phases[index].TaskIDs = append(plan.Phases[index].TaskIDs, taskID)
		}
		break
	}
	goal.Plan = &plan
}

func (o *Orchestrator) saveGoalSupervisorDecision(ctx context.Context, store *assistantstore.GoalStore, goal *assistantstore.Goal, decision assistantstore.GoalSupervisorDecision) assistantstore.GoalSupervisorDecision {
	if store == nil || goal == nil {
		return assistantstore.NormalizeGoalSupervisorDecision(decision)
	}
	if strings.TrimSpace(decision.ID) == "" {
		decision.ID = id.New("gdec")
	}
	decision.GoalID = goal.ID
	if decision.CreatedAt.IsZero() {
		decision.CreatedAt = time.Now().UTC()
	}
	decision = assistantstore.NormalizeGoalSupervisorDecision(decision)
	if err := store.SaveDecision(decision); err != nil {
		o.log().Warn("goal supervisor decision save failed", "error", err, "goal", goal.ID)
		return decision
	}
	if goal.Autopilot != nil {
		goal.Autopilot.LastDecisionID = decision.ID
	}
	_ = o.events.Append(ctx, eventlog.Event{ID: id.New("evt"), Type: "assistant.goal.supervisor.decision", Actor: "Assistant", Payload: eventlog.Payload(map[string]any{
		"goal_id":  goal.ID,
		"decision": decision,
	})})
	return decision
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
		if goal.Plan != nil {
			plan := assistantstore.NormalizeGoalPlan(*goal.Plan)
			plan.Status = assistantstore.GoalPlanStatusBlocked
			plan.UpdatedAt = now
			goal.Plan = &plan
		}
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

func (o *Orchestrator) goalSupervisorDecision(ctx context.Context, store *assistantstore.GoalStore, goal assistantstore.Goal) (assistantstore.GoalSupervisorDecision, error) {
	goal = assistantstore.NormalizeGoal(goal)
	now := time.Now().UTC()
	reports, err := store.ListTaskReports(goal.ID)
	if err != nil {
		return assistantstore.GoalSupervisorDecision{}, err
	}
	if len(goal.OpenQuestions) > 0 {
		decision := assistantstore.GoalSupervisorDecision{
			Decision:   assistantstore.GoalSupervisorDecisionAskQuestion,
			Summary:    "Autopilot needs operator answers before it can select the next task.",
			Rationale:  "Open Goal questions are part of the Goal contract and must block autonomous task creation.",
			Questions:  append([]string(nil), goal.OpenQuestions...),
			StopReason: "Autopilot is blocked by open Goal questions.",
			Evidence:   goalSupervisorEvidence(goal, reports),
		}
		decision = o.saveGoalSupervisorDecision(ctx, store, &goal, decision)
		return decision, nil
	}
	if goal.Plan != nil && assistantstore.NormalizeGoalPlan(*goal.Plan).Status == assistantstore.GoalPlanStatusBlocked {
		decision := assistantstore.GoalSupervisorDecision{
			Decision:   assistantstore.GoalSupervisorDecisionPauseBlocked,
			Summary:    "Goal plan is blocked.",
			Rationale:  "A linked task reported blockers or questions for the current plan phase.",
			StopReason: "Goal plan is blocked by the current phase.",
			Evidence:   goalSupervisorEvidence(goal, reports),
		}
		decision = o.saveGoalSupervisorDecision(ctx, store, &goal, decision)
		return decision, nil
	}
	if recentGoalReportCompletes(reports) || goalPlanComplete(goal.Plan) {
		decision := assistantstore.GoalSupervisorDecision{
			Decision:   assistantstore.GoalSupervisorDecisionMarkComplete,
			Summary:    "Goal plan is complete.",
			Rationale:  "The latest structured task report or all plan phases indicate the Goal has reached its done condition.",
			StopReason: "Goal success criteria are satisfied or no remaining plan phase needs work.",
			Evidence:   goalSupervisorEvidence(goal, reports),
		}
		decision = o.saveGoalSupervisorDecision(ctx, store, &goal, decision)
		return decision, nil
	}
	if noProgress, reason := recentGoalReportsShowNoProgress(reports); noProgress {
		decision := assistantstore.GoalSupervisorDecision{
			Decision:   assistantstore.GoalSupervisorDecisionPauseBlocked,
			Summary:    "Autopilot stopped before creating another low-value task.",
			Rationale:  reason,
			StopReason: reason,
			Evidence:   goalSupervisorEvidence(goal, reports),
		}
		decision = o.saveGoalSupervisorDecision(ctx, store, &goal, decision)
		return decision, nil
	}
	phase, ok := selectGoalPlanPhase(goal.Plan)
	if !ok {
		decision := assistantstore.GoalSupervisorDecision{
			Decision:   assistantstore.GoalSupervisorDecisionMarkComplete,
			Summary:    "No remaining Goal plan phase needs work.",
			Rationale:  "Every known plan phase is completed, skipped, or unavailable.",
			StopReason: "No remaining plan phase.",
			Evidence:   goalSupervisorEvidence(goal, reports),
		}
		decision = o.saveGoalSupervisorDecision(ctx, store, &goal, decision)
		return decision, nil
	}
	taskGoal := goalAutopilotTaskGoalForDecision(goal, assistantstore.GoalSupervisorDecision{PhaseID: phase.ID}, reports)
	decision := assistantstore.GoalSupervisorDecision{
		ID:        id.New("gdec"),
		GoalID:    goal.ID,
		Decision:  assistantstore.GoalSupervisorDecisionCreateTask,
		Summary:   "Create the next task for plan phase `" + phase.ID + "`.",
		Rationale: "The selected phase is the next incomplete, dependency-ready phase in the Goal plan.",
		PhaseID:   phase.ID,
		TaskGoal:  taskGoal,
		Evidence:  goalSupervisorEvidence(goal, reports),
		CreatedAt: now,
	}
	return assistantstore.NormalizeGoalSupervisorDecision(decision), nil
}

func (o *Orchestrator) completeGoalAutopilot(ctx context.Context, store *assistantstore.GoalStore, goal assistantstore.Goal, decision assistantstore.GoalSupervisorDecision) (bool, string, error) {
	goal = assistantstore.NormalizeGoal(goal)
	now := time.Now().UTC()
	goal.Status = assistantstore.GoalStatusCompleted
	if goal.Plan != nil {
		plan := assistantstore.NormalizeGoalPlan(*goal.Plan)
		plan.Status = assistantstore.GoalPlanStatusCompleted
		for index := range plan.Phases {
			if plan.Phases[index].Status != assistantstore.GoalPlanPhaseStatusSkipped {
				plan.Phases[index].Status = assistantstore.GoalPlanPhaseStatusCompleted
			}
		}
		plan.UpdatedAt = now
		goal.Plan = &plan
	}
	if goal.Autopilot == nil {
		autopilot := assistantstore.NormalizeGoalAutopilot(nil)
		goal.Autopilot = &autopilot
	}
	goal.Autopilot.Status = assistantstore.GoalAutopilotStatusCompleted
	goal.Autopilot.CurrentTaskID = ""
	goal.Autopilot.CurrentPhaseID = ""
	goal.Autopilot.LastDecisionID = decision.ID
	goal.Autopilot.LastStepAt = &now
	goal.Autopilot.StopReasons = appendGoalStopReason(goal.Autopilot.StopReasons, firstNonEmptyString(decision.StopReason, "Goal plan completed."))
	goal.UpdatedAt = now
	if err := store.SaveGoal(goal); err != nil {
		return false, "", err
	}
	o.saveGoalSupervisorDecision(ctx, store, &goal, decision)
	_ = store.SaveNote(assistantstore.GoalNote{
		ID:        id.New("gnote"),
		GoalID:    goal.ID,
		Kind:      "autopilot",
		Title:     "Autopilot completed",
		Body:      firstNonEmptyString(decision.Summary, decision.StopReason, "Goal plan completed."),
		CreatedBy: "assistant",
		CreatedAt: now,
	})
	o.appendGoalEvent(ctx, "assistant.goal.autopilot.completed", goal, map[string]any{"goal_id": goal.ID, "reason": decision.StopReason, "autopilot": goal.Autopilot})
	return true, firstNonEmptyString(decision.StopReason, decision.Summary, "Goal completed."), nil
}

func goalSupervisorEvidence(goal assistantstore.Goal, reports []assistantstore.GoalTaskReport) []string {
	var evidence []string
	if goal.Plan != nil {
		phase, ok := selectGoalPlanPhase(goal.Plan)
		if ok {
			evidence = append(evidence, "next phase: "+phase.ID+" - "+phase.Title)
		}
	}
	for _, report := range reports {
		if len(evidence) >= 6 {
			break
		}
		evidence = append(evidence, fmt.Sprintf("task %s: %s", taskShortID(report.TaskID), firstNonEmptyString(report.Summary, report.Status)))
	}
	return evidence
}

func recentGoalReportCompletes(reports []assistantstore.GoalTaskReport) bool {
	if len(reports) == 0 {
		return false
	}
	return reports[0].GoalComplete
}

func recentGoalReportsShowNoProgress(reports []assistantstore.GoalTaskReport) (bool, string) {
	if len(reports) < 2 {
		return false, ""
	}
	noProgress := 0
	for _, report := range reports[:assistantMinInt(len(reports), 3)] {
		if goalReportShowsNoProgress(report) {
			noProgress++
		}
	}
	if noProgress >= 2 {
		return true, "Two recent Goal-linked tasks reported no measurable progress; Autopilot needs a plan revision or operator input."
	}
	return false, ""
}

func goalReportShowsNoProgress(report assistantstore.GoalTaskReport) bool {
	report = assistantstore.NormalizeGoalTaskReport(report)
	hasDiff := report.DiffFiles > 0 || len(report.ChangedFiles) > 0
	switch report.ReviewDecision {
	case goalTaskReviewVerifiedProgress, goalTaskReviewBlockedWithProgress:
		return false
	case goalTaskReviewNeedsValidation:
		return false
	case goalTaskReviewMisaligned:
		return true
	case goalTaskReviewInsufficientEvidence, goalTaskReviewNoChange:
		return true
	}
	if hasDiff {
		return false
	}
	return report.NoChange || !report.AdvancedGoal
}

func goalPlanComplete(plan *assistantstore.GoalPlan) bool {
	if plan == nil || len(plan.Phases) == 0 {
		return false
	}
	planValue := assistantstore.NormalizeGoalPlan(*plan)
	if planValue.Status == assistantstore.GoalPlanStatusCompleted {
		return true
	}
	for _, phase := range planValue.Phases {
		if phase.Status != assistantstore.GoalPlanPhaseStatusCompleted && phase.Status != assistantstore.GoalPlanPhaseStatusSkipped {
			return false
		}
	}
	return true
}

func selectGoalPlanPhase(plan *assistantstore.GoalPlan) (assistantstore.GoalPlanPhase, bool) {
	if plan == nil {
		return assistantstore.GoalPlanPhase{}, false
	}
	planValue := assistantstore.NormalizeGoalPlan(*plan)
	completed := map[string]bool{}
	for _, phase := range planValue.Phases {
		if phase.Status == assistantstore.GoalPlanPhaseStatusCompleted || phase.Status == assistantstore.GoalPlanPhaseStatusSkipped {
			completed[phase.ID] = true
		}
	}
	for _, phase := range planValue.Phases {
		if phase.Status == assistantstore.GoalPlanPhaseStatusCompleted || phase.Status == assistantstore.GoalPlanPhaseStatusSkipped || phase.Status == assistantstore.GoalPlanPhaseStatusBlocked {
			continue
		}
		ready := true
		for _, dep := range phase.DependsOn {
			if !completed[dep] {
				ready = false
				break
			}
		}
		if ready {
			return phase, true
		}
	}
	return assistantstore.GoalPlanPhase{}, false
}

func goalAutopilotBudgetAllowsMore(autopilot assistantstore.GoalAutopilot) bool {
	autopilot = assistantstore.NormalizeGoalAutopilot(&autopilot)
	return autopilot.BudgetTasks == assistantstore.GoalAutopilotUnlimitedBudget || autopilot.TasksStarted < autopilot.BudgetTasks
}

func goalAutopilotTaskLimitLabel(budget int) string {
	if budget == assistantstore.GoalAutopilotUnlimitedBudget {
		return "an unlimited task limit"
	}
	return fmt.Sprintf("a %d task limit", budget)
}

func goalAutopilotProgressLabel(started, budget int) string {
	if budget == assistantstore.GoalAutopilotUnlimitedBudget {
		return fmt.Sprintf("%d/unlimited", started)
	}
	return fmt.Sprintf("%d/%d", started, budget)
}

func goalTimePtr(value time.Time) *time.Time {
	value = value.UTC()
	return &value
}

func buildGoalPlan(goal assistantstore.Goal, now time.Time) assistantstore.GoalPlan {
	goal = assistantstore.NormalizeGoal(goal)
	if now.IsZero() {
		now = time.Now().UTC()
	}
	phases := goalPlanPhases(goal)
	current := ""
	if len(phases) > 0 {
		current = phases[0].ID
	}
	return assistantstore.NormalizeGoalPlan(assistantstore.GoalPlan{
		Status:         assistantstore.GoalPlanStatusActive,
		Summary:        "Supervisor plan for " + goal.Title,
		CurrentPhaseID: current,
		Phases:         phases,
		CreatedAt:      now,
		UpdatedAt:      now,
	})
}

func goalPlanPhases(goal assistantstore.Goal) []assistantstore.GoalPlanPhase {
	if len(goal.SuccessCriteria) > 0 {
		phases := make([]assistantstore.GoalPlanPhase, 0, len(goal.SuccessCriteria)+1)
		var previous string
		for index, criterion := range goal.SuccessCriteria {
			idValue := fmt.Sprintf("phase_%02d", index+1)
			phase := assistantstore.GoalPlanPhase{
				ID:        idValue,
				Title:     truncateAssistantRunText(firstLine(criterion), 80),
				Objective: criterion,
				Status:    assistantstore.GoalPlanPhaseStatusPending,
				DependsOn: nonEmptyStringSlice(previous),
				AcceptanceCriteria: []string{
					"Evidence shows this success criterion is satisfied.",
					"Validation or inspection result is recorded in the task report.",
				},
			}
			phases = append(phases, phase)
			previous = idValue
		}
		return phases
	}
	switch goal.Kind {
	case assistantstore.GoalKindRoutine:
		return linkedGoalPlanPhases(
			phase("phase_01_baseline", "Understand the routine state", "Inspect current inputs, outputs, cadence, and failure modes for this routine.", nil),
			phase("phase_02_cycle", "Run one complete routine cycle", "Perform the routine end to end and update durable state or notes.", []string{"phase_01_baseline"}),
			phase("phase_03_harden", "Harden repeated execution", "Add checks, docs, or workflow improvements that make the routine reliable.", []string{"phase_02_cycle"}),
		)
	case assistantstore.GoalKindWatch:
		return linkedGoalPlanPhases(
			phase("phase_01_signal", "Define the watched signal", "Inspect the condition and identify concrete evidence that should trigger action.", nil),
			phase("phase_02_response", "Implement the response path", "Create the smallest safe response, task, note, or workflow for the watched condition.", []string{"phase_01_signal"}),
			phase("phase_03_verify", "Verify watch usefulness", "Prove the watch can be reviewed, suppressed, or acted on without noise.", []string{"phase_02_response"}),
		)
	case assistantstore.GoalKindMaintenance:
		return linkedGoalPlanPhases(
			phase("phase_01_diagnose", "Diagnose maintenance scope", "Identify the highest-value upkeep issue and current evidence.", nil),
			phase("phase_02_repair", "Make the maintenance improvement", "Apply the smallest cohesive repair with focused tests and docs.", []string{"phase_01_diagnose"}),
			phase("phase_03_verify", "Verify reliability", "Run the relevant checks and record any remaining maintenance risk.", []string{"phase_02_repair"}),
		)
	default:
		return linkedGoalPlanPhases(
			phase("phase_01_foundation", "Establish the architecture and baseline", "Inspect the target repo, define the clean-room architecture, and create the first usable foundation.", nil),
			phase("phase_02_core", "Build the core capability", "Implement the central behaviour needed for the Goal to be useful end to end.", []string{"phase_01_foundation"}),
			phase("phase_03_parity", "Expand feature parity", "Add the next set of important features, styling, compatibility, or operational behaviours.", []string{"phase_02_core"}),
			phase("phase_04_validation", "Prove completion", "Add examples, tests, docs, performance or browser evidence, and close remaining gaps against the Goal.", []string{"phase_03_parity"}),
		)
	}
}

func phase(idValue, title, objective string, dependsOn []string) assistantstore.GoalPlanPhase {
	return assistantstore.GoalPlanPhase{
		ID:        idValue,
		Title:     title,
		Objective: objective,
		Status:    assistantstore.GoalPlanPhaseStatusPending,
		DependsOn: dependsOn,
		AcceptanceCriteria: []string{
			"Changed files and validation evidence map to the phase objective.",
			"Remaining gaps or blockers are recorded as concrete follow-ups or questions.",
			"The supervisor can choose the next slice from durable repo evidence, not only raw worker logs.",
		},
	}
}

func linkedGoalPlanPhases(phases ...assistantstore.GoalPlanPhase) []assistantstore.GoalPlanPhase {
	return phases
}

func nonEmptyStringSlice(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return []string{strings.TrimSpace(value)}
}

func goalAutopilotTaskGoal(goal assistantstore.Goal) string {
	return goalAutopilotTaskGoalForDecision(goal, assistantstore.GoalSupervisorDecision{}, nil)
}

func goalAutopilotTaskGoalForDecision(goal assistantstore.Goal, decision assistantstore.GoalSupervisorDecision, reports []assistantstore.GoalTaskReport) string {
	goal = assistantstore.NormalizeGoal(goal)
	phase, hasPhase := goalPlanPhaseByID(goal.Plan, decision.PhaseID)
	kindInstruction := "Pick the next bounded implementation slice that measurably advances the objective."
	switch goal.Kind {
	case assistantstore.GoalKindRoutine:
		kindInstruction = "Run one complete routine cycle, update the durable state, and create or change code only when that is required by the objective."
	case assistantstore.GoalKindWatch:
		kindInstruction = "Inspect the watched condition, gather evidence, and make only the bounded change or follow-up needed by the condition."
	case assistantstore.GoalKindMaintenance:
		kindInstruction = "Pick one bounded upkeep issue that improves reliability, clarity, tests, docs, or operator ergonomics."
	case assistantstore.GoalKindBuild:
		kindInstruction = "Pick one bounded product slice from a durable feature matrix or create that matrix first, then implement and validate the slice end to end."
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Autopilot task for %s Goal `%s`: %s\n\nObjective: %s", goal.Kind, goal.ID, goal.Title, goal.Objective)
	if hasPhase {
		fmt.Fprintf(&b, "\n\nSelected Goal plan phase:\n- Phase ID: %s\n- Title: %s\n- Objective: %s", phase.ID, phase.Title, phase.Objective)
		if len(phase.AcceptanceCriteria) > 0 {
			b.WriteString("\n- Phase acceptance:")
			for _, criterion := range phase.AcceptanceCriteria {
				fmt.Fprintf(&b, "\n  - %s", criterion)
			}
		}
	}
	if goal.Details != "" {
		fmt.Fprintf(&b, "\n\nDetails:\n%s", goal.Details)
	}
	if goal.ProgressSummary != "" {
		fmt.Fprintf(&b, "\n\nCurrent progress:\n%s", goal.ProgressSummary)
	}
	if goal.Plan != nil {
		b.WriteString("\n\nGoal plan:")
		for _, phase := range goal.Plan.Phases {
			fmt.Fprintf(&b, "\n- %s [%s]: %s", phase.ID, phase.Status, phase.Title)
		}
	}
	if len(reports) > 0 {
		b.WriteString("\n\nRecent structured Goal task reports:")
		for _, report := range reports[:assistantMinInt(len(reports), 4)] {
			fmt.Fprintf(&b, "\n- Task %s phase %s: %s", taskShortID(report.TaskID), firstNonEmptyString(report.PhaseID, "unknown"), firstNonEmptyString(report.Summary, report.Status))
			if len(report.ChangedFiles) > 0 {
				fmt.Fprintf(&b, " Changed files: %s.", strings.Join(report.ChangedFiles[:assistantMinInt(len(report.ChangedFiles), 8)], ", "))
			}
			if len(report.FollowUps) > 0 {
				fmt.Fprintf(&b, " Follow-ups: %s.", strings.Join(report.FollowUps[:assistantMinInt(len(report.FollowUps), 4)], "; "))
			}
			if len(report.Blockers) > 0 {
				fmt.Fprintf(&b, " Blockers: %s.", strings.Join(report.Blockers[:assistantMinInt(len(report.Blockers), 4)], "; "))
			}
		}
	}
	b.WriteString("\n\nAutopilot work mode:")
	fmt.Fprintf(&b, "\n- %s", kindInstruction)
	b.WriteString("\n- Work the selected slice to completion in this task, including implementation, docs, focused tests, and required browser UAT for changed UI.")
	b.WriteString("\n- Do not split the work into a daily drip. If the Goal needs one large feature, build the largest coherent safe slice this task can complete.")
	if goal.Kind == assistantstore.GoalKindBuild {
		b.WriteString("\n- Maintain a durable feature/parity matrix in the target repo. If the repo has no matrix yet, create one before coding, mark the current slice, and update it with evidence before handoff.")
		b.WriteString("\n- Close a concrete matrix item. Avoid broad \"continue building\" work unless it is backed by specific acceptance criteria, changed files, validation, and remaining gaps.")
	}
	b.WriteString("\n- Use existing approval, merge, restart, and verification gates. Autopilot may pass those gates for this Goal when checks succeed.")
	b.WriteString("\n- If the next step needs credentials, private operator judgement, destructive action, or unresolved product direction, stop and report the blocker instead of guessing.")
	b.WriteString("\n- Do not repeat previous task work. Use the recent structured Goal reports and selected phase to choose a concrete next step.")
	b.WriteString("\n\nGoal supervisor report contract:")
	b.WriteString("\nAt the end of the task result, include a single-line JSON object prefixed with `GOAL_REPORT:`.")
	b.WriteString("\nThe object must include: summary, advanced_goal, phase_complete, goal_complete, changed_files, validation, follow_ups, blockers, questions.")
	b.WriteString("\nThe reviewer will independently compare the diff, validation, and changed files against the Goal; do not claim progress unless the task materially advances the selected phase.")
	b.WriteString("\nUse questions for product or operator decisions that should block further Autopilot tasks.")
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

func goalPlanPhaseByID(plan *assistantstore.GoalPlan, phaseID string) (assistantstore.GoalPlanPhase, bool) {
	phaseID = strings.TrimSpace(phaseID)
	if plan == nil || phaseID == "" {
		return assistantstore.GoalPlanPhase{}, false
	}
	for _, phase := range plan.Phases {
		if phase.ID == phaseID {
			return phase, true
		}
	}
	return assistantstore.GoalPlanPhase{}, false
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
			fmt.Fprintf(&b, " Autopilot: %s %s", goal.Autopilot.Status, goalAutopilotProgressLabel(goal.Autopilot.TasksStarted, goal.Autopilot.BudgetTasks))
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
		fmt.Fprintf(&b, "\nAutopilot: %s (%s tasks)", goal.Autopilot.Status, goalAutopilotProgressLabel(goal.Autopilot.TasksStarted, goal.Autopilot.BudgetTasks))
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
	return o.createTaskRecordForGoalPhase(ctx, taskGoal, goal, "")
}

func (o *Orchestrator) createTaskRecordForGoalPhase(ctx context.Context, taskGoal string, goal assistantstore.Goal, phaseID string) (createdTask, error) {
	taskGoal = strings.TrimSpace(taskGoal)
	if taskGoal == "" {
		return createdTask{}, nil
	}
	goal = assistantstore.NormalizeGoal(goal)
	created, _, err := o.createTaskRecordWithRoutedTarget(ctx, taskGoalWithGoalContext(taskGoal, goal), goal.Target, nil, taskCreateOptions{
		GoalID:        goal.ID,
		GoalPhaseID:   strings.TrimSpace(phaseID),
		ExecutionMode: goal.ExecutionMode,
		GoalKind:      goal.Kind,
	})
	return created, err
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
	report := o.goalTaskReportFromTask(ctx, goal, t)
	if err := store.SaveTaskReport(report); err != nil {
		o.log().Warn("goal task report save failed", "error", err, "goal", goalID, "task", t.ID)
	}
	applyGoalTaskReportToPlan(&goal, report, now)
	goal.ProgressSummary = truncateAssistantRunText("Latest linked task completed: "+friendlyTaskTitle(t)+". "+firstNonEmptyString(report.Summary, "Task completed."), 600)
	goal.LastActionAt = &now
	if report.GoalComplete {
		goal.Status = assistantstore.GoalStatusCompleted
		if goal.Autopilot != nil {
			goal.Autopilot.Status = assistantstore.GoalAutopilotStatusCompleted
			goal.Autopilot.StopReasons = appendGoalStopReason(goal.Autopilot.StopReasons, "Linked task reported the Goal complete.")
			goal.Autopilot.CurrentTaskID = ""
			goal.Autopilot.CurrentPhaseID = ""
			goal.Autopilot.LastStepAt = &now
		}
	}
	if len(report.Questions) > 0 {
		goal.OpenQuestions = appendUniqueStrings(goal.OpenQuestions, report.Questions...)
		if goal.ExecutionMode == assistantstore.GoalExecutionModeAutopilot && goal.Autopilot != nil && goal.Status != assistantstore.GoalStatusCompleted {
			goal.Status = assistantstore.GoalStatusBlocked
			goal.Autopilot.Status = assistantstore.GoalAutopilotStatusBlocked
			goal.Autopilot.StopReasons = appendGoalStopReason(goal.Autopilot.StopReasons, "Linked task asked Goal questions.")
			goal.Autopilot.LastStepAt = &now
		}
	}
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
	for _, recommendation := range extractGoalWatchRecommendations(t.Result) {
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

func (o *Orchestrator) goalTaskReportFromTask(ctx context.Context, goal assistantstore.Goal, t taskstore.Task) assistantstore.GoalTaskReport {
	now := time.Now().UTC()
	report := assistantstore.GoalTaskReport{
		ID:            "greport_" + safeGoalReportID(t.ID),
		GoalID:        strings.TrimSpace(t.GoalID),
		TaskID:        strings.TrimSpace(t.ID),
		PhaseID:       strings.TrimSpace(t.GoalPhaseID),
		Title:         friendlyTaskTitle(t),
		Status:        strings.TrimSpace(t.Status),
		Summary:       goalTaskResultSummary(t.Result),
		AdvancedGoal:  true,
		ResultExcerpt: truncateAssistantRunText(t.Result, 900),
		CreatedAt:     now,
	}
	if structured, ok := parseStructuredGoalReport(t.Result); ok {
		report.Summary = firstNonEmptyString(structured.Summary, report.Summary)
		report.AdvancedGoal = structured.AdvancedGoal
		report.PhaseComplete = structured.PhaseComplete
		report.GoalComplete = structured.GoalComplete
		report.NoChange = structured.NoChange
		report.ChangedFiles = structured.ChangedFiles
		report.Validation = structured.Validation
		report.FollowUps = structured.FollowUps
		report.Blockers = structured.Blockers
		report.Questions = structured.Questions
	}
	if diff, err := o.TaskDiff(ctx, t.ID); err == nil {
		report.DiffFiles = diff.Summary.Files
		report.Additions = diff.Summary.Additions
		report.Deletions = diff.Summary.Deletions
		if len(report.ChangedFiles) == 0 {
			for _, file := range diff.Files {
				report.ChangedFiles = append(report.ChangedFiles, file.Path)
				if len(report.ChangedFiles) >= 64 {
					break
				}
			}
		}
	}
	explicitNoChange := t.Status == taskstore.StatusNoChangeRequired || workerReportedNoChangeRequired(t.Result)
	hasDiffEvidence := report.DiffFiles > 0 || len(report.ChangedFiles) > 0
	if explicitNoChange && !hasDiffEvidence {
		report.NoChange = true
		report.AdvancedGoal = false
	}
	if hasDiffEvidence && report.NoChange {
		report.NoChange = false
	}
	if !hasDiffEvidence && !report.NoChange {
		report.AdvancedGoal = false
	}
	if len(report.Blockers) > 0 || len(report.Questions) > 0 {
		report.PhaseComplete = false
		report.GoalComplete = false
	}
	review := reviewGoalTaskProgress(goal, report)
	report.ReviewDecision = review.Decision
	report.ReviewSummary = review.Summary
	report.ReviewEvidence = review.Evidence
	switch review.Decision {
	case goalTaskReviewVerifiedProgress, goalTaskReviewBlockedWithProgress:
		report.AdvancedGoal = true
		report.NoChange = false
	case goalTaskReviewNeedsValidation, goalTaskReviewMisaligned, goalTaskReviewInsufficientEvidence:
		report.AdvancedGoal = false
		report.PhaseComplete = false
		report.GoalComplete = false
		report.NoChange = false
	case goalTaskReviewNoChange:
		report.AdvancedGoal = false
		report.NoChange = true
		report.PhaseComplete = false
		report.GoalComplete = false
	}
	return assistantstore.NormalizeGoalTaskReport(report)
}

type structuredGoalReport struct {
	Summary       string   `json:"summary"`
	AdvancedGoal  bool     `json:"advanced_goal"`
	PhaseComplete bool     `json:"phase_complete"`
	GoalComplete  bool     `json:"goal_complete"`
	NoChange      bool     `json:"no_change"`
	ChangedFiles  []string `json:"changed_files"`
	Validation    []string `json:"validation"`
	FollowUps     []string `json:"follow_ups"`
	Blockers      []string `json:"blockers"`
	Questions     []string `json:"questions"`
}

func parseStructuredGoalReport(result string) (structuredGoalReport, bool) {
	var latest structuredGoalReport
	found := false
	for _, line := range strings.Split(result, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(strings.ToUpper(trimmed), "GOAL_REPORT:") {
			continue
		}
		raw := strings.TrimSpace(trimmed[len("GOAL_REPORT:"):])
		var report structuredGoalReport
		if err := json.Unmarshal([]byte(raw), &report); err == nil {
			report.Summary = strings.TrimSpace(report.Summary)
			report.ChangedFiles = normalizeGoalReportStrings(report.ChangedFiles, 64)
			report.Validation = normalizeGoalReportStrings(report.Validation, 24)
			report.FollowUps = normalizeGoalReportStrings(report.FollowUps, 24)
			report.Blockers = normalizeGoalReportStrings(report.Blockers, 24)
			report.Questions = normalizeGoalReportStrings(report.Questions, 12)
			latest = report
			found = true
		}
	}
	return latest, found
}

func normalizeGoalReportStrings(values []string, limit int) []string {
	out := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		key := strings.ToLower(value)
		if value == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, value)
		if len(out) >= limit {
			break
		}
	}
	return out
}

func goalTaskResultSummary(result string) string {
	result = strings.TrimSpace(result)
	if result == "" {
		return "Task completed."
	}
	for _, line := range strings.Split(result, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(strings.ToUpper(line), "GOAL_REPORT:") {
			continue
		}
		return truncateAssistantRunText(line, 240)
	}
	return truncateAssistantRunText(result, 240)
}

func applyGoalTaskReportToPlan(goal *assistantstore.Goal, report assistantstore.GoalTaskReport, now time.Time) {
	if goal == nil || goal.Plan == nil || strings.TrimSpace(report.PhaseID) == "" {
		return
	}
	plan := assistantstore.NormalizeGoalPlan(*goal.Plan)
	for index := range plan.Phases {
		if plan.Phases[index].ID != report.PhaseID {
			continue
		}
		if !stringSliceContains(plan.Phases[index].TaskIDs, report.TaskID) {
			plan.Phases[index].TaskIDs = append(plan.Phases[index].TaskIDs, report.TaskID)
		}
		if report.PhaseComplete || report.GoalComplete {
			plan.Phases[index].Status = assistantstore.GoalPlanPhaseStatusCompleted
		} else if len(report.Blockers) > 0 || len(report.Questions) > 0 {
			plan.Phases[index].Status = assistantstore.GoalPlanPhaseStatusBlocked
		} else {
			plan.Phases[index].Status = assistantstore.GoalPlanPhaseStatusInProgress
		}
		if report.Summary != "" {
			plan.Phases[index].Evidence = appendUniqueStrings(plan.Phases[index].Evidence, report.Summary)
		}
		break
	}
	if report.GoalComplete {
		plan.Status = assistantstore.GoalPlanStatusCompleted
	} else if len(report.Blockers) > 0 || len(report.Questions) > 0 {
		plan.Status = assistantstore.GoalPlanStatusBlocked
	}
	if next, ok := selectGoalPlanPhase(&plan); ok {
		plan.CurrentPhaseID = next.ID
	} else if plan.Status != assistantstore.GoalPlanStatusBlocked {
		plan.Status = assistantstore.GoalPlanStatusCompleted
		plan.CurrentPhaseID = ""
	}
	plan.UpdatedAt = now
	goal.Plan = &plan
	if goal.Autopilot != nil {
		goal.Autopilot.CurrentPhaseID = plan.CurrentPhaseID
	}
}

const (
	goalTaskReviewVerifiedProgress     = "verified_progress"
	goalTaskReviewBlockedWithProgress  = "blocked_with_progress"
	goalTaskReviewNeedsValidation      = "needs_validation"
	goalTaskReviewMisaligned           = "misaligned"
	goalTaskReviewInsufficientEvidence = "insufficient_evidence"
	goalTaskReviewNoChange             = "no_change"
)

type goalTaskProgressReview struct {
	Decision string
	Summary  string
	Evidence []string
}

func reviewGoalTaskProgress(goal assistantstore.Goal, report assistantstore.GoalTaskReport) goalTaskProgressReview {
	goal = assistantstore.NormalizeGoal(goal)
	report = assistantstore.NormalizeGoalTaskReport(report)
	hasDiff := report.DiffFiles > 0 || len(report.ChangedFiles) > 0
	hasValidation := len(report.Validation) > 0
	if !hasDiff {
		if report.NoChange {
			return goalTaskProgressReview{
				Decision: goalTaskReviewNoChange,
				Summary:  "Worker reported no change and independent diff evidence found no changed files.",
				Evidence: nonEmptyStringSlice(firstNonEmptyString(report.Summary, "No changed files were reported.")),
			}
		}
		return goalTaskProgressReview{
			Decision: goalTaskReviewInsufficientEvidence,
			Summary:  "No changed files or captured diff were available, so Goal progress could not be independently verified.",
			Evidence: nonEmptyStringSlice(firstNonEmptyString(report.Summary, "No changed files were reported.")),
		}
	}

	phase, _ := goalPlanPhaseByID(goal.Plan, report.PhaseID)
	evidence := []string{fmt.Sprintf("Diff evidence: %d files, +%d/-%d.", report.DiffFiles, report.Additions, report.Deletions)}
	if len(report.ChangedFiles) > 0 {
		evidence = append(evidence, "Changed files: "+strings.Join(report.ChangedFiles[:assistantMinInt(len(report.ChangedFiles), 10)], ", ")+".")
	}
	if hasValidation {
		evidence = append(evidence, "Validation: "+strings.Join(report.Validation[:assistantMinInt(len(report.Validation), 4)], "; ")+".")
	}
	if report.Summary != "" {
		evidence = append(evidence, "Worker summary: "+report.Summary)
	}

	score := goalAlignmentScore(goal, phase, report)
	hasProductFile := goalReportHasProductFile(report)
	if len(report.Blockers) > 0 || len(report.Questions) > 0 {
		if score > 0 {
			return goalTaskProgressReview{
				Decision: goalTaskReviewBlockedWithProgress,
				Summary:  "The task changed files that appear relevant to the Goal, but reported blockers or questions that must be handled before completion.",
				Evidence: evidence,
			}
		}
		return goalTaskProgressReview{
			Decision: goalTaskReviewMisaligned,
			Summary:  "The task changed files and reported blockers or questions, but the changed files did not clearly align with the Goal.",
			Evidence: evidence,
		}
	}
	if score > 0 && hasValidation {
		return goalTaskProgressReview{
			Decision: goalTaskReviewVerifiedProgress,
			Summary:  "Changed files, validation evidence, and Goal/phase terms line up, so the task is counted as verified progress.",
			Evidence: evidence,
		}
	}
	if score > 0 && !hasValidation {
		return goalTaskProgressReview{
			Decision: goalTaskReviewNeedsValidation,
			Summary:  "Changed files appear relevant to the Goal, but no validation evidence was reported; Autopilot should verify or rerun before treating it as progress.",
			Evidence: evidence,
		}
	}
	if hasProductFile && !hasValidation {
		evidence = append(evidence, "Changed product files did not share clear Goal or phase terms.")
	}
	return goalTaskProgressReview{
		Decision: goalTaskReviewMisaligned,
		Summary:  "The task produced a diff, but independent file and validation evidence did not clearly align it to the Goal.",
		Evidence: evidence,
	}
}

func goalReportHasProductFile(report assistantstore.GoalTaskReport) bool {
	for _, path := range report.ChangedFiles {
		path = strings.ToLower(filepath.ToSlash(strings.TrimSpace(path)))
		switch {
		case strings.HasPrefix(path, "src/"),
			strings.HasPrefix(path, "lib/"),
			strings.HasPrefix(path, "cmd/"),
			strings.HasPrefix(path, "pkg/"),
			strings.HasPrefix(path, "internal/"),
			strings.HasPrefix(path, "web/"),
			strings.HasPrefix(path, "tests/"),
			strings.HasPrefix(path, "test/"),
			strings.HasPrefix(path, "e2e/"),
			strings.HasPrefix(path, "examples/"),
			strings.HasPrefix(path, "docs/"),
			strings.HasSuffix(path, ".go"),
			strings.HasSuffix(path, ".js"),
			strings.HasSuffix(path, ".ts"),
			strings.HasSuffix(path, ".tsx"),
			strings.HasSuffix(path, ".css"),
			strings.HasSuffix(path, ".html"),
			strings.HasSuffix(path, ".md"):
			return true
		}
	}
	return false
}

func goalAlignmentScore(goal assistantstore.Goal, phase assistantstore.GoalPlanPhase, report assistantstore.GoalTaskReport) int {
	goalText := strings.Join([]string{
		goal.Title,
		goal.Objective,
		goal.Details,
		strings.Join(goal.SuccessCriteria, " "),
		strings.Join(goal.Constraints, " "),
		phase.Title,
		phase.Objective,
		strings.Join(phase.AcceptanceCriteria, " "),
	}, " ")
	goalTerms := significantGoalTerms(goalText)
	if len(goalTerms) == 0 {
		return 0
	}
	evidenceText := strings.Join([]string{
		strings.Join(report.ChangedFiles, " "),
		strings.Join(report.Validation, " "),
	}, " ")
	evidenceTerms := significantGoalTerms(evidenceText)
	score := 0
	for term := range evidenceTerms {
		if goalTerms[term] {
			score++
		}
	}
	return score
}

func significantGoalTerms(text string) map[string]bool {
	out := map[string]bool{}
	var b strings.Builder
	flush := func() {
		token := strings.TrimSpace(b.String())
		b.Reset()
		if len(token) < 3 || goalReviewStopTerm(token) {
			return
		}
		out[token] = true
	}
	for _, r := range strings.ToLower(text) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			continue
		}
		flush()
	}
	flush()
	return out
}

func goalReviewStopTerm(token string) bool {
	switch token {
	case "the", "and", "for", "with", "that", "this", "from", "into", "task", "goal", "phase", "build", "add", "make", "work", "done", "file", "files", "test", "tests", "docs", "src", "code", "change", "changed", "validation", "report", "summary", "objective", "details", "supporting", "support", "capability", "capabilities":
		return true
	default:
		return false
	}
}

func safeGoalReportID(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return id.New("task")
	}
	value = strings.ReplaceAll(value, string(filepath.Separator), "_")
	value = strings.ReplaceAll(value, "..", "_")
	return value
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
	GoalPhaseID   string
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
