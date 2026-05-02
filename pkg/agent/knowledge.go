package agent

import (
	"context"
	"errors"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/andrewneudegg/lab/pkg/eventlog"
	"github.com/andrewneudegg/lab/pkg/id"
	knowledgestore "github.com/andrewneudegg/lab/pkg/knowledge"
)

func (o *Orchestrator) WithKnowledge(store knowledgestore.Repository) *Orchestrator {
	o.knowledge = store
	return o
}

func (o *Orchestrator) knowledgeStore() (knowledgestore.Repository, error) {
	if o.knowledge != nil {
		return o.knowledge, nil
	}
	if strings.TrimSpace(o.cfg.DataDir) == "" {
		return nil, errors.New("knowledge store is not configured")
	}
	o.knowledge = knowledgestore.NewStore(filepath.Join(o.cfg.DataDir, "knowledge"))
	return o.knowledge, nil
}

func (o *Orchestrator) CreateKnowledgeSpace(ctx context.Context, req knowledgestore.CreateSpaceRequest) (knowledgestore.Space, string, error) {
	store, err := o.knowledgeStore()
	if err != nil {
		return knowledgestore.Space{}, "", err
	}
	if req.CreatedBy == "" {
		req.CreatedBy = "OrchestratorAgent"
	}
	space, err := knowledgestore.NewSpace(req, id.New("kspace"), time.Now().UTC())
	if err != nil {
		return knowledgestore.Space{}, "", err
	}
	if err := store.Save(space); err != nil {
		return knowledgestore.Space{}, "", err
	}
	space, _ = store.Load(space.ID)
	o.appendKnowledgeEvent(ctx, "knowledge.space.created", space, map[string]any{"title": space.Title})
	return space, "Knowledge Space created: " + space.Title, nil
}

func (o *Orchestrator) ListKnowledgeSpaces() ([]knowledgestore.Space, error) {
	store, err := o.knowledgeStore()
	if err != nil {
		return nil, err
	}
	spaces, err := store.List()
	if err != nil {
		return nil, err
	}
	sort.Slice(spaces, func(i, j int) bool { return spaces[i].UpdatedAt.After(spaces[j].UpdatedAt) })
	return spaces, nil
}

func (o *Orchestrator) LoadKnowledgeSpace(spaceID string) (knowledgestore.Space, error) {
	store, err := o.knowledgeStore()
	if err != nil {
		return knowledgestore.Space{}, err
	}
	return store.Load(spaceID)
}

func (o *Orchestrator) AddKnowledgeSource(ctx context.Context, spaceID string, req knowledgestore.AddSourceRequest) (knowledgestore.Space, knowledgestore.Source, string, error) {
	store, err := o.knowledgeStore()
	if err != nil {
		return knowledgestore.Space{}, knowledgestore.Source{}, "", err
	}
	space, err := store.Load(spaceID)
	if err != nil {
		return knowledgestore.Space{}, knowledgestore.Source{}, "", err
	}
	now := time.Now().UTC()
	source, err := knowledgestore.BuildSource(ctx, req, id.New("ksrc"), now, nil)
	if err != nil {
		return knowledgestore.Space{}, knowledgestore.Source{}, "", err
	}
	space, err = knowledgestore.AddSource(space, source, time.Now().UTC())
	if err != nil {
		return knowledgestore.Space{}, knowledgestore.Source{}, "", err
	}
	if err := store.Save(space); err != nil {
		return knowledgestore.Space{}, knowledgestore.Source{}, "", err
	}
	space, _ = store.Load(space.ID)
	o.appendKnowledgeEvent(ctx, "knowledge.source.added", space, map[string]any{"source_id": source.ID, "title": source.Title, "status": source.Ingestion.State, "word_count": source.WordCount})
	if source.Ingestion.State == knowledgestore.SourceStatusFailed {
		return space, source, "Source ingestion failed: " + source.Title, nil
	}
	return space, source, "Source indexed: " + source.Title, nil
}

