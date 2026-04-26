import { describe, expect, test } from 'bun:test';
import { readFileSync } from 'node:fs';

const pageSource = readFileSync(new URL('./+page.svelte', import.meta.url), 'utf8');

describe('tasks page selection rendering', () => {
  test('renders selected task from reactive cached state instead of repeated list scans', () => {
    expect(pageSource).toContain('let taskQueueView: TaskQueueView = createTaskQueueView');
    expect(pageSource).toContain('$: currentTask = taskQueueView.currentTask');
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

  test('changes queue filters through explicit selection normalization', () => {
    expect(pageSource).toContain('const setTaskFilter = (filter: TaskFilter) =>');
    expect(pageSource).toContain('selectedTaskId = selectTaskForQueue');
    expect(pageSource).toContain('on:click={() => setTaskFilter(filter.id as TaskFilter)}');
    expect(pageSource).not.toContain('on:click={() => (taskFilter = filter.id as TaskFilter)}');
  });

  test('does not collapse the task queue after desktop task selection', () => {
    expect(pageSource).toContain("window.matchMedia('(max-width: 760px)').matches");
    expect(pageSource).not.toContain('selectedTaskId = id;\\n    taskQueueOpen = false;');
  });

  test('keeps the command panel operable when collapsed', () => {
    expect(pageSource).toContain('let commandPanelOpen = true');
    expect(pageSource).toContain('const setCommandPanelOpen = (open: boolean) =>');
    expect(pageSource).toContain('const toggleCommandPanel = () =>');
    expect(pageSource).toContain('on:click={toggleCommandPanel}');
    expect(pageSource).toContain('.command-panel.collapsed');
    expect(pageSource).toContain('grid-template-rows: auto;');
  });
});
