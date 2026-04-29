package task

import "time"

const (
	StatusQueued               = "queued"
	StatusRunning              = "running"
	StatusBlocked              = "blocked"
	StatusConflictResolution   = "conflict_resolution"
	StatusReadyForReview       = "ready_for_review"
	StatusAwaitingApproval     = "awaiting_approval"
	StatusAwaitingRestart      = "awaiting_restart"
	StatusAwaitingVerification = "awaiting_verification"
	StatusDone                 = "done"
	StatusFailed               = "failed"
	StatusCancelled            = "cancelled"
)

const (
	RestartStatusPending  = "pending"
	RestartStatusRunning  = "running"
	RestartStatusComplete = "complete"
	RestartStatusFailed   = "failed"
)

type Task struct {
	ID                   string                `json:"id"`
	Title                string                `json:"title"`
	Goal                 string                `json:"goal"`
	Status               string                `json:"status"`
	AssignedTo           string                `json:"assigned_to"`
	Priority             int                   `json:"priority"`
	CreatedAt            time.Time             `json:"created_at"`
	UpdatedAt            time.Time             `json:"updated_at"`
	StartedAt            *time.Time            `json:"started_at,omitempty"`
	StoppedAt            *time.Time            `json:"stopped_at,omitempty"`
	DueAt                *time.Time            `json:"due_at,omitempty"`
	ParentID             string                `json:"parent_id,omitempty"`
	ContextIDs           []string              `json:"context_ids,omitempty"`
	DependsOn            []string              `json:"depends_on,omitempty"`
	BlockedBy            []string              `json:"blocked_by,omitempty"`
	GraphPhase           string                `json:"graph_phase,omitempty"`
	Target               *ExecutionTarget      `json:"target,omitempty"`
	AcceptanceCriteria   []AcceptanceCriterion `json:"acceptance_criteria,omitempty"`
	Attachments          []Attachment          `json:"attachments,omitempty"`
	RestartRequired      []string              `json:"restart_required,omitempty"`
	RestartCompleted     []string              `json:"restart_completed,omitempty"`
	RestartStatus        string                `json:"restart_status,omitempty"`
	RestartCurrent       string                `json:"restart_current,omitempty"`
	RestartLastError     string                `json:"restart_last_error,omitempty"`
	AutoRecoveryAttempts int                   `json:"auto_recovery_attempts,omitempty"`
	AutoRecoveryLastAt   *time.Time            `json:"auto_recovery_last_at,omitempty"`
	Workspace            string                `json:"workspace,omitempty"`
	Result               string                `json:"result,omitempty"`
	Plan                 *TaskPlan             `json:"plan,omitempty"`
}

type Attachment struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	ContentType string    `json:"content_type"`
	Size        int64     `json:"size"`
	DataURL     string    `json:"data_url,omitempty"`
	Text        string    `json:"text,omitempty"`
	CreatedAt   time.Time `json:"created_at,omitempty"`
}

type ExecutionTarget struct {
	Mode      string `json:"mode,omitempty"`
	AgentID   string `json:"agent_id,omitempty"`
	Machine   string `json:"machine,omitempty"`
	WorkdirID string `json:"workdir_id,omitempty"`
	Workdir   string `json:"workdir,omitempty"`
	Backend   string `json:"backend,omitempty"`
}

type AcceptanceCriterion struct {
	ID          string `json:"id"`
	Description string `json:"description"`
	Status      string `json:"status"`
}

type TaskPlan struct {
	Status     string         `json:"status"`
	Summary    string         `json:"summary"`
	Steps      []TaskPlanStep `json:"steps"`
	Risks      []string       `json:"risks,omitempty"`
	Review     string         `json:"review,omitempty"`
	CreatedAt  time.Time      `json:"created_at"`
	ReviewedAt *time.Time     `json:"reviewed_at,omitempty"`
}

type TaskPlanStep struct {
	Title  string `json:"title"`
	Detail string `json:"detail,omitempty"`
}
