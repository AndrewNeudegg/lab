package assistant

import (
	"sort"
	"strings"
	"time"
)

const (
	GoalBlockerTraceStatusBlocked          = "blocked"
	GoalBlockerTraceStatusNeedsAgentRepair = "needs_agent_repair"

	GoalBlockerResolverHuman    = "human"
	GoalBlockerResolverAgent    = "agent"
	GoalBlockerResolverExternal = "external"

	GoalBlockerSourceOpenQuestions = "open_questions"
	GoalBlockerSourceTaskReport    = "task_report"
	GoalBlockerSourceDecision      = "goal_decision"
	GoalBlockerSourcePlan          = "goal_plan"
	GoalBlockerSourceAutopilot     = "autopilot"

	GoalBlockerFlowRoleWaitingOnBlockingTask = "waiting_on_blocking_task"
	GoalBlockerFlowRoleBlockingTask          = "blocking_task"
	GoalBlockerFlowRoleAgentRepair           = "agent_repair"
	GoalBlockerFlowRoleGoalQuestion          = "goal_question"
	GoalBlockerFlowRoleGoalBlocked           = "goal_blocked"

	GoalBlockerDecisionKindResume = "resume"
	GoalBlockerDecisionKindReopen = "reopen"
	GoalBlockerDecisionKindCustom = "custom"
	GoalBlockerDecisionKindAnswer = "answer"
	GoalBlockerDecisionKindRetry  = "retry"
)

type GoalBlockerFlowContext struct {
	TaskID     string
	TaskStatus string
}

