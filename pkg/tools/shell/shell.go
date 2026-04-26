package shell

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
	for _, t := range []tool.Tool{LimitedTool{timeout: base.Timeout}, ApprovedTool{timeout: base.Timeout}} {
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
	joined := strings.Join(command, " ")
	switch joined {
	case "go test ./...", "go build ./...", "go fmt ./...", "git status", "git diff", "git log", "ls", "find":
		return true
	}
	return command[0] == "cat" && len(command) == 2 && !strings.Contains(command[1], "..")
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

func run(ctx context.Context, timeout time.Duration, dir string, command []string) (json.RawMessage, error) {
	if len(command) == 0 {
		return nil, fmt.Errorf("command is required")
	}
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	childCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(childCtx, command[0], command[1:]...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	result := map[string]any{"command": strings.Join(command, " "), "output": string(out)}
	if childCtx.Err() == context.DeadlineExceeded {
		result["timed_out"] = true
	}
	if err != nil {
		b, _ := json.Marshal(result)
		return b, err
	}
	return json.Marshal(result)
}
