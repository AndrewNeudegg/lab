package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	assistantstore "github.com/andrewneudegg/lab/pkg/assistant"
	"github.com/andrewneudegg/lab/pkg/eventlog"
	"github.com/andrewneudegg/lab/pkg/id"
	"github.com/andrewneudegg/lab/pkg/llm"
	approvalstore "github.com/andrewneudegg/lab/pkg/tools/approval"
)

const assistantRunDecisionSchema = `{
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "decision": {"type": "string", "enum": ["no_op", "recommend", "created_tasks"]},
    "summary": {"type": "string"},
    "changed": {"type": "array", "items": {"type": "string"}, "maxItems": 6},
    "concerns": {
      "type": "array",
      "maxItems": 6,
      "items": {
        "type": "object",
        "additionalProperties": false,
        "properties": {
          "title": {"type": "string"},
          "detail": {"type": "string"},
          "severity": {"type": "string"},
          "surface": {"type": "string"},
          "object_id": {"type": "string"},
          "object_url": {"type": "string"}
        },
        "required": ["title"]
      }
    },
    "opportunities": {
      "type": "array",
      "maxItems": 6,
      "items": {
        "type": "object",
        "additionalProperties": false,
        "properties": {
          "title": {"type": "string"},
          "detail": {"type": "string"},
          "severity": {"type": "string"},
          "surface": {"type": "string"},
          "object_id": {"type": "string"},
          "object_url": {"type": "string"}
        },
        "required": ["title"]
      }
    },
    "recommended_actions": {
      "type": "array",
      "maxItems": 6,
      "items": {
        "type": "object",
        "additionalProperties": false,
        "properties": {
          "id": {"type": "string"},
          "kind": {"type": "string", "enum": ["task", "research", "workflow", "watch", "observe"]},
          "fingerprint": {"type": "string"},
          "title": {"type": "string"},
          "rationale": {"type": "string"},
          "priority": {"type": "string"},
          "risk": {"type": "string"},
          "target_surface": {"type": "string"},
          "task_goal": {"type": "string"},
          "knowledge_query": {"type": "string"},
          "workflow_hint": {"type": "string"},
          "status": {"type": "string"}
        },
        "required": ["kind", "title", "rationale"]
      }
    }
  },
  "required": ["decision", "summary", "changed", "concerns", "opportunities", "recommended_actions"]
}`

type assistantRunDecision struct {
	Decision           string                      `json:"decision"`
	Summary            string                      `json:"summary"`
	Changed            []string                    `json:"changed"`
	Concerns           []assistantstore.RunFinding `json:"concerns"`
	Opportunities      []assistantstore.RunFinding `json:"opportunities"`
	RecommendedActions []assistantstore.RunAction  `json:"recommended_actions"`
}

func (o *Orchestrator) assistantRunStore() (*assistantstore.RunStore, error) {
	if strings.TrimSpace(o.cfg.DataDir) == "" {
		return nil, fmt.Errorf("assistant run store is not configured")
	}
	return assistantstore.NewRunStore(filepath.Join(o.cfg.DataDir, "assistant_runs")), nil
}

func (o *Orchestrator) assistantSignalStore() (*assistantstore.SignalStore, error) {
	if strings.TrimSpace(o.cfg.DataDir) == "" {
		return nil, fmt.Errorf("assistant signal store is not configured")
	}
	return assistantstore.NewSignalStore(filepath.Join(o.cfg.DataDir, "assistant_signals")), nil
}

func (o *Orchestrator) ListAssistantRuns() ([]assistantstore.Run, error) {
	store, err := o.assistantRunStore()
	if err != nil {
		return nil, err
	}
	return store.List()
}

func (o *Orchestrator) LoadAssistantRun(runID string) (assistantstore.Run, error) {
	store, err := o.assistantRunStore()
	if err != nil {
		return assistantstore.Run{}, err
	}
	return store.Load(runID)
}

