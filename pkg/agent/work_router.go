package agent

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/andrewneudegg/lab/pkg/remoteagent"
	taskstore "github.com/andrewneudegg/lab/pkg/task"
)

const (
	workRouteStatusLocal     = "local"
	workRouteStatusRemote    = "remote"
	workRouteStatusAmbiguous = "ambiguous"
	workRouteStatusMissing   = "missing"
)

type RemoteWorkspace struct {
	ID            string            `json:"id"`
	ProjectID     string            `json:"project_id"`
	AgentID       string            `json:"agent_id"`
	AgentName     string            `json:"agent_name,omitempty"`
	Machine       string            `json:"machine,omitempty"`
	Status        string            `json:"status"`
	CurrentTaskID string            `json:"current_task_id,omitempty"`
	WorkdirID     string            `json:"workdir_id"`
	Workdir       string            `json:"workdir"`
	Label         string            `json:"label,omitempty"`
	RepoURL       string            `json:"repo_url,omitempty"`
	Branch        string            `json:"branch,omitempty"`
	Labels        []string          `json:"labels,omitempty"`
	Backend       string            `json:"backend,omitempty"`
	Metadata      map[string]string `json:"metadata,omitempty"`
	LastSeen      time.Time         `json:"last_seen,omitempty"`
}

type WorkRouteDecision struct {
	Status     string                     `json:"status"`
	Reason     string                     `json:"reason"`
	Target     *taskstore.ExecutionTarget `json:"target,omitempty"`
	Candidates []RemoteWorkspace          `json:"candidates,omitempty"`
}

func (o *Orchestrator) ListRemoteWorkspaces() ([]RemoteWorkspace, error) {
	return o.listRemoteWorkspaces(time.Now().UTC())
}

func (o *Orchestrator) listRemoteWorkspaces(now time.Time) ([]RemoteWorkspace, error) {
	if o.remoteAgents == nil {
		return []RemoteWorkspace{}, nil
	}
	staleAfter := time.Duration(o.cfg.ControlPlane.AgentStaleSeconds) * time.Second
	if staleAfter <= 0 {
		staleAfter = 30 * time.Second
	}
	agents, err := o.remoteAgents.List(staleAfter, now)
	if err != nil {
		return nil, err
	}
	workspaces := make([]RemoteWorkspace, 0)
	for _, agent := range agents {
		for _, workdir := range agent.Workdirs {
			path := strings.TrimSpace(workdir.Path)
			if path == "" {
				continue
			}
			workdirID := firstNonEmptyString(strings.TrimSpace(workdir.ID), path)
			projectID := workspaceProjectID(workdir)
			labels := compactRouteLabels(append(append([]string{}, workdir.Labels...), projectID, workdir.Label, filepath.Base(path), agent.ID, agent.Name))
			workspaces = append(workspaces, RemoteWorkspace{
				ID:            agent.ID + ":" + workdirID,
				ProjectID:     projectID,
				AgentID:       agent.ID,
				AgentName:     agent.Name,
				Machine:       agent.Machine,
				Status:        agent.Status,
				CurrentTaskID: agent.CurrentTaskID,
				WorkdirID:     workdirID,
				Workdir:       path,
				Label:         strings.TrimSpace(workdir.Label),
				RepoURL:       strings.TrimSpace(workdir.RepoURL),
				Branch:        strings.TrimSpace(workdir.Branch),
				Labels:        labels,
				Backend:       firstRemoteBackend(agent),
				Metadata:      compactRouteMetadata(workdir.Metadata),
				LastSeen:      agent.LastSeen,
			})
		}
	}
	sort.Slice(workspaces, func(i, j int) bool {
		if workspaces[i].Status != workspaces[j].Status {
			return workspaces[i].Status == remoteagent.StatusOnline
		}
		if workspaces[i].ProjectID != workspaces[j].ProjectID {
			return workspaces[i].ProjectID < workspaces[j].ProjectID
		}
		return workspaces[i].ID < workspaces[j].ID
	})
	return workspaces, nil
}

