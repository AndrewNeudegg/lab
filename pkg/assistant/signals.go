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
)

type SignalRecord struct {
	Fingerprint   string    `json:"fingerprint"`
	Status        string    `json:"status"`
	Kind          string    `json:"kind,omitempty"`
	Title         string    `json:"title"`
	Surface       string    `json:"surface,omitempty"`
	FirstSeenAt   time.Time `json:"first_seen_at"`
	LastSeenAt    time.Time `json:"last_seen_at"`
	SeenCount     int       `json:"seen_count"`
	UsefulCount   int       `json:"useful_count,omitempty"`
	SnoozedUntil  time.Time `json:"snoozed_until,omitempty"`
	DismissedAt   time.Time `json:"dismissed_at,omitempty"`
	CreatedTaskID string    `json:"created_task_id,omitempty"`
	LastRunID     string    `json:"last_run_id,omitempty"`
	LastActionID  string    `json:"last_action_id,omitempty"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type SignalFeedbackRequest struct {
	Feedback      string `json:"feedback"`
	SnoozeSeconds int    `json:"snooze_seconds,omitempty"`
}

type SignalStore struct {
	dir string
	mu  sync.Mutex
}

func NewSignalStore(dir string) *SignalStore {
	return &SignalStore{dir: dir}
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
	record.LastRunID = strings.TrimSpace(record.LastRunID)
	record.LastActionID = strings.TrimSpace(record.LastActionID)
	if record.Title == "" {
		record.Title = "Assistant recommendation"
	}
	if record.SeenCount < 0 {
		record.SeenCount = 0
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
	return record
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
