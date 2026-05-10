package task

import "time"

const (
	StatusQueued               = "queued"
	StatusRunning              = "running"
	StatusBlocked              = "blocked"
	StatusTimedOut             = "timed_out"
	StatusConflictResolution   = "conflict_resolution"
	StatusReadyForReview       = "ready_for_review"
	StatusAwaitingApproval     = "awaiting_approval"
	StatusAwaitingRestart      = "awaiting_restart"
	StatusAwaitingVerification = "awaiting_verification"
	StatusNoChangeRequired     = "no_change_required"
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
	GoalID               string                `json:"goal_id,omitempty"`
	GoalPhaseID          string                `json:"goal_phase_id,omitempty"`
	ExecutionMode        string                `json:"execution_mode,omitempty"`
	GoalKind             string                `json:"goal_kind,omitempty"`
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
	MergeQueuePosition   int                   `json:"merge_queue_position,omitempty"`
	MergeQueueEnteredAt  *time.Time            `json:"merge_queue_entered_at,omitempty"`
	Workspace            string                `json:"workspace,omitempty"`
	Result               string                `json:"result,omitempty"`
	DiffSnapshot         *TaskDiffSnapshot     `json:"diff_snapshot,omitempty"`
	RemoteDiff           string                `json:"remote_diff,omitempty"`
	RemoteDiffCapturedAt *time.Time            `json:"remote_diff_captured_at,omitempty"`
	Plan                 *TaskPlan             `json:"plan,omitempty"`
}

type TaskDiffSnapshot struct {
	Source     string                  `json:"source,omitempty"`
	BaseRef    string                  `json:"base_ref,omitempty"`
	BaseLabel  string                  `json:"base_label,omitempty"`
	HeadRef    string                  `json:"head_ref,omitempty"`
	HeadLabel  string                  `json:"head_label,omitempty"`
	Workspace  string                  `json:"workspace,omitempty"`
	RawDiff    string                  `json:"raw_diff"`
	Summary    TaskDiffSnapshotSummary `json:"summary"`
	Files      []TaskDiffSnapshotFile  `json:"files,omitempty"`
	CapturedAt time.Time               `json:"captured_at"`
	SHA256     string                  `json:"sha256,omitempty"`
	Warning    string                  `json:"warning,omitempty"`
}

type TaskDiffSnapshotSummary struct {
	Files     int `json:"files"`
	Additions int `json:"additions"`
	Deletions int `json:"deletions"`
}

type TaskDiffSnapshotFile struct {
	Path      string `json:"path"`
	OldPath   string `json:"old_path,omitempty"`
	Status    string `json:"status"`
	Additions int    `json:"additions"`
	Deletions int    `json:"deletions"`
	Binary    bool   `json:"binary,omitempty"`
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
	Mode      string   `json:"mode,omitempty"`
	ProjectID string   `json:"project_id,omitempty"`
	AgentID   string   `json:"agent_id,omitempty"`
	Machine   string   `json:"machine,omitempty"`
	WorkdirID string   `json:"workdir_id,omitempty"`
	Workdir   string   `json:"workdir,omitempty"`
	RepoURL   string   `json:"repo_url,omitempty"`
	Branch    string   `json:"branch,omitempty"`
	Labels    []string `json:"labels,omitempty"`
	Backend   string   `json:"backend,omitempty"`
	Reason    string   `json:"reason,omitempty"`
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
	UIUXBrief  *UIUXBrief     `json:"ui_ux_brief,omitempty"`
	CreatedAt  time.Time      `json:"created_at"`
	ReviewedAt *time.Time     `json:"reviewed_at,omitempty"`
}

type TaskPlanStep struct {
	Title  string `json:"title"`
	Detail string `json:"detail,omitempty"`
}

type UIUXBrief struct {
	OperatorGoal    string   `json:"operator_goal"`
	PrimaryWorkflow string   `json:"primary_workflow"`
	Surfaces        []string `json:"surfaces"`
	ExistingPattern string   `json:"existing_pattern"`
	DesktopLayout   string   `json:"desktop_layout"`
	MobileLayout    string   `json:"mobile_layout"`
	States          []string `json:"states"`
	Accessibility   []string `json:"accessibility"`
	Validation      []string `json:"validation"`
}
