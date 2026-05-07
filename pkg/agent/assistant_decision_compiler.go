package agent

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	assistantstore "github.com/andrewneudegg/lab/pkg/assistant"
)

var assistantTrailingJSONCommaPattern = regexp.MustCompile(`,\s*([}\]])`)

func (o *Orchestrator) compileAssistantRunDecision(run assistantstore.Run, content, source, fallbackReason string) assistantRunDecision {
	audit := &assistantstore.RunDecisionCompiler{
		Source: strings.TrimSpace(source),
		Checks: []string{"schema_parse", "signal_enrichment", "evidence_citations", "safe_actions", "duplicate_actions", "capability_route"},
	}
	if audit.Source == "" {
		audit.Source = "model"
	}

	var decision assistantRunDecision
	if strings.TrimSpace(fallbackReason) != "" {
		audit.Status = "fallback"
		audit.Source = "deterministic"
		audit.Summary = strings.TrimSpace(fallbackReason)
		audit.Rejections = append(audit.Rejections, strings.TrimSpace(fallbackReason))
		decision = fallbackAssistantRunDecisionWithReason(run, fallbackReason)
	} else {
		parsed, repairs, err := parseAssistantRunDecisionWithRepair(content)
		if err != nil {
			reason := assistantFallbackReason("Deterministic fallback scan used because the model output was not valid JSON", err)
			audit.Status = "fallback"
			audit.Source = "deterministic"
			audit.Summary = reason
			audit.Rejections = append(audit.Rejections, "model output rejected: "+truncateAssistantRunText(err.Error(), 220))
			decision = fallbackAssistantRunDecisionWithReason(run, reason)
		} else {
			decision = parsed
			audit.Repairs = append(audit.Repairs, repairs...)
		}
	}

	decision = assistantRunDecisionWithSignals(run, decision)
	decision = compileAssistantRunDecisionSafety(run, decision, audit)
	if audit.Status == "" {
		if len(audit.Rejections) > 0 || len(audit.Repairs) > 0 {
			audit.Status = "repaired"
		} else {
			audit.Status = "accepted"
		}
	}
	if audit.Summary == "" {
		audit.Summary = assistantCompilerSummary(audit)
	}
	decision.Compiler = audit
	return decision
}

func parseAssistantRunDecisionWithRepair(content string) (assistantRunDecision, []string, error) {
	raw := strings.TrimSpace(extractJSON(content))
	if raw == "" {
		return assistantRunDecision{}, nil, fmt.Errorf("empty model response")
	}
	var decision assistantRunDecision
	if err := json.Unmarshal([]byte(raw), &decision); err == nil {
		return decision, nil, nil
	}
	repaired := repairAssistantDecisionJSON(raw)
	if repaired == raw {
		if err := json.Unmarshal([]byte(raw), &decision); err != nil {
			return assistantRunDecision{}, nil, fmt.Errorf("assistant run returned invalid JSON: %w", err)
		}
		return decision, nil, nil
	}
	if err := json.Unmarshal([]byte(repaired), &decision); err != nil {
		return assistantRunDecision{}, nil, fmt.Errorf("assistant run returned invalid JSON after repair: %w", err)
	}
	return decision, []string{"Repaired common JSON formatting before schema validation."}, nil
}

func repairAssistantDecisionJSON(raw string) string {
	repaired := strings.TrimSpace(raw)
	for {
		next := assistantTrailingJSONCommaPattern.ReplaceAllString(repaired, "$1")
		if next == repaired {
			return next
		}
		repaired = next
	}
}

