package shell

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/andrewneudegg/lab/pkg/tool"
)

type Base struct {
	Timeout time.Duration
}

func Register(reg *tool.Registry, base Base) error {
	for _, t := range []tool.Tool{LimitedTool{timeout: base.Timeout}, ChainTool{timeout: base.Timeout}, ApprovedTool{timeout: base.Timeout}} {
		if err := reg.Register(t); err != nil {
			return err
		}
	}
	return nil
}

func schema(v string) json.RawMessage { return json.RawMessage(v) }

type LimitedTool struct{ timeout time.Duration }

func (LimitedTool) Name() string { return "shell.run_limited" }
func (LimitedTool) Description() string {
	return "Run an allowlisted command without shell expansion; destructive commands require policy approval."
}
func (LimitedTool) Schema() json.RawMessage {
	return schema(`{"type":"object","required":["dir","command"],"properties":{"dir":{"type":"string"},"command":{"type":"array","items":{"type":"string"}}}}`)
}
func (LimitedTool) Risk() tool.RiskLevel { return tool.RiskLow }
func (LimitedTool) RiskFor(input json.RawMessage) tool.RiskLevel {
	return riskForInput(input, tool.RiskLow)
}
func (t LimitedTool) Run(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
	var req struct {
		Dir     string   `json:"dir"`
		Command []string `json:"command"`
	}
	if err := json.Unmarshal(input, &req); err != nil {
		return nil, err
	}
	if !allowed(req.Command) {
		return nil, fmt.Errorf("command not allowlisted: %s", strings.Join(req.Command, " "))
	}
	return run(ctx, t.timeout, req.Dir, req.Command)
}

type ChainTool struct{ timeout time.Duration }

func (ChainTool) Name() string { return "shell.run_chain" }
func (ChainTool) Description() string {
	return "Run allowlisted command arrays in order, stopping at the first failure; destructive commands require policy approval."
}
func (ChainTool) Schema() json.RawMessage {
	return schema(`{"type":"object","required":["dir","commands"],"properties":{"dir":{"type":"string"},"commands":{"type":"array","minItems":1,"maxItems":20,"items":{"type":"array","minItems":1,"items":{"type":"string"}}},"target":{"type":"string"}}}`)
}
func (ChainTool) Risk() tool.RiskLevel { return tool.RiskLow }
func (ChainTool) RiskFor(input json.RawMessage) tool.RiskLevel {
	return riskForChainInput(input, tool.RiskLow)
}
func (t ChainTool) Run(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
	var req struct {
		Dir      string     `json:"dir"`
		Commands [][]string `json:"commands"`
	}
	if err := json.Unmarshal(input, &req); err != nil {
		return nil, err
	}
	if len(req.Commands) == 0 {
		return nil, fmt.Errorf("commands is required")
	}
	for i, command := range req.Commands {
		if !allowed(command) {
			return nil, fmt.Errorf("command %d not allowlisted: %s", i+1, strings.Join(command, " "))
		}
	}
	childCtx, cancel := commandContext(ctx, t.timeout)
	defer cancel()
	results := make([]commandResult, 0, len(req.Commands))
	for i, command := range req.Commands {
		result, err := runCommand(childCtx, req.Dir, command)
		results = append(results, result)
		if err != nil {
			b, _ := json.Marshal(map[string]any{
				"commands":       commandStrings(req.Commands),
				"failed_command": result.Command,
				"failed_index":   i,
				"results":        results,
			})
			return b, err
		}
	}
	return json.Marshal(map[string]any{"commands": commandStrings(req.Commands), "results": results})
}

type ApprovedTool struct{ timeout time.Duration }

func (ApprovedTool) Name() string { return "shell.run_approved" }
func (ApprovedTool) Description() string {
	return "Run a command without shell expansion after policy approval when command risk requires it."
}
func (ApprovedTool) Schema() json.RawMessage {
	return schema(`{"type":"object","required":["dir","command","target"],"properties":{"dir":{"type":"string"},"command":{"type":"array","items":{"type":"string"}},"target":{"type":"string"}}}`)
}
func (ApprovedTool) Risk() tool.RiskLevel { return tool.RiskHigh }
func (ApprovedTool) RiskFor(input json.RawMessage) tool.RiskLevel {
	return riskForInput(input, tool.RiskHigh)
}
func (t ApprovedTool) Run(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
	var req struct {
		Dir     string   `json:"dir"`
		Command []string `json:"command"`
	}
	if err := json.Unmarshal(input, &req); err != nil {
		return nil, err
	}
	return run(ctx, t.timeout, req.Dir, req.Command)
}

func riskForInput(input json.RawMessage, fallback tool.RiskLevel) tool.RiskLevel {
	var req struct {
		Command []string `json:"command"`
	}
	if err := json.Unmarshal(input, &req); err != nil {
		return fallback
	}
	if risk := commandRisk(req.Command); risk != "" {
		return risk
	}
	return fallback
}

func riskForChainInput(input json.RawMessage, fallback tool.RiskLevel) tool.RiskLevel {
	var req struct {
		Commands [][]string `json:"commands"`
	}
	if err := json.Unmarshal(input, &req); err != nil {
		return fallback
	}
	risk := tool.RiskLow
	for _, command := range req.Commands {
		switch commandRisk(command) {
		case tool.RiskHigh:
			return tool.RiskHigh
		case tool.RiskLow:
			continue
		default:
			risk = fallback
		}
	}
	return risk
}

