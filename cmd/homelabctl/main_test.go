package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"
)

type observedRequest struct {
	Method string
	Path   string
	Query  string
	Body   map[string]any
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestTaskCommandsCoverCurrentHTTPAPI(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantMethod string
		wantPath   string
		wantQuery  string
		wantBody   map[string]any
	}{
		{
			name:       "health",
			args:       []string{"health"},
			wantMethod: http.MethodGet,
			wantPath:   "/healthz",
		},
		{
			name:       "task runs",
			args:       []string{"task", "runs", "task_123"},
			wantMethod: http.MethodGet,
			wantPath:   "/tasks/task_123/runs",
		},
		{
			name:       "remote task target",
			args:       []string{"task", "new", "--agent", "desk", "--workdir", "repo", "--backend", "codex", "do", "work"},
			wantMethod: http.MethodPost,
			wantPath:   "/tasks",
			wantBody: map[string]any{
				"goal": "do work",
				"target": map[string]any{
					"mode":       "remote",
					"agent_id":   "desk",
					"workdir_id": "repo",
					"backend":    "codex",
				},
			},
		},
		{
			name:       "task cancel",
			args:       []string{"cancel", "task_123"},
			wantMethod: http.MethodPost,
			wantPath:   "/tasks/task_123/cancel",
		},
		{
			name:       "task delete",
			args:       []string{"delete", "task_123"},
			wantMethod: http.MethodPost,
			wantPath:   "/tasks/task_123/delete",
		},
		{
			name:       "task restart",
			args:       []string{"restart", "task_123"},
			wantMethod: http.MethodPost,
			wantPath:   "/tasks/task_123/restart",
		},
		{
			name:       "task queue move",
			args:       []string{"task", "queue", "task_123", "up"},
			wantMethod: http.MethodPost,
			wantPath:   "/tasks/task_123/merge-queue",
			wantBody:   map[string]any{"direction": "up"},
		},
		{
			name:       "task retry with backend",
			args:       []string{"retry", "task_123", "codex", "inspect", "again"},
			wantMethod: http.MethodPost,
			wantPath:   "/tasks/task_123/retry",
			wantBody:   map[string]any{"backend": "codex", "instruction": "inspect again"},
		},
		{
			name:       "task retry with instruction only",
			args:       []string{"task", "retry", "task_123", "inspect", "again"},
			wantMethod: http.MethodPost,
			wantPath:   "/tasks/task_123/retry",
			wantBody:   map[string]any{"backend": "", "instruction": "inspect again"},
		},
		{
			name:       "events date and limit",
			args:       []string{"events", "-limit", "2", "2026-04-26"},
			wantMethod: http.MethodGet,
			wantPath:   "/events",
			wantQuery:  "date=2026-04-26&limit=2",
		},
		{
			name:       "workflow create",
			args:       []string{"workflow", "new", "Research", "bundle:", "Find", "sources"},
			wantMethod: http.MethodPost,
			wantPath:   "/workflows",
			wantBody:   map[string]any{"name": "Research bundle", "goal": "Find sources"},
		},
		{
			name:       "workflow run",
			args:       []string{"workflow", "run", "workflow_123"},
			wantMethod: http.MethodPost,
			wantPath:   "/workflows/workflow_123/run",
		},
		{
			name:       "settings show",
			args:       []string{"settings"},
			wantMethod: http.MethodGet,
			wantPath:   "/settings",
		},
		{
			name:       "auto merge on",
			args:       []string{"settings", "auto-merge", "on"},
			wantMethod: http.MethodPost,
			wantPath:   "/settings",
			wantBody:   map[string]any{"auto_merge_enabled": true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var observed observedRequest
			stdout, stderr, code := runAgainstServer(t, tt.args, "", func(rw http.ResponseWriter, req *http.Request) {
				observed = observeRequest(t, req)
				writeTestJSON(t, rw, http.StatusOK, map[string]any{"ok": true})
			})
			if code != 0 {
				t.Fatalf("exit code = %d, stderr = %s", code, stderr)
			}
			if observed.Method != tt.wantMethod || observed.Path != tt.wantPath || observed.Query != tt.wantQuery {
				t.Fatalf("request = %s %s?%s, want %s %s?%s", observed.Method, observed.Path, observed.Query, tt.wantMethod, tt.wantPath, tt.wantQuery)
			}
			if !reflect.DeepEqual(observed.Body, tt.wantBody) {
				t.Fatalf("body = %#v, want %#v", observed.Body, tt.wantBody)
			}
			if !strings.Contains(stdout, `"ok": true`) {
				t.Fatalf("stdout did not contain pretty JSON response: %q", stdout)
			}
		})
	}
}

