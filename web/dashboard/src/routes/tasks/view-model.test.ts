import { describe, expect, test } from 'bun:test';
import type { HomelabdApproval, HomelabdEvent, HomelabdTask } from '@homelab/shared';
import {
  buildWorkerTraceRuns,
  createTaskQueueView,
  selectTaskForQueue,
  type TaskFilter,
  type TaskQueueFilter
} from './view-model';

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

const remoteTask = (
  id: string,
  status: string,
  updatedMinute: string,
  agentID: string
): HomelabdTask => ({
  ...task(id, status, updatedMinute),
  assigned_to: `remote:${agentID}`,
  target: {
    mode: 'remote',
    agent_id: agentID,
    machine: `${agentID}.local`,
    workdir_id: 'repo',
    workdir: `/srv/${agentID}/repo`,
    backend: 'codex'
  }
});

const graphTask = (
  id: string,
  status: string,
  updatedMinute: string,
  phase: string,
  parentID = '',
  blockedBy: string[] = []
): HomelabdTask => ({
  ...task(id, status, updatedMinute),
  graph_phase: phase,
  parent_id: parentID || undefined,
  blocked_by: blockedBy.length ? blockedBy : undefined,
  depends_on: blockedBy.length ? blockedBy : undefined
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

const delegateEvent = (
  id: string,
  minute: string,
  type: string,
  payload: Record<string, unknown>
): HomelabdEvent => ({
  id,
  task_id: 'task_review',
  actor: 'codex',
  type,
  time: `2026-04-26T00:${minute}:00Z`,
  payload
});

const view = (
  taskFilter: TaskFilter,
  selectedTaskId: string,
  taskSearch = '',
  approvals: HomelabdApproval[] = [],
  queueFilter: TaskQueueFilter = 'all'
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
    queueFilter,
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
      queueFilter: 'all',
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

  test('separates local and remote agent queues', () => {
    const tasks = [
      task('task_local', 'running', '03'),
      remoteTask('task_desk', 'queued', '04', 'desk'),
      remoteTask('task_nuc', 'ready_for_review', '05', 'nuc')
    ];

    const local = createTaskQueueView({
      tasks,
      approvals: [],
      events: [],
      taskFilter: 'all',
      queueFilter: 'local',
      taskSearch: '',
      selectedTaskId: ''
    });
    expect(local.visibleTaskItems.map((item) => item.id)).toEqual(['task_local']);

    const desk = createTaskQueueView({
      tasks,
      approvals: [],
      events: [],
      taskFilter: 'all',
      queueFilter: 'agent:desk',
      taskSearch: '',
      selectedTaskId: ''
    });
    expect(desk.visibleTaskItems.map((item) => item.id)).toEqual(['task_desk']);
  });

  test('search matches remote execution context', () => {
    const result = createTaskQueueView({
      tasks: [
        task('task_local', 'running', '03'),
        remoteTask('task_desk', 'queued', '04', 'desk')
      ],
      approvals: [],
      events: [],
      taskFilter: 'all',
      queueFilter: 'all',
      taskSearch: '/srv/desk/repo',
      selectedTaskId: ''
    });

    expect(result.visibleTaskItems.map((item) => item.id)).toEqual(['task_desk']);
    expect(result.currentTask?.target?.agent_id).toBe('desk');
  });

  test('leaves waiting task graph phases out of the needs-action queue', () => {
    const root = graphTask('task_root', 'blocked', '06', 'root');
    const inspect = graphTask('task_inspect', 'queued', '05', 'inspect', root.id);
    const design = graphTask('task_design', 'blocked', '04', 'design', root.id, [inspect.id]);
    const blockedFailure = graphTask('task_failed', 'blocked', '03', 'implement', root.id);

    const attention = createTaskQueueView({
      tasks: [root, inspect, design, blockedFailure],
      approvals: [],
      events: [],
      taskFilter: 'attention',
      queueFilter: 'all',
      taskSearch: '',
      selectedTaskId: ''
    });

    expect(attention.attentionTaskItems.map((item) => item.id)).toEqual(['task_failed']);
    expect(attention.visibleTaskItems.map((item) => item.id)).toEqual(['task_failed']);

    const all = createTaskQueueView({
      tasks: [root, inspect, design, blockedFailure],
      approvals: [],
      events: [],
      taskFilter: 'all',
      queueFilter: 'all',
      taskSearch: '',
      selectedTaskId: ''
    });

    expect(all.visibleTaskItems.map((item) => item.id)).toEqual([
      'task_root',
      'task_inspect',
      'task_design',
      'task_failed'
    ]);
  });
});

describe('task queue selection helper', () => {
  test('does not require a network refresh to choose the visible task for a new filter', () => {
    const tasks = [task('task_running', 'running', '03'), task('task_review', 'ready_for_review', '02')];

    expect(selectTaskForQueue(tasks, [], 'active', 'all', '', 'task_review')).toBe('task_running');
    expect(selectTaskForQueue(tasks, [], 'attention', 'all', '', 'task_running')).toBe('task_review');
  });
});

describe('worker trace runs', () => {
  test('keeps active output-only runs running until terminal event arrives', () => {
    const runs = buildWorkerTraceRuns([
      delegateEvent('out1', '02', 'agent.delegate.output', {
        id: 'delegate_active',
        backend: 'codex',
        text: 'working'
      })
    ]);

    expect(runs).toHaveLength(1);
    expect(runs[0].id).toBe('delegate_active');
    expect(runs[0].status).toBe('running');
    expect(runs[0].active).toBe(true);
    expect(runs[0].output).toBe('working');
  });

  test('marks failed runs terminal and preserves error text', () => {
    const runs = buildWorkerTraceRuns([
      delegateEvent('out1', '02', 'agent.delegate.output', {
        id: 'delegate_failed',
        backend: 'codex',
        text: 'before failure\n'
      }),
      delegateEvent('fail1', '03', 'agent.delegate.failed', {
        id: 'delegate_failed',
        backend: 'codex',
        error: 'signal: killed'
      })
    ]);

    expect(runs).toHaveLength(1);
    expect(runs[0].status).toBe('failed');
    expect(runs[0].active).toBe(false);
    expect(runs[0].error).toBe('signal: killed');
    expect(runs[0].output).toBe('before failure\n');
  });

  test('groups delegate output events by run id and merges artifact status', () => {
    const runs = buildWorkerTraceRuns(
      [
        delegateEvent('out1', '02', 'agent.delegate.output', {
          id: 'delegate_one',
          backend: 'codex',
          text: 'hello '
        }),
        delegateEvent('out2', '03', 'agent.delegate.output', {
          id: 'delegate_one',
          backend: 'codex',
          text: 'world'
        })
      ],
      [
        {
          id: 'delegate_one',
          kind: 'external_agent',
          task_id: 'task_review',
          backend: 'codex',
          workspace: '/tmp/work',
          status: 'completed',
          output: 'artifact output',
          time: '2026-04-26T00:04:00Z'
        }
      ]
    );

    expect(runs).toHaveLength(1);
    expect(runs[0].id).toBe('delegate_one');
    expect(runs[0].status).toBe('completed');
    expect(runs[0].active).toBe(false);
    expect(runs[0].output).toBe('hello world');
    expect(runs[0].events.map((item) => item.id)).toEqual(['out1', 'out2']);
  });
});