func commandRisk(command []string) tool.RiskLevel {
	if len(command) == 0 {
		return ""
	}
	if len(command) >= 2 && command[0] == "git" {
		switch command[1] {
		case "clean", "reset", "restore", "rm":
			return tool.RiskHigh
		case "checkout":
			if len(command) >= 3 && command[2] == "--" {
				return tool.RiskHigh
			}
		}
	}
	switch command[0] {
	case "rm", "rmdir", "mv", "cp":
		return tool.RiskHigh
	}
	if lowRiskCommand(command) {
		return tool.RiskLow
	}
	return ""
}

func allowed(command []string) bool {
	if len(command) == 0 {
		return false
	}
	if commandRisk(command) == tool.RiskHigh {
		return allowDestructive(command)
	}
	return lowRiskCommand(command)
}

func lowRiskCommand(command []string) bool {
	if len(command) == 0 {
		return false
	}
	if len(command) > 3 && command[0] == "nix" && command[1] == "develop" && command[2] == "-c" {
		return lowRiskCommand(command[3:])
	}
	if lowRiskBunCommand(command) {
		return true
	}
	joined := strings.Join(command, " ")
	switch joined {
	case "go test ./...",
		"go test ./cmd/... ./pkg/... ./constraints",
		"go build ./...",
		"go build ./cmd/... ./pkg/... ./constraints",
		"go fmt ./...",
		"go fmt ./cmd/... ./pkg/... ./constraints",
		"make test",
		"make build",
		"make fmt",
		"git status", "git diff", "git log":
		return true
	}
	return readOnlyCommand(command)
}

func lowRiskBunCommand(command []string) bool {
	if len(command) != 5 {
		return false
	}
	if command[0] != "bun" || command[1] != "run" || command[2] != "--cwd" {
		return false
	}
	if command[3] != "web" {
		return false
	}
	switch command[4] {
	case "check", "build", "test", "browser:preflight", "uat:ui", "uat:tasks", "uat:site", "uat:docs", "e2e":
		return true
	default:
		return false
	}
}

func readOnlyCommand(command []string) bool {
	if len(command) == 0 {
		return false
	}
	switch command[0] {
	case "pwd":
		return len(command) == 1
	case "cat":
		return len(command) == 2 && safeReadPathArg(command[1])
	case "ls", "wc", "head", "tail", "grep":
		return safeReadOnlyArgs(command[1:])
	case "rg":
		return safeRipgrepArgs(command[1:])
	case "find":
		return safeFindArgs(command[1:])
	default:
		return false
	}
}

func safeReadOnlyArgs(args []string) bool {
	for _, arg := range args {
		if !safeReadPathArg(arg) {
			return false
		}
	}
	return true
}

func safeReadPathArg(arg string) bool {
	if arg == "" {
		return true
	}
	if filepath.IsAbs(arg) || arg == ".." || strings.HasPrefix(arg, "../") || strings.Contains(arg, "/../") || strings.HasSuffix(arg, "/..") {
		return false
	}
	return true
}

func safeRipgrepArgs(args []string) bool {
	for i, arg := range args {
		if !safeReadPathArg(arg) {
			return false
		}
		if arg == "--pre" || arg == "--pre-glob" || strings.HasPrefix(arg, "--pre=") || strings.HasPrefix(arg, "--pre-glob=") {
			return false
		}
		if arg == "-g" && i+1 >= len(args) {
			return false
		}
	}
	return true
}

func safeFindArgs(args []string) bool {
	for _, arg := range args {
		if !safeReadPathArg(arg) {
			return false
		}
		switch arg {
		case "-exec", "-execdir", "-ok", "-okdir", "-delete", "-fprint", "-fprint0", "-fprintf", "-fls":
			return false
		}
	}
	return true
}

func allowDestructive(command []string) bool {
	if len(command) == 0 {
		return false
	}
	switch command[0] {
	case "rm", "rmdir", "mv", "cp":
		return true
	case "git":
		if len(command) < 2 {
			return false
		}
		switch command[1] {
		case "clean", "reset", "restore", "rm":
			return true
		case "checkout":
			return len(command) >= 3 && command[2] == "--"
		}
	}
	return false
}

type commandResult struct {
	Command  string `json:"command"`
	Output   string `json:"output"`
	TimedOut bool   `json:"timed_out,omitempty"`
}

func commandContext(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	return context.WithTimeout(ctx, timeout)
}

func runCommand(ctx context.Context, dir string, command []string) (commandResult, error) {
	if len(command) == 0 {
		return commandResult{}, fmt.Errorf("command is required")
	}
	cmd := exec.CommandContext(ctx, command[0], command[1:]...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	result := commandResult{Command: strings.Join(command, " "), Output: string(out), TimedOut: ctx.Err() == context.DeadlineExceeded}
	return result, err
}

func run(ctx context.Context, timeout time.Duration, dir string, command []string) (json.RawMessage, error) {
	childCtx, cancel := commandContext(ctx, timeout)
	defer cancel()
	result, err := runCommand(childCtx, dir, command)
	b, _ := json.Marshal(result)
	if err != nil {
		return b, err
	}
	return b, nil
}

func commandStrings(commands [][]string) []string {
	values := make([]string, 0, len(commands))
	for _, command := range commands {
		values = append(values, strings.Join(command, " "))
	}
	return values
}
