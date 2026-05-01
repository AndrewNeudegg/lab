import type { HomelabdApproval, HomelabdTask } from './types';

const attentionStatuses = new Set([
  'blocked',
  'conflict_resolution',
  'failed',
  'ready_for_review',
  'awaiting_approval',
  'awaiting_restart',
  'awaiting_verification',
  'no_change_required'
]);

const activeStatuses = new Set(['queued', 'running']);
const terminalStatuses = new Set(['done', 'cancelled']);
const criticalAttentionStatuses = new Set(['blocked', 'conflict_resolution', 'failed']);
const decisionAttentionStatuses = new Set([
  'ready_for_review',
  'awaiting_approval',
  'awaiting_restart',
  'awaiting_verification',
  'no_change_required'
]);

export interface TaskAttentionCounts {
  red: number;
  amber: number;
  total: number;
}

type TaskAttentionInput = Pick<HomelabdTask, 'id' | 'status'> &
  Partial<Pick<HomelabdTask, 'parent_id' | 'graph_phase' | 'blocked_by'>>;

export const taskNeedsAttention = (task: Pick<HomelabdTask, 'status'>) =>
  attentionStatuses.has(task.status);

export const taskNeedsCriticalAttention = (task: Pick<HomelabdTask, 'status'>) =>
  criticalAttentionStatuses.has(task.status);

export const taskNeedsDecisionAttention = (task: Pick<HomelabdTask, 'status'>) =>
  decisionAttentionStatuses.has(task.status);

export const taskIsActive = (task: Pick<HomelabdTask, 'status'>) => activeStatuses.has(task.status);

export const taskIsTerminal = (task: Pick<HomelabdTask, 'status'>) =>
  terminalStatuses.has(task.status);

export const taskStateDescription = (status = '') => {
  switch (status) {
    case 'queued':
      return 'Waiting in the execution queue.';
    case 'running':
      return 'A worker is active.';
    case 'ready_for_review':
      return 'Worker finished. Review is queued by the merge queue.';
    case 'blocked':
      return 'Review or execution stopped. Retryable failures are requeued automatically with bounded attempts.';
    case 'conflict_resolution':
      return 'Task branch conflicts with current main. The task supervisor will queue automatic recovery.';
    case 'awaiting_approval':
      return 'Review gate passed. Merge approval is pending.';
    case 'awaiting_restart':
      return 'Merge landed. Required restarts and health checks are in progress.';
    case 'awaiting_verification':
      return 'Merge landed. Verify the running app before accepting.';
    case 'no_change_required':
      return 'Worker found no patch is required. Accept to close without a merge, or reopen with corrected instructions.';
    case 'done':
      return 'Accepted by the human.';
    case 'failed':
      return 'Runtime failure. Rerun or delegate with failure context.';
    case 'cancelled':
      return 'Intentionally stopped.';
    default:
      return 'Unknown workflow state.';
  }
};

export const taskStatusLabel = (status = '') => {
  switch (status) {
    case 'ready_for_review':
      return 'queued for review';
    case 'no_change_required':
      return 'no change required';
    default:
      return status.replaceAll('_', ' ');
  }
};

export const taskStateTransitions = (status = '') => {
  switch (status) {
    case 'queued':
      return 'queued → running';
    case 'running':
      return 'running → ready for review or blocked';
    case 'ready_for_review':
      return 'ready for review → awaiting approval, conflict resolution, no change required, or blocked';
    case 'blocked':
      return 'blocked → running through automatic recovery, cancelled, or deleted';
    case 'conflict_resolution':
      return 'conflict resolution → running through automatic recovery, ready for review, cancelled, or deleted';
    case 'awaiting_approval':
      return 'awaiting approval → awaiting restart, awaiting verification, conflict resolution, blocked, or running';
    case 'awaiting_restart':
      return 'awaiting restart → awaiting verification, queued, or deleted';
    case 'awaiting_verification':
      return 'awaiting verification → done or queued';
    case 'no_change_required':
      return 'no change required → done or queued';
    case 'done':
    case 'cancelled':
      return 'terminal';
    case 'failed':
      return 'failed → running, cancelled, or deleted';
    default:
      return 'unknown';
  }
};

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

const structuredTaskTitleKeys = ['summary', 'title', 'message'] as const;

