import { describe, expect, test } from 'bun:test';
import { readFileSync } from 'node:fs';

const pageSource = readFileSync(new URL('./+page.svelte', import.meta.url), 'utf8');

describe('tasks page composition', () => {
  test('renders selected task state from cached view-model arrays', () => {
    expect(pageSource).toContain('let taskQueueView: TaskQueueView = createTaskQueueView');
    expect(pageSource).toContain('$: currentTask = taskQueueView.currentTask');
    expect(pageSource).toContain('let visibleTaskItems: HomelabdTask[] = []');
    expect(pageSource).toContain('{#each visibleTaskItems as task}');
    expect(pageSource).toContain('let currentTaskEvents: HomelabdEvent[] = []');
    expect(pageSource).toContain('{#each currentTaskEvents as event}');
    expect(pageSource).not.toContain('selectedTask()');
    expect(pageSource).not.toContain('visibleTasks()');
  });

  test('normalises selection through explicit queue and filter handlers', () => {
    expect(pageSource).toContain('const setTaskFilter = (filter: TaskFilter) =>');
    expect(pageSource).toContain('const setQueueFilter = (filter: TaskQueueFilter) =>');
    expect(pageSource).toContain('const nextTaskId = selectTaskForQueue');
    expect(pageSource).toContain('selectedTaskId = nextTaskId');
    expect(pageSource).toContain('resolveTaskSyncSelection');
    expect(pageSource).toContain('selectedTaskId = syncSelection.selectedTaskId');
    expect(pageSource).toContain('on:click={() => setTaskFilter(filter.id as TaskFilter)}');
    expect(pageSource).toContain('on:click={() => setQueueFilter(option.id)}');
    expect(pageSource).not.toContain('selectedTaskId = taskQueueView.selectedTaskId');
  });

  test('uses direct task and approval endpoints instead of a chat command composer', () => {
    expect(pageSource).toContain('client.runTask(taskId)');
    expect(pageSource).toContain('client.reviewTask(taskId)');
    expect(pageSource).toContain('client.acceptTask(taskId)');
    expect(pageSource).toContain('client.reopenTask(taskId');
    expect(pageSource).toContain('client.cancelTask(taskId)');
    expect(pageSource).toContain('client.retryTask(taskId');
    expect(pageSource).toContain('client.deleteTask(taskId)');
    expect(pageSource).toContain('client.approveApproval(approval.id)');
    expect(pageSource).toContain('client.denyApproval(approval.id)');
    expect(pageSource).not.toContain('client.sendMessage');
    expect(pageSource).not.toContain('sendCommand');
    expect(pageSource).not.toContain('command-panel');
    expect(pageSource).not.toContain('Task command');
    expect(pageSource).not.toContain('Pending approvals');
    expect(pageSource).not.toContain('approval-list');
  });

  test('keeps manual sync responsive while selected details refresh separately', () => {
    expect(pageSource).toContain("let taskFilter: TaskFilter = 'attention'");
    expect(pageSource).toContain('let refreshStateSequence = 0');
    expect(pageSource).toContain('function withRefreshTimeout');
    expect(pageSource).toContain('withRefreshTimeout as withTimeout');
    expect(pageSource).toContain("const taskRequest = withRefreshTimeout('Tasks', client.listTasks())");
    expect(pageSource).toContain("collectionFromResponse<HomelabdTask>('Tasks', 'tasks'");
    expect(pageSource).toContain('let taskLoadError =');
    expect(pageSource).toContain('void applySecondaryRefresh');
    expect(pageSource).toContain('void refreshSelectedTaskDetails(syncSelection.selectedTaskId');
    expect(pageSource).toContain('if (sequence === refreshStateSequence)');
    expect(pageSource).toContain('lastRefresh = syncTimeLabel();');
  });

  test('renders mobile queue/detail switching without the old collapsing sidebar model', () => {
    expect(pageSource).toContain("type MobilePanel = 'queue' | 'detail'");
    expect(pageSource).toContain('const showMobilePanel = (panel: MobilePanel)');
    expect(pageSource).toContain("showMobilePanel('detail')");
    expect(pageSource).toContain("showMobilePanel('queue')");
    expect(pageSource).toContain('Back to queue');
    expect(pageSource).toContain("data-mobile-hidden={mobilePanel !== 'queue'}");
    expect(pageSource).toContain("data-mobile-hidden={mobilePanel !== 'detail'}");
    expect(pageSource).not.toContain('aria-label="Task panels"');
    expect(pageSource).not.toContain('mobile-tabs');
    expect(pageSource).not.toContain('taskQueueOpen');
    expect(pageSource).not.toContain('.task-pane.collapsed');
  });

  test('renders workflow, plan, worker trace, and direct action regions', () => {
    expect(pageSource).toContain('primaryTaskAction');
    expect(pageSource).toContain('secondaryTaskOperations');
    expect(pageSource).toContain('aria-label="Task actions"');
    expect(pageSource).toContain('class={`decision-panel ${currentPrimaryAction.tone}`}');
    expect(pageSource).toContain('aria-label="Retry settings"');
    expect(pageSource).toContain('aria-label="Reopen reason"');
    expect(pageSource).toContain('class="detail-section state-context"');
    expect(pageSource).toContain('taskStateDescription');
    expect(pageSource).toContain('taskStateTransitions');
    expect(pageSource).toContain('aria-label="Workflow state"');
    expect(pageSource).toContain('aria-label="Worker runs"');
    expect(pageSource).toContain('aria-label="Task plan"');
    expect(pageSource).toContain('aria-label="Original task input"');
    expect(pageSource).toContain('<dl class="record-summary"');
    expect(pageSource).toContain('border-width: 0 2px 2px 0;');
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
    expect(pageSource).toContain('overflow-wrap: anywhere;');
    expect(pageSource).toContain('white-space: pre-wrap;');
    expect(pageSource).toContain('min-width: 64rem;');
  });
});
