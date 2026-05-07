package assistant

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	SignalStatusActive      = "active"
	SignalStatusDismissed   = "dismissed"
	SignalStatusSnoozed     = "snoozed"
	SignalStatusUseful      = "useful"
	SignalStatusCreatedTask = "created_task"

	SignalFeedbackUseful     = "useful"
	SignalFeedbackDismiss    = "dismiss"
	SignalFeedbackSnooze     = "snooze"
	SignalFeedbackCreateTask = "create_task"

	DefaultSignalCandidateTTL = 7 * 24 * time.Hour
	MaxSignalCandidateTTL     = 30 * 24 * time.Hour
)

type SignalRecord struct {
	Fingerprint      string    `json:"fingerprint"`
	Status           string    `json:"status"`
	Kind             string    `json:"kind,omitempty"`
	Title            string    `json:"title"`
	Surface          string    `json:"surface,omitempty"`
	FirstSeenAt      time.Time `json:"first_seen_at"`
	LastSeenAt       time.Time `json:"last_seen_at"`
	SeenCount        int       `json:"seen_count"`
	UsefulCount      int       `json:"useful_count,omitempty"`
	DismissedCount   int       `json:"dismissed_count,omitempty"`
	SnoozedCount     int       `json:"snoozed_count,omitempty"`
	CreatedTaskCount int       `json:"created_task_count,omitempty"`
	SnoozedUntil     time.Time `json:"snoozed_until,omitempty"`
	DismissedAt      time.Time `json:"dismissed_at,omitempty"`
	CreatedTaskID    string    `json:"created_task_id,omitempty"`
	LastFeedback     string    `json:"last_feedback,omitempty"`
	PolicyHint       string    `json:"policy_hint,omitempty"`
	LastRunID        string    `json:"last_run_id,omitempty"`
	LastActionID     string    `json:"last_action_id,omitempty"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type SignalFeedbackRequest struct {
	Feedback      string `json:"feedback"`
	SnoozeSeconds int    `json:"snooze_seconds,omitempty"`
}

type SignalSubmitRequest struct {
	Fingerprint       string              `json:"fingerprint,omitempty"`
	Source            string              `json:"source,omitempty"`
	Kind              string              `json:"kind,omitempty"`
	Title             string              `json:"title"`
	Detail            string              `json:"detail,omitempty"`
	WhyNow            string              `json:"why_now,omitempty"`
	Severity          string              `json:"severity,omitempty"`
	Surface           string              `json:"surface,omitempty"`
	ObjectID          string              `json:"object_id,omitempty"`
	ObjectURL         string              `json:"object_url,omitempty"`
	Score             int                 `json:"score,omitempty"`
	Confidence        string              `json:"confidence,omitempty"`
	Priority          string              `json:"priority,omitempty"`
	ActionKind        string              `json:"action_kind,omitempty"`
	Rationale         string              `json:"rationale,omitempty"`
	TaskGoal          string              `json:"task_goal,omitempty"`
	Evidence          []RunSignalEvidence `json:"evidence,omitempty"`
	SafeActions       []string            `json:"safe_actions,omitempty"`
	SuggestedNextStep string              `json:"suggested_next_step,omitempty"`
	ObservedAt        time.Time           `json:"observed_at,omitempty"`
	ExpiresAt         time.Time           `json:"expires_at,omitempty"`
	TTLSeconds        int                 `json:"ttl_seconds,omitempty"`
}

type SignalCandidate struct {
	RunSignal
	Source          string    `json:"source,omitempty"`
	FirstObservedAt time.Time `json:"first_observed_at,omitempty"`
	LastObservedAt  time.Time `json:"last_observed_at,omitempty"`
	ExpiresAt       time.Time `json:"expires_at,omitempty"`
	CreatedAt       time.Time `json:"created_at,omitempty"`
	UpdatedAt       time.Time `json:"updated_at,omitempty"`
}

type SignalStore struct {
	dir string
	mu  sync.Mutex
}

type SignalCandidateStore struct {
	dir string
	mu  sync.Mutex
}

func NewSignalStore(dir string) *SignalStore {
	return &SignalStore{dir: dir}
}

func NewSignalCandidateStore(dir string) *SignalCandidateStore {
	return &SignalCandidateStore{dir: dir}
}

func (s *SignalCandidateStore) ListActive(now time.Time) ([]SignalCandidate, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []SignalCandidate{}, nil
		}
		return nil, err
	}
	candidates := []SignalCandidate{}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		candidate, err := s.loadPathLocked(filepath.Join(s.dir, entry.Name()), now)
		if err != nil {
			return nil, err
		}
		if candidate.ExpiresAt.IsZero() || candidate.ExpiresAt.After(now) {
			candidates = append(candidates, candidate)
		}
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].Score != candidates[j].Score {
			return candidates[i].Score > candidates[j].Score
		}
		return candidates[i].UpdatedAt.After(candidates[j].UpdatedAt)
	})
	return candidates, nil
}

func (s *SignalCandidateStore) Load(fingerprint string, now time.Time) (SignalCandidate, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadLocked(fingerprint, now)
}

func (s *SignalCandidateStore) Upsert(req SignalSubmitRequest, now time.Time) (SignalCandidate, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	candidate := SignalCandidateFromSubmitRequest(req, now)
	existing, exists, err := s.loadIfExistsLocked(candidate.Fingerprint, now)
	if err != nil {
		return SignalCandidate{}, err
	}
	if exists {
		candidate.CreatedAt = existing.CreatedAt
		candidate.FirstObservedAt = existing.FirstObservedAt
		candidate.SeenCount = existing.SeenCount + 1
		candidate.Evidence = mergeRunSignalEvidence(candidate.Evidence, existing.Evidence, 8)
		candidate.UsefulCount = existing.UsefulCount
	} else {
		candidate.SeenCount = assistantMaxInt(1, candidate.SeenCount)
	}
	candidate.UpdatedAt = now
	candidate.LastObservedAt = now
	candidate = NormalizeSignalCandidate(candidate, now)
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return SignalCandidate{}, err
	}
	b, err := json.MarshalIndent(candidate, "", "  ")
	if err != nil {
		return SignalCandidate{}, err
	}
	if err := os.WriteFile(filepath.Join(s.dir, signalCandidateFileName(candidate.Fingerprint)+".json"), append(b, '\n'), 0o644); err != nil {
		return SignalCandidate{}, err
	}
	return candidate, nil
}

func (s *SignalCandidateStore) Save(candidate SignalCandidate, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	candidate.UpdatedAt = now
	candidate = NormalizeSignalCandidate(candidate, now)
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(candidate, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(s.dir, signalCandidateFileName(candidate.Fingerprint)+".json"), append(b, '\n'), 0o644)
}

func (s *SignalCandidateStore) loadLocked(fingerprint string, now time.Time) (SignalCandidate, error) {
	return s.loadPathLocked(filepath.Join(s.dir, signalCandidateFileName(fingerprint)+".json"), now)
}

func (s *SignalCandidateStore) loadPathLocked(path string, now time.Time) (SignalCandidate, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return SignalCandidate{}, err
	}
	var candidate SignalCandidate
	if err := json.Unmarshal(b, &candidate); err != nil {
		return SignalCandidate{}, err
	}
	return NormalizeSignalCandidate(candidate, now), nil
}

func (s *SignalCandidateStore) loadIfExistsLocked(fingerprint string, now time.Time) (SignalCandidate, bool, error) {
	if strings.TrimSpace(fingerprint) == "" {
		return SignalCandidate{}, false, nil
	}
	candidate, err := s.loadLocked(fingerprint, now)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return SignalCandidate{}, false, nil
		}
		return SignalCandidate{}, false, err
	}
	return candidate, true, nil
}

func SignalCandidateFromSubmitRequest(req SignalSubmitRequest, now time.Time) SignalCandidate {
	observedAt := req.ObservedAt.UTC()
	if observedAt.IsZero() {
		observedAt = now
	}
	source := strings.TrimSpace(req.Source)
	if source == "" {
		source = firstEvidenceSource(req.Evidence)
	}
	if source == "" {
		source = strings.TrimSpace(req.Surface)
	}
	if source == "" {
		source = "external"
	}
	signal := RunSignal{
		Fingerprint:       req.Fingerprint,
		Kind:              req.Kind,
		Title:             req.Title,
		Detail:            req.Detail,
		WhyNow:            req.WhyNow,
		Severity:          req.Severity,
		Surface:           firstRunValue(req.Surface, source),
		ObjectID:          req.ObjectID,
		ObjectURL:         req.ObjectURL,
		Score:             req.Score,
		Confidence:        req.Confidence,
		Priority:          req.Priority,
		ActionKind:        req.ActionKind,
		Rationale:         req.Rationale,
		TaskGoal:          req.TaskGoal,
		Evidence:          req.Evidence,
		SafeActions:       req.SafeActions,
		SuggestedNextStep: req.SuggestedNextStep,
	}
	if signal.Fingerprint == "" {
		signal.Fingerprint = SignalFingerprint(strings.Join([]string{"candidate", source, req.Kind, req.Surface, req.ObjectID, req.Title}, "|"))
	}
	candidate := SignalCandidate{
		RunSignal:       signal,
		Source:          source,
		FirstObservedAt: observedAt,
		LastObservedAt:  observedAt,
		ExpiresAt:       req.ExpiresAt,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	candidate.ExpiresAt = candidateExpiry(req, now)
	return NormalizeSignalCandidate(candidate, now)
}

func NormalizeSignalCandidate(candidate SignalCandidate, now time.Time) SignalCandidate {
	candidate.Source = strings.TrimSpace(candidate.Source)
	if candidate.Source == "" {
		candidate.Source = firstEvidenceSource(candidate.Evidence)
	}
	if candidate.Source == "" {
		candidate.Source = firstRunValue(candidate.Surface, "external")
	}
	if candidate.Fingerprint == "" {
		candidate.Fingerprint = SignalFingerprint(strings.Join([]string{"candidate", candidate.Source, candidate.Kind, candidate.Surface, candidate.ObjectID, candidate.Title}, "|"))
	}
	if strings.TrimSpace(candidate.ID) == "" {
		candidate.ID = candidate.Fingerprint
	}
	run := NormalizeRun(Run{Snapshot: RunSnapshot{Signals: []RunSignal{candidate.RunSignal}}})
	if len(run.Snapshot.Signals) > 0 {
		candidate.RunSignal = run.Snapshot.Signals[0]
	}
	if candidate.CreatedAt.IsZero() {
		candidate.CreatedAt = now
	}
	if candidate.UpdatedAt.IsZero() {
		candidate.UpdatedAt = now
	}
	if candidate.FirstObservedAt.IsZero() {
		candidate.FirstObservedAt = candidate.CreatedAt
	}
	if candidate.LastObservedAt.IsZero() {
		candidate.LastObservedAt = candidate.FirstObservedAt
	}
	if candidate.ExpiresAt.IsZero() || candidate.ExpiresAt.After(now.Add(MaxSignalCandidateTTL)) {
		candidate.ExpiresAt = now.Add(DefaultSignalCandidateTTL)
	}
	if candidate.SeenCount < 1 {
		candidate.SeenCount = 1
	}
	return candidate
}

func (c SignalCandidate) ToRunSignal() RunSignal {
	run := NormalizeRun(Run{Snapshot: RunSnapshot{Signals: []RunSignal{c.RunSignal}}})
	if len(run.Snapshot.Signals) == 0 {
		return RunSignal{}
	}
	return run.Snapshot.Signals[0]
}

func (s *SignalStore) List() ([]SignalRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []SignalRecord{}, nil
		}
		return nil, err
	}
	signals := []SignalRecord{}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		record, err := s.loadPathLocked(filepath.Join(s.dir, entry.Name()))
		if err != nil {
			return nil, err
		}
		signals = append(signals, record)
	}
	sort.Slice(signals, func(i, j int) bool { return signals[i].UpdatedAt.After(signals[j].UpdatedAt) })
	return signals, nil
}

func (s *SignalStore) Load(fingerprint string) (SignalRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadLocked(fingerprint)
}

func (s *SignalStore) Save(record SignalRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saveLocked(record)
}

func (s *SignalStore) UpsertFromAction(runID string, action RunAction, now time.Time) (SignalRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	action = normalizeRunAction(action, 0)
	record, exists, err := s.loadIfExistsLocked(action.Fingerprint)
	if err != nil {
		return SignalRecord{}, err
	}
	if !exists {
		record = SignalRecord{
			Fingerprint: action.Fingerprint,
			Status:      SignalStatusActive,
			FirstSeenAt: now,
		}
	}
	record.Kind = action.Kind
	record.Title = firstRunValue(action.Title, "Assistant recommendation")
	record.Surface = action.TargetSurface
	if record.Status == SignalStatusUseful && now.After(record.UpdatedAt) {
		record.Status = SignalStatusActive
	}
	record.LastSeenAt = now
	record.LastRunID = strings.TrimSpace(runID)
	record.LastActionID = action.ID
	record.SeenCount++
	record.UpdatedAt = now
	record = NormalizeSignalRecord(record, now)
	if err := s.saveLocked(record); err != nil {
		return SignalRecord{}, err
	}
	return record, nil
}

func (s *SignalStore) loadLocked(fingerprint string) (SignalRecord, error) {
	return s.loadPathLocked(filepath.Join(s.dir, signalFileName(fingerprint)+".json"))
}

func (s *SignalStore) loadPathLocked(path string) (SignalRecord, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return SignalRecord{}, err
	}
	var record SignalRecord
	if err := json.Unmarshal(b, &record); err != nil {
		return SignalRecord{}, err
	}
	return NormalizeSignalRecord(record, time.Now().UTC()), nil
}

func (s *SignalStore) loadIfExistsLocked(fingerprint string) (SignalRecord, bool, error) {
	if strings.TrimSpace(fingerprint) == "" {
		return SignalRecord{}, false, nil
	}
	record, err := s.loadLocked(fingerprint)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return SignalRecord{}, false, nil
		}
		return SignalRecord{}, false, err
	}
	return record, true, nil
}

func (s *SignalStore) saveLocked(record SignalRecord) error {
	now := time.Now().UTC()
	record = NormalizeSignalRecord(record, now)
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(s.dir, signalFileName(record.Fingerprint)+".json"), append(b, '\n'), 0o644)
}

func NormalizeSignalRecord(record SignalRecord, now time.Time) SignalRecord {
	record.Fingerprint = SignalFingerprint(record.Fingerprint)
	record.Status = normalizeSignalStatus(record.Status, record.SnoozedUntil, now)
	record.Kind = strings.TrimSpace(record.Kind)
	record.Title = strings.TrimSpace(record.Title)
	record.Surface = strings.TrimSpace(record.Surface)
	record.CreatedTaskID = strings.TrimSpace(record.CreatedTaskID)
	record.LastFeedback = strings.TrimSpace(record.LastFeedback)
	record.PolicyHint = strings.TrimSpace(record.PolicyHint)
	record.LastRunID = strings.TrimSpace(record.LastRunID)
	record.LastActionID = strings.TrimSpace(record.LastActionID)
	if record.Title == "" {
		record.Title = "Assistant recommendation"
	}
	if record.SeenCount < 0 {
		record.SeenCount = 0
	}
	if record.DismissedCount < 0 {
		record.DismissedCount = 0
	}
	if record.SnoozedCount < 0 {
		record.SnoozedCount = 0
	}
	if record.CreatedTaskCount < 0 {
		record.CreatedTaskCount = 0
	}
	if record.FirstSeenAt.IsZero() {
		record.FirstSeenAt = now
	}
	if record.LastSeenAt.IsZero() {
		record.LastSeenAt = record.FirstSeenAt
	}
	if record.UpdatedAt.IsZero() {
		record.UpdatedAt = now
	}
	if record.CreatedTaskID != "" {
		record.Status = SignalStatusCreatedTask
	}
	if record.PolicyHint == "" {
		record.PolicyHint = signalRecordPolicyHint(record)
	}
	return record
}

func signalRecordPolicyHint(record SignalRecord) string {
	switch record.Status {
	case SignalStatusDismissed:
		return "Suppress this recommendation until a materially new sighting appears."
	case SignalStatusSnoozed:
		return "Delay this recommendation until the snooze expires."
	case SignalStatusUseful:
		return "Clear the current item, but boost materially new sightings for the same signal."
	case SignalStatusCreatedTask:
		return "Do not create duplicate work; link back to the existing created task."
	}
	if record.UsefulCount > 0 {
		return "Prior useful feedback makes new sightings more likely to be worth surfacing."
	}
	if record.DismissedCount > 0 {
		return "Prior dismissals lower priority unless the signal has new evidence."
	}
	if record.SnoozedCount > 0 {
		return "Prior snoozes require clear timing or new evidence before surfacing again."
	}
	return ""
}

func SignalFingerprint(value string) string {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(value, "sig_") && len(value) >= len("sig_")+12 && !strings.ContainsAny(value, `/\`) {
		return value
	}
	sum := sha256.Sum256([]byte(strings.ToLower(strings.Join(strings.Fields(value), " "))))
	return "sig_" + hex.EncodeToString(sum[:])[:20]
}

func FingerprintRunAction(action RunAction) string {
	parts := []string{
		action.Kind,
		action.TargetSurface,
		action.Title,
		action.TaskGoal,
		action.KnowledgeQuery,
		action.WorkflowHint,
		action.Rationale,
	}
	return SignalFingerprint(strings.Join(parts, "|"))
}

func signalFileName(fingerprint string) string {
	return SignalFingerprint(fingerprint)
}

func signalCandidateFileName(fingerprint string) string {
	return SignalFingerprint(fingerprint)
}

func candidateExpiry(req SignalSubmitRequest, now time.Time) time.Time {
	if !req.ExpiresAt.IsZero() {
		expiresAt := req.ExpiresAt.UTC()
		if expiresAt.After(now) && !expiresAt.After(now.Add(MaxSignalCandidateTTL)) {
			return expiresAt
		}
	}
	ttl := time.Duration(req.TTLSeconds) * time.Second
	if ttl <= 0 {
		ttl = DefaultSignalCandidateTTL
	}
	if ttl > MaxSignalCandidateTTL {
		ttl = MaxSignalCandidateTTL
	}
	return now.Add(ttl)
}

func firstEvidenceSource(values []RunSignalEvidence) string {
	for _, value := range values {
		if strings.TrimSpace(value.Source) != "" {
			return strings.TrimSpace(value.Source)
		}
	}
	return ""
}

func mergeRunSignalEvidence(primary, secondary []RunSignalEvidence, limit int) []RunSignalEvidence {
	if limit <= 0 {
		return nil
	}
	seen := map[string]bool{}
	out := make([]RunSignalEvidence, 0, limit)
	for _, values := range [][]RunSignalEvidence{primary, secondary} {
		for _, value := range values {
			key := strings.ToLower(strings.Join([]string{
				strings.TrimSpace(value.Source),
				strings.TrimSpace(value.Kind),
				strings.TrimSpace(value.Title),
				strings.TrimSpace(value.ObjectID),
			}, "|"))
			if key == "|||" || seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, value)
			if len(out) >= limit {
				return out
			}
		}
	}
	return out
}

func assistantMaxInt(left, right int) int {
	if left > right {
		return left
	}
	return right
}

func normalizeSignalStatus(status string, snoozedUntil time.Time, now time.Time) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case SignalStatusDismissed:
		return SignalStatusDismissed
	case SignalStatusUseful:
		return SignalStatusUseful
	case SignalStatusCreatedTask:
		return SignalStatusCreatedTask
	case SignalStatusSnoozed:
		if !snoozedUntil.IsZero() && snoozedUntil.After(now) {
			return SignalStatusSnoozed
		}
		return SignalStatusActive
	default:
		return SignalStatusActive
	}
}
