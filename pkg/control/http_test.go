package control

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/andrewneudegg/lab/pkg/agent"
	"github.com/andrewneudegg/lab/pkg/config"
	"github.com/andrewneudegg/lab/pkg/eventlog"
	"github.com/andrewneudegg/lab/pkg/healthd"
	knowledgestore "github.com/andrewneudegg/lab/pkg/knowledge"
	"github.com/andrewneudegg/lab/pkg/llm"
	"github.com/andrewneudegg/lab/pkg/remoteagent"
	taskstore "github.com/andrewneudegg/lab/pkg/task"
	"github.com/andrewneudegg/lab/pkg/tool"
	approvalstore "github.com/andrewneudegg/lab/pkg/tools/approval"
)

func TestHomelabdDoesNotServeHealthd(t *testing.T) {
	server := Server{}
	mux := http.NewServeMux()
	server.register(mux)

	req := httptest.NewRequest(http.MethodGet, "/healthd", nil)
	rw := httptest.NewRecorder()
	mux.ServeHTTP(rw, req)

	if rw.Code != http.StatusNotFound {
		t.Fatalf("homelabd must not serve healthd endpoints, got status %d", rw.Code)
	}
}

func TestHealthzIsLightweight(t *testing.T) {
	server := Server{}
	mux := http.NewServeMux()
	server.register(mux)

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rw := httptest.NewRecorder()
	mux.ServeHTTP(rw, req)

	if rw.Code != http.StatusOK {
		t.Fatalf("healthz status = %d, want %d", rw.Code, http.StatusOK)
	}
	if rw.Body.String() != "{\"ok\":true}\n" {
		t.Fatalf("healthz body = %q", rw.Body.String())
	}
}

