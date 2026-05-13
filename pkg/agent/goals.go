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

const (
	goalFinalAuditPhaseID     = "phase_final_audit"
	goalFinalAuditMilestoneID = goalFinalAuditPhaseID + "_milestone_01"
	goalFinalAuditMarker      = "Final whole-goal audit"
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
		goal, err = o.attachGoalBlockerTrace(store, goal)
		if err != nil {
			return nil, err
		}
		goals[index] = compactGoalForList(goal)
	}
	return goals, nil
}

func compactGoalForList(goal assistantstore.Goal) assistantstore.Goal {
	goal.Plan = nil
	return goal
}

type GoalTimelineOptions struct {
	Limit int
}

func (o *Orchestrator) LoadGoal(goalID string) (assistantstore.GoalTimeline, error) {
	return o.LoadGoalWithOptions(goalID, GoalTimelineOptions{})
}

func (o *Orchestrator) LoadGoalWithOptions(goalID string, opts GoalTimelineOptions) (assistantstore.GoalTimeline, error) {
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
	trace := assistantstore.DeriveGoalBlockerTrace(goal, decisions, reports)
	goal.BlockerTrace = trace
	timeline := assistantstore.GoalTimeline{
		Goal:         goal,
		BlockerTrace: trace,
		Watches:      watches,
		Signals:      signals,
		Notes:        notes,
		Assessments:  assessments,
		Decisions:    decisions,
		TaskReports:  reports,
	}
	trimGoalTimeline(&timeline, opts.Limit)
	return timeline, nil
}

func trimGoalTimeline(timeline *assistantstore.GoalTimeline, limit int) {
	if timeline == nil {
		return
	}
	if limit < 0 {
		limit = 0
	}
	if limit > 100 {
		limit = 100
	}
	if limit == 0 {
		return
	}
	timeline.Counts = &assistantstore.GoalTimelineCounts{
		Watches:     len(timeline.Watches),
		Signals:     len(timeline.Signals),
		Notes:       len(timeline.Notes),
		Assessments: len(timeline.Assessments),
		Decisions:   len(timeline.Decisions),
		TaskReports: len(timeline.TaskReports),
	}
	if len(timeline.Watches) > limit {
		timeline.Watches = timeline.Watches[:limit]
	}
	if len(timeline.Signals) > limit {
		timeline.Signals = timeline.Signals[:limit]
	}
	if len(timeline.Notes) > limit {
		timeline.Notes = timeline.Notes[:limit]
	}
	if len(timeline.Assessments) > limit {
		timeline.Assessments = timeline.Assessments[:limit]
	}
	if len(timeline.Decisions) > limit {
		timeline.Decisions = timeline.Decisions[:limit]
	}
	if len(timeline.TaskReports) > limit {
		timeline.TaskReports = timeline.TaskReports[:limit]
	}
}

func (o *Orchestrator) attachGoalBlockerTrace(store *assistantstore.GoalStore, goal assistantstore.Goal) (assistantstore.Goal, error) {
	decisions, err := store.ListDecisions(goal.ID)
	if err != nil {
		return assistantstore.Goal{}, err
	}
	reports, err := store.ListTaskReports(goal.ID)
	if err != nil {
		return assistantstore.Goal{}, err
	}
	goal.BlockerTrace = assistantstore.DeriveGoalBlockerTrace(goal, decisions, reports)
	return goal, nil
}

func (o *Orchestrator) GoalBlockerTraceForTask(task taskstore.Task) (*assistantstore.GoalBlockerTrace, error) {
	if strings.TrimSpace(task.GoalID) == "" {
		return nil, nil
	}
	store, err := o.assistantGoalStore()
	if err != nil {
		return nil, err
	}
	goal, err := store.LoadGoal(task.GoalID)
	if err != nil {
		return nil, err
	}
	goal, err = o.ensureGoalPlan(store, goal)
	if err != nil {
		return nil, err
	}
	goal, err = o.attachGoalBlockerTrace(store, goal)
	if err != nil {
		return nil, err
	}
	return goalBlockerTraceForTask(task, goal.BlockerTrace), nil
}

func goalBlockerTraceForTask(task taskstore.Task, trace *assistantstore.GoalBlockerTrace) *assistantstore.GoalBlockerTrace {
	trace = assistantstore.NormalizeGoalBlockerTrace(trace)
	if trace == nil {
		return nil
	}
	if trace.BlockingTaskID != task.ID {
		return trace
	}
	if trace.CreatedAt == nil || task.UpdatedAt.IsZero() || !task.UpdatedAt.After(trace.CreatedAt.UTC()) {
		return trace
	}
	if goalBlockingTaskStatusStillCurrent(task.Status) {
		return trace
	}
	return nil
}

func goalBlockingTaskStatusStillCurrent(status string) bool {
	switch status {
	case taskstore.StatusBlocked, taskstore.StatusTimedOut, taskstore.StatusFailed, taskstore.StatusConflictResolution:
		return true
	default:
		return false
	}
}