func DeriveGoalBlockerTrace(goal Goal, decisions []GoalSupervisorDecision, reports []GoalTaskReport) *GoalBlockerTrace {
	goal = NormalizeGoal(goal)
	if !goalIsBlockedForTrace(goal) {
		return nil
	}
	decisions = normalizeGoalDecisionsForTrace(decisions)
	reports = normalizeGoalReportsForTrace(reports)

	decision := latestBlockingGoalDecision(decisions)
	report := blockingGoalReportForTrace(goal, decision, reports)
	phaseID, phaseTitle := currentBlockedGoalPhase(goal, report)
	currentTaskID := currentAutopilotBlockingTaskID(goal)

	trace := GoalBlockerTrace{
		Status:         GoalBlockerTraceStatusBlocked,
		Resolver:       GoalBlockerResolverHuman,
		GoalID:         goal.ID,
		PhaseID:        phaseID,
		PhaseTitle:     phaseTitle,
		SourceURL:      "/assistant?goal=" + goal.ID,
		Blockers:       append([]string(nil), report.Blockers...),
		Questions:      append([]string(nil), report.Questions...),
		Evidence:       append([]string(nil), decision.Evidence...),
		FollowUps:      append([]string(nil), report.FollowUps...),
		ReviewDecision: report.ReviewDecision,
	}
	if decision.ID != "" {
		trace.DecisionID = decision.ID
		trace.Decision = decision.Decision
	}
	if report.TaskID != "" {
		trace.BlockingTaskID = report.TaskID
		trace.BlockingTaskURL = "/tasks?task=" + report.TaskID
	}

	switch {
	case len(goal.OpenQuestions) > 0:
		trace.SourceType = GoalBlockerSourceOpenQuestions
		trace.SourceID = goal.ID
		if report.TaskID != "" {
			trace.SourceTaskID = report.TaskID
			trace.SourceTaskURL = "/tasks?task=" + report.TaskID
			trace.BlockingTaskID = ""
			trace.BlockingTaskURL = ""
		}
		trace.Resolver = GoalBlockerResolverHuman
		trace.HumanAction = true
		trace.Questions = append([]string(nil), goal.OpenQuestions...)
		trace.Reason = "Goal has an unanswered operator question: " + goal.OpenQuestions[0]
		trace.NextAction = "Record an answer on the Goal so Autopilot can choose the next task with that decision in context."
		trace.OperatorAction = "Answer the Goal question, then resume Autopilot."
		trace.CreatedAt = latestGoalBlockerTime(decision.CreatedAt, goal.UpdatedAt)
	case currentTaskID != "" && report.TaskID != currentTaskID:
		trace.SourceType = GoalBlockerSourceAutopilot
		trace.SourceID = goal.ID
		trace.BlockingTaskID = currentTaskID
		trace.BlockingTaskURL = "/tasks?task=" + currentTaskID
		trace.Resolver = GoalBlockerResolverHuman
		trace.HumanAction = true
		trace.Blockers = nil
		trace.Questions = nil
		trace.FollowUps = nil
		trace.ReviewDecision = ""
		trace.Reason = currentAutopilotBlockerReason(goal)
		trace.NextAction = "Open task " + shortGoalBlockerID(currentTaskID) + " and resolve its task-level blocked state."
		trace.OperatorAction = "Open the current blocked task, then retry, reopen, review, or accept it through the normal task flow."
		trace.CreatedAt = latestGoalBlockerTime(goal.UpdatedAt, decision.CreatedAt)
	case report.TaskID != "" && (len(report.Blockers) > 0 || len(report.Questions) > 0 || report.ReviewDecision != ""):
		trace.SourceType = GoalBlockerSourceTaskReport
		trace.SourceID = report.ID
		if gap, ok := taskReportAgentRepairGap(goal, report); ok {
			trace.Status = GoalBlockerTraceStatusNeedsAgentRepair
			trace.Resolver = GoalBlockerResolverAgent
			trace.HumanAction = false
			trace.Reason = agentRepairBlockerReason(report, gap)
			trace.NextAction = agentRepairNextAction(gap)
			trace.OperatorAction = "No human decision is required. Autopilot should record this as repair work and create the next gap-fix task."
		} else {
			trace.Resolver = goalBlockerResolverForReport(report)
			trace.HumanAction = trace.Resolver == GoalBlockerResolverHuman
			trace.Reason = taskReportBlockerReason(report)
			trace.NextAction = taskReportNextAction(report)
			trace.OperatorAction = taskReportOperatorAction(report)
		}
		trace.CreatedAt = latestGoalBlockerTime(report.CreatedAt, decision.CreatedAt)
	case decision.ID != "":
		trace.SourceType = GoalBlockerSourceDecision
		trace.SourceID = decision.ID
		trace.Resolver = GoalBlockerResolverHuman
		trace.HumanAction = true
		trace.Reason = firstNonEmptyString(decision.StopReason, decision.Rationale, decision.Summary, "Goal supervisor paused Autopilot.")
		trace.NextAction = decisionOperatorAction(decision)
		trace.OperatorAction = decisionOperatorAction(decision)
		trace.CreatedAt = latestGoalBlockerTime(decision.CreatedAt, goal.UpdatedAt)
	case goal.Autopilot != nil && len(goal.Autopilot.StopReasons) > 0:
		trace.SourceType = GoalBlockerSourceAutopilot
		trace.SourceID = goal.ID
		trace.Resolver = GoalBlockerResolverHuman
		trace.HumanAction = true
		trace.Reason = goal.Autopilot.StopReasons[0]
		trace.NextAction = "Resolve or accept the Autopilot stop reason."
		trace.OperatorAction = "Resolve or accept the Autopilot stop reason, then resume Autopilot."
		trace.CreatedAt = latestGoalBlockerTime(goal.UpdatedAt)
	default:
		trace.SourceType = GoalBlockerSourcePlan
		trace.SourceID = goal.ID
		trace.Resolver = GoalBlockerResolverHuman
		trace.HumanAction = true
		trace.Reason = "Goal plan is blocked by the current phase."
		trace.NextAction = "Inspect the current phase and blocking task reports."
		trace.OperatorAction = "Inspect the current phase and blocking task reports, then revise the plan or resume Autopilot."
		trace.CreatedAt = latestGoalBlockerTime(goal.UpdatedAt)
	}

	if len(trace.Evidence) == 0 && len(report.ReviewEvidence) > 0 {
		trace.Evidence = append([]string(nil), report.ReviewEvidence...)
	}
	return GoalBlockerTraceWithFlow(NormalizeGoalBlockerTrace(&trace), GoalBlockerFlowContext{})
}

