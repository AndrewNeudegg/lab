package tool

import (
	"encoding/json"
	"strings"
)

type PolicyDecision struct {
	Allowed       bool   `json:"allowed"`
	NeedsApproval bool   `json:"needs_approval"`
	Reason        string `json:"reason"`
}

type Policy struct {
	requireApproval map[string]bool
	agentTools      map[string]map[string]bool
}

func NewPolicy(requireApprovalFor []string) Policy {
	m := make(map[string]bool, len(requireApprovalFor))
	for _, name := range requireApprovalFor {
		m[name] = true
	}
	return Policy{requireApproval: m, agentTools: defaultAgentTools()}
}

func (p Policy) Decide(agent string, t Tool, input json.RawMessage) PolicyDecision {
	if t == nil {
		return PolicyDecision{Allowed: false, Reason: "tool not registered"}
	}
	if !p.agentAllowed(agent, t.Name()) {
		return PolicyDecision{Allowed: false, Reason: "tool not allowed for agent"}
	}
	if p.requireApproval[t.Name()] {
		return PolicyDecision{Allowed: true, NeedsApproval: true, Reason: "configured approval gate"}
	}
	switch EffectiveRisk(t, input) {
	case RiskReadOnly:
		return PolicyDecision{Allowed: true, Reason: "read-only tool"}
	case RiskLow:
		if trustedAgent(agent) {
			return PolicyDecision{Allowed: true, Reason: "low-risk trusted agent tool"}
		}
		return PolicyDecision{Allowed: false, Reason: "low-risk tools require trusted agent"}
	case RiskMedium:
		if workspaceScoped(input) {
			return PolicyDecision{Allowed: true, Reason: "medium-risk tool scoped to workspace"}
		}
		return PolicyDecision{Allowed: true, NeedsApproval: true, Reason: "medium-risk tool outside task workspace"}
	case RiskHigh:
		return PolicyDecision{Allowed: true, NeedsApproval: true, Reason: "high-risk tool"}
	case RiskCritical:
		if explicitTarget(input) {
			return PolicyDecision{Allowed: true, NeedsApproval: true, Reason: "critical tool with explicit target"}
		}
		return PolicyDecision{Allowed: false, Reason: "critical tool requires explicit target"}
	default:
		return PolicyDecision{Allowed: false, Reason: "unknown risk level"}
	}
}

func (p Policy) DecideNamed(agent, name string, input json.RawMessage) PolicyDecision {
	if name == "" {
		return PolicyDecision{Allowed: false, Reason: "tool name is empty"}
	}
	if !p.agentAllowed(agent, name) {
		return PolicyDecision{Allowed: false, Reason: "tool not allowed for agent"}
	}
	if p.requireApproval[name] {
		return PolicyDecision{Allowed: true, NeedsApproval: true, Reason: "configured approval gate"}
	}
	return PolicyDecision{Allowed: true, Reason: "agent pseudo-tool allowed"}
}

func (p Policy) agentAllowed(agent, name string) bool {
	allowed, ok := p.agentTools[agent]
	if !ok {
		return false
	}
	return allowed["*"] || allowed[name]
}

func defaultAgentTools() map[string]map[string]bool {
	return map[string]map[string]bool{
		"OrchestratorAgent": allow(
			"task.create", "task.run", "task.list",
			"workflow.create", "workflow.list", "workflow.show", "workflow.run",
			"agent.list", "agent.delegate",
			"memory.read", "memory.propose_write",
			"text.correct", "text.summarize",
			"health.errors",
			"internet.search", "internet.fetch", "internet.research",
			"repo.list", "repo.search", "repo.read", "repo.current_diff",
			"git.status", "git.diff", "git.branch", "git.describe", "git.log", "git.show",
			"git.commit", "git.revert", "git.merge", "git.worktree_create", "git.worktree_remove",
			"go.test", "go.build", "bun.check", "bun.build", "bun.test", "bun.uat.tasks",
			"shell.run_limited", "shell.run_approved",
		),
		"CoderAgent": allow(
			"text.correct", "text.summarize",
			"internet.search", "internet.fetch", "internet.research",
			"repo.list", "repo.search", "repo.read", "repo.write_patch", "repo.current_diff",
			"git.status", "git.diff", "git.branch", "git.describe", "git.log", "git.show",
			"go.fmt", "go.test", "go.build", "test.run", "bun.check", "bun.build", "bun.test", "bun.uat.tasks",
			"shell.run_limited",
		),
		"UXAgent": allow(
			"text.correct", "text.summarize",
			"internet.search", "internet.fetch", "internet.research",
			"repo.list", "repo.search", "repo.read", "repo.write_patch", "repo.current_diff",
			"git.status", "git.diff", "git.branch", "git.describe", "git.log", "git.show",
			"go.fmt", "go.test", "go.build", "test.run", "bun.check", "bun.build", "bun.test", "bun.uat.tasks",
			"shell.run_limited",
		),
		"ResearchAgent": allow("text.correct", "text.summarize", "internet.search", "internet.fetch", "internet.research", "memory.propose_write"),
		"ReviewerAgent": allow("text.correct", "text.summarize", "internet.search", "internet.fetch", "internet.research", "repo.read", "repo.search", "repo.current_diff", "git.diff", "git.status", "git.branch", "git.describe", "git.log", "git.show", "git.merge_check", "go.test", "go.build", "test.run", "bun.check", "bun.build", "bun.test", "bun.uat.tasks"),
		"OpsAgent":      allow("service.status", "health.errors"),
		"homelabd":      allow("*"),
		"human":         allow("*"),
		"policy":        allow("*"),
	}
}

func allow(names ...string) map[string]bool {
	allowed := make(map[string]bool, len(names))
	for _, name := range names {
		allowed[name] = true
	}
	return allowed
}

func trustedAgent(agent string) bool {
	switch agent {
	case "OrchestratorAgent", "CoderAgent", "UXAgent", "ResearchAgent", "ReviewerAgent", "OpsAgent", "homelabd":
		return true
	default:
		return false
	}
}

func workspaceScoped(input json.RawMessage) bool {
	var v map[string]any
	if json.Unmarshal(input, &v) != nil {
		return false
	}
	for _, key := range []string{"workspace", "dir", "path"} {
		value, ok := v[key].(string)
		if ok && strings.Contains(value, "workspaces/") {
			return true
		}
	}
	return false
}

func explicitTarget(input json.RawMessage) bool {
	var v map[string]any
	if json.Unmarshal(input, &v) != nil {
		return false
	}
	target, ok := v["target"].(string)
	return ok && target != ""
}