func (o *Orchestrator) ResearchKnowledgeSpace(ctx context.Context, spaceID string, req knowledgestore.ResearchRequest) (knowledgestore.Space, knowledgestore.Report, string, error) {
	store, err := o.knowledgeStore()
	if err != nil {
		return knowledgestore.Space{}, knowledgestore.Report{}, "", err
	}
	space, err := store.Load(spaceID)
	if err != nil {
		return knowledgestore.Space{}, knowledgestore.Report{}, "", err
	}
	report, err := knowledgestore.GenerateReport(space, req, id.New("kreport"), time.Now().UTC())
	if err != nil {
		return knowledgestore.Space{}, knowledgestore.Report{}, "", err
	}
	space, err = knowledgestore.AddReport(space, report, time.Now().UTC())
	if err != nil {
		return knowledgestore.Space{}, knowledgestore.Report{}, "", err
	}
	if err := store.Save(space); err != nil {
		return knowledgestore.Space{}, knowledgestore.Report{}, "", err
	}
	space, _ = store.Load(space.ID)
	o.appendKnowledgeEvent(ctx, "knowledge.report.created", space, map[string]any{"report_id": report.ID, "mode": report.Mode, "evidence": len(report.Evidence)})
	return space, report, "Research report created.", nil
}

func (o *Orchestrator) QueryKnowledgeSpace(ctx context.Context, spaceID string, req knowledgestore.QueryRequest) (knowledgestore.QueryResult, string, error) {
	store, err := o.knowledgeStore()
	if err != nil {
		return knowledgestore.QueryResult{}, "", err
	}
	space, err := store.Load(spaceID)
	if err != nil {
		return knowledgestore.QueryResult{}, "", err
	}
	result, err := knowledgestore.QuerySpace(space, req, time.Now().UTC())
	if err != nil {
		return knowledgestore.QueryResult{}, "", err
	}
	o.appendKnowledgeEvent(ctx, "knowledge.query.created", space, map[string]any{"query": result.Query, "evidence": len(result.Evidence)})
	return result, "Corpus query completed.", nil
}

func (o *Orchestrator) AskKnowledgeSpace(ctx context.Context, spaceID string, req knowledgestore.AskRequest) (knowledgestore.AskResult, string, error) {
	store, err := o.knowledgeStore()
	if err != nil {
		return knowledgestore.AskResult{}, "", err
	}
	space, err := store.Load(spaceID)
	if err != nil {
		return knowledgestore.AskResult{}, "", err
	}
	result, err := knowledgestore.AnswerQuestion(space, req, time.Now().UTC())
	if err != nil {
		return knowledgestore.AskResult{}, "", err
	}
	o.appendKnowledgeEvent(ctx, "knowledge.ask.created", space, map[string]any{"question": result.Question, "evidence": len(result.Evidence)})
	return result, "Grounded answer created.", nil
}

func (o *Orchestrator) StartKnowledgeResearchRun(ctx context.Context, spaceID string, req knowledgestore.CreateResearchRunRequest) (knowledgestore.Space, knowledgestore.ResearchRun, knowledgestore.Report, string, error) {
	store, err := o.knowledgeStore()
	if err != nil {
		return knowledgestore.Space{}, knowledgestore.ResearchRun{}, knowledgestore.Report{}, "", err
	}
	space, err := store.Load(spaceID)
	if err != nil {
		return knowledgestore.Space{}, knowledgestore.ResearchRun{}, knowledgestore.Report{}, "", err
	}
	space, run, report, err := knowledgestore.CompleteResearchRun(space, req, id.New("krun"), id.New("kreport"), time.Now().UTC())
	if err != nil {
		return knowledgestore.Space{}, knowledgestore.ResearchRun{}, knowledgestore.Report{}, "", err
	}
	if err := store.Save(space); err != nil {
		return knowledgestore.Space{}, knowledgestore.ResearchRun{}, knowledgestore.Report{}, "", err
	}
	space, _ = store.Load(space.ID)
	o.appendKnowledgeEvent(ctx, "knowledge.research_run.completed", space, map[string]any{"run_id": run.ID, "report_id": report.ID, "evidence": len(report.Evidence)})
	return space, run, report, "Research run completed.", nil
}

func (o *Orchestrator) appendKnowledgeEvent(ctx context.Context, eventType string, space knowledgestore.Space, payload map[string]any) {
	if o.events == nil {
		return
	}
	if payload == nil {
		payload = map[string]any{}
	}
	payload["space_id"] = space.ID
	payload["space_title"] = space.Title
	_ = o.events.Append(ctx, eventlog.Event{
		ID:      id.New("evt"),
		Type:    eventType,
		Actor:   "homelabd",
		Payload: eventlog.Payload(payload),
	})
}
