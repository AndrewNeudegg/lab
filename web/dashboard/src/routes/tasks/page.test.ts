import { describe, expect, test } from 'bun:test';
import { readFileSync } from 'node:fs';

const pageSource = readFileSync(new URL('./+page.svelte', import.meta.url), 'utf8');

describe('tasks page selection rendering', () => {
  test('renders selected task from reactive cached state instead of repeated list scans', () => {
    expect(pageSource).toContain('let currentTask: HomelabdTask | undefined');
    expect(pageSource).toContain('$: currentTask =');
    expect(pageSource).toContain('class:selected={currentTask?.id === task.id}');
    expect(pageSource).not.toContain('const selectedTask = () =>');
    expect(pageSource).not.toContain('selectedTask()');
  });

  test('renders task list and activity from cached arrays', () => {
    expect(pageSource).toContain('let visibleTaskItems: HomelabdTask[] = []');
    expect(pageSource).toContain('$: visibleTaskItems =');
    expect(pageSource).toContain('{#each visibleTaskItems as task}');
    expect(pageSource).toContain('let currentTaskEvents: HomelabdEvent[] = []');
    expect(pageSource).toContain('{#each currentTaskEvents as event}');
    expect(pageSource).not.toContain('visibleTasks()');
    expect(pageSource).not.toContain('taskEvents(');
  });
});