func NormalizeGoalBlockerTrace(trace *GoalBlockerTrace) *GoalBlockerTrace {
	if trace == nil {
		return nil
	}
	value := *trace
	value.Status = strings.TrimSpace(value.Status)
	if value.Status == "" {
		value.Status = GoalBlockerTraceStatusBlocked
	}
	value.Resolver = strings.TrimSpace(value.Resolver)
	if value.Resolver == "" {
		value.Resolver = GoalBlockerResolverHuman
	}
	value.SourceType = strings.TrimSpace(value.SourceType)
	value.SourceID = strings.TrimSpace(value.SourceID)
	value.SourceTaskID = strings.TrimSpace(value.SourceTaskID)
	value.SourceTaskURL = strings.TrimSpace(value.SourceTaskURL)
	value.DecisionID = strings.TrimSpace(value.DecisionID)
	value.Decision = strings.TrimSpace(value.Decision)
	value.GoalID = strings.TrimSpace(value.GoalID)
	value.PhaseID = strings.TrimSpace(value.PhaseID)
	value.PhaseTitle = strings.TrimSpace(value.PhaseTitle)
	value.BlockingTaskID = strings.TrimSpace(value.BlockingTaskID)
	value.ReviewDecision = strings.TrimSpace(value.ReviewDecision)
	value.Reason = strings.TrimSpace(value.Reason)
	value.NextAction = strings.TrimSpace(value.NextAction)
	value.OperatorAction = strings.TrimSpace(value.OperatorAction)
	value.SourceURL = strings.TrimSpace(value.SourceURL)
	value.BlockingTaskURL = strings.TrimSpace(value.BlockingTaskURL)
	value.Blockers = normalizeRunStringList(value.Blockers, 12)
	value.Questions = normalizeRunStringList(value.Questions, 12)
	value.Evidence = normalizeRunStringList(value.Evidence, 12)
	value.FollowUps = normalizeRunStringList(value.FollowUps, 12)
	if value.CreatedAt != nil {
		created := value.CreatedAt.UTC()
		if created.IsZero() {
			value.CreatedAt = nil
		} else {
			value.CreatedAt = &created
		}
	}
	if value.SourceType == "" || value.Reason == "" {
		return nil
	}
	return &value
}

func GoalBlockerTraceWithFlow(trace *GoalBlockerTrace, ctx GoalBlockerFlowContext) *GoalBlockerTrace {
	trace = NormalizeGoalBlockerTrace(trace)
	if trace == nil {
		return nil
	}
	value := *trace
	value.Flow = deriveGoalBlockerFlow(value, ctx)
	return &value
}