func TestTaskNewAttachesFiles(t *testing.T) {
	path := t.TempDir() + "/context.txt"
	if err := os.WriteFile(path, []byte("browser context"), 0o644); err != nil {
		t.Fatal(err)
	}
	var observed observedRequest
	_, stderr, code := runAgainstServer(t, []string{"task", "new", "--attach", path, "fix", "the", "bug"}, "", func(rw http.ResponseWriter, req *http.Request) {
		observed = observeRequest(t, req)
		writeTestJSON(t, rw, http.StatusOK, map[string]any{"ok": true})
	})
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %s", code, stderr)
	}
	if observed.Method != http.MethodPost || observed.Path != "/tasks" {
		t.Fatalf("request = %s %s, want POST /tasks", observed.Method, observed.Path)
	}
	if observed.Body["goal"] != "fix the bug" {
		t.Fatalf("goal = %#v, want fix the bug", observed.Body["goal"])
	}
	attachments, ok := observed.Body["attachments"].([]any)
	if !ok || len(attachments) != 1 {
		t.Fatalf("attachments = %#v, want one attachment", observed.Body["attachments"])
	}
	attachment, ok := attachments[0].(map[string]any)
	if !ok {
		t.Fatalf("attachment = %#v, want object", attachments[0])
	}
	if attachment["name"] != "context.txt" || attachment["text"] != "browser context" {
		t.Fatalf("attachment = %#v, want named text attachment", attachment)
	}
	if !strings.HasPrefix(fmt.Sprint(attachment["data_url"]), "data:text/plain") {
		t.Fatalf("data_url = %#v, want text/plain data URL", attachment["data_url"])
	}
}

func TestAgentCommandsUseAgentEndpoints(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantMethod string
		wantPath   string
	}{
		{name: "list", args: []string{"agent", "list"}, wantMethod: http.MethodGet, wantPath: "/agents"},
		{name: "show", args: []string{"agent", "show", "desk"}, wantMethod: http.MethodGet, wantPath: "/agents/desk"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var observed observedRequest
			_, stderr, code := runAgainstServer(t, tt.args, "", func(rw http.ResponseWriter, req *http.Request) {
				observed = observeRequest(t, req)
				writeTestJSON(t, rw, http.StatusOK, map[string]any{"ok": true})
			})
			if code != 0 {
				t.Fatalf("exit code = %d, stderr = %s", code, stderr)
			}
			if observed.Method != tt.wantMethod || observed.Path != tt.wantPath {
				t.Fatalf("request = %s %s, want %s %s", observed.Method, observed.Path, tt.wantMethod, tt.wantPath)
			}
		})
	}
}

func TestApprovalCommandsUseApprovalEndpoints(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantMethod string
		wantPath   string
	}{
		{name: "list", args: []string{"approvals"}, wantMethod: http.MethodGet, wantPath: "/approvals"},
		{name: "approve", args: []string{"approve", "app_123"}, wantMethod: http.MethodPost, wantPath: "/approvals/app_123/approve"},
		{name: "deny", args: []string{"approval", "deny", "app_123"}, wantMethod: http.MethodPost, wantPath: "/approvals/app_123/deny"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var observed observedRequest
			_, stderr, code := runAgainstServer(t, tt.args, "", func(rw http.ResponseWriter, req *http.Request) {
				observed = observeRequest(t, req)
				writeTestJSON(t, rw, http.StatusOK, map[string]any{"ok": true})
			})
			if code != 0 {
				t.Fatalf("exit code = %d, stderr = %s", code, stderr)
			}
			if observed.Method != tt.wantMethod || observed.Path != tt.wantPath {
				t.Fatalf("request = %s %s, want %s %s", observed.Method, observed.Path, tt.wantMethod, tt.wantPath)
			}
		})
	}
}

