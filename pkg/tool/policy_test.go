package tool

import (
	"context"
	"encoding/json"
	"testing"
)

type stubTool struct {
	name string
	risk RiskLevel
}

func (s stubTool) Name() string        { return s.name }
func (s stubTool) Description() string { return "" }
func (s stubTool) Schema() json.RawMessage {
	return json.RawMessage(`{}`)
}
func (s stubTool) Risk() RiskLevel { return s.risk }
func (s stubTool) Run(context.Context, json.RawMessage) (json.RawMessage, error) {
	return json.RawMessage(`{}`), nil
}

func TestCoderAgentCannotUseMergeTool(t *testing.T) {
	policy := NewPolicy(nil)
	decision := policy.Decide("CoderAgent", stubTool{name: "git.merge_approved", risk: RiskHigh}, json.RawMessage(`{"target":"main"}`))
	if decision.Allowed {
		t.Fatalf("expected CoderAgent merge to be denied")
	}
	if decision.Reason != "tool not allowed for agent" {
		t.Fatalf("unexpected denial reason: %s", decision.Reason)
	}
}

func TestCoderAgentCanPatchWorkspace(t *testing.T) {
	policy := NewPolicy(nil)
	decision := policy.Decide("CoderAgent", stubTool{name: "repo.write_patch", risk: RiskMedium}, json.RawMessage(`{"workspace":"/tmp/workspaces/task_123","patch":"diff"}`))
	if !decision.Allowed {
		t.Fatalf("expected CoderAgent workspace patch to be allowed: %s", decision.Reason)
	}
	if decision.NeedsApproval {
		t.Fatalf("workspace patch should not require approval by default")
	}
}

func TestUXAgentCanResearchPatchAndRunBrowserUATCommand(t *testing.T) {
	policy := NewPolicy(nil)
	for _, tt := range []struct {
		name  string
		risk  RiskLevel
		input string
	}{
		{name: "internet.research", risk: RiskReadOnly, input: `{"query":"WCAG 2.2 target size"}`},
		{name: "repo.write_patch", risk: RiskMedium, input: `{"workspace":"/tmp/workspaces/task_123","patch":"diff"}`},
		{name: "bun.uat.tasks", risk: RiskLow, input: `{"dir":"/tmp/workspaces/task_123/web"}`},
		{name: "bun.uat.site", risk: RiskLow, input: `{"dir":"/tmp/workspaces/task_123/web"}`},
		{name: "shell.run_limited", risk: RiskLow, input: `{"dir":"/tmp/workspaces/task_123","command":["nix","develop","-c","bun","run","--cwd","web","browser:preflight"]}`},
		{name: "shell.run_limited", risk: RiskLow, input: `{"dir":"/tmp/workspaces/task_123","command":["nix","develop","-c","bun","run","--cwd","web","uat:tasks"]}`},
		{name: "shell.run_limited", risk: RiskLow, input: `{"dir":"/tmp/workspaces/task_123","command":["nix","develop","-c","bun","run","--cwd","web","uat:site"]}`},
	} {
		decision := policy.Decide("UXAgent", stubTool{name: tt.name, risk: tt.risk}, json.RawMessage(tt.input))
		if !decision.Allowed || decision.NeedsApproval {
			t.Fatalf("expected UXAgent %s to be allowed without approval: %+v", tt.name, decision)
		}
	}
}

func TestUXAgentCannotUseMergeTool(t *testing.T) {
	policy := NewPolicy(nil)
	decision := policy.Decide("UXAgent", stubTool{name: "git.merge_approved", risk: RiskHigh}, json.RawMessage(`{"target":"main"}`))
	if decision.Allowed {
		t.Fatalf("expected UXAgent merge to be denied")
	}
	if decision.Reason != "tool not allowed for agent" {
		t.Fatalf("unexpected denial reason: %s", decision.Reason)
	}
}

func TestOrchestratorCannotUseServiceRestart(t *testing.T) {
	policy := NewPolicy(nil)
	decision := policy.Decide("OrchestratorAgent", stubTool{name: "service.restart", risk: RiskHigh}, json.RawMessage(`{"target":"svc"}`))
	if decision.Allowed {
		t.Fatalf("expected OrchestratorAgent service restart to be denied by allowlist")
	}
}

