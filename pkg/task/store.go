package task

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type Store struct {
	dir string
	mu  sync.RWMutex
}

func NewStore(dir string) *Store {
	return &Store{dir: dir}
}

func (s *Store) Save(t Task) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC()
	previous, hasPrevious, err := s.loadIfExistsLocked(t.ID)
	if err != nil {
		return err
	}
	if t.CreatedAt.IsZero() {
		t.CreatedAt = now
	}
	if t.Status == "" {
		t.Status = StatusQueued
	}
	t = applyRunTimestamps(t, previous, hasPrevious, now)
	t.UpdatedAt = now
	t.SummaryOnly = false
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(s.taskPath(t.ID), append(b, '\n'), 0o644); err != nil {
		return err
	}
	return s.writeSummaryLocked(t)
}

func (s *Store) Load(id string) (Task, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.loadLocked(id)
}

func (s *Store) loadLocked(id string) (Task, error) {
	b, err := os.ReadFile(s.taskPath(id))
	if err != nil {
		return Task{}, err
	}
	var t Task
	if err := json.Unmarshal(b, &t); err != nil {
		return Task{}, err
	}
	return t, nil
}

func (s *Store) loadIfExistsLocked(id string) (Task, bool, error) {
	if id == "" {
		return Task{}, false, nil
	}
	t, err := s.loadLocked(id)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Task{}, false, nil
		}
		return Task{}, false, err
	}
	return t, true, nil
}

func applyRunTimestamps(t, previous Task, hasPrevious bool, now time.Time) Task {
	if hasPrevious {
		if t.StartedAt == nil {
			t.StartedAt = cloneTime(previous.StartedAt)
		}
		if t.StoppedAt == nil && t.Status != StatusRunning {
			t.StoppedAt = cloneTime(previous.StoppedAt)
		}
	}

	if t.Status == StatusRunning {
		if !hasPrevious || previous.Status != StatusRunning || t.StartedAt == nil {
			started := now
			t.StartedAt = &started
		}
		t.StoppedAt = nil
		return t
	}

	if t.Status == StatusQueued {
		return t
	}

	if t.StartedAt == nil && hasPrevious {
		switch {
		case previous.StartedAt != nil:
			t.StartedAt = cloneTime(previous.StartedAt)
		case previous.Status == StatusRunning && !previous.UpdatedAt.IsZero():
			started := previous.UpdatedAt
			t.StartedAt = &started
		case previous.Status == StatusRunning && !previous.CreatedAt.IsZero():
			started := previous.CreatedAt
			t.StartedAt = &started
		}
	}

	if t.StartedAt != nil && t.StoppedAt == nil && (!hasPrevious || previous.Status == StatusRunning) {
		stopped := now
		t.StoppedAt = &stopped
	}
	return t
}

func cloneTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.Remove(s.taskPath(id)); err != nil {
		return err
	}
	if err := os.Remove(s.summaryPath(id)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func (s *Store) List() ([]Task, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var tasks []Task
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		t, err := s.loadLocked(entry.Name()[:len(entry.Name())-len(".json")])
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, t)
	}
	return tasks, nil
}

func (s *Store) ListSummaries() ([]Task, error) {
	tasks, staleIDs, err := s.listSummariesFast()
	if err != nil {
		return nil, err
	}
	if len(staleIDs) == 0 {
		return tasks, nil
	}
	if err := s.backfillSummaries(staleIDs); err != nil {
		return nil, err
	}
	tasks, staleIDs, err = s.listSummariesFast()
	if err != nil {
		return nil, err
	}
	if len(staleIDs) > 0 {
		return nil, errors.New("task summary index remained stale after backfill")
	}
	return tasks, nil
}

func (s *Store) listSummariesFast() ([]Task, []string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil, nil
		}
		return nil, nil, err
	}
	var tasks []Task
	var staleIDs []string
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		id := entry.Name()[:len(entry.Name())-len(".json")]
		fresh, err := s.summaryFreshLocked(id)
		if err != nil {
			return nil, nil, err
		}
		if !fresh {
			staleIDs = append(staleIDs, id)
			continue
		}
		t, err := s.loadSummaryLocked(id)
		if err != nil {
			staleIDs = append(staleIDs, id)
			continue
		}
		t.SummaryOnly = true
		tasks = append(tasks, t)
	}
	if len(staleIDs) > 0 {
		return nil, staleIDs, nil
	}
	return tasks, nil, nil
}

func (s *Store) backfillSummaries(ids []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, id := range ids {
		fresh, err := s.summaryFreshLocked(id)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return err
		}
		if fresh {
			if _, err := s.loadSummaryLocked(id); err == nil {
				continue
			}
		}
		full, err := s.loadLocked(id)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return err
		}
		if err := s.writeSummaryLocked(full); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) summaryFreshLocked(id string) (bool, error) {
	fullInfo, err := os.Stat(s.taskPath(id))
	if err != nil {
		return false, err
	}
	summaryInfo, err := os.Stat(s.summaryPath(id))
	if err == nil {
		return !summaryInfo.ModTime().Before(fullInfo.ModTime()), nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	return false, nil
}

func (s *Store) loadSummaryLocked(id string) (Task, error) {
	b, err := os.ReadFile(s.summaryPath(id))
	if err != nil {
		return Task{}, err
	}
	var t Task
	if err := json.Unmarshal(b, &t); err != nil {
		return Task{}, err
	}
	return t, nil
}

func (s *Store) writeSummaryLocked(t Task) error {
	if err := os.MkdirAll(s.summaryDir(), 0o755); err != nil {
		return err
	}
	summary := SummarizeForList(t)
	b, err := json.MarshalIndent(summary, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.summaryPath(t.ID), append(b, '\n'), 0o644)
}

func (s *Store) taskPath(id string) string {
	return filepath.Join(s.dir, id+".json")
}

func (s *Store) summaryDir() string {
	return filepath.Join(s.dir, "index")
}

func (s *Store) summaryPath(id string) string {
	return filepath.Join(s.summaryDir(), id+".json")
}