func TestHealthdErrorsCommandUsesHealthdEndpoint(t *testing.T) {
	var observed observedRequest
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		observed = observeRequest(t, req)
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Body:       io.NopCloser(strings.NewReader(`{"errors":[{"app":"dashboard","message":"boom"}]}`)),
		}, nil
	})}
	var stdout, stderr bytes.Buffer
	code := run([]string{"-healthd-addr", "http://healthd.test", "healthd", "errors", "-limit", "3", "-source", "supervisord", "dashboard"}, strings.NewReader(""), &stdout, &stderr, func(string) string { return "" }, client)
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %s", code, stderr.String())
	}
	if observed.Method != http.MethodGet || observed.Path != "/healthd/errors" || observed.Query != "app=dashboard&limit=3&source=supervisord" {
		t.Fatalf("request = %s %s?%s, want GET /healthd/errors?app=dashboard&limit=3&source=supervisord", observed.Method, observed.Path, observed.Query)
	}
	if !strings.Contains(stdout.String(), `"message": "boom"`) {
		t.Fatalf("stdout = %q, want pretty error JSON", stdout.String())
	}
}

func TestSupervisorCommandsUseSupervisorEndpoint(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantMethod string
		wantPath   string
		wantBody   map[string]any
	}{
		{name: "status", args: []string{"supervisor", "status"}, wantMethod: http.MethodGet, wantPath: "/supervisord"},
		{name: "supervisord alias apps", args: []string{"supervisord", "apps"}, wantMethod: http.MethodGet, wantPath: "/supervisord/apps"},
		{name: "restart supervisor", args: []string{"supervisor", "restart"}, wantMethod: http.MethodPost, wantPath: "/supervisord/restart"},
		{name: "stop supervisor", args: []string{"supervisor", "stop"}, wantMethod: http.MethodPost, wantPath: "/supervisord/stop"},
		{name: "restart app shortcut", args: []string{"supervisor", "restart", "dashboard"}, wantMethod: http.MethodPost, wantPath: "/supervisord/apps/dashboard/restart"},
		{name: "start app shortcut", args: []string{"supervisor", "start", "healthd"}, wantMethod: http.MethodPost, wantPath: "/supervisord/apps/healthd/start"},
		{name: "app restart", args: []string{"supervisor", "app", "restart", "homelabd"}, wantMethod: http.MethodPost, wantPath: "/supervisord/apps/homelabd/restart"},
		{name: "adopt app", args: []string{"supervisor", "app", "adopt", "dashboard", "1234"}, wantMethod: http.MethodPost, wantPath: "/supervisord/apps/dashboard/adopt", wantBody: map[string]any{"pid": float64(1234)}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var observed observedRequest
			stdout, stderr, code := runAgainstSupervisorServer(t, tt.args, func(rw http.ResponseWriter, req *http.Request) {
				observed = observeRequest(t, req)
				writeTestJSON(t, rw, http.StatusOK, map[string]any{"ok": true})
			})
			if code != 0 {
				t.Fatalf("exit code = %d, stderr = %s", code, stderr)
			}
			if observed.Method != tt.wantMethod || observed.Path != tt.wantPath {
				t.Fatalf("request = %s %s, want %s %s", observed.Method, observed.Path, tt.wantMethod, tt.wantPath)
			}
			if !reflect.DeepEqual(observed.Body, tt.wantBody) {
				t.Fatalf("body = %#v, want %#v", observed.Body, tt.wantBody)
			}
			if !strings.Contains(stdout, `"ok": true`) {
				t.Fatalf("stdout did not contain pretty JSON response: %q", stdout)
			}
		})
	}
}

func TestSupervisorAdoptRejectsInvalidPIDBeforeHTTP(t *testing.T) {
	var called bool
	_, stderr, code := runAgainstSupervisorServer(t, []string{"supervisor", "app", "adopt", "dashboard", "nope"}, func(rw http.ResponseWriter, req *http.Request) {
		called = true
	})
	if code != 1 {
		t.Fatalf("exit code = %d, stderr = %s", code, stderr)
	}
	if called {
		t.Fatal("server was called for invalid PID")
	}
	if !strings.Contains(stderr, "pid must be a positive integer") {
		t.Fatalf("stderr = %q", stderr)
	}
}

