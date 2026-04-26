package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/andrewneudegg/lab/pkg/config"
	agentrunner "github.com/andrewneudegg/lab/pkg/externalagent"
	"github.com/andrewneudegg/lab/pkg/remoteagent"
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
	if got[0] != (remoteagent.Workdir{ID: "repo", Path: "/srv/repo", Label: "Repo"}) {
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

type fakeAssignmentRunner struct {
	request agentrunner.RunRequest
	result  agentrunner.RunResult
	err     error
}

func (r *fakeAssignmentRunner) Run(ctx context.Context, req agentrunner.RunRequest) (agentrunner.RunResult, error) {
	r.request = req
	return r.result, r.err
}

type fakeAgentControl struct {
	agentID   string
	taskID    string
	status    string
	result    string
	errorText string
}

func (c *fakeAgentControl) complete(ctx context.Context, agentID, taskID, status, result, errorText string) error {
	c.agentID = agentID
	c.taskID = taskID
	c.status = status
	c.result = result
	c.errorText = errorText
	return nil
}

func jsonResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Status:     fmt.Sprintf("%d %s", status, http.StatusText(status)),
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}
