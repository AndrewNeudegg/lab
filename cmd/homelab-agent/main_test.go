package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/andrewneudegg/lab/pkg/config"
	agentrunner "github.com/andrewneudegg/lab/pkg/externalagent"
	"github.com/andrewneudegg/lab/pkg/remoteagent"
	taskstore "github.com/andrewneudegg/lab/pkg/task"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func TestWorkdirFlagsParseIDPathAndBarePath(t *testing.T) {
	var flags workdirFlags
	if err := flags.Set("repo=/srv/repo"); err != nil {
		t.Fatal(err)
	}
	if err := flags.Set("/srv/other"); err != nil {
		t.Fatal(err)
	}

	if len(flags) != 2 {
		t.Fatalf("flags = %#v, want 2 workdirs", flags)
	}
	if flags[0].ID != "repo" || flags[0].Label != "repo" || flags[0].Path != "/srv/repo" {
		t.Fatalf("first flag = %#v", flags[0])
	}
	if flags[1].ID != "" || flags[1].Path != "/srv/other" {
		t.Fatalf("second flag = %#v", flags[1])
	}
	if flags.String() != "/srv/repo,/srv/other" {
		t.Fatalf("String() = %q", flags.String())
	}
}

func TestWorkdirFlagsRejectEmptyPathAfterEquals(t *testing.T) {
	var flags workdirFlags
	if err := flags.Set("repo="); err == nil {
		t.Fatal("Set(repo=) succeeded, want error")
	}
}

func TestRemoteWorkdirsDropsEmptyAndUsesPathAsID(t *testing.T) {
	got := remoteWorkdirs([]config.RemoteAgentWorkdirConfig{
		{ID: "repo", Path: "/srv/repo", Label: "Repo"},
		{Path: " "},
		{Path: "/srv/other"},
	})
	if len(got) != 2 {
		t.Fatalf("workdirs = %#v, want 2", got)
	}
	if got[0].ID != "repo" || got[0].Path != "/srv/repo" || got[0].Label != "Repo" {
		t.Fatalf("first = %#v", got[0])
	}
	if got[1].ID != "/srv/other" || got[1].Path != "/srv/other" {
		t.Fatalf("second = %#v", got[1])
	}
}

func TestRemoteAgentMetadataAdvertisesTerminalURL(t *testing.T) {
	capabilities, metadata := remoteAgentMetadata("codex", "http://desk.local:18083")

	if !contains(capabilities, "codex") || !contains(capabilities, "terminal") {
		t.Fatalf("capabilities = %#v, want backend and terminal", capabilities)
	}
	if metadata["terminal_base_url"] != "http://desk.local:18083" {
		t.Fatalf("metadata = %#v, want terminal_base_url", metadata)
	}
}

func TestRemoteAgentMetadataOmitsTerminalWhenURLMissing(t *testing.T) {
	capabilities, metadata := remoteAgentMetadata("codex", "")

	if contains(capabilities, "terminal") {
		t.Fatalf("capabilities = %#v, should not advertise terminal", capabilities)
	}
	if len(metadata) != 0 {
		t.Fatalf("metadata = %#v, want empty", metadata)
	}
}