func (o *Orchestrator) StartAssistantRun(ctx context.Context, req assistantstore.RunRequest) (assistantstore.Run, string, error) {
	store, err := o.assistantRunStore()
	if err != nil {
		return assistantstore.Run{}, "", err
	}
	now := time.Now().UTC()
	req = normalizeAssistantRunRequest(req)
	run := assistantstore.Run{
		ID:        id.New("arun"),
		Status:    assistantstore.RunStatusRunning,
		Decision:  assistantstore.RunDecisionNoop,
		Trigger:   assistantstore.RunTrigger{Kind: req.TriggerKind, Label: req.TriggerLabel},
		Autonomy:  req.Autonomy,
		Goal:      req.Goal,
		CreatedAt: now,
		StartedAt: now,
		UpdatedAt: now,
	}
	run.Snapshot = o.assistantRunSnapshot(ctx, now)
	run.Receipts = append(run.Receipts, assistantstore.RunReceipt{
		Kind:      "trigger",
		Message:   "Assistant run started from " + run.Trigger.Label + ".",
		CreatedAt: now,
	})
	if err := store.Save(run); err != nil {
		return assistantstore.Run{}, "", err
	}
	o.appendAssistantRunEvent(ctx, "assistant.run.started", run, map[string]any{
		"trigger":  run.Trigger,
		"autonomy": run.Autonomy,
	})

	decision, response, err := o.evaluateAssistantRun(ctx, run)
	finished := time.Now().UTC()
	if err != nil {
		run.Status = assistantstore.RunStatusFailed
		run.Error = err.Error()
		run.Decision = assistantstore.RunDecisionNoop
		run.Summary = "Assistant run failed before it could produce a decision."
		run.FinishedAt = finished
		run.UpdatedAt = finished
		run.Receipts = append(run.Receipts, assistantstore.RunReceipt{Kind: "error", Message: err.Error(), CreatedAt: finished})
		_ = store.Save(run)
		o.appendAssistantRunEvent(ctx, "assistant.run.failed", run, map[string]any{"error": err.Error()})
		return run, "Assistant run failed: " + err.Error(), err
	}
	run.Status = assistantstore.RunStatusCompleted
	run.Decision = firstNonEmptyString(decision.Decision, assistantstore.RunDecisionNoop)
	run.Summary = decision.Summary
	run.Changed = decision.Changed
	run.Concerns = decision.Concerns
	run.Opportunities = decision.Opportunities
	run.RecommendedActions = decision.RecommendedActions
	run = assistantstore.NormalizeRun(run)
	o.applyAssistantSignalMemory(ctx, &run)
	suppressedActions := pruneAssistantSuppressedRunActions(&run)
	if suppressedActions > 0 {
		run.Receipts = append(run.Receipts, assistantstore.RunReceipt{
			Kind:      "signal_suppressed",
			Message:   fmt.Sprintf("Suppressed %d repeated recommendations using prior Assistant feedback.", suppressedActions),
			CreatedAt: time.Now().UTC(),
		})
	}
	o.applyAssistantRunActions(ctx, &run)
	run.Provider = response.Provider
	run.Model = response.Model
	run.Usage = assistantstore.RunUsage{
		InputTokens:  response.Usage.InputTokens,
		OutputTokens: response.Usage.OutputTokens,
		TotalTokens:  response.Usage.TotalTokens,
	}
	run.FinishedAt = finished
	run.UpdatedAt = finished
	run.Receipts = append(run.Receipts, assistantstore.RunReceipt{
		Kind:      "decision",
		Message:   assistantRunReceiptMessage(run),
		CreatedAt: finished,
	})
	if err := store.Save(run); err != nil {
		return assistantstore.Run{}, "", err
	}
	run, _ = store.Load(run.ID)
	o.appendAssistantRunEvent(ctx, "assistant.run.completed", run, map[string]any{
		"decision": run.Decision,
		"actions":  len(run.RecommendedActions),
		"concerns": len(run.Concerns),
	})
	return run, "Assistant run completed.", nil
}

func (o *Orchestrator) applyAssistantSignalMemory(ctx context.Context, run *assistantstore.Run) {
	if run == nil || len(run.RecommendedActions) == 0 {
		return
	}
	store, err := o.assistantSignalStore()
	if err != nil {
		o.log().Warn("assistant signal store unavailable", "error", err)
		return
	}
	now := time.Now().UTC()
	for index := range run.RecommendedActions {
		record, err := store.UpsertFromAction(run.ID, run.RecommendedActions[index], now)
		if err != nil {
			o.log().Warn("assistant signal update failed", "error", err, "action", run.RecommendedActions[index].ID)
			continue
		}
		applyAssistantSignalToAction(&run.RecommendedActions[index], record, now)
	}
}

func (o *Orchestrator) applyAssistantRunActions(ctx context.Context, run *assistantstore.Run) {
	if run == nil {
		return
	}
	for index := range run.RecommendedActions {
		action := &run.RecommendedActions[index]
		if action.Status == "" {
			action.Status = "recommended"
		}
	}
	if run.Autonomy != assistantstore.RunAutonomyCreateTasks {
		return
	}
	signalStore, _ := o.assistantSignalStore()
	createdCount := 0
	for index := range run.RecommendedActions {
		action := &run.RecommendedActions[index]
		if !strings.EqualFold(action.Kind, "task") {
			continue
		}
		if assistantActionSuppressesTaskCreation(*action, time.Now().UTC()) {
			continue
		}
		goal := strings.TrimSpace(action.TaskGoal)
		if goal == "" {
			goal = strings.TrimSpace(strings.Join([]string{action.Title, "", "Rationale: " + action.Rationale}, "\n"))
		}
		if goal == "" || strings.TrimSpace(action.Title) == "" {
			action.Status = "skipped"
			run.Receipts = append(run.Receipts, assistantstore.RunReceipt{
				Kind:      "task_skipped",
				Message:   "Skipped a task action because the goal or title was empty.",
				ObjectID:  action.ID,
				CreatedAt: time.Now().UTC(),
			})
			continue
		}
		created, err := o.createTaskRecord(ctx, goal)
		if err != nil {
			action.Status = "failed"
			run.Receipts = append(run.Receipts, assistantstore.RunReceipt{
				Kind:      "task_failed",
				Message:   "Failed to create task for " + action.Title + ": " + err.Error(),
				ObjectID:  action.ID,
				CreatedAt: time.Now().UTC(),
			})
			continue
		}
		if created.Task.ID == "" {
			action.Status = "skipped"
			continue
		}
		action.Status = assistantstore.SignalStatusCreatedTask
		action.CreatedTaskID = created.Task.ID
		createdCount++
		if signalStore != nil {
			o.saveAssistantCreatedTaskSignal(signalStore, run.ID, *action, created.Task.ID)
		}
		run.Receipts = append(run.Receipts, assistantstore.RunReceipt{
			Kind:      "task_created",
			Message:   "Created task for recommended action: " + action.Title + ".",
			ObjectID:  created.Task.ID,
			ObjectURL: dashboardTaskURL(created.Task.ID),
			CreatedAt: time.Now().UTC(),
		})
	}
	if createdCount > 0 {
		run.Decision = assistantstore.RunDecisionCreated
	}
}

