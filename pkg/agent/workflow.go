package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/andrewneudegg/lab/pkg/eventlog"
	"github.com/andrewneudegg/lab/pkg/id"
	"github.com/andrewneudegg/lab/pkg/llm"
	workflowstore "github.com/andrewneudegg/lab/pkg/workflow"
)

func (o *Orchestrator) WithWorkflows(store *workflowstore.Store) *Orchestrator {
	o.workflows = store
	return o
}

func (o *Orchestrator) workflowStore() (*workflowstore.Store, error) {
	if o.workflows != nil {
		return o.workflows, nil
	}
	if strings.TrimSpace(o.cfg.DataDir) == "" {
		return nil, errors.New("workflow store is not configured")
	}
	o.workflows = workflowstore.NewStore(filepath.Join(o.cfg.DataDir, "workflows"))
	return o.workflows, nil
}

func (o *Orchestrator) CreateWorkflow(ctx context.Context, req workflowstore.CreateRequest) (workflowstore.Workflow, string, error) {
	store, err := o.workflowStore()
	if err != nil {
		return workflowstore.Workflow{}, "", err
	}
	if req.CreatedBy == "" {
		req.CreatedBy = "OrchestratorAgent"
	}
	item, err := workflowstore.New(req, id.New("workflow"), time.Now().UTC())
	if err != nil {
		return workflowstore.Workflow{}, "", err
	}
	if err := store.Save(item); err != nil {
		return workflowstore.Workflow{}, "", err
	}
	item, _ = store.Load(item.ID)
	o.appendWorkflowEvent(ctx, "workflow.created", item, map[string]any{"name": item.Name, "estimate": item.Estimate})
	return item, formatWorkflowCreated(item), nil
}

func (o *Orchestrator) ListWorkflows() ([]workflowstore.Workflow, error) {
	store, err := o.workflowStore()
	if err != nil {
		return nil, err
	}
	items, err := store.List()
	if err != nil {
		return nil, err
	}
	sort.Slice(items, func(i, j int) bool { return items[i].UpdatedAt.After(items[j].UpdatedAt) })
	return items, nil
}

func (o *Orchestrator) LoadWorkflow(workflowID string) (workflowstore.Workflow, error) {
	store, err := o.workflowStore()
	if err != nil {
		return workflowstore.Workflow{}, err
	}
	return store.Load(workflowID)
}

func (o *Orchestrator) RunWorkflow(ctx context.Context, selector string) (workflowstore.Workflow, string, error) {
	return o.runWorkflow(ctx, selector, map[string]bool{})
}

