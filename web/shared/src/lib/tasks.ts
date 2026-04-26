import type { HomelabdApproval, HomelabdTask } from './types';

const attentionStatuses = new Set([
  'blocked',
  'failed',
  'ready_for_review',
  'awaiting_approval',
  'awaiting_verification'
]);

const activeStatuses = new Set(['queued', 'running']);
const terminalStatuses = new Set(['done', 'cancelled']);

export const taskNeedsAttention = (task: Pick<HomelabdTask, 'status'>) =>
  attentionStatuses.has(task.status);

export const taskIsActive = (task: Pick<HomelabdTask, 'status'>) => activeStatuses.has(task.status);

export const taskIsTerminal = (task: Pick<HomelabdTask, 'status'>) =>
  terminalStatuses.has(task.status);

const parseTime = (value?: string) => {
  if (!value) {
    return undefined;
  }
  const time = Date.parse(value);
  return Number.isNaN(time) ? undefined : time;
};

export const taskStartedAt = (
  task: Pick<HomelabdTask, 'created_at' | 'started_at'>
) => task.started_at || task.created_at;

export const taskRuntimeMs = (
  task: Pick<HomelabdTask, 'created_at' | 'updated_at' | 'started_at' | 'stopped_at' | 'status'>,
  now = new Date()
) => {
  const started = parseTime(taskStartedAt(task));
  if (started === undefined) {
    return undefined;
  }
  const fallbackEnd = taskIsActive(task) ? now.getTime() : parseTime(task.updated_at);
  const ended = parseTime(task.stopped_at) ?? fallbackEnd;
  if (ended === undefined || ended < started) {
    return undefined;
  }
  return ended - started;
};

const normalizedTaskText = (value = '') => value.trim().replace(/\s+/g, ' ');

export const taskInputText = (task: Pick<HomelabdTask, 'id' | 'title' | 'goal'>) =>
  task.goal?.trim() || task.title?.trim() || task.id;

export const taskSummaryTitle = (
  task: Pick<HomelabdTask, 'id' | 'title' | 'goal'>,
  maxLength = 96
) => {
  const source = normalizedTaskText(task.goal) || normalizedTaskText(task.title) || task.id;
  if (source.length <= maxLength) {
    return source;
  }

  const sentenceEnd = source.search(/[.!?](?:\s|$)/);
  if (sentenceEnd >= 24 && sentenceEnd + 1 <= maxLength) {
    return source.slice(0, sentenceEnd + 1);
  }

  const clipped = source.slice(0, maxLength).trimEnd();
  const wordBoundary = clipped.lastIndexOf(' ');
  if (wordBoundary >= Math.floor(maxLength * 0.6)) {
    return `${clipped.slice(0, wordBoundary)}...`;
  }
  return `${clipped}...`;
};

export const pendingActionableApprovals = (
  approvals: HomelabdApproval[],
  tasks: Pick<HomelabdTask, 'id' | 'status'>[]
) => {
  const tasksByID = new Map(tasks.map((task) => [task.id, task]));
  return approvals.filter((approval) => {
    if (approval.status !== 'pending') {
      return false;
    }
    if (!approval.task_id) {
      return true;
    }
    const task = tasksByID.get(approval.task_id);
    return !task || !taskIsTerminal(task);
  });
};

export const taskHasActionableApproval = (
  task: Pick<HomelabdTask, 'id' | 'status'>,
  approvals: HomelabdApproval[]
) =>
  !taskIsTerminal(task) &&
  approvals.some((approval) => approval.status === 'pending' && approval.task_id === task.id);

export const taskNeedsQueueAction = (
  task: Pick<HomelabdTask, 'id' | 'status'>,
  approvals: HomelabdApproval[]
) => taskNeedsAttention(task) || taskHasActionableApproval(task, approvals);

export const needsActionCount = (
  tasks: Pick<HomelabdTask, 'id' | 'status'>[],
  approvals: HomelabdApproval[]
) =>
  tasks.filter((task) => taskNeedsQueueAction(task, approvals)).length +
  pendingActionableApprovals(approvals, tasks).filter((approval) => !approval.task_id).length;