func (o *Orchestrator) UpdateAssistantRunAction(ctx context.Context, runID, actionID string, req assistantstore.SignalFeedbackRequest) (assistantstore.Run, string, error) {
	runStore, err := o.assistantRunStore()
	if err != nil {
		return assistantstore.Run{}, "", err
	}
	signalStore, err := o.assistantSignalStore()
	if err != nil {
		return assistantstore.Run{}, "", err
	}
	run, err := runStore.Load(runID)
	if err != nil {
		return assistantstore.Run{}, "", err
	}
	index := -1
	for i := range run.RecommendedActions {
		if run.RecommendedActions[i].ID == actionID {
			index = i
			break
		}
	}
	if index < 0 {
		return assistantstore.Run{}, "", fmt.Errorf("assistant run action %q not found", actionID)
	}
	now := time.Now().UTC()
	action := &run.RecommendedActions[index]
	record, err := assistantSignalRecordForAction(signalStore, run.ID, *action, now)
	if err != nil {
		return assistantstore.Run{}, "", err
	}
	feedback := strings.ToLower(strings.TrimSpace(req.Feedback))
	reply := ""
	switch feedback {
	case assistantstore.SignalFeedbackUseful:
		record.Status = assistantstore.SignalStatusUseful
		record.UsefulCount++
		record.UpdatedAt = now
		reply = "Marked recommendation as useful."
	case assistantstore.SignalFeedbackDismiss:
		record.Status = assistantstore.SignalStatusDismissed
		record.DismissedAt = now
		record.SnoozedUntil = time.Time{}
		record.UpdatedAt = now
		reply = "Dismissed recommendation."
	case assistantstore.SignalFeedbackSnooze:
		seconds := req.SnoozeSeconds
		if seconds <= 0 {
			seconds = 24 * 60 * 60
		}
		record.Status = assistantstore.SignalStatusSnoozed
		record.SnoozedUntil = now.Add(time.Duration(seconds) * time.Second)
		record.UpdatedAt = now
		reply = "Snoozed recommendation."
	case assistantstore.SignalFeedbackCreateTask:
		taskID := record.CreatedTaskID
		if taskID == "" {
			taskID, err = o.createTaskFromAssistantAction(ctx, *action)
			if err != nil {
				return assistantstore.Run{}, "", err
			}
		}
		record.Status = assistantstore.SignalStatusCreatedTask
		record.CreatedTaskID = taskID
		record.UpdatedAt = now
		run.Decision = assistantstore.RunDecisionCreated
		reply = "Created task from recommendation."
	default:
		return assistantstore.Run{}, "", fmt.Errorf("unknown assistant action feedback %q", req.Feedback)
	}
	if err := signalStore.Save(record); err != nil {
		return assistantstore.Run{}, "", err
	}
	applyAssistantSignalToAction(action, record, now)
	run.Receipts = append(run.Receipts, assistantstore.RunReceipt{
		Kind:      "action_" + feedback,
		Message:   reply,
		ObjectID:  action.ID,
		ObjectURL: assistantActionReceiptURL(*action),
		CreatedAt: now,
	})
	run.UpdatedAt = now
	if err := runStore.Save(run); err != nil {
		return assistantstore.Run{}, "", err
	}
	run, _ = runStore.Load(run.ID)
	o.appendAssistantRunEvent(ctx, "assistant.action.feedback", run, map[string]any{
		"action_id":   action.ID,
		"fingerprint": action.Fingerprint,
		"feedback":    feedback,
		"status":      action.Status,
	})
	return run, reply, nil
}