func (o *Orchestrator) runWorkflow(ctx context.Context, selector string, seen map[string]bool) (workflowstore.Workflow, string, error) {
	store, err := o.workflowStore()
	if err != nil {
		return workflowstore.Workflow{}, "", err
	}
	workflowID, err := o.resolveWorkflowID(selector)
	if err != nil {
		return workflowstore.Workflow{}, "", err
	}
	if seen[workflowID] {
		return workflowstore.Workflow{}, "", fmt.Errorf("workflow %s chains to itself", workflowShortID(workflowID))
	}
	seen[workflowID] = true
	defer delete(seen, workflowID)
	item, err := store.Load(workflowID)
	if err != nil {
		return workflowstore.Workflow{}, "", err
	}

	run := workflowstore.Run{
		ID:         id.New("wfrun"),
		WorkflowID: item.ID,
		Status:     workflowstore.StatusRunning,
		StartedAt:  time.Now().UTC(),
	}
	startIndex := 0
	var resumedStep *workflowstore.StepOutput
	eventType := "workflow.run.started"
	if item.LastRun != nil &&
		item.LastRun.Status == workflowstore.StatusWaiting &&
		item.LastRun.CurrentStep > 0 &&
		item.LastRun.CurrentStep <= len(item.Steps) {
		run = *item.LastRun
		run.Status = workflowstore.StatusRunning
		run.Error = ""
		run.FinishedAt = nil
		startIndex = run.CurrentStep - 1
		if len(run.Outputs) > 0 {
			last := run.Outputs[len(run.Outputs)-1]
			resumedStep = &last
			run.Outputs = append([]workflowstore.StepOutput(nil), run.Outputs[:len(run.Outputs)-1]...)
		}
		eventType = "workflow.run.resumed"
	}
	item.Status = workflowstore.StatusRunning
	item.LastRun = &run
	if err := store.Save(item); err != nil {
		return workflowstore.Workflow{}, "", err
	}
	o.appendWorkflowEvent(ctx, eventType, item, map[string]any{"run_id": run.ID, "current_step": run.CurrentStep})

	for index := startIndex; index < len(item.Steps); index++ {
		step := item.Steps[index]
		run.CurrentStep = index + 1
		var previousAttempt *workflowstore.StepOutput
		if resumedStep != nil && index == startIndex && resumedStep.StepID == step.ID {
			previousAttempt = resumedStep
		}
		output := o.runWorkflowStep(ctx, item, step, run.Outputs, seen, previousAttempt)
		run.Outputs = append(run.Outputs, output)
		item.LastRun = &run
		switch output.Status {
		case workflowstore.StatusCompleted:
			continue
		case workflowstore.StatusWaiting, workflowstore.StatusAwaitingApproval:
			run.Status = output.Status
			item.Status = output.Status
			if err := store.Save(item); err != nil {
				return workflowstore.Workflow{}, "", err
			}
			item, _ = store.Load(item.ID)
			o.appendWorkflowEvent(ctx, "workflow.run.paused", item, map[string]any{"run_id": run.ID, "step_id": output.StepID, "status": output.Status, "summary": output.Summary})
			return item, formatWorkflowRunResult(item), nil
		default:
			finished := time.Now().UTC()
			run.Status = workflowstore.StatusFailed
			run.FinishedAt = &finished
			run.Error = output.Error
			item.Status = workflowstore.StatusFailed
			item.LastRun = &run
			if err := store.Save(item); err != nil {
				return workflowstore.Workflow{}, "", err
			}
			item, _ = store.Load(item.ID)
			o.appendWorkflowEvent(ctx, "workflow.run.failed", item, map[string]any{"run_id": run.ID, "step_id": output.StepID, "error": output.Error})
			return item, formatWorkflowRunResult(item), nil
		}
	}
	finished := time.Now().UTC()
	run.Status = workflowstore.StatusCompleted
	run.FinishedAt = &finished
	item.Status = workflowstore.StatusCompleted
	item.LastRun = &run
	if err := store.Save(item); err != nil {
		return workflowstore.Workflow{}, "", err
	}
	item, _ = store.Load(item.ID)
	o.appendWorkflowEvent(ctx, "workflow.run.completed", item, map[string]any{"run_id": run.ID, "steps": len(run.Outputs)})
	return item, formatWorkflowRunResult(item), nil
}

