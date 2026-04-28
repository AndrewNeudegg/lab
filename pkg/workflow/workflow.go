package workflow

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	StatusDraft            = "draft"
	StatusRunning          = "running"
	StatusWaiting          = "waiting"
	StatusAwaitingApproval = "awaiting_approval"
	StatusCompleted        = "completed"
	StatusFailed           = "failed"
	StatusCancelled        = "cancelled"
)

const (
	StepKindLLM      = "llm"
	StepKindTool     = "tool"
	StepKindWorkflow = "workflow"
	StepKindWait     = "wait"
)

type Workflow struct {
	ID          string       `json:"id"`
	Name        string       `json:"name"`
	Description string       `json:"description,omitempty"`
	Goal        string       `json:"goal,omitempty"`
	Status      string       `json:"status"`
	Steps       []Step       `json:"steps"`
	Estimate    CostEstimate `json:"estimate"`
	CreatedBy   string       `json:"created_by,omitempty"`
	CreatedAt   time.Time    `json:"created_at"`
	UpdatedAt   time.Time    `json:"updated_at"`
	LastRun     *Run         `json:"last_run,omitempty"`
}

type Step struct {
	ID             string          `json:"id"`
	Name           string          `json:"name"`
	Kind           string          `json:"kind"`
	Prompt         string          `json:"prompt,omitempty"`
	Tool           string          `json:"tool,omitempty"`
	Args           json.RawMessage `json:"args,omitempty"`
	WorkflowID     string          `json:"workflow_id,omitempty"`
	Condition      string          `json:"condition,omitempty"`
	TimeoutSeconds int             `json:"timeout_seconds,omitempty"`
	DependsOn      []string        `json:"depends_on,omitempty"`
}

type CostEstimate struct {
	Steps              int    `json:"steps"`
	EstimatedLLMCalls  int    `json:"estimated_llm_calls"`
	EstimatedToolCalls int    `json:"estimated_tool_calls"`
	WorkflowCalls      int    `json:"workflow_calls"`
	Waits              int    `json:"waits"`
	EstimatedSeconds   int    `json:"estimated_seconds"`
	EstimatedMinutes   int    `json:"estimated_minutes"`
	Summary            string `json:"summary"`
}

type Run struct {
	ID          string       `json:"id"`
	WorkflowID  string       `json:"workflow_id"`
	Status      string       `json:"status"`
	CurrentStep int          `json:"current_step"`
	StartedAt   time.Time    `json:"started_at"`
	FinishedAt  *time.Time   `json:"finished_at,omitempty"`
	Outputs     []StepOutput `json:"outputs,omitempty"`
	Error       string       `json:"error,omitempty"`
}

type StepOutput struct {
	StepID     string          `json:"step_id"`
	StepName   string          `json:"step_name"`
	Kind       string          `json:"kind"`
	Status     string          `json:"status"`
	Summary    string          `json:"summary,omitempty"`
	Tool       string          `json:"tool,omitempty"`
	Result     json.RawMessage `json:"result,omitempty"`
	Error      string          `json:"error,omitempty"`
	StartedAt  time.Time       `json:"started_at"`
	FinishedAt *time.Time      `json:"finished_at,omitempty"`
}

type CreateRequest struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Goal        string `json:"goal,omitempty"`
	Steps       []Step `json:"steps,omitempty"`
	CreatedBy   string `json:"created_by,omitempty"`
}

