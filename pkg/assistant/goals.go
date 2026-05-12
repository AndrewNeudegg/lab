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

	taskstore "github.com/andrewneudegg/lab/pkg/task"
)

const (
	GoalAutopilotUnlimitedBudget = -1

	GoalStatusActive    = "active"
	GoalStatusPaused    = "paused"
	GoalStatusBlocked   = "blocked"
	GoalStatusCompleted = "completed"
	GoalStatusArchived  = "archived"

	GoalKindBuild       = "build"
	GoalKindRoutine     = "routine"
	GoalKindWatch       = "watch"
	GoalKindMaintenance = "maintenance"

	GoalExecutionModeGuided    = "guided"
	GoalExecutionModeAutopilot = "autopilot"

	GoalAutopilotStatusReady           = "ready"
	GoalAutopilotStatusRunning         = "running"
	GoalAutopilotStatusPaused          = "paused"
	GoalAutopilotStatusBlocked         = "blocked"
	GoalAutopilotStatusCompleted       = "completed"
	GoalAutopilotStatusBudgetExhausted = "budget_exhausted"
	GoalAutopilotStatusStopped         = "stopped"

	GoalPlanStatusActive    = "active"
	GoalPlanStatusBlocked   = "blocked"
	GoalPlanStatusCompleted = "completed"

	GoalPlanPhaseStatusPending    = "pending"
	GoalPlanPhaseStatusInProgress = "in_progress"
	GoalPlanPhaseStatusBlocked    = "blocked"
	GoalPlanPhaseStatusCompleted  = "completed"
	GoalPlanPhaseStatusSkipped    = "skipped"

	GoalSupervisorDecisionCreateTask   = "create_task"
	GoalSupervisorDecisionAskQuestion  = "ask_question"
	GoalSupervisorDecisionMarkComplete = "mark_complete"
	GoalSupervisorDecisionPauseBlocked = "pause_blocked"
	GoalSupervisorDecisionWait         = "wait"
	GoalSupervisorDecisionRevisePlan   = "revise_plan"

	GoalSignalStatusActive    = "active"
	GoalSignalStatusResolved  = "resolved"
	GoalSignalStatusDismissed = "dismissed"

	GoalWatchStatusActive  = "active"
	GoalWatchStatusPaused  = "paused"
	GoalWatchStatusExpired = "expired"
)

type Goal struct {
	ID              string                     `json:"id"`
	Title           string                     `json:"title"`
	Objective       string                     `json:"objective"`
	Details         string                     `json:"details,omitempty"`
	Status          string                     `json:"status"`
	Kind            string                     `json:"kind,omitempty"`
	ExecutionMode   string                     `json:"execution_mode,omitempty"`
	Target          *taskstore.ExecutionTarget `json:"target,omitempty"`
	Autopilot       *GoalAutopilot             `json:"autopilot,omitempty"`
	Plan            *GoalPlan                  `json:"plan,omitempty"`
	Priority        string                     `json:"priority,omitempty"`
	Autonomy        string                     `json:"autonomy"`
	Cadence         string                     `json:"cadence,omitempty"`
	NextCheckAt     *time.Time                 `json:"next_check_at,omitempty"`
	SuccessCriteria []string                   `json:"success_criteria,omitempty"`
	Constraints     []string                   `json:"constraints,omitempty"`
	LinkedTasks     []string                   `json:"linked_tasks,omitempty"`
	LinkedWorkflows []string                   `json:"linked_workflows,omitempty"`
	ProgressSummary string                     `json:"progress_summary,omitempty"`
	OpenQuestions   []string                   `json:"open_questions,omitempty"`
	BlockerTrace    *GoalBlockerTrace          `json:"blocker_trace,omitempty"`
	LastCheckedAt   *time.Time                 `json:"last_checked_at,omitempty"`
	LastActionAt    *time.Time                 `json:"last_action_at,omitempty"`
	CreatedBy       string                     `json:"created_by,omitempty"`
	CreatedAt       time.Time                  `json:"created_at"`
	UpdatedAt       time.Time                  `json:"updated_at"`
	ArchivedAt      *time.Time                 `json:"archived_at,omitempty"`
}

type GoalCreateRequest struct {
	Title           string                     `json:"title"`
	Objective       string                     `json:"objective,omitempty"`
	Details         string                     `json:"details,omitempty"`
	Kind            string                     `json:"kind,omitempty"`
	ExecutionMode   string                     `json:"execution_mode,omitempty"`
	Target          *taskstore.ExecutionTarget `json:"target,omitempty"`
	Autopilot       *GoalAutopilot             `json:"autopilot,omitempty"`
	Priority        string                     `json:"priority,omitempty"`
	Autonomy        string                     `json:"autonomy,omitempty"`
	Cadence         string                     `json:"cadence,omitempty"`
	NextCheckAt     string                     `json:"next_check_at,omitempty"`
	SuccessCriteria []string                   `json:"success_criteria,omitempty"`
	Constraints     []string                   `json:"constraints,omitempty"`
	OpenQuestions   []string                   `json:"open_questions,omitempty"`
	CreatedBy       string                     `json:"created_by,omitempty"`
}

type GoalUpdateRequest struct {
	Title           string                     `json:"title,omitempty"`
	Objective       string                     `json:"objective,omitempty"`
	Details         string                     `json:"details,omitempty"`
	Status          string                     `json:"status,omitempty"`
	Kind            string                     `json:"kind,omitempty"`
	ExecutionMode   string                     `json:"execution_mode,omitempty"`
	Target          *taskstore.ExecutionTarget `json:"target,omitempty"`
	Autopilot       *GoalAutopilot             `json:"autopilot,omitempty"`
	Priority        string                     `json:"priority,omitempty"`
	Autonomy        string                     `json:"autonomy,omitempty"`
	Cadence         string                     `json:"cadence,omitempty"`
	NextCheckAt     string                     `json:"next_check_at,omitempty"`
	SuccessCriteria []string                   `json:"success_criteria,omitempty"`
	Constraints     []string                   `json:"constraints,omitempty"`
	ProgressSummary string                     `json:"progress_summary,omitempty"`
	OpenQuestions   []string                   `json:"open_questions,omitempty"`
	present         map[string]bool
}