func TestControlClientSendsBearerTokenAndDecodesAssignment(t *testing.T) {
	var gotPath, gotAuth, gotBackend string
	client := controlClient{
		base:  "http://homelabd",
		token: "secret",
		http: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			gotPath = req.URL.Path
			gotAuth = req.Header.Get("Authorization")
			var body map[string]string
			if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			gotBackend = body["backend"]
			return jsonResponse(http.StatusOK, `{"assignment":{"task_id":"task_1","workdir":"/srv/repo","backend":"codex","instruction":"do it"}}`), nil
		})},
	}

	assignment, err := client.claim(context.Background(), "desk", "codex")
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/agents/desk/claim" || gotAuth != "Bearer secret" || gotBackend != "codex" {
		t.Fatalf("request path/auth/backend = %q %q %q", gotPath, gotAuth, gotBackend)
	}
	if assignment == nil || assignment.TaskID != "task_1" || assignment.Workdir != "/srv/repo" {
		t.Fatalf("assignment = %#v", assignment)
	}
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func TestRemoteHeartbeatLoopContinuesWhileAssignmentBlocks(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var heartbeats atomic.Int32
	thirdHeartbeat := make(chan struct{})
	done := startRemoteHeartbeatLoop(ctx, time.Millisecond, func() {
		if heartbeats.Add(1) == 3 {
			close(thirdHeartbeat)
		}
	})

	select {
	case <-thirdHeartbeat:
	case <-time.After(time.Second):
		t.Fatalf("heartbeats = %d, want heartbeat loop to continue while caller is blocked", heartbeats.Load())
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("heartbeat loop did not stop after context cancellation")
	}
}

func TestControlClientReturnsHTTPErrorBody(t *testing.T) {
	client := controlClient{
		base:  "http://homelabd",
		token: "secret",
		http: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return jsonResponse(http.StatusUnauthorized, `{"error":"invalid token"}`), nil
		})},
	}

	err := client.heartbeat(context.Background(), remoteagent.Heartbeat{ID: "desk"})
	if err == nil || !strings.Contains(err.Error(), "401 Unauthorized") || !strings.Contains(err.Error(), "invalid token") {
		t.Fatalf("heartbeat error = %v, want status and body", err)
	}
}

func TestExecuteAssignmentUsesSelectedWorkdirAndReportsCompletion(t *testing.T) {
	runner := &fakeAssignmentRunner{result: agentrunner.RunResult{Output: " done \n"}}
	client := &fakeAgentControl{}
	assignment := &remoteagent.Assignment{
		TaskID:      "task_1",
		Workdir:     "/srv/desk/repo",
		Backend:     "codex",
		Instruction: "fix it",
	}

	if err := executeAssignment(context.Background(), client, runner, "desk", "fallback", assignment); err != nil {
		t.Fatal(err)
	}
	if runner.request.Backend != "codex" || runner.request.Workspace != "/srv/desk/repo" || runner.request.Instruction != "fix it" {
		t.Fatalf("runner request = %#v", runner.request)
	}
	if client.agentID != "desk" || client.taskID != "task_1" || client.status != "completed" || client.result != "done" || client.errorText != "" {
		t.Fatalf("completion = %#v", client)
	}
}

func TestExecuteAssignmentReportsNoChangeRequired(t *testing.T) {
	runner := &fakeAssignmentRunner{result: agentrunner.RunResult{Output: "No change required: duplicate report\n"}}
	client := &fakeAgentControl{}

	if err := executeAssignment(context.Background(), client, runner, "desk", "codex", &remoteagent.Assignment{
		TaskID:      "task_1",
		Workdir:     "/srv/desk/repo",
		Instruction: "investigate it",
	}); err != nil {
		t.Fatal(err)
	}
	if client.status != "no_change_required" || !strings.Contains(client.result, "duplicate report") {
		t.Fatalf("completion = %#v, want no_change_required result", client)
	}
}

