package workflow

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

func (s *Store) Save(item Workflow) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saveLocked(item)
}

func (s *Store) saveLocked(item Workflow) error {
	previous, hasPrevious, err := s.loadIfExistsLocked(item.ID)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	if hasPrevious && item.CreatedAt.IsZero() {
		item.CreatedAt = previous.CreatedAt
	}
	if item.CreatedAt.IsZero() {
		item.CreatedAt = now
	}
	item.UpdatedAt = now
	normalized, err := Normalize(item)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(normalized, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(s.dir, normalized.ID+".json"), append(b, '\n'), 0o644)
}

func (s *Store) Load(id string) (Workflow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadLocked(id)
}

func (s *Store) loadLocked(id string) (Workflow, error) {
	b, err := os.ReadFile(filepath.Join(s.dir, id+".json"))
	if err != nil {
		return Workflow{}, err
	}
	var item Workflow
	if err := json.Unmarshal(b, &item); err != nil {
		return Workflow{}, err
	}
	return Normalize(item)
}

func (s *Store) loadIfExistsLocked(id string) (Workflow, bool, error) {
	if id == "" {
		return Workflow{}, false, nil
	}
	item, err := s.loadLocked(id)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Workflow{}, false, nil
		}
		return Workflow{}, false, err
	}
	return item, true, nil
}

func (s *Store) List() ([]Workflow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var workflows []Workflow
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		item, err := s.loadLocked(entry.Name()[:len(entry.Name())-len(".json")])
		if err != nil {
			return nil, err
		}
		workflows = append(workflows, item)
	}
	return workflows, nil
}