type GoalAutopilot struct {
	Status            string     `json:"status,omitempty"`
	BudgetTasks       int        `json:"budget_tasks,omitempty"`
	TasksStarted      int        `json:"tasks_started,omitempty"`
	MaxRuntimeMinutes int        `json:"max_runtime_minutes,omitempty"`
	StartedAt         *time.Time `json:"started_at,omitempty"`
	LastStepAt        *time.Time `json:"last_step_at,omitempty"`
	StopReasons       []string   `json:"stop_reasons,omitempty"`
	AllowedActions    []string   `json:"allowed_actions,omitempty"`
	CurrentTaskID     string     `json:"current_task_id,omitempty"`
	CurrentPhaseID    string     `json:"current_phase_id,omitempty"`
	LastDecisionID    string     `json:"last_decision_id,omitempty"`
}

type GoalBlockerTrace struct {
	Status          string     `json:"status,omitempty"`
	SourceType      string     `json:"source_type,omitempty"`
	SourceID        string     `json:"source_id,omitempty"`
	DecisionID      string     `json:"decision_id,omitempty"`
	Decision        string     `json:"decision,omitempty"`
	GoalID          string     `json:"goal_id,omitempty"`
	PhaseID         string     `json:"phase_id,omitempty"`
	PhaseTitle      string     `json:"phase_title,omitempty"`
	BlockingTaskID  string     `json:"blocking_task_id,omitempty"`
	ReviewDecision  string     `json:"review_decision,omitempty"`
	Reason          string     `json:"reason,omitempty"`
	OperatorAction  string     `json:"operator_action,omitempty"`
	SourceURL       string     `json:"source_url,omitempty"`
	BlockingTaskURL string     `json:"blocking_task_url,omitempty"`
	Blockers        []string   `json:"blockers,omitempty"`
	Questions       []string   `json:"questions,omitempty"`
	Evidence        []string   `json:"evidence,omitempty"`
	FollowUps       []string   `json:"follow_ups,omitempty"`
	CreatedAt       *time.Time `json:"created_at,omitempty"`
}

type GoalPlan struct {
	Status         string          `json:"status,omitempty"`
	Summary        string          `json:"summary,omitempty"`
	CurrentPhaseID string          `json:"current_phase_id,omitempty"`
	Phases         []GoalPlanPhase `json:"phases,omitempty"`
	CreatedAt      time.Time       `json:"created_at,omitempty"`
	UpdatedAt      time.Time       `json:"updated_at,omitempty"`
}

type GoalPlanPhase struct {
	ID                 string   `json:"id"`
	Title              string   `json:"title"`
	Objective          string   `json:"objective,omitempty"`
	Status             string   `json:"status,omitempty"`
	AcceptanceCriteria []string `json:"acceptance_criteria,omitempty"`
	DependsOn          []string `json:"depends_on,omitempty"`
	TaskIDs            []string `json:"task_ids,omitempty"`
	Evidence           []string `json:"evidence,omitempty"`
}

type GoalSupervisorDecision struct {
	ID         string    `json:"id"`
	GoalID     string    `json:"goal_id"`
	Decision   string    `json:"decision"`
	Summary    string    `json:"summary,omitempty"`
	Rationale  string    `json:"rationale,omitempty"`
	PhaseID    string    `json:"phase_id,omitempty"`
	TaskID     string    `json:"task_id,omitempty"`
	TaskGoal   string    `json:"task_goal,omitempty"`
	Questions  []string  `json:"questions,omitempty"`
	StopReason string    `json:"stop_reason,omitempty"`
	Evidence   []string  `json:"evidence,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}

type GoalTaskReport struct {
	ID             string    `json:"id"`
	GoalID         string    `json:"goal_id"`
	TaskID         string    `json:"task_id"`
	PhaseID        string    `json:"phase_id,omitempty"`
	Title          string    `json:"title,omitempty"`
	Status         string    `json:"status,omitempty"`
	Summary        string    `json:"summary,omitempty"`
	AdvancedGoal   bool      `json:"advanced_goal,omitempty"`
	PhaseComplete  bool      `json:"phase_complete,omitempty"`
	GoalComplete   bool      `json:"goal_complete,omitempty"`
	NoChange       bool      `json:"no_change,omitempty"`
	ChangedFiles   []string  `json:"changed_files,omitempty"`
	Validation     []string  `json:"validation,omitempty"`
	FollowUps      []string  `json:"follow_ups,omitempty"`
	Blockers       []string  `json:"blockers,omitempty"`
	Questions      []string  `json:"questions,omitempty"`
	ReviewDecision string    `json:"review_decision,omitempty"`
	ReviewSummary  string    `json:"review_summary,omitempty"`
	ReviewEvidence []string  `json:"review_evidence,omitempty"`
	DiffFiles      int       `json:"diff_files,omitempty"`
	Additions      int       `json:"additions,omitempty"`
	Deletions      int       `json:"deletions,omitempty"`
	ResultExcerpt  string    `json:"result_excerpt,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
}

type GoalAutopilotRequest struct {
	BudgetTasks       int      `json:"budget_tasks,omitempty"`
	MaxRuntimeMinutes int      `json:"max_runtime_minutes,omitempty"`
	AllowedActions    []string `json:"allowed_actions,omitempty"`
}

