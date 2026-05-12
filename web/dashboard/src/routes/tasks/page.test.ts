import { describe, expect, test } from 'bun:test';
import { readFileSync } from 'node:fs';

const pageSource = readFileSync(new URL('./+page.svelte', import.meta.url), 'utf8');

describe('tasks page composition', () => {
  test('renders selected task state from cached view-model arrays', () => {
    expect(pageSource).toContain('let taskQueueView: TaskQueueView = createTaskQueueView');
    expect(pageSource).toContain('$: currentTaskSummary = taskQueueView.currentTask');
    expect(pageSource).toContain('taskDetails[currentTaskSummary.id] || currentTaskSummary');
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

  test('keeps plain tasks routes as an overview history entry', () => {
    expect(pageSource).toContain('const applyTaskOverviewSelection = () =>');
    expect(pageSource).toContain('const navigateToTaskOverview = (replaceState = true) =>');
    expect(pageSource).toContain("void goto('/tasks', { keepFocus: true, noScroll: true, replaceState })");
    expect(pageSource).toContain('if (!taskId) {');
    expect(pageSource).toContain('applyTaskOverviewSelection();');
    expect(pageSource).toContain('on:click={() => navigateToTaskOverview()}');
  });

  test('uses direct task and approval endpoints instead of a chat command composer', () => {
    expect(pageSource).toContain('client.runTask(taskId)');
    expect(pageSource).toContain('client.reviewTask(taskId)');
    expect(pageSource).toContain('client.moveTaskInMergeQueue');
    expect(pageSource).toContain('client.acceptTask(taskId)');
    expect(pageSource).toContain('client.reopenTask(taskId');
    expect(pageSource).toContain('client.cancelTask(taskId)');
    expect(pageSource).toContain('client.retryTask(taskId');
    expect(pageSource).toContain('client.deleteTask(taskId)');
    expect(pageSource).toContain('client.approveApproval(approval.id)');
    expect(pageSource).toContain('client.denyApproval(approval.id)');
    expect(pageSource).toContain('approvalNoticeTitle(operation, response.reply ||');
    expect(pageSource).not.toContain('client.sendMessage');
    expect(pageSource).not.toContain('sendCommand');
    expect(pageSource).not.toContain('command-panel');
    expect(pageSource).not.toContain('Task command');
    expect(pageSource).not.toContain('Pending approvals');
    expect(pageSource).not.toContain('approval-list');
  });

  test('renders a compact operator-adjustable merge queue', () => {
    expect(pageSource).toContain('let mergeQueueItems: HomelabdTask[] = []');
    expect(pageSource).toContain('merge_queue_position');
    expect(pageSource).toContain('aria-label="Merge queue"');
    expect(pageSource).toContain('aria-label="Auto merge reviewed queue-head tasks"');
    expect(pageSource).toContain('let autoMergeVersion = 0');
    expect(pageSource).toContain('settingsVersion === autoMergeVersion');
    expect(pageSource).toContain('autoMergeVersion += 1');
    expect(pageSource).toContain('client.updateSettings({ auto_merge_enabled: next })');
    expect(pageSource).toContain('Move ${taskSummaryTitle(item, 40)} up in merge queue');
    expect(pageSource).toContain('Move ${taskSummaryTitle(item, 40)} down in merge queue');
    expect(pageSource).toContain('mergeQueueMoveKey');
    expect(pageSource).toContain('max-height: min(13.5rem, 32vh);');
  });

  test('keeps automatic sync status responsive while selected details refresh separately', () => {
    expect(pageSource).toContain("let taskFilter: TaskFilter = 'attention'");
    expect(pageSource).toContain('let refreshStateSequence = 0');
    expect(pageSource).toContain('let syncFailureCount = 0');
    expect(pageSource).toContain('let syncIssue =');
    expect(pageSource).toContain('taskSyncIndicatorState');
    expect(pageSource).toContain('data-sync-status={syncIndicator.tone}');
    expect(pageSource).toContain('class:pulse={refreshing}');
    expect(pageSource).toContain('function withRefreshTimeout');
    expect(pageSource).toContain('withRefreshTimeout as withTimeout');
	    expect(pageSource).toContain("const taskRequest = withRefreshTimeout('Tasks', client.listTasks())");
	    expect(pageSource).toContain("const workspaceRequest = withRefreshTimeout('Workspaces', client.listWorkspaces())");
    expect(pageSource).toContain("collectionFromResponse<HomelabdTask>('Tasks', 'tasks'");
	    expect(pageSource).toContain('selected?.summary_only || taskDetails[taskId]?.summary_only');
	    expect(pageSource).toContain("const result = await withRefreshTimeout('Task details', client.getTask(taskId))");
	    expect(pageSource).toContain("collectionFromResponse<HomelabdRemoteWorkspace>");
	    expect(pageSource).toContain('let taskLoadError =');
    expect(pageSource).toContain('syncFailureCount += 1');
    expect(pageSource).toContain('syncFailureCount = 0');
    expect(pageSource).toContain('void applySecondaryRefresh');
    expect(pageSource).toContain('void refreshSelectedTaskDetails(syncSelection.selectedTaskId');
    expect(pageSource).toContain('if (sequence === refreshStateSequence)');
    expect(pageSource).toContain('lastRefresh = syncTimeLabel();');
	    expect(pageSource).not.toContain('on:click={() => void refreshState()}');
	  });

	  test('creates tasks through explicit auto, local, and remote project targets', () => {
	    expect(pageSource).toContain("type TaskTargetMode = 'auto' | 'local' | 'remote'");
	    expect(pageSource).toContain("let taskTargetMode: TaskTargetMode = 'auto'");
	    expect(pageSource).toContain('client.listWorkspaces()');
	    expect(pageSource).toContain('<option value="auto">Auto route</option>');
	    expect(pageSource).toContain('<option value="remote">Remote project</option>');
	    expect(pageSource).toContain('<option value="local">Local homelabd</option>');
	    expect(pageSource).toContain("mode: 'remote'");
	    expect(pageSource).toContain('project_id: selectedWorkspace.project_id');
	    expect(pageSource).toContain("mode: 'local'");
	    expect(pageSource).toContain("mode: 'auto'");
	  });

  test('preserves the operator-selected queue filter during background refresh', () => {
    const refreshSource = pageSource.slice(
      pageSource.indexOf('const refreshState = () =>'),
      pageSource.indexOf('onMount(() =>')
    );

    expect(refreshSource).toContain('resolveTaskSyncSelection');
    expect(refreshSource).not.toContain("taskFilter = 'all'");
    expect(refreshSource).not.toContain("queueFilter = 'all'");
    expect(refreshSource).not.toContain("taskSearch = ''");
  });

  test('guards route re-application while task navigation is pending', () => {
    const navigateSource = pageSource.slice(
      pageSource.indexOf('const navigateToTask ='),
      pageSource.indexOf('const applyRouteTaskSelection =')
    );

    expect(pageSource).toContain("let pendingRouteTaskId = ''");
    expect(navigateSource).toContain('pendingRouteTaskId = taskId');
    expect(navigateSource).not.toContain('lastAppliedRouteTaskId = taskId;');
    expect(pageSource).toContain('pendingRouteTaskId === taskId');
    expect(pageSource).toContain('routeTaskId !== pendingRouteTaskId');
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
    expect(pageSource).toContain('taskOperatorGuidance');
    expect(pageSource).toContain('aria-label="Workflow state"');
    expect(pageSource).toContain('aria-label="Automatic recovery"');
    expect(pageSource).toContain('currentTask.auto_recovery_attempts');
    expect(pageSource).toContain('aria-label="Worker runs"');
    expect(pageSource).toContain('aria-label="Task plan"');
    expect(pageSource).toContain('aria-label="Original task input"');
    expect(pageSource).toContain('<dl class="record-summary"');
    expect(pageSource).toContain('border-width: 0 2px 2px 0;');
  });

  test('renders Goal blocker traces from task detail without hiding the task lifecycle state', () => {
    expect(pageSource).toContain('currentGoalBlockerTrace');
    expect(pageSource).toContain('currentGoalBlockerFlow');
    expect(pageSource).toContain('performGoalBlockerAction');
    expect(pageSource).toContain('performGoalBlockerDecisionChoice');
    expect(pageSource).toContain('selectGoalBlockerDecisionChoice');
    expect(pageSource).toContain('goal_blocker_trace');
    expect(pageSource).toContain('aria-label="Goal blocker trace"');
    expect(pageSource).toContain('aria-label="Required blocker decision"');
    expect(pageSource).toContain('aria-label="Goal blocker decision choices"');
    expect(pageSource).toContain('aria-label="Goal blocker answer"');
    expect(pageSource).toContain('This task is blocking Goal Autopilot');
    expect(pageSource).toContain('Open blocking task');
    expect(pageSource).toContain('goalBlockerDecisionChoiceId');
    expect(pageSource).toContain('Instruction for the next run');
    expect(pageSource).toContain('Check Goal now');
    expect(pageSource).toContain(
      'Review is queued automatically. You can wait for the merge queue or run Review now from manual controls.'
    );
  });

  test('renders highlighted task diff controls from the dedicated diff endpoint', () => {
    expect(pageSource).toContain('client.getTaskDiff');
    expect(pageSource).toContain('aria-label="Task diff"');
    expect(pageSource).toContain('Changes vs main');
    expect(pageSource).toContain("type DiffMode = 'split' | 'unified'");
    expect(pageSource).toContain('parseUnifiedDiff');
    expect(pageSource).toContain('buildSplitRows');
    expect(pageSource).toContain('inlineChangeSegments');
    expect(pageSource).toContain('diffSourceLabel');
    expect(pageSource).toContain('currentTaskDiff.warning');
    expect(pageSource).toContain('aria-label="Changed files"');
    expect(pageSource).toContain('aria-label="Split diff"');
    expect(pageSource).toContain('aria-label="Unified diff"');
    expect(pageSource).toContain('selectedDiffFilePath');
    expect(pageSource).toContain('overflow-wrap: anywhere;');
    expect(pageSource).toContain('white-space: pre-wrap;');
    expect(pageSource).toContain('min-width: 64rem;');
  });
});
