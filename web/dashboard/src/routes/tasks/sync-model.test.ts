import { describe, expect, test } from 'bun:test';
import { collectionFromResponse, taskListEmptyMessage } from './sync-model';

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
});
