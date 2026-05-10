package assistant

import (
	"sort"
	"strings"
	"time"
)

const (
	GoalBlockerTraceStatusBlocked = "blocked"

	GoalBlockerSourceOpenQuestions = "open_questions"
	GoalBlockerSourceTaskReport    = "task_report"
	GoalBlockerSourceDecision      = "goal_decision"
	GoalBlockerSourcePlan          = "goal_plan"
	GoalBlockerSourceAutopilot     = "autopilot"
)

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

	trace := GoalBlockerTrace{
		Status:         GoalBlockerTraceStatusBlocked,
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
	case len(goal.OpenQuestions) > 0 && (decision.Decision == GoalSupervisorDecisionAskQuestion || report.TaskID == ""):
		trace.SourceType = GoalBlockerSourceOpenQuestions
		trace.SourceID = goal.ID
		trace.Questions = append([]string(nil), goal.OpenQuestions...)
		trace.Reason = "Goal has unanswered operator questions: " + goal.OpenQuestions[0]
		trace.OperatorAction = "Answer the Goal question, then resume Autopilot."
		trace.CreatedAt = latestGoalBlockerTime(decision.CreatedAt, goal.UpdatedAt)
	case report.TaskID != "" && (len(report.Blockers) > 0 || len(report.Questions) > 0 || report.ReviewDecision != ""):
		trace.SourceType = GoalBlockerSourceTaskReport
		trace.SourceID = report.ID
		trace.Reason = taskReportBlockerReason(report)
		trace.OperatorAction = taskReportOperatorAction(report)
		trace.CreatedAt = latestGoalBlockerTime(report.CreatedAt, decision.CreatedAt)
	case decision.ID != "":
		trace.SourceType = GoalBlockerSourceDecision
		trace.SourceID = decision.ID
		trace.Reason = firstNonEmptyString(decision.StopReason, decision.Rationale, decision.Summary, "Goal supervisor paused Autopilot.")
		trace.OperatorAction = decisionOperatorAction(decision)
		trace.CreatedAt = latestGoalBlockerTime(decision.CreatedAt, goal.UpdatedAt)
	case goal.Autopilot != nil && len(goal.Autopilot.StopReasons) > 0:
		trace.SourceType = GoalBlockerSourceAutopilot
		trace.SourceID = goal.ID
		trace.Reason = goal.Autopilot.StopReasons[0]
		trace.OperatorAction = "Resolve or accept the Autopilot stop reason, then resume Autopilot."
		trace.CreatedAt = latestGoalBlockerTime(goal.UpdatedAt)
	default:
		trace.SourceType = GoalBlockerSourcePlan
		trace.SourceID = goal.ID
		trace.Reason = "Goal plan is blocked by the current phase."
		trace.OperatorAction = "Inspect the current phase and blocking task reports, then revise the plan or resume Autopilot."
		trace.CreatedAt = latestGoalBlockerTime(goal.UpdatedAt)
	}

	if len(trace.Evidence) == 0 && len(report.ReviewEvidence) > 0 {
		trace.Evidence = append([]string(nil), report.ReviewEvidence...)
	}
	return NormalizeGoalBlockerTrace(&trace)
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
	value.SourceType = strings.TrimSpace(value.SourceType)
	value.SourceID = strings.TrimSpace(value.SourceID)
	value.DecisionID = strings.TrimSpace(value.DecisionID)
	value.Decision = strings.TrimSpace(value.Decision)
	value.GoalID = strings.TrimSpace(value.GoalID)
	value.PhaseID = strings.TrimSpace(value.PhaseID)
	value.PhaseTitle = strings.TrimSpace(value.PhaseTitle)
	value.BlockingTaskID = strings.TrimSpace(value.BlockingTaskID)
	value.ReviewDecision = strings.TrimSpace(value.ReviewDecision)
	value.Reason = strings.TrimSpace(value.Reason)
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

func goalIsBlockedForTrace(goal Goal) bool {
	if len(goal.OpenQuestions) > 0 || goal.Status == GoalStatusBlocked {
		return true
	}
	if goal.Autopilot != nil && goal.Autopilot.Status == GoalAutopilotStatusBlocked {
		return true
	}
	return goal.Plan != nil && goal.Plan.Status == GoalPlanStatusBlocked
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
	for _, decision := range decisions {
		if decision.Decision == GoalSupervisorDecisionPauseBlocked || decision.Decision == GoalSupervisorDecisionAskQuestion {
			return decision
		}
	}
	if len(decisions) > 0 {
		return decisions[0]
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

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