func (o *Orchestrator) runWorkflowStep(ctx context.Context, item workflowstore.Workflow, step workflowstore.Step, previous []workflowstore.StepOutput, seen map[string]bool, previousAttempt *workflowstore.StepOutput) workflowstore.StepOutput {
	started := time.Now().UTC()
	if previousAttempt != nil && !previousAttempt.StartedAt.IsZero() {
		started = previousAttempt.StartedAt
	}
	output := workflowstore.StepOutput{
		StepID:    step.ID,
		StepName:  step.Name,
		Kind:      step.Kind,
		Status:    workflowstore.StatusRunning,
		StartedAt: started,
	}
	finish := func(status, summary, errText string, result json.RawMessage) workflowstore.StepOutput {
		now := time.Now().UTC()
		output.Status = status
		output.Summary = summary
		output.Error = errText
		output.Result = result
		if status != workflowstore.StatusWaiting && status != workflowstore.StatusAwaitingApproval {
			output.FinishedAt = &now
		}
		return output
	}
	switch step.Kind {
	case workflowstore.StepKindLLM:
		if o.provider == nil || o.model == "" {
			return finish(workflowstore.StatusFailed, "", "no LLM provider configured", nil)
		}
		resp, err := o.provider.Complete(ctx, llm.CompletionRequest{
			Model:       o.model,
			Temperature: 0,
			MaxTokens:   1024,
			Messages: []llm.Message{{
				Role: "system",
				Content: strings.Join([]string{
					"You are executing one durable homelabd workflow step.",
					"Return concise plain text for this step output. Do not invent tool results.",
					diagramGuidancePrompt(),
				}, "\n"),
			}, {
				Role:    "user",
				Content: workflowStepPrompt(item, step, previous),
			}},
		})
		if err != nil {
			return finish(workflowstore.StatusFailed, "", err.Error(), nil)
		}
		result := eventlog.Payload(map[string]any{"message": resp.Message.Content, "provider": responseSource(resp, o.provider.Name()), "usage": resp.Usage})
		return finish(workflowstore.StatusCompleted, strings.TrimSpace(resp.Message.Content), "", result)
	case workflowstore.StepKindTool:
		output.Tool = step.Tool
		result := o.executeProposedTool(ctx, "OrchestratorAgent", proposedToolCall{Tool: step.Tool, Args: step.Args}, "")
		raw := eventlog.Payload(result)
		if result.NeedsApproval {
			summary := "Approval required"
			if result.ApprovalID != "" {
				summary += ": " + result.ApprovalID
			}
			return finish(workflowstore.StatusAwaitingApproval, summary, result.Reason, raw)
		}
		if result.Error != "" {
			return finish(workflowstore.StatusFailed, "", result.Error, raw)
		}
		return finish(workflowstore.StatusCompleted, "Tool completed: "+step.Tool, "", raw)
	case workflowstore.StepKindWorkflow:
		nested, reply, err := o.runWorkflow(ctx, step.WorkflowID, seen)
		result := eventlog.Payload(map[string]any{"workflow_id": nested.ID, "status": nested.Status, "reply": reply})
		if err != nil {
			return finish(workflowstore.StatusFailed, "", err.Error(), result)
		}
		if nested.Status == workflowstore.StatusCompleted {
			return finish(workflowstore.StatusCompleted, "Workflow completed: "+workflowShortID(nested.ID), "", result)
		}
		return finish(nested.Status, "Workflow paused: "+workflowShortID(nested.ID), "", result)
	case workflowstore.StepKindWait:
		status, summary, errText, result := o.evaluateWorkflowWait(ctx, step, started)
		return finish(status, summary, errText, result)
	default:
		return finish(workflowstore.StatusFailed, "", "unsupported workflow step kind: "+step.Kind, nil)
	}
}

func (o *Orchestrator) evaluateWorkflowWait(ctx context.Context, step workflowstore.Step, started time.Time) (string, string, string, json.RawMessage) {
	now := time.Now().UTC()
	elapsed := now.Sub(started)
	if elapsed < 0 {
		elapsed = 0
	}
	timeout := time.Duration(step.TimeoutSeconds) * time.Second
	condition := strings.TrimSpace(step.Condition)
	payload := map[string]any{
		"condition":       condition,
		"timeout_seconds": step.TimeoutSeconds,
		"elapsed_seconds": int(elapsed.Seconds()),
		"started_at":      started,
	}

	if condition == "" {
		if timeout <= 0 || elapsed >= timeout {
			payload["condition_met"] = true
			return workflowstore.StatusCompleted, "Wait completed", "", eventlog.Payload(payload)
		}
		payload["condition_met"] = false
		payload["remaining_seconds"] = int((timeout - elapsed + time.Second - 1) / time.Second)
		return workflowstore.StatusWaiting, fmt.Sprintf("Waiting for %ds", step.TimeoutSeconds), "", eventlog.Payload(payload)
	}

	met, note := o.workflowWaitConditionMet(ctx, condition)
	payload["condition_met"] = met
	if note != "" {
		payload["note"] = note
	}
	if met {
		return workflowstore.StatusCompleted, "Condition met: " + condition, "", eventlog.Payload(payload)
	}
	if timeout > 0 && elapsed >= timeout {
		errText := fmt.Sprintf("timed out after %ds waiting for condition: %s", step.TimeoutSeconds, condition)
		return workflowstore.StatusFailed, "", errText, eventlog.Payload(payload)
	}
	summary := "Waiting for condition: " + condition
	if note != "" {
		summary += " (" + note + ")"
	}
	return workflowstore.StatusWaiting, summary, "", eventlog.Payload(payload)
}

func (o *Orchestrator) workflowWaitConditionMet(ctx context.Context, condition string) (bool, string) {
	normalized := strings.Join(strings.Fields(strings.ToLower(condition)), " ")
	switch normalized {
	case "true", "ok", "ready", "met", "complete", "completed":
		return true, "literal condition is true"
	}
	if strings.Contains(normalized, "homelabd") &&
		strings.Contains(normalized, "health") &&
		(strings.Contains(normalized, "reachable") || strings.Contains(normalized, "healthy") || strings.Contains(normalized, "ok")) {
		return true, "homelabd health is reachable"
	}
	if strings.Contains(normalized, "healthd") &&
		(strings.Contains(normalized, "health") || strings.Contains(normalized, "healthy") || strings.Contains(normalized, "ok")) {
		return o.healthdIsHealthy(ctx)
	}
	return false, "no automatic evaluator matched"
}

