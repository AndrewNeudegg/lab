package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/andrewneudegg/lab/pkg/eventlog"
	"github.com/andrewneudegg/lab/pkg/id"
	knowledgestore "github.com/andrewneudegg/lab/pkg/knowledge"
	"github.com/andrewneudegg/lab/pkg/llm"
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

func (o *Orchestrator) knowledgeModel() knowledgestore.LanguageModel {
	return knowledgestore.NewLanguageModel(o.provider, o.model)
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
	if source.Ingestion.State != knowledgestore.SourceStatusFailed {
		analyzed, analysisErr := o.knowledgeModel().AnalyzeSource(ctx, source, now)
		if analysisErr != nil {
			source = failKnowledgeSource(source, "model_analysis", analysisErr, now)
		} else {
			source = analyzed
		}
	}
	space, err = knowledgestore.AddSource(space, source, now)
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
	return space, source, "Source analysed: " + source.Title, nil
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
	now := time.Now().UTC()
	question := strings.TrimSpace(req.Question)
	if question == "" {
		return knowledgestore.Space{}, knowledgestore.Report{}, "", errors.New("research question is required")
	}
	query, err := knowledgestore.QuerySpace(space, knowledgestore.QueryRequest{
		Query:     question,
		SourceIDs: req.SourceIDs,
		Limit:     12,
	}, now)
	if err != nil {
		return knowledgestore.Space{}, knowledgestore.Report{}, "", err
	}
	run := knowledgestore.ResearchRun{
		ID:              id.New("krun"),
		Objective:       question,
		Question:        question,
		Mode:            req.Mode,
		Depth:           "quick",
		Status:          knowledgestore.ResearchRunStatusSynthesizing,
		SourceIDs:       req.SourceIDs,
		SourcesExamined: countKnowledgeSources(space, req.SourceIDs),
		EvidenceCount:   len(query.Evidence),
		StartedAt:       now,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	report, err := o.knowledgeModel().SynthesizeReport(ctx, space, run, query.Evidence, id.New("kreport"), now)
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
	now := time.Now().UTC()
	query, err := knowledgestore.QuerySpace(space, knowledgestore.QueryRequest{
		Query:     req.Question,
		SourceIDs: req.SourceIDs,
		Limit:     req.Limit,
	}, now)
	if err != nil {
		return knowledgestore.AskResult{}, "", err
	}
	result, err := o.knowledgeModel().AnswerQuestion(ctx, space, req, query.Evidence, now)
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
	now := time.Now().UTC()
	objective := strings.TrimSpace(req.Objective)
	question := strings.TrimSpace(req.Question)
	if objective == "" {
		objective = question
	}
	if question == "" {
		question = objective
	}
	if objective == "" {
		return knowledgestore.Space{}, knowledgestore.ResearchRun{}, knowledgestore.Report{}, "", errors.New("research objective is required")
	}
	run := knowledgestore.ResearchRun{
		ID:              id.New("krun"),
		Objective:       objective,
		Scope:           req.Scope,
		Depth:           req.Depth,
		Status:          knowledgestore.ResearchRunStatusQueued,
		Question:        question,
		Mode:            req.Mode,
		SourceIDs:       req.SourceIDs,
		DiscoverSources: req.DiscoverSources,
		MaxSources:      req.MaxSources,
		CreatedAt:       now,
		UpdatedAt:       now,
		Events: []knowledgestore.ResearchRunEvent{
			{ID: id.New("kevt"), Stage: "queued", Message: "Research run queued for language model planning.", CreatedAt: now},
		},
	}
	space, err = knowledgestore.AddResearchRun(space, run, now)
	if err != nil {
		return knowledgestore.Space{}, knowledgestore.ResearchRun{}, knowledgestore.Report{}, "", err
	}
	if err := store.Save(space); err != nil {
		return knowledgestore.Space{}, knowledgestore.ResearchRun{}, knowledgestore.Report{}, "", err
	}
	space, _ = store.Load(space.ID)
	for _, savedRun := range space.ResearchRuns {
		if savedRun.ID == run.ID {
			run = savedRun
			break
		}
	}
	o.appendKnowledgeEvent(ctx, "knowledge.research_run.queued", space, map[string]any{"run_id": run.ID, "objective": run.Objective})
	go o.executeKnowledgeResearchRun(context.Background(), store, space.ID, run.ID)
	return space, run, knowledgestore.Report{}, "Research run queued.", nil
}

func (o *Orchestrator) executeKnowledgeResearchRun(ctx context.Context, store knowledgestore.Repository, spaceID, runID string) {
	model := o.knowledgeModel()
	space, run, err := loadKnowledgeRun(store, spaceID, runID)
	if err != nil {
		o.log().Error("failed to load queued knowledge research run", "space_id", spaceID, "run_id", runID, "error", err)
		return
	}
	now := time.Now().UTC()
	run = updateKnowledgeRun(run, knowledgestore.ResearchRunStatusPlanning, "planning", "Planning research objective with the configured language model.", now)
	if err := saveKnowledgeRun(store, space, run, now); err != nil {
		o.log().Error("failed to save knowledge research planning state", "space_id", spaceID, "run_id", runID, "error", err)
		return
	}
	plan, planResp, err := model.PlanResearch(ctx, space, knowledgestore.CreateResearchRunRequest{
		Objective:       run.Objective,
		Scope:           run.Scope,
		Depth:           run.Depth,
		Question:        run.Question,
		Mode:            run.Mode,
		SourceIDs:       run.SourceIDs,
		DiscoverSources: run.DiscoverSources,
		MaxSources:      run.MaxSources,
	})
	if err != nil {
		o.failKnowledgeResearchRun(ctx, store, spaceID, run, err)
		return
	}
	run.Plan = plan
	run.Provider = knowledgeResponseProvider(planResp.Provider, o.provider)
	run.Model = knowledgeResponseModel(planResp.Model, o.model)
	run.Usage = addKnowledgeUsage(run.Usage, knowledgeUsage(planResp.Usage))
	if run.DiscoverSources {
		now = time.Now().UTC()
		run = updateKnowledgeRun(run, knowledgestore.ResearchRunStatusDiscovering, "discovery", "Searching online sources and importing fetched evidence into this Knowledge Space.", now)
		if err := saveKnowledgeRun(store, space, run, now); err != nil {
			o.log().Error("failed to save knowledge research discovery state", "space_id", spaceID, "run_id", runID, "error", err)
			return
		}
		space, run, err = o.discoverKnowledgeSources(ctx, store, space, run)
		if err != nil {
			o.failKnowledgeResearchRun(ctx, store, spaceID, run, err)
			return
		}
	}
	now = time.Now().UTC()
	run = updateKnowledgeRun(run, knowledgestore.ResearchRunStatusRetrieving, "retrieval", "Retrieving stored corpus evidence from the planned research queries.", now)
	if err := saveKnowledgeRun(store, space, run, now); err != nil {
		o.log().Error("failed to save knowledge research retrieval state", "space_id", spaceID, "run_id", runID, "error", err)
		return
	}
	query, err := knowledgeRunRetrievalQuery(run)
	if err != nil {
		o.failKnowledgeResearchRun(ctx, store, spaceID, run, err)
		return
	}
	queryResult, err := knowledgestore.QuerySpace(space, knowledgestore.QueryRequest{
		Query:     query,
		SourceIDs: effectiveKnowledgeRunSourceIDs(run),
		Limit:     12,
	}, now)
	if err != nil {
		o.failKnowledgeResearchRun(ctx, store, spaceID, run, err)
		return
	}
	run.SourcesExamined = countKnowledgeSources(space, run.SourceIDs)
	run.EvidenceCount = len(queryResult.Evidence)
	now = time.Now().UTC()
	run = updateKnowledgeRun(run, knowledgestore.ResearchRunStatusReading, "reading", "Reading retrieved evidence before synthesis.", now)
	if err := saveKnowledgeRun(store, space, run, now); err != nil {
		o.log().Error("failed to save knowledge research reading state", "space_id", spaceID, "run_id", runID, "error", err)
		return
	}
	now = time.Now().UTC()
	run = updateKnowledgeRun(run, knowledgestore.ResearchRunStatusSynthesizing, "synthesis", "Synthesising a source-grounded report with the configured language model.", now)
	if err := saveKnowledgeRun(store, space, run, now); err != nil {
		o.log().Error("failed to save knowledge research synthesis state", "space_id", spaceID, "run_id", runID, "error", err)
		return
	}
	report, err := model.SynthesizeReport(ctx, space, run, queryResult.Evidence, id.New("kreport"), now)
	if err != nil {
		o.failKnowledgeResearchRun(ctx, store, spaceID, run, err)
		return
	}
	run.Provider = firstNonEmptyString(run.Provider, report.Provider)
	run.Model = firstNonEmptyString(run.Model, report.Model)
	run.Usage = addKnowledgeUsage(run.Usage, report.Usage)
	now = time.Now().UTC()
	run = updateKnowledgeRun(run, knowledgestore.ResearchRunStatusReviewing, "review", "Persisting model report, citations, gaps, and run provenance.", now)
	if err := saveKnowledgeRun(store, space, run, now); err != nil {
		o.log().Error("failed to save knowledge research review state", "space_id", spaceID, "run_id", runID, "error", err)
		return
	}
	space, err = store.Load(spaceID)
	if err != nil {
		o.failKnowledgeResearchRun(ctx, store, spaceID, run, err)
		return
	}
	space, err = knowledgestore.AddReport(space, report, now)
	if err != nil {
		o.failKnowledgeResearchRun(ctx, store, spaceID, run, err)
		return
	}
	run.ReportID = report.ID
	run.EvidenceCount = len(report.Evidence)
	run.Status = knowledgestore.ResearchRunStatusCompleted
	run.Error = ""
	run.UpdatedAt = now
	run.FinishedAt = now
	run.Events = append(run.Events, knowledgestore.ResearchRunEvent{ID: id.New("kevt"), Stage: "completed", Message: "Research run completed and report stored.", CreatedAt: now})
	space, err = knowledgestore.AddResearchRun(space, run, now)
	if err != nil {
		o.failKnowledgeResearchRun(ctx, store, spaceID, run, err)
		return
	}
	if err := store.Save(space); err != nil {
		o.failKnowledgeResearchRun(ctx, store, spaceID, run, err)
		return
	}
	space, _ = store.Load(spaceID)
	o.appendKnowledgeEvent(ctx, "knowledge.research_run.completed", space, map[string]any{"run_id": run.ID, "report_id": report.ID, "evidence": len(report.Evidence)})
}

type knowledgeResearchBundle struct {
	Sources      []knowledgeResearchSource `json:"sources"`
	SearchErrors []string                  `json:"search_errors"`
}

type knowledgeResearchSource struct {
	Query       string `json:"query"`
	Kind        string `json:"kind"`
	Provider    string `json:"provider"`
	Title       string `json:"title"`
	URL         string `json:"url"`
	Domain      string `json:"domain"`
	Snippet     string `json:"snippet"`
	Fetched     bool   `json:"fetched"`
	FetchError  string `json:"fetch_error"`
	ContentType string `json:"content_type"`
	PageTitle   string `json:"page_title"`
	Text        string `json:"text"`
}

func (o *Orchestrator) discoverKnowledgeSources(ctx context.Context, store knowledgestore.Repository, space knowledgestore.Space, run knowledgestore.ResearchRun) (knowledgestore.Space, knowledgestore.ResearchRun, error) {
	if o.registry == nil {
		return space, run, errors.New("internet research tool registry is not configured")
	}
	query := firstNonEmptyString(run.Plan.RewrittenObjective, run.Objective, run.Question)
	if query == "" {
		return space, run, errors.New("research discovery query is required")
	}
	maxSources := run.MaxSources
	if maxSources <= 0 {
		maxSources = knowledgeMaxSourcesForDepth(run.Depth)
		run.MaxSources = maxSources
	}
	maxSearches := knowledgeMaxSearchesForDepth(run.Depth)
	queries := knowledgeDiscoveryQueries(run, query, maxSearches)
	toolInput := map[string]any{
		"query":        query,
		"source":       "web",
		"depth":        run.Depth,
		"provider":     "searxng",
		"max_sources":  maxSources,
		"max_searches": maxSearches,
		"fetch":        true,
	}
	if len(queries) > 0 {
		toolInput["queries"] = queries
	}
	raw, err := o.runTool(ctx, "homelabd", "internet.research", toolInput, "")
	if err != nil {
		return space, run, err
	}
	var bundle knowledgeResearchBundle
	if err := json.Unmarshal(raw, &bundle); err != nil {
		return space, run, fmt.Errorf("decode internet research result: %w", err)
	}
	if len(bundle.SearchErrors) > 0 {
		run.Events = append(run.Events, knowledgestore.ResearchRunEvent{
			ID:        id.New("kevt"),
			Stage:     "discovery",
			Message:   "Search returned errors: " + strings.Join(bundle.SearchErrors, "; "),
			CreatedAt: time.Now().UTC(),
		})
	}
	imported := 0
	for index, candidate := range bundle.Sources {
		candidateState := knowledgeCandidateFromResearchSource(candidate, index)
		run.Candidates = appendOrReplaceKnowledgeCandidate(run.Candidates, candidateState)
		if candidate.FetchError != "" {
			candidateState.Status = "failed"
			candidateState.Error = candidate.FetchError
			run.Candidates = appendOrReplaceKnowledgeCandidate(run.Candidates, candidateState)
			if err := saveKnowledgeRun(store, space, run, time.Now().UTC()); err != nil {
				return space, run, err
			}
			continue
		}
		if strings.TrimSpace(candidate.Text) == "" {
			candidateState.Status = "skipped"
			candidateState.Error = "candidate did not include fetched text"
			run.Candidates = appendOrReplaceKnowledgeCandidate(run.Candidates, candidateState)
			if err := saveKnowledgeRun(store, space, run, time.Now().UTC()); err != nil {
				return space, run, err
			}
			continue
		}
		now := time.Now().UTC()
		source, err := knowledgestore.BuildSource(ctx, knowledgestore.AddSourceRequest{
			Title:   firstNonEmptyString(candidate.PageTitle, candidate.Title, candidate.URL),
			Kind:    knowledgestore.SourceKindURL,
			URI:     candidate.URL,
			Content: candidate.Text,
		}, id.New("ksrc"), now, nil)
		if err != nil {
			candidateState.Status = "failed"
			candidateState.Error = err.Error()
			run.Candidates = appendOrReplaceKnowledgeCandidate(run.Candidates, candidateState)
			if err := saveKnowledgeRun(store, space, run, time.Now().UTC()); err != nil {
				return space, run, err
			}
			continue
		}
		source.Provenance.ContentType = firstNonEmptyString(candidate.ContentType, source.Provenance.ContentType)
		source.Provenance.FetchedAt = now
		if source.Provenance.Extractor == "" {
			source.Provenance.Extractor = "internet.research"
		} else if !strings.Contains(source.Provenance.Extractor, "internet.research") {
			source.Provenance.Extractor += "+internet.research"
		}
		analyzed, err := o.knowledgeModel().AnalyzeSource(ctx, source, now)
		if err != nil {
			candidateState.Status = "failed"
			candidateState.Error = err.Error()
			run.Candidates = appendOrReplaceKnowledgeCandidate(run.Candidates, candidateState)
			if err := saveKnowledgeRun(store, space, run, time.Now().UTC()); err != nil {
				return space, run, err
			}
			continue
		}
		space, err = knowledgestore.AddSource(space, analyzed, now)
		if err != nil {
			return space, run, err
		}
		candidateState.Status = "imported"
		candidateState.SourceID = analyzed.ID
		run.Candidates = appendOrReplaceKnowledgeCandidate(run.Candidates, candidateState)
		run.SourceIDs = appendUniqueStrings(run.SourceIDs, analyzed.ID)
		run.Events = append(run.Events, knowledgestore.ResearchRunEvent{ID: id.New("kevt"), Stage: "discovery", Message: "Imported and analysed source: " + analyzed.Title, CreatedAt: now})
		imported++
		space, err = knowledgestore.AddResearchRun(space, run, now)
		if err != nil {
			return space, run, err
		}
		if err := store.Save(space); err != nil {
			return space, run, err
		}
	}
	if imported == 0 {
		if err := saveKnowledgeRun(store, space, run, time.Now().UTC()); err != nil {
			return space, run, err
		}
		return space, run, errors.New("online discovery did not import any usable sources")
	}
	now := time.Now().UTC()
	run.Events = append(run.Events, knowledgestore.ResearchRunEvent{ID: id.New("kevt"), Stage: "discovery", Message: fmt.Sprintf("Imported %d online source%s into the corpus.", imported, pluralSuffix(imported)), CreatedAt: now})
	space, err = knowledgestore.AddResearchRun(space, run, now)
	if err != nil {
		return space, run, err
	}
	if err := store.Save(space); err != nil {
		return space, run, err
	}
	space, err = store.Load(space.ID)
	if err != nil {
		return space, run, err
	}
	_, run, err = loadKnowledgeRun(store, space.ID, run.ID)
	if err != nil {
		return space, run, err
	}
	return space, run, nil
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

func (o *Orchestrator) failKnowledgeResearchRun(ctx context.Context, store knowledgestore.Repository, spaceID string, run knowledgestore.ResearchRun, cause error) {
	space, loaded, err := loadKnowledgeRun(store, spaceID, run.ID)
	if err == nil {
		run = loaded
	} else {
		space, err = store.Load(spaceID)
		if err != nil {
			o.log().Error("failed to load knowledge research run for failure update", "space_id", spaceID, "run_id", run.ID, "error", err, "cause", cause)
			return
		}
	}
	now := time.Now().UTC()
	run.Status = knowledgestore.ResearchRunStatusFailed
	run.Error = strings.TrimSpace(cause.Error())
	run.UpdatedAt = now
	run.FinishedAt = now
	run.Events = append(run.Events, knowledgestore.ResearchRunEvent{ID: id.New("kevt"), Stage: "failed", Message: run.Error, CreatedAt: now})
	space, err = knowledgestore.AddResearchRun(space, run, now)
	if err != nil {
		o.log().Error("failed to update failed knowledge research run", "space_id", spaceID, "run_id", run.ID, "error", err, "cause", cause)
		return
	}
	if err := store.Save(space); err != nil {
		o.log().Error("failed to save failed knowledge research run", "space_id", spaceID, "run_id", run.ID, "error", err, "cause", cause)
		return
	}
	space, _ = store.Load(spaceID)
	o.appendKnowledgeEvent(ctx, "knowledge.research_run.failed", space, map[string]any{"run_id": run.ID, "error": run.Error})
}

func failKnowledgeSource(source knowledgestore.Source, stage string, cause error, now time.Time) knowledgestore.Source {
	started := source.Ingestion.StartedAt
	if started.IsZero() {
		started = now
	}
	source.Ingestion = knowledgestore.SourceIngestion{
		State:       knowledgestore.SourceStatusFailed,
		Stage:       strings.TrimSpace(stage),
		Error:       strings.TrimSpace(cause.Error()),
		StartedAt:   started,
		CompletedAt: now,
	}
	source.UpdatedAt = now
	normalized, err := knowledgestore.NormalizeSource(source)
	if err != nil {
		return source
	}
	return normalized
}

func updateKnowledgeRun(run knowledgestore.ResearchRun, status, stage, message string, now time.Time) knowledgestore.ResearchRun {
	run.Status = status
	run.UpdatedAt = now
	if run.StartedAt.IsZero() {
		run.StartedAt = now
	}
	run.Events = append(run.Events, knowledgestore.ResearchRunEvent{ID: id.New("kevt"), Stage: stage, Message: message, CreatedAt: now})
	return run
}

func saveKnowledgeRun(store knowledgestore.Repository, space knowledgestore.Space, run knowledgestore.ResearchRun, now time.Time) error {
	latest, err := store.Load(space.ID)
	if err == nil {
		space = latest
	}
	space, err = knowledgestore.AddResearchRun(space, run, now)
	if err != nil {
		return err
	}
	return store.Save(space)
}

func loadKnowledgeRun(store knowledgestore.Repository, spaceID, runID string) (knowledgestore.Space, knowledgestore.ResearchRun, error) {
	space, err := store.Load(spaceID)
	if err != nil {
		return knowledgestore.Space{}, knowledgestore.ResearchRun{}, err
	}
	for _, run := range space.ResearchRuns {
		if run.ID == runID {
			return space, run, nil
		}
	}
	return knowledgestore.Space{}, knowledgestore.ResearchRun{}, errors.New("knowledge research run not found")
}

func knowledgeRunRetrievalQuery(run knowledgestore.ResearchRun) (string, error) {
	parts := []string{
		run.Objective,
		run.Question,
		run.Plan.RewrittenObjective,
		strings.Join(run.Plan.SearchQueries, "\n"),
	}
	var compact []string
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			compact = append(compact, part)
		}
	}
	if len(compact) == 0 {
		return "", errors.New("research run has no retrieval query")
	}
	return strings.Join(compact, "\n"), nil
}