func TestMessagePrintsPlainReplyAndSendsConfiguredSender(t *testing.T) {
	var observed observedRequest
	stdout, stderr, code := runAgainstServer(t, []string{"-from", "operator", "message", "status"}, "", func(rw http.ResponseWriter, req *http.Request) {
		observed = observeRequest(t, req)
		writeTestJSON(t, rw, http.StatusOK, map[string]any{"reply": "all clear", "source": "program"})
	})
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %s", code, stderr)
	}
	if observed.Method != http.MethodPost || observed.Path != "/message" {
		t.Fatalf("request = %s %s, want POST /message", observed.Method, observed.Path)
	}
	if observed.Body["from"] != "operator" || observed.Body["content"] != "status" {
		t.Fatalf("body = %#v", observed.Body)
	}
	if stdout != "all clear\n" {
		t.Fatalf("stdout = %q, want plain reply", stdout)
	}
}

func TestUXShortcutSendsChatCommand(t *testing.T) {
	var observed observedRequest
	stdout, stderr, code := runAgainstServer(t, []string{"ux", "task_123", "audit", "mobile", "states"}, "", func(rw http.ResponseWriter, req *http.Request) {
		observed = observeRequest(t, req)
		writeTestJSON(t, rw, http.StatusOK, map[string]any{"reply": "queued ux", "source": "program"})
	})
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %s", code, stderr)
	}
	if observed.Method != http.MethodPost || observed.Path != "/message" {
		t.Fatalf("request = %s %s, want POST /message", observed.Method, observed.Path)
	}
	if observed.Body["content"] != "ux task_123 audit mobile states" {
		t.Fatalf("body = %#v", observed.Body)
	}
	if stdout != "queued ux\n" {
		t.Fatalf("stdout = %q, want plain reply", stdout)
	}
}

func TestRememberShortcutSendsChatCommand(t *testing.T) {
	var observed observedRequest
	stdout, stderr, code := runAgainstServer(t, []string{"remember", "prefer", "distilled", "lessons"}, "", func(rw http.ResponseWriter, req *http.Request) {
		observed = observeRequest(t, req)
		writeTestJSON(t, rw, http.StatusOK, map[string]any{"reply": "remembered", "source": "program"})
	})
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %s", code, stderr)
	}
	if observed.Method != http.MethodPost || observed.Path != "/message" {
		t.Fatalf("request = %s %s, want POST /message", observed.Method, observed.Path)
	}
	if observed.Body["content"] != "remember prefer distilled lessons" {
		t.Fatalf("body = %#v", observed.Body)
	}
	if stdout != "remembered\n" {
		t.Fatalf("stdout = %q, want plain reply", stdout)
	}
}

func TestParseWorkflowCreateArgs(t *testing.T) {
	name, goal := parseWorkflowCreateArgs([]string{"Research", "bundle:", "Find", "sources"})
	if name != "Research bundle" || goal != "Find sources" {
		t.Fatalf("parseWorkflowCreateArgs = (%q, %q), want Research bundle / Find sources", name, goal)
	}
	name, goal = parseWorkflowCreateArgs([]string{"Watch", "deploy"})
	if name != "Watch deploy" || goal != "Watch deploy" {
		t.Fatalf("parseWorkflowCreateArgs = (%q, %q), want same name and goal", name, goal)
	}
}

func TestTaskDiffCommandPrintsRawPatch(t *testing.T) {
	var observed observedRequest
	stdout, stderr, code := runAgainstServer(t, []string{"diff", "task_123"}, "", func(rw http.ResponseWriter, req *http.Request) {
		observed = observeRequest(t, req)
		writeTestJSON(t, rw, http.StatusOK, map[string]any{
			"raw_diff": "diff --git a/app.txt b/app.txt\n--- a/app.txt\n+++ b/app.txt\n@@ -1 +1,2 @@\n base\n+changed\n",
			"summary":  map[string]any{"files": 1, "additions": 1, "deletions": 0},
			"files":    []any{},
		})
	})
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %s", code, stderr)
	}
	if observed.Method != http.MethodGet || observed.Path != "/tasks/task_123/diff" {
		t.Fatalf("request = %s %s, want GET /tasks/task_123/diff", observed.Method, observed.Path)
	}
	if !strings.Contains(stdout, "# 1 changed file(s), +1/-0") || !strings.Contains(stdout, "+changed") {
		t.Fatalf("stdout = %q, want summary and raw diff", stdout)
	}
}