const looseStructuredTaskTitle = (value: string) => {
  const match = value.match(/["'](?:summary|title|message)["']\s*:\s*["']([^"'\r\n}]*)/i);
  return match?.[1]?.trim() || '';
};

const parsedStructuredTaskTitleValue = (value: unknown, depth = 0): string => {
  if (depth > 4 || value === undefined || value === null) {
    return '';
  }
  if (typeof value === 'string') {
    const trimmed = value.trim();
    if (!trimmed) {
      return '';
    }
    try {
      return parsedStructuredTaskTitleValue(JSON.parse(trimmed), depth + 1);
    } catch {
      return '';
    }
  }
  if (typeof value !== 'object' || Array.isArray(value)) {
    return '';
  }
  const record = value as Record<string, unknown>;
  for (const key of structuredTaskTitleKeys) {
    const field = record[key];
    if (typeof field === 'string') {
      return parsedStructuredTaskTitleValue(field, depth + 1) || field.trim();
    }
    const parsed = parsedStructuredTaskTitleValue(field, depth + 1);
    if (parsed) {
      return parsed;
    }
  }
  return '';
};

const parsedStructuredTaskTitle = (value: string) => {
  try {
    return parsedStructuredTaskTitleValue(JSON.parse(value));
  } catch {
    return '';
  }
};

const cleanTaskDisplayText = (value = '') => {
  let next = value.trim().replace(/\s+/g, ' ');
  for (let depth = 0; depth < 4 && next; depth += 1) {
    const recovered = parsedStructuredTaskTitle(next) || looseStructuredTaskTitle(next);
    if (!recovered || recovered === next) {
      break;
    }
    next = recovered.trim().replace(/\s+/g, ' ');
  }
  next = next.trim().replace(/\s+/g, ' ').replace(/^[`"']+|[`"']+$/g, '');
  for (;;) {
    const lower = next.toLowerCase();
    if (lower.startsWith('summary:')) {
      next = next.slice('summary:'.length).trim();
      continue;
    }
    if (lower.startsWith('title:')) {
      next = next.slice('title:'.length).trim();
      continue;
    }
    return next;
  }
};

const normalizedTaskText = (value = '') => cleanTaskDisplayText(value);

export const taskInputText = (task: Pick<HomelabdTask, 'id' | 'title' | 'goal'>) =>
  task.goal?.trim() || task.title?.trim() || task.id;

export const taskSummaryTitle = (
  task: Pick<HomelabdTask, 'id' | 'title' | 'goal'>,
  maxLength = 96
) => {
  const source = normalizedTaskText(task.title) || normalizedTaskText(task.goal) || task.id;
  if (source.length <= maxLength) {
    return source;
  }

  const sentenceEnd = source.search(/[.!?](?:\s|$)/);
  if (sentenceEnd >= 24 && sentenceEnd + 1 <= maxLength) {
    const firstSentence = source.slice(0, sentenceEnd + 1);
    const secondSentenceEnd = source.slice(sentenceEnd + 1).search(/[.!?](?:\s|$)/);
    const combinedSentenceEnd =
      secondSentenceEnd >= 0 ? sentenceEnd + 1 + secondSentenceEnd + 1 : -1;
    if (
      firstSentence.toLowerCase() === 'work this task to completion if possible.' &&
      combinedSentenceEnd > sentenceEnd &&
      combinedSentenceEnd <= maxLength
    ) {
      return source.slice(0, combinedSentenceEnd);
    }
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

const taskIsGraphParent = (task: TaskAttentionInput) => task.graph_phase === 'root' && !task.parent_id;

const taskIsBlockedByGraphDependency = (task: TaskAttentionInput) =>
  task.status === 'blocked' &&
  Boolean(task.parent_id) &&
  Boolean(task.graph_phase) &&
  Boolean(task.blocked_by?.length);

export const taskNeedsDashboardAttention = (
  task: TaskAttentionInput,
  approvals: HomelabdApproval[]
) => {
  if (taskHasActionableApproval(task, approvals)) {
    return true;
  }
  if (taskIsGraphParent(task) || taskIsBlockedByGraphDependency(task)) {
    return false;
  }
  return taskNeedsQueueAction(task, approvals);
};

export const taskAttentionCounts = (
  tasks: TaskAttentionInput[],
  approvals: HomelabdApproval[]
): TaskAttentionCounts => {
  const actionableTaskIDs = new Set<string>();
  const counts = tasks.reduce(
    (next, task) => {
      if (!taskNeedsDashboardAttention(task, approvals)) {
        return next;
      }
      actionableTaskIDs.add(task.id);
      if (taskNeedsCriticalAttention(task)) {
        next.red += 1;
      } else {
        next.amber += 1;
      }
      return next;
    },
    { red: 0, amber: 0, total: 0 }
  );

  counts.amber += pendingActionableApprovals(approvals, tasks).filter(
    (approval) => !approval.task_id || !actionableTaskIDs.has(approval.task_id)
  ).length;
  counts.total = counts.red + counts.amber;
  return counts;
};

export const needsActionCount = (
  tasks: Pick<HomelabdTask, 'id' | 'status'>[],
  approvals: HomelabdApproval[]
) =>
  tasks.filter((task) => taskNeedsQueueAction(task, approvals)).length +
  pendingActionableApprovals(approvals, tasks).filter((approval) => !approval.task_id).length;
