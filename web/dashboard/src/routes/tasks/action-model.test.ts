import { describe, expect, test } from 'bun:test';
import type { HomelabdApproval, HomelabdTask } from '@homelab/shared';
import {
  approvalNoticeTitle,
  pendingApprovalForTask,
  primaryTaskAction,
  secondaryTaskOperations,
  taskHasDirectDecision
} from './action-model';

const task = (status: string): HomelabdTask => ({
  id: `task_${status}`,
  title: `${status} task`,
  goal: `${status} task`,
  status,
  assigned_to: 'codex',
  priority: 5,
  created_at: '2026-04-27T08:00:00Z',
  updated_at: '2026-04-27T08:00:00Z'
});

const approval = (taskID: string): HomelabdApproval => ({
  id: 'approval_1',
  task_id: taskID,
  tool: 'git.merge_approved',
  reason: 'merge reviewed task branch into repo root',
  status: 'pending',
  created_at: '2026-04-27T08:00:00Z',
  updated_at: '2026-04-27T08:00:00Z'
});

describe('task action model', () => {
  test('uses a pending approval as the primary decision', () => {
    const selected = task('awaiting_approval');
    const approvals = [approval(selected.id)];

    const action = primaryTaskAction(selected, approvals);

    expect(action.type).toBe('approval');
    expect(action.label).toBe('Approve merge');
    expect(pendingApprovalForTask(selected, approvals)?.id).toBe('approval_1');
    expect(taskHasDirectDecision(selected, approvals)).toBe(true);
  });

  test('maps task states to direct non-chat operations', () => {
    expect(primaryTaskAction(task('queued'), []).type === 'task' && primaryTaskAction(task('queued'), []).operation).toBe(
      'run'
    );
    expect(primaryTaskAction(task('running'), []).type).toBe('none');
    expect(primaryTaskAction(task('ready_for_review'), []).type).toBe('none');
    expect(
      primaryTaskAction(task('awaiting_verification'), []).type === 'task' &&
        primaryTaskAction(task('awaiting_verification'), []).operation
    ).toBe('accept');
    expect(primaryTaskAction(task('blocked'), []).type === 'task' && primaryTaskAction(task('blocked'), []).operation).toBe(
      'retry'
    );
    const conflictAction = primaryTaskAction(task('conflict_resolution'), []);
    expect(conflictAction.type === 'task' && conflictAction.detail).toContain('Automatic conflict recovery');
  });

  test('does not label failed approval recovery as a granted approval', () => {
    expect(approvalNoticeTitle('approve', 'Approved and executed approval_1')).toBe('Approval granted');
    expect(approvalNoticeTitle('approve', 'approval is already failed')).toBe('Approval handled');
    expect(approvalNoticeTitle('approve', 'I queued automatic conflict recovery')).toBe('Approval handled');
    expect(approvalNoticeTitle('deny', 'Approval denied')).toBe('Approval denied');
  });

  test('locks acceptance while post-merge restart is running', () => {
    const restarting = {
      ...task('awaiting_restart'),
      restart_status: 'running',
      restart_current: 'dashboard'
    };
    const failed = {
      ...task('awaiting_restart'),
      restart_status: 'failed',
      restart_last_error: 'dashboard health check failed'
    };

    expect(primaryTaskAction(restarting, []).type).toBe('none');
    expect(primaryTaskAction(restarting, []).label).toBe('Restart in progress');
    const action = primaryTaskAction(failed, []);
    expect(action.type === 'task' && action.operation).toBe('restart');
    expect(action.detail).toContain('dashboard health check failed');
    expect(secondaryTaskOperations(restarting, [])).toContain('restart');
  });

  test('keeps destructive actions secondary and out of active worker states', () => {
    expect(secondaryTaskOperations(task('running'), [])).toEqual(['cancel']);
    expect(secondaryTaskOperations(task('ready_for_review'), [])).toContain('review');
    expect(secondaryTaskOperations(task('ready_for_review'), [])).toContain('delete');
    expect(secondaryTaskOperations(task('done'), [])).toContain('delete');
  });
});