func knowledgeDiscoveryQueries(run knowledgestore.ResearchRun, primary string, limit int) []string {
	candidates := append([]string{}, run.Plan.SearchQueries...)
	if len(candidates) == 0 {
		candidates = append(candidates, run.Plan.RewrittenObjective, run.Objective, run.Question, primary)
	}
	return compactKnowledgeStrings(candidates, limit)
}

func effectiveKnowledgeRunSourceIDs(run knowledgestore.ResearchRun) []string {
	return appendUniqueStrings(nil, run.SourceIDs...)
}

func knowledgeMaxSearchesForDepth(depth string) int {
	switch strings.ToLower(strings.TrimSpace(depth)) {
	case "quick":
		return 2
	case "deep":
		return 8
	default:
		return 4
	}
}

func knowledgeMaxSourcesForDepth(depth string) int {
	switch strings.ToLower(strings.TrimSpace(depth)) {
	case "quick":
		return 4
	case "deep":
		return 12
	default:
		return 8
	}
}

func knowledgeCandidateFromResearchSource(source knowledgeResearchSource, index int) knowledgestore.SourceCandidate {
	title := firstNonEmptyString(source.PageTitle, source.Title, source.URL)
	return knowledgestore.SourceCandidate{
		ID:          id.New(fmt.Sprintf("kcand_%02d", index+1)),
		Query:       strings.TrimSpace(source.Query),
		Kind:        strings.TrimSpace(source.Kind),
		Provider:    strings.TrimSpace(source.Provider),
		Title:       strings.TrimSpace(title),
		URL:         strings.TrimSpace(source.URL),
		Domain:      strings.TrimSpace(source.Domain),
		Snippet:     strings.TrimSpace(source.Snippet),
		ContentType: strings.TrimSpace(source.ContentType),
		Status:      "candidate",
	}
}

