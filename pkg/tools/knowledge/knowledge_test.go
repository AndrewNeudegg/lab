package knowledge

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	knowledgestore "github.com/andrewneudegg/lab/pkg/knowledge"
	"github.com/andrewneudegg/lab/pkg/tool"
)

func TestKnowledgeToolsManageCorpusAndQueueResearch(t *testing.T) {
	now := time.Date(2026, 5, 4, 15, 0, 0, 0, time.UTC)
	store := knowledgestore.NewStore(filepath.Join(t.TempDir(), "knowledge"))
	var sourceSeq int
	var runReq knowledgestore.CreateResearchRunRequest
	base := Base{
		Store: store,
		CreateSpace: func(_ context.Context, req knowledgestore.CreateSpaceRequest) (knowledgestore.Space, string, error) {
			space, err := knowledgestore.NewSpace(req, "kspace_tool", now)
			if err != nil {
				return knowledgestore.Space{}, "", err
			}
			if err := store.Save(space); err != nil {
				return knowledgestore.Space{}, "", err
			}
			return space, "Knowledge Space created.", nil
		},
		AddSource: func(_ context.Context, spaceID string, req knowledgestore.AddSourceRequest) (knowledgestore.Space, knowledgestore.Source, string, error) {
			space, err := store.Load(spaceID)
			if err != nil {
				return knowledgestore.Space{}, knowledgestore.Source{}, "", err
			}
			sourceSeq++
			source, err := knowledgestore.NewSource(req, fmt.Sprintf("ksrc_tool_%d", sourceSeq), now)
			if err != nil {
				return knowledgestore.Space{}, knowledgestore.Source{}, "", err
			}
			source.Summary = "Tool source about Knowledge corpus retrieval."
			source.KeyTerms = []string{"knowledge", "retrieval"}
			source, err = knowledgestore.NormalizeSource(source)
			if err != nil {
				return knowledgestore.Space{}, knowledgestore.Source{}, "", err
			}
			space, err = knowledgestore.AddSource(space, source, now)
			if err != nil {
				return knowledgestore.Space{}, knowledgestore.Source{}, "", err
			}
			if err := store.Save(space); err != nil {
				return knowledgestore.Space{}, knowledgestore.Source{}, "", err
			}
			return space, source, "Source analysed.", nil
		},
		Query: func(_ context.Context, spaceID string, req knowledgestore.QueryRequest) (knowledgestore.QueryResult, string, error) {
			space, err := store.Load(spaceID)
			if err != nil {
				return knowledgestore.QueryResult{}, "", err
			}
			result, err := knowledgestore.QuerySpace(space, req, now)
			if err != nil {
				return knowledgestore.QueryResult{}, "", err
			}
			return result, "Knowledge query completed.", nil
		},
		Ask: func(_ context.Context, spaceID string, req knowledgestore.AskRequest) (knowledgestore.Space, knowledgestore.AskResult, knowledgestore.Report, string, error) {
			space, err := store.Load(spaceID)
			if err != nil {
				return knowledgestore.Space{}, knowledgestore.AskResult{}, knowledgestore.Report{}, "", err
			}
			query, err := knowledgestore.QuerySpace(space, knowledgestore.QueryRequest{Query: req.Question, Limit: req.Limit, SourceIDs: req.SourceIDs}, now)
			if err != nil {
				return knowledgestore.Space{}, knowledgestore.AskResult{}, knowledgestore.Report{}, "", err
			}
			result := knowledgestore.AskResult{Question: req.Question, Answer: "Knowledge stores source-grounded evidence.", Evidence: query.Evidence, CreatedAt: now}
			report := knowledgestore.Report{ID: "kreport_tool", Question: req.Question, Mode: knowledgestore.ReportModeAsk, Answer: result.Answer, Evidence: query.Evidence, CreatedAt: now}
			space, err = knowledgestore.AddReport(space, report, now)
			if err != nil {
				return knowledgestore.Space{}, knowledgestore.AskResult{}, knowledgestore.Report{}, "", err
			}
			if err := store.Save(space); err != nil {
				return knowledgestore.Space{}, knowledgestore.AskResult{}, knowledgestore.Report{}, "", err
			}
			return space, result, report, "Knowledge answer stored.", nil
		},
		StartResearchRun: func(_ context.Context, spaceID string, req knowledgestore.CreateResearchRunRequest) (knowledgestore.Space, knowledgestore.ResearchRun, knowledgestore.Report, string, error) {
			runReq = req
			space, err := store.Load(spaceID)
			if err != nil {
				return knowledgestore.Space{}, knowledgestore.ResearchRun{}, knowledgestore.Report{}, "", err
			}
			run := knowledgestore.ResearchRun{
				ID:              "krun_tool",
				Objective:       req.Objective,
				Depth:           req.Depth,
				Status:          knowledgestore.ResearchRunStatusQueued,
				Mode:            req.Mode,
				DiscoverSources: req.DiscoverSources,
				WorkspacePath:   "runs/kspace_tool/krun_tool",
				CreatedAt:       now,
				UpdatedAt:       now,
			}
			space, err = knowledgestore.AddResearchRun(space, run, now)
			if err != nil {
				return knowledgestore.Space{}, knowledgestore.ResearchRun{}, knowledgestore.Report{}, "", err
			}
			if err := store.Save(space); err != nil {
				return knowledgestore.Space{}, knowledgestore.ResearchRun{}, knowledgestore.Report{}, "", err
			}
			return space, run, knowledgestore.Report{}, "Research run queued.", nil
		},
	}
	reg := tool.NewRegistry()
	if err := Register(reg, base); err != nil {
		t.Fatal(err)
	}

	created := runTool(t, reg, "knowledge.create", `{"title":"Chat research","objective":"Collect useful source-grounded answers"}`)
	var createdBody struct {
		Space spaceSummary `json:"space"`
	}
	if err := json.Unmarshal(created, &createdBody); err != nil {
		t.Fatal(err)
	}
	if createdBody.Space.ID != "kspace_tool" || createdBody.Space.Title != "Chat research" {
		t.Fatalf("created = %s", created)
	}

	added := runTool(t, reg, "knowledge.add_source", `{"space_id":"kspace_tool","title":"Knowledge note","kind":"note","content":"Knowledge corpus tools retrieve source-grounded evidence before answering questions."}`)
	var addedBody struct {
		Source sourceSummary `json:"source"`
	}
	if err := json.Unmarshal(added, &addedBody); err != nil {
		t.Fatal(err)
	}
	if addedBody.Source.ID == "" || addedBody.Source.WordCount == 0 || addedBody.Source.Summary == "" {
		t.Fatalf("source summary = %#v, want compact analysed source", addedBody.Source)
	}

	listed := runTool(t, reg, "knowledge.list", `{"query":"retrieval","include_sources":true}`)
	if strings.Contains(string(listed), `"content"`) || strings.Contains(string(listed), `"chunks"`) {
		t.Fatalf("knowledge.list returned heavy fields: %s", listed)
	}
	var listBody struct {
		Spaces []spaceSummary `json:"spaces"`
	}
	if err := json.Unmarshal(listed, &listBody); err != nil {
		t.Fatal(err)
	}
	if len(listBody.Spaces) != 1 || len(listBody.Spaces[0].Sources) != 1 {
		t.Fatalf("spaces = %#v, want matching space with compact source", listBody.Spaces)
	}

	queried := runTool(t, reg, "knowledge.query", `{"space_id":"kspace_tool","query":"source grounded evidence","limit":3}`)
	var queryBody struct {
		Result knowledgestore.QueryResult `json:"result"`
	}
	if err := json.Unmarshal(queried, &queryBody); err != nil {
		t.Fatal(err)
	}
	if len(queryBody.Result.Evidence) == 0 {
		t.Fatalf("query result = %#v, want evidence", queryBody.Result)
	}

	asked := runTool(t, reg, "knowledge.ask", `{"space_id":"kspace_tool","question":"What should the agent use before answering?","limit":3}`)
	var askBody struct {
		Result knowledgestore.AskResult `json:"result"`
		Report reportSummary            `json:"report"`
	}
	if err := json.Unmarshal(asked, &askBody); err != nil {
		t.Fatal(err)
	}
	if askBody.Report.ID == "" || askBody.Result.Answer == "" {
		t.Fatalf("ask result = %s", asked)
	}

	researched := runTool(t, reg, "knowledge.research", `{"space_id":"kspace_tool","objective":"Learn more about grounded chat research","depth":"standard"}`)
	var researchBody struct {
		Run           researchRunSummary `json:"run"`
		DashboardPath string             `json:"dashboard_path"`
	}
	if err := json.Unmarshal(researched, &researchBody); err != nil {
		t.Fatal(err)
	}
	if !runReq.DiscoverSources || !researchBody.Run.DiscoverSources {
		t.Fatalf("research request = %#v, output = %#v; discover_sources should default true", runReq, researchBody.Run)
	}
	if !strings.Contains(researchBody.DashboardPath, "#knowledge-research-krun_tool") {
		t.Fatalf("dashboard path = %q, want run anchor", researchBody.DashboardPath)
	}
}

func runTool(t *testing.T, reg *tool.Registry, name, input string) json.RawMessage {
	t.Helper()
	out, err := reg.Run(context.Background(), name, json.RawMessage(input))
	if err != nil {
		t.Fatalf("%s failed: %v", name, err)
	}
	return out
}
