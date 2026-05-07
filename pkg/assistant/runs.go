package assistant

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	RunStatusRunning   = "running"
	RunStatusCompleted = "completed"
	RunStatusFailed    = "failed"

	RunDecisionNoop      = "no_op"
	RunDecisionRecommend = "recommend"
	RunDecisionCreated   = "created_tasks"

	RunAutonomyObserve      = "observe"
	RunAutonomyPropose      = "propose"
	RunAutonomyCreateTasks  = "create_tasks"
	RunAutonomyRunWorkflows = "run_workflows"
	RunAutonomyExecuteSafe  = "execute_safe"
)

type RunRequest struct {
	TriggerKind  string `json:"trigger_kind,omitempty"`
	TriggerLabel string `json:"trigger_label,omitempty"`
	GoalID       string `json:"goal_id,omitempty"`
	Goal         string `json:"goal,omitempty"`
	Autonomy     string `json:"autonomy,omitempty"`
}

type RunArchiveRequest struct {
	Archived *bool  `json:"archived,omitempty"`
	Reason   string `json:"reason,omitempty"`
	Actor    string `json:"actor,omitempty"`
}

type Run struct {
	ID                 string               `json:"id"`
	Status             string               `json:"status"`
	Decision           string               `json:"decision"`
	Trigger            RunTrigger           `json:"trigger"`
	Autonomy           string               `json:"autonomy"`
	GoalID             string               `json:"goal_id,omitempty"`
	Goal               string               `json:"goal,omitempty"`
	Summary            string               `json:"summary"`
	Changed            []string             `json:"changed,omitempty"`
	Concerns           []RunFinding         `json:"concerns,omitempty"`
	Opportunities      []RunFinding         `json:"opportunities,omitempty"`
	RecommendedActions []RunAction          `json:"recommended_actions,omitempty"`
	Route              *RunCapabilityRoute  `json:"route,omitempty"`
	Compiler           *RunDecisionCompiler `json:"compiler,omitempty"`
	Receipts           []RunReceipt         `json:"receipts,omitempty"`
	Snapshot           RunSnapshot          `json:"snapshot"`
	Error              string               `json:"error,omitempty"`
	Provider           string               `json:"provider,omitempty"`
	Model              string               `json:"model,omitempty"`
	Usage              RunUsage             `json:"usage,omitempty"`
	Archived           bool                 `json:"archived,omitempty"`
	ArchivedAt         *time.Time           `json:"archived_at,omitempty"`
	ArchivedBy         string               `json:"archived_by,omitempty"`
	ArchivedReason     string               `json:"archived_reason,omitempty"`
	CreatedAt          time.Time            `json:"created_at"`
	StartedAt          time.Time            `json:"started_at,omitempty"`
	FinishedAt         time.Time            `json:"finished_at,omitempty"`
	UpdatedAt          time.Time            `json:"updated_at"`
}

type RunDecisionCompiler struct {
	Status      string                  `json:"status,omitempty"`
	Source      string                  `json:"source,omitempty"`
	Summary     string                  `json:"summary,omitempty"`
	Checks      []string                `json:"checks,omitempty"`
	Contracts   []RunCapabilityContract `json:"contracts,omitempty"`
	Scorecard   *RunDecisionScorecard   `json:"scorecard,omitempty"`
	PolicyHints []RunPolicyHint         `json:"policy_hints,omitempty"`
	Repairs     []string                `json:"repairs,omitempty"`
	Rejections  []string                `json:"rejections,omitempty"`
}

type RunCapabilityContract struct {
	ID                 string   `json:"id,omitempty"`
	Capability         string   `json:"capability,omitempty"`
	ActionKind         string   `json:"action_kind,omitempty"`
	AllowedSafeActions []string `json:"allowed_safe_actions,omitempty"`
	RequiredEvidence   []string `json:"required_evidence,omitempty"`
	RequiredInputs     []string `json:"required_inputs,omitempty"`
	AutonomyCeiling    string   `json:"autonomy_ceiling,omitempty"`
	Risk               string   `json:"risk,omitempty"`
	RequiresApproval   bool     `json:"requires_approval,omitempty"`
	DuplicateRule      string   `json:"duplicate_rule,omitempty"`
	SuppressionRule    string   `json:"suppression_rule,omitempty"`
	CompletionRule     string   `json:"completion_rule,omitempty"`
	Explanation        string   `json:"explanation,omitempty"`
}

