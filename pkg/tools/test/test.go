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
	Timeout  time.Duration
	RepoRoot string
}

func Register(reg *tool.Registry, base Base) error {
	for _, t := range []tool.Tool{
		RunTool{timeout: base.Timeout},
		GoTestTool{timeout: base.Timeout},
		GoBuildTool{timeout: base.Timeout},
		GoFmtTool{timeout: base.Timeout},
		BunCheckTool{timeout: base.Timeout, repoRoot: base.RepoRoot},
		BunBuildTool{timeout: base.Timeout, repoRoot: base.RepoRoot},
		BunTestTool{timeout: base.Timeout, repoRoot: base.RepoRoot},
		BunUATTasksTool{timeout: base.Timeout, repoRoot: base.RepoRoot},
	} {
		if err := reg.Register(t); err != nil {
			return err
		}
	}
	return nil
}

func runBunScript(ctx context.Context, timeout time.Duration, repoRoot, dir, script string) (json.RawMessage, error) {
	if _, err := exec.LookPath("bun"); err == nil {
		if raw, err := run(ctx, timeout, dir, "bun", "install"); err != nil {
			return raw, err
		}
		return run(ctx, timeout, dir, "bun", "run", script)
	}
	if repoRoot == "" {
		repoRoot = dir
	}
	command := fmt.Sprintf("cd \"$1\" && bun install && bun run %s", script)
	return run(ctx, timeout, repoRoot, "nix", "develop", repoRoot, "-c", "bash", "-lc", command, "bun-tool", dir)
}

type BunCheckTool struct {
	timeout  time.Duration
	repoRoot string
}

func (BunCheckTool) Name() string        { return "bun.check" }
func (BunCheckTool) Description() string { return "Run bun install and bun run check in a workspace." }
func (BunCheckTool) Schema() json.RawMessage {
	return schema(`{"type":"object","required":["dir"],"properties":{"dir":{"type":"string"}}}`)
}
func (BunCheckTool) Risk() tool.RiskLevel { return tool.RiskLow }
func (t BunCheckTool) Run(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
	var req struct {
		Dir string `json:"dir"`
	}
	_ = json.Unmarshal(input, &req)
	return runBunScript(ctx, t.timeout, t.repoRoot, req.Dir, "check")
}

type BunBuildTool struct {
	timeout  time.Duration
	repoRoot string
}

func (BunBuildTool) Name() string        { return "bun.build" }
func (BunBuildTool) Description() string { return "Run bun install and bun run build in a workspace." }
func (BunBuildTool) Schema() json.RawMessage {
	return schema(`{"type":"object","required":["dir"],"properties":{"dir":{"type":"string"}}}`)
}
func (BunBuildTool) Risk() tool.RiskLevel { return tool.RiskLow }
func (t BunBuildTool) Run(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
	var req struct {
		Dir string `json:"dir"`
	}
	_ = json.Unmarshal(input, &req)
	return runBunScript(ctx, t.timeout, t.repoRoot, req.Dir, "build")
}

type BunTestTool struct {
	timeout  time.Duration
	repoRoot string
}

func (BunTestTool) Name() string        { return "bun.test" }
func (BunTestTool) Description() string { return "Run bun install and bun run test in a workspace." }
func (BunTestTool) Schema() json.RawMessage {
	return schema(`{"type":"object","required":["dir"],"properties":{"dir":{"type":"string"}}}`)
}
func (BunTestTool) Risk() tool.RiskLevel { return tool.RiskLow }
func (t BunTestTool) Run(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
	var req struct {
		Dir string `json:"dir"`
	}
	_ = json.Unmarshal(input, &req)
	return runBunScript(ctx, t.timeout, t.repoRoot, req.Dir, "test")
}

type BunUATTasksTool struct {
	timeout  time.Duration
	repoRoot string
}

func (BunUATTasksTool) Name() string { return "bun.uat.tasks" }
func (BunUATTasksTool) Description() string {
	return "Run the isolated dashboard task-page Playwright UAT in a workspace."
}
func (BunUATTasksTool) Schema() json.RawMessage {
	return schema(`{"type":"object","required":["dir"],"properties":{"dir":{"type":"string"}}}`)
}
func (BunUATTasksTool) Risk() tool.RiskLevel { return tool.RiskLow }
func (t BunUATTasksTool) Run(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
	var req struct {
		Dir string `json:"dir"`
	}
	_ = json.Unmarshal(input, &req)
	timeout := t.timeout
	if timeout < 2*time.Minute {
		timeout = 2 * time.Minute
	}
	return runBunScript(ctx, timeout, t.repoRoot, req.Dir, "uat:tasks")
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