func (o *Orchestrator) ensureGoalPlan(store *assistantstore.GoalStore, goal assistantstore.Goal) (assistantstore.Goal, error) {
	goal = assistantstore.NormalizeGoal(goal)
	now := time.Now().UTC()
	changed := false
	if goal.Plan == nil {
		plan := buildGoalPlan(goal, now)
		goal.Plan = &plan
		changed = true
	} else {
		plan := assistantstore.NormalizeGoalPlan(*goal.Plan)
		if ensureGoalPlanMilestones(&plan, goal, now) {
			changed = true
		}
		if plan.CurrentPhaseID == "" {
			if phase, ok := selectGoalPlanPhase(&plan); ok {
				plan.CurrentPhaseID = phase.ID
				changed = true
			}
		}
		goal.Plan = &plan
	}
	if changed {
		goal.UpdatedAt = now
		if err := store.SaveGoal(goal); err != nil {
			return assistantstore.Goal{}, err
		}
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
		for milestoneIndex := range plan.Phases[index].Milestones {
			if plan.Phases[index].Milestones[milestoneIndex].Status != assistantstore.GoalMilestoneStatusBlocked {
				continue
			}
			if len(plan.Phases[index].Milestones[milestoneIndex].TaskIDs) > 0 {
				plan.Phases[index].Milestones[milestoneIndex].Status = assistantstore.GoalMilestoneStatusInProgress
			} else {
				plan.Phases[index].Milestones[milestoneIndex].Status = assistantstore.GoalMilestoneStatusPending
			}
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
		if taskBlockedByRemoteGoalReview(t) {
			return o.recheckBlockedRemoteGoalTask(ctx, store, goal, t)
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

func (o *Orchestrator) recheckBlockedRemoteGoalTask(ctx context.Context, store *assistantstore.GoalStore, goal assistantstore.Goal, t taskstore.Task) (bool, string, error) {
	shortID := taskShortID(t.ID)
	if !goalAutopilotAllows(goal, "review_task") {
		return o.blockOrStopGoalAutopilot(ctx, store, goal, assistantstore.GoalAutopilotStatusBlocked, "Autopilot policy does not allow reviewing blocked remote Goal tasks.")
	}
	reply, err := o.reviewRemoteTask(ctx, t)
	if err != nil {
		return false, "", err
	}
	refreshed, err := o.tasks.Load(t.ID)
	if err != nil {
		return false, "", err
	}
	if refreshed.Status == taskstore.StatusBlocked {
		reason := fmt.Sprintf("Linked remote task %s is still blocked after automatic validation review.", shortID)
		return o.blockOrStopGoalAutopilot(ctx, store, goal, assistantstore.GoalAutopilotStatusBlocked, reason)
	}
	if refreshed.Status == taskstore.StatusAwaitingVerification || refreshed.Status == taskstore.StatusNoChangeRequired {
		if !goalAutopilotAllows(goal, "accept_task") {
			return o.blockOrStopGoalAutopilot(ctx, store, goal, assistantstore.GoalAutopilotStatusBlocked, "Autopilot policy does not allow accepting recovered remote Goal tasks.")
		}
		acceptReply, err := o.acceptTaskWithActor(ctx, refreshed.ID, "Assistant Autopilot")
		if err != nil {
			return false, "", err
		}
		if err := o.clearGoalAutopilotCurrentTask(store, goal.ID, refreshed.ID, "Autopilot accepted linked task "+shortID+" after automatic remote Goal re-review and will check the next step."); err != nil {
			return false, "", err
		}
		return true, fmt.Sprintf("Autopilot rechecked blocked remote Goal task %s.\n%s\n%s\nAutopilot accepted the recovered task and will select the next Goal step on the next supervisor pass.", shortID, reply, acceptReply), nil
	}
	if refreshed.Status == taskstore.StatusDone {
		if err := o.clearGoalAutopilotCurrentTask(store, goal.ID, refreshed.ID, "Autopilot found linked task "+shortID+" already accepted after automatic remote Goal re-review and will check the next step."); err != nil {
			return false, "", err
		}
		return true, fmt.Sprintf("Autopilot rechecked blocked remote Goal task %s.\n%s\nAutopilot found the task already accepted and will select the next Goal step on the next supervisor pass.", shortID, reply), nil
	}
	return true, fmt.Sprintf("Autopilot rechecked blocked remote Goal task %s.\n%s", shortID, reply), nil
}

func (o *Orchestrator) clearGoalAutopilotCurrentTask(store *assistantstore.GoalStore, goalID, taskID, noteBody string) error {
	goal, err := store.LoadGoal(goalID)
	if err != nil {
		return err
	}
	goal = assistantstore.NormalizeGoal(goal)
	if goal.Autopilot == nil || strings.TrimSpace(goal.Autopilot.CurrentTaskID) != strings.TrimSpace(taskID) {
		return nil
	}
	now := time.Now().UTC()
	goal.Autopilot.CurrentTaskID = ""
	goal.Autopilot.LastStepAt = &now
	goal.UpdatedAt = now
	if err := store.SaveGoal(goal); err != nil {
		return err
	}
	_ = store.SaveNote(assistantstore.GoalNote{
		ID:        id.New("gnote"),
		GoalID:    goal.ID,
		Kind:      "autopilot",
		Title:     "Autopilot task accepted",
		Body:      strings.TrimSpace(noteBody),
		TaskID:    strings.TrimSpace(taskID),
		CreatedBy: "assistant",
		CreatedAt: now,
	})
	return nil
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
	o.goalAutopilotMu.Lock()
	defer o.goalAutopilotMu.Unlock()

	freshGoal, err := store.LoadGoal(goal.ID)
	if err != nil {
		return false, "", err
	}
	freshGoal = assistantstore.NormalizeGoal(freshGoal)
	if current, ok := o.currentGoalAutopilotTask(freshGoal); ok && !taskTerminal(current.Status) {
		return false, fmt.Sprintf("Autopilot is waiting for task %s (%s).", taskShortID(current.ID), firstNonEmptyString(current.Status, "unknown")), nil
	}
	goal = freshGoal
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
	markGoalPlanTaskCreated(&goal, decision, created.Task.ID, now)
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
		Body:      fmt.Sprintf("Created %s %s task %s for this %s Goal phase `%s` milestone `%s` (%s task limit).", goal.ExecutionMode, firstNonEmptyString(decision.TaskType, assistantstore.GoalTaskTypeBuild), taskShortID(created.Task.ID), goal.Kind, firstNonEmptyString(decision.PhaseID, "unplanned"), firstNonEmptyString(decision.MilestoneID, "unplanned"), goalAutopilotProgressLabel(goal.Autopilot.TasksStarted, goal.Autopilot.BudgetTasks)),
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

func markGoalPlanTaskCreated(goal *assistantstore.Goal, decision assistantstore.GoalSupervisorDecision, taskID string, now time.Time) {
	if goal == nil || goal.Plan == nil {
		return
	}
	phaseID := strings.TrimSpace(decision.PhaseID)
	milestoneID := strings.TrimSpace(decision.MilestoneID)
	gapID := strings.TrimSpace(decision.GapID)
	taskType := strings.TrimSpace(decision.TaskType)
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
		for milestoneIndex := range plan.Phases[index].Milestones {
			if plan.Phases[index].Milestones[milestoneIndex].ID != milestoneID {
				continue
			}
			if taskType == assistantstore.GoalTaskTypeChallenge {
				if !stringSliceContains(plan.Phases[index].Milestones[milestoneIndex].ChallengeTaskIDs, taskID) {
					plan.Phases[index].Milestones[milestoneIndex].ChallengeTaskIDs = append(plan.Phases[index].Milestones[milestoneIndex].ChallengeTaskIDs, taskID)
				}
				if plan.Phases[index].Milestones[milestoneIndex].Status == assistantstore.GoalMilestoneStatusClaimed {
					plan.Phases[index].Milestones[milestoneIndex].Status = assistantstore.GoalMilestoneStatusChallenged
				}
			} else {
				if !stringSliceContains(plan.Phases[index].Milestones[milestoneIndex].TaskIDs, taskID) {
					plan.Phases[index].Milestones[milestoneIndex].TaskIDs = append(plan.Phases[index].Milestones[milestoneIndex].TaskIDs, taskID)
				}
				if plan.Phases[index].Milestones[milestoneIndex].Status == assistantstore.GoalMilestoneStatusPending {
					plan.Phases[index].Milestones[milestoneIndex].Status = assistantstore.GoalMilestoneStatusInProgress
				}
			}
			break
		}
		break
	}
	for gapIndex := range plan.Gaps {
		if plan.Gaps[gapIndex].ID != gapID {
			continue
		}
		plan.Gaps[gapIndex].Status = assistantstore.GoalGapStatusInProgress
		plan.Gaps[gapIndex].TaskIDs = appendUniqueStrings(plan.Gaps[gapIndex].TaskIDs, taskID)
		plan.Gaps[gapIndex].UpdatedAt = now
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
	if goal.Plan != nil {
		plan := assistantstore.NormalizeGoalPlan(*goal.Plan)
		if ensureGoalPlanMilestones(&plan, goal, now) {
			goal.Plan = &plan
			goal.UpdatedAt = now
			if err := store.SaveGoal(goal); err != nil {
				return assistantstore.GoalSupervisorDecision{}, err
			}
		}
	}
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
	if gap, ok := selectOpenGoalGap(goal.Plan); ok {
		if gap.PhaseID == "" && gap.MilestoneID != "" {
			if phase, ok := goalPlanPhaseForMilestone(goal.Plan, gap.MilestoneID); ok {
				gap.PhaseID = phase.ID
			}
		}
		decision := assistantstore.GoalSupervisorDecision{
			ID:          id.New("gdec"),
			GoalID:      goal.ID,
			Decision:    assistantstore.GoalSupervisorDecisionCreateTask,
			Summary:     "Create a gap-fix task for `" + gap.ID + "`.",
			Rationale:   "Open challenge gaps take priority over new feature work so Autopilot repairs known weaknesses before broadening the implementation.",
			PhaseID:     gap.PhaseID,
			MilestoneID: gap.MilestoneID,
			GapID:       gap.ID,
			TaskType:    assistantstore.GoalTaskTypeGapFix,
			Evidence:    goalSupervisorEvidence(goal, reports),
			CreatedAt:   now,
		}
		decision.TaskGoal = goalAutopilotTaskGoalForDecision(goal, decision, reports)
		return assistantstore.NormalizeGoalSupervisorDecision(decision), nil
	}
	if phase, milestone, ok := selectMilestoneNeedingChallenge(goal.Plan); ok {
		decision := assistantstore.GoalSupervisorDecision{
			ID:          id.New("gdec"),
			GoalID:      goal.ID,
			Decision:    assistantstore.GoalSupervisorDecisionCreateTask,
			Summary:     "Challenge milestone `" + milestone.ID + "` before accepting it.",
			Rationale:   "Every claimed milestone must survive read-only scrutiny before the supervisor can move to the next milestone.",
			PhaseID:     phase.ID,
			MilestoneID: milestone.ID,
			TaskType:    assistantstore.GoalTaskTypeChallenge,
			Evidence:    goalSupervisorEvidence(goal, reports),
			CreatedAt:   now,
		}
		decision.TaskGoal = goalAutopilotTaskGoalForDecision(goal, decision, reports)
		return assistantstore.NormalizeGoalSupervisorDecision(decision), nil
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
	if goalPlanComplete(goal.Plan) && allGoalGapsClosed(goal.Plan) {
		if recentGoalReportCompletes(reports) {
			decision := assistantstore.GoalSupervisorDecision{
				Decision:   assistantstore.GoalSupervisorDecisionMarkComplete,
				Summary:    "Final audit certified the Goal.",
				Rationale:  "The plan is exhausted, all gaps are closed, and the latest challenge explicitly set goal_complete true.",
				StopReason: "Goal success criteria are satisfied by final whole-goal audit.",
				Evidence:   goalSupervisorEvidence(goal, reports),
			}
			decision = o.saveGoalSupervisorDecision(ctx, store, &goal, decision)
			return decision, nil
		}
		if report, ok := latestFinalAuditChallengeReport(reports); ok && !goalReportsAdvancedSinceFinalAudit(reports, report.TaskID) {
			decision := assistantstore.GoalSupervisorDecision{
				Decision:   assistantstore.GoalSupervisorDecisionPauseBlocked,
				Summary:    "Final audit did not certify the Goal.",
				Rationale:  "The plan is exhausted, but the latest final audit did not set goal_complete true. The supervisor will not mark the Goal done without a whole-goal certification.",
				StopReason: "Final audit required more work or operator direction before completion.",
				Evidence:   goalSupervisorEvidence(goal, reports),
			}
			if report.Summary != "" {
				decision.Evidence = appendUniqueStrings(decision.Evidence, "final audit: "+report.Summary)
			}
			decision = o.saveGoalSupervisorDecision(ctx, store, &goal, decision)
			return decision, nil
		}
		plan := assistantstore.NormalizeGoalPlan(*goal.Plan)
		if ensureGoalFinalAuditMilestone(&plan, goal, now) {
			goal.Plan = &plan
			goal.UpdatedAt = now
			if goal.Autopilot != nil {
				goal.Autopilot.CurrentPhaseID = goalFinalAuditPhaseID
			}
			if err := store.SaveGoal(goal); err != nil {
				return assistantstore.GoalSupervisorDecision{}, err
			}
		}
		phase, milestone, ok := finalAuditPlanMilestone(&plan)
		if !ok {
			decision := assistantstore.GoalSupervisorDecision{
				Decision:   assistantstore.GoalSupervisorDecisionPauseBlocked,
				Summary:    "Final audit could not be scheduled.",
				Rationale:  "The plan is exhausted, but the supervisor could not create or find the final whole-goal audit milestone.",
				StopReason: "No actionable final audit milestone is ready.",
				Evidence:   goalSupervisorEvidence(goal, reports),
			}
			decision = o.saveGoalSupervisorDecision(ctx, store, &goal, decision)
			return decision, nil
		}
		decision := assistantstore.GoalSupervisorDecision{
			ID:          id.New("gdec"),
			GoalID:      goal.ID,
			Decision:    assistantstore.GoalSupervisorDecisionCreateTask,
			Summary:     "Run final whole-goal audit before completion.",
			Rationale:   "All planned milestones are accepted and no gaps remain, but completion requires an explicit challenge with goal_complete true.",
			PhaseID:     phase.ID,
			MilestoneID: milestone.ID,
			TaskType:    assistantstore.GoalTaskTypeChallenge,
			Evidence:    goalSupervisorEvidence(goal, reports),
			CreatedAt:   now,
		}
		decision.TaskGoal = goalAutopilotTaskGoalForDecision(goal, decision, reports)
		return assistantstore.NormalizeGoalSupervisorDecision(decision), nil
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
	phase, milestone, ok := selectGoalPlanMilestone(goal.Plan)
	if !ok {
		decision := assistantstore.GoalSupervisorDecision{
			Decision:   assistantstore.GoalSupervisorDecisionPauseBlocked,
			Summary:    "No dependency-ready Goal milestone needs build work.",
			Rationale:  "The plan is not complete, but the supervisor could not find a pending milestone, claimed milestone, or open gap to advance.",
			StopReason: "No actionable Goal milestone is ready.",
			Evidence:   goalSupervisorEvidence(goal, reports),
		}
		decision = o.saveGoalSupervisorDecision(ctx, store, &goal, decision)
		return decision, nil
	}
	taskGoal := goalAutopilotTaskGoalForDecision(goal, assistantstore.GoalSupervisorDecision{PhaseID: phase.ID, MilestoneID: milestone.ID, TaskType: assistantstore.GoalTaskTypeBuild}, reports)
	decision := assistantstore.GoalSupervisorDecision{
		ID:          id.New("gdec"),
		GoalID:      goal.ID,
		Decision:    assistantstore.GoalSupervisorDecisionCreateTask,
		Summary:     "Create the next task for milestone `" + milestone.ID + "`.",
		Rationale:   "The selected milestone is the next incomplete, dependency-ready slice in the Goal plan.",
		PhaseID:     phase.ID,
		MilestoneID: milestone.ID,
		TaskType:    assistantstore.GoalTaskTypeBuild,
		TaskGoal:    taskGoal,
		Evidence:    goalSupervisorEvidence(goal, reports),
		CreatedAt:   now,
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
			for milestoneIndex := range plan.Phases[index].Milestones {
				plan.Phases[index].Milestones[milestoneIndex].Status = assistantstore.GoalMilestoneStatusAccepted
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
		if gap, ok := selectOpenGoalGap(goal.Plan); ok {
			evidence = append(evidence, "open gap: "+gap.ID+" - "+firstNonEmptyString(gap.Area, gap.Claim))
		}
		if phase, milestone, ok := selectGoalPlanMilestone(goal.Plan); ok {
			evidence = append(evidence, "next milestone: "+phase.ID+"/"+milestone.ID+" - "+milestone.Title)
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
	return goalTaskReportCompletesGoal(reports[0])
}

func goalTaskReportCompletesGoal(report assistantstore.GoalTaskReport) bool {
	report = assistantstore.NormalizeGoalTaskReport(report)
	if !report.GoalComplete || report.TaskType != assistantstore.GoalTaskTypeChallenge || report.Challenge == nil {
		return false
	}
	return report.Challenge.Verdict == assistantstore.GoalChallengeVerdictPassed && (isFinalAuditMilestoneID(report.MilestoneID) || isFinalAuditMilestoneID(report.Challenge.MilestoneID))
}

func latestFinalAuditChallengeReport(reports []assistantstore.GoalTaskReport) (assistantstore.GoalTaskReport, bool) {
	for _, report := range reports {
		report = assistantstore.NormalizeGoalTaskReport(report)
		if report.TaskType != assistantstore.GoalTaskTypeChallenge || report.Challenge == nil {
			continue
		}
		if isFinalAuditMilestoneID(report.MilestoneID) || isFinalAuditMilestoneID(report.Challenge.MilestoneID) {
			return report, true
		}
	}
	return assistantstore.GoalTaskReport{}, false
}

func goalReportsAdvancedSinceFinalAudit(reports []assistantstore.GoalTaskReport, finalAuditTaskID string) bool {
	finalAuditTaskID = strings.TrimSpace(finalAuditTaskID)
	for _, report := range reports {
		report = assistantstore.NormalizeGoalTaskReport(report)
		if finalAuditTaskID != "" && report.TaskID == finalAuditTaskID {
			return false
		}
		if report.TaskType == assistantstore.GoalTaskTypeGapFix && report.AdvancedGoal {
			return true
		}
		if report.TaskType == assistantstore.GoalTaskTypeBuild && (report.AdvancedGoal || report.PhaseComplete || len(report.ChangedFiles) > 0 || report.DiffFiles > 0) {
			return true
		}
	}
	return false
}

func isFinalAuditMilestoneID(milestoneID string) bool {
	return strings.EqualFold(strings.TrimSpace(milestoneID), goalFinalAuditMilestoneID)
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
		if phase.Status == assistantstore.GoalPlanPhaseStatusSkipped {
			continue
		}
		if len(phase.Milestones) > 0 {
			if !goalPhaseMilestonesAccepted(phase) {
				return false
			}
			continue
		}
		if phase.Status != assistantstore.GoalPlanPhaseStatusCompleted {
			return false
		}
	}
	return true
}

func allGoalGapsClosed(plan *assistantstore.GoalPlan) bool {
	if plan == nil {
		return true
	}
	planValue := assistantstore.NormalizeGoalPlan(*plan)
	for _, gap := range planValue.Gaps {
		switch gap.Status {
		case assistantstore.GoalGapStatusFixed, assistantstore.GoalGapStatusAcceptedRisk, assistantstore.GoalGapStatusDisproven:
			continue
		default:
			return false
		}
	}
	return true
}

func goalPhaseMilestonesAccepted(phase assistantstore.GoalPlanPhase) bool {
	if len(phase.Milestones) == 0 {
		return phase.Status == assistantstore.GoalPlanPhaseStatusCompleted || phase.Status == assistantstore.GoalPlanPhaseStatusSkipped
	}
	for _, milestone := range phase.Milestones {
		if milestone.Status != assistantstore.GoalMilestoneStatusAccepted {
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

func selectGoalPlanMilestone(plan *assistantstore.GoalPlan) (assistantstore.GoalPlanPhase, assistantstore.GoalMilestone, bool) {
	if plan == nil {
		return assistantstore.GoalPlanPhase{}, assistantstore.GoalMilestone{}, false
	}
	planValue := assistantstore.NormalizeGoalPlan(*plan)
	completed := map[string]bool{}
	for _, phase := range planValue.Phases {
		if phase.Status == assistantstore.GoalPlanPhaseStatusSkipped || goalPhaseMilestonesAccepted(phase) {
			completed[phase.ID] = true
		}
	}
	for _, phase := range planValue.Phases {
		if phase.Status == assistantstore.GoalPlanPhaseStatusSkipped || goalPhaseMilestonesAccepted(phase) || phase.Status == assistantstore.GoalPlanPhaseStatusBlocked {
			continue
		}
		ready := true
		for _, dep := range phase.DependsOn {
			if !completed[dep] {
				ready = false
				break
			}
		}
		if !ready {
			continue
		}
		if len(phase.Milestones) == 0 {
			return phase, assistantstore.GoalMilestone{ID: phase.ID + "_milestone", PhaseID: phase.ID, Title: phase.Title, Objective: phase.Objective}, true
		}
		for _, milestone := range phase.Milestones {
			switch milestone.Status {
			case assistantstore.GoalMilestoneStatusAccepted, assistantstore.GoalMilestoneStatusClaimed, assistantstore.GoalMilestoneStatusChallenged, assistantstore.GoalMilestoneStatusBlocked:
				continue
			default:
				return phase, milestone, true
			}
		}
	}
	return assistantstore.GoalPlanPhase{}, assistantstore.GoalMilestone{}, false
}

func selectMilestoneNeedingChallenge(plan *assistantstore.GoalPlan) (assistantstore.GoalPlanPhase, assistantstore.GoalMilestone, bool) {
	if plan == nil {
		return assistantstore.GoalPlanPhase{}, assistantstore.GoalMilestone{}, false
	}
	planValue := assistantstore.NormalizeGoalPlan(*plan)
	for _, phase := range planValue.Phases {
		if phase.Status == assistantstore.GoalPlanPhaseStatusSkipped || phase.Status == assistantstore.GoalPlanPhaseStatusBlocked {
			continue
		}
		for _, milestone := range phase.Milestones {
			if milestone.Status == assistantstore.GoalMilestoneStatusClaimed {
				return phase, milestone, true
			}
		}
	}
	return assistantstore.GoalPlanPhase{}, assistantstore.GoalMilestone{}, false
}

func selectOpenGoalGap(plan *assistantstore.GoalPlan) (assistantstore.GoalGap, bool) {
	if plan == nil {
		return assistantstore.GoalGap{}, false
	}
	planValue := assistantstore.NormalizeGoalPlan(*plan)
	for _, severity := range []string{assistantstore.GoalGapSeverityCritical, assistantstore.GoalGapSeverityHigh, assistantstore.GoalGapSeverityMedium, assistantstore.GoalGapSeverityLow} {
		for _, gap := range planValue.Gaps {
			if gap.Status == assistantstore.GoalGapStatusOpen && gap.Severity == severity {
				return gap, true
			}
		}
	}
	return assistantstore.GoalGap{}, false
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
	plan := assistantstore.NormalizeGoalPlan(assistantstore.GoalPlan{
		Status:         assistantstore.GoalPlanStatusActive,
		Summary:        "Supervisor plan for " + goal.Title,
		CurrentPhaseID: current,
		Phases:         phases,
		CreatedAt:      now,
		UpdatedAt:      now,
	})
	ensureGoalPlanMilestones(&plan, goal, now)
	return assistantstore.NormalizeGoalPlan(plan)
}

func ensureGoalPlanMilestones(plan *assistantstore.GoalPlan, goal assistantstore.Goal, now time.Time) bool {
	if plan == nil {
		return false
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	changed := false
	for phaseIndex := range plan.Phases {
		phase := &plan.Phases[phaseIndex]
		if len(phase.Milestones) == 0 {
			phase.Milestones = defaultGoalMilestones(goal, *phase)
			changed = true
		}
		for milestoneIndex := range phase.Milestones {
			milestone := &phase.Milestones[milestoneIndex]
			if strings.TrimSpace(milestone.PhaseID) == "" {
				milestone.PhaseID = phase.ID
				changed = true
			}
			if phase.Status == assistantstore.GoalPlanPhaseStatusCompleted && milestone.Status != assistantstore.GoalMilestoneStatusAccepted {
				milestone.Status = assistantstore.GoalMilestoneStatusAccepted
				changed = true
			}
			if phase.Status == assistantstore.GoalPlanPhaseStatusBlocked && milestone.Status == assistantstore.GoalMilestoneStatusPending && milestoneIndex == 0 {
				milestone.Status = assistantstore.GoalMilestoneStatusBlocked
				changed = true
			}
			if phase.Status == assistantstore.GoalPlanPhaseStatusInProgress && milestone.Status == assistantstore.GoalMilestoneStatusPending && !phaseHasActiveMilestone(*phase) && milestoneIndex == 0 {
				milestone.Status = assistantstore.GoalMilestoneStatusInProgress
				changed = true
			}
		}
	}
	if changed {
		plan.UpdatedAt = now
	}
	return changed
}

func ensureGoalFinalAuditMilestone(plan *assistantstore.GoalPlan, goal assistantstore.Goal, now time.Time) bool {
	if plan == nil {
		return false
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	changed := false
	lastDependency := ""
	finalPhaseIndex := -1
	for index := range plan.Phases {
		phaseID := strings.TrimSpace(plan.Phases[index].ID)
		if phaseID == goalFinalAuditPhaseID {
			finalPhaseIndex = index
			continue
		}
		if phaseID != "" && plan.Phases[index].Status != assistantstore.GoalPlanPhaseStatusSkipped {
			lastDependency = phaseID
		}
	}
	if finalPhaseIndex < 0 {
		phase := assistantstore.GoalPlanPhase{
			ID:        goalFinalAuditPhaseID,
			Title:     goalFinalAuditMarker,
			Objective: "Challenge the whole Goal against every success criterion, constraint, durable evidence item, and delivery state before completion.",
			Status:    assistantstore.GoalPlanPhaseStatusInProgress,
			DependsOn: nonEmptyStringSlice(lastDependency),
			AcceptanceCriteria: []string{
				"Every Goal success criterion is directly evaluated, not only the last milestone.",
				"Repository delivery state is checked and either clean, committed, or recorded as an explicit gap.",
				"The challenge report sets goal_complete true only when the whole Goal is credibly complete.",
			},
			Milestones: []assistantstore.GoalMilestone{finalAuditMilestone()},
		}
		plan.Phases = append(plan.Phases, phase)
		finalPhaseIndex = len(plan.Phases) - 1
		changed = true
	}
	phase := &plan.Phases[finalPhaseIndex]
	if phase.Status != assistantstore.GoalPlanPhaseStatusInProgress {
		phase.Status = assistantstore.GoalPlanPhaseStatusInProgress
		changed = true
	}
	if strings.TrimSpace(phase.Title) == "" {
		phase.Title = goalFinalAuditMarker
		changed = true
	}
	if strings.TrimSpace(phase.Objective) == "" {
		phase.Objective = "Challenge the whole Goal against every success criterion, constraint, durable evidence item, and delivery state before completion."
		changed = true
	}
	milestoneIndex := -1
	for index := range phase.Milestones {
		if isFinalAuditMilestoneID(phase.Milestones[index].ID) {
			milestoneIndex = index
			break
		}
	}
	if milestoneIndex < 0 {
		phase.Milestones = append(phase.Milestones, finalAuditMilestone())
		milestoneIndex = len(phase.Milestones) - 1
		changed = true
	}
	milestone := &phase.Milestones[milestoneIndex]
	if milestone.PhaseID != goalFinalAuditPhaseID {
		milestone.PhaseID = goalFinalAuditPhaseID
		changed = true
	}
	if milestone.Status != assistantstore.GoalMilestoneStatusClaimed && milestone.Status != assistantstore.GoalMilestoneStatusChallenged {
		milestone.Status = assistantstore.GoalMilestoneStatusClaimed
		changed = true
	}
	plan.Status = assistantstore.GoalPlanStatusActive
	plan.CurrentPhaseID = goalFinalAuditPhaseID
	plan.UpdatedAt = now
	return changed
}

func finalAuditMilestone() assistantstore.GoalMilestone {
	milestone := goalMilestone(goalFinalAuditPhaseID, "01", goalFinalAuditMarker, "Run a read-only whole-goal audit and decide whether the Goal can be completed.", assistantstore.GoalMilestoneStatusClaimed)
	milestone.ID = goalFinalAuditMilestoneID
	milestone.AcceptanceCriteria = []string{
		"The audit covers every Goal success criterion and constraint.",
		"The audit checks current repo state, tests, docs, examples, browser or operational evidence, and delivery cleanliness.",
		"If the Goal is not complete, the report includes concrete gaps, blockers, or operator questions instead of a generic pass.",
	}
	milestone.EvidenceRequirements = []string{
		"Validation commands or inspection evidence for the whole Goal.",
		"Explicit delivery state, including commit or clean working tree status for remote build Goals.",
		"A GOAL_CHALLENGE report whose goal_complete field is true only when the whole Goal is certifiably complete.",
	}
	milestone.ChallengePolicy = "This final audit is the only challenge allowed to complete the Goal."
	return milestone
}

func finalAuditPlanMilestone(plan *assistantstore.GoalPlan) (assistantstore.GoalPlanPhase, assistantstore.GoalMilestone, bool) {
	if plan == nil {
		return assistantstore.GoalPlanPhase{}, assistantstore.GoalMilestone{}, false
	}
	planValue := assistantstore.NormalizeGoalPlan(*plan)
	for _, phase := range planValue.Phases {
		if phase.ID != goalFinalAuditPhaseID {
			continue
		}
		for _, milestone := range phase.Milestones {
			if isFinalAuditMilestoneID(milestone.ID) {
				return phase, milestone, true
			}
		}
	}
	return assistantstore.GoalPlanPhase{}, assistantstore.GoalMilestone{}, false
}

func phaseHasActiveMilestone(phase assistantstore.GoalPlanPhase) bool {
	for _, milestone := range phase.Milestones {
		switch milestone.Status {
		case assistantstore.GoalMilestoneStatusInProgress, assistantstore.GoalMilestoneStatusClaimed, assistantstore.GoalMilestoneStatusChallenged, assistantstore.GoalMilestoneStatusBlocked:
			return true
		}
	}
	return false
}

func defaultGoalMilestones(goal assistantstore.Goal, phase assistantstore.GoalPlanPhase) []assistantstore.GoalMilestone {
	phaseID := strings.TrimSpace(phase.ID)
	if phaseID == "" {
		phaseID = "phase"
	}
	status := assistantstore.GoalMilestoneStatusPending
	if phase.Status == assistantstore.GoalPlanPhaseStatusInProgress {
		status = assistantstore.GoalMilestoneStatusInProgress
	}
	if phase.Status == assistantstore.GoalPlanPhaseStatusBlocked {
		status = assistantstore.GoalMilestoneStatusBlocked
	}
	if phase.Status == assistantstore.GoalPlanPhaseStatusCompleted || phase.Status == assistantstore.GoalPlanPhaseStatusSkipped {
		status = assistantstore.GoalMilestoneStatusAccepted
	}
	milestones := []assistantstore.GoalMilestone{
		goalMilestone(phaseID, "01_scope", "Scope the slice", "Identify the exact repo evidence, feature matrix entry, acceptance criteria, and risks for this phase before broad implementation.", status),
		goalMilestone(phaseID, "02_build", "Deliver a usable slice", "Implement the smallest cohesive capability that an operator could inspect or use, with code and documentation kept in sync.", assistantstore.GoalMilestoneStatusPending),
		goalMilestone(phaseID, "03_prove", "Prove the slice", "Run the relevant checks, browser or operational UAT, and update durable evidence so the supervisor can judge what changed.", assistantstore.GoalMilestoneStatusPending),
	}
	if goal.Kind == assistantstore.GoalKindRoutine || goal.Kind == assistantstore.GoalKindWatch {
		milestones[0].Title = "Inspect current state"
		milestones[1].Title = "Run one useful cycle"
		milestones[2].Title = "Harden the loop"
	}
	if phase.Status == assistantstore.GoalPlanPhaseStatusCompleted || phase.Status == assistantstore.GoalPlanPhaseStatusSkipped {
		for index := range milestones {
			milestones[index].Status = assistantstore.GoalMilestoneStatusAccepted
		}
	}
	return milestones
}

func goalMilestone(phaseID, suffix, title, objective, status string) assistantstore.GoalMilestone {
	return assistantstore.GoalMilestone{
		ID:        phaseID + "_milestone_" + suffix,
		PhaseID:   phaseID,
		Title:     title,
		Objective: objective,
		Status:    status,
		AcceptanceCriteria: []string{
			"The task result makes a concrete claim about this milestone, not only generic activity.",
			"Changed files, tests, docs, screenshots, logs, or inspection evidence can be checked independently.",
			"Any uncertainty is captured as a specific gap, blocker, or operator question.",
		},
		EvidenceRequirements: []string{
			"Relevant changed files or explicit no-change rationale.",
			"Validation command, browser UAT, manual inspection, or operational check output.",
			"Updated durable planning artefact when the Goal is a build or parity objective.",
		},
		ChallengePolicy: "Run a read-only challenge immediately after this milestone is claimed.",
	}
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
	decision = assistantstore.NormalizeGoalSupervisorDecision(decision)
	phase, hasPhase := goalPlanPhaseByID(goal.Plan, decision.PhaseID)
	milestone, hasMilestone := goalPlanMilestoneByID(goal.Plan, decision.MilestoneID)
	gap, hasGap := goalPlanGapByID(goal.Plan, decision.GapID)
	if decision.TaskType == assistantstore.GoalTaskTypeChallenge {
		return goalAutopilotChallengeTaskGoal(goal, decision, phase, milestone, hasPhase, hasMilestone, reports)
	}
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
	if hasMilestone {
		fmt.Fprintf(&b, "\n\nSelected Goal milestone:\n- Milestone ID: %s\n- Title: %s\n- Objective: %s\n- Status: %s", milestone.ID, milestone.Title, milestone.Objective, milestone.Status)
		if len(milestone.AcceptanceCriteria) > 0 {
			b.WriteString("\n- Milestone acceptance:")
			for _, criterion := range milestone.AcceptanceCriteria {
				fmt.Fprintf(&b, "\n  - %s", criterion)
			}
		}
		if len(milestone.EvidenceRequirements) > 0 {
			b.WriteString("\n- Evidence required:")
			for _, requirement := range milestone.EvidenceRequirements {
				fmt.Fprintf(&b, "\n  - %s", requirement)
			}
		}
	}
	if hasGap {
		fmt.Fprintf(&b, "\n\nSelected challenge gap:\n- Gap ID: %s\n- Area: %s\n- Severity: %s\n- Claim under challenge: %s\n- Evidence: %s\n- Suggested task: %s", gap.ID, gap.Area, gap.Severity, gap.Claim, gap.Evidence, gap.SuggestedTask)
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
			for _, milestone := range phase.Milestones {
				fmt.Fprintf(&b, "\n  - %s [%s]: %s", milestone.ID, milestone.Status, milestone.Title)
			}
		}
		if len(goal.Plan.Gaps) > 0 {
			b.WriteString("\n\nOpen and recent challenge gaps:")
			for _, gap := range goal.Plan.Gaps[:assistantMinInt(len(goal.Plan.Gaps), 12)] {
				fmt.Fprintf(&b, "\n- %s [%s/%s] %s: %s", gap.ID, gap.Status, gap.Severity, firstNonEmptyString(gap.Area, "gap"), firstNonEmptyString(gap.SuggestedTask, gap.Claim))
			}
		}
	}
	if len(reports) > 0 {
		b.WriteString("\n\nRecent structured Goal task reports:")
		for _, report := range reports[:assistantMinInt(len(reports), 4)] {
			fmt.Fprintf(&b, "\n- Task %s %s phase %s milestone %s: %s", taskShortID(report.TaskID), firstNonEmptyString(report.TaskType, "build"), firstNonEmptyString(report.PhaseID, "unknown"), firstNonEmptyString(report.MilestoneID, "unknown"), firstNonEmptyString(report.Summary, report.Status))
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
	if decision.TaskType == assistantstore.GoalTaskTypeGapFix {
		b.WriteString("\n- This is a gap-fix task. Fix or disprove the selected challenge gap before taking unrelated feature work.")
	}
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
	b.WriteString("\nThe object must include: task_type, phase_id, milestone_id, summary, advanced_goal, phase_complete, goal_complete, changed_files, validation, follow_ups, blockers, questions, claims.")
	b.WriteString("\nUse task_type `build` for normal milestone work and `gap_fix` when this task is repairing a selected challenge gap. Include gap_ids for gap-fix work.")
	b.WriteString("\nClaims must be an array of objects with claim and evidence fields. Make claims specific enough for a later challenge task to disprove.")
	b.WriteString("\nThe reviewer will independently compare the diff, validation, and changed files against the Goal; do not claim progress unless the task materially advances the selected phase.")
	b.WriteString("\nA build or gap-fix task may claim a milestone or phase complete, but the supervisor will not accept it as done until a separate challenge task passes.")
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

func goalAutopilotChallengeTaskGoal(goal assistantstore.Goal, decision assistantstore.GoalSupervisorDecision, phase assistantstore.GoalPlanPhase, milestone assistantstore.GoalMilestone, hasPhase, hasMilestone bool, reports []assistantstore.GoalTaskReport) string {
	finalAudit := isFinalAuditMilestoneID(decision.MilestoneID) || isFinalAuditMilestoneID(milestone.ID)
	var b strings.Builder
	fmt.Fprintf(&b, "Autopilot challenge task for %s Goal `%s`: %s\n\nObjective: %s", goal.Kind, goal.ID, goal.Title, goal.Objective)
	if hasPhase {
		fmt.Fprintf(&b, "\n\nPhase under review:\n- Phase ID: %s\n- Title: %s\n- Objective: %s\n- Status: %s", phase.ID, phase.Title, phase.Objective, phase.Status)
	}
	if hasMilestone {
		fmt.Fprintf(&b, "\n\nMilestone under challenge:\n- Milestone ID: %s\n- Title: %s\n- Objective: %s\n- Status: %s", milestone.ID, milestone.Title, milestone.Objective, milestone.Status)
		if len(milestone.AcceptanceCriteria) > 0 {
			b.WriteString("\n- Acceptance criteria:")
			for _, criterion := range milestone.AcceptanceCriteria {
				fmt.Fprintf(&b, "\n  - %s", criterion)
			}
		}
		if len(milestone.Claims) > 0 {
			b.WriteString("\n- Claims to verify:")
			for _, claim := range milestone.Claims {
				fmt.Fprintf(&b, "\n  - %s", claim.Claim)
				if len(claim.Evidence) > 0 {
					fmt.Fprintf(&b, " Evidence: %s.", strings.Join(claim.Evidence[:assistantMinInt(len(claim.Evidence), 4)], "; "))
				}
			}
		}
		if len(milestone.Evidence) > 0 {
			b.WriteString("\n- Prior milestone evidence:")
			for _, evidence := range milestone.Evidence[:assistantMinInt(len(milestone.Evidence), 8)] {
				fmt.Fprintf(&b, "\n  - %s", evidence)
			}
		}
	}
	if goal.Details != "" {
		fmt.Fprintf(&b, "\n\nDetails:\n%s", goal.Details)
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
	if len(reports) > 0 {
		b.WriteString("\n\nRecent structured Goal task reports:")
		for _, report := range reports[:assistantMinInt(len(reports), 6)] {
			fmt.Fprintf(&b, "\n- Task %s %s phase %s milestone %s: %s", taskShortID(report.TaskID), firstNonEmptyString(report.TaskType, "build"), firstNonEmptyString(report.PhaseID, "unknown"), firstNonEmptyString(report.MilestoneID, "unknown"), firstNonEmptyString(report.Summary, report.Status))
			if len(report.Claims) > 0 {
				claimTexts := make([]string, 0, assistantMinInt(len(report.Claims), 3))
				for _, claim := range report.Claims[:assistantMinInt(len(report.Claims), 3)] {
					claimTexts = append(claimTexts, claim.Claim)
				}
				fmt.Fprintf(&b, " Claims: %s.", strings.Join(claimTexts, "; "))
			}
			if len(report.Validation) > 0 {
				fmt.Fprintf(&b, " Validation: %s.", strings.Join(report.Validation[:assistantMinInt(len(report.Validation), 3)], "; "))
			}
		}
	}
	if finalAudit {
		b.WriteString("\n\nFinal whole-goal audit mode:")
		b.WriteString("\n- This task decides whether the Goal can be marked complete. Do not limit the review to the last phase or milestone.")
		b.WriteString("\n- Evaluate every Goal success criterion and constraint against the current repo, docs, tests, examples, browser or operational evidence, and recent challenge history.")
		b.WriteString("\n- For remote build Goals, inspect the target repository delivery state. A dirty worktree, uncaptured diff, uncommitted deliverable, or missing exact commit/version is a high delivery gap unless the Goal explicitly permits an undelivered state.")
		b.WriteString("\n- If the Goal is not complete, return concrete gaps, blockers, or operator questions that the supervisor can act on. Do not return a generic pass with goal_complete false and no gaps.")
		b.WriteString("\n- Set goal_complete true only when the whole Goal is credibly complete, not merely when this audit task ran successfully.")
	}
	b.WriteString("\n\nChallenge mode:")
	b.WriteString("\n- Do not implement new features in this task. Inspect the repo, docs, tests, examples, browser behaviour, and durable planning artefacts.")
	b.WriteString("\n- Try to disprove the milestone claims. Look for missing behaviour, shallow tests, self-certified matrices, broken examples, accessibility gaps, packaging gaps, performance gaps, and mismatches with the Goal.")
	b.WriteString("\n- Run lightweight validation or browser checks where practical, but keep the task read-only unless a tiny diagnostic artefact is unavoidable.")
	if finalAudit {
		b.WriteString("\n- Passing with goal_complete true means the whole Goal can complete. Failing is useful progress: record concrete gaps with suggested repair tasks.")
	} else {
		b.WriteString("\n- Passing means the milestone evidence is credible enough for Autopilot to move on. Failing is useful progress: record concrete gaps with suggested repair tasks.")
	}
	b.WriteString("\n\nGoal challenge report contract:")
	b.WriteString("\nAt the end of the task result, include a single-line JSON object prefixed with `GOAL_CHALLENGE:`.")
	b.WriteString("\nThe object must include: milestone_id, verdict, summary, evidence, claims_challenged, gaps, goal_complete.")
	b.WriteString("\nUse verdict `passed`, `failed`, or `needs_user`. Gaps must be an array of objects with area, claim, severity, evidence, and suggested_task.")
	b.WriteString("\nSet goal_complete true only if this is the final milestone, all success criteria are credibly satisfied, and no open critical/high gaps remain.")
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

func goalPlanMilestoneByID(plan *assistantstore.GoalPlan, milestoneID string) (assistantstore.GoalMilestone, bool) {
	milestoneID = strings.TrimSpace(milestoneID)
	if plan == nil || milestoneID == "" {
		return assistantstore.GoalMilestone{}, false
	}
	for _, phase := range plan.Phases {
		for _, milestone := range phase.Milestones {
			if milestone.ID == milestoneID {
				return milestone, true
			}
		}
	}
	return assistantstore.GoalMilestone{}, false
}

func goalPlanPhaseForMilestone(plan *assistantstore.GoalPlan, milestoneID string) (assistantstore.GoalPlanPhase, bool) {
	milestoneID = strings.TrimSpace(milestoneID)
	if plan == nil || milestoneID == "" {
		return assistantstore.GoalPlanPhase{}, false
	}
	for _, phase := range plan.Phases {
		for _, milestone := range phase.Milestones {
			if milestone.ID == milestoneID {
				return phase, true
			}
		}
	}
	return assistantstore.GoalPlanPhase{}, false
}

func goalPhaseMilestoneIndex(phase assistantstore.GoalPlanPhase, milestoneID string) int {
	milestoneID = strings.TrimSpace(milestoneID)
	if milestoneID == "" && len(phase.Milestones) == 1 {
		return 0
	}
	for index, milestone := range phase.Milestones {
		if milestone.ID == milestoneID {
			return index
		}
	}
	return -1
}

func appendGoalClaim(claims []assistantstore.GoalClaim, claim assistantstore.GoalClaim) []assistantstore.GoalClaim {
	claim = assistantstore.NormalizeGoalClaim(claim, len(claims))
	if claim.Claim == "" {
		return claims
	}
	key := strings.ToLower(strings.TrimSpace(claim.MilestoneID + "\x00" + claim.Claim))
	for index, existing := range claims {
		existingKey := strings.ToLower(strings.TrimSpace(existing.MilestoneID + "\x00" + existing.Claim))
		if existing.ID == claim.ID || existingKey == key {
			claims[index] = claim
			return claims
		}
	}
	return append(claims, claim)
}

func appendGoalChallenge(challenges []assistantstore.GoalChallenge, challenge assistantstore.GoalChallenge) []assistantstore.GoalChallenge {
	challenge = assistantstore.NormalizeGoalChallenge(challenge, len(challenges))
	for index, existing := range challenges {
		if existing.ID == challenge.ID || (challenge.TaskID != "" && existing.TaskID == challenge.TaskID) {
			challenges[index] = challenge
			return challenges
		}
	}
	return append(challenges, challenge)
}

func appendGoalGap(gaps []assistantstore.GoalGap, gap assistantstore.GoalGap) []assistantstore.GoalGap {
	gap = assistantstore.NormalizeGoalGap(gap, len(gaps))
	key := goalGapKey(gap)
	for index, existing := range gaps {
		if existing.ID == gap.ID || goalGapKey(existing) == key {
			if existing.ID != "" {
				gap.ID = existing.ID
			}
			if gap.CreatedAt.IsZero() {
				gap.CreatedAt = existing.CreatedAt
			}
			gaps[index] = gap
			return gaps
		}
	}
	return append(gaps, gap)
}

func goalGapKey(gap assistantstore.GoalGap) string {
	return strings.ToLower(strings.Join([]string{
		strings.TrimSpace(gap.PhaseID),
		strings.TrimSpace(gap.MilestoneID),
		strings.TrimSpace(gap.Area),
		strings.TrimSpace(gap.Claim),
		strings.TrimSpace(gap.SuggestedTask),
	}, "\x00"))
}

func goalPlanGapByID(plan *assistantstore.GoalPlan, gapID string) (assistantstore.GoalGap, bool) {
	gapID = strings.TrimSpace(gapID)
	if plan == nil || gapID == "" {
		return assistantstore.GoalGap{}, false
	}
	for _, gap := range plan.Gaps {
		if gap.ID == gapID {
			return gap, true
		}
	}
	return assistantstore.GoalGap{}, false
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
	if goal.Plan != nil {
		plan := assistantstore.NormalizeGoalPlan(*goal.Plan)
		milestones, accepted, gaps, openGaps := goalPlanFeedbackCounts(plan)
		fmt.Fprintf(&b, "\nPlan: %s, %s", labelFromSlugForReply(plan.Status), pluralForReply(len(plan.Phases), "phase", "phases"))
		if milestones > 0 {
			fmt.Fprintf(&b, ", %d/%d milestones accepted", accepted, milestones)
		}
		if gaps > 0 {
			fmt.Fprintf(&b, ", %d/%d gaps open", openGaps, gaps)
		}
		if plan.CurrentPhaseID != "" {
			fmt.Fprintf(&b, "\nCurrent phase: %s", plan.CurrentPhaseID)
		}
	}
	if len(timeline.Watches) > 0 {
		fmt.Fprintf(&b, "\nWatches: %d", len(timeline.Watches))
	}
	if len(timeline.Assessments) > 0 {
		fmt.Fprintf(&b, "\nLatest assessment: %s", firstNonEmptyString(timeline.Assessments[0].Summary, timeline.Assessments[0].Decision))
	}
	return b.String(), nil
}

func goalPlanFeedbackCounts(plan assistantstore.GoalPlan) (milestones, accepted, gaps, openGaps int) {
	for _, phase := range plan.Phases {
		for _, milestone := range phase.Milestones {
			milestones++
			if milestone.Status == assistantstore.GoalMilestoneStatusAccepted {
				accepted++
			}
		}
	}
	gaps = len(plan.Gaps)
	for _, gap := range plan.Gaps {
		switch gap.Status {
		case assistantstore.GoalGapStatusFixed, assistantstore.GoalGapStatusAcceptedRisk, assistantstore.GoalGapStatusDisproven:
		default:
			openGaps++
		}
	}
	return milestones, accepted, gaps, openGaps
}

func labelFromSlugForReply(value string) string {
	value = strings.TrimSpace(strings.ReplaceAll(value, "_", " "))
	if value == "" {
		return "unknown"
	}
	return value
}

func pluralForReply(count int, singular, plural string) string {
	if count == 1 {
		return fmt.Sprintf("%d %s", count, singular)
	}
	return fmt.Sprintf("%d %s", count, plural)
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
	if goalTaskReportCompletesGoal(report) && goalPlanComplete(goal.Plan) && allGoalGapsClosed(goal.Plan) {
		goal.Status = assistantstore.GoalStatusCompleted
		if goal.Autopilot != nil {
			goal.Autopilot.Status = assistantstore.GoalAutopilotStatusCompleted
			goal.Autopilot.StopReasons = appendGoalStopReason(goal.Autopilot.StopReasons, "Challenge accepted the Goal as complete.")
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
		TaskType:      assistantstore.GoalTaskTypeBuild,
		Title:         friendlyTaskTitle(t),
		Status:        strings.TrimSpace(t.Status),
		Summary:       goalTaskResultSummary(t.Result),
		AdvancedGoal:  true,
		ResultExcerpt: truncateAssistantRunText(t.Result, 900),
		CreatedAt:     now,
	}
	if structured, ok := parseStructuredGoalReport(t.Result); ok {
		report.TaskType = firstNonEmptyString(structured.TaskType, report.TaskType)
		report.PhaseID = firstNonEmptyString(structured.PhaseID, report.PhaseID)
		report.MilestoneID = firstNonEmptyString(structured.MilestoneID, report.MilestoneID)
		report.Summary = firstNonEmptyString(structured.Summary, report.Summary)
		report.AdvancedGoal = structured.AdvancedGoal
		report.PhaseComplete = structured.PhaseComplete
		report.GoalComplete = structured.GoalComplete
		report.NoChange = structured.NoChange
		report.ChangedFiles = []string(structured.ChangedFiles)
		report.Validation = []string(structured.Validation)
		report.FollowUps = []string(structured.FollowUps)
		report.Blockers = []string(structured.Blockers)
		report.Questions = []string(structured.Questions)
		report.Claims = structured.normalizedClaims()
		report.GapIDs = []string(structured.GapIDs)
		if structured.Challenge != nil {
			challenge := *structured.Challenge
			report.Challenge = &challenge
			report.TaskType = assistantstore.GoalTaskTypeChallenge
		}
	}
	if challenge, ok := parseStructuredGoalChallenge(t.Result); ok {
		report.TaskType = assistantstore.GoalTaskTypeChallenge
		report.MilestoneID = firstNonEmptyString(challenge.MilestoneID, report.MilestoneID)
		report.Summary = firstNonEmptyString(challenge.Summary, report.Summary)
		report.AdvancedGoal = true
		report.GoalComplete = challenge.GoalComplete
		report.Challenge = &challenge
		report.Validation = appendUniqueStrings(report.Validation, challenge.Evidence...)
	}
	if report.MilestoneID == "" {
		if milestone, ok := inferReportMilestone(goal, report.PhaseID); ok {
			report.MilestoneID = milestone.ID
		}
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
	if report.TaskType == assistantstore.GoalTaskTypeChallenge && report.Challenge != nil && report.Challenge.Verdict == assistantstore.GoalChallengeVerdictNeedsUser {
		report.Questions = appendUniqueStrings(report.Questions, firstNonEmptyString(report.Challenge.Summary, "Challenge needs operator input before this milestone can be accepted."))
		report.GoalComplete = false
	}
	if report.TaskType == assistantstore.GoalTaskTypeChallenge && report.GoalComplete && remoteTask(t) && resultWarnsDirtyGitTree(t.Result) {
		report.GoalComplete = false
		deliveryGap := assistantstore.NormalizeGoalGap(assistantstore.GoalGap{
			ID:            id.New("ggap"),
			PhaseID:       report.PhaseID,
			MilestoneID:   report.MilestoneID,
			Area:          "delivery state",
			Claim:         "Remote build Goal cannot complete while the target repository is dirty or undelivered.",
			Severity:      assistantstore.GoalGapSeverityHigh,
			Evidence:      "Remote result included a dirty Git worktree warning.",
			SuggestedTask: "Commit or intentionally capture the remote repository deliverable, then rerun the final whole-goal audit with the exact commit/version recorded.",
			Source:        "supervisor",
			SourceTaskID:  report.TaskID,
			Status:        assistantstore.GoalGapStatusOpen,
			CreatedAt:     now,
			UpdatedAt:     now,
		}, 0)
		if report.Challenge != nil {
			report.Challenge.GoalComplete = false
			report.Challenge.Gaps = appendGoalGap(report.Challenge.Gaps, deliveryGap)
		}
		report.Summary = firstNonEmptyString(report.Summary, "Challenge passed") + " Delivery is not complete because the remote target repository is dirty."
		report.Validation = appendUniqueStrings(report.Validation, deliveryGap.Evidence)
	}
	if report.GoalComplete && report.TaskType != assistantstore.GoalTaskTypeChallenge {
		report.GoalComplete = false
		report.FollowUps = appendUniqueStrings(report.FollowUps, "Worker claimed Goal completion; supervisor will require a read-only challenge before accepting it.")
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
	TaskType      string                        `json:"task_type"`
	PhaseID       string                        `json:"phase_id"`
	MilestoneID   string                        `json:"milestone_id"`
	Summary       string                        `json:"summary"`
	AdvancedGoal  bool                          `json:"advanced_goal"`
	PhaseComplete bool                          `json:"phase_complete"`
	GoalComplete  bool                          `json:"goal_complete"`
	NoChange      bool                          `json:"no_change"`
	ChangedFiles  goalReportStrings             `json:"changed_files"`
	Validation    goalReportStrings             `json:"validation"`
	FollowUps     goalReportStrings             `json:"follow_ups"`
	Blockers      goalReportStrings             `json:"blockers"`
	Questions     goalReportStrings             `json:"questions"`
	Claims        []structuredGoalClaim         `json:"claims"`
	Challenge     *assistantstore.GoalChallenge `json:"challenge"`
	GapIDs        goalReportStrings             `json:"gap_ids"`
}

func (report structuredGoalReport) normalizedClaims() []assistantstore.GoalClaim {
	claims := make([]assistantstore.GoalClaim, 0, len(report.Claims))
	for index, claim := range report.Claims {
		goalClaim := assistantstore.NormalizeGoalClaim(assistantstore.GoalClaim{
			ID:           claim.ID,
			MilestoneID:  claim.MilestoneID,
			Claim:        claim.Claim,
			Evidence:     normalizeGoalReportStrings([]string(claim.Evidence), 24),
			SourceTaskID: claim.SourceTaskID,
			Status:       claim.Status,
			CreatedAt:    claim.CreatedAt,
		}, index)
		if goalClaim.Claim != "" {
			claims = append(claims, goalClaim)
		}
	}
	return claims
}

type structuredGoalChallenge struct {
	MilestoneID      string                   `json:"milestone_id"`
	Verdict          string                   `json:"verdict"`
	Summary          string                   `json:"summary"`
	Evidence         []string                 `json:"evidence"`
	ClaimsChallenged []string                 `json:"claims_challenged"`
	Gaps             []assistantstore.GoalGap `json:"gaps"`
	GoalComplete     bool                     `json:"goal_complete"`
}

type structuredGoalClaim struct {
	ID           string            `json:"id,omitempty"`
	MilestoneID  string            `json:"milestone_id,omitempty"`
	Claim        string            `json:"claim"`
	Evidence     goalReportStrings `json:"evidence,omitempty"`
	SourceTaskID string            `json:"source_task_id,omitempty"`
	Status       string            `json:"status,omitempty"`
	CreatedAt    time.Time         `json:"created_at,omitempty"`
}

type goalReportStrings []string

func (values *goalReportStrings) UnmarshalJSON(data []byte) error {
	var stringsValue []string
	if err := json.Unmarshal(data, &stringsValue); err == nil {
		*values = stringsValue
		return nil
	}
	var single string
	if err := json.Unmarshal(data, &single); err == nil {
		if strings.TrimSpace(single) == "" {
			*values = nil
			return nil
		}
		*values = []string{single}
		return nil
	}
	var mixed []any
	if err := json.Unmarshal(data, &mixed); err == nil {
		out := make([]string, 0, len(mixed))
		for _, item := range mixed {
			if text, ok := item.(string); ok {
				out = append(out, text)
			}
		}
		*values = out
		return nil
	}
	*values = nil
	return nil
}

func parseStructuredGoalReport(result string) (structuredGoalReport, bool) {
	var latest structuredGoalReport
	found := false
	for _, line := range strings.Split(result, "\n") {
		trimmed := strings.TrimSpace(line)
		index := strings.Index(strings.ToUpper(trimmed), "GOAL_REPORT:")
		if index < 0 {
			continue
		}
		raw := goalReportJSONPayload(strings.TrimSpace(trimmed[index+len("GOAL_REPORT:"):]))
		var report structuredGoalReport
		if err := json.Unmarshal([]byte(raw), &report); err == nil {
			report.TaskType = strings.TrimSpace(report.TaskType)
			report.PhaseID = strings.TrimSpace(report.PhaseID)
			report.MilestoneID = strings.TrimSpace(report.MilestoneID)
			report.Summary = strings.TrimSpace(report.Summary)
			report.ChangedFiles = goalReportStrings(normalizeGoalReportStrings([]string(report.ChangedFiles), 64))
			report.Validation = goalReportStrings(normalizeGoalReportStrings([]string(report.Validation), 24))
			report.FollowUps = goalReportStrings(normalizeGoalReportStrings([]string(report.FollowUps), 24))
			report.Blockers = goalReportStrings(normalizeGoalReportStrings([]string(report.Blockers), 24))
			report.Questions = goalReportStrings(normalizeGoalReportStrings([]string(report.Questions), 12))
			report.GapIDs = goalReportStrings(normalizeGoalReportStrings([]string(report.GapIDs), 24))
			claims := make([]assistantstore.GoalClaim, 0, len(report.Claims))
			for index, claim := range report.Claims {
				goalClaim := assistantstore.NormalizeGoalClaim(assistantstore.GoalClaim{
					ID:           claim.ID,
					MilestoneID:  claim.MilestoneID,
					Claim:        claim.Claim,
					Evidence:     normalizeGoalReportStrings([]string(claim.Evidence), 24),
					SourceTaskID: claim.SourceTaskID,
					Status:       claim.Status,
					CreatedAt:    claim.CreatedAt,
				}, index)
				if goalClaim.Claim != "" {
					claims = append(claims, goalClaim)
				}
			}
			report.Claims = make([]structuredGoalClaim, 0, len(claims))
			for _, claim := range claims {
				report.Claims = append(report.Claims, structuredGoalClaim{
					ID:           claim.ID,
					MilestoneID:  claim.MilestoneID,
					Claim:        claim.Claim,
					Evidence:     goalReportStrings(claim.Evidence),
					SourceTaskID: claim.SourceTaskID,
					Status:       claim.Status,
					CreatedAt:    claim.CreatedAt,
				})
			}
			if report.Challenge != nil {
				challenge := assistantstore.NormalizeGoalChallenge(*report.Challenge, 0)
				report.Challenge = &challenge
			}
			latest = report
			found = true
		}
	}
	return latest, found
}

func parseStructuredGoalChallenge(result string) (assistantstore.GoalChallenge, bool) {
	var latest assistantstore.GoalChallenge
	found := false
	for _, line := range strings.Split(result, "\n") {
		trimmed := strings.TrimSpace(line)
		index := strings.Index(strings.ToUpper(trimmed), "GOAL_CHALLENGE:")
		if index < 0 {
			continue
		}
		raw := goalReportJSONPayload(strings.TrimSpace(trimmed[index+len("GOAL_CHALLENGE:"):]))
		var structured structuredGoalChallenge
		if err := json.Unmarshal([]byte(raw), &structured); err == nil {
			challenge := assistantstore.GoalChallenge{
				MilestoneID:      strings.TrimSpace(structured.MilestoneID),
				Verdict:          structured.Verdict,
				Summary:          strings.TrimSpace(structured.Summary),
				Evidence:         normalizeGoalReportStrings(structured.Evidence, 24),
				ClaimsChallenged: normalizeGoalReportStrings(structured.ClaimsChallenged, 24),
				Gaps:             structured.Gaps,
				GoalComplete:     structured.GoalComplete,
				CreatedAt:        time.Now().UTC(),
			}
			challenge = assistantstore.NormalizeGoalChallenge(challenge, 0)
			latest = challenge
			found = true
		}
	}
	return latest, found
}

func goalReportJSONPayload(raw string) string {
	raw = strings.TrimSpace(raw)
	if !strings.HasPrefix(raw, "{") {
		return raw
	}
	depth := 0
	inString := false
	escaped := false
	for index, r := range raw {
		if inString {
			if escaped {
				escaped = false
				continue
			}
			if r == '\\' {
				escaped = true
				continue
			}
			if r == '"' {
				inString = false
			}
			continue
		}
		switch r {
		case '"':
			inString = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return raw[:index+1]
			}
		}
	}
	return raw
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
		upper := strings.ToUpper(line)
		if line == "" || strings.HasPrefix(upper, "GOAL_REPORT:") || strings.HasPrefix(upper, "GOAL_CHALLENGE:") {
			continue
		}
		return truncateAssistantRunText(line, 240)
	}
	return truncateAssistantRunText(result, 240)
}

func resultWarnsDirtyGitTree(result string) bool {
	normalized := strings.ToLower(result)
	return strings.Contains(normalized, "warning: git tree") && strings.Contains(normalized, "dirty")
}

func inferReportMilestone(goal assistantstore.Goal, phaseID string) (assistantstore.GoalMilestone, bool) {
	phase, ok := goalPlanPhaseByID(goal.Plan, phaseID)
	if !ok {
		return assistantstore.GoalMilestone{}, false
	}
	for _, milestone := range phase.Milestones {
		switch milestone.Status {
		case assistantstore.GoalMilestoneStatusInProgress, assistantstore.GoalMilestoneStatusPending, assistantstore.GoalMilestoneStatusClaimed, assistantstore.GoalMilestoneStatusChallenged:
			return milestone, true
		}
	}
	return assistantstore.GoalMilestone{}, false
}

func applyGoalTaskReportToPlan(goal *assistantstore.Goal, report assistantstore.GoalTaskReport, now time.Time) {
	if goal == nil || goal.Plan == nil {
		return
	}
	report = assistantstore.NormalizeGoalTaskReport(report)
	if strings.TrimSpace(report.PhaseID) == "" && strings.TrimSpace(report.MilestoneID) != "" {
		if phase, ok := goalPlanPhaseForMilestone(goal.Plan, report.MilestoneID); ok {
			report.PhaseID = phase.ID
		}
	}
	if strings.TrimSpace(report.PhaseID) == "" {
		return
	}
	plan := assistantstore.NormalizeGoalPlan(*goal.Plan)
	ensureGoalPlanMilestones(&plan, *goal, now)
	if report.TaskType == assistantstore.GoalTaskTypeChallenge && report.Challenge != nil {
		applyGoalChallengeReportToPlan(&plan, report, now)
		updateGoalPlanProgress(&plan, now)
		goal.Plan = &plan
		if goal.Autopilot != nil {
			if phase, milestone, ok := selectGoalPlanMilestone(&plan); ok {
				goal.Autopilot.CurrentPhaseID = phase.ID
				_ = milestone
			} else {
				goal.Autopilot.CurrentPhaseID = plan.CurrentPhaseID
			}
		}
		return
	}
	for index := range plan.Phases {
		if plan.Phases[index].ID != report.PhaseID {
			continue
		}
		if !stringSliceContains(plan.Phases[index].TaskIDs, report.TaskID) {
			plan.Phases[index].TaskIDs = append(plan.Phases[index].TaskIDs, report.TaskID)
		}
		milestoneIndex := goalPhaseMilestoneIndex(plan.Phases[index], report.MilestoneID)
		if milestoneIndex >= 0 {
			milestone := &plan.Phases[index].Milestones[milestoneIndex]
			if !stringSliceContains(milestone.TaskIDs, report.TaskID) {
				milestone.TaskIDs = append(milestone.TaskIDs, report.TaskID)
			}
			for claimIndex, claim := range report.Claims {
				claim.MilestoneID = firstNonEmptyString(claim.MilestoneID, milestone.ID)
				claim.SourceTaskID = firstNonEmptyString(claim.SourceTaskID, report.TaskID)
				if claim.CreatedAt.IsZero() {
					claim.CreatedAt = now
				}
				claim = assistantstore.NormalizeGoalClaim(claim, len(milestone.Claims)+claimIndex)
				if claim.Claim != "" {
					milestone.Claims = appendGoalClaim(milestone.Claims, claim)
				}
			}
			if len(report.Claims) == 0 && (report.PhaseComplete || report.GoalComplete) && strings.TrimSpace(report.Summary) != "" {
				milestone.Claims = appendGoalClaim(milestone.Claims, assistantstore.NormalizeGoalClaim(assistantstore.GoalClaim{
					ID:           id.New("gclaim"),
					MilestoneID:  milestone.ID,
					Claim:        report.Summary,
					Evidence:     append([]string(nil), report.Validation...),
					SourceTaskID: report.TaskID,
					Status:       "claimed",
					CreatedAt:    now,
				}, len(milestone.Claims)))
			}
			milestone.Evidence = appendUniqueStrings(milestone.Evidence, report.Summary)
			milestone.Evidence = appendUniqueStrings(milestone.Evidence, report.Validation...)
			if len(report.Blockers) > 0 || len(report.Questions) > 0 {
				milestone.Status = assistantstore.GoalMilestoneStatusBlocked
			} else if report.AdvancedGoal || report.PhaseComplete || report.GoalComplete || len(report.Claims) > 0 {
				milestone.Status = assistantstore.GoalMilestoneStatusClaimed
			} else if milestone.Status == assistantstore.GoalMilestoneStatusPending {
				milestone.Status = assistantstore.GoalMilestoneStatusInProgress
			}
		}
		if len(report.Blockers) > 0 || len(report.Questions) > 0 {
			plan.Phases[index].Status = assistantstore.GoalPlanPhaseStatusBlocked
		} else {
			plan.Phases[index].Status = assistantstore.GoalPlanPhaseStatusInProgress
		}
		if report.Summary != "" {
			plan.Phases[index].Evidence = appendUniqueStrings(plan.Phases[index].Evidence, report.Summary)
		}
		break
	}
	if report.TaskType == assistantstore.GoalTaskTypeGapFix {
		for _, gapID := range report.GapIDs {
			for index := range plan.Gaps {
				if plan.Gaps[index].ID != gapID {
					continue
				}
				plan.Gaps[index].TaskIDs = appendUniqueStrings(plan.Gaps[index].TaskIDs, report.TaskID)
				plan.Gaps[index].UpdatedAt = now
				if len(report.Blockers) > 0 || len(report.Questions) > 0 {
					plan.Gaps[index].Status = assistantstore.GoalGapStatusOpen
				} else if report.NoChange {
					plan.Gaps[index].Status = assistantstore.GoalGapStatusDisproven
				} else if report.AdvancedGoal {
					plan.Gaps[index].Status = assistantstore.GoalGapStatusFixed
				}
				break
			}
		}
	}
	if len(report.Blockers) > 0 || len(report.Questions) > 0 {
		plan.Status = assistantstore.GoalPlanStatusBlocked
	} else {
		updateGoalPlanProgress(&plan, now)
	}
	if phase, _, ok := selectGoalPlanMilestone(&plan); ok {
		plan.CurrentPhaseID = phase.ID
	} else if plan.Status == assistantstore.GoalPlanStatusCompleted {
		plan.CurrentPhaseID = ""
	}
	plan.UpdatedAt = now
	goal.Plan = &plan
	if goal.Autopilot != nil {
		goal.Autopilot.CurrentPhaseID = plan.CurrentPhaseID
	}
}

func applyGoalChallengeReportToPlan(plan *assistantstore.GoalPlan, report assistantstore.GoalTaskReport, now time.Time) {
	if plan == nil || report.Challenge == nil {
		return
	}
	challenge := assistantstore.NormalizeGoalChallenge(*report.Challenge, len(plan.Challenges))
	if challenge.ID == "" || strings.HasPrefix(challenge.ID, "challenge_") {
		challenge.ID = id.New("gchallenge")
	}
	challenge.TaskID = firstNonEmptyString(challenge.TaskID, report.TaskID)
	challenge.MilestoneID = firstNonEmptyString(challenge.MilestoneID, report.MilestoneID)
	if challenge.CreatedAt.IsZero() {
		challenge.CreatedAt = now
	}
	plan.Challenges = appendGoalChallenge(plan.Challenges, challenge)
	for phaseIndex := range plan.Phases {
		for milestoneIndex := range plan.Phases[phaseIndex].Milestones {
			milestone := &plan.Phases[phaseIndex].Milestones[milestoneIndex]
			if milestone.ID != challenge.MilestoneID {
				continue
			}
			if !stringSliceContains(milestone.ChallengeTaskIDs, report.TaskID) {
				milestone.ChallengeTaskIDs = append(milestone.ChallengeTaskIDs, report.TaskID)
			}
			milestone.LatestChallengeID = challenge.ID
			milestone.Evidence = appendUniqueStrings(milestone.Evidence, challenge.Summary)
			milestone.Evidence = appendUniqueStrings(milestone.Evidence, challenge.Evidence...)
			switch challenge.Verdict {
			case assistantstore.GoalChallengeVerdictPassed:
				milestone.Status = assistantstore.GoalMilestoneStatusAccepted
			case assistantstore.GoalChallengeVerdictNeedsUser:
				milestone.Status = assistantstore.GoalMilestoneStatusBlocked
			default:
				milestone.Status = assistantstore.GoalMilestoneStatusChallenged
			}
			for _, gap := range challenge.Gaps {
				gap.PhaseID = firstNonEmptyString(gap.PhaseID, plan.Phases[phaseIndex].ID)
				gap.MilestoneID = firstNonEmptyString(gap.MilestoneID, milestone.ID)
				gap.Source = firstNonEmptyString(gap.Source, "challenge")
				gap.SourceTaskID = firstNonEmptyString(gap.SourceTaskID, report.TaskID)
				gap.Status = assistantstore.GoalGapStatusOpen
				if gap.CreatedAt.IsZero() {
					gap.CreatedAt = now
				}
				gap.UpdatedAt = now
				gap = assistantstore.NormalizeGoalGap(gap, len(plan.Gaps))
				if gap.ID == "" || strings.HasPrefix(gap.ID, "gap_") {
					gap.ID = id.New("ggap")
				}
				plan.Gaps = appendGoalGap(plan.Gaps, gap)
				milestone.GapIDs = appendUniqueStrings(milestone.GapIDs, gap.ID)
			}
			return
		}
	}
}

func updateGoalPlanProgress(plan *assistantstore.GoalPlan, now time.Time) {
	if plan == nil {
		return
	}
	allDone := len(plan.Phases) > 0
	anyBlocked := false
	for phaseIndex := range plan.Phases {
		phase := &plan.Phases[phaseIndex]
		if phase.Status == assistantstore.GoalPlanPhaseStatusSkipped {
			continue
		}
		if goalPhaseMilestonesAccepted(*phase) {
			phase.Status = assistantstore.GoalPlanPhaseStatusCompleted
			continue
		}
		allDone = false
		phaseHasWork := len(phase.TaskIDs) > 0 || len(phase.Evidence) > 0
		phaseBlocked := false
		for _, milestone := range phase.Milestones {
			if milestone.Status == assistantstore.GoalMilestoneStatusBlocked {
				phaseBlocked = true
			}
			if len(milestone.TaskIDs) > 0 || len(milestone.Claims) > 0 || len(milestone.Evidence) > 0 || milestone.Status != assistantstore.GoalMilestoneStatusPending {
				phaseHasWork = true
			}
		}
		if phaseBlocked {
			phase.Status = assistantstore.GoalPlanPhaseStatusBlocked
			anyBlocked = true
		} else if phaseHasWork {
			phase.Status = assistantstore.GoalPlanPhaseStatusInProgress
		} else {
			phase.Status = assistantstore.GoalPlanPhaseStatusPending
		}
	}
	if allDone && allGoalGapsClosed(plan) {
		plan.Status = assistantstore.GoalPlanStatusCompleted
		plan.CurrentPhaseID = ""
	} else if anyBlocked {
		plan.Status = assistantstore.GoalPlanStatusBlocked
	} else {
		plan.Status = assistantstore.GoalPlanStatusActive
	}
	if phase, _, ok := selectGoalPlanMilestone(plan); ok {
		plan.CurrentPhaseID = phase.ID
	}
	plan.UpdatedAt = now
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
	if report.TaskType == assistantstore.GoalTaskTypeChallenge {
		if report.Challenge == nil {
			return goalTaskProgressReview{
				Decision: goalTaskReviewInsufficientEvidence,
				Summary:  "Challenge task did not include a GOAL_CHALLENGE report, so the supervisor cannot accept or reject the milestone.",
				Evidence: nonEmptyStringSlice(firstNonEmptyString(report.Summary, "No GOAL_CHALLENGE report was found.")),
			}
		}
		challenge := assistantstore.NormalizeGoalChallenge(*report.Challenge, 0)
		evidence := append([]string{}, challenge.Evidence...)
		if challenge.Summary != "" {
			evidence = append(evidence, "Challenge summary: "+challenge.Summary)
		}
		if len(challenge.Gaps) > 0 {
			evidence = append(evidence, fmt.Sprintf("Challenge gaps: %d.", len(challenge.Gaps)))
		}
		switch challenge.Verdict {
		case assistantstore.GoalChallengeVerdictPassed:
			return goalTaskProgressReview{
				Decision: goalTaskReviewVerifiedProgress,
				Summary:  "Read-only challenge passed, so the milestone can be accepted.",
				Evidence: evidence,
			}
		case assistantstore.GoalChallengeVerdictNeedsUser:
			return goalTaskProgressReview{
				Decision: goalTaskReviewBlockedWithProgress,
				Summary:  "Read-only challenge found a question that needs operator input before the milestone can be accepted.",
				Evidence: evidence,
			}
		default:
			if len(challenge.Gaps) == 0 {
				return goalTaskProgressReview{
					Decision: goalTaskReviewNeedsValidation,
					Summary:  "Read-only challenge failed but did not provide concrete gaps for Autopilot to repair.",
					Evidence: evidence,
				}
			}
			return goalTaskProgressReview{
				Decision: goalTaskReviewVerifiedProgress,
				Summary:  "Read-only challenge found concrete gaps; the supervisor will create gap-fix work before moving on.",
				Evidence: evidence,
			}
		}
	}
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