func TestExecuteAssignmentReportsCapturedGitDiff(t *testing.T) {
	workdir := t.TempDir()
	gitAgentTestRun(t, workdir, "init", "--initial-branch=main")
	gitAgentTestRun(t, workdir, "config", "user.email", "test@example.com")
	gitAgentTestRun(t, workdir, "config", "user.name", "Test User")
	if err := os.WriteFile(filepath.Join(workdir, "README.md"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitAgentTestRun(t, workdir, "add", "README.md")
	gitAgentTestRun(t, workdir, "commit", "-m", "base")
	if err := os.WriteFile(filepath.Join(workdir, "README.md"), []byte("base\npreexisting\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	runner := &fakeAssignmentRunner{
		result: agentrunner.RunResult{Output: "done\n"},
		mutate: func(req agentrunner.RunRequest) {
			if err := os.MkdirAll(filepath.Join(req.Workspace, "src"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(req.Workspace, "src", "grid.js"), []byte("export const ready = true;\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		},
	}
	client := &fakeAgentControl{}
	if err := executeAssignment(context.Background(), client, runner, "desk", "codex", &remoteagent.Assignment{
		TaskID:      "task_1",
		Workdir:     workdir,
		Instruction: "build it",
	}); err != nil {
		t.Fatal(err)
	}
	if client.status != "completed" || !strings.Contains(client.diff, "new file mode") || !strings.Contains(client.diff, "+export const ready = true;") {
		t.Fatalf("completion = %#v, want captured untracked diff", client)
	}
	if strings.Contains(client.diff, "preexisting") {
		t.Fatalf("diff = %q, want task-scoped diff without preexisting worktree changes", client.diff)
	}
	if client.diffSource != "remote_agent_task_snapshot" || client.diffBaseRef == "" || client.diffHeadRef == "" {
		t.Fatalf("diff metadata source/base/head = %q/%q/%q, want task snapshot metadata", client.diffSource, client.diffBaseRef, client.diffHeadRef)
	}
}

func TestExecuteAssignmentReportsRunnerFailure(t *testing.T) {
	runner := &fakeAssignmentRunner{err: fmt.Errorf("runner failed")}
	client := &fakeAgentControl{}

	if err := executeAssignment(context.Background(), client, runner, "desk", "codex", &remoteagent.Assignment{
		TaskID:      "task_1",
		Workdir:     "/srv/desk/repo",
		Instruction: "fix it",
	}); err != nil {
		t.Fatal(err)
	}
	if runner.request.Backend != "codex" {
		t.Fatalf("backend = %q, want fallback codex", runner.request.Backend)
	}
	if client.status != "failed" || !strings.Contains(client.result, "runner failed") || client.errorText != "runner failed" {
		t.Fatalf("failure completion = %#v", client)
	}
}

func TestExecuteAssignmentReportsRunnerTimeout(t *testing.T) {
	runner := &fakeAssignmentRunner{
		result: agentrunner.RunResult{Output: "partial work\n", Error: "external agent timed out"},
		err:    context.DeadlineExceeded,
	}
	client := &fakeAgentControl{}

	if err := executeAssignment(context.Background(), client, runner, "desk", "codex", &remoteagent.Assignment{
		TaskID:      "task_1",
		Workdir:     "/srv/desk/repo",
		Instruction: "fix it",
	}); err != nil {
		t.Fatal(err)
	}
	if client.status != taskstore.StatusTimedOut || client.errorText != "external agent timed out" || !strings.Contains(client.result, "partial work") {
		t.Fatalf("timeout completion = %#v, want timed_out with partial result", client)
	}
}

func TestExecuteAssignmentDurablyKeepsPendingCompletionWhenCallbackFails(t *testing.T) {
	runner := &fakeAssignmentRunner{result: agentrunner.RunResult{Output: "done\n"}}
	client := &fakeAgentControl{err: fmt.Errorf("control plane offline")}
	store := pendingCompletionStore{dir: filepath.Join(t.TempDir(), "pending")}
	state := newRemoteAgentRuntimeState()
	var heartbeats atomic.Int32
	assignment := &remoteagent.Assignment{
		TaskID:      "task_1",
		Workdir:     "/srv/desk/repo",
		Backend:     "codex",
		Instruction: "fix it",
	}

	err := executeAssignmentDurably(context.Background(), client, runner, store, state, func() {
		heartbeats.Add(1)
	}, "desk", "fallback", assignment)
	if err == nil || !strings.Contains(err.Error(), "control plane offline") {
		t.Fatalf("executeAssignmentDurably error = %v, want callback failure", err)
	}
	if state.currentTask() != "task_1" {
		t.Fatalf("current task = %q, want task_1 while completion is pending", state.currentTask())
	}
	pending, err := store.list()
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 || pending[0].TaskID != "task_1" || pending[0].Status != "completed" || pending[0].Result != "done" {
		t.Fatalf("pending completions = %#v, want saved completion", pending)
	}
	if client.completeCalls != 1 {
		t.Fatalf("complete calls = %d, want initial callback attempt", client.completeCalls)
	}

	client.err = nil
	hadPending, err := flushPendingCompletions(context.Background(), client, store, state, func() {
		heartbeats.Add(1)
	})
	if err != nil {
		t.Fatal(err)
	}
	if !hadPending {
		t.Fatal("flushPendingCompletions hadPending = false, want true")
	}
	if state.currentTask() != "" {
		t.Fatalf("current task = %q, want cleared after replay", state.currentTask())
	}
	pending, err = store.list()
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 0 {
		t.Fatalf("pending completions after replay = %#v, want empty", pending)
	}
	if client.completeCalls != 2 || client.status != "completed" || client.result != "done" {
		t.Fatalf("completion after replay = %#v, want accepted completed result", client)
	}
	if heartbeats.Load() == 0 {
		t.Fatal("heartbeats = 0, want state changes to be advertised")
	}
}

func TestPendingCompletionStoreOrdersSavedCompletions(t *testing.T) {
	store := pendingCompletionStore{dir: filepath.Join(t.TempDir(), "pending")}
	newer := remoteCompletionPayload{
		AgentID:   "desk",
		TaskID:    "task_newer",
		Status:    "completed",
		CreatedAt: time.Date(2026, 5, 11, 12, 0, 0, 0, time.UTC),
	}
	older := remoteCompletionPayload{
		AgentID:   "desk",
		TaskID:    "task_older",
		Status:    "completed",
		CreatedAt: time.Date(2026, 5, 11, 11, 0, 0, 0, time.UTC),
	}
	if err := store.save(newer); err != nil {
		t.Fatal(err)
	}
	if err := store.save(older); err != nil {
		t.Fatal(err)
	}

	got, err := store.list()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].TaskID != "task_older" || got[1].TaskID != "task_newer" {
		t.Fatalf("pending order = %#v, want oldest first", got)
	}
	if err := store.delete("task_older"); err != nil {
		t.Fatal(err)
	}
	got, err = store.list()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].TaskID != "task_newer" {
		t.Fatalf("pending after delete = %#v, want only newer", got)
	}
}

type fakeAssignmentRunner struct {
	request agentrunner.RunRequest
	result  agentrunner.RunResult
	err     error
	mutate  func(agentrunner.RunRequest)
}

func (r *fakeAssignmentRunner) Run(ctx context.Context, req agentrunner.RunRequest) (agentrunner.RunResult, error) {
	r.request = req
	if r.mutate != nil {
		r.mutate(req)
	}
	return r.result, r.err
}

type fakeAgentControl struct {
	agentID       string
	taskID        string
	status        string
	result        string
	errorText     string
	diff          string
	diffSource    string
	diffBaseRef   string
	diffHeadRef   string
	completeCalls int
	err           error
}

func (c *fakeAgentControl) complete(ctx context.Context, agentID, taskID, status, result, errorText string, diff gitDiffSnapshot) error {
	c.completeCalls++
	c.agentID = agentID
	c.taskID = taskID
	c.status = status
	c.result = result
	c.errorText = errorText
	c.diff = diff.RawDiff
	c.diffSource = diff.Source
	c.diffBaseRef = diff.BaseRef
	c.diffHeadRef = diff.HeadRef
	return c.err
}

func jsonResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Status:     fmt.Sprintf("%d %s", status, http.StatusText(status)),
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func gitAgentTestRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, strings.TrimSpace(string(out)))
	}
}