func appendOrReplaceKnowledgeCandidate(candidates []knowledgestore.SourceCandidate, candidate knowledgestore.SourceCandidate) []knowledgestore.SourceCandidate {
	candidate.ID = strings.TrimSpace(candidate.ID)
	if candidate.ID == "" {
		candidate.ID = id.New("kcand")
	}
	candidate.URL = strings.TrimSpace(candidate.URL)
	for index, existing := range candidates {
		if existing.ID == candidate.ID || (candidate.URL != "" && strings.EqualFold(existing.URL, candidate.URL)) {
			candidates[index] = candidate
			return candidates
		}
	}
	return append(candidates, candidate)
}

func appendUniqueStrings(values []string, next ...string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(values)+len(next))
	for _, value := range append(values, next...) {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func pluralSuffix(count int) string {
	if count == 1 {
		return ""
	}
	return "s"
}

func countKnowledgeSources(space knowledgestore.Space, sourceIDs []string) int {
	allowed := map[string]bool{}
	for _, sourceID := range sourceIDs {
		sourceID = strings.TrimSpace(sourceID)
		if sourceID != "" {
			allowed[sourceID] = true
		}
	}
	count := 0
	for _, source := range space.Sources {
		if source.Ingestion.State == knowledgestore.SourceStatusFailed {
			continue
		}
		if len(allowed) == 0 || allowed[source.ID] {
			count++
		}
	}
	return count
}

func knowledgeUsage(usage llm.Usage) knowledgestore.TokenUsage {
	return knowledgestore.TokenUsage{InputTokens: usage.InputTokens, OutputTokens: usage.OutputTokens, TotalTokens: usage.TotalTokens}
}

func compactKnowledgeStrings(values []string, limit int) []string {
	seen := map[string]bool{}
	var out []string
	for _, value := range values {
		value = strings.Join(strings.Fields(value), " ")
		key := strings.ToLower(value)
		if value == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, value)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}

func addKnowledgeUsage(left, right knowledgestore.TokenUsage) knowledgestore.TokenUsage {
	return knowledgestore.TokenUsage{
		InputTokens:  left.InputTokens + right.InputTokens,
		OutputTokens: left.OutputTokens + right.OutputTokens,
		TotalTokens:  left.TotalTokens + right.TotalTokens,
	}
}

func knowledgeResponseProvider(value string, provider llm.Provider) string {
	value = strings.TrimSpace(value)
	if value != "" {
		return value
	}
	if provider != nil {
		return provider.Name()
	}
	return ""
}

func knowledgeResponseModel(value, configuredModel string) string {
	value = strings.TrimSpace(value)
	if value != "" {
		return value
	}
	return strings.TrimSpace(configuredModel)
}