func (o *Orchestrator) resolveTaskTarget(goal string, requested *taskstore.ExecutionTarget) (*taskstore.ExecutionTarget, WorkRouteDecision, error) {
	target := normalizeTaskTarget(requested)
	mode := strings.ToLower(strings.TrimSpace(target.Mode))
	if mode == "" {
		mode = "auto"
	}
	if mode == "local" {
		return nil, WorkRouteDecision{Status: workRouteStatusLocal, Reason: "explicit local target"}, nil
	}
	if mode == "remote" && target.AgentID != "" && o.remoteAgents != nil {
		agent, err := o.remoteAgents.Load(target.AgentID)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				reason := fmt.Sprintf("remote agent %q is not registered", target.AgentID)
				return nil, WorkRouteDecision{Status: workRouteStatusMissing, Reason: reason}, fmt.Errorf("%s", reason)
			}
			return nil, WorkRouteDecision{}, err
		}
		if len(agent.Workdirs) == 0 {
			reason := fmt.Sprintf("remote working directory is required: remote agent %q has not advertised any workdirs", target.AgentID)
			return nil, WorkRouteDecision{Status: workRouteStatusMissing, Reason: reason}, fmt.Errorf("%s", reason)
		}
		if target.WorkdirID != "" || target.Workdir != "" {
			resolved := target
			if err := resolveAdvertisedWorkdir(agent, &resolved); err != nil {
				return nil, WorkRouteDecision{Status: workRouteStatusMissing, Reason: err.Error()}, err
			}
			target = resolved
		}
	}
	workspaces, err := o.listRemoteWorkspaces(time.Now().UTC())
	if err != nil {
		return nil, WorkRouteDecision{}, err
	}
	if mode == "remote" {
		resolved, decision, ok := bestRemoteWorkspaceTarget(goal, target, workspaces, true)
		if ok {
			return resolved, decision, nil
		}
		return nil, decision, fmt.Errorf("%s", decision.Reason)
	}
	if mode != "auto" {
		return nil, WorkRouteDecision{}, fmt.Errorf("unsupported task target mode %q", target.Mode)
	}
	if targetHasRoutingConstraint(target) {
		resolved, decision, ok := bestRemoteWorkspaceTarget(goal, target, workspaces, true)
		if ok {
			return resolved, decision, nil
		}
		return nil, decision, fmt.Errorf("%s", decision.Reason)
	}
	if localSelfImprovementGoal(goal) {
		return nil, WorkRouteDecision{Status: workRouteStatusLocal, Reason: "goal appears to improve homelabd or the control plane"}, nil
	}
	if resolved, decision, ok := bestRemoteWorkspaceTarget(goal, target, workspaces, false); ok && routeHasStrongGoalMatch(decision) {
		return resolved, decision, nil
	}
	if len(workspaces) == 0 {
		return nil, WorkRouteDecision{Status: workRouteStatusLocal, Reason: "no remote workspaces are registered"}, nil
	}
	if len(workspaces) == 1 {
		resolved := targetFromRemoteWorkspace(workspaces[0], target, "only registered remote workspace")
		return resolved, WorkRouteDecision{Status: workRouteStatusRemote, Reason: "only registered remote workspace", Target: resolved, Candidates: workspaces}, nil
	}
	candidates := rankedRemoteWorkspaceCandidates(goal, target, workspaces)
	if len(candidates) > 0 && candidates[0].score > 0 && (len(candidates) == 1 || candidates[0].score >= candidates[1].score+4) {
		resolved := targetFromRemoteWorkspace(candidates[0].workspace, target, "matched remote workspace from goal text")
		return resolved, WorkRouteDecision{Status: workRouteStatusRemote, Reason: "matched remote workspace from goal text", Target: resolved, Candidates: rankedWorkspaceRefs(candidates)}, nil
	}
	refs := rankedWorkspaceRefs(candidates)
	if len(refs) == 0 {
		refs = workspaces
	}
	return nil, WorkRouteDecision{Status: workRouteStatusAmbiguous, Reason: "multiple remote workspaces are registered; specify a project, agent, workdir, or label", Candidates: refs}, fmt.Errorf("ambiguous task target: multiple remote workspaces are registered; specify a project, agent, workdir, or label")
}

func bestRemoteWorkspaceTarget(goal string, target taskstore.ExecutionTarget, workspaces []RemoteWorkspace, constrained bool) (*taskstore.ExecutionTarget, WorkRouteDecision, bool) {
	if len(workspaces) == 0 {
		return nil, WorkRouteDecision{Status: workRouteStatusMissing, Reason: "no remote workspaces are registered"}, false
	}
	ranked := rankedRemoteWorkspaceCandidates(goal, target, workspaces)
	if len(ranked) == 0 || ranked[0].score <= 0 {
		reason := "no remote workspace matched the requested target"
		if !constrained {
			reason = "no remote workspace matched the goal text"
		}
		return nil, WorkRouteDecision{Status: workRouteStatusMissing, Reason: reason, Candidates: rankedWorkspaceRefs(ranked)}, false
	}
	if constrained && len(ranked) > 1 && ranked[0].score == ranked[1].score {
		return nil, WorkRouteDecision{Status: workRouteStatusAmbiguous, Reason: "remote target matched multiple workspaces; specify agent and workdir", Candidates: rankedWorkspaceRefs(ranked)}, false
	}
	resolved := targetFromRemoteWorkspace(ranked[0].workspace, target, "matched remote workspace")
	return resolved, WorkRouteDecision{Status: workRouteStatusRemote, Reason: resolved.Reason, Target: resolved, Candidates: rankedWorkspaceRefs(ranked)}, true
}

