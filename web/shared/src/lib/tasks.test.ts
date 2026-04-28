import { describe, expect, test } from 'bun:test';
import {
  needsActionCount,
  pendingActionableApprovals,
  taskAttentionCounts,
  taskNeedsCriticalAttention,
  taskNeedsDashboardAttention,
  taskNeedsDecisionAttention,
  taskRuntimeMs,
  taskInputText,
  taskIsActive,
  taskIsTerminal,
  taskNeedsAttention,
  taskNeedsQueueAction,
  taskStartedAt,
  taskStateDescription,
  taskStateTransitions,
  taskSummaryTitle
} from './tasks';
import type { HomelabdApproval, HomelabdTask } from './types';

const task = (id: string, status: string): Pick<HomelabdTask, 'id' | 'status'> => ({ id, status });
const timedTask = (fields: Partial<HomelabdTask>): HomelabdTask => ({
  id: 'task_timed',
  title: 'timed',
  goal: 'timed',
  status: 'running',
  assigned_to: 'CoderAgent',
  priority: 5,
  created_at: '2026-04-25T00:00:00Z',
  updated_at: '2026-04-25T00:00:00Z',
  ...fields
});

const approval = (
  id: string,
  taskID: string | undefined,
  status = 'pending'
): HomelabdApproval => ({
  id,
  task_id: taskID,
  tool: 'git.merge_approved',
  reason: 'merge reviewed task branch into repo root',
  status,
  created_at: '2026-04-25T00:00:00Z',
  updated_at: '2026-04-25T00:00:00Z'
});

describe('task queue attention logic', () => {
  test('classifies task statuses by operator action needed', () => {
    expect(taskNeedsAttention(task('blocked', 'blocked'))).toBe(true);
    expect(taskNeedsCriticalAttention(task('blocked', 'blocked'))).toBe(true);
    expect(taskNeedsAttention(task('conflict', 'conflict_resolution'))).toBe(true);
    expect(taskNeedsAttention(task('review', 'ready_for_review'))).toBe(true);
    expect(taskNeedsDecisionAttention(task('review', 'ready_for_review'))).toBe(true);
    expect(taskNeedsAttention(task('running', 'running'))).toBe(false);
    expect(taskIsActive(task('queued', 'queued'))).toBe(true);
    expect(taskIsTerminal(task('done', 'done'))).toBe(true);
  });

  test('does not count stale pending approvals for terminal tasks', () => {
    const tasks = [task('task_done', 'done')];
    const approvals = [
      approval('approval_old_1', 'task_done'),
      approval('approval_old_2', 'task_done')
    ];

    expect(pendingActionableApprovals(approvals, tasks)).toEqual([]);
    expect(needsActionCount(tasks, approvals)).toBe(0);
  });

  test('counts pending approvals for active or unknown task targets', () => {
    const tasks = [task('task_running', 'running')];
    const approvals = [
      approval('approval_active', 'task_running'),
      approval('approval_external', undefined),
      approval('approval_granted', 'task_running', 'granted')
    ];

    expect(pendingActionableApprovals(approvals, tasks).map((item) => item.id)).toEqual([
      'approval_active',
      'approval_external'
    ]);
    expect(needsActionCount(tasks, approvals)).toBe(2);
    expect(taskNeedsQueueAction(tasks[0], approvals)).toBe(true);
  });

  test('adds attention tasks to actionable approvals', () => {
    const tasks = [task('task_failed', 'failed'), task('task_running', 'running')];
    const approvals = [approval('approval_active', 'task_running')];

    expect(needsActionCount(tasks, approvals)).toBe(2);
  });

  test('splits dashboard attention into red and amber navbar counts', () => {
    const tasks = [
      task('task_failed', 'failed'),
      task('task_review', 'ready_for_review'),
      task('task_running', 'running'),
      task('task_done', 'done'),
      {
        ...task('task_waiting_on_graph', 'blocked'),
        parent_id: 'task_root',
        graph_phase: 'implement',
        blocked_by: ['task_design']
      }
    ];
    const approvals = [
      approval('approval_running', 'task_running'),
      approval('approval_done', 'task_done'),
      approval('approval_external', undefined)
    ];

    expect(taskNeedsDashboardAttention(tasks[4], approvals)).toBe(false);
    expect(taskAttentionCounts(tasks, approvals)).toEqual({ red: 1, amber: 3, total: 4 });
  });

  test('calculates runtime from task lifecycle timestamps', () => {
    const completed = timedTask({
      status: 'done',
      started_at: '2026-04-25T00:01:00Z',
      stopped_at: '2026-04-25T00:06:30Z',
      updated_at: '2026-04-25T00:07:00Z'
    });
    const running = timedTask({
      status: 'running',
      started_at: '2026-04-25T00:01:00Z'
    });
    const legacy = timedTask({
      status: 'blocked',
      updated_at: '2026-04-25T00:04:00Z'
    });

    expect(taskStartedAt(completed)).toBe('2026-04-25T00:01:00Z');
    expect(taskRuntimeMs(completed)).toBe(330000);
    expect(taskRuntimeMs(running, new Date('2026-04-25T00:03:00Z'))).toBe(120000);
    expect(taskRuntimeMs(legacy)).toBe(240000);
  });

  test('uses stored task titles for task pane summaries', () => {
    const detailed = timedTask({
      id: 'task_long',
      title: 'Expose full input below activity',
      goal:
        'Work this task to completion if possible. Inspect the task workspace before editing. Make a minimal patch that satisfies the task goal.\nTask goal: expose the full input below activity.'
    });

    expect(taskSummaryTitle(detailed)).toBe('Expose full input below activity');
    expect(taskInputText(detailed)).toContain('Task goal: expose the full input below activity.');
  });

  test('summarizes full task input when no title exists', () => {
    const detailed = timedTask({
      id: 'task_long',
      title: '',
      goal:
        'Work this task to completion if possible. Inspect the task workspace before editing. Make a minimal patch that satisfies the task goal.\nTask goal: expose the full input below activity.'
    });

    expect(taskSummaryTitle(detailed)).toBe(
      'Work this task to completion if possible. Inspect the task workspace before editing.'
    );
  });

  test('falls back to title and id for task display text', () => {
    const withoutGoal = timedTask({ id: 'task_title_only', title: 'title only', goal: '' });
    const withoutText = timedTask({ id: 'task_empty', title: '', goal: '' });

    expect(taskSummaryTitle(withoutGoal)).toBe('title only');
    expect(taskInputText(withoutText)).toBe('task_empty');
  });

  test('explains workflow states and valid next transitions', () => {
    expect(taskStateDescription('ready_for_review')).toContain('review gate');
    expect(taskStateTransitions('ready_for_review')).toContain('conflict resolution');
    expect(taskStateDescription('conflict_resolution')).toContain('conflicts');
    expect(taskStateTransitions('conflict_resolution')).toContain('ready for review');
    expect(taskStateTransitions('awaiting_approval')).toContain('conflict resolution');
    expect(taskStateDescription('running')).toContain('worker owns');
    expect(taskStateTransitions('blocked')).toBe('blocked → running, cancelled, or deleted');
  });
});