func (o *Orchestrator) healthdIsHealthy(ctx context.Context) (bool, string) {
	if o.cfg.Healthd.Enabled != nil && !*o.cfg.Healthd.Enabled {
		return false, "healthd is disabled"
	}
	addr := strings.TrimSpace(o.cfg.Healthd.Addr)
	if addr == "" {
		return false, "healthd address is not configured"
	}
	timeout := time.Duration(o.cfg.Healthd.RequestTimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 2 * time.Second
	}
	client := &http.Client{Timeout: timeout}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(workflowHTTPAddr(addr), "/")+"/healthd?window=5m", nil)
	if err != nil {
		return false, err.Error()
	}
	resp, err := client.Do(req)
	if err != nil {
		return false, "healthd unreachable: " + err.Error()
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return false, "healthd response unreadable: " + err.Error()
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return false, fmt.Sprintf("healthd returned %s", resp.Status)
	}
	var snapshot struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(body, &snapshot); err != nil {
		return false, "healthd response invalid: " + err.Error()
	}
	status := strings.TrimSpace(snapshot.Status)
	if status == "" {
		return false, "healthd status missing"
	}
	return status == "healthy", "healthd status: " + status
}

func workflowHTTPAddr(addr string) string {
	if strings.HasPrefix(addr, "http://") || strings.HasPrefix(addr, "https://") {
		return addr
	}
	return "http://" + addr
}

func workflowStepPrompt(item workflowstore.Workflow, step workflowstore.Step, previous []workflowstore.StepOutput) string {
	var lines []string
	lines = append(lines, "Workflow: "+item.Name)
	if item.Goal != "" {
		lines = append(lines, "Goal: "+item.Goal)
	}
	lines = append(lines, "Step: "+step.Name)
	lines = append(lines, "Prompt: "+step.Prompt)
	if len(previous) > 0 {
		lines = append(lines, "Previous outputs:")
		for _, output := range previous {
			lines = append(lines, "- "+output.StepName+": "+strings.TrimSpace(output.Summary))
		}
	}
	return strings.Join(lines, "\n")
}

func (o *Orchestrator) appendWorkflowEvent(ctx context.Context, eventType string, item workflowstore.Workflow, payload map[string]any) {
	if o.events == nil {
		return
	}
	payload["workflow_id"] = item.ID
	_ = o.events.Append(ctx, eventlog.Event{
		ID:      id.New("evt"),
		Type:    eventType,
		Actor:   "OrchestratorAgent",
		Payload: eventlog.Payload(payload),
	})
}

func (o *Orchestrator) resolveWorkflowID(selector string) (string, error) {
	selector = strings.TrimSpace(selector)
	if selector == "" {
		return "", errors.New("workflow id or name is required")
	}
	store, err := o.workflowStore()
	if err != nil {
		return "", err
	}
	if item, err := store.Load(selector); err == nil {
		return item.ID, nil
	}
	items, err := o.ListWorkflows()
	if err != nil {
		return "", err
	}
	normalized := strings.ToLower(selector)
	var matches []workflowstore.Workflow
	for _, item := range items {
		short := workflowShortID(item.ID)
		if strings.EqualFold(selector, short) ||
			strings.Contains(strings.ToLower(item.ID), normalized) ||
			strings.Contains(strings.ToLower(item.Name), normalized) {
			matches = append(matches, item)
		}
	}
	if len(matches) == 0 {
		return "", fmt.Errorf("no workflow matches %q", selector)
	}
	if len(matches) > 1 {
		var parts []string
		for _, match := range matches {
			parts = append(parts, workflowShortID(match.ID)+" "+match.Name)
		}
		return "", fmt.Errorf("workflow selector %q matched %s", selector, strings.Join(parts, ", "))
	}
	return matches[0].ID, nil
}