func TestTaskDiffCommandPrintsNoDiff(t *testing.T) {
	stdout, stderr, code := runAgainstServer(t, []string{"task", "diff", "task_123"}, "", func(rw http.ResponseWriter, req *http.Request) {
		writeTestJSON(t, rw, http.StatusOK, map[string]any{
			"raw_diff": "",
			"summary":  map[string]any{"files": 0, "additions": 0, "deletions": 0},
			"files":    []any{},
		})
	})
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %s", code, stderr)
	}
	if stdout != "no diff\n" {
		t.Fatalf("stdout = %q, want no diff", stdout)
	}
}

func TestTaskDiffCommandPrintsRemoteDiffGuidance(t *testing.T) {
	stdout, stderr, code := runAgainstServer(t, []string{"task", "diff", "task_123"}, "", func(rw http.ResponseWriter, req *http.Request) {
		writeTestJSON(t, rw, http.StatusOK, map[string]any{
			"base_label": "remote agent",
			"head_label": "desk",
			"raw_diff":   "",
			"summary":    map[string]any{"files": 0, "additions": 0, "deletions": 0},
			"files":      []any{},
		})
	})
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %s", code, stderr)
	}
	if !strings.Contains(stdout, "remote task diff is recorded by the remote agent") {
		t.Fatalf("stdout = %q, want remote diff guidance", stdout)
	}
}

func TestInteractiveShellSendsLinesToMessageEndpoint(t *testing.T) {
	var messages []string
	stdout, stderr, code := runAgainstServer(t, []string{"shell"}, "status\n\napprove app_1\nexit\n", func(rw http.ResponseWriter, req *http.Request) {
		observed := observeRequest(t, req)
		messages = append(messages, observed.Body["content"].(string))
		writeTestJSON(t, rw, http.StatusOK, map[string]any{"reply": "reply: " + observed.Body["content"].(string)})
	})
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %s", code, stderr)
	}
	if !reflect.DeepEqual(messages, []string{"status", "approve app_1"}) {
		t.Fatalf("messages = %#v", messages)
	}
	if !strings.Contains(stdout, "reply: status") || !strings.Contains(stdout, "reply: approve app_1") {
		t.Fatalf("stdout did not include shell replies: %q", stdout)
	}
}

func TestTerminalCommandsUseHTTPAPI(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantMethod string
		wantPath   string
		wantBody   map[string]any
	}{
		{
			name:       "start",
			args:       []string{"terminal", "start", "/tmp/work"},
			wantMethod: http.MethodPost,
			wantPath:   "/terminal/sessions",
			wantBody:   map[string]any{"cwd": "/tmp/work"},
		},
		{
			name:       "send",
			args:       []string{"terminal", "send", "term_1", "ls", "-la"},
			wantMethod: http.MethodPost,
			wantPath:   "/terminal/sessions/term_1/input",
			wantBody:   map[string]any{"data": "ls -la\n"},
		},
		{
			name:       "show",
			args:       []string{"terminal", "show", "term_1"},
			wantMethod: http.MethodGet,
			wantPath:   "/terminal/sessions/term_1",
		},
		{
			name:       "input",
			args:       []string{"terminal", "input", "term_1", "\u0003"},
			wantMethod: http.MethodPost,
			wantPath:   "/terminal/sessions/term_1/input",
			wantBody:   map[string]any{"data": "\u0003"},
		},
		{
			name:       "signal shortcut",
			args:       []string{"terminal", "interrupt", "term_1"},
			wantMethod: http.MethodPost,
			wantPath:   "/terminal/sessions/term_1/signal",
			wantBody:   map[string]any{"signal": "interrupt"},
		},
		{
			name:       "close",
			args:       []string{"terminal", "close", "term_1"},
			wantMethod: http.MethodDelete,
			wantPath:   "/terminal/sessions/term_1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var observed observedRequest
			_, stderr, code := runAgainstServer(t, tt.args, "", func(rw http.ResponseWriter, req *http.Request) {
				observed = observeRequest(t, req)
				writeTestJSON(t, rw, http.StatusOK, map[string]any{"ok": true})
			})
			if code != 0 {
				t.Fatalf("exit code = %d, stderr = %s", code, stderr)
			}
			if observed.Method != tt.wantMethod || observed.Path != tt.wantPath {
				t.Fatalf("request = %s %s, want %s %s", observed.Method, observed.Path, tt.wantMethod, tt.wantPath)
			}
			if !reflect.DeepEqual(observed.Body, tt.wantBody) {
				t.Fatalf("body = %#v, want %#v", observed.Body, tt.wantBody)
			}
		})
	}
}