func deriveGoalBlockerFlow(trace GoalBlockerTrace, ctx GoalBlockerFlowContext) *GoalBlockerFlow {
	goalID := strings.TrimSpace(trace.GoalID)
	taskID := strings.TrimSpace(ctx.TaskID)
	taskStatus := strings.TrimSpace(ctx.TaskStatus)
	taskIsBlocker := taskID != "" && strings.TrimSpace(trace.BlockingTaskID) == taskID
	nextAction := firstNonEmptyString(
		trace.NextAction,
		trace.OperatorAction,
		firstString(trace.Blockers),
		firstString(trace.FollowUps),
		"Create the next repair task and rerun the relevant challenge.",
	)

	if trace.SourceType == GoalBlockerSourceOpenQuestions {
		question := firstNonEmptyString(firstString(trace.Questions), trace.Reason)
		return &GoalBlockerFlow{
			Role:                 GoalBlockerFlowRoleGoalQuestion,
			Title:                "Goal is blocked by an open question",
			Question:             question,
			DecisionLabel:        "Answer the Goal question",
			DecisionDetail:       "Record the product decision on the Goal so Autopilot can continue with that answer.",
			ShowBlockingTaskLink: false,
			ShowResumeGoalAction: false,
			ShowCheckGoalAction:  goalID != "",
			DecisionChoices:      goalQuestionDecisionChoices(question),
		}
	}

	if trace.Resolver == GoalBlockerResolverAgent {
		title := "Goal needs autonomous repair"
		if taskIsBlocker {
			title = "Autopilot found repair work"
		}
		return &GoalBlockerFlow{
			Role:                 GoalBlockerFlowRoleAgentRepair,
			Title:                title,
			DecisionLabel:        "No human decision needed",
			DecisionDetail:       nextAction,
			ShowBlockingTaskLink: trace.BlockingTaskID != "" && !taskIsBlocker,
			ShowResumeGoalAction: true,
			ShowCheckGoalAction:  goalID != "",
		}
	}

	if taskID == "" {
		if trace.BlockingTaskID != "" {
			return &GoalBlockerFlow{
				Role:                 GoalBlockerFlowRoleGoalBlocked,
				Title:                "Goal is blocked by task " + shortGoalBlockerID(trace.BlockingTaskID),
				DecisionLabel:        "Open the blocking task",
				DecisionDetail:       "The decision belongs to the linked task or its review record. This Goal does not have an open question to answer.",
				ShowBlockingTaskLink: true,
				ShowResumeGoalAction: false,
				ShowCheckGoalAction:  goalID != "",
			}
		}
		return &GoalBlockerFlow{
			Role:                 GoalBlockerFlowRoleGoalBlocked,
			Title:                "Goal Autopilot is blocked",
			DecisionLabel:        "Inspect the Goal blocker",
			DecisionDetail:       "The Goal is blocked without a single blocking task. Resolve the blocker source, revise the plan, or resume Autopilot after the issue is cleared.",
			ShowBlockingTaskLink: false,
			ShowResumeGoalAction: false,
			ShowCheckGoalAction:  goalID != "",
		}
	}

	if !taskIsBlocker {
		if trace.BlockingTaskID != "" {
			return &GoalBlockerFlow{
				Role:                 GoalBlockerFlowRoleWaitingOnBlockingTask,
				Title:                "Goal is blocked by task " + shortGoalBlockerID(trace.BlockingTaskID),
				DecisionLabel:        "Open the blocking task",
				DecisionDetail:       "This task belongs to the blocked Goal, but the decision is on the linked blocking task.",
				ShowBlockingTaskLink: true,
				ShowResumeGoalAction: false,
				ShowCheckGoalAction:  false,
			}
		}
		return &GoalBlockerFlow{
			Role:                 GoalBlockerFlowRoleGoalBlocked,
			Title:                "Goal Autopilot is blocked",
			DecisionLabel:        "Inspect the Goal blocker",
			DecisionDetail:       "The Goal is blocked without a single blocking task. Open the Goal to inspect the current blocker.",
			ShowBlockingTaskLink: false,
			ShowResumeGoalAction: false,
			ShowCheckGoalAction:  goalID != "",
		}
	}

	if goalBlockerTaskStatusIsTerminalAccepted(taskStatus) {
		return &GoalBlockerFlow{
			Role:                 GoalBlockerFlowRoleBlockingTask,
			Title:                "This task is blocking Goal Autopilot",
			DecisionLabel:        "Decide whether to resume the Goal",
			DecisionDetail:       "This task is already closed, but its report left a Goal-level blocker. Choose whether the current evidence is acceptable, or reopen the task with the missing requirement.",
			ShowBlockingTaskLink: false,
			ShowResumeGoalAction: false,
			ShowCheckGoalAction:  goalID != "",
			DecisionChoices:      terminalGoalBlockerDecisionChoices(trace),
		}
	}

	if goalBlockerTaskStatusIsReviewable(taskStatus) {
		return &GoalBlockerFlow{
			Role:                 GoalBlockerFlowRoleBlockingTask,
			Title:                "This task is blocking Goal Autopilot",
			DecisionLabel:        "Verify or reject this result",
			DecisionDetail:       "Review the task output. Accept it if it resolves the blocker, or reopen it with the missing evidence or product decision.",
			ShowBlockingTaskLink: false,
			ShowResumeGoalAction: false,
			ShowCheckGoalAction:  goalID != "",
		}
	}

	if goalBlockerTaskStatusIsRepairable(taskStatus) {
		return &GoalBlockerFlow{
			Role:                 GoalBlockerFlowRoleBlockingTask,
			Title:                "This task is blocking Goal Autopilot",
			DecisionLabel:        "Repair the blocker",
			DecisionDetail:       "Use Retry or Reopen with the specific evidence, dependency, or operator decision needed before the Goal can continue.",
			ShowBlockingTaskLink: false,
			ShowResumeGoalAction: false,
			ShowCheckGoalAction:  goalID != "",
			DecisionChoices:      repairableGoalBlockerDecisionChoices(trace),
		}
	}

	return &GoalBlockerFlow{
		Role:                 GoalBlockerFlowRoleBlockingTask,
		Title:                "This task is blocking Goal Autopilot",
		DecisionLabel:        "Watch this task complete",
		DecisionDetail:       "Autopilot is waiting for this task to finish. When it reaches review or a blocked state, the task actions become the decision point.",
		ShowBlockingTaskLink: false,
		ShowResumeGoalAction: false,
		ShowCheckGoalAction:  goalID != "",
	}
}

