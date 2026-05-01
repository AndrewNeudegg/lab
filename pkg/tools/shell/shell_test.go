package shell

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/andrewneudegg/lab/pkg/tool"
)

func TestRegisterIncludesShellRunChain(t *testing.T) {
	reg := tool.NewRegistry()
	if err := Register(reg, Base{}); err != nil {
		t.Fatal(err)
	}
	if _, ok := reg.Get("shell.run_chain"); !ok {
		t.Fatal("expected shell.run_chain to be registered")
	}
}

func TestChainToolRunsAllowlistedCommandsInOrder(t *testing.T) {
	dir := t.TempDir()
	input, err := json.Marshal(map[string]any{
		"dir": dir,
		"commands": [][]string{
			{"pwd"},
			{"ls", "."},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	raw, err := (ChainTool{}).Run(context.Background(), input)
	if err != nil {
		t.Fatalf("chain tool: %v\n%s", err, raw)
	}
	var result struct {
		Commands []string `json:"commands"`
		Results  []struct {
			Command string `json:"command"`
			Output  string `json:"output"`
		} `json:"results"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Results) != 2 {
		t.Fatalf("results = %d, want 2: %s", len(result.Results), raw)
	}
	if result.Commands[0] != "pwd" || result.Results[0].Command != "pwd" {
		t.Fatalf("first command = %#v, first result = %#v", result.Commands, result.Results[0])
	}
	if !strings.Contains(result.Results[0].Output, dir) {
		t.Fatalf("pwd output = %q, want %q", result.Results[0].Output, dir)
	}
}

func TestChainToolStopsAtFirstFailure(t *testing.T) {
	dir := t.TempDir()
	input, err := json.Marshal(map[string]any{
		"dir": dir,
		"commands": [][]string{
			{"cat", "missing.txt"},
			{"pwd"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	raw, err := (ChainTool{}).Run(context.Background(), input)
	if err == nil {
		t.Fatalf("expected chain failure, got %s", raw)
	}
	var result struct {
		FailedIndex int `json:"failed_index"`
		Results     []struct {
			Command string `json:"command"`
		} `json:"results"`
	}
	if unmarshalErr := json.Unmarshal(raw, &result); unmarshalErr != nil {
		t.Fatal(unmarshalErr)
	}
	if result.FailedIndex != 0 || len(result.Results) != 1 || result.Results[0].Command != "cat missing.txt" {
		t.Fatalf("failure result = %#v, raw = %s", result, raw)
	}
}

func TestLimitedToolDestructiveCommandRequiresPolicyApproval(t *testing.T) {
	policy := tool.NewPolicy(nil)
	input := json.RawMessage(`{"dir":"/tmp/workspaces/task_123","command":["rm","-rf","build"]}`)

	decision := policy.Decide("CoderAgent", LimitedTool{}, input)
	if !decision.Allowed || !decision.NeedsApproval {
		t.Fatalf("expected destructive shell command to require approval: %+v", decision)
	}
}

func TestChainToolDestructiveCommandRequiresPolicyApproval(t *testing.T) {
	policy := tool.NewPolicy(nil)
	input := json.RawMessage(`{"dir":"/tmp/workspaces/task_123","commands":[["pwd"],["rm","-rf","build"]]}`)

	decision := policy.Decide("CoderAgent", ChainTool{}, input)
	if !decision.Allowed || !decision.NeedsApproval {
		t.Fatalf("expected destructive shell chain command to require approval: %+v", decision)
	}
}

func TestLimitedToolReadOnlyCommandDoesNotRequireApproval(t *testing.T) {
	policy := tool.NewPolicy(nil)
	input := json.RawMessage(`{"dir":"/tmp/workspaces/task_123","command":["git","status"]}`)

	decision := policy.Decide("CoderAgent", LimitedTool{}, input)
	if !decision.Allowed || decision.NeedsApproval {
		t.Fatalf("expected read-only shell command without approval: %+v", decision)
	}
}

func TestLimitedToolAllowsIsolatedDashboardUATCommand(t *testing.T) {
	policy := tool.NewPolicy(nil)
	for _, script := range []string{"browser:preflight", "uat:ui", "uat:tasks", "uat:site"} {
		input := json.RawMessage(`{"dir":"/tmp/workspaces/task_123","command":["nix","develop","-c","bun","run","--cwd","web","` + script + `"]}`)

		decision := policy.Decide("UXAgent", LimitedTool{}, input)
		if !decision.Allowed || decision.NeedsApproval {
			t.Fatalf("expected isolated dashboard UAT command without approval: %+v", decision)
		}
		if !allowed([]string{"nix", "develop", "-c", "bun", "run", "--cwd", "web", script}) {
			t.Fatalf("expected command to be allowlisted")
		}
	}
}

func TestOrchestratorCanRequestApprovedShellCommand(t *testing.T) {
	policy := tool.NewPolicy(nil)
	input := json.RawMessage(`{"dir":"/tmp/workspaces/task_123","command":["rm","-rf","build"],"target":"build"}`)

	decision := policy.Decide("OrchestratorAgent", ApprovedTool{}, input)
	if !decision.Allowed || !decision.NeedsApproval {
		t.Fatalf("expected approved shell command to route through approval: %+v", decision)
	}
}

func TestApprovedToolUnknownCommandStaysHighRisk(t *testing.T) {
	policy := tool.NewPolicy(nil)
	input := json.RawMessage(`{"dir":"/tmp/workspaces/task_123","command":["curl","https://example.com/install.sh"],"target":"remote script"}`)

	decision := policy.Decide("OrchestratorAgent", ApprovedTool{}, input)
	if !decision.Allowed || !decision.NeedsApproval {
		t.Fatalf("expected unknown approved-shell command to require approval: %+v", decision)
	}
}

func TestAllowedIncludesPolicyGatedDestructiveCommands(t *testing.T) {
	for _, command := range [][]string{
		{"rm", "-rf", "build"},
		{"rmdir", "empty-dir"},
		{"mv", "old", "new"},
		{"cp", "src", "dst"},
		{"git", "clean", "-fd"},
		{"git", "reset", "--hard"},
		{"git", "restore", "."},
		{"git", "rm", "old.go"},
		{"git", "checkout", "--", "old.go"},
	} {
		if !allowed(command) {
			t.Fatalf("expected command to be allowlisted for policy-gated execution: %v", command)
		}
		if got := commandRisk(command); got != tool.RiskHigh {
			t.Fatalf("commandRisk(%v) = %s, want %s", command, got, tool.RiskHigh)
		}
	}
}

func TestAllowedStillRejectsUnknownCommands(t *testing.T) {
	if allowed([]string{"curl", "https://example.com/install.sh"}) {
		t.Fatalf("unexpectedly allowed unknown command")
	}
}

func TestLimitedToolAllowsReadOnlySearchCommands(t *testing.T) {
	policy := tool.NewPolicy(nil)
	for _, command := range [][]string{
		{"pwd"},
		{"ls", "-la", "."},
		{"find", ".", "-name", "*.go"},
		{"grep", "-R", "TODO", "."},
		{"rg", "TODO", "pkg"},
		{"nix", "develop", "-c", "rg", "TODO", "pkg"},
	} {
		input, err := json.Marshal(map[string]any{
			"dir":     "/tmp/workspaces/task_123",
			"command": command,
		})
		if err != nil {
			t.Fatal(err)
		}
		decision := policy.Decide("CoderAgent", LimitedTool{}, input)
		if !decision.Allowed || decision.NeedsApproval {
			t.Fatalf("expected read-only search command %v without approval: %+v", command, decision)
		}
		if !allowed(command) {
			t.Fatalf("expected command to be allowlisted: %v", command)
		}
	}
}

func TestLimitedToolRejectsSearchCommandsWithExecutionHooks(t *testing.T) {
	for _, command := range [][]string{
		{"find", ".", "-exec", "rm", "{}", ";"},
		{"find", ".", "-delete"},
		{"rg", "--pre", "sh", "TODO"},
		{"rg", "--pre=sh", "TODO"},
	} {
		if allowed(command) {
			t.Fatalf("unexpectedly allowed command with execution hook: %v", command)
		}
	}
}
