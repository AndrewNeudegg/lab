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
