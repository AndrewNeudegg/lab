package shell

import (
	"encoding/json"
	"testing"

	"github.com/andrewneudegg/lab/pkg/tool"
)

func TestLimitedToolDestructiveCommandRequiresPolicyApproval(t *testing.T) {
	policy := tool.NewPolicy(nil)
	input := json.RawMessage(`{"dir":"/tmp/workspaces/task_123","command":["rm","-rf","build"]}`)

	decision := policy.Decide("CoderAgent", LimitedTool{}, input)
	if !decision.Allowed || !decision.NeedsApproval {
		t.Fatalf("expected destructive shell command to require approval: %+v", decision)
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
	for _, script := range []string{"browser:preflight", "uat:tasks", "uat:site"} {
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
