package agent

import (
	"fmt"
	"strings"

	assistantstore "github.com/andrewneudegg/lab/pkg/assistant"
)

func assistantCapabilityContracts() []assistantstore.RunCapabilityContract {
	return []assistantstore.RunCapabilityContract{
		{
			ID:                 "observe",
			Capability:         "observe",
			ActionKind:         "observe",
			AllowedSafeActions: []string{"useful", "snooze", "dismiss"},
			RequiredEvidence:   []string{"snapshot_signal_or_finding"},
			AutonomyCeiling:    assistantstore.RunAutonomyExecuteSafe,
			Risk:               "low",
			DuplicateRule:      "same kind, title, target, and fingerprint may only appear once per run",
			SuppressionRule:    "dismissed, snoozed, useful-cleared, and already-created signals are not re-opened without a new sighting",
			CompletionRule:     "archive when the operator marks useful, snoozes, dismisses, creates linked work, or no action remains",
			Explanation:        "Observation can surface evidence and ask for feedback, but it cannot mutate the system.",
		},
		{
			ID:                 "task",
			Capability:         "tasks",
			ActionKind:         "task",
			AllowedSafeActions: []string{"create_task", "useful", "snooze", "dismiss"},
			RequiredEvidence:   []string{"snapshot_signal", "object_id_or_object_url"},
			RequiredInputs:     []string{"task_goal"},
			AutonomyCeiling:    assistantstore.RunAutonomyCreateTasks,
			Risk:               "low",
			DuplicateRule:      "task recommendations must carry a stable fingerprint and cannot duplicate existing signal work",
			SuppressionRule:    "prior created-task, dismissed, and snoozed feedback blocks duplicate task creation",
			CompletionRule:     "resolved when task is created, useful, snoozed, dismissed, skipped, or failed",
			Explanation:        "Task actions are allowed only when grounded in known evidence and bounded by a clear task goal.",
		},
		{
			ID:                 "research",
			Capability:         "knowledge",
			ActionKind:         "research",
			AllowedSafeActions: []string{"useful", "snooze", "dismiss"},
			RequiredEvidence:   []string{"snapshot_signal", "object_id_or_object_url"},
			RequiredInputs:     []string{"knowledge_query"},
			AutonomyCeiling:    assistantstore.RunAutonomyPropose,
			Risk:               "low",
			RequiresApproval:   true,
			DuplicateRule:      "research recommendations must not duplicate an open recommendation for the same question",
			SuppressionRule:    "prior feedback suppresses stale research suggestions until new evidence appears",
			CompletionRule:     "resolved when operator marks useful, snoozes, dismisses, or starts linked research work",
			Explanation:        "Research actions prepare a Knowledge follow-up; they do not perform the research automatically.",
		},
		{
			ID:                 "workflow",
			Capability:         "workflows",
			ActionKind:         "workflow",
			AllowedSafeActions: []string{"useful", "snooze", "dismiss"},
			RequiredEvidence:   []string{"snapshot_signal", "object_id_or_object_url"},
			RequiredInputs:     []string{"workflow_hint"},
			AutonomyCeiling:    assistantstore.RunAutonomyRunWorkflows,
			Risk:               "low",
			RequiresApproval:   true,
			DuplicateRule:      "workflow recommendations must target one repeatable outcome and avoid duplicate workflow hints",
			SuppressionRule:    "prior feedback suppresses stale workflow suggestions until new evidence appears",
			CompletionRule:     "resolved when operator marks useful, snoozes, dismisses, or creates linked workflow work",
			Explanation:        "Workflow actions can propose a repeatable thinking path, but execution stays behind workflow autonomy and receipts.",
		},
	}
}

func assistantCapabilityContractForAction(run assistantstore.Run, action assistantstore.RunAction) (assistantstore.RunCapabilityContract, bool) {
	kind := strings.ToLower(strings.TrimSpace(action.Kind))
	if kind == "watch" {
		kind = "observe"
	}
	for _, contract := range assistantCapabilityContracts() {
		if strings.EqualFold(contract.ActionKind, kind) {
			contract.RequiresApproval = assistantContractRequiresApproval(run.Autonomy, contract)
			return contract, true
		}
	}
	return assistantstore.RunCapabilityContract{}, false
}

func assistantContractRequiresApproval(autonomy string, contract assistantstore.RunCapabilityContract) bool {
	if contract.RequiresApproval {
		return true
	}
	switch contract.ActionKind {
	case "task":
		return !assistantAutonomyAllowsTaskCreation(autonomy)
	case "workflow":
		return autonomy != assistantstore.RunAutonomyRunWorkflows && autonomy != assistantstore.RunAutonomyExecuteSafe
	default:
		return false
	}
}

