package assistant

import (
	"strings"
	"testing"
	"time"
)

func TestDeriveGoalBlockerTraceUsesBlockingTaskReport(t *testing.T) {
	now := time.Date(2026, 5, 10, 13, 30, 0, 0, time.UTC)
	goal := Goal{
		ID:            "goal_1",
		Status:        GoalStatusBlocked,
		ExecutionMode: GoalExecutionModeAutopilot,
		Autopilot: &GoalAutopilot{
			Status: GoalAutopilotStatusBlocked,
		},
		Plan: &GoalPlan{
			Status:         GoalPlanStatusBlocked,
			CurrentPhaseID: "phase_03",
			Phases: []GoalPlanPhase{
				{ID: "phase_03", Title: "Feature parity", Status: GoalPlanPhaseStatusBlocked},
			},
		},
		UpdatedAt: now,
	}
	decisions := []GoalSupervisorDecision{{
		ID:         "gdec_1",
		GoalID:     "goal_1",
		Decision:   GoalSupervisorDecisionPauseBlocked,
		Summary:    "Goal plan is blocked.",
		Rationale:  "A linked task reported blockers or questions for the current plan phase.",
		StopReason: "Goal plan is blocked by the current phase.",
		CreatedAt:  now.Add(time.Minute),
	}}
	reports := []GoalTaskReport{{
		ID:             "greport_1",
		GoalID:         "goal_1",
		TaskID:         "task_20260510_132539_5fff954d",
		PhaseID:        "phase_03",
		Summary:        "Completed package publishing shape parity.",
		Blockers:       []string{"npm is not installed in this environment, so npm pack --dry-run could not be executed."},
		FollowUps:      []string{"Run npm pack --dry-run in an environment with npm installed."},
		ReviewDecision: "blocked_with_progress",
		CreatedAt:      now.Add(30 * time.Second),
	}}

	trace := DeriveGoalBlockerTrace(goal, decisions, reports)

	if trace == nil {
		t.Fatal("trace is nil, want blocker trace")
	}
	if trace.SourceType != GoalBlockerSourceTaskReport {
		t.Fatalf("source type = %q, want task_report", trace.SourceType)
	}
	if trace.BlockingTaskID != "task_20260510_132539_5fff954d" {
		t.Fatalf("blocking task = %q", trace.BlockingTaskID)
	}
	if trace.PhaseID != "phase_03" || trace.PhaseTitle != "Feature parity" {
		t.Fatalf("phase = %q/%q", trace.PhaseID, trace.PhaseTitle)
	}
	if !strings.Contains(trace.Reason, "npm is not installed") {
		t.Fatalf("reason = %q, want npm blocker", trace.Reason)
	}
	if !strings.Contains(trace.OperatorAction, "resolve or accept") {
		t.Fatalf("operator action = %q, want resolve or accept guidance", trace.OperatorAction)
	}
	if trace.BlockingTaskURL != "/tasks?task=task_20260510_132539_5fff954d" {
		t.Fatalf("blocking task URL = %q", trace.BlockingTaskURL)
	}
}

func TestDeriveGoalBlockerTraceUsesOpenQuestions(t *testing.T) {
	now := time.Date(2026, 5, 10, 13, 30, 0, 0, time.UTC)
	goal := Goal{
		ID:            "goal_1",
		Status:        GoalStatusActive,
		ExecutionMode: GoalExecutionModeAutopilot,
		OpenQuestions: []string{"Which feature slice should be next?"},
		Autopilot:     &GoalAutopilot{Status: GoalAutopilotStatusRunning},
		UpdatedAt:     now,
	}

	trace := DeriveGoalBlockerTrace(goal, []GoalSupervisorDecision{{
		ID:        "gdec_1",
		GoalID:    "goal_1",
		Decision:  GoalSupervisorDecisionAskQuestion,
		CreatedAt: now,
	}}, nil)

	if trace == nil {
		t.Fatal("trace is nil, want open-question blocker")
	}
	if trace.SourceType != GoalBlockerSourceOpenQuestions {
		t.Fatalf("source type = %q, want open_questions", trace.SourceType)
	}
	if !strings.Contains(trace.Reason, "Which feature slice") {
		t.Fatalf("reason = %q, want open question", trace.Reason)
	}
	if !strings.Contains(trace.OperatorAction, "Answer") {
		t.Fatalf("operator action = %q, want answer guidance", trace.OperatorAction)
	}
}

