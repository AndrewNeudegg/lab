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
	mu  sync.Mutex
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
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(s.dir, t.ID+".json"), append(b, '\n'), 0o644)
}

func (s *Store) Load(id string) (Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadLocked(id)
}

func (s *Store) loadLocked(id string) (Task, error) {
	b, err := os.ReadFile(filepath.Join(s.dir, id+".json"))
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
	return os.Remove(filepath.Join(s.dir, id+".json"))
}

func (s *Store) List() ([]Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
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
