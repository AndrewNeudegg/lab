package test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/andrewneudegg/lab/pkg/tool"
)

var goPackagePatterns = []string{"./cmd/...", "./pkg/...", "./constraints"}

const (
	minBunScriptTimeout = 3 * time.Minute
	minBunBuildTime     = 6 * time.Minute
	minBunUATTasksTime  = 5 * time.Minute
	minBunUATSiteTime   = 10 * time.Minute
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
		BunUATSiteTool{timeout: base.Timeout, repoRoot: base.RepoRoot},
	} {
		if err := reg.Register(t); err != nil {
			return err
		}
	}
	return nil
}

func runBunScript(ctx context.Context, timeout time.Duration, repoRoot, dir, script string) (json.RawMessage, error) {
	return runBunScriptWithOptions(ctx, bunScriptTimeout(timeout, script), repoRoot, dir, script)
}

func runBrowserUATScript(ctx context.Context, timeout time.Duration, repoRoot, dir, script string) (json.RawMessage, error) {
	return runBunScriptWithOptions(ctx, timeout, repoRoot, dir, script)
}

func runBunScriptWithOptions(ctx context.Context, timeout time.Duration, repoRoot, dir, script string) (json.RawMessage, error) {
	if nixRoot := bunNixRoot(repoRoot, dir); nixRoot != "" {
		if _, err := exec.LookPath("nix"); err == nil {
			return runBunScriptInNix(ctx, timeout, nixRoot, dir, script)
		}
	}
	if _, err := exec.LookPath("bun"); err == nil {
		if raw, err := run(ctx, timeout, dir, "bun", "install"); err != nil {
			return raw, err
		}
		return run(ctx, timeout, dir, "bun", "run", script)
	}
	return runBunScriptInNix(ctx, timeout, repoRoot, dir, script)
}

func bunScriptTimeout(timeout time.Duration, script string) time.Duration {
	minimum := minBunScriptTimeout
	if script == "build" {
		minimum = minBunBuildTime
	}
	return atLeastTimeout(timeout, minimum)
}

func atLeastTimeout(timeout, minimum time.Duration) time.Duration {
	if timeout < minimum {
		return minimum
	}
	return timeout
}

func bunNixRoot(repoRoot, dir string) string {
	if root := findFlakeRoot(dir); root != "" {
		return root
	}
	if hasFlake(repoRoot) {
		return repoRoot
	}
	return ""
}

func hasFlake(dir string) bool {
	if dir == "" {
		return false
	}
	info, err := os.Stat(filepath.Join(dir, "flake.nix"))
	return err == nil && !info.IsDir()
}

func findFlakeRoot(start string) string {
	if start == "" {
		return ""
	}
	dir, err := filepath.Abs(start)
	if err != nil {
		dir = filepath.Clean(start)
	}
	for {
		if hasFlake(dir) {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

func runBunScriptInNix(ctx context.Context, timeout time.Duration, repoRoot, dir, script string) (json.RawMessage, error) {
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
	timeout = atLeastTimeout(timeout, minBunUATTasksTime)
	return runBrowserUATScript(ctx, timeout, t.repoRoot, req.Dir, "uat:tasks")
}

type BunUATSiteTool struct {
	timeout  time.Duration
	repoRoot string
}

func (BunUATSiteTool) Name() string { return "bun.uat.site" }
func (BunUATSiteTool) Description() string {
	return "Run the isolated dashboard site-wide Playwright UAT in a workspace."
}
func (BunUATSiteTool) Schema() json.RawMessage {
	return schema(`{"type":"object","required":["dir"],"properties":{"dir":{"type":"string"}}}`)
}
func (BunUATSiteTool) Risk() tool.RiskLevel { return tool.RiskLow }
func (t BunUATSiteTool) Run(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
	var req struct {
		Dir string `json:"dir"`
	}
	_ = json.Unmarshal(input, &req)
	timeout := t.timeout
	timeout = atLeastTimeout(timeout, minBunUATSiteTime)
	return runBrowserUATScript(ctx, timeout, t.repoRoot, req.Dir, "uat:site")
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
	cmd.Env = os.Environ()
	if name == "go" && os.Getenv("GOCACHE") == "" {
		cacheDir := "/tmp/homelab-go-build-cache"
		_ = os.MkdirAll(cacheDir, 0o755)
		cmd.Env = append(cmd.Env, "GOCACHE="+cacheDir)
	}
	if name == "go" && os.Getenv("GOMODCACHE") == "" {
		modCacheDir := "/tmp/homelab-go-mod-cache"
		_ = os.MkdirAll(modCacheDir, 0o755)
		cmd.Env = append(cmd.Env, "GOMODCACHE="+modCacheDir)
	}
	if name == "nix" && os.Getenv("XDG_CACHE_HOME") == "" {
		cacheDir := "/tmp/homelab-nix-cache"
		_ = os.MkdirAll(cacheDir, 0o755)
		cmd.Env = append(cmd.Env, "XDG_CACHE_HOME="+cacheDir)
	}
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
	return run(ctx, t.timeout, req.Dir, "go", append([]string{"test"}, goPackagePatterns...)...)
}

type GoTestTool struct{ timeout time.Duration }

func (GoTestTool) Name() string { return "go.test" }
func (GoTestTool) Description() string {
	return "Run the repository Go test suite in a workspace, excluding ignored runtime data directories."
}
func (GoTestTool) Schema() json.RawMessage {
	return schema(`{"type":"object","required":["dir"],"properties":{"dir":{"type":"string"}}}`)
}
func (GoTestTool) Risk() tool.RiskLevel { return tool.RiskLow }
func (t GoTestTool) Run(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
	var req struct {
		Dir string `json:"dir"`
	}
	_ = json.Unmarshal(input, &req)
	return run(ctx, t.timeout, req.Dir, "go", append([]string{"test"}, goPackagePatterns...)...)
}

type GoBuildTool struct{ timeout time.Duration }

func (GoBuildTool) Name() string { return "go.build" }
func (GoBuildTool) Description() string {
	return "Run the repository Go build in a workspace, excluding ignored runtime data directories."
}
func (GoBuildTool) Schema() json.RawMessage {
	return schema(`{"type":"object","required":["dir"],"properties":{"dir":{"type":"string"}}}`)
}
func (GoBuildTool) Risk() tool.RiskLevel { return tool.RiskLow }
func (t GoBuildTool) Run(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
	var req struct {
		Dir string `json:"dir"`
	}
	_ = json.Unmarshal(input, &req)
	return run(ctx, t.timeout, req.Dir, "go", append([]string{"build"}, goPackagePatterns...)...)
}

type GoFmtTool struct{ timeout time.Duration }

func (GoFmtTool) Name() string { return "go.fmt" }
func (GoFmtTool) Description() string {
	return "Run gofmt on repository Go packages in a workspace, excluding ignored runtime data directories."
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
	return run(ctx, t.timeout, req.Dir, "go", append([]string{"fmt"}, goPackagePatterns...)...)
}
