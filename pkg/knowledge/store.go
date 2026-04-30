package knowledge

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

func (s *Store) Save(space Space) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	previous, hasPrevious, err := s.loadIfExistsLocked(space.ID)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	if hasPrevious && space.CreatedAt.IsZero() {
		space.CreatedAt = previous.CreatedAt
	}
	if space.CreatedAt.IsZero() {
		space.CreatedAt = now
	}
	if space.UpdatedAt.IsZero() {
		space.UpdatedAt = now
	}
	space.Insight = BuildSpaceInsight(space.Sources, space.UpdatedAt)
	normalized, err := NormalizeSpace(space)
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

func (s *Store) Load(id string) (Space, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadLocked(id)
}

func (s *Store) loadLocked(id string) (Space, error) {
	b, err := os.ReadFile(filepath.Join(s.dir, id+".json"))
	if err != nil {
		return Space{}, err
	}
	var space Space
	if err := json.Unmarshal(b, &space); err != nil {
		return Space{}, err
	}
	return NormalizeSpace(space)
}

func (s *Store) loadIfExistsLocked(id string) (Space, bool, error) {
	if id == "" {
		return Space{}, false, nil
	}
	space, err := s.loadLocked(id)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Space{}, false, nil
		}
		return Space{}, false, err
	}
	return space, true, nil
}

func (s *Store) List() ([]Space, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var spaces []Space
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		space, err := s.loadLocked(entry.Name()[:len(entry.Name())-len(".json")])
		if err != nil {
			return nil, err
		}
		spaces = append(spaces, space)
	}
	return spaces, nil
}