func compileAssistantRunDecisionSafety(run assistantstore.Run, decision assistantRunDecision, audit *assistantstore.RunDecisionCompiler) assistantRunDecision {
	decision = normalizeAssistantRunDecision(decision)
	decision = compileAssistantDecisionFindings(run, decision, audit)

	switch decision.Decision {
	case assistantstore.RunDecisionNoop, assistantstore.RunDecisionRecommend, assistantstore.RunDecisionCreated:
	default:
		audit.Repairs = append(audit.Repairs, fmt.Sprintf("Changed unknown decision %q to no_op.", decision.Decision))
		decision.Decision = assistantstore.RunDecisionNoop
	}

	seen := map[string]bool{}
	kept := make([]assistantstore.RunAction, 0, len(decision.RecommendedActions))
	for _, action := range decision.RecommendedActions {
		action = assistantstore.NormalizeRun(assistantstore.Run{RecommendedActions: []assistantstore.RunAction{action}}).RecommendedActions[0]
		if !assistantCompilerActionKindAllowed(action.Kind) {
			audit.Rejections = append(audit.Rejections, fmt.Sprintf("Rejected action %q because kind %q is not allowed.", action.Title, action.Kind))
			continue
		}
		if strings.TrimSpace(action.Title) == "" || strings.TrimSpace(action.Rationale) == "" {
			audit.Rejections = append(audit.Rejections, "Rejected an action because title or rationale was missing.")
			continue
		}
		key := assistantCompilerActionKey(action)
		if seen[key] {
			audit.Rejections = append(audit.Rejections, fmt.Sprintf("Rejected duplicate action %q.", action.Title))
			continue
		}
		seen[key] = true
		signal, hasSignal := assistantBestSignalForAction(action, run.Snapshot.Signals)
		if hasSignal {
			if signal.Suppressed {
				audit.Rejections = append(audit.Rejections, fmt.Sprintf("Rejected action %q because its signal is suppressed.", action.Title))
				continue
			}
			if !assistantSignalAllowsRecommendation(signal, action.Kind) {
				audit.Rejections = append(audit.Rejections, fmt.Sprintf("Rejected action %q because source %q does not allow %q.", action.Title, signal.Surface, action.Kind))
				continue
			}
			action.Fingerprint = signal.Fingerprint
			action.TargetSurface = firstNonEmptyString(action.TargetSurface, signal.Surface)
			action.Priority = firstNonEmptyString(action.Priority, signal.Priority)
			action.Risk = firstNonEmptyString(action.Risk, "low")
			if strings.EqualFold(action.Kind, "task") {
				action.TaskGoal = firstNonEmptyString(action.TaskGoal, signal.TaskGoal, signal.SuggestedNextStep)
			}
			if strings.EqualFold(action.Kind, "research") {
				action.KnowledgeQuery = firstNonEmptyString(action.KnowledgeQuery, signal.SuggestedNextStep, signal.Title)
			}
			if strings.EqualFold(action.Kind, "workflow") {
				action.WorkflowHint = firstNonEmptyString(action.WorkflowHint, signal.SuggestedNextStep, signal.Title)
			}
		} else if assistantCompilerActionNeedsEvidence(action) {
			audit.Rejections = append(audit.Rejections, fmt.Sprintf("Rejected action %q because it did not cite known snapshot evidence.", action.Title))
			continue
		}
		if missing := assistantCompilerMissingActionInput(action); missing != "" {
			audit.Rejections = append(audit.Rejections, fmt.Sprintf("Rejected action %q because %s was missing.", action.Title, missing))
			continue
		}
		kept = append(kept, action)
	}
	if len(kept) != len(decision.RecommendedActions) {
		audit.Repairs = append(audit.Repairs, fmt.Sprintf("Kept %d of %d model recommendations after harness checks.", len(kept), len(decision.RecommendedActions)))
	}
	decision.RecommendedActions = kept
	if len(decision.RecommendedActions) > 0 && decision.Decision == assistantstore.RunDecisionNoop {
		audit.Repairs = append(audit.Repairs, "Changed no_op to recommend because verified actions remain.")
		decision.Decision = assistantstore.RunDecisionRecommend
	}
	if len(decision.RecommendedActions) == 0 && assistantDecisionFindingCount(decision) == 0 && decision.Decision != assistantstore.RunDecisionNoop {
		audit.Repairs = append(audit.Repairs, "Changed recommendation to no_op because no verified action or finding remains.")
		decision.Decision = assistantstore.RunDecisionNoop
	}
	return normalizeAssistantRunDecision(decision)
}

func compileAssistantDecisionFindings(run assistantstore.Run, decision assistantRunDecision, audit *assistantstore.RunDecisionCompiler) assistantRunDecision {
	decision.Concerns = assistantCompilerFilterFindings(run, decision.Concerns, "concern", audit)
	decision.Opportunities = assistantCompilerFilterFindings(run, decision.Opportunities, "opportunity", audit)
	return decision
}