func TestDeriveGoalBlockerTracePrefersOpenQuestionOverSourceTaskReport(t *testing.T) {
	now := time.Date(2026, 5, 16, 20, 39, 0, 0, time.UTC)
	goal := Goal{
		ID:            "goal_1",
		Status:        GoalStatusBlocked,
		ExecutionMode: GoalExecutionModeAutopilot,
		OpenQuestions: []string{"Will the product owner waive unsupported platforms?"},
		Autopilot:     &GoalAutopilot{Status: GoalAutopilotStatusBlocked},
		Plan:          &GoalPlan{Status: GoalPlanStatusBlocked, CurrentPhaseID: "phase_final_audit"},
		UpdatedAt:     now,
	}

	trace := DeriveGoalBlockerTrace(goal, []GoalSupervisorDecision{{
		ID:         "gdec_1",
		GoalID:     "goal_1",
		Decision:   GoalSupervisorDecisionPauseBlocked,
		StopReason: "Task reported a blocker.",
		TaskID:     "task_source",
		CreatedAt:  now,
	}}, []GoalTaskReport{{
		ID:             "greport_task_source",
		GoalID:         "goal_1",
		TaskID:         "task_source",
		PhaseID:        "phase_final_audit",
		ReviewDecision: "blocked_with_progress",
		Blockers:       []string{"Manual assistive-technology UAT is missing."},
		Questions:      []string{"Will the product owner waive unsupported platforms?"},
		CreatedAt:      now,
	}})

	if trace == nil {
		t.Fatal("trace is nil, want open-question blocker")
	}
	if trace.SourceType != GoalBlockerSourceOpenQuestions {
		t.Fatalf("source type = %q, want open_questions", trace.SourceType)
	}
	if trace.BlockingTaskID != "" || trace.BlockingTaskURL != "" {
		t.Fatalf("blocking task = %q/%q, want no actionable blocking task", trace.BlockingTaskID, trace.BlockingTaskURL)
	}
	if trace.SourceTaskID != "task_source" || trace.SourceTaskURL != "/tasks?task=task_source" {
		t.Fatalf("source task = %q/%q, want source task provenance", trace.SourceTaskID, trace.SourceTaskURL)
	}
	if !strings.Contains(trace.Reason, "unanswered operator question") {
		t.Fatalf("reason = %q, want Goal question framing", trace.Reason)
	}
}

func TestDeriveGoalBlockerTraceReturnsNilForUnblockedGoal(t *testing.T) {
	trace := DeriveGoalBlockerTrace(Goal{
		ID:            "goal_1",
		Status:        GoalStatusActive,
		ExecutionMode: GoalExecutionModeAutopilot,
		Autopilot:     &GoalAutopilot{Status: GoalAutopilotStatusRunning},
	}, nil, nil)
	if trace != nil {
		t.Fatalf("trace = %#v, want nil for unblocked Goal", trace)
	}
}

func TestDeriveGoalBlockerTraceReturnsNilWhenCompletedGoalHasStaleQuestionsAndReports(t *testing.T) {
	now := time.Date(2026, 5, 12, 19, 48, 0, 0, time.UTC)
	goal := Goal{
		ID:            "goal_1",
		Status:        GoalStatusCompleted,
		ExecutionMode: GoalExecutionModeAutopilot,
		OpenQuestions: []string{"Should older evidence be accepted?"},
		Autopilot:     &GoalAutopilot{Status: GoalAutopilotStatusCompleted},
		Plan: &GoalPlan{
			Status: GoalPlanStatusCompleted,
			Phases: []GoalPlanPhase{
				{ID: "phase_03", Status: GoalPlanPhaseStatusCompleted},
				{ID: "phase_04", Status: GoalPlanPhaseStatusCompleted},
			},
		},
		UpdatedAt: now,
	}
	reports := []GoalTaskReport{
		{
			ID:             "greport_complete",
			GoalID:         "goal_1",
			TaskID:         "task_complete",
			PhaseID:        "phase_04",
			Summary:        "Goal is complete.",
			GoalComplete:   true,
			ReviewDecision: "verified_progress",
			CreatedAt:      now,
		},
		{
			ID:             "greport_old_blocker",
			GoalID:         "goal_1",
			TaskID:         "task_old",
			PhaseID:        "phase_03",
			Blockers:       []string{"npm was missing for an old dry-run."},
			ReviewDecision: "blocked_with_progress",
			CreatedAt:      now.Add(-48 * time.Hour),
		},
	}

	trace := DeriveGoalBlockerTrace(goal, []GoalSupervisorDecision{
		{
			ID:        "gdec_complete",
			GoalID:    "goal_1",
			Decision:  GoalSupervisorDecisionMarkComplete,
			CreatedAt: now,
		},
		{
			ID:        "gdec_old_blocker",
			GoalID:    "goal_1",
			Decision:  GoalSupervisorDecisionPauseBlocked,
			CreatedAt: now.Add(-48 * time.Hour),
		},
	}, reports)

	if trace != nil {
		t.Fatalf("trace = %#v, want nil because completed Goal supersedes stale blockers", trace)
	}
}