func New(req CreateRequest, id string, now time.Time) (Workflow, error) {
	workflow := Workflow{
		ID:          strings.TrimSpace(id),
		Name:        strings.TrimSpace(req.Name),
		Description: strings.TrimSpace(req.Description),
		Goal:        strings.TrimSpace(req.Goal),
		Status:      StatusDraft,
		Steps:       req.Steps,
		CreatedBy:   strings.TrimSpace(req.CreatedBy),
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	return Normalize(workflow)
}

func Normalize(item Workflow) (Workflow, error) {
	item.ID = strings.TrimSpace(item.ID)
	if item.ID == "" {
		return Workflow{}, errors.New("workflow id is required")
	}
	item.Name = strings.TrimSpace(item.Name)
	item.Description = strings.TrimSpace(item.Description)
	item.Goal = strings.TrimSpace(item.Goal)
	item.CreatedBy = strings.TrimSpace(item.CreatedBy)
	if item.Name == "" {
		item.Name = firstLine(item.Goal)
	}
	if item.Name == "" {
		return Workflow{}, errors.New("workflow name is required")
	}
	if item.Status == "" {
		item.Status = StatusDraft
	}
	if len(item.Steps) == 0 {
		prompt := item.Goal
		if prompt == "" {
			prompt = item.Name
		}
		item.Steps = []Step{{
			Name:   "Plan next action",
			Kind:   StepKindLLM,
			Prompt: prompt,
		}}
	}
	steps := make([]Step, 0, len(item.Steps))
	for index, step := range item.Steps {
		normalized, err := normalizeStep(step, index, item.Goal)
		if err != nil {
			return Workflow{}, err
		}
		steps = append(steps, normalized)
	}
	item.Steps = steps
	item.Estimate = Estimate(item.Steps)
	return item, nil
}

func Estimate(steps []Step) CostEstimate {
	var estimate CostEstimate
	estimate.Steps = len(steps)
	for _, step := range steps {
		switch stepKind(step.Kind) {
		case StepKindTool:
			estimate.EstimatedToolCalls++
			estimate.EstimatedSeconds += 30
		case StepKindWorkflow:
			estimate.WorkflowCalls++
			estimate.EstimatedSeconds += 60
		case StepKindWait:
			estimate.Waits++
			seconds := step.TimeoutSeconds
			if seconds <= 0 {
				seconds = 300
			}
			estimate.EstimatedSeconds += seconds
		default:
			estimate.EstimatedLLMCalls++
			estimate.EstimatedSeconds += 45
		}
	}
	if estimate.EstimatedSeconds > 0 {
		estimate.EstimatedMinutes = (estimate.EstimatedSeconds + 59) / 60
	}
	estimate.Summary = fmt.Sprintf(
		"%d step(s), %d LLM call(s), %d tool call(s), %d wait(s), about %dm",
		estimate.Steps,
		estimate.EstimatedLLMCalls,
		estimate.EstimatedToolCalls,
		estimate.Waits,
		estimate.EstimatedMinutes,
	)
	return estimate
}

func normalizeStep(step Step, index int, workflowGoal string) (Step, error) {
	step.ID = strings.TrimSpace(step.ID)
	if step.ID == "" {
		step.ID = fmt.Sprintf("step_%02d", index+1)
	}
	step.Name = strings.TrimSpace(step.Name)
	step.Kind = stepKind(step.Kind)
	step.Prompt = strings.TrimSpace(step.Prompt)
	step.Tool = strings.TrimSpace(step.Tool)
	step.WorkflowID = strings.TrimSpace(step.WorkflowID)
	step.Condition = strings.TrimSpace(step.Condition)
	if step.Name == "" {
		step.Name = fmt.Sprintf("Step %d", index+1)
	}
	switch step.Kind {
	case StepKindLLM:
		if step.Prompt == "" {
			step.Prompt = workflowGoal
		}
		if step.Prompt == "" {
			return Step{}, fmt.Errorf("workflow step %q prompt is required", step.ID)
		}
	case StepKindTool:
		if step.Tool == "" {
			return Step{}, fmt.Errorf("workflow step %q tool is required", step.ID)
		}
		if len(step.Args) == 0 {
			step.Args = json.RawMessage(`{}`)
		}
		if !json.Valid(step.Args) {
			return Step{}, fmt.Errorf("workflow step %q args must be valid JSON", step.ID)
		}
	case StepKindWorkflow:
		if step.WorkflowID == "" {
			return Step{}, fmt.Errorf("workflow step %q workflow_id is required", step.ID)
		}
	case StepKindWait:
		if step.TimeoutSeconds < 0 {
			return Step{}, fmt.Errorf("workflow step %q timeout_seconds must not be negative", step.ID)
		}
	default:
		return Step{}, fmt.Errorf("workflow step %q has unsupported kind %q", step.ID, step.Kind)
	}
	return step, nil
}

func stepKind(kind string) string {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "", "llm", "model":
		return StepKindLLM
	case "tool":
		return StepKindTool
	case "workflow", "chain":
		return StepKindWorkflow
	case "wait", "condition":
		return StepKindWait
	default:
		return strings.ToLower(strings.TrimSpace(kind))
	}
}

func firstLine(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if idx := strings.IndexAny(value, "\r\n"); idx >= 0 {
		value = value[:idx]
	}
	if len(value) > 96 {
		value = strings.TrimSpace(value[:96]) + "..."
	}
	return value
}