func TestMessageEndpointReturnsInteractionStats(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	cfg.DataDir = filepath.Join(dir, "data")
	cfg.Repo.Root = dir
	cfg.Repo.WorkspaceRoot = filepath.Join(dir, "workspaces")
	orch := agent.NewOrchestrator(
		cfg,
		eventlog.NewStore(filepath.Join(cfg.DataDir, "events")),
		taskstore.NewStore(filepath.Join(cfg.DataDir, "tasks")),
		approvalstore.NewStore(filepath.Join(cfg.DataDir, "approvals")),
		tool.NewRegistry(),
		tool.NewPolicy(nil),
		messageStatsProvider{},
		"test-model",
	)
	server := Server{Orchestrator: orch}
	mux := http.NewServeMux()
	server.register(mux)

	response := requestJSON(t, mux, http.MethodPost, "/message", `{"from":"dashboard","content":"what did that take?"}`, "", http.StatusOK)
	var got struct {
		Reply   string                 `json:"reply"`
		Source  string                 `json:"source"`
		Buttons []string               `json:"buttons"`
		Stats   agent.InteractionStats `json:"stats"`
	}
	if err := json.NewDecoder(response.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.Reply != "Measured reply." || got.Source != "test-provider" {
		t.Fatalf("response = %#v, want measured provider reply", got)
	}
	if strings.Join(got.Buttons, "|") != "Yes|No" {
		t.Fatalf("buttons = %#v, want yes/no choices", got.Buttons)
	}
	if got.Stats.ModelTurns != 1 || got.Stats.ToolCalls != 0 || got.Stats.TotalTokens != 17 || got.Stats.ElapsedMilliseconds <= 0 {
		t.Fatalf("stats = %#v, want one model turn, zero tool calls, token usage, and elapsed time", got.Stats)
	}
}

func TestChatClearEndpointRemovesConversationEventsAndHTTPTranscript(t *testing.T) {
	server, _, cfg := newHTTPTestServer(t)
	server.ChatLogDir = filepath.Join(cfg.DataDir, "chat")
	mux := http.NewServeMux()
	server.register(mux)

	requestJSON(t, mux, http.MethodPost, "/message", `{"from":"dashboard","content":"help","conversation_id":"chat_alpha"}`, "", http.StatusOK)
	requestJSON(t, mux, http.MethodPost, "/message", `{"from":"dashboard","content":"status","conversation_id":"chat_beta"}`, "", http.StatusOK)

	response := requestJSON(t, mux, http.MethodPost, "/chat/clear", `{"conversation_id":"chat_alpha"}`, "", http.StatusOK)
	var cleared struct {
		RemovedEvents     int `json:"removed_events"`
		RemovedLogEntries int `json:"removed_log_entries"`
	}
	if err := json.NewDecoder(response.Body).Decode(&cleared); err != nil {
		t.Fatal(err)
	}
	if cleared.RemovedEvents != 2 || cleared.RemovedLogEntries != 2 {
		t.Fatalf("clear response = %#v, want two event and two transcript removals", cleared)
	}

	events := requestJSON(t, mux, http.MethodGet, "/events", "", "", http.StatusOK)
	if strings.Contains(events.Body.String(), "chat_alpha") || strings.Contains(events.Body.String(), "help") {
		t.Fatalf("events still contain cleared conversation: %s", events.Body.String())
	}
	if !strings.Contains(events.Body.String(), "chat_beta") {
		t.Fatalf("events did not keep other conversation: %s", events.Body.String())
	}

	logBytes, err := os.ReadFile(filepath.Join(server.ChatLogDir, time.Now().UTC().Format("2006-01-02")+".jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	logText := string(logBytes)
	if strings.Contains(logText, "chat_alpha") || strings.Contains(logText, "help") {
		t.Fatalf("chat log still contains cleared conversation: %s", logText)
	}
	if !strings.Contains(logText, "chat_beta") {
		t.Fatalf("chat log did not keep other conversation: %s", logText)
	}
}

func TestSettingsEndpointPersistsAutoMerge(t *testing.T) {
	server, _, _ := newHTTPTestServer(t)
	mux := http.NewServeMux()
	server.register(mux)

	initial := requestJSON(t, mux, http.MethodGet, "/settings", "", "", http.StatusOK)
	if !strings.Contains(initial.Body.String(), `"auto_merge_enabled":false`) {
		t.Fatalf("initial settings = %s", initial.Body.String())
	}

	updated := requestJSON(t, mux, http.MethodPost, "/settings", `{"auto_merge_enabled":true}`, "", http.StatusOK)
	if !strings.Contains(updated.Body.String(), `"auto_merge_enabled":true`) {
		t.Fatalf("updated settings = %s", updated.Body.String())
	}

	reloaded := requestJSON(t, mux, http.MethodGet, "/settings", "", "", http.StatusOK)
	if !strings.Contains(reloaded.Body.String(), `"auto_merge_enabled":true`) {
		t.Fatalf("reloaded settings = %s", reloaded.Body.String())
	}
}

func TestAssistantEndpointReturnsFilteredCatalogue(t *testing.T) {
	server := Server{}
	mux := http.NewServeMux()
	server.register(mux)

	req := httptest.NewRequest(http.MethodGet, "/assistant?area=research&q=sources", nil)
	rw := httptest.NewRecorder()
	mux.ServeHTTP(rw, req)

	if rw.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rw.Code, rw.Body.String())
	}
	var got struct {
		Name       string `json:"name"`
		Activities []struct {
			ID string `json:"id"`
		} `json:"activities"`
		Capabilities []struct {
			ID               string `json:"id"`
			Name             string `json:"name"`
			WorkflowTemplate struct {
				Steps []struct {
					Name string `json:"name"`
					Kind string `json:"kind"`
				} `json:"steps"`
			} `json:"workflow_template"`
		} `json:"capabilities"`
		UXPatterns []struct {
			ID string `json:"id"`
		} `json:"ux_patterns"`
	}
	if err := json.NewDecoder(rw.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.Name != "Assistant" {
		t.Fatalf("name = %q, want Assistant", got.Name)
	}
	if len(got.Capabilities) != 1 || got.Capabilities[0].ID != "research-prepare" {
		t.Fatalf("capabilities = %#v, want research-prepare", got.Capabilities)
	}
	if len(got.Capabilities[0].WorkflowTemplate.Steps) == 0 {
		t.Fatalf("workflow template missing steps: %#v", got.Capabilities[0].WorkflowTemplate)
	}
	if len(got.Activities) != 1 || got.Activities[0].ID != "prepare-decision" {
		t.Fatalf("activities = %#v, want prepare-decision", got.Activities)
	}
	if len(got.UXPatterns) == 0 {
		t.Fatal("expected ux patterns in assistant catalogue")
	}
}

func TestKnowledgeSpaceEndpointsProcessSourcesAndReports(t *testing.T) {
	server := newKnowledgeHTTPTestServer(t, &scriptedControlProvider{contents: []string{
		`{
			"summary":"Evidence should stay beside generated answers for review.",
			"key_terms":["evidence","answers","review"],
			"questions":["How do reviewers verify answers?"],
			"claims":[{"id":"c1","text":"Research reports need source labels.","importance":"high"}],
			"entities":[{"name":"Research reports","type":"artefact","description":"Source-grounded outputs"}],
			"reliability_notes":["Operator-provided text source."]
		}`,
		`{
			"answer":"Reviewers verify answers by checking the visible evidence labels [S1].",
			"key_findings":["[S1] Source labels keep claims verifiable."],
			"gaps":["Only stored corpus evidence was used."]
		}`,
		`{
			"answer":"Reviewers use cited evidence to verify generated claims [S1].",
			"key_findings":["[S1] Evidence stays beside answers."],
			"gaps":["No connected external source was queried."]
		}`,
		`{
			"rewritten_objective":"Review evidence handling",
			"clarifying_questions":["Which report audience matters most?"],
			"search_queries":["evidence labels reviewers verify claims"],
			"steps":["Retrieve stored evidence","Synthesize cited report"],
			"expected_outputs":["Markdown report"]
		}`,
		`{
			"answer":"## Evidence handling\nThe run found that research reports need source labels so reviewers can verify claims [S1].",
			"key_findings":["[S1] Source labels support claim verification."],
			"gaps":["No web or connector sources were added."]
		}`,
	}})
	mux := http.NewServeMux()
	server.register(mux)

	created := requestJSON(t, mux, http.MethodPost, "/knowledge/spaces", `{"title":"Research space","objective":"Understand grounded answers"}`, "", http.StatusCreated)
	var createBody struct {
		Space knowledgestore.Space `json:"space"`
		Reply string               `json:"reply"`
	}
	if err := json.NewDecoder(created.Body).Decode(&createBody); err != nil {
		t.Fatal(err)
	}
	if createBody.Space.ID == "" || createBody.Space.Title != "Research space" {
		t.Fatalf("create body = %#v, want created space", createBody)
	}

	sourcePath := "/knowledge/spaces/" + createBody.Space.ID + "/sources"
	added := requestJSON(t, mux, http.MethodPost, sourcePath, `{"title":"Evidence note","kind":"text","content":"Evidence should stay beside generated answers. Research reports need source labels so reviewers can verify claims."}`, "", http.StatusCreated)
	var sourceBody struct {
		Space  knowledgestore.Space  `json:"space"`
		Source knowledgestore.Source `json:"source"`
	}
	if err := json.NewDecoder(added.Body).Decode(&sourceBody); err != nil {
		t.Fatal(err)
	}
	if sourceBody.Space.Insight.SourceCount != 1 || sourceBody.Source.Summary == "" {
		t.Fatalf("source body = %#v, want processed source and updated insight", sourceBody)
	}

	reportPath := "/knowledge/spaces/" + createBody.Space.ID + "/research"
	reported := requestJSON(t, mux, http.MethodPost, reportPath, `{"question":"How should reviewers verify answers?","mode":"research"}`, "", http.StatusOK)
	var reportBody struct {
		Space  knowledgestore.Space  `json:"space"`
		Report knowledgestore.Report `json:"report"`
	}
	if err := json.NewDecoder(reported.Body).Decode(&reportBody); err != nil {
		t.Fatal(err)
	}
	if len(reportBody.Report.Evidence) == 0 || len(reportBody.Space.Reports) != 1 {
		t.Fatalf("report body = %#v, want stored report with evidence", reportBody)
	}

	queryPath := "/knowledge/spaces/" + createBody.Space.ID + "/query"
	queried := requestJSON(t, mux, http.MethodPost, queryPath, `{"query":"evidence labels","limit":3}`, "", http.StatusOK)
	var queryBody struct {
		Result knowledgestore.QueryResult `json:"result"`
	}
	if err := json.NewDecoder(queried.Body).Decode(&queryBody); err != nil {
		t.Fatal(err)
	}
	if len(queryBody.Result.Evidence) == 0 {
		t.Fatalf("query body = %#v, want retrieved evidence", queryBody)
	}

	askPath := "/knowledge/spaces/" + createBody.Space.ID + "/ask"
	asked := requestJSON(t, mux, http.MethodPost, askPath, `{"question":"How do reviewers use evidence?"}`, "", http.StatusOK)
	var askBody struct {
		Result knowledgestore.AskResult `json:"result"`
	}
	if err := json.NewDecoder(asked.Body).Decode(&askBody); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(askBody.Result.Answer, "[S1]") {
		t.Fatalf("ask body = %#v, want cited answer", askBody)
	}

	runPath := "/knowledge/spaces/" + createBody.Space.ID + "/research-runs"
	ran := requestJSON(t, mux, http.MethodPost, runPath, `{"objective":"Review evidence handling","depth":"standard"}`, "", http.StatusCreated)
	var runBody struct {
		Space  knowledgestore.Space       `json:"space"`
		Run    knowledgestore.ResearchRun `json:"run"`
		Report *knowledgestore.Report     `json:"report"`
	}
	if err := json.NewDecoder(ran.Body).Decode(&runBody); err != nil {
		t.Fatal(err)
	}
	if runBody.Run.Status != knowledgestore.ResearchRunStatusQueued || len(runBody.Space.ResearchRuns) != 1 || runBody.Report != nil {
		t.Fatalf("run body = %#v, want queued async run without immediate report", runBody)
	}
	completedSpace, completedRun := waitForKnowledgeRun(t, mux, createBody.Space.ID, runBody.Run.ID)
	if completedRun.Status != knowledgestore.ResearchRunStatusCompleted || completedRun.ReportID == "" || len(completedSpace.Reports) != 2 {
		t.Fatalf("run = %#v space = %#v, want completed run and stored report", completedRun, completedSpace)
	}

	listed := requestJSON(t, mux, http.MethodGet, "/knowledge/spaces", "", "", http.StatusOK)
	if !strings.Contains(listed.Body.String(), createBody.Space.ID) {
		t.Fatalf("list body = %s, want created space", listed.Body.String())
	}
}

func TestKnowledgeResearchRunDiscoversOnlineCheeseSourcesOverAPI(t *testing.T) {
	research := &controlInternetResearchStub{sourceBatches: [][]map[string]any{
		{
			{
				"query":        "online cheese types and properties",
				"kind":         "web",
				"provider":     "searxng",
				"title":        "Cheddar cheese profile",
				"url":          "https://example.com/cheddar",
				"domain":       "example.com",
				"snippet":      "Cheddar is a hard cheese with ageing, texture, and melting properties.",
				"fetched":      true,
				"content_type": "text/html",
				"page_title":   "Cheddar cheese profile",
				"text":         "Cheddar is a hard aged cheese with firm texture, sharp flavour, and reliable melting properties.",
			},
			{
				"query":        "online cheese types and properties",
				"kind":         "web",
				"provider":     "searxng",
				"title":        "Brie cheese profile",
				"url":          "https://example.com/brie",
				"domain":       "example.com",
				"snippet":      "Brie is a soft-ripened cheese with a bloomy rind and creamy interior.",
				"fetched":      true,
				"content_type": "text/html",
				"page_title":   "Brie cheese profile",
				"text":         "Brie is a soft-ripened cheese with a bloomy rind, creamy interior, mild aroma, and high moisture.",
			},
			{
				"query":        "online cheese types and properties",
				"kind":         "web",
				"provider":     "searxng",
				"title":        "Conference calendar",
				"url":          "https://example.com/conference-calendar",
				"domain":       "example.com",
				"snippet":      "Events, sponsorships, and venue logistics.",
				"fetched":      true,
				"content_type": "text/html",
				"page_title":   "Conference calendar",
				"text":         "The annual conference calendar lists event dates, sponsor packages, venue logistics, and registration deadlines.",
			},
		},
		{
			{
				"query":        "fresh blue washed rind cheese taxonomy",
				"kind":         "web",
				"provider":     "searxng",
				"title":        "Mozzarella cheese profile",
				"url":          "https://example.com/mozzarella",
				"domain":       "example.com",
				"snippet":      "Mozzarella is a fresh high-moisture cheese.",
				"fetched":      true,
				"content_type": "text/html",
				"page_title":   "Mozzarella cheese profile",
				"text":         "Mozzarella is a fresh high-moisture pasta filata cheese with mild flavour and elastic texture.",
			},
			{
				"query":        "fresh blue washed rind cheese taxonomy",
				"kind":         "web",
				"provider":     "searxng",
				"title":        "Blue cheese profile",
				"url":          "https://example.com/blue-cheese",
				"domain":       "example.com",
				"snippet":      "Blue cheese is ripened with blue mould veining.",
				"fetched":      true,
				"content_type": "text/html",
				"page_title":   "Blue cheese profile",
				"text":         "Blue cheese is a mould-ripened family with blue veining, salty flavour, and crumbly or creamy textures.",
			},
		},
	}}
	server := newKnowledgeHTTPTestServerWithTools(t, &scriptedControlProvider{contents: []string{
		`{
			"rewritten_objective":"online cheese types and properties",
			"clarifying_questions":[],
			"search_queries":["cheddar brie cheese properties"],
			"steps":["Search online","Import fetched sources","Synthesize cited comparison"],
			"expected_outputs":["Cited cheese property report"]
		}`,
		`{
			"summary":"Cheddar is a hard aged cheese with sharp flavour and melting properties.",
			"key_terms":["cheddar","hard cheese","melting"],
			"questions":["What properties does cheddar have?"],
			"claims":[{"id":"claim_cheddar","text":"Cheddar is hard, aged, and melts reliably.","importance":"high"}],
			"entities":[{"name":"Cheddar","type":"cheese","description":"Hard aged cheese"}],
			"reliability_notes":["Fetched online source."]
		}`,
		`{
			"decision":"accept",
			"relevance_score":86,
			"reason":"Cheddar is one cheese type and helps answer the taxonomy question.",
			"coverage":["Cited cheese property report","cheddar brie cheese properties"],
			"follow_up_queries":[]
		}`,
		`{
			"summary":"Brie is a soft-ripened cheese with a bloomy rind and creamy interior.",
			"key_terms":["brie","soft-ripened","bloomy rind"],
			"questions":["What properties does brie have?"],
			"claims":[{"id":"claim_brie","text":"Brie has a bloomy rind and creamy interior.","importance":"high"}],
			"entities":[{"name":"Brie","type":"cheese","description":"Soft-ripened cheese"}],
			"reliability_notes":["Fetched online source."]
		}`,
		`{
			"decision":"accept",
			"relevance_score":88,
			"reason":"Brie is a soft-ripened cheese type and complements the cheddar source.",
			"coverage":["Cited cheese property report","cheddar brie cheese properties"],
			"follow_up_queries":[]
		}`,
		`{
			"summary":"The source is an event calendar covering dates, sponsors, venues, and registration.",
			"key_terms":["conference","events","sponsors"],
			"questions":["What event logistics are listed?"],
			"claims":[{"id":"claim_event","text":"The page lists conference logistics.","importance":"low"}],
			"entities":[{"name":"Conference calendar","type":"event listing","description":"Events page"}],
			"reliability_notes":["Fetched online source."]
		}`,
		`{
			"decision":"reject",
			"relevance_score":5,
			"reason":"The source does not cover cheese types, properties, or taxonomy.",
			"coverage":[],
			"follow_up_queries":["fresh blue washed rind cheese taxonomy"]
		}`,
		`{
			"decision":"continue",
			"stop_reason":"Cheddar and brie are covered, but fresh and blue families are still missing.",
			"supported_claims":["Cheddar is a hard aged cheese.","Brie is a soft-ripened cheese."],
			"gaps":["Fresh cheeses are missing.","Blue mould-ripened cheeses are missing."],
			"follow_up_queries":["fresh blue washed rind cheese taxonomy"],
			"coverage":["hard aged cheese","soft-ripened cheese"]
		}`,
		`{
			"summary":"Mozzarella is a fresh high-moisture pasta filata cheese.",
			"key_terms":["mozzarella","fresh cheese","pasta filata"],
			"questions":["What properties does mozzarella have?"],
			"claims":[{"id":"claim_mozzarella","text":"Mozzarella is fresh, high-moisture, and elastic.","importance":"high"}],
			"entities":[{"name":"Mozzarella","type":"cheese","description":"Fresh pasta filata cheese"}],
			"reliability_notes":["Fetched online source."]
		}`,
		`{
			"decision":"accept",
			"relevance_score":84,
			"reason":"Mozzarella covers the fresh cheese gap.",
			"coverage":["fresh cheeses","Cited cheese property report"],
			"follow_up_queries":[]
		}`,
		`{
			"summary":"Blue cheese is a mould-ripened family with blue veining and salty flavour.",
			"key_terms":["blue cheese","mould-ripened","veining"],
			"questions":["What properties does blue cheese have?"],
			"claims":[{"id":"claim_blue","text":"Blue cheese is mould-ripened with blue veining.","importance":"high"}],
			"entities":[{"name":"Blue cheese","type":"cheese family","description":"Mould-ripened cheese family"}],
			"reliability_notes":["Fetched online source."]
		}`,
		`{
			"decision":"accept",
			"relevance_score":82,
			"reason":"Blue cheese covers the mould-ripened taxonomy gap.",
			"coverage":["blue mould-ripened cheeses","Cited cheese property report"],
			"follow_up_queries":[]
		}`,
		`{
			"decision":"complete",
			"stop_reason":"Accepted sources now cover hard aged, soft-ripened, fresh, and blue mould-ripened cheese families.",
			"supported_claims":["Cheddar is hard aged.","Brie is soft-ripened.","Mozzarella is fresh.","Blue cheese is mould-ripened."],
			"gaps":[],
			"follow_up_queries":[],
			"coverage":["hard aged cheese","soft-ripened cheese","fresh cheeses","blue mould-ripened cheeses"]
		}`,
		`{
			"answer":"The run found a starter cheese taxonomy: cheddar represents hard aged cheeses [S1], brie represents soft-ripened bloomy-rind cheeses [S2], mozzarella represents fresh high-moisture cheeses [S3], and blue cheese represents mould-ripened veined cheeses [S4].",
			"key_findings":["[S1] Cheddar has hard aged melting properties.","[S2] Brie has a creamy soft-ripened profile.","[S3] Mozzarella is fresh and high-moisture.","[S4] Blue cheese is mould-ripened with blue veining."],
			"gaps":[]
		}`,
	}}, research)
	mux := http.NewServeMux()
	server.register(mux)

	created := requestJSON(t, mux, http.MethodPost, "/knowledge/spaces", `{"title":"Cheese research","objective":"Build a cheese type corpus"}`, "", http.StatusCreated)
	var createBody struct {
		Space knowledgestore.Space `json:"space"`
	}
	if err := json.NewDecoder(created.Body).Decode(&createBody); err != nil {
		t.Fatal(err)
	}

	runPath := "/knowledge/spaces/" + createBody.Space.ID + "/research-runs"
	ran := requestJSON(t, mux, http.MethodPost, runPath, `{"objective":"Search online for types of cheese and their properties","depth":"quick","discover_sources":true}`, "", http.StatusCreated)
	var runBody struct {
		Space knowledgestore.Space       `json:"space"`
		Run   knowledgestore.ResearchRun `json:"run"`
	}
	if err := json.NewDecoder(ran.Body).Decode(&runBody); err != nil {
		t.Fatal(err)
	}
	if runBody.Run.Status != knowledgestore.ResearchRunStatusQueued || !runBody.Run.DiscoverSources {
		t.Fatalf("initial run = %#v, want queued discovery run without a source cap", runBody.Run)
	}

	completedSpace, completedRun := waitForKnowledgeRun(t, mux, createBody.Space.ID, runBody.Run.ID)
	if completedRun.Status != knowledgestore.ResearchRunStatusCompleted {
		t.Fatalf("run = %#v, want completed", completedRun)
	}
	if len(completedSpace.Sources) != 4 {
		t.Fatalf("sources = %#v, want four imported online cheese sources after follow-up discovery", completedSpace.Sources)
	}
	if len(completedRun.Candidates) != 5 || completedRun.Candidates[0].Status != "accepted" || completedRun.Candidates[1].Status != "accepted" || completedRun.Candidates[2].Status != "rejected" || completedRun.Candidates[3].Status != "accepted" || completedRun.Candidates[4].Status != "accepted" {
		t.Fatalf("candidates = %#v, want iterative accepted cheese candidates and rejected unrelated candidate", completedRun.Candidates)
	}
	if len(completedRun.ResearchLoops) != 2 || completedRun.ResearchLoops[0].Decision != "continue" || completedRun.ResearchLoops[1].Decision != "complete" || completedRun.StopReason == "" {
		t.Fatalf("research loops = %#v stop=%q, want continue then complete loop decisions", completedRun.ResearchLoops, completedRun.StopReason)
	}
	if completedRun.ReportID == "" || completedRun.EvidenceCount == 0 || completedRun.SourcesExamined != 4 || completedRun.WorkspacePath == "" {
		t.Fatalf("completed run = %#v, want report, evidence, imported source count, and workspace", completedRun)
	}
	if !containsResearchEvent(completedRun.Events, "coverage", "Coverage sufficient") {
		t.Fatalf("events = %#v, want coverage stop event", completedRun.Events)
	}
	for _, source := range completedSpace.Sources {
		if source.Ingestion.State != knowledgestore.SourceStatusReady || len(source.Claims) == 0 {
			t.Fatalf("source = %#v, want model-analysed ready source with claims", source)
		}
		if !strings.Contains(source.Provenance.Extractor, "internet.research") || !strings.Contains(source.Provenance.Extractor, "language-model") {
			t.Fatalf("source provenance = %#v, want internet research and language model provenance", source.Provenance)
		}
	}
	calls := research.Calls()
	if len(calls) != 2 {
		t.Fatalf("internet research calls = %#v, want initial and follow-up calls", calls)
	}
	call := calls[0]
	if call.Provider != "searxng" || !call.Fetch || call.MaxSearches != 2 || call.Depth != "quick" || call.Source != "web" {
		t.Fatalf("internet research call = %#v, want explicit fetched SearXNG cheese discovery", call)
	}
	if len(call.Queries) != 1 || call.Queries[0] != "cheddar brie cheese properties" {
		t.Fatalf("internet research queries = %#v, want model-planned cheese query", call.Queries)
	}
	if len(calls[1].Queries) != 1 || calls[1].Queries[0] != "fresh blue washed rind cheese taxonomy" {
		t.Fatalf("follow-up research queries = %#v, want coverage-driven follow-up query", calls[1].Queries)
	}
}

func TestKnowledgeResearchRunDiscoveryFailureIsVisibleOverAPI(t *testing.T) {
	research := &controlInternetResearchStub{sources: []map[string]any{
		{
			"query":       "cheese taxonomy",
			"kind":        "web",
			"provider":    "searxng",
			"title":       "Unavailable cheese source",
			"url":         "https://example.com/unavailable-cheese",
			"domain":      "example.com",
			"snippet":     "This result could not be fetched.",
			"fetch_error": "fetch failed: 503 service unavailable",
		},
	}}
	server := newKnowledgeHTTPTestServerWithTools(t, &scriptedControlProvider{contents: []string{
		`{
			"rewritten_objective":"cheese taxonomy",
			"clarifying_questions":[],
			"search_queries":["cheese taxonomy"],
			"steps":["Search online","Import fetched sources","Report failure if no source text is usable"],
			"expected_outputs":["Visible failure"]
		}`,
	}}, research)
	mux := http.NewServeMux()
	server.register(mux)

	created := requestJSON(t, mux, http.MethodPost, "/knowledge/spaces", `{"title":"Cheese taxonomy"}`, "", http.StatusCreated)
	var createBody struct {
		Space knowledgestore.Space `json:"space"`
	}
	if err := json.NewDecoder(created.Body).Decode(&createBody); err != nil {
		t.Fatal(err)
	}
	ran := requestJSON(t, mux, http.MethodPost, "/knowledge/spaces/"+createBody.Space.ID+"/research-runs", `{"objective":"Search online for cheese taxonomy","discover_sources":true}`, "", http.StatusCreated)
	var runBody struct {
		Run knowledgestore.ResearchRun `json:"run"`
	}
	if err := json.NewDecoder(ran.Body).Decode(&runBody); err != nil {
		t.Fatal(err)
	}

	failedSpace, failedRun := waitForKnowledgeRun(t, mux, createBody.Space.ID, runBody.Run.ID)
	if failedRun.Status != knowledgestore.ResearchRunStatusFailed {
		t.Fatalf("run = %#v, want failed discovery run", failedRun)
	}
	if !strings.Contains(failedRun.Error, "online discovery did not import any usable sources") {
		t.Fatalf("error = %q, want visible no usable sources failure", failedRun.Error)
	}
	if len(failedRun.Candidates) != 1 || failedRun.Candidates[0].Status != "failed" || !strings.Contains(failedRun.Candidates[0].Error, "503") {
		t.Fatalf("candidates = %#v, want failed candidate with fetch error", failedRun.Candidates)
	}
	if len(failedSpace.Sources) != 0 || len(failedSpace.Reports) != 0 || failedRun.ReportID != "" {
		t.Fatalf("space = %#v run = %#v, want no imported sources or report on discovery failure", failedSpace, failedRun)
	}
	if !containsResearchEvent(failedRun.Events, "failed", "online discovery did not import any usable sources") {
		t.Fatalf("events = %#v, want failure event", failedRun.Events)
	}
}

func TestKnowledgeResearchRunStoredOnlyDoesNotCallOnlineDiscovery(t *testing.T) {
	research := &controlInternetResearchStub{}
	server := newKnowledgeHTTPTestServerWithTools(t, &scriptedControlProvider{contents: []string{
		`{
			"summary":"Stored cheese note says mozzarella is a fresh high-moisture cheese.",
			"key_terms":["mozzarella","fresh cheese"],
			"questions":["What kind of cheese is mozzarella?"],
			"claims":[{"id":"claim_mozzarella","text":"Mozzarella is fresh and high-moisture.","importance":"medium"}],
			"entities":[{"name":"Mozzarella","type":"cheese","description":"Fresh cheese"}],
			"reliability_notes":["Operator-provided text source."]
		}`,
		`{
			"rewritten_objective":"Summarise stored cheese source",
			"clarifying_questions":[],
			"search_queries":["mozzarella fresh cheese"],
			"steps":["Retrieve stored evidence","Synthesize answer"],
			"expected_outputs":["Stored-source report"]
		}`,
		`{
			"answer":"The stored source says mozzarella is a fresh high-moisture cheese [S1].",
			"key_findings":["[S1] Mozzarella is fresh and high-moisture."],
			"gaps":["No online discovery was requested."]
		}`,
	}}, research)
	mux := http.NewServeMux()
	server.register(mux)

	created := requestJSON(t, mux, http.MethodPost, "/knowledge/spaces", `{"title":"Stored cheese notes"}`, "", http.StatusCreated)
	var createBody struct {
		Space knowledgestore.Space `json:"space"`
	}
	if err := json.NewDecoder(created.Body).Decode(&createBody); err != nil {
		t.Fatal(err)
	}
	added := requestJSON(t, mux, http.MethodPost, "/knowledge/spaces/"+createBody.Space.ID+"/sources", `{"title":"Mozzarella note","kind":"note","content":"Mozzarella is a fresh high-moisture cheese used for quick comparisons."}`, "", http.StatusCreated)
	var sourceBody struct {
		Source knowledgestore.Source `json:"source"`
	}
	if err := json.NewDecoder(added.Body).Decode(&sourceBody); err != nil {
		t.Fatal(err)
	}
	ran := requestJSON(t, mux, http.MethodPost, "/knowledge/spaces/"+createBody.Space.ID+"/research-runs", `{"objective":"Summarise stored cheese source","source_ids":["`+sourceBody.Source.ID+`"],"discover_sources":false}`, "", http.StatusCreated)
	var runBody struct {
		Run knowledgestore.ResearchRun `json:"run"`
	}
	if err := json.NewDecoder(ran.Body).Decode(&runBody); err != nil {
		t.Fatal(err)
	}

	space, run := waitForKnowledgeRun(t, mux, createBody.Space.ID, runBody.Run.ID)
	if run.Status != knowledgestore.ResearchRunStatusCompleted || run.DiscoverSources || len(run.Candidates) != 0 {
		t.Fatalf("run = %#v, want completed stored-only run with no candidates", run)
	}
	if len(space.Sources) != 1 || run.SourcesExamined != 1 || run.ReportID == "" {
		t.Fatalf("space = %#v run = %#v, want stored source report", space, run)
	}
	if len(research.Calls()) != 0 {
		t.Fatalf("internet research calls = %#v, want none for stored-only run", research.Calls())
	}
}

func TestKnowledgeSpaceListEndpointReturnsEmptyArray(t *testing.T) {
	server, _, _ := newHTTPTestServer(t)
	mux := http.NewServeMux()
	server.register(mux)

	listed := requestJSON(t, mux, http.MethodGet, "/knowledge/spaces", "", "", http.StatusOK)
	if listed.Body.String() != "{\"spaces\":[]}\n" {
		t.Fatalf("list body = %s, want empty spaces array", listed.Body.String())
	}
}

func TestTaskRunsEndpointListsExternalArtifacts(t *testing.T) {
	server, _, cfg := newHTTPTestServer(t)
	if err := os.MkdirAll(filepath.Join(cfg.DataDir, "runs"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeJSONFile(t, filepath.Join(cfg.DataDir, "runs", "delegate_one.json"), map[string]any{
		"id":         "delegate_one",
		"kind":       "external_agent",
		"task_id":    "task_one",
		"backend":    "codex",
		"workspace":  "/tmp/work",
		"status":     "completed",
		"output":     "done",
		"time":       time.Now().UTC(),
		"started_at": time.Now().UTC(),
	})
	writeJSONFile(t, filepath.Join(cfg.DataDir, "runs", "builtin.json"), map[string]any{
		"id":      "builtin",
		"task_id": "task_one",
		"status":  "completed",
	})

	mux := http.NewServeMux()
	server.register(mux)
	req := httptest.NewRequest(http.MethodGet, "/tasks/task_one/runs", nil)
	rw := httptest.NewRecorder()
	mux.ServeHTTP(rw, req)

	if rw.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rw.Code, rw.Body.String())
	}
	var got struct {
		Runs []agent.ExternalRunArtifact `json:"runs"`
	}
	if err := json.NewDecoder(rw.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if len(got.Runs) != 1 || got.Runs[0].ID != "delegate_one" {
		t.Fatalf("runs = %#v, want delegate_one only", got.Runs)
	}
	if got.Runs[0].Path == "" {
		t.Fatalf("run path was not returned: %#v", got.Runs[0])
	}
}

func TestApprovalEditEndpointUpdatesPendingArgs(t *testing.T) {
	server, _, cfg := newHTTPTestServer(t)
	approvals := approvalstore.NewStore(filepath.Join(cfg.DataDir, "approvals"))
	req := approvalstore.Request{
		ID:     "approval_http_edit",
		Tool:   "task.create",
		Args:   json.RawMessage(`{"goal":"old"}`),
		Reason: "test approval",
		Status: approvalstore.StatusPending,
	}
	if err := approvals.Save(req); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	server.register(mux)

	requestJSON(t, mux, http.MethodPost, "/approvals/approval_http_edit/edit", `{"args":{"target":"missing goal"}}`, "", http.StatusBadRequest)
	requestJSON(t, mux, http.MethodPost, "/approvals/approval_http_edit/edit", `{"args":{"goal":"new"}}`, "", http.StatusOK)

	updated, err := approvals.Load(req.ID)
	if err != nil {
		t.Fatal(err)
	}
	var editedArgs map[string]string
	if err := json.Unmarshal(updated.Args, &editedArgs); err != nil {
		t.Fatal(err)
	}
	if editedArgs["goal"] != "new" {
		t.Fatalf("args = %s, want edited args", updated.Args)
	}
	if !strings.Contains(updated.Reason, "args edited by human") {
		t.Fatalf("reason = %q, want edit audit suffix", updated.Reason)
	}
}

func TestTaskDiffEndpointReturnsStructuredBranchDiff(t *testing.T) {
	dir := t.TempDir()
	repo := filepath.Join(dir, "repo")
	workspaceRoot := filepath.Join(dir, "workspaces")
	workspace := filepath.Join(workspaceRoot, "task_20260426_204322_c01777ee")
	gitHTTPTestRun(t, "", "init", "--initial-branch=main", repo)
	gitHTTPTestRun(t, repo, "config", "user.email", "test@example.com")
	gitHTTPTestRun(t, repo, "config", "user.name", "Test User")
	if err := os.WriteFile(filepath.Join(repo, "app.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitHTTPTestRun(t, repo, "add", "app.txt")
	gitHTTPTestRun(t, repo, "commit", "-m", "base")
	if err := os.MkdirAll(workspaceRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	gitHTTPTestRun(t, repo, "worktree", "add", "-b", "homelabd/task_20260426_204322_c01777ee", workspace)
	if err := os.WriteFile(filepath.Join(workspace, "app.txt"), []byte("base\nchanged\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitHTTPTestRun(t, workspace, "add", "app.txt")
	gitHTTPTestRun(t, workspace, "commit", "-m", "change app")

	cfg := config.Default()
	cfg.DataDir = filepath.Join(dir, "data")
	cfg.Repo.Root = repo
	cfg.Repo.WorkspaceRoot = workspaceRoot
	tasks := taskstore.NewStore(filepath.Join(cfg.DataDir, "tasks"))
	orch := agent.NewOrchestrator(
		cfg,
		eventlog.NewStore(filepath.Join(cfg.DataDir, "events")),
		tasks,
		approvalstore.NewStore(filepath.Join(cfg.DataDir, "approvals")),
		tool.NewRegistry(),
		tool.NewPolicy(nil),
		nil,
		"",
	)
	taskID := "task_20260426_204322_c01777ee"
	if err := tasks.Save(taskstore.Task{
		ID:         taskID,
		Title:      "change app",
		Goal:       "change app",
		Status:     taskstore.StatusConflictResolution,
		AssignedTo: "codex",
		Workspace:  workspace,
		CreatedAt:  time.Now().UTC(),
		UpdatedAt:  time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}

	server := Server{Orchestrator: orch}
	mux := http.NewServeMux()
	server.register(mux)
	req := httptest.NewRequest(http.MethodGet, "/tasks/c01777ee/diff", nil)
	rw := httptest.NewRecorder()
	mux.ServeHTTP(rw, req)

	if rw.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rw.Code, rw.Body.String())
	}
	var got agent.TaskDiff
	if err := json.NewDecoder(rw.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.TaskID != taskID {
		t.Fatalf("task id = %q, want %q", got.TaskID, taskID)
	}
	if got.Summary.Files != 1 || got.Summary.Additions != 1 || got.Summary.Deletions != 0 {
		t.Fatalf("summary = %#v, want one added line in one file", got.Summary)
	}
	if len(got.Files) != 1 || got.Files[0].Path != "app.txt" || got.Files[0].Status != "modified" {
		t.Fatalf("files = %#v, want modified app.txt", got.Files)
	}
	if !strings.Contains(got.RawDiff, "+changed") || got.BaseLabel != "main" {
		t.Fatalf("diff = %#v, base label = %q", got.RawDiff, got.BaseLabel)
	}
}

func TestTaskCancelEndpointCancelsTask(t *testing.T) {
	server, tasks, _ := newHTTPTestServer(t)
	now := time.Now().UTC()
	taskID := "task_cancel_endpoint"
	if err := tasks.Save(taskstore.Task{
		ID:         taskID,
		Title:      "cancel me",
		Goal:       "cancel me",
		Status:     taskstore.StatusRunning,
		AssignedTo: "codex",
		Workspace:  "/tmp/work",
		CreatedAt:  now,
		UpdatedAt:  now,
	}); err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	server.register(mux)
	req := httptest.NewRequest(http.MethodPost, "/tasks/"+taskID+"/cancel", nil)
	rw := httptest.NewRecorder()
	mux.ServeHTTP(rw, req)

	if rw.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rw.Code, rw.Body.String())
	}
	task, err := tasks.Load(taskID)
	if err != nil {
		t.Fatal(err)
	}
	if task.Status != taskstore.StatusCancelled {
		t.Fatalf("status = %q, want cancelled", task.Status)
	}
}

func TestTaskDeleteEndpointDeletesTask(t *testing.T) {
	server, tasks, _ := newHTTPTestServer(t)
	now := time.Now().UTC()
	taskID := "task_delete_endpoint"
	if err := tasks.Save(taskstore.Task{
		ID:         taskID,
		Title:      "delete me",
		Goal:       "delete me",
		Status:     taskstore.StatusBlocked,
		AssignedTo: "codex",
		CreatedAt:  now,
		UpdatedAt:  now,
	}); err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	server.register(mux)
	req := httptest.NewRequest(http.MethodPost, "/tasks/"+taskID+"/delete", nil)
	rw := httptest.NewRecorder()
	mux.ServeHTTP(rw, req)

	if rw.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rw.Code, rw.Body.String())
	}
	if _, err := tasks.Load(taskID); err == nil {
		t.Fatal("deleted task still loads")
	}
}

func TestTaskMergeQueueEndpointReordersTask(t *testing.T) {
	server, tasks, _ := newHTTPTestServer(t)
	now := time.Now().UTC()
	firstID := "task_queue_endpoint_first"
	secondID := "task_queue_endpoint_second"
	for _, task := range []taskstore.Task{
		{
			ID:         firstID,
			Title:      "first",
			Goal:       "first",
			Status:     taskstore.StatusAwaitingApproval,
			AssignedTo: "codex",
			CreatedAt:  now.Add(-time.Minute),
			UpdatedAt:  now.Add(-time.Minute),
		},
		{
			ID:         secondID,
			Title:      "second",
			Goal:       "second",
			Status:     taskstore.StatusAwaitingApproval,
			AssignedTo: "codex",
			CreatedAt:  now,
			UpdatedAt:  now,
		},
	} {
		if err := tasks.Save(task); err != nil {
			t.Fatal(err)
		}
	}

	mux := http.NewServeMux()
	server.register(mux)
	requestJSON(t, mux, http.MethodPost, "/tasks/"+secondID+"/merge-queue", `{"direction":"up"}`, "", http.StatusOK)

	first, err := tasks.Load(firstID)
	if err != nil {
		t.Fatal(err)
	}
	second, err := tasks.Load(secondID)
	if err != nil {
		t.Fatal(err)
	}
	if second.MergeQueuePosition != 1 || first.MergeQueuePosition != 2 {
		t.Fatalf("positions: first=%d second=%d, want first=2 second=1", first.MergeQueuePosition, second.MergeQueuePosition)
	}
}

func TestWorkflowHTTPLifecycle(t *testing.T) {
	server, _, _ := newHTTPTestServer(t)
	mux := http.NewServeMux()
	server.register(mux)

	create := requestJSON(t, mux, http.MethodPost, "/workflows", `{
		"name":"Watch deploy",
		"goal":"Wait until the deployment is healthy",
		"steps":[{"name":"Health gate","kind":"wait","condition":"manual deployment gate","timeout_seconds":120}]
	}`, "", http.StatusCreated)
	var created struct {
		Workflow struct {
			ID       string `json:"id"`
			Name     string `json:"name"`
			Status   string `json:"status"`
			Estimate struct {
				Waits            int `json:"waits"`
				EstimatedSeconds int `json:"estimated_seconds"`
			} `json:"estimate"`
		} `json:"workflow"`
		Reply string `json:"reply"`
	}
	if err := json.Unmarshal(create.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.Workflow.ID == "" || created.Workflow.Estimate.Waits != 1 || created.Workflow.Estimate.EstimatedSeconds != 120 {
		t.Fatalf("created workflow = %#v", created.Workflow)
	}

	list := requestJSON(t, mux, http.MethodGet, "/workflows", "", "", http.StatusOK)
	if !strings.Contains(list.Body.String(), "Watch deploy") {
		t.Fatalf("workflow list = %s, want created workflow", list.Body.String())
	}

	run := requestJSON(t, mux, http.MethodPost, "/workflows/"+created.Workflow.ID+"/run", `{}`, "", http.StatusOK)
	if !strings.Contains(run.Body.String(), `"status":"waiting"`) || !strings.Contains(run.Body.String(), "manual deployment gate") {
		t.Fatalf("run response = %s, want waiting workflow run", run.Body.String())
	}
}

func TestAgentHeartbeatRequiresBearerToken(t *testing.T) {
	server := Server{RemoteAgents: remoteagent.NewStore(t.TempDir()), AgentToken: "secret"}
	mux := http.NewServeMux()
	server.register(mux)

	req := httptest.NewRequest(http.MethodPost, "/agents/desk/heartbeat", strings.NewReader(`{"name":"Desk"}`))
	req.Header.Set("Authorization", "Bearer wrong")
	rw := httptest.NewRecorder()
	mux.ServeHTTP(rw, req)

	if rw.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rw.Code, http.StatusUnauthorized)
	}
}

func TestAgentHeartbeatRegistersAgent(t *testing.T) {
	store := remoteagent.NewStore(t.TempDir())
	server := Server{RemoteAgents: store, AgentToken: "secret"}
	mux := http.NewServeMux()
	server.register(mux)

	req := httptest.NewRequest(http.MethodPost, "/agents/desk/heartbeat", strings.NewReader(`{"name":"Desk","machine":"desk","workdirs":[{"id":"repo","path":"/repo"}]}`))
	req.Header.Set("Authorization", "Bearer secret")
	rw := httptest.NewRecorder()
	mux.ServeHTTP(rw, req)

	if rw.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", rw.Code, http.StatusOK, rw.Body.String())
	}
	agent, err := store.Load("desk")
	if err != nil {
		t.Fatal(err)
	}
	if agent.Name != "Desk" || len(agent.Workdirs) != 1 {
		t.Fatalf("agent = %#v, want registered heartbeat", agent)
	}
}

func TestAgentHeartbeatForwardsToHealthd(t *testing.T) {
	store := remoteagent.NewStore(t.TempDir())
	var forwarded healthd.ProcessHeartbeat
	var forwardedAddr string
	server := Server{
		RemoteAgents:    store,
		AgentToken:      "secret",
		HealthdURL:      "http://healthd.local",
		AgentStaleAfter: 45 * time.Second,
		HealthdPush: func(ctx context.Context, client *http.Client, addr string, heartbeat healthd.ProcessHeartbeat) error {
			forwardedAddr = addr
			forwarded = heartbeat
			return nil
		},
	}
	mux := http.NewServeMux()
	server.register(mux)

	req := httptest.NewRequest(http.MethodPost, "/agents/desk/heartbeat", strings.NewReader(`{
		"name":"Desk",
		"machine":"desk.local",
		"capabilities":["codex","directory-context"],
		"current_task_id":"task_1",
		"workdirs":[{"id":"repo","path":"/repo"}]
	}`))
	req.Header.Set("Authorization", "Bearer secret")
	rw := httptest.NewRecorder()
	mux.ServeHTTP(rw, req)

	if rw.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", rw.Code, http.StatusOK, rw.Body.String())
	}
	if forwardedAddr != "http://healthd.local" {
		t.Fatalf("forwarded addr = %q", forwardedAddr)
	}
	if forwarded.Name != "remote-agent:desk" || forwarded.Type != "remote_agent" || forwarded.TTLSeconds != 45 {
		t.Fatalf("forwarded heartbeat = %#v", forwarded)
	}
	if forwarded.Metadata["service.instance.id"] != "desk" ||
		forwarded.Metadata["machine"] != "desk.local" ||
		forwarded.Metadata["current_task_id"] != "task_1" ||
		forwarded.Metadata["workdirs"] != "1" {
		t.Fatalf("metadata = %#v", forwarded.Metadata)
	}
}

func TestRemoteAgentHTTPTaskLifecycle(t *testing.T) {
	server, tasks, approvals, mux := newRemoteControlTestServer(t)

	agentHeartbeat := `{"name":"Desk","machine":"desk.local","workdirs":[{"id":"repo","path":"/srv/desk/repo"}],"capabilities":["codex"]}`
	requestJSON(t, mux, http.MethodPost, "/agents/desk/heartbeat", agentHeartbeat, "secret", http.StatusOK)
	requestJSON(t, mux, http.MethodPost, "/agents/nuc/heartbeat", `{"name":"Nuc","machine":"nuc.local","workdirs":[{"id":"repo","path":"/srv/nuc/repo"}]}`, "secret", http.StatusOK)

	createBody := `{"goal":"update the remote checkout","target":{"mode":"remote","agent_id":"desk","workdir_id":"repo","backend":"codex"}}`
	requestJSON(t, mux, http.MethodPost, "/tasks", createBody, "", http.StatusCreated)

	allTasks, err := tasks.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(allTasks) != 1 {
		t.Fatalf("task count = %d, want one remote task", len(allTasks))
	}
	task := allTasks[0]
	if task.Target == nil || task.Target.AgentID != "desk" || task.Target.Workdir != "/srv/desk/repo" || task.Workspace != "" {
		t.Fatalf("created task = %#v, want remote target with no local workspace", task)
	}

	wrongClaim := requestJSON(t, mux, http.MethodPost, "/agents/nuc/claim", `{"backend":"codex"}`, "secret", http.StatusOK)
	var wrongClaimBody struct {
		Assignment *remoteagent.Assignment `json:"assignment"`
	}
	if err := json.Unmarshal(wrongClaim.Body.Bytes(), &wrongClaimBody); err != nil {
		t.Fatal(err)
	}
	if wrongClaimBody.Assignment != nil {
		t.Fatalf("wrong agent claimed assignment %#v", wrongClaimBody.Assignment)
	}

	claim := requestJSON(t, mux, http.MethodPost, "/agents/desk/claim", `{"backend":"codex"}`, "secret", http.StatusOK)
	var claimBody struct {
		Assignment *remoteagent.Assignment `json:"assignment"`
	}
	if err := json.Unmarshal(claim.Body.Bytes(), &claimBody); err != nil {
		t.Fatal(err)
	}
	if claimBody.Assignment == nil || claimBody.Assignment.TaskID != task.ID || claimBody.Assignment.Workdir != "/srv/desk/repo" {
		t.Fatalf("assignment = %#v", claimBody.Assignment)
	}
	running, err := tasks.Load(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if running.Status != taskstore.StatusRunning || running.AssignedTo != "desk" {
		t.Fatalf("running task = %#v", running)
	}

	requestJSON(t, mux, http.MethodPost, "/agents/nuc/tasks/"+task.ID+"/complete", `{"status":"completed","result":"bad"}`, "secret", http.StatusConflict)
	requestJSON(t, mux, http.MethodPost, "/agents/desk/tasks/"+task.ID+"/complete", `{"status":"completed","result":"changed remote files; validation passed"}`, "secret", http.StatusOK)

	ready, err := tasks.Load(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if ready.Status != taskstore.StatusReadyForReview || !strings.Contains(ready.Result, "changed remote files") {
		t.Fatalf("ready task = %#v", ready)
	}

	review := requestJSON(t, mux, http.MethodPost, "/tasks/"+task.ID+"/review", `{}`, "", http.StatusOK)
	if strings.Contains(review.Body.String(), "Merge approval requested") {
		t.Fatalf("remote review requested local merge approval: %s", review.Body.String())
	}
	verified, err := tasks.Load(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if verified.Status != taskstore.StatusAwaitingVerification {
		t.Fatalf("verified status = %q, want awaiting_verification", verified.Status)
	}
	approvalList, err := approvals.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(approvalList) != 0 {
		t.Fatalf("approvals = %#v, remote review must not create local merge approval", approvalList)
	}
	_ = server
}

func TestCreateRemoteTaskRejectsUnknownAgentAndMissingWorkdir(t *testing.T) {
	_, _, _, mux := newRemoteControlTestServer(t)

	unknown := requestJSON(t, mux, http.MethodPost, "/tasks", `{"goal":"bad","target":{"mode":"remote","agent_id":"missing","workdir_id":"repo"}}`, "", http.StatusInternalServerError)
	if !strings.Contains(unknown.Body.String(), "remote agent") {
		t.Fatalf("unknown agent response = %s", unknown.Body.String())
	}

	requestJSON(t, mux, http.MethodPost, "/agents/desk/heartbeat", `{"name":"Desk","workdirs":[]}`, "secret", http.StatusOK)
	missingWorkdir := requestJSON(t, mux, http.MethodPost, "/tasks", `{"goal":"bad","target":{"mode":"remote","agent_id":"desk","workdir_id":"repo"}}`, "", http.StatusInternalServerError)
	if !strings.Contains(missingWorkdir.Body.String(), "remote working directory") {
		t.Fatalf("missing workdir response = %s", missingWorkdir.Body.String())
	}

	requestJSON(t, mux, http.MethodPost, "/agents/desk/heartbeat", `{"name":"Desk","workdirs":[{"id":"repo","path":"/srv/desk/repo"}]}`, "secret", http.StatusOK)
	unknownWorkdir := requestJSON(t, mux, http.MethodPost, "/tasks", `{"goal":"bad","target":{"mode":"remote","agent_id":"desk","workdir_id":"wrong-repo"}}`, "", http.StatusInternalServerError)
	if !strings.Contains(unknownWorkdir.Body.String(), "not advertised") {
		t.Fatalf("unknown workdir response = %s", unknownWorkdir.Body.String())
	}
}

func newHTTPTestServer(t *testing.T) (*Server, *taskstore.Store, config.Config) {
	t.Helper()
	dir := t.TempDir()
	cfg := config.Default()
	cfg.DataDir = filepath.Join(dir, "data")
	cfg.Repo.Root = dir
	cfg.Repo.WorkspaceRoot = filepath.Join(dir, "workspaces")
	tasks := taskstore.NewStore(filepath.Join(cfg.DataDir, "tasks"))
	orch := agent.NewOrchestrator(
		cfg,
		eventlog.NewStore(filepath.Join(cfg.DataDir, "events")),
		tasks,
		approvalstore.NewStore(filepath.Join(cfg.DataDir, "approvals")),
		tool.NewRegistry(),
		tool.NewPolicy(nil),
		nil,
		"",
	)
	return &Server{Orchestrator: orch}, tasks, cfg
}

func newKnowledgeHTTPTestServer(t *testing.T, provider llm.Provider) *Server {
	return newKnowledgeHTTPTestServerWithTools(t, provider)
}

func newKnowledgeHTTPTestServerWithTools(t *testing.T, provider llm.Provider, tools ...tool.Tool) *Server {
	t.Helper()
	dir := t.TempDir()
	cfg := config.Default()
	cfg.DataDir = filepath.Join(dir, "data")
	cfg.Repo.Root = dir
	cfg.Repo.WorkspaceRoot = filepath.Join(dir, "workspaces")
	registry := tool.NewRegistry()
	for _, registeredTool := range tools {
		if err := registry.Register(registeredTool); err != nil {
			t.Fatal(err)
		}
	}
	orch := agent.NewOrchestrator(
		cfg,
		eventlog.NewStore(filepath.Join(cfg.DataDir, "events")),
		taskstore.NewStore(filepath.Join(cfg.DataDir, "tasks")),
		approvalstore.NewStore(filepath.Join(cfg.DataDir, "approvals")),
		registry,
		tool.NewPolicy(nil),
		provider,
		"test-model",
	)
	return &Server{Orchestrator: orch}
}

func newRemoteControlTestServer(t *testing.T) (*Server, *taskstore.Store, *approvalstore.Store, *http.ServeMux) {
	t.Helper()
	dir := t.TempDir()
	cfg := config.Default()
	cfg.DataDir = filepath.Join(dir, "data")
	cfg.Repo.Root = filepath.Join(dir, "repo")
	cfg.Repo.WorkspaceRoot = filepath.Join(dir, "workspaces")
	tasks := taskstore.NewStore(filepath.Join(cfg.DataDir, "tasks"))
	approvals := approvalstore.NewStore(filepath.Join(cfg.DataDir, "approvals"))
	remoteAgents := remoteagent.NewStore(filepath.Join(cfg.DataDir, "remote_agents"))
	orch := agent.NewOrchestrator(
		cfg,
		eventlog.NewStore(filepath.Join(cfg.DataDir, "events")),
		tasks,
		approvals,
		tool.NewRegistry(),
		tool.NewPolicy(nil),
		nil,
		"",
	).WithRemoteAgents(remoteAgents)
	server := &Server{
		Orchestrator: orch,
		RemoteAgents: remoteAgents,
		AgentToken:   "secret",
	}
	mux := http.NewServeMux()
	server.register(mux)
	return server, tasks, approvals, mux
}

func requestJSON(t *testing.T, mux *http.ServeMux, method, path, body, token string, wantStatus int) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rw := httptest.NewRecorder()
	mux.ServeHTTP(rw, req)
	if rw.Code != wantStatus {
		t.Fatalf("%s %s status = %d, want %d: %s", method, path, rw.Code, wantStatus, rw.Body.String())
	}
	return rw
}

type messageStatsProvider struct{}

func (messageStatsProvider) Name() string { return "test-provider" }

func (messageStatsProvider) Complete(_ context.Context, req llm.CompletionRequest) (llm.CompletionResponse, error) {
	return llm.CompletionResponse{
		Message: llm.Message{
			Role:    "assistant",
			Content: `{"message":"Measured reply.","done":true,"tool_calls":[],"buttons":["Yes","No"]}`,
		},
		Usage: llm.Usage{
			InputTokens:  11,
			OutputTokens: 6,
			TotalTokens:  17,
		},
		Provider: messageStatsProvider{}.Name(),
		Model:    req.Model,
	}, nil
}

type scriptedControlProvider struct {
	mu       sync.Mutex
	contents []string
}

func (p *scriptedControlProvider) Name() string { return "scripted" }

func (p *scriptedControlProvider) Complete(_ context.Context, req llm.CompletionRequest) (llm.CompletionResponse, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.contents) == 0 {
		return llm.CompletionResponse{}, context.Canceled
	}
	content := p.contents[0]
	p.contents = p.contents[1:]
	return llm.CompletionResponse{
		Message:  llm.Message{Role: "assistant", Content: content},
		Provider: p.Name(),
		Model:    req.Model,
		Usage:    llm.Usage{InputTokens: 10, OutputTokens: 6, TotalTokens: 16},
	}, nil
}

type controlInternetResearchCall struct {
	Query       string   `json:"query"`
	Queries     []string `json:"queries"`
	Source      string   `json:"source"`
	Depth       string   `json:"depth"`
	Provider    string   `json:"provider"`
	MaxSearches int      `json:"max_searches"`
	Fetch       bool     `json:"fetch"`
}

type controlInternetResearchStub struct {
	mu            sync.Mutex
	calls         []controlInternetResearchCall
	sources       []map[string]any
	sourceBatches [][]map[string]any
	searchErrors  []string
}

func (controlInternetResearchStub) Name() string        { return "internet.research" }
func (controlInternetResearchStub) Description() string { return "stubbed internet research" }
func (controlInternetResearchStub) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object"}`)
}
func (controlInternetResearchStub) Risk() tool.RiskLevel { return tool.RiskReadOnly }
func (s *controlInternetResearchStub) Run(_ context.Context, raw json.RawMessage) (json.RawMessage, error) {
	var call controlInternetResearchCall
	_ = json.Unmarshal(raw, &call)
	s.mu.Lock()
	s.calls = append(s.calls, call)
	sources := s.sources
	if len(s.sourceBatches) > 0 {
		index := len(s.calls) - 1
		if index >= len(s.sourceBatches) {
			index = len(s.sourceBatches) - 1
		}
		sources = s.sourceBatches[index]
	}
	s.mu.Unlock()
	return json.Marshal(map[string]any{
		"query":           call.Query,
		"source":          call.Source,
		"depth":           call.Depth,
		"method":          "stubbed search and fetch",
		"search_provider": call.Provider,
		"sources":         sources,
		"search_errors":   s.searchErrors,
	})
}

func (s *controlInternetResearchStub) Calls() []controlInternetResearchCall {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]controlInternetResearchCall(nil), s.calls...)
}

func containsResearchEvent(events []knowledgestore.ResearchRunEvent, stage, text string) bool {
	for _, event := range events {
		if event.Stage == stage && strings.Contains(event.Message, text) {
			return true
		}
	}
	return false
}

func waitForKnowledgeRun(t *testing.T, mux *http.ServeMux, spaceID, runID string) (knowledgestore.Space, knowledgestore.ResearchRun) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	path := "/knowledge/spaces/" + spaceID
	for time.Now().Before(deadline) {
		response := requestJSON(t, mux, http.MethodGet, path, "", "", http.StatusOK)
		var space knowledgestore.Space
		if err := json.NewDecoder(response.Body).Decode(&space); err != nil {
			t.Fatal(err)
		}
		for _, run := range space.ResearchRuns {
			if run.ID == runID && (run.Status == knowledgestore.ResearchRunStatusCompleted || run.Status == knowledgestore.ResearchRunStatusFailed) {
				return space, run
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("knowledge research run %s did not finish", runID)
	return knowledgestore.Space{}, knowledgestore.ResearchRun{}
}

func writeJSONFile(t *testing.T, path string, value any) {
	t.Helper()
	b, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(b, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}

func gitHTTPTestRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
}