func TestDeriveGoalBlockerTraceIgnoresHistoricalReportWhenPlanIsNotBlocked(t *testing.T) {
	now := time.Date(2026, 5, 12, 19, 48, 0, 0, time.UTC)
	goal := Goal{
		ID:            "goal_1",
		Status:        GoalStatusActive,
		ExecutionMode: GoalExecutionModeAutopilot,
		Autopilot:     &GoalAutopilot{Status: GoalAutopilotStatusRunning},
		Plan: &GoalPlan{
			Status:         GoalPlanStatusActive,
			CurrentPhaseID: "phase_04",
			Phases: []GoalPlanPhase{
				{ID: "phase_03", Status: GoalPlanPhaseStatusCompleted},
				{ID: "phase_04", Status: GoalPlanPhaseStatusInProgress},
			},
		},
		UpdatedAt: now,
	}
	reports := []GoalTaskReport{{
		ID:             "greport_old_blocker",
		GoalID:         "goal_1",
		TaskID:         "task_old",
		PhaseID:        "phase_03",
		Blockers:       []string{"old blocker"},
		ReviewDecision: "blocked_with_progress",
		CreatedAt:      now.Add(-24 * time.Hour),
	}}

	trace := DeriveGoalBlockerTrace(goal, []GoalSupervisorDecision{{
		ID:        "gdec_create_next",
		GoalID:    "goal_1",
		Decision:  GoalSupervisorDecisionCreateTask,
		CreatedAt: now,
	}}, reports)

	if trace != nil {
		t.Fatalf("trace = %#v, want nil because old report is not the current blocker", trace)
	}
}

func TestDeriveGoalBlockerTraceClassifiesActionableGapAsAgentRepair(t *testing.T) {
	now := time.Date(2026, 5, 13, 7, 0, 0, 0, time.UTC)
	goal := Goal{
		ID:            "goal_1",
		Status:        GoalStatusBlocked,
		ExecutionMode: GoalExecutionModeAutopilot,
		Autopilot:     &GoalAutopilot{Status: GoalAutopilotStatusBlocked},
		Plan: &GoalPlan{
			Status:         GoalPlanStatusBlocked,
			CurrentPhaseID: "phase_final_audit",
			Phases: []GoalPlanPhase{
				{ID: "phase_final_audit", Title: "Final audit", Status: GoalPlanPhaseStatusBlocked},
			},
			Gaps: []GoalGap{{
				ID:            "ggap_scope",
				PhaseID:       "phase_final_audit",
				Area:          "public parity scope",
				Claim:         "Enterprise feature categories are under-scoped.",
				Severity:      GoalGapSeverityHigh,
				Status:        GoalGapStatusOpen,
				SuggestedTask: "Map every current Enterprise feature category to evidence, exclusion, or gap severity.",
				CreatedAt:     now,
				UpdatedAt:     now,
			}},
		},
		UpdatedAt: now,
	}
	reports := []GoalTaskReport{{
		ID:             "greport_1",
		GoalID:         "goal_1",
		TaskID:         "task_gap_fix",
		PhaseID:        "phase_final_audit",
		Summary:        "Delivery cleanliness is fixed, but scope gap remains.",
		Blockers:       []string{"Open high gap ggap_scope prevents credible completion."},
		FollowUps:      []string{"Map every current Enterprise feature category."},
		ReviewDecision: "needs_validation",
		GapIDs:         []string{"ggap_delivery"},
		CreatedAt:      now,
	}}

	trace := DeriveGoalBlockerTrace(goal, []GoalSupervisorDecision{{
		ID:        "gdec_blocked",
		GoalID:    "goal_1",
		Decision:  GoalSupervisorDecisionPauseBlocked,
		TaskID:    "task_gap_fix",
		CreatedAt: now,
	}}, reports)

	if trace == nil {
		t.Fatal("trace is nil, want agent-repair trace")
	}
	if trace.Status != GoalBlockerTraceStatusNeedsAgentRepair {
		t.Fatalf("status = %q, want agent repair", trace.Status)
	}
	if trace.Resolver != GoalBlockerResolverAgent || trace.HumanAction {
		t.Fatalf("resolver/human = %q/%v, want agent/false", trace.Resolver, trace.HumanAction)
	}
	if !strings.Contains(trace.NextAction, "Map every current Enterprise feature category") {
		t.Fatalf("next action = %q, want gap suggested task", trace.NextAction)
	}
	if !strings.Contains(trace.OperatorAction, "No human decision") {
		t.Fatalf("operator action = %q, want no-human guidance", trace.OperatorAction)
	}
}