func goalQuestionDecisionChoices(question string) []GoalBlockerDecisionChoice {
	prompt := firstNonEmptyString(question, "the open Goal question")
	return []GoalBlockerDecisionChoice{
		{
			ID:                 "require_full",
			Kind:               GoalBlockerDecisionKindAnswer,
			Title:              "Require the full requirement",
			Detail:             "Use when the missing evidence or stricter path remains required before completion.",
			ActionLabel:        "Record answer and resume",
			DefaultInstruction: "The full requirement remains in scope for this Goal. Do not claim the Goal complete until this is satisfied with evidence: " + prompt,
		},
		{
			ID:                 "record_waiver",
			Kind:               GoalBlockerDecisionKindAnswer,
			Title:              "Record a waiver or deferment",
			Detail:             "Use when the product owner accepts that some requirement is unsupported or deferred for now.",
			ActionLabel:        "Record waiver and resume",
			DefaultInstruction: "Record an explicit product-owner waiver or deferment for the unsupported or untested requirement, then continue Autopilot using that decision as acceptance context: " + prompt,
		},
		{
			ID:          "custom",
			Kind:        GoalBlockerDecisionKindAnswer,
			Title:       "Answer another way",
			Detail:      "Write the exact product decision or operator instruction that should guide the next run.",
			ActionLabel: "Record custom answer",
		},
	}
}

func terminalGoalBlockerDecisionChoices(trace GoalBlockerTrace) []GoalBlockerDecisionChoice {
	return []GoalBlockerDecisionChoice{
		{
			ID:          "accept_current",
			Kind:        GoalBlockerDecisionKindResume,
			Title:       "Accept current evidence",
			Detail:      "Use when the blocker is acceptable and the Goal can continue.",
			ActionLabel: "Accept and resume",
		},
		{
			ID:                 "require_more",
			Kind:               GoalBlockerDecisionKindReopen,
			Title:              "Not acceptable: require more work",
			Detail:             "Use when the answer is no: reopen the task with a clear missing requirement.",
			ActionLabel:        "Reopen with this answer",
			DefaultInstruction: requireMoreGoalBlockerInstruction(trace),
		},
		{
			ID:          "custom",
			Kind:        GoalBlockerDecisionKindCustom,
			Title:       "Answer another way",
			Detail:      "Write the exact instruction or product decision that should guide the next run.",
			ActionLabel: "Reopen with custom answer",
		},
	}
}

func repairableGoalBlockerDecisionChoices(trace GoalBlockerTrace) []GoalBlockerDecisionChoice {
	return []GoalBlockerDecisionChoice{
		{
			ID:          "retry_current",
			Kind:        GoalBlockerDecisionKindRetry,
			Title:       "Retry current task",
			Detail:      "Use when the blocker is likely transient or the same worker can continue from the current state.",
			ActionLabel: "Retry task",
		},
		{
			ID:                 "retry_with_instruction",
			Kind:               GoalBlockerDecisionKindRetry,
			Title:              "Retry with instruction",
			Detail:             "Use when the worker needs a specific repair instruction before the Goal can continue.",
			ActionLabel:        "Retry with instruction",
			DefaultInstruction: retryGoalBlockerInstruction(trace),
		},
		{
			ID:                 "reopen_with_direction",
			Kind:               GoalBlockerDecisionKindReopen,
			Title:              "Reopen with new direction",
			Detail:             "Use when the task needs a changed requirement or product decision, not just another retry.",
			ActionLabel:        "Reopen task",
			DefaultInstruction: requireMoreGoalBlockerInstruction(trace),
		},
	}
}

func retryGoalBlockerInstruction(trace GoalBlockerTrace) string {
	reason := firstNonEmptyString(trace.Reason, trace.NextAction, firstString(trace.Blockers), "the current Goal blocker")
	return strings.Join([]string{
		"Retry this task and repair the current blocker:",
		reason,
		"Preserve any useful work already produced, capture the diff successfully, and report validation evidence before returning to review.",
	}, " ")
}

func requireMoreGoalBlockerInstruction(trace GoalBlockerTrace) string {
	question := firstNonEmptyString(firstString(trace.Questions), trace.Reason, "the Goal blocker")
	return strings.Join([]string{
		"Not acceptable.",
		"Require the stricter path before resuming Autopilot: " + question,
		"Do not close or resume the Goal until the missing evidence, comparison, implementation, or product decision is completed and reported back with validation.",
	}, " ")
}

