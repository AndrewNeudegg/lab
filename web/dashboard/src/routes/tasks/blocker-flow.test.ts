import { describe, expect, test } from 'bun:test';
import type { HomelabdTask } from '@homelab/shared';
import { goalBlockerFlow, taskIsGoalBlocker } from './blocker-flow';

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

const trace = {
  status: 'blocked',
  source_type: 'task_report',
  source_id: 'greport_grid',
  goal_id: 'goal_grid',
  phase_title: 'Ship virtual scrolling',
  blocking_task_id: 'task_blocker',
  reason: 'The task needs validation evidence before Autopilot can continue.',
  questions: ['Should the supervisor accept the current evidence or require comparison work?'],
  source_url: '/assistant?goal=goal_grid',
  blocking_task_url: '/tasks?task=task_blocker'
};

describe('Goal blocker task flow', () => {
  test('points a waiting task at the blocking task instead of offering Goal resume', () => {
    const flow = goalBlockerFlow(baseTask({ goal_blocker_trace: trace }));

    expect(flow?.role).toBe('waiting_on_blocking_task');
    expect(flow?.decisionLabel).toBe('Open the blocking task');
    expect(flow?.showBlockingTaskLink).toBe(true);
    expect(flow?.showResumeGoalAction).toBe(false);
  });

  test('turns a closed blocking task into an explicit Goal resume decision', () => {
    const task = baseTask({
      id: 'task_blocker',
      status: 'done',
      goal_blocker_trace: trace
    });
    const flow = goalBlockerFlow(task);

    expect(taskIsGoalBlocker(task)).toBe(true);
    expect(flow?.role).toBe('blocking_task');
    expect(flow?.decisionLabel).toBe('Decide whether to resume the Goal');
    expect(flow?.decisionDetail).toContain('already closed');
    expect(flow?.decisionDetail).toContain('reopen the task');
    expect(flow?.showBlockingTaskLink).toBe(false);
    expect(flow?.showResumeGoalAction).toBe(false);
    expect(flow?.decisionChoices.map((choice) => choice.title)).toEqual([
      'Accept current evidence',
      'Not acceptable: require more work',
      'Answer another way'
    ]);
    expect(flow?.decisionChoices[1].defaultInstruction).toContain('Not acceptable.');
    expect(flow?.decisionChoices[1].defaultInstruction).toContain('comparison work');
  });

  test('keeps a blocked blocker focused on retry or reopen repair', () => {
    const flow = goalBlockerFlow(
      baseTask({
        id: 'task_blocker',
        status: 'timed_out',
        goal_blocker_trace: trace
      })
    );

    expect(flow?.decisionLabel).toBe('Repair the blocker');
    expect(flow?.decisionDetail).toContain('Retry or Reopen');
    expect(flow?.showResumeGoalAction).toBe(false);
    expect(flow?.showCheckGoalAction).toBe(true);
  });

  test('returns no flow for ordinary tasks', () => {
    expect(goalBlockerFlow(baseTask())).toBeUndefined();
  });
});
