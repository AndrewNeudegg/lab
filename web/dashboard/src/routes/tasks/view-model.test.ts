import { describe, expect, test } from 'bun:test';
import type { HomelabdApproval, HomelabdEvent, HomelabdTask } from '@homelab/shared';
import { createTaskQueueView, selectTaskForQueue, type TaskFilter } from './view-model';

const task = (id: string, status: string, updatedMinute: string): HomelabdTask => ({
  id,
  title: `${id} title`,
  goal: `${id} goal`,
  status,
  assigned_to: 'CoderAgent',
  priority: 5,
  created_at: `2026-04-26T00:${updatedMinute}:00Z`,
  updated_at: `2026-04-26T00:${updatedMinute}:00Z`
});

const plannedTask = (
  id: string,
  status: string,
  updatedMinute: string,
  planSummary: string
): HomelabdTask => ({
  ...task(id, status, updatedMinute),
  plan: {
    status: 'reviewed',
    summary: planSummary,
    steps: [{ title: 'Inspect scope', detail: 'Read relevant files before editing.' }],
    created_at: `2026-04-26T00:${updatedMinute}:00Z`,
    reviewed_at: `2026-04-26T00:${updatedMinute}:00Z`
  }
});

const approval = (id: string, taskID?: string): HomelabdApproval => ({
  id,
  task_id: taskID,
  tool: 'git.merge_approved',
  reason: 'merge reviewed task branch into repo root',
  status: 'pending',
  created_at: '2026-04-26T00:00:00Z',
  updated_at: '2026-04-26T00:00:00Z'
});

const event = (id: string, taskID: string, minute: string): HomelabdEvent => ({
  id,
  task_id: taskID,
  actor: 'CoderAgent',
  type: 'task.updated',
  time: `2026-04-26T00:${minute}:00Z`
});

const view = (
  taskFilter: TaskFilter,
  selectedTaskId: string,
  taskSearch = '',
  approvals: HomelabdApproval[] = []
) => {
  const tasks = [
    task('task_running', 'running', '03'),
    task('task_review', 'ready_for_review', '02'),
    task('task_done', 'done', '01')
  ];
  return createTaskQueueView({
    tasks,
    approvals,
    events: [
      event('event_old', 'task_review', '04'),
      event('event_other', 'task_running', '05'),
      event('event_new', 'task_review', '06')
    ],
    taskFilter,
    taskSearch,
    selectedTaskId
  });
};

describe('task queue view model', () => {
  test('selects the first visible needs-action task when the current task is outside the filter', () => {
    const result = view('attention', 'task_running');

    expect(result.visibleTaskItems.map((item) => item.id)).toEqual(['task_review']);
    expect(result.selectedTaskId).toBe('task_review');
    expect(result.currentTask?.id).toBe('task_review');
  });

  test('selects the first running task when switching to the running queue', () => {
    const result = view('active', 'task_review');

    expect(result.visibleTaskItems.map((item) => item.id)).toEqual(['task_running']);
    expect(result.selectedTaskId).toBe('task_running');
    expect(result.currentTask?.status).toBe('running');
  });

  test('keeps a clicked task selected in the all queue without waiting for another sync', () => {
    const result = view('all', 'task_done');

    expect(result.visibleTaskItems.map((item) => item.id)).toEqual([
      'task_running',
      'task_review',
      'task_done'
    ]);
    expect(result.selectedTaskId).toBe('task_done');
    expect(result.currentTask?.id).toBe('task_done');
  });

  test('search narrows queue selection and clears it when no task is visible', () => {
    expect(view('all', 'task_running', 'review').selectedTaskId).toBe('task_review');
    expect(view('all', 'task_running', 'does-not-exist').currentTask).toBeUndefined();
  });

  test('search matches reviewed plan summaries', () => {
    const result = createTaskQueueView({
      tasks: [
        task('task_running', 'running', '03'),
        plannedTask('task_planned', 'queued', '04', 'Plan to update the terminal transcript')
      ],
      approvals: [],
      events: [],
      taskFilter: 'all',
      taskSearch: 'terminal transcript',
      selectedTaskId: ''
    });

    expect(result.visibleTaskItems.map((item) => item.id)).toEqual(['task_planned']);
    expect(result.currentTask?.id).toBe('task_planned');
  });

  test('includes pending approvals in needs-action queue only while they are actionable', () => {
    const result = view('attention', 'task_running', '', [approval('approval_running', 'task_running')]);

    expect(result.pendingApprovalItems.map((item) => item.id)).toEqual(['approval_running']);
    expect(result.visibleTaskItems.map((item) => item.id)).toEqual([
      'task_running',
      'task_review'
    ]);
  });

  test('returns current task activity only, newest first', () => {
    const result = view('attention', 'task_review');

    expect(result.currentTaskEvents.map((item) => item.id)).toEqual(['event_new', 'event_old']);
  });
});

describe('task queue selection helper', () => {
  test('does not require a network refresh to choose the visible task for a new filter', () => {
    const tasks = [task('task_running', 'running', '03'), task('task_review', 'ready_for_review', '02')];

    expect(selectTaskForQueue(tasks, [], 'active', '', 'task_review')).toBe('task_running');
    expect(selectTaskForQueue(tasks, [], 'attention', '', 'task_running')).toBe('task_review');
  });
});