type RunDecisionScorecard struct {
	Score                 int    `json:"score"`
	Grade                 string `json:"grade,omitempty"`
	JSONValid             bool   `json:"json_valid"`
	JSONRepaired          bool   `json:"json_repaired"`
	FallbackUsed          bool   `json:"fallback_used"`
	SignalCount           int    `json:"signal_count,omitempty"`
	ActiveSignalCount     int    `json:"active_signal_count,omitempty"`
	SuppressedSignalCount int    `json:"suppressed_signal_count,omitempty"`
	PolicyHintCount       int    `json:"policy_hint_count,omitempty"`
	ModelActionCount      int    `json:"model_action_count,omitempty"`
	KeptActionCount       int    `json:"kept_action_count,omitempty"`
	RejectedActionCount   int    `json:"rejected_action_count,omitempty"`
	RepairCount           int    `json:"repair_count,omitempty"`
	PlanPreviewCount      int    `json:"plan_preview_count,omitempty"`
}

type RunPolicyHint struct {
	Fingerprint string `json:"fingerprint,omitempty"`
	Source      string `json:"source,omitempty"`
	Kind        string `json:"kind,omitempty"`
	Status      string `json:"status,omitempty"`
	Effect      string `json:"effect,omitempty"`
	Reason      string `json:"reason,omitempty"`
	SeenCount   int    `json:"seen_count,omitempty"`
	UsefulCount int    `json:"useful_count,omitempty"`
}

type RunCapabilityRoute struct {
	Capability       string `json:"capability"`
	Decision         string `json:"decision,omitempty"`
	Reason           string `json:"reason,omitempty"`
	NextStep         string `json:"next_step,omitempty"`
	Autonomy         string `json:"autonomy,omitempty"`
	RequiresApproval bool   `json:"requires_approval,omitempty"`
}

type RunTrigger struct {
	Kind  string `json:"kind"`
	Label string `json:"label"`
}

type RunFinding struct {
	Title     string `json:"title"`
	GoalID    string `json:"goal_id,omitempty"`
	Detail    string `json:"detail,omitempty"`
	Severity  string `json:"severity,omitempty"`
	Surface   string `json:"surface,omitempty"`
	ObjectID  string `json:"object_id,omitempty"`
	ObjectURL string `json:"object_url,omitempty"`
}

type RunAction struct {
	ID             string                 `json:"id"`
	Fingerprint    string                 `json:"fingerprint,omitempty"`
	ContractID     string                 `json:"contract_id,omitempty"`
	Contract       *RunCapabilityContract `json:"contract,omitempty"`
	Kind           string                 `json:"kind"`
	GoalID         string                 `json:"goal_id,omitempty"`
	Title          string                 `json:"title"`
	Rationale      string                 `json:"rationale"`
	Priority       string                 `json:"priority,omitempty"`
	Risk           string                 `json:"risk,omitempty"`
	TargetSurface  string                 `json:"target_surface,omitempty"`
	TaskGoal       string                 `json:"task_goal,omitempty"`
	KnowledgeQuery string                 `json:"knowledge_query,omitempty"`
	WorkflowHint   string                 `json:"workflow_hint,omitempty"`
	Status         string                 `json:"status,omitempty"`
	CreatedTaskID  string                 `json:"created_task_id,omitempty"`
	Plan           *RunActionPlanPreview  `json:"plan,omitempty"`
	SeenCount      int                    `json:"seen_count,omitempty"`
	UsefulCount    int                    `json:"useful_count,omitempty"`
	SnoozedUntil   time.Time              `json:"snoozed_until,omitempty"`
}

type RunActionPlanPreview struct {
	Status           string                 `json:"status,omitempty"`
	Summary          string                 `json:"summary,omitempty"`
	RequiresApproval bool                   `json:"requires_approval,omitempty"`
	Steps            []RunActionPlanStep    `json:"steps,omitempty"`
	Receipts         []RunActionPlanReceipt `json:"receipts,omitempty"`
	Blockers         []string               `json:"blockers,omitempty"`
}

