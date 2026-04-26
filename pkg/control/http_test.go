package control

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/andrewneudegg/lab/pkg/agent"
	"github.com/andrewneudegg/lab/pkg/config"
	"github.com/andrewneudegg/lab/pkg/eventlog"
	"github.com/andrewneudegg/lab/pkg/healthd"
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

func newHTTPTestServer(t *testing.T) (Server, *taskstore.Store, config.Config) {
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
	return Server{Orchestrator: orch}, tasks, cfg
}

func newRemoteControlTestServer(t *testing.T) (Server, *taskstore.Store, *approvalstore.Store, *http.ServeMux) {
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
	server := Server{
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
