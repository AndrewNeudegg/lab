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
}

func NewPolicy(requireApprovalFor []string) Policy {
	m := make(map[string]bool, len(requireApprovalFor))
	for _, name := range requireApprovalFor {
		m[name] = true
	}
	return Policy{requireApproval: m}
}

func (p Policy) Decide(agent string, t Tool, input json.RawMessage) PolicyDecision {
	if t == nil {
		return PolicyDecision{Allowed: false, Reason: "tool not registered"}
	}
	if p.requireApproval[t.Name()] {
		return PolicyDecision{Allowed: true, NeedsApproval: true, Reason: "configured approval gate"}
	}
	switch t.Risk() {
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

func trustedAgent(agent string) bool {
	switch agent {
	case "OrchestratorAgent", "CoderAgent", "ResearchAgent", "ReviewerAgent", "OpsAgent", "homelabd":
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
