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

	RunAutonomyObserve     = "observe"
	RunAutonomyPropose     = "propose"
	RunAutonomyCreateTasks = "create_tasks"
)

type RunRequest struct {
	TriggerKind  string `json:"trigger_kind,omitempty"`
	TriggerLabel string `json:"trigger_label,omitempty"`
	Goal         string `json:"goal,omitempty"`
	Autonomy     string `json:"autonomy,omitempty"`
}

type Run struct {
	ID                 string       `json:"id"`
	Status             string       `json:"status"`
	Decision           string       `json:"decision"`
	Trigger            RunTrigger   `json:"trigger"`
	Autonomy           string       `json:"autonomy"`
	Goal               string       `json:"goal,omitempty"`
	Summary            string       `json:"summary"`
	Changed            []string     `json:"changed,omitempty"`
	Concerns           []RunFinding `json:"concerns,omitempty"`
	Opportunities      []RunFinding `json:"opportunities,omitempty"`
	RecommendedActions []RunAction  `json:"recommended_actions,omitempty"`
	Receipts           []RunReceipt `json:"receipts,omitempty"`
	Snapshot           RunSnapshot  `json:"snapshot"`
	Error              string       `json:"error,omitempty"`
	Provider           string       `json:"provider,omitempty"`
	Model              string       `json:"model,omitempty"`
	Usage              RunUsage     `json:"usage,omitempty"`
	CreatedAt          time.Time    `json:"created_at"`
	StartedAt          time.Time    `json:"started_at,omitempty"`
	FinishedAt         time.Time    `json:"finished_at,omitempty"`
	UpdatedAt          time.Time    `json:"updated_at"`
}

type RunTrigger struct {
	Kind  string `json:"kind"`
	Label string `json:"label"`
}

type RunFinding struct {
	Title     string `json:"title"`
	Detail    string `json:"detail,omitempty"`
	Severity  string `json:"severity,omitempty"`
	Surface   string `json:"surface,omitempty"`
	ObjectID  string `json:"object_id,omitempty"`
	ObjectURL string `json:"object_url,omitempty"`
}

type RunAction struct {
	ID             string    `json:"id"`
	Fingerprint    string    `json:"fingerprint,omitempty"`
	Kind           string    `json:"kind"`
	Title          string    `json:"title"`
	Rationale      string    `json:"rationale"`
	Priority       string    `json:"priority,omitempty"`
	Risk           string    `json:"risk,omitempty"`
	TargetSurface  string    `json:"target_surface,omitempty"`
	TaskGoal       string    `json:"task_goal,omitempty"`
	KnowledgeQuery string    `json:"knowledge_query,omitempty"`
	WorkflowHint   string    `json:"workflow_hint,omitempty"`
	Status         string    `json:"status,omitempty"`
	CreatedTaskID  string    `json:"created_task_id,omitempty"`
	SeenCount      int       `json:"seen_count,omitempty"`
	UsefulCount    int       `json:"useful_count,omitempty"`
	SnoozedUntil   time.Time `json:"snoozed_until,omitempty"`
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

func NormalizeRun(run Run) Run {
	run.ID = strings.TrimSpace(run.ID)
	run.Status = normalizeRunStatus(run.Status)
	run.Decision = normalizeRunDecision(run.Decision)
	run.Autonomy = normalizeRunAutonomy(run.Autonomy)
	run.Trigger.Kind = firstRunValue(run.Trigger.Kind, "manual")
	run.Trigger.Label = firstRunValue(run.Trigger.Label, "Manual proactive check")
	run.Goal = strings.TrimSpace(run.Goal)
	run.Summary = strings.TrimSpace(run.Summary)
	run.Error = strings.TrimSpace(run.Error)
	run.Provider = strings.TrimSpace(run.Provider)
	run.Model = strings.TrimSpace(run.Model)
	if run.Snapshot.TaskCounts == nil {
		run.Snapshot.TaskCounts = map[string]int{}
	}
	if run.Snapshot.WorkflowCounts == nil {
		run.Snapshot.WorkflowCounts = map[string]int{}
	}
	if run.Snapshot.RemoteAgentCounts == nil {
		run.Snapshot.RemoteAgentCounts = map[string]int{}
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

func normalizeRunFinding(value RunFinding) RunFinding {
	value.Title = strings.TrimSpace(value.Title)
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
	value.Kind = firstRunValue(value.Kind, "observe")
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
	if value.SeenCount < 0 {
		value.SeenCount = 0
	}
	if value.UsefulCount < 0 {
		value.UsefulCount = 0
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