type scoredWorkspace struct {
	workspace RemoteWorkspace
	score     int
}

func rankedRemoteWorkspaceCandidates(goal string, target taskstore.ExecutionTarget, workspaces []RemoteWorkspace) []scoredWorkspace {
	out := make([]scoredWorkspace, 0, len(workspaces))
	for _, workspace := range workspaces {
		score := remoteWorkspaceScore(goal, target, workspace)
		if score > 0 {
			out = append(out, scoredWorkspace{workspace: workspace, score: score})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].score != out[j].score {
			return out[i].score > out[j].score
		}
		if out[i].workspace.Status != out[j].workspace.Status {
			return out[i].workspace.Status == remoteagent.StatusOnline
		}
		return out[i].workspace.ID < out[j].workspace.ID
	})
	return out
}

func remoteWorkspaceScore(goal string, target taskstore.ExecutionTarget, workspace RemoteWorkspace) int {
	score := 0
	if target.ProjectID != "" {
		if !equalRouteText(target.ProjectID, workspace.ProjectID) {
			return 0
		}
		score += 120
	}
	if target.AgentID != "" {
		if !equalRouteText(target.AgentID, workspace.AgentID) {
			return 0
		}
		score += 100
	}
	if target.WorkdirID != "" {
		if !equalRouteText(target.WorkdirID, workspace.WorkdirID) {
			return 0
		}
		score += 90
	}
	if target.Workdir != "" {
		if filepath.Clean(target.Workdir) != filepath.Clean(workspace.Workdir) {
			return 0
		}
		score += 90
	}
	if target.RepoURL != "" {
		if !equalRouteText(target.RepoURL, workspace.RepoURL) {
			return 0
		}
		score += 80
	}
	if target.Branch != "" {
		if !equalRouteText(target.Branch, workspace.Branch) {
			return 0
		}
		score += 40
	}
	for _, label := range target.Labels {
		if !workspaceHasLabel(workspace, label) {
			return 0
		}
		score += 50
	}
	haystack := strings.ToLower(strings.Join([]string{
		workspace.ProjectID,
		workspace.AgentID,
		workspace.AgentName,
		workspace.Machine,
		workspace.WorkdirID,
		workspace.Workdir,
		workspace.Label,
		workspace.RepoURL,
		strings.Join(workspace.Labels, " "),
	}, " "))
	for _, token := range significantRouteTokens(goal) {
		if token == "" {
			continue
		}
		if strings.Contains(haystack, token) {
			score += 8
		}
	}
	return score
}

func routeHasStrongGoalMatch(decision WorkRouteDecision) bool {
	if decision.Status != workRouteStatusRemote || len(decision.Candidates) == 0 {
		return false
	}
	if len(decision.Candidates) == 1 {
		return true
	}
	return decision.Reason != ""
}

func targetFromRemoteWorkspace(workspace RemoteWorkspace, requested taskstore.ExecutionTarget, reason string) *taskstore.ExecutionTarget {
	backend := firstNonEmptyString(requested.Backend, workspace.Backend, "codex")
	target := &taskstore.ExecutionTarget{
		Mode:      "remote",
		ProjectID: workspace.ProjectID,
		AgentID:   workspace.AgentID,
		Machine:   workspace.Machine,
		WorkdirID: workspace.WorkdirID,
		Workdir:   workspace.Workdir,
		RepoURL:   firstNonEmptyString(requested.RepoURL, workspace.RepoURL),
		Branch:    firstNonEmptyString(requested.Branch, workspace.Branch),
		Labels:    compactRouteLabels(append(append([]string{}, workspace.Labels...), requested.Labels...)),
		Backend:   backend,
		Reason:    reason,
	}
	return target
}

