import { describe, expect, test } from 'bun:test';
import {
  collectionFromResponse,
  taskListEmptyMessage,
  withRefreshTimeout,
  type RefreshTimers
} from './sync-model';

const createManualTimers = () => {
  let timeoutHandler: (() => void) | undefined;
  const cleared: ReturnType<typeof setTimeout>[] = [];
  const timers: RefreshTimers = {
    setTimeout(handler, _timeoutMs) {
      timeoutHandler = handler;
      return 1 as ReturnType<typeof setTimeout>;
    },
    clearTimeout(timer) {
      cleared.push(timer);
    }
  };

  return {
    timers,
    cleared,
    fireTimeout() {
      timeoutHandler?.();
    }
  };
};

describe('task sync model', () => {
  test('normalises wrapped and raw collection responses', () => {
    expect(
      collectionFromResponse<{ id: string }>('Tasks', 'tasks', { tasks: [{ id: 'task_a' }] })
    ).toEqual([{ id: 'task_a' }]);
    expect(collectionFromResponse<{ id: string }>('Tasks', 'tasks', [{ id: 'task_b' }])).toEqual(
      [{ id: 'task_b' }]
    );
  });

  test('rejects malformed collection responses with a specific message', () => {
    expect(() => collectionFromResponse('Tasks', 'tasks', { items: [] })).toThrow(
      'Tasks response did not contain a tasks array.'
    );
  });

  test('distinguishes task sync failures from genuinely empty filtered queues', () => {
    expect(
      taskListEmptyMessage({
        apiBase: '/api',
        refreshing: false,
        taskLoadError: 'Tasks timed out after 7s.',
        totalTasks: 0
      })
    ).toBe('Task sync failed before any tasks loaded from /api/tasks.');
    expect(
      taskListEmptyMessage({
        apiBase: '/api',
        refreshing: false,
        taskLoadError: '',
        totalTasks: 2
      })
    ).toBe('No tasks match the current filters.');
  });

  test('wraps refresh operations without replacing their success or failure', async () => {
    const successTimers = createManualTimers();
    await expect(
      withRefreshTimeout('Tasks', Promise.resolve({ tasks: [{ id: 'task_a' }] }), 7000, successTimers.timers)
    ).resolves.toEqual({ tasks: [{ id: 'task_a' }] });
    expect(successTimers.cleared).toEqual([1]);

    const failureTimers = createManualTimers();
    await expect(
      withRefreshTimeout(
        'Tasks',
        Promise.reject(new ReferenceError('request is not defined')),
        7000,
        failureTimers.timers
      )
    ).rejects.toThrow('request is not defined');
    expect(failureTimers.cleared).toEqual([1]);
  });

  test('reports refresh timeouts with the resource label', async () => {
    const manualTimers = createManualTimers();
    const operation = new Promise(() => {});
    const wrapped = withRefreshTimeout('Tasks', operation, 7000, manualTimers.timers);

    manualTimers.fireTimeout();

    await expect(wrapped).rejects.toThrow('Tasks timed out after 7s.');
  });
});
