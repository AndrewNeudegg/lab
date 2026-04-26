package remoteagent

import (
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
	StatusOnline  = "online"
	StatusOffline = "offline"
)

type Workdir struct {
	ID    string `json:"id"`
	Path  string `json:"path"`
	Label string `json:"label,omitempty"`
}

type Agent struct {
	ID            string            `json:"id"`
	Name          string            `json:"name"`
	Machine       string            `json:"machine"`
	Version       string            `json:"version,omitempty"`
	Status        string            `json:"status"`
	LastSeen      time.Time         `json:"last_seen"`
	StartedAt     time.Time         `json:"started_at,omitempty"`
	Capabilities  []string          `json:"capabilities,omitempty"`
	Workdirs      []Workdir         `json:"workdirs,omitempty"`
	CurrentTaskID string            `json:"current_task_id,omitempty"`
	Metadata      map[string]string `json:"metadata,omitempty"`
}

type Heartbeat struct {
	ID            string            `json:"id"`
	Name          string            `json:"name"`
	Machine       string            `json:"machine"`
	Version       string            `json:"version,omitempty"`
	StartedAt     time.Time         `json:"started_at,omitempty"`
	Capabilities  []string          `json:"capabilities,omitempty"`
	Workdirs      []Workdir         `json:"workdirs,omitempty"`
	CurrentTaskID string            `json:"current_task_id,omitempty"`
	Metadata      map[string]string `json:"metadata,omitempty"`
}

type Assignment struct {
	TaskID      string `json:"task_id"`
	Title       string `json:"title"`
	Goal        string `json:"goal"`
	Workdir     string `json:"workdir"`
	WorkdirID   string `json:"workdir_id,omitempty"`
	Backend     string `json:"backend"`
	Instruction string `json:"instruction"`
}

type Store struct {
	dir string
	mu  sync.Mutex
}

func NewStore(dir string) *Store {
	return &Store{dir: dir}
}

func (s *Store) UpsertHeartbeat(h Heartbeat, now time.Time) (Agent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	h.ID = strings.TrimSpace(h.ID)
	if h.ID == "" {
		return Agent{}, errors.New("agent id is required")
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	agent, _, err := s.loadIfExistsLocked(h.ID)
	if err != nil {
		return Agent{}, err
	}
	agent.ID = h.ID
	agent.Name = firstNonEmpty(h.Name, h.ID)
	agent.Machine = firstNonEmpty(h.Machine, "unknown")
	agent.Version = h.Version
	agent.Status = StatusOnline
	agent.LastSeen = now
	if !h.StartedAt.IsZero() {
		agent.StartedAt = h.StartedAt
	}
	agent.Capabilities = compactStrings(h.Capabilities)
	agent.Workdirs = compactWorkdirs(h.Workdirs)
	agent.CurrentTaskID = strings.TrimSpace(h.CurrentTaskID)
	agent.Metadata = h.Metadata
	if err := s.saveLocked(agent); err != nil {
		return Agent{}, err
	}
	return agent, nil
}

func (s *Store) SetCurrentTask(id, taskID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	agent, _, err := s.loadIfExistsLocked(id)
	if err != nil {
		return err
	}
	if agent.ID == "" {
		return os.ErrNotExist
	}
	agent.CurrentTaskID = taskID
	return s.saveLocked(agent)
}

func (s *Store) ClearCurrentTask(id, taskID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	agent, _, err := s.loadIfExistsLocked(id)
	if err != nil {
		return err
	}
	if agent.ID == "" {
		return os.ErrNotExist
	}
	if taskID == "" || agent.CurrentTaskID == taskID {
		agent.CurrentTaskID = ""
	}
	return s.saveLocked(agent)
}

func (s *Store) Load(id string) (Agent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	agent, ok, err := s.loadIfExistsLocked(id)
	if err != nil {
		return Agent{}, err
	}
	if !ok {
		return Agent{}, os.ErrNotExist
	}
	return agent, nil
}

func (s *Store) List(staleAfter time.Duration, now time.Time) ([]Agent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []Agent{}, nil
		}
		return nil, err
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	agents := make([]Agent, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		agent, err := s.loadLocked(entry.Name()[:len(entry.Name())-len(".json")])
		if err != nil {
			return nil, err
		}
		if staleAfter > 0 && !agent.LastSeen.IsZero() && now.Sub(agent.LastSeen) > staleAfter {
			agent.Status = StatusOffline
		}
		agents = append(agents, agent)
	}
	sort.Slice(agents, func(i, j int) bool {
		if agents[i].Status != agents[j].Status {
			return agents[i].Status == StatusOnline
		}
		return agents[i].LastSeen.After(agents[j].LastSeen)
	})
	return agents, nil
}

func (s *Store) saveLocked(agent Agent) error {
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(agent, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(s.dir, agent.ID+".json"), append(b, '\n'), 0o644)
}

func (s *Store) loadLocked(id string) (Agent, error) {
	agent, ok, err := s.loadIfExistsLocked(id)
	if err != nil {
		return Agent{}, err
	}
	if !ok {
		return Agent{}, os.ErrNotExist
	}
	return agent, nil
}

func (s *Store) loadIfExistsLocked(id string) (Agent, bool, error) {
	b, err := os.ReadFile(filepath.Join(s.dir, id+".json"))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Agent{}, false, nil
		}
		return Agent{}, false, err
	}
	var agent Agent
	if err := json.Unmarshal(b, &agent); err != nil {
		return Agent{}, false, err
	}
	return agent, true, nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func compactStrings(values []string) []string {
	seen := make(map[string]bool, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func compactWorkdirs(values []Workdir) []Workdir {
	out := make([]Workdir, 0, len(values))
	seen := make(map[string]bool, len(values))
	for _, value := range values {
		value.ID = strings.TrimSpace(value.ID)
		value.Path = strings.TrimSpace(value.Path)
		value.Label = strings.TrimSpace(value.Label)
		if value.Path == "" {
			continue
		}
		if value.ID == "" {
			value.ID = value.Path
		}
		key := value.ID + "\x00" + value.Path
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, value)
	}
	return out
}