func assistantSignalRecordForAction(store *assistantstore.SignalStore, runID string, action assistantstore.RunAction, now time.Time) (assistantstore.SignalRecord, error) {
	action = assistantstore.NormalizeRun(assistantstore.Run{RecommendedActions: []assistantstore.RunAction{action}}).RecommendedActions[0]
	record, err := store.Load(action.Fingerprint)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return assistantstore.SignalRecord{}, err
		}
		record = assistantstore.SignalRecord{
			Fingerprint:  action.Fingerprint,
			Status:       assistantstore.SignalStatusActive,
			Kind:         action.Kind,
			Title:        action.Title,
			Surface:      action.TargetSurface,
			FirstSeenAt:  now,
			LastSeenAt:   now,
			SeenCount:    assistantMaxInt(1, action.SeenCount),
			LastRunID:    runID,
			LastActionID: action.ID,
			UpdatedAt:    now,
		}
	}
	return assistantstore.NormalizeSignalRecord(record, now), nil
}

func applyAssistantSignalToAction(action *assistantstore.RunAction, record assistantstore.SignalRecord, now time.Time) {
	if action == nil {
		return
	}
	record = assistantstore.NormalizeSignalRecord(record, now)
	action.Fingerprint = record.Fingerprint
	action.SeenCount = record.SeenCount
	action.UsefulCount = record.UsefulCount
	action.SnoozedUntil = record.SnoozedUntil
	if record.CreatedTaskID != "" {
		action.CreatedTaskID = record.CreatedTaskID
		action.Status = assistantstore.SignalStatusCreatedTask
		return
	}
	switch record.Status {
	case assistantstore.SignalStatusDismissed, assistantstore.SignalStatusSnoozed, assistantstore.SignalStatusUseful:
		action.Status = record.Status
	case assistantstore.SignalStatusActive:
		if action.Status == "" || action.Status == assistantstore.SignalStatusActive {
			action.Status = "recommended"
		}
	}
}

func assistantActionSuppressesTaskCreation(action assistantstore.RunAction, now time.Time) bool {
	switch action.Status {
	case assistantstore.SignalStatusDismissed, assistantstore.SignalStatusCreatedTask:
		return true
	case assistantstore.SignalStatusSnoozed:
		return action.SnoozedUntil.IsZero() || action.SnoozedUntil.After(now)
	default:
		return strings.TrimSpace(action.CreatedTaskID) != ""
	}
}

func (o *Orchestrator) createTaskFromAssistantAction(ctx context.Context, action assistantstore.RunAction) (string, error) {
	if strings.TrimSpace(action.CreatedTaskID) != "" {
		return action.CreatedTaskID, nil
	}
	goal := strings.TrimSpace(action.TaskGoal)
	if goal == "" {
		goal = strings.TrimSpace(strings.Join([]string{action.Title, "", "Rationale: " + action.Rationale}, "\n"))
	}
	if goal == "" {
		return "", fmt.Errorf("assistant recommendation has no task goal")
	}
	created, err := o.createTaskRecord(ctx, goal)
	if err != nil {
		return "", err
	}
	if created.Task.ID == "" {
		return "", fmt.Errorf("task was not created")
	}
	return created.Task.ID, nil
}

func (o *Orchestrator) saveAssistantCreatedTaskSignal(store *assistantstore.SignalStore, runID string, action assistantstore.RunAction, taskID string) {
	now := time.Now().UTC()
	record, err := assistantSignalRecordForAction(store, runID, action, now)
	if err != nil {
		o.log().Warn("assistant created task signal load failed", "error", err)
		return
	}
	record.Status = assistantstore.SignalStatusCreatedTask
	record.CreatedTaskID = taskID
	record.UpdatedAt = now
	if err := store.Save(record); err != nil {
		o.log().Warn("assistant created task signal save failed", "error", err)
	}
}

func assistantActionReceiptURL(action assistantstore.RunAction) string {
	if strings.TrimSpace(action.CreatedTaskID) != "" {
		return dashboardTaskURL(action.CreatedTaskID)
	}
	return ""
}

func assistantMaxInt(left, right int) int {
	if left > right {
		return left
	}
	return right
}

func (o *Orchestrator) StartAssistantProactiveLoop(ctx context.Context) {
	if !o.cfg.Assistant.ProactiveEnabled {
		return
	}
	interval := time.Duration(o.cfg.Assistant.ProactiveIntervalSeconds) * time.Second
	if interval <= 0 {
		interval = time.Hour
	}
	autonomy := o.cfg.Assistant.ProactiveAutonomy
	eventWatchEnabled := o.cfg.Assistant.ProactiveEventWatchEnabled == nil || *o.cfg.Assistant.ProactiveEventWatchEnabled
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				_, _, err := o.StartAssistantRun(ctx, assistantstore.RunRequest{
					TriggerKind:  "schedule",
					TriggerLabel: "Scheduled proactive check",
					Goal:         "Review current homelabd state and recommend useful next actions.",
					Autonomy:     autonomy,
				})
				if err != nil {
					o.log().Warn("scheduled assistant run failed", "error", err)
				}
			}
		}
	}()
	if eventWatchEnabled {
		o.startAssistantEventWatchLoop(ctx, autonomy)
	}
}