type GoalWatch struct {
	ID              string     `json:"id"`
	GoalID          string     `json:"goal_id"`
	Title           string     `json:"title"`
	Condition       string     `json:"condition,omitempty"`
	Source          string     `json:"source,omitempty"`
	Cadence         string     `json:"cadence,omitempty"`
	Severity        string     `json:"severity,omitempty"`
	Status          string     `json:"status"`
	ExpiresAt       *time.Time `json:"expires_at,omitempty"`
	OnTrigger       string     `json:"on_trigger,omitempty"`
	SuggestedAction string     `json:"suggested_action,omitempty"`
	LastCheckedAt   *time.Time `json:"last_checked_at,omitempty"`
	LastTriggeredAt *time.Time `json:"last_triggered_at,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

type GoalWatchRequest struct {
	Title           string `json:"title"`
	Condition       string `json:"condition,omitempty"`
	Source          string `json:"source,omitempty"`
	Cadence         string `json:"cadence,omitempty"`
	Severity        string `json:"severity,omitempty"`
	ExpiresAt       string `json:"expires_at,omitempty"`
	OnTrigger       string `json:"on_trigger,omitempty"`
	SuggestedAction string `json:"suggested_action,omitempty"`
}

type GoalSignal struct {
	ID         string              `json:"id"`
	GoalID     string              `json:"goal_id"`
	WatchID    string              `json:"watch_id,omitempty"`
	Kind       string              `json:"kind"`
	Summary    string              `json:"summary"`
	Evidence   []RunSignalEvidence `json:"evidence,omitempty"`
	Severity   string              `json:"severity,omitempty"`
	Status     string              `json:"status"`
	CreatedAt  time.Time           `json:"created_at"`
	UpdatedAt  time.Time           `json:"updated_at"`
	ResolvedAt *time.Time          `json:"resolved_at,omitempty"`
}

type GoalNote struct {
	ID        string    `json:"id"`
	GoalID    string    `json:"goal_id"`
	Kind      string    `json:"kind,omitempty"`
	Title     string    `json:"title,omitempty"`
	Body      string    `json:"body"`
	TaskID    string    `json:"task_id,omitempty"`
	RunID     string    `json:"run_id,omitempty"`
	CreatedBy string    `json:"created_by,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

type GoalNoteRequest struct {
	Kind      string `json:"kind,omitempty"`
	Title     string `json:"title,omitempty"`
	Body      string `json:"body"`
	TaskID    string `json:"task_id,omitempty"`
	RunID     string `json:"run_id,omitempty"`
	CreatedBy string `json:"created_by,omitempty"`
}

type GoalAssessment struct {
	ID          string     `json:"id"`
	GoalID      string     `json:"goal_id"`
	RunID       string     `json:"run_id,omitempty"`
	Trigger     string     `json:"trigger,omitempty"`
	Decision    string     `json:"decision,omitempty"`
	Summary     string     `json:"summary,omitempty"`
	Actions     []string   `json:"actions,omitempty"`
	NextCheckAt *time.Time `json:"next_check_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
}

type GoalSnapshotRef struct {
	ID              string                     `json:"id"`
	Title           string                     `json:"title"`
	Objective       string                     `json:"objective,omitempty"`
	Details         string                     `json:"details,omitempty"`
	Status          string                     `json:"status,omitempty"`
	Kind            string                     `json:"kind,omitempty"`
	ExecutionMode   string                     `json:"execution_mode,omitempty"`
	Target          *taskstore.ExecutionTarget `json:"target,omitempty"`
	Autopilot       *GoalAutopilot             `json:"autopilot,omitempty"`
	Plan            *GoalPlan                  `json:"plan,omitempty"`
	Priority        string                     `json:"priority,omitempty"`
	Autonomy        string                     `json:"autonomy,omitempty"`
	Cadence         string                     `json:"cadence,omitempty"`
	NextCheckAt     *time.Time                 `json:"next_check_at,omitempty"`
	LastCheckedAt   *time.Time                 `json:"last_checked_at,omitempty"`
	ProgressSummary string                     `json:"progress_summary,omitempty"`
	SuccessCriteria []string                   `json:"success_criteria,omitempty"`
	Constraints     []string                   `json:"constraints,omitempty"`
	OpenQuestions   []string                   `json:"open_questions,omitempty"`
	LinkedTasks     []string                   `json:"linked_tasks,omitempty"`
	URL             string                     `json:"url,omitempty"`
	Due             bool                       `json:"due,omitempty"`
}

type GoalTimeline struct {
	Goal         Goal                     `json:"goal"`
	BlockerTrace *GoalBlockerTrace        `json:"blocker_trace,omitempty"`
	Watches      []GoalWatch              `json:"watches,omitempty"`
	Signals      []GoalSignal             `json:"signals,omitempty"`
	Notes        []GoalNote               `json:"notes,omitempty"`
	Assessments  []GoalAssessment         `json:"assessments,omitempty"`
	Decisions    []GoalSupervisorDecision `json:"decisions,omitempty"`
	TaskReports  []GoalTaskReport         `json:"task_reports,omitempty"`
}

type GoalStore struct {
	dir string
	mu  sync.Mutex
}

func NewGoalStore(dir string) *GoalStore {
	return &GoalStore{dir: dir}
}

func (s *GoalStore) SaveGoal(goal Goal) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC()
	previous, hasPrevious, err := s.loadGoalIfExistsLocked(goal.ID)
	if err != nil {
		return err
	}
	if hasPrevious && goal.CreatedAt.IsZero() {
		goal.CreatedAt = previous.CreatedAt
	}
	if hasPrevious && goal.CreatedBy == "" {
		goal.CreatedBy = previous.CreatedBy
	}
	if goal.CreatedAt.IsZero() {
		goal.CreatedAt = now
	}
	if goal.UpdatedAt.IsZero() {
		goal.UpdatedAt = now
	}
	goal = NormalizeGoal(goal)
	goal.BlockerTrace = nil
	return s.writeJSONLocked(s.goalsDir(), goal.ID, goal)
}

func (s *GoalStore) LoadGoal(id string) (Goal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadGoalLocked(id)
}

func (s *GoalStore) loadGoalLocked(id string) (Goal, error) {
	b, err := os.ReadFile(filepath.Join(s.goalsDir(), safeGoalFileID(id)+".json"))
	if err != nil {
		return Goal{}, err
	}
	var goal Goal
	if err := json.Unmarshal(b, &goal); err != nil {
		return Goal{}, err
	}
	return NormalizeGoal(goal), nil
}

func (s *GoalStore) loadGoalIfExistsLocked(id string) (Goal, bool, error) {
	if strings.TrimSpace(id) == "" {
		return Goal{}, false, nil
	}
	goal, err := s.loadGoalLocked(id)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Goal{}, false, nil
		}
		return Goal{}, false, err
	}
	return goal, true, nil
}

func (s *GoalStore) ListGoals() ([]Goal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := os.ReadDir(s.goalsDir())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []Goal{}, nil
		}
		return nil, err
	}
	goals := []Goal{}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		goal, err := s.loadGoalLocked(entry.Name()[:len(entry.Name())-len(".json")])
		if err != nil {
			return nil, err
		}
		goals = append(goals, goal)
	}
	sort.SliceStable(goals, func(i, j int) bool {
		leftRank := goalStatusRank(goals[i].Status)
		rightRank := goalStatusRank(goals[j].Status)
		if leftRank != rightRank {
			return leftRank < rightRank
		}
		if goals[i].UpdatedAt.Equal(goals[j].UpdatedAt) {
			return goals[i].Title < goals[j].Title
		}
		return goals[i].UpdatedAt.After(goals[j].UpdatedAt)
	})
	return goals, nil
}

func (s *GoalStore) SaveWatch(watch GoalWatch) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC()
	if watch.CreatedAt.IsZero() {
		watch.CreatedAt = now
	}
	if watch.UpdatedAt.IsZero() {
		watch.UpdatedAt = now
	}
	watch = NormalizeGoalWatch(watch)
	return s.writeJSONLocked(s.watchesDir(), watch.ID, watch)
}

func (s *GoalStore) LoadWatch(id string) (GoalWatch, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, err := os.ReadFile(filepath.Join(s.watchesDir(), safeGoalFileID(id)+".json"))
	if err != nil {
		return GoalWatch{}, err
	}
	var watch GoalWatch
	if err := json.Unmarshal(b, &watch); err != nil {
		return GoalWatch{}, err
	}
	return NormalizeGoalWatch(watch), nil
}

func (s *GoalStore) ListWatches(goalID string) ([]GoalWatch, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := os.ReadDir(s.watchesDir())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []GoalWatch{}, nil
		}
		return nil, err
	}
	var watches []GoalWatch
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		watch, err := s.loadWatchFileLocked(entry.Name())
		if err != nil {
			return nil, err
		}
		if goalID != "" && watch.GoalID != goalID {
			continue
		}
		watches = append(watches, watch)
	}
	sort.SliceStable(watches, func(i, j int) bool { return watches[i].UpdatedAt.After(watches[j].UpdatedAt) })
	return watches, nil
}

func (s *GoalStore) loadWatchFileLocked(name string) (GoalWatch, error) {
	b, err := os.ReadFile(filepath.Join(s.watchesDir(), name))
	if err != nil {
		return GoalWatch{}, err
	}
	var watch GoalWatch
	if err := json.Unmarshal(b, &watch); err != nil {
		return GoalWatch{}, err
	}
	return NormalizeGoalWatch(watch), nil
}

func (s *GoalStore) SaveSignal(signal GoalSignal) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC()
	if signal.CreatedAt.IsZero() {
		signal.CreatedAt = now
	}
	if signal.UpdatedAt.IsZero() {
		signal.UpdatedAt = now
	}
	signal = NormalizeGoalSignal(signal)
	return s.writeJSONLocked(s.signalsDir(), signal.ID, signal)
}

func (s *GoalStore) ListSignals(goalID string) ([]GoalSignal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := os.ReadDir(s.signalsDir())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []GoalSignal{}, nil
		}
		return nil, err
	}
	var signals []GoalSignal
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		b, err := os.ReadFile(filepath.Join(s.signalsDir(), entry.Name()))
		if err != nil {
			return nil, err
		}
		var signal GoalSignal
		if err := json.Unmarshal(b, &signal); err != nil {
			return nil, err
		}
		signal = NormalizeGoalSignal(signal)
		if goalID != "" && signal.GoalID != goalID {
			continue
		}
		signals = append(signals, signal)
	}
	sort.SliceStable(signals, func(i, j int) bool { return signals[i].UpdatedAt.After(signals[j].UpdatedAt) })
	return signals, nil
}

func (s *GoalStore) SaveNote(note GoalNote) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if note.CreatedAt.IsZero() {
		note.CreatedAt = time.Now().UTC()
	}
	note = NormalizeGoalNote(note)
	return s.writeJSONLocked(s.notesDir(), note.ID, note)
}

func (s *GoalStore) ListNotes(goalID string) ([]GoalNote, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := os.ReadDir(s.notesDir())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []GoalNote{}, nil
		}
		return nil, err
	}
	var notes []GoalNote
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		b, err := os.ReadFile(filepath.Join(s.notesDir(), entry.Name()))
		if err != nil {
			return nil, err
		}
		var note GoalNote
		if err := json.Unmarshal(b, &note); err != nil {
			return nil, err
		}
		note = NormalizeGoalNote(note)
		if goalID != "" && note.GoalID != goalID {
			continue
		}
		notes = append(notes, note)
	}
	sort.SliceStable(notes, func(i, j int) bool { return notes[i].CreatedAt.After(notes[j].CreatedAt) })
	return notes, nil
}

func (s *GoalStore) SaveAssessment(assessment GoalAssessment) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if assessment.CreatedAt.IsZero() {
		assessment.CreatedAt = time.Now().UTC()
	}
	assessment = NormalizeGoalAssessment(assessment)
	return s.writeJSONLocked(s.assessmentsDir(), assessment.ID, assessment)
}

func (s *GoalStore) ListAssessments(goalID string) ([]GoalAssessment, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := os.ReadDir(s.assessmentsDir())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []GoalAssessment{}, nil
		}
		return nil, err
	}
	var assessments []GoalAssessment
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		b, err := os.ReadFile(filepath.Join(s.assessmentsDir(), entry.Name()))
		if err != nil {
			return nil, err
		}
		var assessment GoalAssessment
		if err := json.Unmarshal(b, &assessment); err != nil {
			return nil, err
		}
		assessment = NormalizeGoalAssessment(assessment)
		if goalID != "" && assessment.GoalID != goalID {
			continue
		}
		assessments = append(assessments, assessment)
	}
	sort.SliceStable(assessments, func(i, j int) bool { return assessments[i].CreatedAt.After(assessments[j].CreatedAt) })
	return assessments, nil
}

func (s *GoalStore) SaveDecision(decision GoalSupervisorDecision) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if decision.CreatedAt.IsZero() {
		decision.CreatedAt = time.Now().UTC()
	}
	decision = NormalizeGoalSupervisorDecision(decision)
	return s.writeJSONLocked(s.decisionsDir(), decision.ID, decision)
}

func (s *GoalStore) ListDecisions(goalID string) ([]GoalSupervisorDecision, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := os.ReadDir(s.decisionsDir())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []GoalSupervisorDecision{}, nil
		}
		return nil, err
	}
	var decisions []GoalSupervisorDecision
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		b, err := os.ReadFile(filepath.Join(s.decisionsDir(), entry.Name()))
		if err != nil {
			return nil, err
		}
		var decision GoalSupervisorDecision
		if err := json.Unmarshal(b, &decision); err != nil {
			return nil, err
		}
		decision = NormalizeGoalSupervisorDecision(decision)
		if goalID != "" && decision.GoalID != goalID {
			continue
		}
		decisions = append(decisions, decision)
	}
	sort.SliceStable(decisions, func(i, j int) bool { return decisions[i].CreatedAt.After(decisions[j].CreatedAt) })
	return decisions, nil
}

func (s *GoalStore) SaveTaskReport(report GoalTaskReport) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if report.CreatedAt.IsZero() {
		report.CreatedAt = time.Now().UTC()
	}
	report = NormalizeGoalTaskReport(report)
	return s.writeJSONLocked(s.taskReportsDir(), report.ID, report)
}

func (s *GoalStore) ListTaskReports(goalID string) ([]GoalTaskReport, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := os.ReadDir(s.taskReportsDir())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []GoalTaskReport{}, nil
		}
		return nil, err
	}
	var reports []GoalTaskReport
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		b, err := os.ReadFile(filepath.Join(s.taskReportsDir(), entry.Name()))
		if err != nil {
			return nil, err
		}
		var report GoalTaskReport
		if err := json.Unmarshal(b, &report); err != nil {
			return nil, err
		}
		report = NormalizeGoalTaskReport(report)
		if goalID != "" && report.GoalID != goalID {
			continue
		}
		reports = append(reports, report)
	}
	sort.SliceStable(reports, func(i, j int) bool { return reports[i].CreatedAt.After(reports[j].CreatedAt) })
	return reports, nil
}

func (s *GoalStore) writeJSONLocked(dir, id string, value any) error {
	id = safeGoalFileID(id)
	if id == "" {
		return errors.New("id is required")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, id+".json"), append(b, '\n'), 0o644)
}

func (s *GoalStore) goalsDir() string {
	return filepath.Join(s.dir, "goals")
}

func (s *GoalStore) watchesDir() string {
	return filepath.Join(s.dir, "watches")
}

func (s *GoalStore) signalsDir() string {
	return filepath.Join(s.dir, "signals")
}

func (s *GoalStore) notesDir() string {
	return filepath.Join(s.dir, "notes")
}

func (s *GoalStore) assessmentsDir() string {
	return filepath.Join(s.dir, "assessments")
}

func (s *GoalStore) decisionsDir() string {
	return filepath.Join(s.dir, "decisions")
}

func (s *GoalStore) taskReportsDir() string {
	return filepath.Join(s.dir, "task_reports")
}

func (r *GoalUpdateRequest) UnmarshalJSON(data []byte) error {
	type goalUpdateRequest GoalUpdateRequest
	var decoded goalUpdateRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*r = GoalUpdateRequest(decoded)
	r.present = make(map[string]bool, len(raw))
	for key := range raw {
		r.present[key] = true
	}
	return nil
}

func (r GoalUpdateRequest) HasField(name string) bool {
	if len(r.present) > 0 {
		return r.present[name]
	}
	switch name {
	case "title":
		return r.Title != ""
	case "objective":
		return r.Objective != ""
	case "details":
		return r.Details != ""
	case "status":
		return r.Status != ""
	case "kind":
		return r.Kind != ""
	case "execution_mode":
		return r.ExecutionMode != ""
	case "target":
		return r.Target != nil
	case "autopilot":
		return r.Autopilot != nil
	case "priority":
		return r.Priority != ""
	case "autonomy":
		return r.Autonomy != ""
	case "cadence":
		return r.Cadence != ""
	case "next_check_at":
		return r.NextCheckAt != ""
	case "success_criteria":
		return len(r.SuccessCriteria) > 0
	case "constraints":
		return len(r.Constraints) > 0
	case "progress_summary":
		return r.ProgressSummary != ""
	case "open_questions":
		return len(r.OpenQuestions) > 0
	default:
		return false
	}
}

func NormalizeGoal(goal Goal) Goal {
	goal.ID = strings.TrimSpace(goal.ID)
	goal.Title = strings.TrimSpace(goal.Title)
	goal.Objective = strings.TrimSpace(goal.Objective)
	if goal.Objective == "" {
		goal.Objective = goal.Title
	}
	goal.Details = strings.TrimSpace(goal.Details)
	goal.Status = normalizeGoalStatus(goal.Status)
	goal.Kind = normalizeGoalKind(goal.Kind)
	goal.Target = normalizeExecutionTarget(goal.Target)
	if goal.Autopilot != nil && strings.TrimSpace(goal.ExecutionMode) == "" {
		goal.ExecutionMode = GoalExecutionModeAutopilot
	}
	goal.ExecutionMode = normalizeGoalExecutionMode(goal.ExecutionMode)
	if goal.ExecutionMode == GoalExecutionModeAutopilot {
		autopilot := NormalizeGoalAutopilot(goal.Autopilot)
		goal.Autopilot = &autopilot
	} else {
		goal.Autopilot = nil
	}
	if goal.Plan != nil {
		plan := NormalizeGoalPlan(*goal.Plan)
		if len(plan.Phases) > 0 || plan.Summary != "" {
			goal.Plan = &plan
		} else {
			goal.Plan = nil
		}
	}
	goal.Priority = strings.TrimSpace(goal.Priority)
	if goal.Priority == "" {
		goal.Priority = "medium"
	}
	goal.Autonomy = normalizeRunAutonomy(goal.Autonomy)
	goal.Cadence = strings.TrimSpace(goal.Cadence)
	goal.SuccessCriteria = normalizeRunStringList(goal.SuccessCriteria, 16)
	goal.Constraints = normalizeRunStringList(goal.Constraints, 16)
	goal.LinkedTasks = normalizeRunRecentStringList(goal.LinkedTasks, 64)
	goal.LinkedWorkflows = normalizeRunStringList(goal.LinkedWorkflows, 64)
	goal.ProgressSummary = strings.TrimSpace(goal.ProgressSummary)
	goal.OpenQuestions = normalizeRunStringList(goal.OpenQuestions, 16)
	if goalClearsOpenQuestions(goal) {
		goal.OpenQuestions = nil
	}
	goal.CreatedBy = strings.TrimSpace(goal.CreatedBy)
	goal.CreatedAt = goal.CreatedAt.UTC()
	goal.UpdatedAt = goal.UpdatedAt.UTC()
	if goal.NextCheckAt != nil {
		next := goal.NextCheckAt.UTC()
		if next.IsZero() {
			goal.NextCheckAt = nil
		} else {
			goal.NextCheckAt = &next
		}
	}
	if goal.LastCheckedAt != nil {
		checked := goal.LastCheckedAt.UTC()
		if checked.IsZero() {
			goal.LastCheckedAt = nil
		} else {
			goal.LastCheckedAt = &checked
		}
	}
	if goal.LastActionAt != nil {
		action := goal.LastActionAt.UTC()
		if action.IsZero() {
			goal.LastActionAt = nil
		} else {
			goal.LastActionAt = &action
		}
	}
	if goal.ArchivedAt != nil {
		archived := goal.ArchivedAt.UTC()
		if archived.IsZero() || goal.Status != GoalStatusArchived {
			goal.ArchivedAt = nil
		} else {
			goal.ArchivedAt = &archived
		}
	}
	return goal
}

func goalClearsOpenQuestions(goal Goal) bool {
	if goal.Status == GoalStatusCompleted || goal.Status == GoalStatusArchived {
		return true
	}
	if goal.Autopilot != nil && goal.Autopilot.Status == GoalAutopilotStatusCompleted {
		return true
	}
	return goal.Plan != nil && NormalizeGoalPlan(*goal.Plan).Status == GoalPlanStatusCompleted
}

func NormalizeGoalAutopilot(autopilot *GoalAutopilot) GoalAutopilot {
	value := GoalAutopilot{}
	if autopilot != nil {
		value = *autopilot
	}
	value.Status = normalizeGoalAutopilotStatus(value.Status)
	if value.BudgetTasks < 0 {
		value.BudgetTasks = GoalAutopilotUnlimitedBudget
	}
	if value.BudgetTasks == 0 {
		value.BudgetTasks = 1
	}
	if value.TasksStarted < 0 {
		value.TasksStarted = 0
	}
	if value.MaxRuntimeMinutes < 0 {
		value.MaxRuntimeMinutes = 0
	}
	value.StopReasons = normalizeRunStringList(value.StopReasons, 16)
	value.AllowedActions = normalizeRunStringList(value.AllowedActions, 16)
	if len(value.AllowedActions) == 0 {
		value.AllowedActions = []string{
			"create_task",
			"run_task",
			"review_task",
			"approve_merge",
			"accept_task",
			"reflect_goal",
		}
	}
	value.CurrentTaskID = strings.TrimSpace(value.CurrentTaskID)
	value.CurrentPhaseID = strings.TrimSpace(value.CurrentPhaseID)
	value.LastDecisionID = strings.TrimSpace(value.LastDecisionID)
	if value.StartedAt != nil {
		started := value.StartedAt.UTC()
		if started.IsZero() {
			value.StartedAt = nil
		} else {
			value.StartedAt = &started
		}
	}
	if value.LastStepAt != nil {
		step := value.LastStepAt.UTC()
		if step.IsZero() {
			value.LastStepAt = nil
		} else {
			value.LastStepAt = &step
		}
	}
	return value
}

func NormalizeGoalPlan(plan GoalPlan) GoalPlan {
	plan.Status = normalizeGoalPlanStatus(plan.Status)
	plan.Summary = strings.TrimSpace(plan.Summary)
	plan.CurrentPhaseID = strings.TrimSpace(plan.CurrentPhaseID)
	if plan.CreatedAt.IsZero() {
		plan.CreatedAt = time.Now().UTC()
	} else {
		plan.CreatedAt = plan.CreatedAt.UTC()
	}
	if plan.UpdatedAt.IsZero() {
		plan.UpdatedAt = plan.CreatedAt
	} else {
		plan.UpdatedAt = plan.UpdatedAt.UTC()
	}
	phases := make([]GoalPlanPhase, 0, len(plan.Phases))
	seen := map[string]bool{}
	for index, phase := range plan.Phases {
		phase = NormalizeGoalPlanPhase(phase, index)
		if phase.ID == "" || seen[phase.ID] {
			continue
		}
		seen[phase.ID] = true
		phases = append(phases, phase)
		if plan.CurrentPhaseID == "" && phase.Status != GoalPlanPhaseStatusCompleted && phase.Status != GoalPlanPhaseStatusSkipped {
			plan.CurrentPhaseID = phase.ID
		}
	}
	plan.Phases = phases
	if len(plan.Phases) == 0 {
		plan.CurrentPhaseID = ""
	}
	return plan
}

func NormalizeGoalPlanPhase(phase GoalPlanPhase, index int) GoalPlanPhase {
	phase.ID = strings.TrimSpace(phase.ID)
	if phase.ID == "" {
		phase.ID = "phase_" + strconv.Itoa(index+1)
	}
	phase.Title = strings.TrimSpace(phase.Title)
	if phase.Title == "" {
		phase.Title = "Phase " + strconv.Itoa(index+1)
	}
	phase.Objective = strings.TrimSpace(phase.Objective)
	phase.Status = normalizeGoalPlanPhaseStatus(phase.Status)
	phase.AcceptanceCriteria = normalizeRunStringList(phase.AcceptanceCriteria, 12)
	phase.DependsOn = normalizeRunStringList(phase.DependsOn, 12)
	phase.TaskIDs = normalizeRunStringList(phase.TaskIDs, 24)
	phase.Evidence = normalizeRunStringList(phase.Evidence, 24)
	return phase
}

func NormalizeGoalSupervisorDecision(decision GoalSupervisorDecision) GoalSupervisorDecision {
	decision.ID = strings.TrimSpace(decision.ID)
	decision.GoalID = strings.TrimSpace(decision.GoalID)
	decision.Decision = normalizeGoalSupervisorDecision(decision.Decision)
	decision.Summary = strings.TrimSpace(decision.Summary)
	decision.Rationale = strings.TrimSpace(decision.Rationale)
	decision.PhaseID = strings.TrimSpace(decision.PhaseID)
	decision.TaskID = strings.TrimSpace(decision.TaskID)
	decision.TaskGoal = strings.TrimSpace(decision.TaskGoal)
	decision.Questions = normalizeRunStringList(decision.Questions, 12)
	decision.StopReason = strings.TrimSpace(decision.StopReason)
	decision.Evidence = normalizeRunStringList(decision.Evidence, 24)
	decision.CreatedAt = decision.CreatedAt.UTC()
	return decision
}

func NormalizeGoalTaskReport(report GoalTaskReport) GoalTaskReport {
	report.ID = strings.TrimSpace(report.ID)
	report.GoalID = strings.TrimSpace(report.GoalID)
	report.TaskID = strings.TrimSpace(report.TaskID)
	report.PhaseID = strings.TrimSpace(report.PhaseID)
	report.Title = strings.TrimSpace(report.Title)
	report.Status = strings.TrimSpace(report.Status)
	report.Summary = strings.TrimSpace(report.Summary)
	report.ChangedFiles = normalizeRunStringList(report.ChangedFiles, 64)
	report.Validation = normalizeRunStringList(report.Validation, 24)
	report.FollowUps = normalizeRunStringList(report.FollowUps, 24)
	report.Blockers = normalizeRunStringList(report.Blockers, 24)
	report.Questions = normalizeRunStringList(report.Questions, 12)
	report.ReviewDecision = strings.TrimSpace(report.ReviewDecision)
	report.ReviewSummary = strings.TrimSpace(report.ReviewSummary)
	report.ReviewEvidence = normalizeRunStringList(report.ReviewEvidence, 24)
	report.ResultExcerpt = strings.TrimSpace(report.ResultExcerpt)
	if report.DiffFiles < 0 {
		report.DiffFiles = 0
	}
	if report.Additions < 0 {
		report.Additions = 0
	}
	if report.Deletions < 0 {
		report.Deletions = 0
	}
	report.CreatedAt = report.CreatedAt.UTC()
	return report
}

func NormalizeGoalWatch(watch GoalWatch) GoalWatch {
	watch.ID = strings.TrimSpace(watch.ID)
	watch.GoalID = strings.TrimSpace(watch.GoalID)
	watch.Title = strings.TrimSpace(watch.Title)
	watch.Condition = strings.TrimSpace(watch.Condition)
	watch.Source = strings.TrimSpace(watch.Source)
	watch.Cadence = strings.TrimSpace(watch.Cadence)
	watch.Severity = strings.TrimSpace(watch.Severity)
	if watch.Severity == "" {
		watch.Severity = "info"
	}
	watch.Status = normalizeGoalWatchStatus(watch.Status)
	watch.OnTrigger = strings.TrimSpace(watch.OnTrigger)
	if watch.OnTrigger == "" {
		watch.OnTrigger = "create_signal"
	}
	watch.SuggestedAction = strings.TrimSpace(watch.SuggestedAction)
	watch.CreatedAt = watch.CreatedAt.UTC()
	watch.UpdatedAt = watch.UpdatedAt.UTC()
	if watch.ExpiresAt != nil {
		expires := watch.ExpiresAt.UTC()
		if expires.IsZero() {
			watch.ExpiresAt = nil
		} else {
			watch.ExpiresAt = &expires
		}
	}
	if watch.LastCheckedAt != nil {
		checked := watch.LastCheckedAt.UTC()
		if checked.IsZero() {
			watch.LastCheckedAt = nil
		} else {
			watch.LastCheckedAt = &checked
		}
	}
	if watch.LastTriggeredAt != nil {
		triggered := watch.LastTriggeredAt.UTC()
		if triggered.IsZero() {
			watch.LastTriggeredAt = nil
		} else {
			watch.LastTriggeredAt = &triggered
		}
	}
	return watch
}

func NormalizeGoalSignal(signal GoalSignal) GoalSignal {
	signal.ID = strings.TrimSpace(signal.ID)
	signal.GoalID = strings.TrimSpace(signal.GoalID)
	signal.WatchID = strings.TrimSpace(signal.WatchID)
	signal.Kind = strings.TrimSpace(signal.Kind)
	if signal.Kind == "" {
		signal.Kind = "goal_signal"
	}
	signal.Summary = strings.TrimSpace(signal.Summary)
	signal.Severity = strings.TrimSpace(signal.Severity)
	signal.Status = normalizeGoalSignalStatus(signal.Status)
	signal.Evidence = normalizeRunSignalEvidenceList(signal.Evidence)
	signal.CreatedAt = signal.CreatedAt.UTC()
	signal.UpdatedAt = signal.UpdatedAt.UTC()
	if signal.ResolvedAt != nil {
		resolved := signal.ResolvedAt.UTC()
		if resolved.IsZero() {
			signal.ResolvedAt = nil
		} else {
			signal.ResolvedAt = &resolved
		}
	}
	return signal
}

func NormalizeGoalNote(note GoalNote) GoalNote {
	note.ID = strings.TrimSpace(note.ID)
	note.GoalID = strings.TrimSpace(note.GoalID)
	note.Kind = strings.TrimSpace(note.Kind)
	if note.Kind == "" {
		note.Kind = "note"
	}
	note.Title = strings.TrimSpace(note.Title)
	note.Body = strings.TrimSpace(note.Body)
	note.TaskID = strings.TrimSpace(note.TaskID)
	note.RunID = strings.TrimSpace(note.RunID)
	note.CreatedBy = strings.TrimSpace(note.CreatedBy)
	note.CreatedAt = note.CreatedAt.UTC()
	return note
}

func NormalizeGoalAssessment(assessment GoalAssessment) GoalAssessment {
	assessment.ID = strings.TrimSpace(assessment.ID)
	assessment.GoalID = strings.TrimSpace(assessment.GoalID)
	assessment.RunID = strings.TrimSpace(assessment.RunID)
	assessment.Trigger = strings.TrimSpace(assessment.Trigger)
	assessment.Decision = strings.TrimSpace(assessment.Decision)
	assessment.Summary = strings.TrimSpace(assessment.Summary)
	assessment.Actions = normalizeRunStringList(assessment.Actions, 16)
	assessment.CreatedAt = assessment.CreatedAt.UTC()
	if assessment.NextCheckAt != nil {
		next := assessment.NextCheckAt.UTC()
		if next.IsZero() {
			assessment.NextCheckAt = nil
		} else {
			assessment.NextCheckAt = &next
		}
	}
	return assessment
}

func normalizeRunRecentStringList(values []string, limit int) []string {
	if len(values) == 0 || limit <= 0 {
		return nil
	}
	seen := map[string]bool{}
	out := make([]string, 0, assistantMinInt(len(values), limit))
	for i := len(values) - 1; i >= 0; i-- {
		value := strings.TrimSpace(values[i])
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
		if len(out) >= limit {
			break
		}
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

func GoalToSnapshotRef(goal Goal, now time.Time) GoalSnapshotRef {
	goal = NormalizeGoal(goal)
	return GoalSnapshotRef{
		ID:              goal.ID,
		Title:           goal.Title,
		Objective:       goal.Objective,
		Details:         goal.Details,
		Status:          goal.Status,
		Kind:            goal.Kind,
		ExecutionMode:   goal.ExecutionMode,
		Target:          normalizeExecutionTarget(goal.Target),
		Autopilot:       cloneGoalAutopilot(goal.Autopilot),
		Plan:            cloneGoalPlan(goal.Plan),
		Priority:        goal.Priority,
		Autonomy:        goal.Autonomy,
		Cadence:         goal.Cadence,
		NextCheckAt:     cloneGoalTime(goal.NextCheckAt),
		LastCheckedAt:   cloneGoalTime(goal.LastCheckedAt),
		ProgressSummary: goal.ProgressSummary,
		SuccessCriteria: append([]string(nil), goal.SuccessCriteria...),
		Constraints:     append([]string(nil), goal.Constraints...),
		OpenQuestions:   append([]string(nil), goal.OpenQuestions...),
		LinkedTasks:     append([]string(nil), goal.LinkedTasks...),
		URL:             "/assistant?goal=" + goal.ID,
		Due:             GoalIsDue(goal, now),
	}
}

func GoalIsDue(goal Goal, now time.Time) bool {
	goal = NormalizeGoal(goal)
	if goal.Status != GoalStatusActive && goal.Status != GoalStatusBlocked {
		return false
	}
	if goal.NextCheckAt == nil {
		return true
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	return !goal.NextCheckAt.After(now.UTC())
}

func GoalNextCheckTime(goal Goal, from time.Time) *time.Time {
	goal = NormalizeGoal(goal)
	if from.IsZero() {
		from = time.Now().UTC()
	}
	duration := goalCadenceDuration(goal.Cadence)
	if duration <= 0 {
		return nil
	}
	next := from.UTC().Add(duration)
	return &next
}

func ParseGoalTime(value string) (*time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return nil, err
	}
	parsed = parsed.UTC()
	return &parsed, nil
}

func normalizeGoalStatus(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case GoalStatusPaused:
		return GoalStatusPaused
	case GoalStatusBlocked:
		return GoalStatusBlocked
	case GoalStatusCompleted:
		return GoalStatusCompleted
	case GoalStatusArchived:
		return GoalStatusArchived
	default:
		return GoalStatusActive
	}
}

func normalizeGoalKind(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "project", "feature", "implementation", GoalKindBuild:
		return GoalKindBuild
	case "recurring", GoalKindRoutine:
		return GoalKindRoutine
	case "monitor", "monitoring", GoalKindWatch:
		return GoalKindWatch
	case "maintain", "upkeep", GoalKindMaintenance:
		return GoalKindMaintenance
	default:
		return strings.ToLower(strings.TrimSpace(value))
	}
}

func normalizeGoalExecutionMode(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case GoalExecutionModeAutopilot, "auto", "autonomous", "human_out_of_loop", "human-out-of-loop":
		return GoalExecutionModeAutopilot
	default:
		return GoalExecutionModeGuided
	}
}

func normalizeGoalAutopilotStatus(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case GoalAutopilotStatusRunning:
		return GoalAutopilotStatusRunning
	case GoalAutopilotStatusPaused:
		return GoalAutopilotStatusPaused
	case GoalAutopilotStatusBlocked:
		return GoalAutopilotStatusBlocked
	case GoalAutopilotStatusCompleted:
		return GoalAutopilotStatusCompleted
	case GoalAutopilotStatusBudgetExhausted, "budget-exhausted":
		return GoalAutopilotStatusBudgetExhausted
	case GoalAutopilotStatusStopped:
		return GoalAutopilotStatusStopped
	default:
		return GoalAutopilotStatusReady
	}
}

func normalizeGoalPlanStatus(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case GoalPlanStatusBlocked:
		return GoalPlanStatusBlocked
	case GoalPlanStatusCompleted:
		return GoalPlanStatusCompleted
	default:
		return GoalPlanStatusActive
	}
}

func normalizeGoalPlanPhaseStatus(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case GoalPlanPhaseStatusInProgress, "running":
		return GoalPlanPhaseStatusInProgress
	case GoalPlanPhaseStatusBlocked:
		return GoalPlanPhaseStatusBlocked
	case GoalPlanPhaseStatusCompleted, "done":
		return GoalPlanPhaseStatusCompleted
	case GoalPlanPhaseStatusSkipped:
		return GoalPlanPhaseStatusSkipped
	default:
		return GoalPlanPhaseStatusPending
	}
}

func normalizeGoalSupervisorDecision(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case GoalSupervisorDecisionAskQuestion:
		return GoalSupervisorDecisionAskQuestion
	case GoalSupervisorDecisionMarkComplete:
		return GoalSupervisorDecisionMarkComplete
	case GoalSupervisorDecisionPauseBlocked:
		return GoalSupervisorDecisionPauseBlocked
	case GoalSupervisorDecisionWait:
		return GoalSupervisorDecisionWait
	case GoalSupervisorDecisionRevisePlan:
		return GoalSupervisorDecisionRevisePlan
	default:
		return GoalSupervisorDecisionCreateTask
	}
}

func normalizeGoalWatchStatus(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case GoalWatchStatusPaused:
		return GoalWatchStatusPaused
	case GoalWatchStatusExpired:
		return GoalWatchStatusExpired
	default:
		return GoalWatchStatusActive
	}
}

func normalizeGoalSignalStatus(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case GoalSignalStatusResolved:
		return GoalSignalStatusResolved
	case GoalSignalStatusDismissed:
		return GoalSignalStatusDismissed
	default:
		return GoalSignalStatusActive
	}
}

func goalStatusRank(status string) int {
	switch normalizeGoalStatus(status) {
	case GoalStatusActive:
		return 0
	case GoalStatusBlocked:
		return 1
	case GoalStatusPaused:
		return 2
	case GoalStatusCompleted:
		return 3
	case GoalStatusArchived:
		return 4
	default:
		return 5
	}
}

func goalCadenceDuration(cadence string) time.Duration {
	switch strings.ToLower(strings.TrimSpace(cadence)) {
	case "", "manual", "none", "never":
		return 0
	case "hourly":
		return time.Hour
	case "daily":
		return 24 * time.Hour
	case "weekly":
		return 7 * 24 * time.Hour
	case "monthly":
		return 30 * 24 * time.Hour
	}
	if duration, err := time.ParseDuration(cadence); err == nil {
		return duration
	}
	if days, err := strconv.Atoi(strings.TrimSuffix(cadence, "d")); err == nil && strings.HasSuffix(cadence, "d") && days > 0 {
		return time.Duration(days) * 24 * time.Hour
	}
	return 0
}

func safeGoalFileID(id string) string {
	id = strings.TrimSpace(id)
	id = strings.ReplaceAll(id, string(filepath.Separator), "_")
	id = strings.ReplaceAll(id, "..", "_")
	return id
}

func cloneGoalTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	cloned := value.UTC()
	return &cloned
}

func cloneGoalAutopilot(value *GoalAutopilot) *GoalAutopilot {
	if value == nil {
		return nil
	}
	cloned := NormalizeGoalAutopilot(value)
	cloned.StopReasons = append([]string(nil), cloned.StopReasons...)
	cloned.AllowedActions = append([]string(nil), cloned.AllowedActions...)
	return &cloned
}

func cloneGoalPlan(value *GoalPlan) *GoalPlan {
	if value == nil {
		return nil
	}
	cloned := NormalizeGoalPlan(*value)
	cloned.Phases = append([]GoalPlanPhase(nil), cloned.Phases...)
	for index := range cloned.Phases {
		cloned.Phases[index].AcceptanceCriteria = append([]string(nil), cloned.Phases[index].AcceptanceCriteria...)
		cloned.Phases[index].DependsOn = append([]string(nil), cloned.Phases[index].DependsOn...)
		cloned.Phases[index].TaskIDs = append([]string(nil), cloned.Phases[index].TaskIDs...)
		cloned.Phases[index].Evidence = append([]string(nil), cloned.Phases[index].Evidence...)
	}
	return &cloned
}
