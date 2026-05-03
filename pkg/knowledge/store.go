package knowledge

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type Repository interface {
	Save(space Space) error
	Load(id string) (Space, error)
	List() ([]Space, error)
}

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
	normalized, err := NormalizeSpace(space)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return err
	}
	for index := range normalized.Sources {
		path, err := s.writeSourceSnapshotLocked(normalized.ID, normalized.Sources[index])
		if err != nil {
			return err
		}
		normalized.Sources[index].Provenance.SnapshotPath = path
	}
	for index := range normalized.ResearchRuns {
		path, err := s.writeResearchRunWorkspaceLocked(normalized.ID, normalized.ResearchRuns[index], normalized.Reports)
		if err != nil {
			return err
		}
		normalized.ResearchRuns[index].WorkspacePath = path
	}
	normalized.Insight = BuildSpaceInsight(normalized.Sources, normalized.UpdatedAt)
	b, err := json.MarshalIndent(normalized, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(s.dir, normalized.ID+".json"), append(b, '\n'), 0o644)
}

func (s *Store) writeResearchRunWorkspaceLocked(spaceID string, run ResearchRun, reports []Report) (string, error) {
	if strings.TrimSpace(run.ID) == "" {
		return run.WorkspacePath, nil
	}
	relative := filepath.Join("runs", spaceID, run.ID)
	fullDir := filepath.Join(s.dir, relative)
	if err := os.MkdirAll(fullDir, 0o755); err != nil {
		return "", err
	}
	run.WorkspacePath = relative
	state, err := json.MarshalIndent(run, "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(fullDir, "state.json"), append(state, '\n'), 0o644); err != nil {
		return "", err
	}
	var events strings.Builder
	for _, event := range run.Events {
		line, err := json.Marshal(event)
		if err != nil {
			return "", err
		}
		events.Write(line)
		events.WriteByte('\n')
	}
	if err := os.WriteFile(filepath.Join(fullDir, "events.jsonl"), []byte(events.String()), 0o644); err != nil {
		return "", err
	}
	if len(run.Candidates) > 0 {
		candidates, err := json.MarshalIndent(run.Candidates, "", "  ")
		if err != nil {
			return "", err
		}
		if err := os.WriteFile(filepath.Join(fullDir, "sources.json"), append(candidates, '\n'), 0o644); err != nil {
			return "", err
		}
		if err := os.WriteFile(filepath.Join(fullDir, "candidates.json"), append(candidates, '\n'), 0o644); err != nil {
			return "", err
		}
	}
	if len(run.ResearchLoops) > 0 {
		loops, err := json.MarshalIndent(run.ResearchLoops, "", "  ")
		if err != nil {
			return "", err
		}
		if err := os.WriteFile(filepath.Join(fullDir, "loops.json"), append(loops, '\n'), 0o644); err != nil {
			return "", err
		}
	}
	if len(run.Coverage) > 0 {
		coverage, err := json.MarshalIndent(run.Coverage, "", "  ")
		if err != nil {
			return "", err
		}
		if err := os.WriteFile(filepath.Join(fullDir, "coverage.json"), append(coverage, '\n'), 0o644); err != nil {
			return "", err
		}
	}
	if report, ok := reportForRun(reports, run); ok {
		data, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			return "", err
		}
		if err := os.WriteFile(filepath.Join(fullDir, "report.json"), append(data, '\n'), 0o644); err != nil {
			return "", err
		}
		evidence, err := json.MarshalIndent(report.Evidence, "", "  ")
		if err != nil {
			return "", err
		}
		if err := os.WriteFile(filepath.Join(fullDir, "evidence.json"), append(evidence, '\n'), 0o644); err != nil {
			return "", err
		}
	}
	return relative, nil
}

func reportForRun(reports []Report, run ResearchRun) (Report, bool) {
	for _, report := range reports {
		if strings.TrimSpace(run.ReportID) != "" && report.ID == run.ReportID {
			return report, true
		}
	}
	return Report{}, false
}

func (s *Store) writeSourceSnapshotLocked(spaceID string, source Source) (string, error) {
	if strings.TrimSpace(source.Content) == "" {
		return source.Provenance.SnapshotPath, nil
	}
	relative := filepath.Join("snapshots", spaceID, source.ID+".txt")
	fullPath := filepath.Join(s.dir, relative)
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		return "", err
	}
	return relative, os.WriteFile(fullPath, []byte(source.Content+"\n"), 0o644)
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
			return []Space{}, nil
		}
		return nil, err
	}
	spaces := []Space{}
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
