package task

import "time"

const (
	StatusQueued               = "queued"
	StatusRunning              = "running"
	StatusBlocked              = "blocked"
	StatusReadyForReview       = "ready_for_review"
	StatusAwaitingApproval     = "awaiting_approval"
	StatusAwaitingVerification = "awaiting_verification"
	StatusDone                 = "done"
	StatusFailed               = "failed"
	StatusCancelled            = "cancelled"
)

type Task struct {
	ID         string     `json:"id"`
	Title      string     `json:"title"`
	Goal       string     `json:"goal"`
	Status     string     `json:"status"`
	AssignedTo string     `json:"assigned_to"`
	Priority   int        `json:"priority"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
	StartedAt  *time.Time `json:"started_at,omitempty"`
	StoppedAt  *time.Time `json:"stopped_at,omitempty"`
	DueAt      *time.Time `json:"due_at,omitempty"`
	ParentID   string     `json:"parent_id,omitempty"`
	ContextIDs []string   `json:"context_ids,omitempty"`
	Workspace  string     `json:"workspace,omitempty"`
	Result     string     `json:"result,omitempty"`
}
