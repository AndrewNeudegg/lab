import { taskHasActionableApproval, taskIsActive, taskIsTerminal } from '@homelab/shared';
import type { HomelabdApproval, HomelabdTask } from '@homelab/shared';

export type TaskOperation =
  | 'run'
  | 'review'
  | 'accept'
  | 'restart'
  | 'reopen'
  | 'cancel'
  | 'retry'
  | 'delete';

export type ApprovalOperation = 'approve' | 'deny';

export type PrimaryTaskAction =
  | {
      type: 'task';
      operation: TaskOperation;
      label: string;
      detail: string;
      tone: 'primary' | 'warning' | 'danger' | 'neutral';
    }
  | {
      type: 'approval';
      operation: ApprovalOperation;
      approval: HomelabdApproval;
      label: string;
      detail: string;
      tone: 'primary' | 'warning' | 'danger' | 'neutral';
    }
  | {
      type: 'none';
      label: string;
      detail: string;
      tone: 'primary' | 'warning' | 'danger' | 'neutral';
    };

export const approvalNoticeTitle = (operation: ApprovalOperation, reply = '') => {
  if (operation === 'deny') {
    return 'Approval denied';
  }
  const lower = reply.toLowerCase();
  if (
    lower.includes('already') ||
    lower.includes('failed') ||
    lower.includes('stale') ||
    lower.includes('recovery') ||
    lower.includes('requeued')
  ) {
    return 'Approval handled';
  }
  return 'Approval granted';
};

export const pendingApprovalForTask = (
  task: Pick<HomelabdTask, 'id' | 'status'> | undefined,
  approvals: HomelabdApproval[]
) =>
  task
    ? approvals.find((approval) => approval.status === 'pending' && approval.task_id === task.id)
    : undefined;

export const primaryTaskAction = (
  task: HomelabdTask | undefined,
  approvals: HomelabdApproval[]
): PrimaryTaskAction => {
  if (!task) {
    return {
      type: 'none',
      label: 'Select a task',
      detail: 'Choose a queue item to see direct actions.',
      tone: 'neutral'
    };
  }

  const approval = pendingApprovalForTask(task, approvals);
  if (approval) {
    return {
      type: 'approval',
      operation: 'approve',
      approval,
      label: 'Approve merge',
      detail: 'Runs the pending approved tool request for this task.',
      tone: 'primary'
    };
  }

  switch (task.status) {
    case 'queued':
      return {
        type: 'task',
        operation: 'run',
        label: 'Start now',
        detail: 'Starts the local worker for this queued task.',
        tone: 'primary'
      };
    case 'running':
      return {
        type: 'task',
        operation: 'cancel',
        label: 'Stop worker',
        detail: 'Cancels the active run and keeps the workspace for inspection.',
        tone: 'danger'
      };
    case 'ready_for_review':
      return {
        type: 'task',
        operation: 'review',
        label: 'Run review gate',
        detail: 'Runs checks and pre-merge validation without sending a chat prompt.',
        tone: 'primary'
      };
    case 'awaiting_approval':
      return {
        type: 'none',
        label: 'Waiting for approval',
        detail: 'No pending approval is attached to this task yet. Sync to refresh approvals.',
        tone: 'warning'
      };
    case 'awaiting_restart':
      if (task.restart_status === 'failed') {
        return {
          type: 'task',
          operation: 'restart',
          label: 'Retry restart',
          detail: task.restart_last_error || 'Post-merge restart failed. Retry the enforced restart gate.',
          tone: 'warning'
        };
      }
      return {
        type: 'none',
        label: 'Restarting services',
        detail: task.restart_current
          ? `Restarting ${task.restart_current}; verification is locked until health checks pass.`
          : 'Post-merge restarts are queued before verification.',
        tone: 'warning'
      };
    case 'awaiting_verification':
      return {
        type: 'task',
        operation: 'accept',
        label: 'Accept result',
        detail: 'Marks the merged or remote result as verified and done.',
        tone: 'primary'
      };
    case 'blocked':
    case 'failed':
    case 'conflict_resolution':
      return {
        type: 'task',
        operation: 'retry',
        label: task.status === 'conflict_resolution' ? 'Retry now' : 'Retry with worker',
        detail:
          task.status === 'conflict_resolution'
            ? 'Automatic conflict recovery is handled by the task supervisor; this starts an immediate retry.'
            : 'Starts a direct retry from the current workspace state.',
        tone: 'warning'
      };
    case 'done':
    case 'cancelled':
      return {
        type: 'task',
        operation: 'reopen',
        label: 'Reopen',
        detail: 'Returns this terminal task to the queue with a reason.',
        tone: 'neutral'
      };
    default:
      return {
        type: 'task',
        operation: 'run',
        label: 'Continue',
        detail: 'Runs the next direct task action for this state.',
        tone: 'primary'
      };
  }
};

export const secondaryTaskOperations = (
  task: HomelabdTask | undefined,
  approvals: HomelabdApproval[]
): TaskOperation[] => {
  if (!task) {
    return [];
  }

  const primary = primaryTaskAction(task, approvals);
  const operations = new Set<TaskOperation>();

  if (task.status === 'ready_for_review') {
    operations.add('retry');
  }
  if (task.status === 'awaiting_verification') {
    operations.add('reopen');
  }
  if (task.status === 'awaiting_restart') {
    operations.add('restart');
    operations.add('reopen');
  }
  if (task.status === 'awaiting_approval') {
    operations.add('review');
  }
  if (!taskIsActive(task) && !taskIsTerminal(task)) {
    operations.add('retry');
  }
  if (taskIsTerminal(task)) {
    operations.add('reopen');
  }
  if (!taskIsActive(task)) {
    operations.add('delete');
  }
  if (taskIsActive(task)) {
    operations.add('cancel');
  }

  if (primary.type === 'task') {
    operations.delete(primary.operation);
  }
  return [...operations];
};

export const taskOperationLabel = (operation: TaskOperation) => {
  switch (operation) {
    case 'run':
      return 'Start';
    case 'review':
      return 'Review';
    case 'accept':
      return 'Accept';
    case 'restart':
      return 'Restart';
    case 'reopen':
      return 'Reopen';
    case 'cancel':
      return 'Stop';
    case 'retry':
      return 'Retry';
    case 'delete':
      return 'Delete';
  }
};

export const taskOperationNeedsReason = (operation: TaskOperation) =>
  operation === 'reopen' || operation === 'retry' || operation === 'delete';

export const taskHasDirectDecision = (task: HomelabdTask, approvals: HomelabdApproval[]) =>
  taskHasActionableApproval(task, approvals) || primaryTaskAction(task, approvals).type === 'task';