func (o *Orchestrator) startAssistantEventWatchLoop(ctx context.Context, autonomy string) {
	if o.events == nil {
		return
	}
	poll := time.Duration(o.cfg.Assistant.ProactiveEventPollSeconds) * time.Second
	if poll <= 0 {
		poll = 15 * time.Second
	}
	cooldown := time.Duration(o.cfg.Assistant.ProactiveEventCooldownSeconds) * time.Second
	if cooldown <= 0 {
		cooldown = 5 * time.Minute
	}
	go func() {
		ticker := time.NewTicker(poll)
		defer ticker.Stop()
		lastSeen := time.Now().UTC()
		lastTriggered := time.Time{}
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-ticker.C:
				events := o.assistantEventsAfter(lastSeen, now.UTC())
				if len(events) == 0 {
					lastSeen = now.UTC()
					continue
				}
				lastSeen = latestAssistantEventTime(events, now.UTC())
				trigger, ok := assistantTriggerFromEvents(events)
				if !ok || (!lastTriggered.IsZero() && now.UTC().Sub(lastTriggered) < cooldown) {
					continue
				}
				lastTriggered = now.UTC()
				_, _, err := o.StartAssistantRun(ctx, assistantstore.RunRequest{
					TriggerKind:  "event",
					TriggerLabel: trigger.Label,
					Goal:         trigger.Goal,
					Autonomy:     autonomy,
				})
				if err != nil {
					o.log().Warn("event-triggered assistant run failed", "error", err, "trigger", trigger.Label)
				}
			}
		}
	}()
}

type assistantEventTrigger struct {
	Label string
	Goal  string
}

func (o *Orchestrator) assistantEventsAfter(after, now time.Time) []eventlog.Event {
	if o.events == nil {
		return nil
	}
	after = after.UTC()
	now = now.UTC()
	days := []time.Time{now}
	if after.Format("2006-01-02") != now.Format("2006-01-02") {
		days = append(days, after)
	}
	seen := map[string]bool{}
	var out []eventlog.Event
	for _, day := range days {
		events, err := o.events.ReadDay(day)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			o.log().Warn("assistant event watch read failed", "error", err)
			continue
		}
		for _, event := range events {
			if event.Time.After(after) && !event.Time.After(now) && !seen[event.ID] {
				out = append(out, event)
				seen[event.ID] = true
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Time.Before(out[j].Time) })
	return out
}

func latestAssistantEventTime(events []eventlog.Event, fallback time.Time) time.Time {
	latest := fallback
	for _, event := range events {
		if event.Time.After(latest) {
			latest = event.Time
		}
	}
	return latest
}

func assistantTriggerFromEvents(events []eventlog.Event) (assistantEventTrigger, bool) {
	for index := len(events) - 1; index >= 0; index-- {
		event := events[index]
		label, ok := assistantEventTriggerLabel(event)
		if !ok {
			continue
		}
		return assistantEventTrigger{
			Label: label,
			Goal:  assistantEventTriggerGoal(event),
		}, true
	}
	return assistantEventTrigger{}, false
}

func assistantEventTriggerLabel(event eventlog.Event) (string, bool) {
	switch event.Type {
	case "task.blocked":
		return assistantEventLabel("Task blocked", event), true
	case "task.conflict_resolution":
		return assistantEventLabel("Task needs conflict resolution", event), true
	case "task.review.failed":
		return assistantEventLabel("Task review failed", event), true
	case "task.restart.failed":
		return assistantEventLabel("Task restart failed", event), true
	case "task.auto_recovery.exhausted":
		return assistantEventLabel("Task automatic recovery exhausted", event), true
	case "task.awaiting_approval":
		return assistantEventLabel("Task awaiting approval", event), true
	case "task.awaiting_restart":
		return assistantEventLabel("Task awaiting restart", event), true
	case "task.awaiting_verification":
		return assistantEventLabel("Task awaiting verification", event), true
	case "approval.requested":
		return assistantEventLabel("Approval requested", event), true
	case "approval.failed":
		return assistantEventLabel("Approval failed", event), true
	default:
		return "", false
	}
}

func assistantEventLabel(prefix string, event eventlog.Event) string {
	if strings.TrimSpace(event.TaskID) == "" {
		return prefix
	}
	return prefix + ": " + taskShortID(event.TaskID)
}

func assistantEventTriggerGoal(event eventlog.Event) string {
	return strings.Join([]string{
		"Review current homelabd state after " + event.Type + ".",
		"Decide whether the event requires a task, research, workflow, or no action.",
		"Use the current Tasks, Knowledge, Workflows, health, supervisor, and recent event snapshot.",
	}, " ")
}

