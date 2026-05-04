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
	ListSummaries() ([]Space, error)
	Delete(id string) error
	DeleteSourceArtifacts(spaceID, sourceID string) error
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
	if err := s.writeRetrievalIndexLocked(normalized); err != nil {
		return err
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

func (s *Store) writeRetrievalIndexLocked(space Space) error {
	index, err := BuildRetrievalIndex(space, time.Now().UTC())
	if err != nil {
		return err
	}
	relative := filepath.Join("indexes", space.ID)
	fullDir := filepath.Join(s.dir, relative)
	if err := os.MkdirAll(fullDir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(index, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(fullDir, "chunks.json"), append(data, '\n'), 0o644)
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

func (s *Store) ListSummaries() ([]Space, error) {
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
		space, err := s.loadSummaryLocked(entry.Name()[:len(entry.Name())-len(".json")])
		if err != nil {
			return nil, err
		}
		spaces = append(spaces, space)
	}
	return spaces, nil
}

type spaceSummaryJSON struct {
	ID           string              `json:"id"`
	Title        string              `json:"title"`
	Description  string              `json:"description,omitempty"`
	Objective    string              `json:"objective,omitempty"`
	Sources      []sourceSummaryJSON `json:"sources,omitempty"`
	Reports      []Report            `json:"reports,omitempty"`
	ResearchRuns []ResearchRun       `json:"research_runs,omitempty"`
	Insight      SpaceInsight        `json:"insight"`
	CreatedBy    string              `json:"created_by,omitempty"`
	CreatedAt    time.Time           `json:"created_at"`
	UpdatedAt    time.Time           `json:"updated_at"`
}

type sourceSummaryJSON struct {
	ID          string           `json:"id"`
	Title       string           `json:"title"`
	Kind        string           `json:"kind"`
	URI         string           `json:"uri,omitempty"`
	Summary     string           `json:"summary"`
	KeyTerms    []string         `json:"key_terms,omitempty"`
	Questions   []string         `json:"questions,omitempty"`
	Claims      []SourceClaim    `json:"claims,omitempty"`
	Entities    []SourceEntity   `json:"entities,omitempty"`
	Reliability []string         `json:"reliability_notes,omitempty"`
	WordCount   int              `json:"word_count"`
	Provenance  SourceProvenance `json:"provenance,omitempty"`
	Ingestion   SourceIngestion  `json:"ingestion,omitempty"`
	CreatedAt   time.Time        `json:"created_at"`
	UpdatedAt   time.Time        `json:"updated_at"`
}

func (s *Store) loadSummaryLocked(id string) (Space, error) {
	b, err := os.ReadFile(filepath.Join(s.dir, id+".json"))
	if err != nil {
		return Space{}, err
	}
	var raw spaceSummaryJSON
	if err := json.Unmarshal(b, &raw); err != nil {
		return Space{}, err
	}
	return normalizeSpaceSummary(raw)
}

func normalizeSpaceSummary(raw spaceSummaryJSON) (Space, error) {
	space := Space{
		ID:           strings.TrimSpace(raw.ID),
		Title:        strings.TrimSpace(raw.Title),
		Description:  strings.TrimSpace(raw.Description),
		Objective:    strings.TrimSpace(raw.Objective),
		Reports:      raw.Reports,
		ResearchRuns: raw.ResearchRuns,
		Insight:      raw.Insight,
		CreatedBy:    strings.TrimSpace(raw.CreatedBy),
		CreatedAt:    raw.CreatedAt,
		UpdatedAt:    raw.UpdatedAt,
	}
	if space.ID == "" {
		return Space{}, errors.New("knowledge space id is required")
	}
	if space.Title == "" {
		space.Title = firstLine(space.Objective)
	}
	if space.Title == "" {
		return Space{}, errors.New("knowledge space title is required")
	}
	for _, source := range raw.Sources {
		normalized, err := normalizeSourceSummary(source)
		if err != nil {
			return Space{}, err
		}
		space.Sources = append(space.Sources, normalized)
	}
	for index := range space.Reports {
		space.Reports[index] = normalizeReport(space.Reports[index])
	}
	for index := range space.ResearchRuns {
		space.ResearchRuns[index] = normalizeResearchRun(space.ResearchRuns[index])
	}
	if space.Insight.SourceCount == 0 && len(space.Sources) > 0 {
		space.Insight = BuildSpaceInsight(space.Sources, firstNonZeroTime(space.UpdatedAt, time.Now().UTC()))
	}
	return space, nil
}

func normalizeSourceSummary(raw sourceSummaryJSON) (Source, error) {
	source := Source{
		ID:          strings.TrimSpace(raw.ID),
		Title:       strings.TrimSpace(raw.Title),
		Kind:        normalizeSourceKind(raw.Kind),
		URI:         strings.TrimSpace(raw.URI),
		Summary:     strings.TrimSpace(raw.Summary),
		KeyTerms:    compactStrings(raw.KeyTerms, 12),
		Questions:   compactStrings(raw.Questions, 8),
		Claims:      normalizeSourceClaims(raw.Claims),
		Entities:    normalizeSourceEntities(raw.Entities),
		Reliability: compactStrings(raw.Reliability, 8),
		WordCount:   raw.WordCount,
		Provenance:  raw.Provenance,
		Ingestion:   raw.Ingestion,
		CreatedAt:   raw.CreatedAt,
		UpdatedAt:   raw.UpdatedAt,
	}
	if source.ID == "" {
		return Source{}, errors.New("knowledge source id is required")
	}
	if source.Title == "" {
		source.Title = source.URI
	}
	if source.Title == "" {
		return Source{}, errors.New("knowledge source title is required")
	}
	if source.Kind == SourceKindURL && source.URI == "" {
		source.URI = source.Title
	}
	source.Provenance = normalizeSourceProvenance(source.Provenance, source)
	source.Ingestion = normalizeSourceIngestion(source.Ingestion, source.WordCount > 0 || source.Provenance.ByteCount > 0 || source.Summary != "")
	return source, nil
}

func firstNonZeroTime(values ...time.Time) time.Time {
	for _, value := range values {
		if !value.IsZero() {
			return value
		}
	}
	return time.Time{}
}

func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	id = strings.TrimSpace(id)
	if id == "" {
		return errors.New("knowledge space id is required")
	}
	if strings.ContainsAny(id, `/\`) {
		return errors.New("knowledge space id must not contain path separators")
	}
	if err := os.Remove(filepath.Join(s.dir, id+".json")); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	for _, relative := range []string{
		filepath.Join("snapshots", id),
		filepath.Join("indexes", id),
		filepath.Join("runs", id),
	} {
		if err := os.RemoveAll(filepath.Join(s.dir, relative)); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) DeleteSourceArtifacts(spaceID, sourceID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	spaceID = strings.TrimSpace(spaceID)
	sourceID = strings.TrimSpace(sourceID)
	if spaceID == "" {
		return errors.New("knowledge space id is required")
	}
	if sourceID == "" {
		return errors.New("knowledge source id is required")
	}
	if strings.ContainsAny(spaceID, `/\`) || strings.ContainsAny(sourceID, `/\`) {
		return errors.New("knowledge ids must not contain path separators")
	}
	path := filepath.Join(s.dir, "snapshots", spaceID, sourceID+".txt")
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
