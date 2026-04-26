package approval

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	StatusPending = "pending"
	StatusGranted = "granted"
	StatusDenied  = "denied"
	StatusFailed  = "failed"
	StatusStale   = "stale"
)

type Request struct {
	ID        string          `json:"id"`
	TaskID    string          `json:"task_id,omitempty"`
	Tool      string          `json:"tool"`
	Args      json.RawMessage `json:"args"`
	Reason    string          `json:"reason"`
	Status    string          `json:"status"`
	CreatedAt time.Time       `json:"created_at"`
	UpdatedAt time.Time       `json:"updated_at"`
}

type Store struct {
	dir string
	mu  sync.Mutex
}

func NewStore(dir string) *Store {
	return &Store{dir: dir}
}

func (s *Store) Save(r Request) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r.CreatedAt.IsZero() {
		r.CreatedAt = time.Now().UTC()
	}
	r.UpdatedAt = time.Now().UTC()
	if r.Status == "" {
		r.Status = StatusPending
	}
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(s.dir, r.ID+".json"), append(b, '\n'), 0o644)
}

func (s *Store) Load(id string) (Request, error) {
	b, err := os.ReadFile(filepath.Join(s.dir, id+".json"))
	if err != nil {
		return Request{}, err
	}
	var r Request
	if err := json.Unmarshal(b, &r); err != nil {
		return Request{}, err
	}
	return r, nil
}

func (s *Store) List() ([]Request, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var requests []Request
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		r, err := s.Load(entry.Name()[:len(entry.Name())-len(".json")])
		if err != nil {
			return nil, err
		}
		requests = append(requests, r)
	}
	return requests, nil
}