func assistantCompilerFilterFindings(run assistantstore.Run, findings []assistantstore.RunFinding, label string, audit *assistantstore.RunDecisionCompiler) []assistantstore.RunFinding {
	kept := findings[:0]
	for _, finding := range findings {
		if strings.TrimSpace(finding.ObjectID) != "" || strings.TrimSpace(finding.ObjectURL) != "" {
			if !assistantSnapshotHasObjectRef(run.Snapshot, finding.ObjectID, finding.ObjectURL) {
				audit.Rejections = append(audit.Rejections, fmt.Sprintf("Rejected %s %q because cited evidence was not in the snapshot.", label, finding.Title))
				continue
			}
		}
		kept = append(kept, finding)
	}
	return kept
}

func assistantCompilerActionKindAllowed(kind string) bool {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "task", "research", "workflow", "watch", "observe":
		return true
	default:
		return false
	}
}

func assistantCompilerActionNeedsEvidence(action assistantstore.RunAction) bool {
	switch strings.ToLower(strings.TrimSpace(action.Kind)) {
	case "task", "research", "workflow":
		return true
	default:
		return false
	}
}

func assistantCompilerMissingActionInput(action assistantstore.RunAction) string {
	switch strings.ToLower(strings.TrimSpace(action.Kind)) {
	case "task":
		if strings.TrimSpace(action.TaskGoal) == "" {
			return "task_goal"
		}
	case "research":
		if strings.TrimSpace(action.KnowledgeQuery) == "" {
			return "knowledge_query"
		}
	case "workflow":
		if strings.TrimSpace(action.WorkflowHint) == "" {
			return "workflow_hint"
		}
	}
	return ""
}

func assistantCompilerActionKey(action assistantstore.RunAction) string {
	return strings.ToLower(strings.Join([]string{
		strings.TrimSpace(action.Kind),
		strings.TrimSpace(action.Fingerprint),
		strings.TrimSpace(action.TargetSurface),
		strings.TrimSpace(action.Title),
		strings.TrimSpace(action.TaskGoal),
		strings.TrimSpace(action.KnowledgeQuery),
		strings.TrimSpace(action.WorkflowHint),
	}, "|"))
}

func assistantSnapshotHasObjectRef(snapshot assistantstore.RunSnapshot, objectID, objectURL string) bool {
	objectID = strings.TrimSpace(objectID)
	objectURL = strings.TrimSpace(objectURL)
	if objectID == "" && objectURL == "" {
		return true
	}
	for _, signal := range snapshot.Signals {
		if assistantObjectRefMatches(signal.ObjectID, signal.ObjectURL, objectID, objectURL) {
			return true
		}
		for _, evidence := range signal.Evidence {
			if assistantObjectRefMatches(evidence.ObjectID, evidence.ObjectURL, objectID, objectURL) {
				return true
			}
		}
	}
	for _, ref := range append(append(append([]assistantstore.RunObjectRef{}, snapshot.AttentionTasks...), snapshot.RecentWorkflows...), snapshot.KnowledgeSpaces...) {
		if assistantObjectRefMatches(ref.ID, ref.URL, objectID, objectURL) {
			return true
		}
	}
	for _, item := range snapshot.Health.Items {
		if assistantObjectRefMatches(item.ID, item.URL, objectID, objectURL) || assistantObjectRefMatches(item.Title, item.URL, objectID, objectURL) {
			return true
		}
	}
	for _, item := range snapshot.Supervisor.Items {
		if assistantObjectRefMatches(item.ID, item.URL, objectID, objectURL) || assistantObjectRefMatches(item.Title, item.URL, objectID, objectURL) {
			return true
		}
	}
	for _, event := range snapshot.RecentEvents {
		if assistantObjectRefMatches(event.ID, "", objectID, objectURL) || assistantObjectRefMatches(event.TaskID, "", objectID, objectURL) {
			return true
		}
	}
	return false
}

func assistantObjectRefMatches(candidateID, candidateURL, objectID, objectURL string) bool {
	return (objectID != "" && strings.EqualFold(strings.TrimSpace(candidateID), objectID)) ||
		(objectURL != "" && strings.TrimSpace(candidateURL) == objectURL)
}

func assistantCompilerSummary(audit *assistantstore.RunDecisionCompiler) string {
	switch audit.Status {
	case "fallback":
		return "Model output was rejected; deterministic fallback produced the decision."
	case "repaired":
		return "Harness repaired or filtered the model decision before accepting it."
	default:
		return "Harness accepted the model decision after schema, evidence, safety, and routing checks."
	}
}