func assistantActionPlanPreview(run assistantstore.Run, action assistantstore.RunAction, contract assistantstore.RunCapabilityContract, hasSignal bool) assistantstore.RunActionPlanPreview {
	blockers := assistantActionPlanBlockers(action, contract, hasSignal)
	requiresApproval := assistantContractRequiresApproval(run.Autonomy, contract)
	status := "ready"
	if len(blockers) > 0 {
		status = "blocked"
	} else if requiresApproval {
		status = "approval_required"
	}
	plan := assistantstore.RunActionPlanPreview{
		Status:           status,
		Summary:          assistantActionPlanSummary(action, contract, status),
		RequiresApproval: requiresApproval,
		Blockers:         blockers,
		Steps: []assistantstore.RunActionPlanStep{
			{Title: "Bind recommendation to snapshot evidence", Surface: firstNonEmptyString(action.TargetSurface, contract.Capability), Mode: "check", Status: assistantPlanStepStatus(len(blockers) == 0)},
			{Title: "Apply " + contract.ID + " capability contract", Surface: contract.Capability, Mode: "check", Status: "passed"},
			assistantActionPlanExecutionStep(action, contract, requiresApproval, len(blockers) == 0),
		},
		Receipts: []assistantstore.RunActionPlanReceipt{
			{Kind: "contract_checked", Message: fmt.Sprintf("Contract %q constrained action kind %q.", contract.ID, action.Kind)},
			{Kind: "dry_run", Message: "No mutation is performed until autonomy and approval checks pass."},
		},
	}
	if requiresApproval {
		plan.Receipts = append(plan.Receipts, assistantstore.RunActionPlanReceipt{Kind: "approval_required", Message: "Operator approval is required before execution."})
	}
	if len(blockers) > 0 {
		plan.Receipts = append(plan.Receipts, assistantstore.RunActionPlanReceipt{Kind: "blocked", Message: strings.Join(blockers, " ")})
	}
	return plan
}

func assistantActionPlanBlockers(action assistantstore.RunAction, contract assistantstore.RunCapabilityContract, hasSignal bool) []string {
	var blockers []string
	if !hasSignal && assistantCompilerActionNeedsEvidence(action) {
		blockers = append(blockers, "No matching snapshot signal was found.")
	}
	if missing := assistantCompilerMissingActionInput(action); missing != "" {
		blockers = append(blockers, missing+" is missing.")
	}
	if !assistantRiskWithinContract(action.Risk, contract.Risk) {
		blockers = append(blockers, fmt.Sprintf("risk %q exceeds contract risk %q.", action.Risk, contract.Risk))
	}
	return blockers
}

func assistantActionPlanSummary(action assistantstore.RunAction, contract assistantstore.RunCapabilityContract, status string) string {
	switch status {
	case "blocked":
		return "Harness blocked this " + contract.ActionKind + " action before execution."
	case "approval_required":
		return "Harness prepared this " + contract.ActionKind + " action for operator approval."
	default:
		if strings.EqualFold(action.Kind, "task") {
			return "Harness can create a bounded task when autonomy allows."
		}
		return "Harness accepted this " + contract.ActionKind + " action as a non-mutating recommendation."
	}
}

func assistantActionPlanExecutionStep(action assistantstore.RunAction, contract assistantstore.RunCapabilityContract, requiresApproval, unblocked bool) assistantstore.RunActionPlanStep {
	status := "ready"
	if !unblocked {
		status = "blocked"
	} else if requiresApproval {
		status = "approval_required"
	}
	title := "Return recommendation for operator decision"
	mode := "propose"
	if strings.EqualFold(action.Kind, "task") {
		title = "Create bounded follow-up task"
		mode = "mutation"
	}
	return assistantstore.RunActionPlanStep{Title: title, Surface: contract.Capability, Mode: mode, Status: status}
}

func assistantPlanStepStatus(ok bool) string {
	if ok {
		return "passed"
	}
	return "blocked"
}

func assistantRiskWithinContract(actionRisk, contractRisk string) bool {
	order := map[string]int{"": 0, "none": 0, "low": 1, "medium": 2, "high": 3, "critical": 4}
	actionValue, ok := order[strings.ToLower(strings.TrimSpace(actionRisk))]
	if !ok {
		actionValue = 3
	}
	contractValue, ok := order[strings.ToLower(strings.TrimSpace(contractRisk))]
	if !ok {
		contractValue = 1
	}
	return actionValue <= contractValue
}

func ensureAssistantActionPlanPreview(run assistantstore.Run, action *assistantstore.RunAction) {
	if action == nil {
		return
	}
	contract, ok := assistantCapabilityContractForAction(run, *action)
	if !ok {
		return
	}
	action.ContractID = firstNonEmptyString(action.ContractID, contract.ID)
	if action.Contract == nil {
		action.Contract = &contract
	}
	if action.Plan == nil {
		_, hasSignal := assistantBestSignalForAction(*action, run.Snapshot.Signals)
		if len(run.Snapshot.Signals) == 0 {
			hasSignal = true
		}
		plan := assistantActionPlanPreview(run, *action, contract, hasSignal)
		action.Plan = &plan
	}
}

func markAssistantActionPlanExecuted(action *assistantstore.RunAction, message string) {
	if action == nil || action.Plan == nil {
		return
	}
	action.Plan.Status = "executed"
	action.Plan.RequiresApproval = false
	for index := range action.Plan.Steps {
		if action.Plan.Steps[index].Status == "ready" || action.Plan.Steps[index].Status == "approval_required" {
			action.Plan.Steps[index].Status = "executed"
		}
	}
	action.Plan.Receipts = append(action.Plan.Receipts, assistantstore.RunActionPlanReceipt{
		Kind:    "executed",
		Message: strings.TrimSpace(message),
	})
}

func markAssistantActionPlanBlocked(action *assistantstore.RunAction, blocker string) {
	if action == nil || action.Plan == nil {
		return
	}
	blocker = strings.TrimSpace(blocker)
	action.Plan.Status = "blocked"
	if blocker != "" {
		action.Plan.Blockers = append(action.Plan.Blockers, blocker)
		action.Plan.Receipts = append(action.Plan.Receipts, assistantstore.RunActionPlanReceipt{Kind: "blocked", Message: blocker})
	}
	for index := range action.Plan.Steps {
		if action.Plan.Steps[index].Status == "ready" || action.Plan.Steps[index].Status == "approval_required" {
			action.Plan.Steps[index].Status = "blocked"
		}
	}
}
