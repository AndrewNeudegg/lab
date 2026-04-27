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
    expect(pageSource).toContain('const setQueueFilter = (filter: TaskQueueFilter) =>');
    expect(pageSource).toContain('const nextTaskId = selectTaskForQueue');
    expect(pageSource).toContain('selectedTaskId = nextTaskId');
    expect(pageSource).toContain('resolveTaskSyncSelection');
    expect(pageSource).toContain('selectedTaskId = syncSelection.selectedTaskId');
    expect(pageSource).toContain('on:click={() => setTaskFilter(filter.id as TaskFilter)}');
    expect(pageSource).toContain('on:click={() => setQueueFilter(option.id)}');
    expect(pageSource).not.toContain('on:click={() => (taskFilter = filter.id as TaskFilter)}');
  });

  test('does not write selected task state from the derived queue view', () => {
    expect(pageSource).not.toContain('taskQueueView.selectedTaskId !== selectedTaskId');
    expect(pageSource).not.toContain('selectedTaskId = taskQueueView.selectedTaskId');
  });

  test('keeps manual sync responsive while refreshing selected task details', () => {
    expect(pageSource).toContain("let taskFilter: TaskFilter = 'all'");
    expect(pageSource).toContain('let refreshStateSequence = 0');
    expect(pageSource).toContain('function withRefreshTimeout');
    expect(pageSource).toContain('withRefreshTimeout as withTimeout');
    expect(pageSource).not.toContain('const withRefreshTimeout = async <T,>');
    expect(pageSource).toContain("const taskRequest = withRefreshTimeout('Tasks', client.listTasks())");
    expect(pageSource).toContain("collectionFromResponse<HomelabdTask>('Tasks', 'tasks'");
    expect(pageSource).toContain('let taskLoadError =');
    expect(pageSource).toContain('void applySecondaryRefresh');
    expect(pageSource).toContain('void refreshSelectedTaskDetails(syncSelection.selectedTaskId');
    expect(pageSource).toContain('if (sequence === refreshStateSequence)');
    expect(pageSource).toContain('lastRefresh = syncTimeLabel();');
  });

  test('shows task sync failures in the queue pane instead of only the command panel', () => {
    expect(pageSource).toContain('aria-label="Task sync status"');
    expect(pageSource).toContain('Task sync failed');
    expect(pageSource).toContain('{emptyTaskListMessage}');
    expect(pageSource).toContain('taskListEmptyMessage');
  });

  test('does not hide the queue when a task is selected', () => {
    expect(pageSource).toContain('const selectTask = (id: string) =>');
    expect(pageSource).toContain('selectedTaskId = id;');
    expect(pageSource).not.toContain('taskQueueOpen = false;');
    expect(pageSource).not.toContain("window.matchMedia('(max-width: 760px)').matches");
  });

  test('keeps the command panel operable when collapsed', () => {
    expect(pageSource).toContain('let commandPanelOpen = true');
    expect(pageSource).toContain('const setCommandPanelOpen = (open: boolean) =>');
    expect(pageSource).toContain('const toggleCommandPanel = () =>');
    expect(pageSource).toContain('on:click={toggleCommandPanel}');
    expect(pageSource).toContain('.command-panel.collapsed');
    expect(pageSource).toContain('grid-template-rows: auto;');
  });

  test('renders explicit workflow state machine guidance', () => {
    expect(pageSource).toContain('taskStateDescription');
    expect(pageSource).toContain('taskStateTransitions');
    expect(pageSource).toContain('aria-label="Workflow state"');
    expect(pageSource).toContain('Expected transition:');
    expect(pageSource).toContain('It will not restart a worker automatically.');
    expect(pageSource).toContain('A worker currently owns this task.');
  });

  test('renders the reviewed task plan above the original input', () => {
    const planIndex = pageSource.indexOf('aria-label="Task plan"');
    const inputIndex = pageSource.indexOf('aria-label="Original task input"');

    expect(planIndex).toBeGreaterThan(0);
    expect(inputIndex).toBeGreaterThan(planIndex);
    expect(pageSource).toContain('Reviewed plan');
    expect(pageSource).toContain('{#each currentTask.plan.steps as step}');
  });

  test('requires explicit remote execution context confirmation', () => {
    expect(pageSource).toContain('aria-label="Execution queues"');
    expect(pageSource).toContain('Run exactly on');
    expect(pageSource).toContain('bind:checked={contextAcknowledged}');
    expect(pageSource).toContain('Boolean(selectedAgent && (!selectedWorkdir || !contextAcknowledged))');
    expect(pageSource).toContain('aria-label="Execution context"');
    expect(pageSource).toContain('Remote execution context');
  });

  test('renders worker trace runs with direct stop and retry controls', () => {
    expect(pageSource).toContain('buildWorkerTraceRuns');
    expect(pageSource).toContain('let currentTaskRuns: WorkerTraceRun[] = []');
    expect(pageSource).toContain('aria-label="Worker runs"');
    expect(pageSource).toContain("performTaskAction('cancel')");
    expect(pageSource).toContain("performTaskAction('retry')");
    expect(pageSource).toContain('client.listTaskRuns');
    expect(pageSource).toContain('runOutput(run)');
    expect(pageSource).toContain('run.artifact?.path');
  });

  test('renders highlighted task diff controls from the dedicated diff endpoint', () => {
    expect(pageSource).toContain('client.getTaskDiff');
    expect(pageSource).toContain('aria-label="Task diff"');
    expect(pageSource).toContain('Changes vs main');
    expect(pageSource).toContain("type DiffMode = 'split' | 'unified'");
    expect(pageSource).toContain('parseUnifiedDiff');
    expect(pageSource).toContain('buildSplitRows');
    expect(pageSource).toContain('inlineChangeSegments');
    expect(pageSource).toContain('aria-label="Changed files"');
    expect(pageSource).toContain('aria-label="Split diff"');
    expect(pageSource).toContain('aria-label="Unified diff"');
    expect(pageSource).toContain('selectedDiffFilePath');
    expect(pageSource).toContain('disabled={diffLoadingTaskId === currentTask?.id || isRemoteTask(currentTask)}');
    expect(pageSource).toContain('!isRemoteTask(currentTask) && void refreshTaskDiff(currentTask.id)');
  });

  test('keeps diff file labels legible and theme-aware', () => {
    expect(pageSource).toContain('--diff-text-strong: var(--text-strong, #111827);');
    expect(pageSource).toContain(":global(html[data-theme='dark'] .diff-review)");
    expect(pageSource).toContain('grid-template-rows: auto auto;');
    expect(pageSource).toContain('min-height: 3.15rem;');
    expect(pageSource).toContain('line-height: 1.25;');
  });

  test('wraps long diff lines inside the diff pane', () => {
    expect(pageSource).toContain('.split-diff,');
    expect(pageSource).toContain('width: 100%;');
    expect(pageSource).toContain('min-width: 0;');
    expect(pageSource).toContain('overflow-wrap: anywhere;');
    expect(pageSource).toContain('white-space: pre-wrap;');
    expect(pageSource).toContain('white-space: nowrap;');
  });

  test('caps the mobile queue above the selected task record', () => {
    expect(pageSource).toContain('height: auto;');
    expect(pageSource).toContain('max-height: min(54dvh, 28rem);');
    expect(pageSource).toContain('grid-template-columns: minmax(0, 1fr);');
    expect(pageSource).toContain('.task-pane.collapsed');
    expect(pageSource).toContain('position: sticky;');
  });
});