func (o *Orchestrator) handleWorkflowCommand(ctx context.Context, fields []string, message string) (string, error) {
	if len(fields) == 0 {
		return "", errors.New("usage: workflow <list|show|new|run>")
	}
	if len(fields) == 1 {
		return o.formatWorkflowList()
	}
	action := commandWord(fields[1])
	switch action {
	case "list", "ls":
		return o.formatWorkflowList()
	case "show", "get":
		if len(fields) < 3 {
			return "usage: workflow show <workflow_id>", nil
		}
		return o.formatWorkflowShow(strings.Join(fields[2:], " "))
	case "new", "create":
		name, goal := parseWorkflowCreateText(strings.TrimSpace(strings.TrimPrefix(message, fields[0]+" "+fields[1])))
		item, reply, err := o.CreateWorkflow(ctx, workflowstore.CreateRequest{Name: name, Goal: goal, CreatedBy: "chat"})
		if err != nil {
			return "", err
		}
		_ = item
		return reply, nil
	case "run", "start":
		if len(fields) < 3 {
			return "usage: workflow run <workflow_id>", nil
		}
		_, reply, err := o.RunWorkflow(ctx, strings.Join(fields[2:], " "))
		return reply, err
	default:
		return "usage: workflow <list|show|new|run>", nil
	}
}

func (o *Orchestrator) executeWorkflowTool(ctx context.Context, actor, name string, raw json.RawMessage) toolExecution {
	switch name {
	case "workflow.create":
		var req workflowstore.CreateRequest
		if err := json.Unmarshal(raw, &req); err != nil {
			return toolExecution{Tool: name, Allowed: false, Error: err.Error()}
		}
		if req.CreatedBy == "" {
			req.CreatedBy = actor
		}
		item, reply, err := o.CreateWorkflow(ctx, req)
		result := toolExecution{Tool: name, Allowed: true, Result: eventlog.Payload(map[string]any{"workflow": item, "message": reply})}
		if err != nil {
			result.Error = err.Error()
		}
		if o.events != nil {
			_ = o.events.Append(ctx, eventlog.Event{ID: id.New("evt"), Type: "tool.result", Actor: actor, Payload: eventlog.Payload(result)})
		}
		return result
	case "workflow.list":
		items, err := o.ListWorkflows()
		result := toolExecution{Tool: name, Allowed: true, Result: eventlog.Payload(map[string]any{"workflows": items})}
		if err != nil {
			result.Error = err.Error()
		}
		if o.events != nil {
			_ = o.events.Append(ctx, eventlog.Event{ID: id.New("evt"), Type: "tool.result", Actor: actor, Payload: eventlog.Payload(result)})
		}
		return result
	case "workflow.show":
		var req struct {
			WorkflowID string `json:"workflow_id"`
		}
		if err := json.Unmarshal(raw, &req); err != nil {
			return toolExecution{Tool: name, Allowed: false, Error: err.Error()}
		}
		workflowID, err := o.resolveWorkflowID(req.WorkflowID)
		var item workflowstore.Workflow
		if err == nil {
			item, err = o.LoadWorkflow(workflowID)
		}
		result := toolExecution{Tool: name, Allowed: true, Result: eventlog.Payload(map[string]any{"workflow": item})}
		if err != nil {
			result.Error = err.Error()
		}
		if o.events != nil {
			_ = o.events.Append(ctx, eventlog.Event{ID: id.New("evt"), Type: "tool.result", Actor: actor, Payload: eventlog.Payload(result)})
		}
		return result
	case "workflow.run":
		var req struct {
			WorkflowID string `json:"workflow_id"`
		}
		if err := json.Unmarshal(raw, &req); err != nil {
			return toolExecution{Tool: name, Allowed: false, Error: err.Error()}
		}
		item, reply, err := o.RunWorkflow(ctx, req.WorkflowID)
		result := toolExecution{Tool: name, Allowed: true, Result: eventlog.Payload(map[string]any{"workflow": item, "message": reply})}
		if err != nil {
			result.Error = err.Error()
		}
		if o.events != nil {
			_ = o.events.Append(ctx, eventlog.Event{ID: id.New("evt"), Type: "tool.result", Actor: actor, Payload: eventlog.Payload(result)})
		}
		return result
	default:
		return toolExecution{Tool: name, Allowed: false, Error: "workflow tool not registered"}
	}
}