func normalizeAssistantRunRequest(req assistantstore.RunRequest) assistantstore.RunRequest {
	req.TriggerKind = strings.ToLower(strings.TrimSpace(req.TriggerKind))
	if req.TriggerKind == "" {
		req.TriggerKind = "manual"
	}
	req.TriggerLabel = strings.TrimSpace(req.TriggerLabel)
	if req.TriggerLabel == "" {
		req.TriggerLabel = "Manual proactive check"
	}
	req.Goal = strings.TrimSpace(req.Goal)
	if req.Goal == "" {
		req.Goal = "Review current homelabd state and recommend useful next actions."
	}
	req.Autonomy = strings.TrimSpace(req.Autonomy)
	if req.Autonomy == "" {
		req.Autonomy = assistantstore.RunAutonomyPropose
	}
	return req
}

func (o *Orchestrator) evaluateAssistantRun(ctx context.Context, run assistantstore.Run) (assistantRunDecision, llm.CompletionResponse, error) {
	if o.provider == nil {
		return assistantRunDecisionWithSignals(run, fallbackAssistantRunDecision(run)), llm.CompletionResponse{}, nil
	}
	resp, err := o.provider.Complete(ctx, llm.CompletionRequest{
		Model:       o.model,
		Temperature: 0.1,
		MaxTokens:   1800,
		ResponseFormat: &llm.ResponseFormat{
			Name:        "assistant_proactive_run",
			Description: "Decision output from a bounded proactive assistant run.",
			Schema:      json.RawMessage(assistantRunDecisionSchema),
			Strict:      true,
		},
		Messages: []llm.Message{
			{
				Role: "system",
				Content: strings.Join([]string{
					"You are homelabd's proactive executive layer.",
					"Use the harness: Tasks are for changing things, Knowledge is for durable research and memory, and Workflows are for repeatable thinking.",
					"Treat snapshot.signals as the pre-scored watchlist. Each signal can include why_now, evidence, safe_actions, and suggested_next_step so any source can plug in without source-specific reasoning.",
					"Prefer high-score, high-confidence signals, ground recommendations in their evidence, use only safe_actions, and do not recommend suppressed signals.",
					"Do not claim to have executed actions unless the snapshot already proves they happened.",
					"Prefer no-op when there is no actionable signal. Prefer recommendations over mutation.",
					"Return exactly one JSON object matching the schema.",
				}, "\n"),
			},
			{
				Role: "user",
				Content: "Assistant run request and compact state snapshot:\n" + mustJSON(map[string]any{
					"trigger":  run.Trigger,
					"autonomy": run.Autonomy,
					"goal":     run.Goal,
					"snapshot": run.Snapshot,
				}),
			},
		},
	})
	if err != nil {
		return assistantRunDecision{}, resp, err
	}
	var decision assistantRunDecision
	if err := json.Unmarshal([]byte(extractJSON(resp.Message.Content)), &decision); err != nil {
		return assistantRunDecision{}, resp, fmt.Errorf("assistant run returned invalid JSON: %w", err)
	}
	return assistantRunDecisionWithSignals(run, decision), resp, nil
}

func normalizeAssistantRunDecision(decision assistantRunDecision) assistantRunDecision {
	decision.Decision = strings.TrimSpace(decision.Decision)
	if decision.Decision == "" {
		if len(decision.Concerns) > 0 || len(decision.Opportunities) > 0 || len(decision.RecommendedActions) > 0 {
			decision.Decision = assistantstore.RunDecisionRecommend
		} else {
			decision.Decision = assistantstore.RunDecisionNoop
		}
	}
	decision.Summary = strings.TrimSpace(decision.Summary)
	if decision.Summary == "" {
		decision.Summary = "No assistant summary was provided."
	}
	return decision
}

func fallbackAssistantRunDecision(run assistantstore.Run) assistantRunDecision {
	decision := assistantRunDecision{
		Decision: assistantstore.RunDecisionNoop,
		Summary:  "No urgent action found in the current homelabd state.",
		Changed:  []string{"Fallback deterministic scan used because no language model provider is configured."},
	}
	if len(run.Snapshot.Signals) > 0 {
		return decision
	}
	for _, task := range run.Snapshot.AttentionTasks {
		decision.Concerns = append(decision.Concerns, assistantstore.RunFinding{
			Title:     "Task needs attention: " + task.Title,
			Detail:    task.Summary,
			Severity:  "warning",
			Surface:   "tasks",
			ObjectID:  task.ID,
			ObjectURL: task.URL,
		})
	}
	if run.Snapshot.PendingApprovals > 0 {
		decision.Concerns = append(decision.Concerns, assistantstore.RunFinding{
			Title:    "Pending approvals need review",
			Detail:   fmt.Sprintf("%d approvals are waiting for an operator decision.", run.Snapshot.PendingApprovals),
			Severity: "warning",
			Surface:  "tasks",
		})
	}
	if run.Snapshot.Health.Status == "critical" || run.Snapshot.Health.Status == "warning" {
		decision.Concerns = append(decision.Concerns, assistantstore.RunFinding{
			Title:    "Health needs review",
			Detail:   "healthd reported " + run.Snapshot.Health.Status + ".",
			Severity: run.Snapshot.Health.Status,
			Surface:  "health",
		})
	}
	if len(decision.Concerns) > 0 {
		decision.Decision = assistantstore.RunDecisionRecommend
		decision.Summary = "Current homelabd state has actionable items to review."
		decision.RecommendedActions = append(decision.RecommendedActions, assistantstore.RunAction{
			ID:            "action_1",
			Kind:          "task",
			Title:         "Review proactive assistant findings",
			Rationale:     "The deterministic scan found tasks, approvals, or health state that may need operator action.",
			Priority:      "medium",
			Risk:          "low",
			TargetSurface: "tasks",
			TaskGoal:      assistantTaskGoalFromFindings(decision.Concerns),
			Status:        "recommended",
		})
	}
	return decision
}