func TestTerminalStreamPrintsServerSentOutputUntilExit(t *testing.T) {
	stdout, stderr, code := runAgainstServer(t, []string{"terminal", "stream", "term_1"}, "", func(rw http.ResponseWriter, req *http.Request) {
		if req.URL.Path != "/terminal/sessions/term_1/events" {
			t.Fatalf("path = %s", req.URL.Path)
		}
		rw.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(rw, "event: ready\ndata: {}\n\n")
		fmt.Fprintf(rw, "event: output\ndata: {\"type\":\"output\",\"data\":\"hello\\n\"}\n\n")
		fmt.Fprintf(rw, "event: exit\ndata: {\"type\":\"exit\",\"code\":0}\n\n")
	})
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %s", code, stderr)
	}
	if !strings.Contains(stdout, "hello\n") || !strings.Contains(stdout, "[terminal exited with code 0]") {
		t.Fatalf("stdout = %q", stdout)
	}
}

func TestFullWorkflowIntegration(t *testing.T) {
	var sequence []string
	handler := func(rw http.ResponseWriter, req *http.Request) {
		sequence = append(sequence, req.Method+" "+req.URL.RequestURI())
		switch req.URL.Path {
		case "/tasks":
			if req.Method == http.MethodPost {
				writeTestJSON(t, rw, http.StatusCreated, map[string]any{"reply": "created task_1"})
				return
			}
			writeTestJSON(t, rw, http.StatusOK, map[string]any{"tasks": []map[string]any{{"id": "task_1", "status": "queued"}}})
		case "/tasks/task_1":
			writeTestJSON(t, rw, http.StatusOK, map[string]any{"id": "task_1", "status": "queued"})
		case "/tasks/task_1/run", "/tasks/task_1/review", "/tasks/task_1/accept", "/tasks/task_1/restart", "/tasks/task_1/reopen", "/tasks/task_1/cancel", "/tasks/task_1/delete":
			writeTestJSON(t, rw, http.StatusOK, map[string]any{"reply": "ok"})
		case "/tasks/task_1/runs":
			writeTestJSON(t, rw, http.StatusOK, map[string]any{"runs": []any{}})
		case "/agents":
			writeTestJSON(t, rw, http.StatusOK, map[string]any{"agents": []map[string]any{{"id": "desk"}}})
		case "/agents/desk":
			writeTestJSON(t, rw, http.StatusOK, map[string]any{"id": "desk"})
		case "/tasks/task_1/diff":
			writeTestJSON(t, rw, http.StatusOK, map[string]any{
				"raw_diff": "",
				"summary":  map[string]any{"files": 0, "additions": 0, "deletions": 0},
				"files":    []any{},
			})
		case "/approvals":
			writeTestJSON(t, rw, http.StatusOK, map[string]any{"approvals": []map[string]any{{"id": "app_1"}}})
		case "/approvals/app_1/approve":
			writeTestJSON(t, rw, http.StatusOK, map[string]any{"reply": "approved"})
		case "/events":
			writeTestJSON(t, rw, http.StatusOK, map[string]any{"events": []any{}})
		default:
			t.Fatalf("unexpected request %s %s", req.Method, req.URL.RequestURI())
		}
	}
	skipIfNoLoopback(t)
	server := httptest.NewServer(http.HandlerFunc(handler))
	defer server.Close()

	commands := [][]string{
		{"task", "new", "ship", "it"},
		{"tasks"},
		{"show", "task_1"},
		{"runs", "task_1"},
		{"diff", "task_1"},
		{"run", "task_1"},
		{"review", "task_1"},
		{"accept", "task_1"},
		{"task", "restart", "task_1"},
		{"reopen", "task_1", "needs", "work"},
		{"cancel", "task_1"},
		{"delete", "task_1"},
		{"agent", "list"},
		{"agent", "show", "desk"},
		{"approvals"},
		{"approve", "app_1"},
		{"events", "-limit", "1"},
	}
	for _, command := range commands {
		var stdout, stderr bytes.Buffer
		args := append([]string{"-addr", server.URL}, command...)
		if code := run(args, strings.NewReader(""), &stdout, &stderr, func(string) string { return "" }, server.Client()); code != 0 {
			t.Fatalf("command %q failed with code %d: %s", strings.Join(command, " "), code, stderr.String())
		}
	}
	want := []string{
		"POST /tasks",
		"GET /tasks",
		"GET /tasks/task_1",
		"GET /tasks/task_1/runs",
		"GET /tasks/task_1/diff",
		"POST /tasks/task_1/run",
		"POST /tasks/task_1/review",
		"POST /tasks/task_1/accept",
		"POST /tasks/task_1/restart",
		"POST /tasks/task_1/reopen",
		"POST /tasks/task_1/cancel",
		"POST /tasks/task_1/delete",
		"GET /agents",
		"GET /agents/desk",
		"GET /approvals",
		"POST /approvals/app_1/approve",
		"GET /events?limit=1",
	}
	if !reflect.DeepEqual(sequence, want) {
		t.Fatalf("sequence = %#v, want %#v", sequence, want)
	}
}