func goalBlockerTaskStatusIsTerminalAccepted(status string) bool {
	switch status {
	case "done", "cancelled":
		return true
	default:
		return false
	}
}

func goalBlockerTaskStatusIsReviewable(status string) bool {
	switch status {
	case "awaiting_verification", "awaiting_approval", "ready_for_review", "no_change_required":
		return true
	default:
		return false
	}
}

func goalBlockerTaskStatusIsRepairable(status string) bool {
	switch status {
	case "blocked", "timed_out", "failed", "conflict_resolution":
		return true
	default:
		return false
	}
}

func goalIsBlockedForTrace(goal Goal) bool {
	if goal.Status == GoalStatusCompleted || goal.Status == GoalStatusArchived {
		return false
	}
	if goal.Autopilot != nil && goal.Autopilot.Status == GoalAutopilotStatusCompleted {
		return false
	}
	if goal.Plan != nil && NormalizeGoalPlan(*goal.Plan).Status == GoalPlanStatusCompleted {
		return false
	}
	if len(goal.OpenQuestions) > 0 || goal.Status == GoalStatusBlocked {
		return true
	}
	if goal.Autopilot != nil && goal.Autopilot.Status == GoalAutopilotStatusBlocked {
		return true
	}
	return goalPlanBlockedForTrace(goal)
}