func assistantTaskGoalFromFindings(findings []assistantstore.RunFinding) string {
	var lines []string
	lines = append(lines, "Review proactive assistant findings and decide whether follow-up work is needed.")
	for _, finding := range findings {
		lines = append(lines, "- "+finding.Title+": "+finding.Detail)
	}
	return strings.Join(lines, "\n")
}

func (o *Orchestrator) assistantRunSnapshot(ctx context.Context, now time.Time) assistantstore.RunSnapshot {
	snapshot := assistantstore.RunSnapshot{
		GeneratedAt:       now,
		TaskCounts:        map[string]int{},
		WorkflowCounts:    map[string]int{},
		RemoteAgentCounts: map[string]int{},
	}
	if tasks, err := o.ListTasks(); err == nil {
		for _, task := range tasks {
			snapshot.TaskCounts[task.Status]++
			if assistantTaskNeedsAttention(task.Status) && len(snapshot.AttentionTasks) < 8 {
				snapshot.AttentionTasks = append(snapshot.AttentionTasks, assistantstore.RunObjectRef{
					ID:      task.ID,
					Title:   friendlyTaskTitle(task),
					Status:  task.Status,
					Summary: strings.TrimSpace(task.Result),
					URL:     dashboardTaskURL(task.ID),
				})
			}
		}
	}
	if o.approvals != nil {
		if approvals, err := o.approvals.List(); err == nil {
			for _, approval := range approvals {
				if approval.Status == approvalstore.StatusPending {
					snapshot.PendingApprovals++
				}
			}
		}
	}
	if workflows, err := o.ListWorkflows(); err == nil {
		for _, workflow := range workflows {
			snapshot.WorkflowCounts[workflow.Status]++
			if len(snapshot.RecentWorkflows) < 5 {
				snapshot.RecentWorkflows = append(snapshot.RecentWorkflows, assistantstore.RunObjectRef{
					ID:      workflow.ID,
					Title:   workflow.Name,
					Status:  workflow.Status,
					Summary: workflow.Description,
					URL:     "/workflows?workflow=" + workflow.ID,
				})
			}
		}
	}
	if spaces, err := o.ListKnowledgeSpaces(); err == nil {
		for _, space := range spaces {
			if len(snapshot.KnowledgeSpaces) >= 5 {
				break
			}
			snapshot.KnowledgeSpaces = append(snapshot.KnowledgeSpaces, assistantstore.RunObjectRef{
				ID:      space.ID,
				Title:   space.Title,
				Summary: fmt.Sprintf("%d sources, %d reports", len(space.Sources), len(space.Reports)),
				URL:     "/knowledge?space=" + space.ID,
			})
		}
	}
	if o.remoteAgents != nil {
		staleAfter := time.Duration(o.cfg.ControlPlane.AgentStaleSeconds) * time.Second
		if staleAfter <= 0 {
			staleAfter = 30 * time.Second
		}
		if agents, err := o.remoteAgents.List(staleAfter, now); err == nil {
			for _, agent := range agents {
				snapshot.RemoteAgentCounts[agent.Status]++
			}
		}
	}
	snapshot.Health = o.assistantHealthSnapshot(ctx)
	snapshot.Supervisor = o.assistantSupervisorSnapshot(ctx)
	snapshot.RecentEvents = o.assistantRecentEvents(now, 12)
	snapshot.Signals = o.assistantWatchlistSignals(snapshot, now)
	return snapshot
}

func assistantTaskNeedsAttention(status string) bool {
	switch status {
	case "blocked", "failed", "conflict_resolution", "ready_for_review", "awaiting_approval", "awaiting_restart", "awaiting_verification", "no_change_required":
		return true
	default:
		return false
	}
}

func (o *Orchestrator) assistantRecentEvents(now time.Time, limit int) []assistantstore.RunEventRef {
	if o.events == nil || limit <= 0 {
		return nil
	}
	events, err := o.events.ReadDay(now.UTC())
	if err != nil {
		return nil
	}
	if len(events) > limit {
		events = events[len(events)-limit:]
	}
	out := make([]assistantstore.RunEventRef, 0, len(events))
	for _, event := range events {
		out = append(out, assistantstore.RunEventRef{
			ID:      event.ID,
			Type:    event.Type,
			Actor:   event.Actor,
			TaskID:  event.TaskID,
			Summary: truncateAssistantRunText(string(event.Payload), 240),
			Time:    event.Time,
		})
	}
	return out
}