func TestOrchestratorCanRemoveTaskWorkspace(t *testing.T) {
	policy := NewPolicy(nil)
	decision := policy.Decide("OrchestratorAgent", stubTool{name: "git.worktree_remove", risk: RiskMedium}, json.RawMessage(`{"workspace":"/tmp/workspaces/task_123","force":true}`))
	if !decision.Allowed || decision.NeedsApproval {
		t.Fatalf("expected OrchestratorAgent workspace removal to be allowed without approval: %+v", decision)
	}
}

func TestOrchestratorCanReadHealthErrors(t *testing.T) {
	policy := NewPolicy(nil)
	decision := policy.Decide("OrchestratorAgent", stubTool{name: "health.errors", risk: RiskReadOnly}, json.RawMessage(`{"limit":10}`))
	if !decision.Allowed || decision.NeedsApproval {
		t.Fatalf("expected OrchestratorAgent health.errors to be allowed without approval: %+v", decision)
	}
}

func TestOrchestratorCanRequestApprovalGatedGitWorkflow(t *testing.T) {
	policy := NewPolicy(nil)
	for _, name := range []string{"git.commit", "git.revert", "git.merge"} {
		decision := policy.Decide("OrchestratorAgent", stubTool{name: name, risk: RiskHigh}, json.RawMessage(`{"dir":"/tmp/workspaces/task_123","target":"repo"}`))
		if !decision.Allowed || !decision.NeedsApproval {
			t.Fatalf("expected OrchestratorAgent %s to be allowed with approval: %+v", name, decision)
		}
	}
}

func TestReviewerCanRunPremergeCheck(t *testing.T) {
	policy := NewPolicy(nil)
	decision := policy.Decide("ReviewerAgent", stubTool{name: "git.merge_check", risk: RiskReadOnly}, json.RawMessage(`{"target":"/repo","branch":"homelabd/task_123"}`))
	if !decision.Allowed || decision.NeedsApproval {
		t.Fatalf("expected ReviewerAgent premerge check to be allowed without approval: %+v", decision)
	}
}

func TestAgentsCanUseInternetReadTools(t *testing.T) {
	policy := NewPolicy(nil)
	for _, agent := range []string{"OrchestratorAgent", "CoderAgent", "UXAgent", "ResearchAgent", "ReviewerAgent"} {
		for _, name := range []string{"text.correct", "text.summarize", "internet.search", "internet.fetch", "internet.research"} {
			decision := policy.Decide(agent, stubTool{name: name, risk: RiskReadOnly}, json.RawMessage(`{"query":"golang","url":"https://example.com"}`))
			if !decision.Allowed || decision.NeedsApproval {
				t.Fatalf("expected %s %s to be allowed without approval: %+v", agent, name, decision)
			}
		}
	}
}

func TestConfiguredApprovalStillAppliesToAllowedTool(t *testing.T) {
	policy := NewPolicy([]string{"git.merge_approved"})
	decision := policy.Decide("human", stubTool{name: "git.merge_approved", risk: RiskHigh}, json.RawMessage(`{"target":"main"}`))
	if !decision.Allowed || !decision.NeedsApproval {
		t.Fatalf("expected allowed human merge to require approval: %+v", decision)
	}
}

func TestPseudoToolAllowlist(t *testing.T) {
	policy := NewPolicy(nil)
	decision := policy.DecideNamed("CoderAgent", "task.run", json.RawMessage(`{"task_id":"task_123"}`))
	if decision.Allowed {
		t.Fatalf("expected CoderAgent task.run pseudo-tool to be denied")
	}
	decision = policy.DecideNamed("OrchestratorAgent", "task.run", json.RawMessage(`{"task_id":"task_123"}`))
	if !decision.Allowed {
		t.Fatalf("expected OrchestratorAgent task.run pseudo-tool to be allowed: %s", decision.Reason)
	}
	decision = policy.DecideNamed("OrchestratorAgent", "workflow.run", json.RawMessage(`{"workflow_id":"workflow_123"}`))
	if !decision.Allowed {
		t.Fatalf("expected OrchestratorAgent workflow.run pseudo-tool to be allowed: %s", decision.Reason)
	}
}
