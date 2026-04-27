package externalagent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/andrewneudegg/lab/pkg/config"
)

func TestRunnerExecutesInRequestedWorkspaceWithTaskEnvironment(t *testing.T) {
	workspace := t.TempDir()
	runner := NewRunner(map[string]config.ExternalAgentConfig{
		"codex": {
			Enabled: true,
			Command: "sh",
			Args: []string{
				"-c",
				"pwd; printf '%s\\n' \"$HOMELABD_TASK_ID\" \"$HOMELABD_WORKSPACE\" \"$HOMELABD_BACKEND\"",
			},
			TimeoutSeconds: 5,
		},
	})

	result, err := runner.Run(context.Background(), RunRequest{
		Backend:     "codex",
		TaskID:      "task_1",
		Workspace:   workspace,
		Instruction: "do work",
	})
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(result.Output), "\n")
	if len(lines) != 4 {
		t.Fatalf("output = %q, want pwd and three env lines", result.Output)
	}
	gotWorkspace, err := filepath.EvalSymlinks(lines[0])
	if err != nil {
		t.Fatal(err)
	}
	wantWorkspace, err := filepath.EvalSymlinks(workspace)
	if err != nil {
		t.Fatal(err)
	}
	if gotWorkspace != wantWorkspace {
		t.Fatalf("pwd = %q, want workspace %q", gotWorkspace, wantWorkspace)
	}
	if lines[1] != "task_1" || lines[2] != workspace || lines[3] != "codex" {
		t.Fatalf("env lines = %#v", lines[1:])
	}
}

func TestRunnerRejectsMissingWorkspaceBeforeExecuting(t *testing.T) {
	runner := NewRunner(map[string]config.ExternalAgentConfig{
		"codex": {Enabled: true, Command: "sh", Args: []string{"-c", "exit 99"}},
	})

	if _, err := runner.Run(context.Background(), RunRequest{
		Backend:     "codex",
		TaskID:      "task_1",
		Instruction: "do work",
	}); err == nil || !strings.Contains(err.Error(), "workspace is required") {
		t.Fatalf("error = %v, want workspace required", err)
	}
}

func TestRunnerDefaultsUnsetTimeoutToFiveHours(t *testing.T) {
	got := timeoutForConfig(config.ExternalAgentConfig{})
	if got != 5*time.Hour {
		t.Fatalf("timeout = %s, want 5h", got)
	}
}

func TestRunnerReturnsProcessFailureAndOutput(t *testing.T) {
	workspace := t.TempDir()
	runner := NewRunner(map[string]config.ExternalAgentConfig{
		"codex": {Enabled: true, Command: "sh", Args: []string{"-c", "echo failed; exit 7"}},
	})

	result, err := runner.Run(context.Background(), RunRequest{
		Backend:     "codex",
		TaskID:      "task_1",
		Workspace:   workspace,
		Instruction: "do work",
	})
	if err == nil {
		t.Fatal("Run succeeded, want process error")
	}
	if !strings.Contains(result.Output, "failed") || result.Error == "" {
		t.Fatalf("result = %#v, want output and error", result)
	}
	if _, statErr := os.Stat(workspace); statErr != nil {
		t.Fatalf("workspace should remain present: %v", statErr)
	}
}

func TestRunnerStreamsOutputChunks(t *testing.T) {
	var mu sync.Mutex
	var chunks []OutputChunk
	runner := NewRunner(map[string]config.ExternalAgentConfig{
		"test": {
			Enabled: true,
			Command: "sh",
			Args: []string{
				"-c",
				"printf 'out-one\\n'; printf 'err-one\\n' >&2; printf '%s\\n' \"$HOMELABD_EXTERNAL_RUN_ID\"",
			},
			TimeoutSeconds: 5,
		},
	}, WithOutputHandler(func(_ context.Context, chunk OutputChunk) {
		mu.Lock()
		defer mu.Unlock()
		chunks = append(chunks, chunk)
	}))

	result, err := runner.Run(context.Background(), RunRequest{
		Backend:     "test",
		RunID:       "delegate_test",
		TaskID:      "task_test",
		Workspace:   t.TempDir(),
		Instruction: "run",
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.ID != "delegate_test" {
		t.Fatalf("result ID = %q, want delegate_test", result.ID)
	}
	for _, want := range []string{"out-one", "err-one", "delegate_test"} {
		if !strings.Contains(result.Output, want) {
			t.Fatalf("result output %q missing %q", result.Output, want)
		}
	}

	mu.Lock()
	defer mu.Unlock()
	if len(chunks) < 2 {
		t.Fatalf("chunks = %d, want at least 2", len(chunks))
	}
	streams := map[string]bool{}
	for i, chunk := range chunks {
		if chunk.RunID != "delegate_test" {
			t.Fatalf("chunk %d run ID = %q, want delegate_test", i, chunk.RunID)
		}
		if chunk.TaskID != "task_test" {
			t.Fatalf("chunk %d task ID = %q, want task_test", i, chunk.TaskID)
		}
		if chunk.Sequence <= 0 {
			t.Fatalf("chunk %d sequence = %d, want positive", i, chunk.Sequence)
		}
		if chunk.Time.IsZero() {
			t.Fatalf("chunk %d time is zero", i)
		}
		streams[chunk.Stream] = true
	}
	if !streams["stdout"] || !streams["stderr"] {
		t.Fatalf("streams = %v, want stdout and stderr", streams)
	}
}

func TestRunnerCancelsRunningProcess(t *testing.T) {
	started := make(chan struct{})
	var once sync.Once
	runner := NewRunner(map[string]config.ExternalAgentConfig{
		"test": {
			Enabled: true,
			Command: "sh",
			Args: []string{
				"-c",
				"printf 'ready\\n'; exec sleep 30",
			},
			TimeoutSeconds: 60,
		},
	}, WithOutputHandler(func(_ context.Context, chunk OutputChunk) {
		if strings.Contains(chunk.Text, "ready") {
			once.Do(func() { close(started) })
		}
	}))

	ctx, cancel := context.WithCancel(context.Background())
	workspace := t.TempDir()
	done := make(chan struct {
		result RunResult
		err    error
	}, 1)
	go func() {
		result, err := runner.Run(ctx, RunRequest{
			Backend:     "test",
			RunID:       "delegate_cancel",
			TaskID:      "task_cancel",
			Workspace:   workspace,
			Instruction: "run",
		})
		done <- struct {
			result RunResult
			err    error
		}{result: result, err: err}
	}()

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("runner did not stream startup output before cancellation")
	}
	cancel()

	select {
	case got := <-done:
		if got.err == nil {
			t.Fatal("Run() error = nil, want cancellation error")
		}
		if got.result.ID != "delegate_cancel" {
			t.Fatalf("result ID = %q, want delegate_cancel", got.result.ID)
		}
		if !strings.Contains(got.result.Output, "ready") {
			t.Fatalf("result output = %q, want streamed ready output", got.result.Output)
		}
		if got.result.Duration > 10*time.Second {
			t.Fatalf("duration = %s, process was not cancelled promptly", got.result.Duration)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("runner did not return after context cancellation")
	}
}
