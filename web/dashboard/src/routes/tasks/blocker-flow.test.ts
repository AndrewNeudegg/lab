import { describe, expect, test } from 'bun:test';
import type { HomelabdTask } from '@homelab/shared';
import { goalBlockerFlow, normaliseGoalBlockerFlow, taskIsGoalBlocker } from './blocker-flow';

const baseTask = (overrides: Partial<HomelabdTask> = {}): HomelabdTask => ({
  id: 'task_waiting',
  goal_id: 'goal_grid',
  title: 'Build grid task',
  goal: 'Build the replacement grid',
  status: 'blocked',
  assigned_to: 'codex',
  priority: 5,
  created_at: '2026-05-11T10:00:00Z',
  updated_at: '2026-05-11T10:00:00Z',
  ...overrides
});

describe('Goal blocker task flow', () => {
  test('renders the API-provided flow instead of deriving blocker decisions in the browser', () => {
    const flow = goalBlockerFlow(
      baseTask({
        goal_blocker_trace: {
          status: 'blocked',
          source_type: 'task_report',
          source_id: 'greport_grid',
          goal_id: 'goal_grid',
          blocking_task_id: 'task_blocker',
          reason: 'The task needs validation evidence before Autopilot can continue.',
          questions: ['Should the supervisor accept the current evidence or require comparison work?'],
          flow: {
            role: 'waiting_on_blocking_task',
            title: 'Goal is blocked by task 5fff954d',
            decision_label: 'Open the blocking task',
            decision_detail: 'This task belongs to the blocked Goal, but the decision is on the linked blocking task.',
            show_blocking_task_link: true,
            show_resume_goal_action: false,
            show_check_goal_action: false
          }
        }
      })
    );

    expect(flow?.role).toBe('waiting_on_blocking_task');
    expect(flow?.decisionLabel).toBe('Open the blocking task');
    expect(flow?.showBlockingTaskLink).toBe(true);
    expect(flow?.showResumeGoalAction).toBe(false);
    expect(flow?.decisionChoices).toEqual([]);
  });

  test('keeps stale task-report questions out of the Goal answer flow unless the API marks them open', () => {
    const flow = goalBlockerFlow(
      baseTask({
        id: 'task_blocker',
        status: 'done',
        goal_blocker_trace: {
          status: 'blocked',
          source_type: 'task_report',
          source_id: 'greport_grid',
          goal_id: 'goal_grid',
          blocking_task_id: 'task_blocker',
          reason: 'Manual screen-reader UAT is missing.',
          questions: ['Should unsupported platforms be waived?'],
          flow: {
            role: 'blocking_task',
            title: 'This task is blocking Goal Autopilot',
            decision_label: 'Decide whether to resume the Goal',
            decision_detail: 'This task is already closed, but its report left a Goal-level blocker.',
            show_blocking_task_link: false,
            show_resume_goal_action: false,
            show_check_goal_action: true,
            decision_choices: [
              {
                id: 'accept_current',
                kind: 'resume',
                title: 'Accept current evidence',
                detail: 'Use when the blocker is acceptable and the Goal can continue.',
                action_label: 'Accept and resume'
              }
            ]
          }
        }
      })
    );

    expect(taskIsGoalBlocker(baseTask({ id: 'task_blocker', goal_blocker_trace: { blocking_task_id: 'task_blocker', status: 'blocked', source_type: 'task_report', source_id: 'r', reason: 'Blocked.' } }))).toBe(true);
    expect(flow?.role).toBe('blocking_task');
    expect(flow?.question).toBeUndefined();
    expect(flow?.decisionChoices[0].kind).toBe('resume');
  });

  test('normalises open Goal question choices from the API action model', () => {
    const flow = normaliseGoalBlockerFlow({
      role: 'goal_question',
      title: 'Goal is blocked by an open question',
      question: 'Should unsupported platforms be waived?',
      decision_label: 'Answer the Goal question',
      decision_detail: 'Record the product decision on the Goal.',
      show_blocking_task_link: false,
      show_resume_goal_action: false,
      show_check_goal_action: true,
      decision_choices: [
        {
          id: 'record_waiver',
          kind: 'answer',
          title: 'Record a waiver or deferment',
          detail: 'Use when the product owner accepts a deferment.',
          action_label: 'Record waiver and resume',
          default_instruction: 'Record the waiver.'
        }
      ]
    });

    expect(flow?.role).toBe('goal_question');
    expect(flow?.question).toBe('Should unsupported platforms be waived?');
    expect(flow?.decisionChoices[0].kind).toBe('answer');
    expect(flow?.decisionChoices[0].actionLabel).toBe('Record waiver and resume');
    expect(flow?.decisionChoices[0].defaultInstruction).toBe('Record the waiver.');
  });

  test('returns no flow when the API did not provide one', () => {
    expect(goalBlockerFlow(baseTask())).toBeUndefined();
  });
});
