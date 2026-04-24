package test

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/andrewneudegg/lab/pkg/tool"
)

type Base struct {
	Timeout time.Duration
}

func Register(reg *tool.Registry, base Base) error {
	for _, t := range []tool.Tool{RunTool{timeout: base.Timeout}, GoTestTool{timeout: base.Timeout}, GoBuildTool{timeout: base.Timeout}, GoFmtTool{timeout: base.Timeout}} {
		if err := reg.Register(t); err != nil {
			return err
		}
	}
	return nil
}

func schema(v string) json.RawMessage { return json.RawMessage(v) }

func run(ctx context.Context, timeout time.Duration, dir string, name string, args ...string) (json.RawMessage, error) {
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	childCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(childCtx, name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	result := map[string]any{"command": strings.Join(append([]string{name}, args...), " "), "output": string(out)}
	if childCtx.Err() == context.DeadlineExceeded {
		result["timed_out"] = true
	}
	if err != nil {
		b, _ := json.Marshal(result)
		return b, fmt.Errorf("%s: %w", name, err)
	}
	return json.Marshal(result)
}

type RunTool struct{ timeout time.Duration }

func (RunTool) Name() string        { return "test.run" }
func (RunTool) Description() string { return "Run the default Go test suite in a workspace." }
func (RunTool) Schema() json.RawMessage {
	return schema(`{"type":"object","required":["dir"],"properties":{"dir":{"type":"string"}}}`)
}
func (RunTool) Risk() tool.RiskLevel { return tool.RiskLow }
func (t RunTool) Run(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
	var req struct {
		Dir string `json:"dir"`
	}
	_ = json.Unmarshal(input, &req)
	return run(ctx, t.timeout, req.Dir, "go", "test", "./...")
}

type GoTestTool struct{ timeout time.Duration }

func (GoTestTool) Name() string        { return "go.test" }
func (GoTestTool) Description() string { return "Run go test ./... in a workspace." }
func (GoTestTool) Schema() json.RawMessage {
	return schema(`{"type":"object","required":["dir"],"properties":{"dir":{"type":"string"}}}`)
}
func (GoTestTool) Risk() tool.RiskLevel { return tool.RiskLow }
func (t GoTestTool) Run(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
	var req struct {
		Dir string `json:"dir"`
	}
	_ = json.Unmarshal(input, &req)
	return run(ctx, t.timeout, req.Dir, "go", "test", "./...")
}

type GoBuildTool struct{ timeout time.Duration }

func (GoBuildTool) Name() string        { return "go.build" }
func (GoBuildTool) Description() string { return "Run go build ./... in a workspace." }
func (GoBuildTool) Schema() json.RawMessage {
	return schema(`{"type":"object","required":["dir"],"properties":{"dir":{"type":"string"}}}`)
}
func (GoBuildTool) Risk() tool.RiskLevel { return tool.RiskLow }
func (t GoBuildTool) Run(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
	var req struct {
		Dir string `json:"dir"`
	}
	_ = json.Unmarshal(input, &req)
	return run(ctx, t.timeout, req.Dir, "go", "build", "./...")
}

type GoFmtTool struct{ timeout time.Duration }

func (GoFmtTool) Name() string { return "go.fmt" }
func (GoFmtTool) Description() string {
	return "Run gofmt on Go files in a workspace through go fmt ./..."
}
func (GoFmtTool) Schema() json.RawMessage {
	return schema(`{"type":"object","required":["dir"],"properties":{"dir":{"type":"string"}}}`)
}
func (GoFmtTool) Risk() tool.RiskLevel { return tool.RiskLow }
func (t GoFmtTool) Run(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
	var req struct {
		Dir string `json:"dir"`
	}
	_ = json.Unmarshal(input, &req)
	return run(ctx, t.timeout, req.Dir, "go", "fmt", "./...")
}