func normalizeGoalDecisionsForTrace(decisions []GoalSupervisorDecision) []GoalSupervisorDecision {
	out := make([]GoalSupervisorDecision, 0, len(decisions))
	for _, decision := range decisions {
		out = append(out, NormalizeGoalSupervisorDecision(decision))
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out
}

func normalizeGoalReportsForTrace(reports []GoalTaskReport) []GoalTaskReport {
	out := make([]GoalTaskReport, 0, len(reports))
	for _, report := range reports {
		out = append(out, NormalizeGoalTaskReport(report))
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out
}

func latestBlockingGoalDecision(decisions []GoalSupervisorDecision) GoalSupervisorDecision {
	if len(decisions) == 0 {
		return GoalSupervisorDecision{}
	}
	latest := decisions[0]
	if latest.Decision == GoalSupervisorDecisionPauseBlocked || latest.Decision == GoalSupervisorDecisionAskQuestion {
		return latest
	}
	return GoalSupervisorDecision{}
}

func blockingGoalReportForTrace(goal Goal, decision GoalSupervisorDecision, reports []GoalTaskReport) GoalTaskReport {
	if decision.TaskID != "" {
		for _, report := range reports {
			if report.TaskID == decision.TaskID {
				return report
			}
		}
	}
	if !goalPlanBlockedForTrace(goal) {
		return GoalTaskReport{}
	}
	currentPhaseID := ""
	if goal.Plan != nil {
		currentPhaseID = goal.Plan.CurrentPhaseID
	}
	for _, report := range reports {
		if currentPhaseID != "" && report.PhaseID != "" && report.PhaseID != currentPhaseID {
			continue
		}
		if goalReportBlocksAutopilot(report) {
			return report
		}
	}
	for _, report := range reports {
		if goalReportBlocksAutopilot(report) {
			return report
		}
	}
	return GoalTaskReport{}
}

func currentAutopilotBlockingTaskID(goal Goal) string {
	if goal.Autopilot == nil {
		return ""
	}
	taskID := strings.TrimSpace(goal.Autopilot.CurrentTaskID)
	if taskID == "" {
		return ""
	}
	if goal.Autopilot.Status != GoalAutopilotStatusBlocked && goal.Status != GoalStatusBlocked {
		return ""
	}
	if len(goal.Autopilot.StopReasons) == 0 {
		return taskID
	}
	for _, reason := range goal.Autopilot.StopReasons {
		reason = strings.TrimSpace(reason)
		if reason == "" {
			continue
		}
		if strings.Contains(reason, taskID) || strings.Contains(reason, shortGoalBlockerID(taskID)) {
			return taskID
		}
		lower := strings.ToLower(reason)
		if strings.Contains(lower, "linked task") || strings.Contains(lower, "current task") {
			return taskID
		}
	}
	return ""
}

func currentAutopilotBlockerReason(goal Goal) string {
	if goal.Autopilot != nil {
		for _, reason := range goal.Autopilot.StopReasons {
			if reason = strings.TrimSpace(reason); reason != "" {
				return reason
			}
		}
		if taskID := strings.TrimSpace(goal.Autopilot.CurrentTaskID); taskID != "" {
			return "Autopilot is blocked by current task " + shortGoalBlockerID(taskID) + "."
		}
	}
	return "Autopilot is blocked by its current task."
}

func goalPlanBlockedForTrace(goal Goal) bool {
	if goal.Plan == nil {
		return false
	}
	return NormalizeGoalPlan(*goal.Plan).Status == GoalPlanStatusBlocked
}

func goalReportBlocksAutopilot(report GoalTaskReport) bool {
	if len(report.Blockers) > 0 || len(report.Questions) > 0 {
		return true
	}
	switch strings.TrimSpace(report.ReviewDecision) {
	case "blocked_with_progress", "needs_validation", "misaligned", "insufficient_evidence":
		return true
	default:
		return false
	}
}

func taskReportAgentRepairGap(goal Goal, report GoalTaskReport) (GoalGap, bool) {
	report = NormalizeGoalTaskReport(report)
	if strings.EqualFold(report.BlockerResolver, GoalBlockerResolverHuman) || strings.EqualFold(report.BlockerResolver, GoalBlockerResolverExternal) {
		return GoalGap{}, false
	}
	if len(report.Questions) > 0 {
		return GoalGap{}, false
	}
	if report.Challenge != nil && report.Challenge.Verdict == GoalChallengeVerdictNeedsUser {
		return GoalGap{}, false
	}
	gap, ok := firstOpenActionableGoalGap(goal.Plan, report.GapIDs)
	if !ok {
		return GoalGap{}, false
	}
	if len(report.Blockers) == 0 && report.ReviewDecision == "" && len(report.FollowUps) == 0 {
		return GoalGap{}, false
	}
	return gap, true
}

func firstOpenActionableGoalGap(plan *GoalPlan, attemptedGapIDs []string) (GoalGap, bool) {
	if plan == nil {
		return GoalGap{}, false
	}
	planValue := NormalizeGoalPlan(*plan)
	var fallback GoalGap
	for _, severity := range []string{GoalGapSeverityCritical, GoalGapSeverityHigh, GoalGapSeverityMedium, GoalGapSeverityLow} {
		for _, gap := range planValue.Gaps {
			if gap.Status != GoalGapStatusOpen || gap.Severity != severity {
				continue
			}
			if strings.TrimSpace(gap.SuggestedTask) == "" && strings.TrimSpace(gap.Claim) == "" {
				continue
			}
			if fallback.ID == "" {
				fallback = gap
			}
			if !stringSliceContains(attemptedGapIDs, gap.ID) {
				return gap, true
			}
		}
	}
	if fallback.ID != "" {
		return fallback, true
	}
	return GoalGap{}, false
}

func agentRepairBlockerReason(report GoalTaskReport, gap GoalGap) string {
	if gap.ID != "" {
		return "Autopilot found more repair work: open " + firstNonEmptyString(gap.Severity, "unscored") + " gap " + gap.ID + " prevents Goal completion."
	}
	if len(report.Blockers) > 0 {
		return "Autopilot found more repair work: " + report.Blockers[0]
	}
	return "Autopilot found more repair work before the Goal can complete."
}

func agentRepairNextAction(gap GoalGap) string {
	return firstNonEmptyString(gap.SuggestedTask, gap.Claim, "Create the next gap-fix task, then rerun the relevant challenge.")
}

func goalBlockerResolverForReport(report GoalTaskReport) string {
	report = NormalizeGoalTaskReport(report)
	switch strings.TrimSpace(report.BlockerResolver) {
	case GoalBlockerResolverAgent, GoalBlockerResolverHuman, GoalBlockerResolverExternal:
		return report.BlockerResolver
	}
	for _, question := range report.Questions {
		if strings.TrimSpace(question) != "" {
			return GoalBlockerResolverHuman
		}
	}
	for _, blocker := range report.Blockers {
		text := strings.ToLower(blocker)
		switch {
		case strings.Contains(text, "credential"),
			strings.Contains(text, "secret"),
			strings.Contains(text, "permission"),
			strings.Contains(text, "approval"),
			strings.Contains(text, "operator"),
			strings.Contains(text, "human"):
			return GoalBlockerResolverHuman
		case strings.Contains(text, "rate limit"),
			strings.Contains(text, "429"),
			strings.Contains(text, "network"),
			strings.Contains(text, "service unavailable"),
			strings.Contains(text, "dependency"):
			return GoalBlockerResolverExternal
		}
	}
	return GoalBlockerResolverHuman
}

func currentBlockedGoalPhase(goal Goal, report GoalTaskReport) (string, string) {
	phaseID := firstNonEmptyString(report.PhaseID)
	if phaseID == "" && goal.Plan != nil {
		phaseID = goal.Plan.CurrentPhaseID
	}
	if goal.Plan == nil {
		return phaseID, ""
	}
	for _, phase := range goal.Plan.Phases {
		if phase.ID == phaseID || (phaseID == "" && phase.Status == GoalPlanPhaseStatusBlocked) {
			return phase.ID, phase.Title
		}
	}
	return phaseID, ""
}

func taskReportBlockerReason(report GoalTaskReport) string {
	if len(report.Blockers) > 0 {
		return "Task " + shortGoalBlockerID(report.TaskID) + " reported blocker: " + report.Blockers[0]
	}
	if len(report.Questions) > 0 {
		return "Task " + shortGoalBlockerID(report.TaskID) + " needs an operator answer: " + report.Questions[0]
	}
	if report.ReviewSummary != "" {
		return "Task " + shortGoalBlockerID(report.TaskID) + " review blocked progress: " + report.ReviewSummary
	}
	switch report.ReviewDecision {
	case "needs_validation":
		return "Task " + shortGoalBlockerID(report.TaskID) + " needs validation evidence before Autopilot can continue."
	case "insufficient_evidence":
		return "Task " + shortGoalBlockerID(report.TaskID) + " did not provide enough evidence for Goal progress."
	case "misaligned":
		return "Task " + shortGoalBlockerID(report.TaskID) + " did not clearly align with the Goal."
	default:
		return "Task " + shortGoalBlockerID(report.TaskID) + " blocked the current Goal phase."
	}
}

func taskReportOperatorAction(report GoalTaskReport) string {
	if len(report.Questions) > 0 {
		return "Answer the task question, then resume Autopilot."
	}
	if len(report.Blockers) > 0 {
		return "Open the blocking task, resolve or accept the blocker, then resume Autopilot."
	}
	switch report.ReviewDecision {
	case "needs_validation":
		return "Complete the missing validation or accept that it is not required, then resume Autopilot."
	case "insufficient_evidence", "misaligned":
		return "Review the task evidence, revise the plan or task direction, then resume Autopilot."
	default:
		return "Review the blocking task and decide whether to fix, accept, or revise the Goal plan."
	}
}

func taskReportNextAction(report GoalTaskReport) string {
	if strings.TrimSpace(report.NextAction) != "" {
		return report.NextAction
	}
	if len(report.Questions) > 0 {
		return "Answer the task question."
	}
	if len(report.Blockers) > 0 {
		return firstNonEmptyString(report.Blockers[0], "Resolve or accept the reported blocker.")
	}
	switch report.ReviewDecision {
	case "needs_validation":
		return "Provide missing validation or reject the task with a stricter instruction."
	case "insufficient_evidence", "misaligned":
		return "Review the evidence and choose whether to revise the plan or rerun the task."
	default:
		return "Review the blocking task and decide the next action."
	}
}

func decisionOperatorAction(decision GoalSupervisorDecision) string {
	switch decision.Decision {
	case GoalSupervisorDecisionAskQuestion:
		return "Answer the open Goal question, then resume Autopilot."
	case GoalSupervisorDecisionRevisePlan:
		return "Revise the Goal plan, then resume Autopilot."
	default:
		return "Resolve the supervisor stop reason, then resume Autopilot."
	}
}

func latestGoalBlockerTime(values ...time.Time) *time.Time {
	var latest time.Time
	for _, value := range values {
		if value.IsZero() {
			continue
		}
		if latest.IsZero() || value.After(latest) {
			latest = value
		}
	}
	if latest.IsZero() {
		return nil
	}
	latest = latest.UTC()
	return &latest
}

func shortGoalBlockerID(id string) string {
	id = strings.TrimSpace(id)
	if len(id) <= 8 {
		return id
	}
	return id[len(id)-8:]
}

func stringSliceContains(values []string, want string) bool {
	want = strings.TrimSpace(want)
	if want == "" {
		return false
	}
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), want) {
			return true
		}
	}
	return false
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func firstString(values []string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
