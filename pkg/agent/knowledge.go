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

func (o *Orchestrator) knowledgeFetcher() knowledgestore.HTTPFetcher {
	return knowledgestore.HTTPFetcher{Extraction: knowledgestore.TextExtractionOptionsFromConfig(o.cfg.Knowledge)}
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

func (o *Orchestrator) UpdateKnowledgeSpace(ctx context.Context, spaceID string, req knowledgestore.UpdateSpaceRequest) (knowledgestore.Space, string, error) {
	store, err := o.knowledgeStore()
	if err != nil {
		return knowledgestore.Space{}, "", err
	}
	space, err := store.Load(spaceID)
	if err != nil {
		return knowledgestore.Space{}, "", err
	}
	updated, err := knowledgestore.UpdateSpace(space, req, time.Now().UTC())
	if err != nil {
		return knowledgestore.Space{}, "", err
	}
	if err := store.Save(updated); err != nil {
		return knowledgestore.Space{}, "", err
	}
	updated, _ = store.Load(updated.ID)
	o.appendKnowledgeEvent(ctx, "knowledge.space.updated", updated, map[string]any{"title": updated.Title})
	return updated, "Knowledge Space updated: " + updated.Title, nil
}

func (o *Orchestrator) DeleteKnowledgeSpace(ctx context.Context, spaceID string) (string, error) {
	store, err := o.knowledgeStore()
	if err != nil {
		return "", err
	}
	space, err := store.Load(spaceID)
	if err != nil {
		return "", err
	}
	if err := store.Delete(space.ID); err != nil {
		return "", err
	}
	o.appendKnowledgeEvent(ctx, "knowledge.space.deleted", space, map[string]any{"title": space.Title})
	return "Knowledge Space deleted: " + space.Title, nil
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
	source, err := knowledgestore.BuildSource(ctx, req, id.New("ksrc"), now, o.knowledgeFetcher())
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

func (o *Orchestrator) DeleteKnowledgeSource(ctx context.Context, spaceID, sourceID string) (knowledgestore.Space, string, error) {
	store, err := o.knowledgeStore()
	if err != nil {
		return knowledgestore.Space{}, "", err
	}
	space, err := store.Load(spaceID)
	if err != nil {
		return knowledgestore.Space{}, "", err
	}
	updated, source, err := knowledgestore.RemoveSource(space, sourceID, time.Now().UTC())
	if err != nil {
		return knowledgestore.Space{}, "", err
	}
	if err := store.Save(updated); err != nil {
		return knowledgestore.Space{}, "", err
	}
	if err := store.DeleteSourceArtifacts(updated.ID, source.ID); err != nil {
		return knowledgestore.Space{}, "", err
	}
	updated, _ = store.Load(updated.ID)
	o.appendKnowledgeEvent(ctx, "knowledge.source.deleted", updated, map[string]any{"source_id": source.ID, "title": source.Title})
	return updated, "Source deleted: " + source.Title, nil
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

func (o *Orchestrator) AskKnowledgeSpace(ctx context.Context, spaceID string, req knowledgestore.AskRequest) (knowledgestore.Space, knowledgestore.AskResult, knowledgestore.Report, string, error) {
	store, err := o.knowledgeStore()
	if err != nil {
		return knowledgestore.Space{}, knowledgestore.AskResult{}, knowledgestore.Report{}, "", err
	}
	space, err := store.Load(spaceID)
	if err != nil {
		return knowledgestore.Space{}, knowledgestore.AskResult{}, knowledgestore.Report{}, "", err
	}
	now := time.Now().UTC()
	query, err := knowledgestore.QuerySpace(space, knowledgestore.QueryRequest{
		Query:     req.Question,
		SourceIDs: req.SourceIDs,
		Limit:     req.Limit,
	}, now)
	if err != nil {
		return knowledgestore.Space{}, knowledgestore.AskResult{}, knowledgestore.Report{}, "", err
	}
	result, err := o.knowledgeModel().AnswerQuestion(ctx, space, req, query.Evidence, now)
	if err != nil {
		return knowledgestore.Space{}, knowledgestore.AskResult{}, knowledgestore.Report{}, "", err
	}
	report := knowledgestore.Report{
		ID:          id.New("kreport"),
		Question:    result.Question,
		Mode:        knowledgestore.ReportModeAsk,
		Answer:      result.Answer,
		KeyFindings: result.KeyFindings,
		Evidence:    result.Evidence,
		Gaps:        result.Gaps,
		Provider:    result.Provider,
		Model:       result.Model,
		Usage:       result.Usage,
		CreatedAt:   result.CreatedAt,
	}
	space, err = knowledgestore.AddReport(space, report, time.Now().UTC())
	if err != nil {
		return knowledgestore.Space{}, knowledgestore.AskResult{}, knowledgestore.Report{}, "", err
	}
	if err := store.Save(space); err != nil {
		return knowledgestore.Space{}, knowledgestore.AskResult{}, knowledgestore.Report{}, "", err
	}
	space, _ = store.Load(space.ID)
	if len(space.Reports) > 0 {
		report = space.Reports[0]
	}
	o.appendKnowledgeEvent(ctx, "knowledge.ask.created", space, map[string]any{"question": result.Question, "report_id": report.ID, "evidence": len(result.Evidence)})
	return space, result, report, "Grounded answer saved.", nil
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
	o.startKnowledgeResearchRunWorker(store, space.ID, run.ID)
	return space, run, knowledgestore.Report{}, "Research run queued.", nil
}

func (o *Orchestrator) RecoverKnowledgeResearchRuns(ctx context.Context) (int, error) {
	store, err := o.knowledgeStore()
	if err != nil {
		return 0, err
	}
	spaces, err := store.List()
	if err != nil {
		return 0, err
	}
	recovered := 0
	now := time.Now().UTC()
	for _, space := range spaces {
		changed := false
		for _, run := range space.ResearchRuns {
			if !knowledgeResearchRunResumable(run.Status) {
				continue
			}
			run.Error = ""
			run.UpdatedAt = now
			run.Events = append(run.Events, knowledgestore.ResearchRunEvent{
				ID:        id.New("kevt"),
				Stage:     "recovered",
				Message:   "Research run recovered after homelabd startup.",
				CreatedAt: now,
			})
			var addErr error
			space, addErr = knowledgestore.AddResearchRun(space, run, now)
			if addErr != nil {
				return recovered, addErr
			}
			changed = true
		}
		if changed {
			if err := store.Save(space); err != nil {
				return recovered, err
			}
		}
		for _, run := range space.ResearchRuns {
			if !knowledgeResearchRunResumable(run.Status) {
				continue
			}
			if o.startKnowledgeResearchRunWorker(store, space.ID, run.ID) {
				recovered++
			}
		}
	}
	if recovered > 0 {
		o.log().Info("recovered knowledge research runs", "count", recovered)
	}
	return recovered, nil
}

func (o *Orchestrator) startKnowledgeResearchRunWorker(store knowledgestore.Repository, spaceID, runID string) bool {
	key := strings.TrimSpace(spaceID) + "/" + strings.TrimSpace(runID)
	o.knowledgeMu.Lock()
	if o.knowledgeRuns == nil {
		o.knowledgeRuns = make(map[string]struct{})
	}
	if _, exists := o.knowledgeRuns[key]; exists {
		o.knowledgeMu.Unlock()
		return false
	}
	o.knowledgeRuns[key] = struct{}{}
	o.knowledgeMu.Unlock()
	go func() {
		defer func() {
			o.knowledgeMu.Lock()
			delete(o.knowledgeRuns, key)
			o.knowledgeMu.Unlock()
		}()
		o.executeKnowledgeResearchRun(context.Background(), store, spaceID, runID)
	}()
	return true
}

func (o *Orchestrator) executeKnowledgeResearchRun(ctx context.Context, store knowledgestore.Repository, spaceID, runID string) {
	model := o.knowledgeModel()
	space, run, err := loadKnowledgeRun(store, spaceID, runID)
	if err != nil {
		o.log().Error("failed to load queued knowledge research run", "space_id", spaceID, "run_id", runID, "error", err)
		return
	}
	if !knowledgeResearchRunResumable(run.Status) {
		return
	}
	if run.ReportID != "" && knowledgeReportExists(space, run.ReportID) {
		now := time.Now().UTC()
		run.Status = knowledgestore.ResearchRunStatusCompleted
		run.Error = ""
		run.UpdatedAt = now
		if run.FinishedAt.IsZero() {
			run.FinishedAt = now
		}
		run.Events = append(run.Events, knowledgestore.ResearchRunEvent{ID: id.New("kevt"), Stage: "completed", Message: "Research run completed from existing report artefact.", CreatedAt: now})
		if err := saveKnowledgeRun(store, space, run, now); err != nil {
			o.log().Error("failed to save completed recovered knowledge research run", "space_id", spaceID, "run_id", runID, "error", err)
		}
		return
	}
	now := time.Now().UTC()
	if !knowledgeRunHasPlan(run) {
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
		})
		if err != nil {
			o.failKnowledgeResearchRun(ctx, store, spaceID, run, err)
			return
		}
		run.Plan = plan
		run.Coverage = knowledgestore.BuildResearchCoverage(run, nil)
		run.Provider = knowledgeResponseProvider(planResp.Provider, o.provider)
		run.Model = knowledgeResponseModel(planResp.Model, o.model)
		run.Usage = addKnowledgeUsage(run.Usage, knowledgeUsage(planResp.Usage))
	}
	if run.DiscoverSources && knowledgeResearchRunNeedsDiscovery(run.Status) {
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
	queryResult, err := knowledgestore.ResearchEvidence(space, run, knowledgeEvidenceLimitForDepth(run.Depth))
	if err != nil {
		o.failKnowledgeResearchRun(ctx, store, spaceID, run, err)
		return
	}
	run.SourcesExamined = countKnowledgeSources(space, run.SourceIDs)
	run.EvidenceCount = len(queryResult)
	run.Coverage = knowledgestore.BuildResearchCoverage(run, queryResult)
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
	report, err := model.SynthesizeReport(ctx, space, run, queryResult, id.New("kreport"), now)
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
	Truncated   bool   `json:"truncated"`
}

type knowledgeDiscoveryLoopResult struct {
	Imported        int
	Usable          int
	Accepted        int
	Rejected        int
	Failed          int
	CandidateIDs    []string
	SourceIDs       []string
	FollowUpQueries []string
}

func (o *Orchestrator) discoverKnowledgeSources(ctx context.Context, store knowledgestore.Repository, space knowledgestore.Space, run knowledgestore.ResearchRun) (knowledgestore.Space, knowledgestore.ResearchRun, error) {
	if o.registry == nil {
		return space, run, errors.New("internet research tool registry is not configured")
	}
	primaryQuery := firstNonEmptyString(run.Plan.RewrittenObjective, run.Objective, run.Question)
	if primaryQuery == "" {
		return space, run, errors.New("research discovery query is required")
	}
	maxSearches := knowledgeMaxSearchesForDepth(run.Depth)
	nextQueries := knowledgeNextDiscoveryQueries(run, primaryQuery, maxSearches)
	seenQueries := knowledgeSeenLoopQueries(run.ResearchLoops)
	model := o.knowledgeModel()
	for {
		queries := filterKnowledgeNewQueries(nextQueries, seenQueries, maxSearches)
		if len(queries) == 0 {
			if countKnowledgeSources(space, run.SourceIDs) > 0 {
				now := time.Now().UTC()
				run.StopReason = firstNonEmptyString(run.StopReason, "Stopped because no new follow-up queries remained after de-duplication.")
				run.Events = append(run.Events, knowledgestore.ResearchRunEvent{ID: id.New("kevt"), Stage: "coverage", Message: run.StopReason, CreatedAt: now})
				if err := saveKnowledgeRun(store, space, run, now); err != nil {
					return space, run, err
				}
				return space, run, nil
			}
			return space, run, errors.New("online discovery did not produce any new search queries")
		}
		for _, searched := range queries {
			seenQueries[strings.ToLower(strings.TrimSpace(searched))] = true
		}
		now := time.Now().UTC()
		loop := knowledgestore.ResearchLoop{
			ID:        id.New("kloop"),
			Index:     len(run.ResearchLoops) + 1,
			Query:     primaryQuery,
			Queries:   queries,
			Status:    "searching",
			StartedAt: now,
		}
		run.ResearchLoops = appendOrReplaceResearchLoop(run.ResearchLoops, loop)
		run.Events = append(run.Events, knowledgestore.ResearchRunEvent{ID: id.New("kevt"), Stage: "discovery_loop", Message: fmt.Sprintf("Research loop %d searching %d quer%s.", loop.Index, len(queries), pluralY(len(queries))), CreatedAt: now})
		if err := saveKnowledgeRun(store, space, run, now); err != nil {
			return space, run, err
		}
		toolInput := map[string]any{
			"query":        primaryQuery,
			"queries":      queries,
			"source":       "all",
			"depth":        run.Depth,
			"provider":     "searxng",
			"language":     knowledgeDiscoveryLanguage(run),
			"max_searches": maxSearches,
			"fetch":        true,
		}
		raw, err := o.runTool(ctx, "homelabd", "internet.research", toolInput, "")
		if err != nil {
			return space, run, err
		}
		var bundle knowledgeResearchBundle
		if err := json.Unmarshal(raw, &bundle); err != nil {
			return space, run, fmt.Errorf("decode internet research result: %w", err)
		}
		loop.Status = "reading"
		run.ResearchLoops = appendOrReplaceResearchLoop(run.ResearchLoops, loop)
		if err := saveKnowledgeRun(store, space, run, time.Now().UTC()); err != nil {
			return space, run, err
		}
		var result knowledgeDiscoveryLoopResult
		space, run, result, err = o.processKnowledgeDiscoveryBundle(ctx, store, space, run, bundle)
		if err != nil {
			return space, run, err
		}
		loop.CandidateIDs = appendUniqueStrings(loop.CandidateIDs, result.CandidateIDs...)
		loop.SourceIDs = appendUniqueStrings(loop.SourceIDs, result.SourceIDs...)
		loop.AcceptedCount = result.Accepted
		loop.RejectedCount = result.Rejected
		loop.FailedCount = result.Failed
		if countKnowledgeSources(space, run.SourceIDs) == 0 {
			loop.Status = "completed"
			loop.Decision = "continue"
			loop.StopReason = "No usable sources were imported in this loop."
			loop.FollowUpQueries = result.FollowUpQueries
			loop.FinishedAt = time.Now().UTC()
			run.ResearchLoops = appendOrReplaceResearchLoop(run.ResearchLoops, loop)
			if len(result.FollowUpQueries) == 0 {
				if err := saveKnowledgeRun(store, space, run, loop.FinishedAt); err != nil {
					return space, run, err
				}
				return space, run, errors.New("online discovery did not import any usable sources")
			}
			nextQueries = compactKnowledgeStrings(result.FollowUpQueries, maxSearches)
			if err := saveKnowledgeRun(store, space, run, loop.FinishedAt); err != nil {
				return space, run, err
			}
			continue
		}
		queryResult, err := knowledgestore.ResearchEvidence(space, run, knowledgeEvidenceLimitForDepth(run.Depth))
		if err != nil {
			return space, run, err
		}
		run.EvidenceCount = len(queryResult)
		run.Coverage = knowledgestore.BuildResearchCoverage(run, queryResult)
		loop.EvidenceCount = len(queryResult)
		loop.Status = "evaluating"
		run.ResearchLoops = appendOrReplaceResearchLoop(run.ResearchLoops, loop)
		if err := saveKnowledgeRun(store, space, run, time.Now().UTC()); err != nil {
			return space, run, err
		}
		decision, decisionResp, err := model.EvaluateResearchCoverage(ctx, space, run, loop, queryResult, time.Now().UTC())
		if err != nil {
			return space, run, err
		}
		run.Provider = firstNonEmptyString(run.Provider, knowledgeResponseProvider(decisionResp.Provider, o.provider))
		run.Model = firstNonEmptyString(run.Model, knowledgeResponseModel(decisionResp.Model, o.model))
		usage := knowledgeUsage(decisionResp.Usage)
		run.Usage = addKnowledgeUsage(run.Usage, usage)
		loop.Usage = addKnowledgeUsage(loop.Usage, usage)
		loop.Decision = decision.Decision
		loop.StopReason = decision.StopReason
		loop.SupportedClaims = decision.SupportedClaims
		loop.Gaps = decision.Gaps
		loop.FollowUpQueries = decision.FollowUpQueries
		loop.Coverage = decision.Coverage
		loop.Status = "completed"
		loop.FinishedAt = time.Now().UTC()
		run.StopReason = decision.StopReason
		run.ResearchLoops = appendOrReplaceResearchLoop(run.ResearchLoops, loop)
		if decision.Decision == "complete" {
			run.Events = append(run.Events, knowledgestore.ResearchRunEvent{ID: id.New("kevt"), Stage: "coverage", Message: "Coverage sufficient: " + decision.StopReason, CreatedAt: loop.FinishedAt})
			if err := saveKnowledgeRun(store, space, run, loop.FinishedAt); err != nil {
				return space, run, err
			}
			return space, run, nil
		}
		followUps := append([]string{}, decision.FollowUpQueries...)
		followUps = append(followUps, result.FollowUpQueries...)
		nextQueries = compactKnowledgeStrings(followUps, maxSearches)
		if len(nextQueries) == 0 {
			now = time.Now().UTC()
			run.StopReason = firstNonEmptyString(decision.StopReason, "Coverage remains incomplete, but no follow-up queries were available.")
			run.Events = append(run.Events, knowledgestore.ResearchRunEvent{ID: id.New("kevt"), Stage: "coverage", Message: run.StopReason, CreatedAt: now})
			if err := saveKnowledgeRun(store, space, run, now); err != nil {
				return space, run, err
			}
			return space, run, nil
		}
		if err := saveKnowledgeRun(store, space, run, loop.FinishedAt); err != nil {
			return space, run, err
		}
	}
}

func (o *Orchestrator) processKnowledgeDiscoveryBundle(ctx context.Context, store knowledgestore.Repository, space knowledgestore.Space, run knowledgestore.ResearchRun, bundle knowledgeResearchBundle) (knowledgestore.Space, knowledgestore.ResearchRun, knowledgeDiscoveryLoopResult, error) {
	result := knowledgeDiscoveryLoopResult{}
	var err error
	filteredReasons := map[string]int{}
	if len(bundle.SearchErrors) > 0 {
		result.Failed += len(bundle.SearchErrors)
		run.Events = append(run.Events, knowledgestore.ResearchRunEvent{
			ID:        id.New("kevt"),
			Stage:     "discovery",
			Message:   "Search returned errors: " + strings.Join(bundle.SearchErrors, "; "),
			CreatedAt: time.Now().UTC(),
		})
	}
	for index, candidate := range bundle.Sources {
		candidateState := knowledgeCandidateFromResearchSource(candidate, index)
		existingCandidate, hasExistingCandidate := findKnowledgeCandidate(run.Candidates, candidateState)
		if hasExistingCandidate && existingCandidate.Status == "accepted" && existingCandidate.SourceID != "" && knowledgeSourceExists(space, existingCandidate.SourceID) {
			run.SourceIDs = appendUniqueStrings(run.SourceIDs, existingCandidate.SourceID)
			result.Usable++
			result.Accepted++
			result.CandidateIDs = appendUniqueStrings(result.CandidateIDs, existingCandidate.ID)
			result.SourceIDs = appendUniqueStrings(result.SourceIDs, existingCandidate.SourceID)
			continue
		}
		if hasExistingCandidate && existingCandidate.Status == "rejected" {
			result.Rejected++
			result.CandidateIDs = appendUniqueStrings(result.CandidateIDs, existingCandidate.ID)
			continue
		}
		if skip, reason := knowledgeCandidatePreflight(run, candidateState); skip {
			filteredReasons[reason]++
			result.Rejected++
			continue
		}
		run.Candidates = appendOrReplaceKnowledgeCandidate(run.Candidates, candidateState)
		result.CandidateIDs = appendUniqueStrings(result.CandidateIDs, candidateState.ID)
		if candidate.FetchError != "" {
			candidateState.Status = "failed"
			candidateState.Error = candidate.FetchError
			candidateState.ExtractionState = "failed"
			candidateState.ExtractionMessage = candidate.FetchError
			run.Candidates = appendOrReplaceKnowledgeCandidate(run.Candidates, candidateState)
			result.Failed++
			if err := saveKnowledgeRun(store, space, run, time.Now().UTC()); err != nil {
				return space, run, result, err
			}
			continue
		}
		if strings.TrimSpace(candidate.Text) == "" {
			candidateState.Status = "skipped"
			candidateState.Error = "candidate did not include fetched text"
			candidateState.ExtractionState = "failed"
			candidateState.ExtractionMessage = candidateState.Error
			run.Candidates = appendOrReplaceKnowledgeCandidate(run.Candidates, candidateState)
			result.Failed++
			if err := saveKnowledgeRun(store, space, run, time.Now().UTC()); err != nil {
				return space, run, result, err
			}
			continue
		}
		if knowledgeExtractionFailedText(candidate.Text) {
			candidateState.Status = "failed"
			candidateState.Error = strings.TrimSpace(candidate.Text)
			candidateState.ExtractionState = "failed"
			candidateState.ExtractionMessage = candidateState.Error
			run.Candidates = appendOrReplaceKnowledgeCandidate(run.Candidates, candidateState)
			result.Failed++
			if err := saveKnowledgeRun(store, space, run, time.Now().UTC()); err != nil {
				return space, run, result, err
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
			result.Failed++
			if err := saveKnowledgeRun(store, space, run, time.Now().UTC()); err != nil {
				return space, run, result, err
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
			candidateState.ExtractionState = firstNonEmptyString(candidateState.ExtractionState, "text")
			run.Candidates = appendOrReplaceKnowledgeCandidate(run.Candidates, candidateState)
			result.Failed++
			if err := saveKnowledgeRun(store, space, run, time.Now().UTC()); err != nil {
				return space, run, result, err
			}
			continue
		}
		candidateState.WordCount = analyzed.WordCount
		candidateState.ExtractionState = "text"
		if candidate.Truncated {
			candidateState.ExtractionMessage = "Fetched text was truncated for model analysis."
		} else {
			candidateState.ExtractionMessage = "Fetched text extracted for model analysis."
		}
		evaluation, evalResp, err := o.knowledgeModel().EvaluateSourceForRun(ctx, analyzed, run, candidateState, now)
		if err != nil {
			candidateState.Status = "failed"
			candidateState.Error = err.Error()
			run.Candidates = appendOrReplaceKnowledgeCandidate(run.Candidates, candidateState)
			result.Failed++
			if err := saveKnowledgeRun(store, space, run, time.Now().UTC()); err != nil {
				return space, run, result, err
			}
			continue
		}
		run.Provider = firstNonEmptyString(run.Provider, knowledgeResponseProvider(evalResp.Provider, o.provider))
		run.Model = firstNonEmptyString(run.Model, knowledgeResponseModel(evalResp.Model, o.model))
		run.Usage = addKnowledgeUsage(run.Usage, knowledgeUsage(evalResp.Usage))
		candidateState.Usefulness = evaluation.Decision
		candidateState.RelevanceScore = evaluation.RelevanceScore
		candidateState.Coverage = evaluation.Coverage
		candidateState.ExtractionMessage = firstNonEmptyString(evaluation.Reason, candidateState.ExtractionMessage)
		result.FollowUpQueries = appendUniqueStrings(result.FollowUpQueries, evaluation.FollowUpQueries...)
		if evaluation.Decision == "reject" {
			candidateState.Status = "rejected"
			run.Candidates = appendOrReplaceKnowledgeCandidate(run.Candidates, candidateState)
			run.Events = append(run.Events, knowledgestore.ResearchRunEvent{ID: id.New("kevt"), Stage: "discovery", Message: "Rejected source as not useful for this run: " + analyzed.Title, CreatedAt: now})
			result.Rejected++
			if err := saveKnowledgeRun(store, space, run, time.Now().UTC()); err != nil {
				return space, run, result, err
			}
			continue
		}
		space, err = knowledgestore.AddSource(space, analyzed, now)
		if err != nil {
			return space, run, result, err
		}
		candidateState.Status = "accepted"
		candidateState.SourceID = analyzed.ID
		run.Candidates = appendOrReplaceKnowledgeCandidate(run.Candidates, candidateState)
		run.SourceIDs = appendUniqueStrings(run.SourceIDs, analyzed.ID)
		run.Events = append(run.Events, knowledgestore.ResearchRunEvent{ID: id.New("kevt"), Stage: "discovery", Message: "Imported and analysed source: " + analyzed.Title, CreatedAt: now})
		result.Imported++
		result.Usable++
		result.Accepted++
		result.SourceIDs = appendUniqueStrings(result.SourceIDs, analyzed.ID)
		space, err = knowledgestore.AddResearchRun(space, run, now)
		if err != nil {
			return space, run, result, err
		}
		if err := store.Save(space); err != nil {
			return space, run, result, err
		}
	}
	now := time.Now().UTC()
	if len(filteredReasons) > 0 {
		run.Events = append(run.Events, knowledgestore.ResearchRunEvent{ID: id.New("kevt"), Stage: "discovery", Message: knowledgeFilteredCandidatesMessage(filteredReasons), CreatedAt: now})
	}
	if result.Imported > 0 {
		run.Events = append(run.Events, knowledgestore.ResearchRunEvent{ID: id.New("kevt"), Stage: "discovery", Message: fmt.Sprintf("Imported %d online source%s into the corpus.", result.Imported, pluralSuffix(result.Imported)), CreatedAt: now})
	} else if result.Usable > 0 {
		run.Events = append(run.Events, knowledgestore.ResearchRunEvent{ID: id.New("kevt"), Stage: "discovery", Message: "Reused previously imported discovery sources.", CreatedAt: now})
	}
	space, err = knowledgestore.AddResearchRun(space, run, now)
	if err != nil {
		return space, run, result, err
	}
	if err := store.Save(space); err != nil {
		return space, run, result, err
	}
	space, err = store.Load(space.ID)
	if err != nil {
		return space, run, result, err
	}
	_, run, err = loadKnowledgeRun(store, space.ID, run.ID)
	if err != nil {
		return space, run, result, err
	}
	return space, run, result, nil
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

func knowledgeResearchRunResumable(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case knowledgestore.ResearchRunStatusQueued,
		knowledgestore.ResearchRunStatusPlanning,
		knowledgestore.ResearchRunStatusDiscovering,
		knowledgestore.ResearchRunStatusRetrieving,
		knowledgestore.ResearchRunStatusReading,
		knowledgestore.ResearchRunStatusSynthesizing,
		knowledgestore.ResearchRunStatusReviewing:
		return true
	default:
		return false
	}
}

func knowledgeResearchRunNeedsDiscovery(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case knowledgestore.ResearchRunStatusRetrieving,
		knowledgestore.ResearchRunStatusReading,
		knowledgestore.ResearchRunStatusSynthesizing,
		knowledgestore.ResearchRunStatusReviewing:
		return false
	default:
		return true
	}
}

func knowledgeRunHasPlan(run knowledgestore.ResearchRun) bool {
	return strings.TrimSpace(run.Plan.RewrittenObjective) != "" ||
		len(run.Plan.SearchQueries) > 0 ||
		len(run.Plan.Steps) > 0 ||
		len(run.Plan.ExpectedOutputs) > 0
}

func knowledgeReportExists(space knowledgestore.Space, reportID string) bool {
	reportID = strings.TrimSpace(reportID)
	if reportID == "" {
		return false
	}
	for _, report := range space.Reports {
		if report.ID == reportID {
			return true
		}
	}
	return false
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

func knowledgeNextDiscoveryQueries(run knowledgestore.ResearchRun, primary string, limit int) []string {
	for index := len(run.ResearchLoops) - 1; index >= 0; index-- {
		loop := run.ResearchLoops[index]
		if strings.EqualFold(strings.TrimSpace(loop.Decision), "continue") && len(loop.FollowUpQueries) > 0 {
			return compactKnowledgeStrings(loop.FollowUpQueries, limit)
		}
	}
	return knowledgeDiscoveryQueries(run, primary, limit)
}

func knowledgeSeenLoopQueries(loops []knowledgestore.ResearchLoop) map[string]bool {
	seen := map[string]bool{}
	for _, loop := range loops {
		for _, query := range append([]string{loop.Query}, loop.Queries...) {
			query = strings.ToLower(strings.TrimSpace(query))
			if query != "" {
				seen[query] = true
			}
		}
	}
	return seen
}

func filterKnowledgeNewQueries(queries []string, seen map[string]bool, limit int) []string {
	var out []string
	local := map[string]bool{}
	for _, query := range queries {
		query = strings.Join(strings.Fields(query), " ")
		key := strings.ToLower(strings.TrimSpace(query))
		if key == "" || seen[key] || local[key] {
			continue
		}
		local[key] = true
		out = append(out, query)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
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

func knowledgeEvidenceLimitForDepth(depth string) int {
	switch strings.ToLower(strings.TrimSpace(depth)) {
	case "quick":
		return 16
	case "deep":
		return 48
	default:
		return 32
	}
}

func knowledgeDiscoveryLanguage(run knowledgestore.ResearchRun) string {
	text := strings.Join([]string{
		run.Objective,
		run.Question,
		run.Scope,
		run.Plan.RewrittenObjective,
		strings.Join(run.Plan.SearchQueries, " "),
		strings.Join(run.Plan.Steps, " "),
		strings.Join(run.Plan.ExpectedOutputs, " "),
	}, " ")
	if language := knowledgeInferLanguage(text); language != "" {
		return language
	}
	return "en"
}

func knowledgeInferLanguage(value string) string {
	lower := " " + strings.ToLower(strings.Join(strings.Fields(value), " ")) + " "
	for _, r := range lower {
		switch {
		case r >= 0x0400 && r <= 0x052f:
			return "ru"
		case r >= 0x0370 && r <= 0x03ff:
			return "el"
		case r >= 0x0590 && r <= 0x05ff:
			return "he"
		case r >= 0x0600 && r <= 0x06ff:
			return "ar"
		case r >= 0x0900 && r <= 0x097f:
			return "hi"
		case r >= 0x3040 && r <= 0x30ff:
			return "ja"
		case r >= 0x4e00 && r <= 0x9fff:
			return "zh"
		case r >= 0xac00 && r <= 0xd7af:
			return "ko"
		case r >= 0x0e00 && r <= 0x0e7f:
			return "th"
		}
	}
	switch {
	case strings.ContainsAny(lower, "¿¡ñ") ||
		knowledgeContainsAny(lower, []string{" qué ", " como ", " dónde ", " cuales ", " cuáles ", " para ", " porque ", " queso ", " quesos ", " frijol ", " frijoles ", " habas ", " cultivo ", " cultivan "}):
		return "es"
	case strings.ContainsAny(lower, "àâçéèêëîïôùûüÿœæ") ||
		knowledgeContainsAny(lower, []string{" quel ", " quelle ", " quels ", " quelles ", " dans ", " pour ", " avec ", " fromage ", " fromages ", " haricot ", " haricots "}):
		return "fr"
	case strings.ContainsAny(lower, "äöüß") ||
		knowledgeContainsAny(lower, []string{" der ", " die ", " das ", " und ", " mit ", " für ", " käse ", " bohnen "}):
		return "de"
	case strings.ContainsAny(lower, "ãõ") ||
		knowledgeContainsAny(lower, []string{" quais ", " como ", " onde ", " para ", " porque ", " queijo ", " queijos ", " feijão ", " feijões "}):
		return "pt"
	case knowledgeContainsAny(lower, []string{" quale ", " quali ", " come ", " dove ", " perché ", " formaggio ", " formaggi ", " fagiolo ", " fagioli "}):
		return "it"
	default:
		return ""
	}
}

func knowledgeCandidatePreflight(run knowledgestore.ResearchRun, candidate knowledgestore.SourceCandidate) (bool, string) {
	text := strings.ToLower(strings.Join([]string{
		candidate.Title,
		candidate.Snippet,
		candidate.URL,
		candidate.Domain,
	}, " "))
	if knowledgeContainsAny(text, []string{
		"stripchat",
		"chaturbate",
		"onlyfans",
		"camgirl",
		"webcam model",
		"webcam recorder",
		"adult streaming",
		"porn",
		"xxx",
		"erotic",
		"nude",
	}) {
		return true, "adult or streaming-site search result"
	}
	if knowledgeCandidateIsSourceCodeHost(candidate) && !knowledgeRunAllowsCodeSources(run) {
		return true, "source-code host unrelated to a non-software research objective"
	}
	if !knowledgeCandidateHasResearchOverlap(run, candidate) {
		return true, "low-overlap search result"
	}
	return false, ""
}

func knowledgeFilteredCandidatesMessage(reasons map[string]int) string {
	total := 0
	items := make([]string, 0, len(reasons))
	for reason, count := range reasons {
		total += count
		items = append(items, fmt.Sprintf("%d %s", count, reason))
	}
	sort.Strings(items)
	return fmt.Sprintf("Filtered %d search result%s before the candidate pool: %s.", total, pluralSuffix(total), strings.Join(items, "; "))
}

func knowledgeContainsAny(value string, needles []string) bool {
	for _, needle := range needles {
		needle = strings.TrimSpace(strings.ToLower(needle))
		if needle != "" && strings.Contains(value, needle) {
			return true
		}
	}
	return false
}

func knowledgeCandidateIsSourceCodeHost(candidate knowledgestore.SourceCandidate) bool {
	domain := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(candidate.Domain)), "www.")
	url := strings.ToLower(strings.TrimSpace(candidate.URL))
	for _, host := range []string{"github.com", "gitlab.com", "bitbucket.org", "sourceforge.net", "npmjs.com", "pkg.go.dev"} {
		if domain == host || strings.HasSuffix(domain, "."+host) || strings.Contains(url, "://"+host+"/") || strings.Contains(url, "://www."+host+"/") {
			return true
		}
	}
	return false
}

func knowledgeRunAllowsCodeSources(run knowledgestore.ResearchRun) bool {
	text := strings.ToLower(strings.Join([]string{
		run.Objective,
		run.Question,
		run.Scope,
		run.Plan.RewrittenObjective,
		strings.Join(run.Plan.SearchQueries, " "),
		strings.Join(run.Plan.Steps, " "),
		strings.Join(run.Plan.ExpectedOutputs, " "),
	}, " "))
	if knowledgeContainsAny(text, []string{"github", "gitlab", "source code", "repository", "programming", "software", "developer", "api", "sdk", "library", "package", "module", "implementation"}) {
		return true
	}
	terms := knowledgeTokenSet(text)
	for _, term := range []string{"code", "coding", "repo", "repos", "release", "releases", "changelog", "commit", "commits"} {
		if terms[term] {
			return true
		}
	}
	return false
}

func knowledgeCandidateHasResearchOverlap(run knowledgestore.ResearchRun, candidate knowledgestore.SourceCandidate) bool {
	researchTerms := knowledgeRunResearchTerms(run)
	if len(researchTerms) == 0 {
		return true
	}
	candidateTerms := knowledgeTokenSet(strings.Join([]string{
		candidate.Title,
		candidate.Snippet,
		candidate.URL,
		candidate.Domain,
	}, " "))
	for term := range candidateTerms {
		if researchTerms[term] {
			return true
		}
	}
	return false
}

func knowledgeRunResearchTerms(run knowledgestore.ResearchRun) map[string]bool {
	return knowledgeTokenSet(strings.Join([]string{
		run.Objective,
		run.Question,
		run.Scope,
		run.Plan.RewrittenObjective,
		strings.Join(run.Plan.SearchQueries, " "),
		strings.Join(run.Plan.Steps, " "),
		strings.Join(run.Plan.ExpectedOutputs, " "),
	}, " "))
}

func knowledgeTokenSet(value string) map[string]bool {
	cleaned := strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' {
			return r
		}
		if r >= '0' && r <= '9' {
			return r
		}
		return ' '
	}, strings.ToLower(value))
	terms := map[string]bool{}
	for _, token := range strings.Fields(cleaned) {
		knowledgeAddToken(terms, token)
	}
	return terms
}

func knowledgeAddToken(terms map[string]bool, token string) {
	token = strings.TrimSpace(strings.ToLower(token))
	if len(token) < 3 || knowledgeStopToken(token) {
		return
	}
	terms[token] = true
	if strings.HasSuffix(token, "ies") && len(token) > 4 {
		terms[strings.TrimSuffix(token, "ies")+"y"] = true
	}
	if strings.HasSuffix(token, "s") && len(token) > 4 {
		terms[strings.TrimSuffix(token, "s")] = true
	}
}

func knowledgeStopToken(token string) bool {
	switch token {
	case "about", "after", "again", "against", "all", "also", "and", "any", "are", "around", "because", "been", "before", "being", "between", "both", "but", "can", "could", "current", "does", "each", "every", "few", "for", "from", "had", "has", "have", "having", "how", "into", "its", "kind", "kinds", "latest", "more", "most", "near", "not", "now", "objective", "off", "online", "other", "our", "out", "over", "per", "produce", "query", "question", "report", "research", "run", "same", "search", "should", "some", "source", "sources", "specific", "study", "such", "than", "that", "the", "their", "then", "there", "these", "this", "those", "through", "type", "types", "use", "using", "want", "what", "when", "where", "which", "while", "why", "with", "within", "would", "www":
		return true
	default:
		return false
	}
}

func knowledgeExtractionFailedText(value string) bool {
	lower := strings.ToLower(strings.TrimSpace(value))
	return strings.HasPrefix(lower, "unsupported content type for text extraction:") ||
		strings.Contains(lower, "did not contain extractable text") ||
		strings.Contains(lower, "no text operators found")
}

func knowledgeCandidateFromResearchSource(source knowledgeResearchSource, index int) knowledgestore.SourceCandidate {
	title := firstNonEmptyString(source.PageTitle, source.Title, source.URL)
	extractionState := ""
	if source.Fetched {
		extractionState = "text"
		if strings.Contains(strings.ToLower(source.ContentType), "pdf") {
			extractionState = "pdf_text"
		}
	}
	return knowledgestore.SourceCandidate{
		ID:              id.New(fmt.Sprintf("kcand_%02d", index+1)),
		Query:           strings.TrimSpace(source.Query),
		Kind:            strings.TrimSpace(source.Kind),
		Provider:        strings.TrimSpace(source.Provider),
		Title:           strings.TrimSpace(title),
		URL:             strings.TrimSpace(source.URL),
		Domain:          strings.TrimSpace(source.Domain),
		Snippet:         strings.TrimSpace(source.Snippet),
		ContentType:     strings.TrimSpace(source.ContentType),
		Fetched:         source.Fetched,
		ExtractionState: extractionState,
		Status:          "candidate",
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

func appendOrReplaceResearchLoop(loops []knowledgestore.ResearchLoop, loop knowledgestore.ResearchLoop) []knowledgestore.ResearchLoop {
	loop.ID = strings.TrimSpace(loop.ID)
	if loop.ID == "" {
		loop.ID = id.New("kloop")
	}
	for index, existing := range loops {
		if existing.ID == loop.ID {
			loops[index] = loop
			return loops
		}
	}
	return append(loops, loop)
}

func findKnowledgeCandidate(candidates []knowledgestore.SourceCandidate, candidate knowledgestore.SourceCandidate) (knowledgestore.SourceCandidate, bool) {
	id := strings.TrimSpace(candidate.ID)
	url := strings.TrimSpace(candidate.URL)
	for _, existing := range candidates {
		if id != "" && existing.ID == id {
			return existing, true
		}
		if url != "" && strings.EqualFold(existing.URL, url) {
			return existing, true
		}
	}
	return knowledgestore.SourceCandidate{}, false
}

func knowledgeSourceExists(space knowledgestore.Space, sourceID string) bool {
	sourceID = strings.TrimSpace(sourceID)
	if sourceID == "" {
		return false
	}
	for _, source := range space.Sources {
		if source.ID == sourceID {
			return true
		}
	}
	return false
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

func pluralY(count int) string {
	if count == 1 {
		return "y"
	}
	return "ies"
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
