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
	GoalStatusActive    = "active"
	GoalStatusPaused    = "paused"
	GoalStatusBlocked   = "blocked"
	GoalStatusCompleted = "completed"
	GoalStatusArchived  = "archived"

	GoalSignalStatusActive    = "active"
	GoalSignalStatusResolved  = "resolved"
	GoalSignalStatusDismissed = "dismissed"

	GoalWatchStatusActive  = "active"
	GoalWatchStatusPaused  = "paused"
	GoalWatchStatusExpired = "expired"
)

type Goal struct {
	ID              string     `json:"id"`
	Title           string     `json:"title"`
	Objective       string     `json:"objective"`
	Details         string     `json:"details,omitempty"`
	Status          string     `json:"status"`
	Kind            string     `json:"kind,omitempty"`
	Priority        string     `json:"priority,omitempty"`
	Autonomy        string     `json:"autonomy"`
	Cadence         string     `json:"cadence,omitempty"`
	NextCheckAt     *time.Time `json:"next_check_at,omitempty"`
	SuccessCriteria []string   `json:"success_criteria,omitempty"`
	Constraints     []string   `json:"constraints,omitempty"`
	LinkedTasks     []string   `json:"linked_tasks,omitempty"`
	LinkedWorkflows []string   `json:"linked_workflows,omitempty"`
	ProgressSummary string     `json:"progress_summary,omitempty"`
	OpenQuestions   []string   `json:"open_questions,omitempty"`
	LastCheckedAt   *time.Time `json:"last_checked_at,omitempty"`
	LastActionAt    *time.Time `json:"last_action_at,omitempty"`
	CreatedBy       string     `json:"created_by,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
	ArchivedAt      *time.Time `json:"archived_at,omitempty"`
}

type GoalCreateRequest struct {
	Title           string   `json:"title"`
	Objective       string   `json:"objective,omitempty"`
	Details         string   `json:"details,omitempty"`
	Kind            string   `json:"kind,omitempty"`
	Priority        string   `json:"priority,omitempty"`
	Autonomy        string   `json:"autonomy,omitempty"`
	Cadence         string   `json:"cadence,omitempty"`
	NextCheckAt     string   `json:"next_check_at,omitempty"`
	SuccessCriteria []string `json:"success_criteria,omitempty"`
	Constraints     []string `json:"constraints,omitempty"`
	OpenQuestions   []string `json:"open_questions,omitempty"`
	CreatedBy       string   `json:"created_by,omitempty"`
}

type GoalUpdateRequest struct {
	Title           string   `json:"title,omitempty"`
	Objective       string   `json:"objective,omitempty"`
	Details         string   `json:"details,omitempty"`
	Status          string   `json:"status,omitempty"`
	Kind            string   `json:"kind,omitempty"`
	Priority        string   `json:"priority,omitempty"`
	Autonomy        string   `json:"autonomy,omitempty"`
	Cadence         string   `json:"cadence,omitempty"`
	NextCheckAt     string   `json:"next_check_at,omitempty"`
	SuccessCriteria []string `json:"success_criteria,omitempty"`
	Constraints     []string `json:"constraints,omitempty"`
	ProgressSummary string   `json:"progress_summary,omitempty"`
	OpenQuestions   []string `json:"open_questions,omitempty"`
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
	ID              string     `json:"id"`
	Title           string     `json:"title"`
	Objective       string     `json:"objective,omitempty"`
	Details         string     `json:"details,omitempty"`
	Status          string     `json:"status,omitempty"`
	Kind            string     `json:"kind,omitempty"`
	Priority        string     `json:"priority,omitempty"`
	Autonomy        string     `json:"autonomy,omitempty"`
	Cadence         string     `json:"cadence,omitempty"`
	NextCheckAt     *time.Time `json:"next_check_at,omitempty"`
	LastCheckedAt   *time.Time `json:"last_checked_at,omitempty"`
	ProgressSummary string     `json:"progress_summary,omitempty"`
	SuccessCriteria []string   `json:"success_criteria,omitempty"`
	Constraints     []string   `json:"constraints,omitempty"`
	OpenQuestions   []string   `json:"open_questions,omitempty"`
	LinkedTasks     []string   `json:"linked_tasks,omitempty"`
	URL             string     `json:"url,omitempty"`
	Due             bool       `json:"due,omitempty"`
}

type GoalTimeline struct {
	Goal        Goal             `json:"goal"`
	Watches     []GoalWatch      `json:"watches,omitempty"`
	Signals     []GoalSignal     `json:"signals,omitempty"`
	Notes       []GoalNote       `json:"notes,omitempty"`
	Assessments []GoalAssessment `json:"assessments,omitempty"`
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

func NormalizeGoal(goal Goal) Goal {
	goal.ID = strings.TrimSpace(goal.ID)
	goal.Title = strings.TrimSpace(goal.Title)
	goal.Objective = strings.TrimSpace(goal.Objective)
	if goal.Objective == "" {
		goal.Objective = goal.Title
	}
	goal.Details = strings.TrimSpace(goal.Details)
	goal.Status = normalizeGoalStatus(goal.Status)
	goal.Kind = strings.TrimSpace(goal.Kind)
	if goal.Kind == "" {
		goal.Kind = "project"
	}
	goal.Priority = strings.TrimSpace(goal.Priority)
	if goal.Priority == "" {
		goal.Priority = "medium"
	}
	goal.Autonomy = normalizeRunAutonomy(goal.Autonomy)
	goal.Cadence = strings.TrimSpace(goal.Cadence)
	goal.SuccessCriteria = normalizeRunStringList(goal.SuccessCriteria, 16)
	goal.Constraints = normalizeRunStringList(goal.Constraints, 16)
	goal.LinkedTasks = normalizeRunStringList(goal.LinkedTasks, 64)
	goal.LinkedWorkflows = normalizeRunStringList(goal.LinkedWorkflows, 64)
	goal.ProgressSummary = strings.TrimSpace(goal.ProgressSummary)
	goal.OpenQuestions = normalizeRunStringList(goal.OpenQuestions, 16)
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

func GoalToSnapshotRef(goal Goal, now time.Time) GoalSnapshotRef {
	goal = NormalizeGoal(goal)
	return GoalSnapshotRef{
		ID:              goal.ID,
		Title:           goal.Title,
		Objective:       goal.Objective,
		Details:         goal.Details,
		Status:          goal.Status,
		Kind:            goal.Kind,
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
