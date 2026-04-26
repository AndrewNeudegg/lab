package control

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/andrewneudegg/lab/pkg/agent"
	"github.com/andrewneudegg/lab/pkg/config"
	"github.com/andrewneudegg/lab/pkg/eventlog"
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