type RunActionPlanStep struct {
	Title   string `json:"title"`
	Surface string `json:"surface,omitempty"`
	Mode    string `json:"mode,omitempty"`
	Status  string `json:"status,omitempty"`
}

type RunActionPlanReceipt struct {
	Kind    string `json:"kind"`
	Message string `json:"message"`
}

type RunReceipt struct {
	Kind      string    `json:"kind"`
	Message   string    `json:"message"`
	ObjectID  string    `json:"object_id,omitempty"`
	ObjectURL string    `json:"object_url,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

type RunUsage struct {
	InputTokens  int `json:"input_tokens,omitempty"`
	OutputTokens int `json:"output_tokens,omitempty"`
	TotalTokens  int `json:"total_tokens,omitempty"`
}

type RunSnapshot struct {
	GeneratedAt       time.Time         `json:"generated_at"`
	Signals           []RunSignal       `json:"signals,omitempty"`
	Goals             []GoalSnapshotRef `json:"goals,omitempty"`
	TaskCounts        map[string]int    `json:"task_counts,omitempty"`
	AttentionTasks    []RunObjectRef    `json:"attention_tasks,omitempty"`
	PendingApprovals  int               `json:"pending_approvals,omitempty"`
	WorkflowCounts    map[string]int    `json:"workflow_counts,omitempty"`
	RecentWorkflows   []RunObjectRef    `json:"recent_workflows,omitempty"`
	KnowledgeSpaces   []RunObjectRef    `json:"knowledge_spaces,omitempty"`
	RemoteAgentCounts map[string]int    `json:"remote_agent_counts,omitempty"`
	Health            RunSystemSnapshot `json:"health,omitempty"`
	Supervisor        RunSystemSnapshot `json:"supervisor,omitempty"`
	RecentEvents      []RunEventRef     `json:"recent_events,omitempty"`
}

type RunSignal struct {
	ID                string              `json:"id"`
	Fingerprint       string              `json:"fingerprint"`
	Kind              string              `json:"kind"`
	GoalID            string              `json:"goal_id,omitempty"`
	Title             string              `json:"title"`
	Detail            string              `json:"detail,omitempty"`
	WhyNow            string              `json:"why_now,omitempty"`
	Severity          string              `json:"severity,omitempty"`
	Surface           string              `json:"surface,omitempty"`
	ObjectID          string              `json:"object_id,omitempty"`
	ObjectURL         string              `json:"object_url,omitempty"`
	Score             int                 `json:"score"`
	Confidence        string              `json:"confidence,omitempty"`
	Priority          string              `json:"priority,omitempty"`
	ActionKind        string              `json:"action_kind,omitempty"`
	Rationale         string              `json:"rationale,omitempty"`
	TaskGoal          string              `json:"task_goal,omitempty"`
	Evidence          []RunSignalEvidence `json:"evidence,omitempty"`
	SafeActions       []string            `json:"safe_actions,omitempty"`
	SuggestedNextStep string              `json:"suggested_next_step,omitempty"`
	Suppressed        bool                `json:"suppressed,omitempty"`
	SuppressionReason string              `json:"suppression_reason,omitempty"`
	FeedbackHint      string              `json:"feedback_hint,omitempty"`
	DismissedCount    int                 `json:"dismissed_count,omitempty"`
	SnoozedCount      int                 `json:"snoozed_count,omitempty"`
	SeenCount         int                 `json:"seen_count,omitempty"`
	UsefulCount       int                 `json:"useful_count,omitempty"`
	CreatedTaskID     string              `json:"created_task_id,omitempty"`
	SnoozedUntil      time.Time           `json:"snoozed_until,omitempty"`
}

type RunSignalEvidence struct {
	Source     string     `json:"source,omitempty"`
	Kind       string     `json:"kind,omitempty"`
	Title      string     `json:"title"`
	Detail     string     `json:"detail,omitempty"`
	ObjectID   string     `json:"object_id,omitempty"`
	ObjectURL  string     `json:"object_url,omitempty"`
	ObservedAt *time.Time `json:"observed_at,omitempty"`
	Weight     int        `json:"weight,omitempty"`
}

type RunObjectRef struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Status  string `json:"status,omitempty"`
	Summary string `json:"summary,omitempty"`
	URL     string `json:"url,omitempty"`
}

type RunSystemSnapshot struct {
	Status string         `json:"status,omitempty"`
	Error  string         `json:"error,omitempty"`
	Items  []RunObjectRef `json:"items,omitempty"`
}

type RunEventRef struct {
	ID      string    `json:"id"`
	Type    string    `json:"type"`
	Actor   string    `json:"actor,omitempty"`
	TaskID  string    `json:"task_id,omitempty"`
	Summary string    `json:"summary,omitempty"`
	Time    time.Time `json:"time"`
}

type RunStore struct {
	dir string
	mu  sync.Mutex
}

func NewRunStore(dir string) *RunStore {
	return &RunStore{dir: dir}
}

func (s *RunStore) Save(run Run) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC()
	previous, hasPrevious, err := s.loadIfExistsLocked(run.ID)
	if err != nil {
		return err
	}
	if hasPrevious && run.CreatedAt.IsZero() {
		run.CreatedAt = previous.CreatedAt
	}
	if run.CreatedAt.IsZero() {
		run.CreatedAt = now
	}
	if run.UpdatedAt.IsZero() {
		run.UpdatedAt = now
	}
	run = NormalizeRun(run)
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(run, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(s.dir, run.ID+".json"), append(b, '\n'), 0o644)
}

func (s *RunStore) Load(id string) (Run, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadLocked(id)
}

func (s *RunStore) loadLocked(id string) (Run, error) {
	b, err := os.ReadFile(filepath.Join(s.dir, id+".json"))
	if err != nil {
		return Run{}, err
	}
	var run Run
	if err := json.Unmarshal(b, &run); err != nil {
		return Run{}, err
	}
	return NormalizeRun(run), nil
}

func (s *RunStore) loadIfExistsLocked(id string) (Run, bool, error) {
	if strings.TrimSpace(id) == "" {
		return Run{}, false, nil
	}
	run, err := s.loadLocked(id)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Run{}, false, nil
		}
		return Run{}, false, err
	}
	return run, true, nil
}

func (s *RunStore) List() ([]Run, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []Run{}, nil
		}
		return nil, err
	}
	runs := []Run{}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		run, err := s.loadLocked(entry.Name()[:len(entry.Name())-len(".json")])
		if err != nil {
			return nil, err
		}
		runs = append(runs, run)
	}
	sort.Slice(runs, func(i, j int) bool { return runs[i].UpdatedAt.After(runs[j].UpdatedAt) })
	return runs, nil
}

func (s *RunStore) SetArchived(id string, archived bool, actor, reason string, now time.Time) (Run, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	run, err := s.loadLocked(id)
	if err != nil {
		return Run{}, err
	}
	if now.IsZero() {
		now = time.Now().UTC()
	} else {
		now = now.UTC()
	}
	actor = strings.TrimSpace(actor)
	if actor == "" {
		actor = "agent"
	}
	run.Archived = archived
	if archived {
		archivedAt := now
		run.ArchivedAt = &archivedAt
		run.ArchivedBy = actor
		run.ArchivedReason = strings.TrimSpace(reason)
	} else {
		run.ArchivedAt = nil
		run.ArchivedBy = ""
		run.ArchivedReason = ""
	}
	run.UpdatedAt = now
	run = NormalizeRun(run)
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return Run{}, err
	}
	b, err := json.MarshalIndent(run, "", "  ")
	if err != nil {
		return Run{}, err
	}
	if err := os.WriteFile(filepath.Join(s.dir, run.ID+".json"), append(b, '\n'), 0o644); err != nil {
		return Run{}, err
	}
	return run, nil
}

func NormalizeRun(run Run) Run {
	run.ID = strings.TrimSpace(run.ID)
	run.Status = normalizeRunStatus(run.Status)
	run.Decision = normalizeRunDecision(run.Decision)
	run.Autonomy = normalizeRunAutonomy(run.Autonomy)
	run.Trigger.Kind = firstRunValue(run.Trigger.Kind, "manual")
	run.Trigger.Label = firstRunValue(run.Trigger.Label, "Manual proactive check")
	run.GoalID = strings.TrimSpace(run.GoalID)
	run.Goal = strings.TrimSpace(run.Goal)
	run.Summary = strings.TrimSpace(run.Summary)
	run.Error = strings.TrimSpace(run.Error)
	run.Provider = strings.TrimSpace(run.Provider)
	run.Model = strings.TrimSpace(run.Model)
	run.ArchivedBy = strings.TrimSpace(run.ArchivedBy)
	run.ArchivedReason = strings.TrimSpace(run.ArchivedReason)
	if run.Route != nil {
		route := normalizeRunCapabilityRoute(*run.Route)
		if route.Capability == "" && route.Decision == "" && route.NextStep == "" {
			run.Route = nil
		} else {
			run.Route = &route
		}
	}
	if run.Compiler != nil {
		compiler := normalizeRunDecisionCompiler(*run.Compiler)
		if compiler.Status == "" && compiler.Summary == "" && compiler.Source == "" {
			run.Compiler = nil
		} else {
			run.Compiler = &compiler
		}
	}
	if !run.Archived {
		run.ArchivedAt = nil
		run.ArchivedBy = ""
		run.ArchivedReason = ""
	} else if run.ArchivedAt != nil {
		archivedAt := run.ArchivedAt.UTC()
		if archivedAt.IsZero() {
			run.ArchivedAt = nil
		} else {
			run.ArchivedAt = &archivedAt
		}
	}
	if run.Snapshot.TaskCounts == nil {
		run.Snapshot.TaskCounts = map[string]int{}
	}
	if run.Snapshot.WorkflowCounts == nil {
		run.Snapshot.WorkflowCounts = map[string]int{}
	}
	if run.Snapshot.RemoteAgentCounts == nil {
		run.Snapshot.RemoteAgentCounts = map[string]int{}
	}
	for index := range run.Snapshot.Signals {
		run.Snapshot.Signals[index] = normalizeRunSignal(run.Snapshot.Signals[index], index)
	}
	for index := range run.Concerns {
		run.Concerns[index] = normalizeRunFinding(run.Concerns[index])
	}
	for index := range run.Opportunities {
		run.Opportunities[index] = normalizeRunFinding(run.Opportunities[index])
	}
	for index := range run.RecommendedActions {
		run.RecommendedActions[index] = normalizeRunAction(run.RecommendedActions[index], index)
	}
	for index := range run.Receipts {
		run.Receipts[index].Kind = strings.TrimSpace(run.Receipts[index].Kind)
		run.Receipts[index].Message = strings.TrimSpace(run.Receipts[index].Message)
		run.Receipts[index].ObjectID = strings.TrimSpace(run.Receipts[index].ObjectID)
		run.Receipts[index].ObjectURL = strings.TrimSpace(run.Receipts[index].ObjectURL)
	}
	return run
}

func normalizeRunSignal(value RunSignal, index int) RunSignal {
	value.ID = strings.TrimSpace(value.ID)
	if value.ID == "" {
		value.ID = "signal_" + strconv.Itoa(index+1)
	}
	value.Fingerprint = SignalFingerprint(value.Fingerprint)
	value.Kind = strings.TrimSpace(value.Kind)
	if value.Kind == "" {
		value.Kind = "watchlist"
	}
	value.GoalID = strings.TrimSpace(value.GoalID)
	value.Title = strings.TrimSpace(value.Title)
	if value.Title == "" {
		value.Title = "Assistant signal"
	}
	value.Detail = strings.TrimSpace(value.Detail)
	value.WhyNow = strings.TrimSpace(value.WhyNow)
	value.Severity = strings.TrimSpace(value.Severity)
	value.Surface = strings.TrimSpace(value.Surface)
	value.ObjectID = strings.TrimSpace(value.ObjectID)
	value.ObjectURL = strings.TrimSpace(value.ObjectURL)
	if value.Score < 0 {
		value.Score = 0
	}
	if value.Score > 100 {
		value.Score = 100
	}
	value.Confidence = strings.TrimSpace(value.Confidence)
	value.Priority = strings.TrimSpace(value.Priority)
	value.ActionKind = strings.TrimSpace(value.ActionKind)
	value.Rationale = strings.TrimSpace(value.Rationale)
	value.TaskGoal = strings.TrimSpace(value.TaskGoal)
	value.SuggestedNextStep = strings.TrimSpace(value.SuggestedNextStep)
	value.Evidence = normalizeRunSignalEvidenceList(value.Evidence)
	value.SafeActions = normalizeRunSignalSafeActions(value.SafeActions)
	value.SuppressionReason = strings.TrimSpace(value.SuppressionReason)
	value.FeedbackHint = strings.TrimSpace(value.FeedbackHint)
	if value.DismissedCount < 0 {
		value.DismissedCount = 0
	}
	if value.SnoozedCount < 0 {
		value.SnoozedCount = 0
	}
	if value.SeenCount < 0 {
		value.SeenCount = 0
	}
	if value.UsefulCount < 0 {
		value.UsefulCount = 0
	}
	value.CreatedTaskID = strings.TrimSpace(value.CreatedTaskID)
	return value
}

func normalizeRunSignalEvidenceList(values []RunSignalEvidence) []RunSignalEvidence {
	if len(values) == 0 {
		return nil
	}
	out := make([]RunSignalEvidence, 0, len(values))
	for _, value := range values {
		value.Source = strings.TrimSpace(value.Source)
		value.Kind = strings.TrimSpace(value.Kind)
		value.Title = strings.TrimSpace(value.Title)
		value.Detail = strings.TrimSpace(value.Detail)
		value.ObjectID = strings.TrimSpace(value.ObjectID)
		value.ObjectURL = strings.TrimSpace(value.ObjectURL)
		if value.ObservedAt != nil {
			observedAt := value.ObservedAt.UTC()
			if observedAt.IsZero() {
				value.ObservedAt = nil
			} else {
				value.ObservedAt = &observedAt
			}
		}
		if value.Weight < 0 {
			value.Weight = 0
		}
		if value.Weight > 100 {
			value.Weight = 100
		}
		if value.Title == "" && value.Detail == "" && value.ObjectID == "" && value.ObjectURL == "" {
			continue
		}
		if value.Title == "" {
			value.Title = firstRunValue(value.ObjectID, "Signal evidence")
		}
		out = append(out, value)
		if len(out) >= 8 {
			break
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func normalizeRunSignalSafeActions(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
		if len(out) >= 8 {
			break
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func normalizeRunFinding(value RunFinding) RunFinding {
	value.Title = strings.TrimSpace(value.Title)
	value.GoalID = strings.TrimSpace(value.GoalID)
	value.Detail = strings.TrimSpace(value.Detail)
	value.Severity = strings.TrimSpace(value.Severity)
	value.Surface = strings.TrimSpace(value.Surface)
	value.ObjectID = strings.TrimSpace(value.ObjectID)
	value.ObjectURL = strings.TrimSpace(value.ObjectURL)
	return value
}

func normalizeRunAction(value RunAction, index int) RunAction {
	value.ID = strings.TrimSpace(value.ID)
	if value.ID == "" {
		value.ID = "action_" + strconv.Itoa(index+1)
	}
	value.ContractID = strings.TrimSpace(value.ContractID)
	value.Kind = firstRunValue(value.Kind, "observe")
	value.GoalID = strings.TrimSpace(value.GoalID)
	value.Title = strings.TrimSpace(value.Title)
	value.Rationale = strings.TrimSpace(value.Rationale)
	value.Priority = strings.TrimSpace(value.Priority)
	value.Risk = strings.TrimSpace(value.Risk)
	value.TargetSurface = strings.TrimSpace(value.TargetSurface)
	value.TaskGoal = strings.TrimSpace(value.TaskGoal)
	value.KnowledgeQuery = strings.TrimSpace(value.KnowledgeQuery)
	value.WorkflowHint = strings.TrimSpace(value.WorkflowHint)
	value.Status = strings.TrimSpace(value.Status)
	value.CreatedTaskID = strings.TrimSpace(value.CreatedTaskID)
	value.Fingerprint = strings.TrimSpace(value.Fingerprint)
	if value.Fingerprint == "" {
		value.Fingerprint = FingerprintRunAction(value)
	} else {
		value.Fingerprint = SignalFingerprint(value.Fingerprint)
	}
	if value.Contract != nil {
		contract := normalizeRunCapabilityContractMetadata(*value.Contract)
		if contract.ID == "" && contract.Capability == "" && contract.ActionKind == "" {
			value.Contract = nil
		} else {
			value.Contract = &contract
			if value.ContractID == "" {
				value.ContractID = contract.ID
			}
		}
	}
	if value.Plan != nil {
		plan := normalizeRunActionPlanPreview(*value.Plan)
		if plan.Status == "" && plan.Summary == "" && len(plan.Steps) == 0 {
			value.Plan = nil
		} else {
			value.Plan = &plan
		}
	}
	if value.SeenCount < 0 {
		value.SeenCount = 0
	}
	if value.UsefulCount < 0 {
		value.UsefulCount = 0
	}
	return value
}

func normalizeRunDecisionCompiler(value RunDecisionCompiler) RunDecisionCompiler {
	value.Status = strings.TrimSpace(value.Status)
	value.Source = strings.TrimSpace(value.Source)
	value.Summary = strings.TrimSpace(value.Summary)
	value.Checks = normalizeRunStringList(value.Checks, 16)
	value.Repairs = normalizeRunStringList(value.Repairs, 16)
	value.Rejections = normalizeRunStringList(value.Rejections, 16)
	for index := range value.Contracts {
		value.Contracts[index] = normalizeRunCapabilityContractMetadata(value.Contracts[index])
	}
	value.Contracts = compactRunCapabilityContracts(value.Contracts)
	for index := range value.PolicyHints {
		value.PolicyHints[index] = normalizeRunPolicyHint(value.PolicyHints[index])
	}
	value.PolicyHints = compactRunPolicyHints(value.PolicyHints)
	if value.Scorecard != nil {
		scorecard := *value.Scorecard
		if scorecard.Score < 0 {
			scorecard.Score = 0
		}
		if scorecard.Score > 100 {
			scorecard.Score = 100
		}
		scorecard.Grade = strings.TrimSpace(scorecard.Grade)
		value.Scorecard = &scorecard
	}
	return value
}

func normalizeRunCapabilityContractMetadata(value RunCapabilityContract) RunCapabilityContract {
	value.ID = strings.TrimSpace(value.ID)
	value.Capability = strings.TrimSpace(value.Capability)
	value.ActionKind = strings.TrimSpace(value.ActionKind)
	value.AllowedSafeActions = normalizeRunStringList(value.AllowedSafeActions, 8)
	value.RequiredEvidence = normalizeRunStringList(value.RequiredEvidence, 8)
	value.RequiredInputs = normalizeRunStringList(value.RequiredInputs, 8)
	value.AutonomyCeiling = normalizeRunAutonomy(value.AutonomyCeiling)
	value.Risk = strings.TrimSpace(value.Risk)
	value.DuplicateRule = strings.TrimSpace(value.DuplicateRule)
	value.SuppressionRule = strings.TrimSpace(value.SuppressionRule)
	value.CompletionRule = strings.TrimSpace(value.CompletionRule)
	value.Explanation = strings.TrimSpace(value.Explanation)
	return value
}

func normalizeRunPolicyHint(value RunPolicyHint) RunPolicyHint {
	value.Fingerprint = strings.TrimSpace(value.Fingerprint)
	if value.Fingerprint != "" {
		value.Fingerprint = SignalFingerprint(value.Fingerprint)
	}
	value.Source = strings.TrimSpace(value.Source)
	value.Kind = strings.TrimSpace(value.Kind)
	value.Status = strings.TrimSpace(value.Status)
	value.Effect = strings.TrimSpace(value.Effect)
	value.Reason = strings.TrimSpace(value.Reason)
	if value.SeenCount < 0 {
		value.SeenCount = 0
	}
	if value.UsefulCount < 0 {
		value.UsefulCount = 0
	}
	return value
}

func normalizeRunActionPlanPreview(value RunActionPlanPreview) RunActionPlanPreview {
	value.Status = strings.TrimSpace(value.Status)
	value.Summary = strings.TrimSpace(value.Summary)
	value.Blockers = normalizeRunStringList(value.Blockers, 8)
	for index := range value.Steps {
		value.Steps[index].Title = strings.TrimSpace(value.Steps[index].Title)
		value.Steps[index].Surface = strings.TrimSpace(value.Steps[index].Surface)
		value.Steps[index].Mode = strings.TrimSpace(value.Steps[index].Mode)
		value.Steps[index].Status = strings.TrimSpace(value.Steps[index].Status)
	}
	value.Steps = compactRunPlanSteps(value.Steps)
	for index := range value.Receipts {
		value.Receipts[index].Kind = strings.TrimSpace(value.Receipts[index].Kind)
		value.Receipts[index].Message = strings.TrimSpace(value.Receipts[index].Message)
	}
	value.Receipts = compactRunPlanReceipts(value.Receipts)
	return value
}

func normalizeRunStringList(values []string, limit int) []string {
	if len(values) == 0 || limit <= 0 {
		return nil
	}
	seen := map[string]bool{}
	out := make([]string, 0, assistantMinInt(len(values), limit))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
		if len(out) >= limit {
			break
		}
	}
	return out
}

func compactRunCapabilityContracts(values []RunCapabilityContract) []RunCapabilityContract {
	out := values[:0]
	for _, value := range values {
		if value.ID == "" && value.Capability == "" && value.ActionKind == "" {
			continue
		}
		out = append(out, value)
	}
	return out
}

func compactRunPolicyHints(values []RunPolicyHint) []RunPolicyHint {
	out := values[:0]
	for _, value := range values {
		if value.Reason == "" && value.Effect == "" {
			continue
		}
		out = append(out, value)
	}
	return out
}

func compactRunPlanSteps(values []RunActionPlanStep) []RunActionPlanStep {
	out := values[:0]
	for _, value := range values {
		if value.Title == "" {
			continue
		}
		out = append(out, value)
		if len(out) >= 8 {
			break
		}
	}
	return out
}

func compactRunPlanReceipts(values []RunActionPlanReceipt) []RunActionPlanReceipt {
	out := values[:0]
	for _, value := range values {
		if value.Kind == "" && value.Message == "" {
			continue
		}
		out = append(out, value)
		if len(out) >= 8 {
			break
		}
	}
	return out
}

func normalizeRunCapabilityRoute(value RunCapabilityRoute) RunCapabilityRoute {
	rawAutonomy := strings.TrimSpace(value.Autonomy)
	value.Capability = strings.TrimSpace(value.Capability)
	value.Decision = strings.TrimSpace(value.Decision)
	value.Reason = strings.TrimSpace(value.Reason)
	value.NextStep = strings.TrimSpace(value.NextStep)
	value.Autonomy = normalizeRunAutonomy(value.Autonomy)
	if rawAutonomy == "" {
		value.Autonomy = ""
	}
	return value
}

func normalizeRunStatus(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case RunStatusRunning:
		return RunStatusRunning
	case RunStatusFailed:
		return RunStatusFailed
	default:
		return RunStatusCompleted
	}
}

func normalizeRunDecision(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case RunDecisionRecommend:
		return RunDecisionRecommend
	case RunDecisionCreated:
		return RunDecisionCreated
	default:
		return RunDecisionNoop
	}
}

func normalizeRunAutonomy(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case RunAutonomyExecuteSafe:
		return RunAutonomyExecuteSafe
	case RunAutonomyRunWorkflows:
		return RunAutonomyRunWorkflows
	case RunAutonomyCreateTasks:
		return RunAutonomyCreateTasks
	case RunAutonomyPropose:
		return RunAutonomyPropose
	default:
		return RunAutonomyObserve
	}
}

func firstRunValue(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}

func assistantMinInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}
