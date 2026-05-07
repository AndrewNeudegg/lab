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

func (o *Orchestrator) handleGoalCommand(ctx context.Context, fields []string, message string) (string, error) {
	if len(fields) == 0 {
		return "usage: goal <objective>|list|show <goal_id>|check <goal_id>|pause <goal_id>|archive <goal_id>", nil
	}
	if commandWord(fields[0]) == "goals" {
		if len(fields) == 1 || commandWord(fields[1]) == "list" {
			return o.formatGoalsReply()
		}
		fields = append([]string{"goal"}, fields[1:]...)
	}
	if len(fields) == 1 {
		return "usage: goal <objective>|list|show <goal_id>|check <goal_id>|pause <goal_id>|archive <goal_id>", nil
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
	return fmt.Sprintf("Created Goal `%s`: %s\nNext: `goal check %s` or let proactive mode review it at %s.", goal.ID, goal.Title, goal.ID, goalNextCheckLabel(goal)), nil
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
		fmt.Fprintf(&b, "\n- `%s` %s [%s]", goal.ID, goal.Title, goal.Status)
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
	fmt.Fprintf(&b, "Goal `%s`: %s\nStatus: %s\nObjective: %s", goal.ID, goal.Title, goal.Status, goal.Objective)
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
			case "list", "ls", "show", "get", "check", "pause", "archive", "new", "create", "add":
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
	return o.createTaskRecordWithOptions(ctx, taskGoalWithGoalContext(taskGoal, goal), nil, taskCreateOptions{GoalID: goal.ID})
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
	GoalID string
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
