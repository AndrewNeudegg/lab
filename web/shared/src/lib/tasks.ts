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