func TestInvalidEventsLimitFailsBeforeHTTP(t *testing.T) {
	var called bool
	_, stderr, code := runAgainstServer(t, []string{"events", "-limit", "-1"}, "", func(rw http.ResponseWriter, req *http.Request) {
		called = true
	})
	if code != 1 {
		t.Fatalf("exit code = %d, stderr = %s", code, stderr)
	}
	if called {
		t.Fatal("server was called for invalid event limit")
	}
	if !strings.Contains(stderr, "limit must be a positive integer") {
		t.Fatalf("stderr = %q", stderr)
	}
}

func runAgainstServer(t *testing.T, args []string, stdin string, handler http.HandlerFunc) (string, string, int) {
	t.Helper()
	skipIfNoLoopback(t)
	server := httptest.NewServer(handler)
	defer server.Close()
	var stdout, stderr bytes.Buffer
	code := run(append([]string{"-addr", server.URL}, args...), strings.NewReader(stdin), &stdout, &stderr, func(string) string { return "" }, server.Client())
	return stdout.String(), stderr.String(), code
}

func runAgainstSupervisorServer(t *testing.T, args []string, handler http.HandlerFunc) (string, string, int) {
	t.Helper()
	skipIfNoLoopback(t)
	server := httptest.NewServer(handler)
	defer server.Close()
	var stdout, stderr bytes.Buffer
	code := run(append([]string{"-supervisord-addr", server.URL}, args...), strings.NewReader(""), &stdout, &stderr, func(string) string { return "" }, server.Client())
	return stdout.String(), stderr.String(), code
}

func skipIfNoLoopback(t *testing.T) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("loopback listener unavailable in this test environment: %v", err)
	}
	_ = ln.Close()
}

func observeRequest(t *testing.T, req *http.Request) observedRequest {
	t.Helper()
	var body map[string]any
	if req.Body != nil {
		raw, err := io.ReadAll(req.Body)
		if err != nil {
			t.Fatal(err)
		}
		if len(bytes.TrimSpace(raw)) > 0 {
			if err := json.Unmarshal(raw, &body); err != nil {
				t.Fatalf("decode request body: %v: %s", err, string(raw))
			}
		}
	}
	return observedRequest{
		Method: req.Method,
		Path:   req.URL.Path,
		Query:  req.URL.RawQuery,
		Body:   body,
	}
}

func writeTestJSON(t *testing.T, rw http.ResponseWriter, status int, value any) {
	t.Helper()
	rw.Header().Set("Content-Type", "application/json")
	rw.WriteHeader(status)
	if err := json.NewEncoder(rw).Encode(value); err != nil {
		t.Fatal(err)
	}
}