func (o *Orchestrator) formatWorkflowList() (string, error) {
	items, err := o.ListWorkflows()
	if err != nil {
		return "", err
	}
	if len(items) == 0 {
		return strings.Join([]string{
			"No workflows yet.",
			"Create one with `workflow new <name>: <goal>` or use the Workflows page.",
		}, "\n"), nil
	}
	lines := []string{"Workflows:"}
	for _, item := range items {
		lines = append(lines, fmt.Sprintf(
			"- %s %s [%s] %s. Next: `workflow show %s`, `workflow run %s`",
			workflowShortID(item.ID),
			item.Name,
			item.Status,
			item.Estimate.Summary,
			workflowShortID(item.ID),
			workflowShortID(item.ID),
		))
	}
	return strings.Join(lines, "\n"), nil
}

func (o *Orchestrator) formatWorkflowShow(selector string) (string, error) {
	workflowID, err := o.resolveWorkflowID(selector)
	if err != nil {
		return "", err
	}
	item, err := o.LoadWorkflow(workflowID)
	if err != nil {
		return "", err
	}
	lines := []string{
		fmt.Sprintf("Workflow %s: %s", workflowShortID(item.ID), item.Name),
		"Status: " + item.Status,
		"Estimate: " + item.Estimate.Summary,
	}
	if item.Goal != "" {
		lines = append(lines, "Goal: "+item.Goal)
	}
	lines = append(lines, "Steps:")
	for _, step := range item.Steps {
		lines = append(lines, fmt.Sprintf("- %s [%s] %s", step.ID, step.Kind, workflowStepTarget(step)))
	}
	if item.LastRun != nil {
		lines = append(lines, fmt.Sprintf("Last run: %s (%d/%d step(s))", item.LastRun.Status, len(item.LastRun.Outputs), len(item.Steps)))
	}
	lines = append(lines, "Next:")
	lines = append(lines, "```")
	lines = append(lines, "workflow run "+workflowShortID(item.ID))
	lines = append(lines, "workflows")
	lines = append(lines, "```")
	return strings.Join(lines, "\n"), nil
}

func formatWorkflowCreated(item workflowstore.Workflow) string {
	return strings.Join([]string{
		fmt.Sprintf("Created workflow %s: %s", workflowShortID(item.ID), item.Name),
		"Estimate: " + item.Estimate.Summary,
		"Next:",
		"```",
		"workflow run " + workflowShortID(item.ID),
		"workflow show " + workflowShortID(item.ID),
		"```",
	}, "\n")
}

func formatWorkflowRunResult(item workflowstore.Workflow) string {
	lines := []string{
		fmt.Sprintf("Workflow %s %s: %s", workflowShortID(item.ID), item.Status, item.Name),
	}
	if item.LastRun != nil {
		lines = append(lines, fmt.Sprintf("Run %s processed %d/%d step(s).", item.LastRun.ID, len(item.LastRun.Outputs), len(item.Steps)))
		for _, output := range item.LastRun.Outputs {
			line := fmt.Sprintf("- %s [%s] %s", output.StepName, output.Status, output.Summary)
			if output.Error != "" {
				line += ": " + output.Error
			}
			lines = append(lines, line)
		}
	}
	if item.Status == workflowstore.StatusWaiting {
		lines = append(lines, "Next: `workflow run "+workflowShortID(item.ID)+"` to re-check, `workflow show "+workflowShortID(item.ID)+"`")
	} else {
		lines = append(lines, "Next: `workflow show "+workflowShortID(item.ID)+"`")
	}
	return strings.Join(lines, "\n")
}

func parseWorkflowCreateText(text string) (string, string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return "", ""
	}
	if name, goal, ok := strings.Cut(text, ":"); ok {
		name = strings.TrimSpace(name)
		goal = strings.TrimSpace(goal)
		if goal == "" {
			goal = name
		}
		return name, goal
	}
	return firstLine(text), text
}

func workflowStepTarget(step workflowstore.Step) string {
	switch step.Kind {
	case workflowstore.StepKindTool:
		return step.Name + " -> " + step.Tool
	case workflowstore.StepKindWorkflow:
		return step.Name + " -> " + workflowShortID(step.WorkflowID)
	case workflowstore.StepKindWait:
		return step.Name + " -> " + step.Condition
	default:
		return step.Name
	}
}

func workflowShortID(workflowID string) string {
	parts := strings.Split(workflowID, "_")
	if len(parts) > 0 && len(parts[len(parts)-1]) >= 6 {
		return parts[len(parts)-1]
	}
	if len(workflowID) <= 8 {
		return workflowID
	}
	return workflowID[len(workflowID)-8:]
}
