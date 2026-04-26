package externalagent

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/andrewneudegg/lab/pkg/config"
)

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