func truncateAssistantRunText(value string, max int) string {
	value = strings.TrimSpace(value)
	if max <= 0 || len(value) <= max {
		return value
	}
	return value[:max] + "...[truncated]"
}

func (o *Orchestrator) assistantHealthSnapshot(ctx context.Context) assistantstore.RunSystemSnapshot {
	var raw struct {
		Status string `json:"status"`
		Checks []struct {
			Name    string `json:"name"`
			Status  string `json:"status"`
			Message string `json:"message"`
		} `json:"checks"`
		Processes []struct {
			Name   string `json:"name"`
			Status string `json:"status"`
			Type   string `json:"type"`
		} `json:"processes"`
	}
	if err := assistantFetchJSON(ctx, o.cfg.Healthd.Addr, "/healthd", &raw); err != nil {
		return assistantstore.RunSystemSnapshot{Error: err.Error()}
	}
	out := assistantstore.RunSystemSnapshot{Status: strings.TrimSpace(raw.Status)}
	for _, check := range raw.Checks {
		if check.Status == "healthy" && len(out.Items) >= 5 {
			continue
		}
		out.Items = append(out.Items, assistantstore.RunObjectRef{Title: check.Name, Status: check.Status, Summary: check.Message, URL: "/healthd"})
		if len(out.Items) >= 8 {
			break
		}
	}
	for _, process := range raw.Processes {
		if process.Status == "healthy" || len(out.Items) >= 8 {
			continue
		}
		out.Items = append(out.Items, assistantstore.RunObjectRef{Title: process.Name, Status: process.Status, Summary: process.Type, URL: "/healthd"})
	}
	return out
}

func (o *Orchestrator) assistantSupervisorSnapshot(ctx context.Context) assistantstore.RunSystemSnapshot {
	var raw struct {
		Status string `json:"status"`
		Apps   []struct {
			Name    string `json:"name"`
			State   string `json:"state"`
			Desired string `json:"desired"`
			Message string `json:"message"`
		} `json:"apps"`
	}
	if err := assistantFetchJSON(ctx, o.cfg.Supervisord.Addr, "/supervisord", &raw); err != nil {
		return assistantstore.RunSystemSnapshot{Error: err.Error()}
	}
	out := assistantstore.RunSystemSnapshot{Status: strings.TrimSpace(raw.Status)}
	for _, app := range raw.Apps {
		if app.State == "running" && app.Desired == "running" && len(out.Items) >= 5 {
			continue
		}
		out.Items = append(out.Items, assistantstore.RunObjectRef{
			ID:      app.Name,
			Title:   app.Name,
			Status:  app.State,
			Summary: assistantSupervisorAppSummary(app.Desired, app.Message),
			URL:     "/supervisord",
		})
		if len(out.Items) >= 8 {
			break
		}
	}
	return out
}

func assistantSupervisorAppSummary(desired, message string) string {
	desired = strings.TrimSpace(desired)
	message = strings.TrimSpace(message)
	if desired == "" {
		return message
	}
	if message == "" {
		return "desired " + desired
	}
	return "desired " + desired + " / " + message
}

func assistantFetchJSON(ctx context.Context, addr, path string, target any) error {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return fmt.Errorf("service address is not configured")
	}
	timeoutCtx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(timeoutCtx, http.MethodGet, assistantHTTPBase(addr)+path, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("service returned %s", resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(target)
}

func assistantHTTPBase(addr string) string {
	if strings.HasPrefix(addr, "http://") || strings.HasPrefix(addr, "https://") {
		return strings.TrimRight(addr, "/")
	}
	return "http://" + strings.TrimRight(addr, "/")
}

func assistantRunReceiptMessage(run assistantstore.Run) string {
	if len(run.RecommendedActions) == 0 && len(run.Concerns) == 0 && len(run.Opportunities) == 0 {
		return "No follow-up action was recommended."
	}
	return fmt.Sprintf("Recommended %d actions from %d concerns and %d opportunities.", len(run.RecommendedActions), len(run.Concerns), len(run.Opportunities))
}

func (o *Orchestrator) appendAssistantRunEvent(ctx context.Context, eventType string, run assistantstore.Run, payload map[string]any) {
	if o.events == nil {
		return
	}
	if payload == nil {
		payload = map[string]any{}
	}
	payload["run_id"] = run.ID
	_ = o.events.Append(ctx, eventlog.Event{
		ID:      id.New("evt"),
		Type:    eventType,
		Actor:   "Assistant",
		Payload: eventlog.Payload(payload),
	})
}

func sortedAssistantCountPairs(values map[string]int) []assistantstore.RunObjectRef {
	out := make([]assistantstore.RunObjectRef, 0, len(values))
	for key, value := range values {
		out = append(out, assistantstore.RunObjectRef{Title: key, Summary: fmt.Sprintf("%d", value)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Title < out[j].Title })
	return out
}