func normalizeTaskTarget(target *taskstore.ExecutionTarget) taskstore.ExecutionTarget {
	if target == nil {
		return taskstore.ExecutionTarget{}
	}
	value := *target
	value.Mode = strings.ToLower(strings.TrimSpace(value.Mode))
	value.ProjectID = strings.TrimSpace(value.ProjectID)
	value.AgentID = strings.TrimSpace(value.AgentID)
	value.Machine = strings.TrimSpace(value.Machine)
	value.WorkdirID = strings.TrimSpace(value.WorkdirID)
	value.Workdir = strings.TrimSpace(value.Workdir)
	value.RepoURL = strings.TrimSpace(value.RepoURL)
	value.Branch = strings.TrimSpace(value.Branch)
	value.Labels = compactRouteLabels(value.Labels)
	value.Backend = strings.TrimSpace(value.Backend)
	value.Reason = strings.TrimSpace(value.Reason)
	return value
}

func targetHasRoutingConstraint(target taskstore.ExecutionTarget) bool {
	return target.ProjectID != "" || target.AgentID != "" || target.WorkdirID != "" || target.Workdir != "" || target.RepoURL != "" || target.Branch != "" || len(target.Labels) > 0
}

func workspaceProjectID(workdir remoteagent.Workdir) string {
	if value := routeSlug(workdir.ProjectID); value != "" {
		return value
	}
	if value := routeSlug(workdir.Label); value != "" {
		return value
	}
	if value := routeSlug(filepath.Base(workdir.Path)); value != "" {
		return value
	}
	if value := routeSlug(workdir.ID); value != "" {
		return value
	}
	return "workspace"
}

func firstRemoteBackend(agent remoteagent.Agent) string {
	for _, capability := range agent.Capabilities {
		capability = strings.TrimSpace(capability)
		switch capability {
		case "codex", "claude", "gemini":
			return capability
		}
	}
	for _, capability := range agent.Capabilities {
		capability = strings.TrimSpace(capability)
		if capability == "" || genericRemoteCapability(capability) {
			continue
		}
		return capability
	}
	return "codex"
}

func genericRemoteCapability(capability string) bool {
	switch capability {
	case "task.claim", "task.complete", "directory-context", "terminal":
		return true
	default:
		return false
	}
}

func localSelfImprovementGoal(goal string) bool {
	value := strings.ToLower(goal)
	for _, needle := range []string{
		"homelabd",
		"homelab",
		"control plane",
		"this platform",
		"this assistant",
		"assistant itself",
		"self-improve",
		"self improve",
		"task supervisor",
		"goal autopilot",
		"/home/lab/lab",
	} {
		if strings.Contains(value, needle) {
			return true
		}
	}
	return false
}

func workspaceHasLabel(workspace RemoteWorkspace, label string) bool {
	label = routeSlug(label)
	if label == "" {
		return false
	}
	for _, candidate := range append(append([]string{}, workspace.Labels...), workspace.ProjectID, workspace.Label, filepath.Base(workspace.Workdir)) {
		if routeSlug(candidate) == label {
			return true
		}
	}
	return false
}

func significantRouteTokens(value string) []string {
	fields := strings.FieldsFunc(strings.ToLower(value), func(r rune) bool {
		return !(unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_' || r == '.')
	})
	var out []string
	seen := map[string]bool{}
	for _, field := range fields {
		token := strings.Trim(field, "-_.")
		if len(token) < 3 || routeStopWord(token) || seen[token] {
			continue
		}
		seen[token] = true
		out = append(out, token)
	}
	return out
}

func routeStopWord(value string) bool {
	switch value {
	case "the", "and", "for", "with", "this", "that", "task", "build", "make", "fix", "update", "repo", "project", "work", "agent", "remote", "local":
		return true
	default:
		return false
	}
}

func routeSlug(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	lastDash := false
	for _, r := range value {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash && b.Len() > 0 {
			b.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

func equalRouteText(left, right string) bool {
	left = strings.TrimSpace(left)
	right = strings.TrimSpace(right)
	return strings.EqualFold(left, right) || routeSlug(left) == routeSlug(right)
}

func compactRouteLabels(values []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		key := routeSlug(value)
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, value)
	}
	return out
}

func compactRouteMetadata(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	out := map[string]string{}
	for key, value := range values {
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key == "" || value == "" {
			continue
		}
		out[key] = value
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func rankedWorkspaceRefs(values []scoredWorkspace) []RemoteWorkspace {
	out := make([]RemoteWorkspace, 0, len(values))
	for _, value := range values {
		out = append(out, value.workspace)
		if len(out) >= 8 {
			break
		}
	}
	return out
}
