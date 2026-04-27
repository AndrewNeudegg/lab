<script lang="ts">
  import { onMount } from 'svelte';
  import {
    createHomelabdClient,
    Navbar,
    taskInputText,
    taskIsActive,
    taskRuntimeMs,
    taskStartedAt,
    taskStateDescription,
    taskStateTransitions,
    taskSummaryTitle,
    type HomelabdApproval,
    type HomelabdEvent,
    type HomelabdRemoteAgent,
    type HomelabdRemoteAgentWorkdir,
    type HomelabdRunArtifact,
    type HomelabdTask,
    type HomelabdTaskDiffResponse
  } from '@homelab/shared';
  import {
    buildSplitRows,
    inlineChangeSegments,
    parseUnifiedDiff,
    type DiffSplitRow,
    type InlineSegment,
    type ParsedDiffFile,
    type ParsedDiffLine
  } from './diff-view';
  import {
    buildWorkerTraceRuns,
    createTaskQueueView,
    resolveTaskSyncSelection,
    selectTaskForQueue,
    type TaskFilter,
    type TaskQueueFilter,
    type TaskQueueView,
    type WorkerTraceRun
  } from './view-model';
  import {
    pendingApprovalForTask,
    primaryTaskAction,
    secondaryTaskOperations,
    taskOperationLabel,
    type PrimaryTaskAction,
    type TaskOperation
  } from './action-model';
  import {
    collectionFromResponse,
    errorMessage,
    taskListEmptyMessage,
    withRefreshTimeout as withTimeout
  } from './sync-model';

  type DiffMode = 'split' | 'unified';
  type MobilePanel = 'queue' | 'detail';
  type Notice = {
    id: number;
    tone: 'success' | 'error' | 'info';
    title: string;
    detail: string;
  };
  type RefreshSelectedTaskDetailsOptions = {
    force?: boolean;
    resetDiffSelection?: boolean;
    task?: HomelabdTask;
  };

  const apiBase = import.meta.env.VITE_HOMELABD_API_BASE || '/api';
  const client = createHomelabdClient({ baseUrl: apiBase });
  const refreshTimeoutMs = 7000;

  let newTaskDraft = '';
  let creatingTask = false;
  let contextAcknowledged = false;
  let refreshing = false;
  let taskLoadError = '';
  let diffError = '';
  let actionLoading = '';
  let approvalLoading = '';
  let diffLoadingTaskId = '';
  let taskFilter: TaskFilter = 'attention';
  let queueFilter: TaskQueueFilter = 'all';
  let taskSearch = '';
  let selectedTaskId = '';
  let loadedRunsTaskId = '';
  let selectedDiffFilePath = '';
  let loadedDiffTaskId = '';
  let diffMode: DiffMode = 'split';
  let mobilePanel: MobilePanel = 'queue';
  let lastRefresh = '';
  let selectedAgentId = '';
  let selectedWorkdirId = '';
  let retryBackend = 'codex';
  let retryInstruction = '';
  let reopenReason = '';
  let deleteConfirmTaskId = '';
  let notice: Notice | undefined;
  let noticeId = 0;
  let refreshStateSequence = 0;

  let tasks: HomelabdTask[] = [];
  let agents: HomelabdRemoteAgent[] = [];
  let approvals: HomelabdApproval[] = [];
  let events: HomelabdEvent[] = [];
  let taskRuns: Record<string, HomelabdRunArtifact[]> = {};
  let taskDiffs: Record<string, HomelabdTaskDiffResponse> = {};

  let taskQueueView: TaskQueueView = createTaskQueueView({
    tasks,
    approvals,
    events,
    taskFilter,
    queueFilter,
    taskSearch,
    selectedTaskId
  });
  let pendingApprovalItems: HomelabdApproval[] = [];
  let attentionTaskItems: HomelabdTask[] = [];
  let activeTaskItems: HomelabdTask[] = [];
  let visibleTaskItems: HomelabdTask[] = [];
  let currentTask: HomelabdTask | undefined;
  let currentTaskEvents: HomelabdEvent[] = [];
  let currentTaskRuns: WorkerTraceRun[] = [];
  let currentTaskDiff: HomelabdTaskDiffResponse | undefined;
  let currentDiffFiles: ParsedDiffFile[] = [];
  let currentDiffFile: ParsedDiffFile | undefined;
  let currentSplitRows: DiffSplitRow[] = [];
  let needsActionTotal = 0;
  let onlineAgentItems: HomelabdRemoteAgent[] = [];
  let selectedAgent: HomelabdRemoteAgent | undefined;
  let selectedWorkdirs: HomelabdRemoteAgentWorkdir[] = [];
  let selectedWorkdir: HomelabdRemoteAgentWorkdir | undefined;
  let queueOptions: { id: TaskQueueFilter; label: string; count: number; detail: string }[] = [];
  let selectedContextLabel = 'Local homelabd workspace';
  let emptyTaskListMessage = '';
  let currentPrimaryAction: PrimaryTaskAction = primaryTaskAction(undefined, []);
  let currentSecondaryOperations: TaskOperation[] = [];
  let currentPendingApproval: HomelabdApproval | undefined;

  const syncTimeLabel = () =>
    new Date().toLocaleTimeString([], {
      hour: '2-digit',
      minute: '2-digit',
      second: '2-digit'
    });

  const shortID = (id = '') => {
    const parts = id.split('_');
    const tail = parts[parts.length - 1] || id;
    return tail.length > 8 ? tail.slice(0, 8) : tail;
  };

  const statusLabel = (status = '') => status.replaceAll('_', ' ');

  const workdirLabel = (workdir?: HomelabdRemoteAgentWorkdir) => {
    if (!workdir) {
      return 'No directory';
    }
    return workdir.label || workdir.id || workdir.path;
  };

  const targetLabel = (task: HomelabdTask) => {
    if (!task.target || task.target.mode !== 'remote') {
      return task.workspace ? 'Local workspace' : 'Local';
    }
    const machine = task.target.machine || task.target.agent_id || 'remote';
    const dir = task.target.workdir || task.target.workdir_id || 'directory';
    return `${machine} / ${dir}`;
  };

  const isRemoteTask = (task?: HomelabdTask) => task?.target?.mode === 'remote';

  const compactTime = (value?: string) => {
    if (!value) {
      return 'unknown';
    }
    const date = new Date(value);
    if (Number.isNaN(date.getTime())) {
      return value;
    }
    return date.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
  };

  const compactDuration = (milliseconds?: number) => {
    if (milliseconds === undefined) {
      return 'unknown';
    }
    const totalSeconds = Math.max(0, Math.floor(milliseconds / 1000));
    const days = Math.floor(totalSeconds / 86400);
    const hours = Math.floor((totalSeconds % 86400) / 3600);
    const minutes = Math.floor((totalSeconds % 3600) / 60);
    const seconds = totalSeconds % 60;
    if (days > 0) {
      return `${days}d ${hours}h`;
    }
    if (hours > 0) {
      return `${hours}h ${minutes}m`;
    }
    if (minutes > 0) {
      return `${minutes}m ${seconds}s`;
    }
    return `${seconds}s`;
  };

  const truncate = (value = '', max = 120) => {
    const normalized = value.trim().replace(/\s+/g, ' ');
    return normalized.length > max ? `${normalized.slice(0, max)}...` : normalized;
  };

  const setNotice = (tone: Notice['tone'], title: string, detail: string) => {
    noticeId += 1;
    notice = { id: noticeId, tone, title, detail };
  };

  const clearNotice = () => {
    notice = undefined;
  };

  function withRefreshTimeout<T>(label: string, operation: Promise<T>): Promise<T> {
    return withTimeout(label, operation, refreshTimeoutMs, window);
  }

  $: taskQueueView = createTaskQueueView({
    tasks,
    approvals,
    events,
    taskFilter,
    queueFilter,
    taskSearch,
    selectedTaskId
  });
  $: pendingApprovalItems = taskQueueView.pendingApprovalItems;
  $: activeTaskItems = taskQueueView.activeTaskItems;
  $: attentionTaskItems = taskQueueView.attentionTaskItems;
  $: visibleTaskItems = taskQueueView.visibleTaskItems;
  $: currentTask = taskQueueView.currentTask;
  $: currentTaskEvents = taskQueueView.currentTaskEvents;
  $: currentTaskRuns = currentTask
    ? buildWorkerTraceRuns(currentTaskEvents, taskRuns[currentTask.id] || [])
    : [];
  $: currentTaskDiff = currentTask ? taskDiffs[currentTask.id] : undefined;
  $: currentDiffFiles = currentTaskDiff ? parseUnifiedDiff(currentTaskDiff.raw_diff) : [];
  $: if (currentDiffFiles.length && !currentDiffFiles.some((file) => diffFileKey(file) === selectedDiffFilePath)) {
    selectedDiffFilePath = diffFileKey(currentDiffFiles[0]);
  }
  $: if (!currentDiffFiles.length && selectedDiffFilePath) {
    selectedDiffFilePath = '';
  }
  $: currentDiffFile = currentDiffFiles.find((file) => diffFileKey(file) === selectedDiffFilePath);
  $: currentSplitRows = buildSplitRows(currentDiffFile);
  $: emptyTaskListMessage = taskListEmptyMessage({
    apiBase,
    refreshing,
    taskLoadError,
    totalTasks: tasks.length
  });
  $: if (!currentTask?.id && (loadedRunsTaskId || loadedDiffTaskId || selectedDiffFilePath)) {
    loadedRunsTaskId = '';
    loadedDiffTaskId = '';
    selectedDiffFilePath = '';
  }
  $: needsActionTotal =
    attentionTaskItems.length + pendingApprovalItems.filter((approval) => !approval.task_id).length;
  $: onlineAgentItems = agents.filter((agent) => agent.status !== 'offline');
  $: if (!selectedAgentId && onlineAgentItems[0]) {
    selectedAgentId = onlineAgentItems[0].id;
  }
  $: selectedAgent =
    agents.find((agent) => agent.id === selectedAgentId) || onlineAgentItems[0] || agents[0];
  $: selectedWorkdirs = selectedAgent?.workdirs || [];
  $: if (
    selectedWorkdirs.length &&
    !selectedWorkdirs.some((workdir) => workdir.id === selectedWorkdirId)
  ) {
    selectedWorkdirId = selectedWorkdirs[0].id;
  }
  $: selectedWorkdir =
    selectedWorkdirs.find((workdir) => workdir.id === selectedWorkdirId) || selectedWorkdirs[0];
  $: queueOptions = [
    { id: 'all', label: 'All queues', count: tasks.length, detail: 'Local and remote targets' },
    {
      id: 'local',
      label: 'Local homelabd',
      count: tasks.filter((task) => task.target?.mode !== 'remote').length,
      detail: 'Local worktrees'
    },
    ...agents.map((agent) => ({
      id: `agent:${agent.id}` as TaskQueueFilter,
      label: agent.name || agent.id,
      count: tasks.filter((task) => task.target?.mode === 'remote' && task.target?.agent_id === agent.id)
        .length,
      detail: `${agent.machine || 'unknown'} / ${agent.status}`
    }))
  ];
  $: selectedContextLabel =
    selectedAgent && selectedWorkdir
      ? `${selectedAgent.name || selectedAgent.id} on ${selectedAgent.machine || 'unknown'} in ${selectedWorkdir.path}`
      : 'Local homelabd workspace';
  $: currentPendingApproval = pendingApprovalForTask(currentTask, approvals);
  $: currentPrimaryAction = primaryTaskAction(currentTask, approvals);
  $: currentSecondaryOperations = secondaryTaskOperations(currentTask, approvals);

  const eventLabel = (event: HomelabdEvent) => event.type.replaceAll('.', ' ');

  const planStatusLabel = (status = '') => status.replaceAll('_', ' ') || 'planned';

  const eventDetail = (event: HomelabdEvent) => {
    if (!event.payload) {
      return '';
    }
    if (typeof event.payload === 'string') {
      return truncate(event.payload, 180);
    }
    if (typeof event.payload !== 'object') {
      return truncate(String(event.payload), 180);
    }
    const payload = event.payload as Record<string, unknown>;
    for (const key of ['message', 'content', 'reply', 'result', 'error', 'reason', 'command', 'summary']) {
      const value = payload[key];
      if (typeof value === 'string' && value.trim()) {
        return truncate(value, 180);
      }
    }
    return truncate(JSON.stringify(payload), 180);
  };

  const runStatusTone = (run: WorkerTraceRun) => {
    if (run.active || run.status === 'running') {
      return 'blue';
    }
    if (run.status === 'failed') {
      return 'red';
    }
    if (run.status === 'ignored') {
      return 'amber';
    }
    return 'green';
  };

  const runOutput = (run: WorkerTraceRun) => run.output || run.artifact?.output || '';

  const runCommand = (run: WorkerTraceRun) => run.command?.join(' ') || 'external worker';

  const diffFileKey = (file: ParsedDiffFile) => `${file.oldPath || ''}->${file.path}`;

  const diffStatusLabel = (status = '') => status.replaceAll('_', ' ') || 'modified';

  const diffFileTitle = (file: ParsedDiffFile) =>
    file.oldPath && file.oldPath !== file.path ? `${file.oldPath} -> ${file.path}` : file.path;

  const diffLineMarker = (line?: ParsedDiffLine) => {
    if (!line) {
      return '';
    }
    if (line.kind === 'add') {
      return '+';
    }
    if (line.kind === 'delete') {
      return '-';
    }
    return ' ';
  };

  const diffLineSegments = (
    line?: ParsedDiffLine,
    partner?: ParsedDiffLine,
    side: 'left' | 'right' = 'left'
  ): InlineSegment[] => {
    if (!line) {
      return [{ text: '', changed: false }];
    }
    if (partner && line.kind !== 'context') {
      const segments =
        side === 'left'
          ? inlineChangeSegments(line.content, partner.content)
          : inlineChangeSegments(partner.content, line.content);
      return side === 'left' ? segments.left : segments.right;
    }
    return [{ text: line.content, changed: false }];
  };

  const taskTone = (task: HomelabdTask) => {
    if (task.status === 'blocked' || task.status === 'failed' || task.status === 'conflict_resolution') {
      return 'red';
    }
    if (
      task.status === 'ready_for_review' ||
      task.status === 'awaiting_approval' ||
      task.status === 'awaiting_verification'
    ) {
      return 'amber';
    }
    if (taskIsActive(task)) {
      return 'blue';
    }
    if (task.status === 'done') {
      return 'green';
    }
    return 'gray';
  };

  const refreshTaskDiff = async (taskId: string) => {
    if (!taskId) {
      return;
    }
    diffLoadingTaskId = taskId;
    diffError = '';
    try {
      const result = await withRefreshTimeout('Task diff', client.getTaskDiff(taskId));
      taskDiffs = { ...taskDiffs, [taskId]: result };
    } catch (err) {
      diffError = errorMessage(err, 'Unable to load task diff.');
    } finally {
      if (diffLoadingTaskId === taskId) {
        diffLoadingTaskId = '';
      }
    }
  };

  const refreshTaskRuns = async (taskId: string) => {
    if (!taskId) {
      return;
    }
    try {
      const result = await withRefreshTimeout('Worker runs', client.listTaskRuns(taskId));
      taskRuns = { ...taskRuns, [taskId]: result.runs };
    } catch (err) {
      setNotice('error', 'Worker runs failed', errorMessage(err, 'Unable to load worker runs.'));
    }
  };

  const refreshSelectedTaskDetails = async (
    taskId: string,
    options: RefreshSelectedTaskDetailsOptions = {}
  ) => {
    if (!taskId) {
      return;
    }

    const selected = options.task || tasks.find((task) => task.id === taskId);
    const detailTasks: Promise<void>[] = [];

    if (options.force || loadedRunsTaskId !== taskId) {
      loadedRunsTaskId = taskId;
      detailTasks.push(refreshTaskRuns(taskId));
    }

    if (selected && !isRemoteTask(selected) && (options.force || loadedDiffTaskId !== taskId)) {
      loadedDiffTaskId = taskId;
      if (options.resetDiffSelection) {
        selectedDiffFilePath = '';
      }
      detailTasks.push(refreshTaskDiff(taskId));
    }

    await Promise.allSettled(detailTasks);
  };

  const applySecondaryRefresh = async (
    sequence: number,
    requests: {
      approvals: Promise<unknown>;
      events: Promise<unknown>;
      agents: Promise<unknown>;
    },
    baseTasks: HomelabdTask[],
    initialErrors: string[] = []
  ) => {
    const refreshErrors = [...initialErrors];
    let nextApprovals = approvals;
    const [approvalResult, eventResult, agentResult] = await Promise.allSettled([
      requests.approvals,
      requests.events,
      requests.agents
    ]);
    if (sequence !== refreshStateSequence) {
      return;
    }

    if (approvalResult.status === 'fulfilled') {
      try {
        const approvalItems = collectionFromResponse<HomelabdApproval>(
          'Approvals',
          'approvals',
          approvalResult.value
        );
        nextApprovals = [...approvalItems].sort(
          (left, right) => Date.parse(right.updated_at) - Date.parse(left.updated_at)
        );
        approvals = nextApprovals;
      } catch (err) {
        refreshErrors.push(errorMessage(err, 'Unable to load approvals.'));
      }
    } else {
      refreshErrors.push(errorMessage(approvalResult.reason, 'Unable to load approvals.'));
    }

    if (eventResult.status === 'fulfilled') {
      try {
        events = collectionFromResponse<HomelabdEvent>('Events', 'events', eventResult.value);
      } catch (err) {
        refreshErrors.push(errorMessage(err, 'Unable to load events.'));
      }
    } else {
      refreshErrors.push(errorMessage(eventResult.reason, 'Unable to load events.'));
    }

    if (agentResult.status === 'fulfilled') {
      try {
        agents = collectionFromResponse<HomelabdRemoteAgent>('Agents', 'agents', agentResult.value);
      } catch (err) {
        refreshErrors.push(errorMessage(err, 'Unable to load agents.'));
      }
    } else {
      refreshErrors.push(errorMessage(agentResult.reason, 'Unable to load agents.'));
    }

    const syncSelection = resolveTaskSyncSelection({
      tasks: baseTasks,
      approvals: nextApprovals,
      taskFilter,
      queueFilter,
      taskSearch,
      selectedTaskId
    });
    selectedTaskId = syncSelection.selectedTaskId;
    if (refreshErrors.length) {
      setNotice('error', 'Sync incomplete', refreshErrors.join(' '));
    }
  };

  const refreshState = async () => {
    const sequence = (refreshStateSequence += 1);
    refreshing = true;
    const refreshErrors: string[] = [];
    let nextTasks = tasks;
    const taskRequest = withRefreshTimeout('Tasks', client.listTasks());
    const approvalRequest = withRefreshTimeout('Approvals', client.listApprovals());
    const eventRequest = withRefreshTimeout('Events', client.listEvents({ limit: 500 }));
    const agentRequest = withRefreshTimeout('Agents', client.listAgents());
    try {
      const taskResult = await Promise.resolve(taskRequest).then(
        (value) => ({ status: 'fulfilled' as const, value }),
        (reason) => ({ status: 'rejected' as const, reason })
      );
      if (sequence !== refreshStateSequence) {
        void Promise.allSettled([approvalRequest, eventRequest, agentRequest]);
        return;
      }

      if (taskResult.status === 'fulfilled') {
        try {
          nextTasks = collectionFromResponse<HomelabdTask>('Tasks', 'tasks', taskResult.value).sort(
            (left, right) => Date.parse(right.updated_at) - Date.parse(left.updated_at)
          );
          tasks = nextTasks;
          taskLoadError = '';
        } catch (err) {
          taskLoadError = errorMessage(err, 'Unable to load tasks.');
          refreshErrors.push(taskLoadError);
        }
      } else {
        taskLoadError = errorMessage(taskResult.reason, 'Unable to load tasks.');
        refreshErrors.push(taskLoadError);
      }

      const syncSelection = resolveTaskSyncSelection({
        tasks: nextTasks,
        approvals,
        taskFilter,
        queueFilter,
        taskSearch,
        selectedTaskId
      });
      selectedTaskId = syncSelection.selectedTaskId;
      if (!syncSelection.selectedTaskId) {
        loadedRunsTaskId = '';
        loadedDiffTaskId = '';
        selectedDiffFilePath = '';
      }
      lastRefresh = syncTimeLabel();
      void applySecondaryRefresh(
        sequence,
        {
          approvals: approvalRequest,
          events: eventRequest,
          agents: agentRequest
        },
        nextTasks,
        refreshErrors
      );
      if (syncSelection.shouldLoadRuns) {
        void refreshSelectedTaskDetails(syncSelection.selectedTaskId, {
          force: true,
          task: syncSelection.selectedTask
        });
      }
    } finally {
      if (sequence === refreshStateSequence) {
        refreshing = false;
      }
    }
  };

  onMount(() => {
    void refreshState();
    const interval = window.setInterval(() => {
      void refreshState();
    }, 8000);
    return () => window.clearInterval(interval);
  });

  const createTargetedTask = async () => {
    const goal = newTaskDraft.trim();
    if (!goal || creatingTask) {
      return;
    }
    creatingTask = true;
    clearNotice();
    try {
      const target =
        selectedAgent && selectedWorkdir
          ? {
              mode: 'remote',
              agent_id: selectedAgent.id,
              machine: selectedAgent.machine,
              workdir_id: selectedWorkdir.id,
              workdir: selectedWorkdir.path,
              backend: selectedAgent.capabilities?.includes('codex') ? 'codex' : undefined
            }
          : undefined;
      const response = await client.createTask({ goal, target });
      newTaskDraft = '';
      contextAcknowledged = false;
      setNotice('success', 'Task created', response.reply || 'Task created.');
      await refreshState();
      mobilePanel = 'queue';
    } catch (err) {
      setNotice('error', 'Task creation failed', errorMessage(err, 'Unable to create task.'));
    } finally {
      creatingTask = false;
    }
  };

  const selectTask = (id: string) => {
    selectedTaskId = id;
    mobilePanel = 'detail';
    loadedRunsTaskId = '';
    loadedDiffTaskId = '';
    selectedDiffFilePath = '';
    deleteConfirmTaskId = '';
    void refreshSelectedTaskDetails(id, {
      force: true,
      resetDiffSelection: true,
      task: tasks.find((task) => task.id === id)
    });
  };

  const syncSelectionForCurrentFilters = () => {
    const nextTaskId = selectTaskForQueue(
      tasks,
      approvals,
      taskFilter,
      queueFilter,
      taskSearch,
      selectedTaskId
    );
    selectedTaskId = nextTaskId;
    if (!nextTaskId) {
      loadedRunsTaskId = '';
      loadedDiffTaskId = '';
      selectedDiffFilePath = '';
      return;
    }
    void refreshSelectedTaskDetails(nextTaskId, {
      resetDiffSelection: true,
      task: tasks.find((task) => task.id === nextTaskId)
    });
  };

  const setTaskFilter = (filter: TaskFilter) => {
    taskFilter = filter;
    mobilePanel = 'queue';
    syncSelectionForCurrentFilters();
  };

  const setQueueFilter = (filter: TaskQueueFilter) => {
    queueFilter = filter;
    mobilePanel = 'queue';
    syncSelectionForCurrentFilters();
  };

  const handleTaskSearchInput = (event: Event) => {
    taskSearch = (event.currentTarget as HTMLInputElement).value;
    mobilePanel = 'queue';
    syncSelectionForCurrentFilters();
  };

  const handleAgentChange = () => {
    contextAcknowledged = false;
  };

  const actionLoadingKey = (operation: TaskOperation, taskId: string) => `${operation}:${taskId}`;

  const approvalLoadingKey = (operation: 'approve' | 'deny', approvalId: string) =>
    `${operation}:${approvalId}`;

  const performApprovalAction = async (
    approval: HomelabdApproval,
    operation: 'approve' | 'deny'
  ) => {
    const key = approvalLoadingKey(operation, approval.id);
    if (approvalLoading) {
      return;
    }
    approvalLoading = key;
    clearNotice();
    try {
      const response =
        operation === 'approve'
          ? await client.approveApproval(approval.id)
          : await client.denyApproval(approval.id);
      setNotice(
        'success',
        operation === 'approve' ? 'Approval granted' : 'Approval denied',
        response.reply || 'Approval updated.'
      );
      await refreshState();
      if (approval.task_id) {
        await refreshSelectedTaskDetails(approval.task_id, {
          force: true,
          task: tasks.find((task) => task.id === approval.task_id)
        });
      }
    } catch (err) {
      setNotice('error', 'Approval action failed', errorMessage(err, 'Unable to update approval.'));
    } finally {
      approvalLoading = '';
    }
  };

  const performTaskOperation = async (operation: TaskOperation) => {
    if (!currentTask || actionLoading) {
      return;
    }
    const taskId = currentTask.id;
    if (operation === 'delete' && deleteConfirmTaskId !== taskId) {
      deleteConfirmTaskId = taskId;
      setNotice('info', 'Confirm delete', 'Press Delete again to remove the task record and workspace.');
      return;
    }

    const key = actionLoadingKey(operation, taskId);
    actionLoading = key;
    clearNotice();
    try {
      const response = await (async () => {
        switch (operation) {
          case 'run':
            return client.runTask(taskId);
          case 'review':
            return client.reviewTask(taskId);
          case 'accept':
            return client.acceptTask(taskId);
          case 'reopen':
            return client.reopenTask(taskId, { reason: reopenReason.trim() });
          case 'cancel':
            return client.cancelTask(taskId);
          case 'retry':
            return client.retryTask(taskId, {
              backend: retryBackend,
              instruction: retryInstruction.trim()
            });
          case 'delete':
            return client.deleteTask(taskId);
        }
      })();
      setNotice('success', `${taskOperationLabel(operation)} submitted`, response.reply || 'Done.');
      if (operation === 'reopen') {
        reopenReason = '';
      }
      if (operation === 'retry') {
        retryInstruction = '';
      }
      if (operation === 'delete') {
        selectedTaskId = '';
        mobilePanel = 'queue';
      }
      deleteConfirmTaskId = '';
      await refreshState();
      if (operation !== 'delete') {
        await refreshSelectedTaskDetails(taskId, {
          force: true,
          task: tasks.find((task) => task.id === taskId)
        });
      }
    } catch (err) {
      setNotice('error', `${taskOperationLabel(operation)} failed`, errorMessage(err, 'Task action failed.'));
    } finally {
      actionLoading = '';
    }
  };

  const performPrimaryAction = () => {
    if (currentPrimaryAction.type === 'task') {
      void performTaskOperation(currentPrimaryAction.operation);
    }
    if (currentPrimaryAction.type === 'approval') {
      void performApprovalAction(currentPrimaryAction.approval, currentPrimaryAction.operation);
    }
  };

  const operationBusy = (operation: TaskOperation) =>
    Boolean(currentTask && actionLoading === actionLoadingKey(operation, currentTask.id));

  const operationButtonText = (operation: TaskOperation) => {
    if (operationBusy(operation)) {
      switch (operation) {
        case 'run':
          return 'Starting';
        case 'review':
          return 'Reviewing';
        case 'accept':
          return 'Accepting';
        case 'reopen':
          return 'Reopening';
        case 'cancel':
          return 'Stopping';
        case 'retry':
          return 'Retrying';
        case 'delete':
          return 'Deleting';
      }
    }
    if (operation === 'delete' && currentTask && deleteConfirmTaskId === currentTask.id) {
      return 'Confirm delete';
    }
    return taskOperationLabel(operation);
  };
</script>

<svelte:head>
  <title>homelabd Tasks</title>
  <meta name="description" content="Button-driven homelabd task queue and review console" />
</svelte:head>

<div class="tasks-page">
  <Navbar title="Tasks" subtitle="homelabd" current="/tasks" apiBase={apiBase} />

  <nav class="mobile-tabs" aria-label="Task panels">
    <button
      type="button"
      class:active={mobilePanel === 'queue'}
      on:click={() => (mobilePanel = 'queue')}
    >
      Queue <span>{visibleTaskItems.length}</span>
    </button>
    <button
      type="button"
      class:active={mobilePanel === 'detail'}
      disabled={!currentTask}
      on:click={() => (mobilePanel = 'detail')}
    >
      Task <span>{currentTask ? shortID(currentTask.id) : '-'}</span>
    </button>
  </nav>

  <div class="shell">
    <aside class="task-pane" data-mobile-hidden={mobilePanel !== 'queue'} aria-label="Task queue">
      <header class="task-header">
        <div>
          <p>Task queue</p>
          <h1>{needsActionTotal} need action</h1>
          <span>Synced {lastRefresh || 'never'}</span>
        </div>
        <button type="button" disabled={refreshing} on:click={() => void refreshState()}>
          {refreshing ? 'Syncing' : 'Sync'}
        </button>
      </header>

      <section class="triage" aria-label="Task filters">
        {#each [
          { id: 'attention', label: 'Needs action', count: needsActionTotal },
          { id: 'active', label: 'Running', count: activeTaskItems.length },
          { id: 'all', label: 'All', count: tasks.length }
        ] as filter}
          <button
            type="button"
            class:active={taskFilter === filter.id}
            on:click={() => setTaskFilter(filter.id as TaskFilter)}
          >
            <strong>{filter.count}</strong>
            <span>{filter.label}</span>
          </button>
        {/each}
      </section>

      <label class="hidden" for="task-search">Search tasks</label>
      <input
        id="task-search"
        bind:value={taskSearch}
        placeholder="Search tasks"
        on:input={handleTaskSearchInput}
      />

      <section class="task-list" aria-label="Task list">
        {#if visibleTaskItems.length === 0}
          <p class="empty">{emptyTaskListMessage}</p>
        {:else}
          {#each visibleTaskItems as task}
            <button
              type="button"
              class="task-row"
              class:selected={currentTask?.id === task.id}
              on:click={() => selectTask(task.id)}
            >
              <span class={`dot ${taskTone(task)}`} aria-hidden="true"></span>
              <span class="task-copy">
                <strong>{taskSummaryTitle(task, 84)}</strong>
                <small>
                  <span>{shortID(task.id)} / updated {compactTime(task.updated_at)}</span>
                  <span class={`status ${taskTone(task)}`}>{statusLabel(task.status)}</span>
                </small>
                <em>
                  {targetLabel(task)}
                  {#if pendingApprovalForTask(task, approvals)}
                    / approval pending
                  {/if}
                </em>
              </span>
            </button>
          {/each}
        {/if}
      </section>

      {#if taskLoadError}
        <section class="sync-alert" role="alert" aria-label="Task sync status">
          <strong>Task sync failed</strong>
          <span>{taskLoadError}</span>
        </section>
      {/if}

      {#if pendingApprovalItems.length}
        <section class="approval-list" aria-label="Pending approvals">
          <h2>Pending approvals</h2>
          {#each pendingApprovalItems as approval}
            <article>
              <span class="dot amber" aria-hidden="true"></span>
              <div>
                <strong>{approval.tool}</strong>
                <small>{shortID(approval.id)}{approval.task_id ? ` / ${shortID(approval.task_id)}` : ''}</small>
                <p>{truncate(approval.reason, 96)}</p>
                <div class="mini-actions">
                  <button
                    type="button"
                    disabled={approvalLoading !== ''}
                    on:click={() => void performApprovalAction(approval, 'approve')}
                  >
                    {approvalLoading === approvalLoadingKey('approve', approval.id) ? 'Approving' : 'Approve'}
                  </button>
                  <button
                    type="button"
                    disabled={approvalLoading !== ''}
                    on:click={() => void performApprovalAction(approval, 'deny')}
                  >
                    {approvalLoading === approvalLoadingKey('deny', approval.id) ? 'Denying' : 'Deny'}
                  </button>
                </div>
              </div>
            </article>
          {/each}
        </section>
      {/if}

      <section class="queue-groups" aria-label="Execution queues">
        <h2>Execution queues</h2>
        {#each queueOptions as option}
          <button
            type="button"
            class:active={queueFilter === option.id}
            on:click={() => setQueueFilter(option.id)}
          >
            <strong>{option.label}</strong>
            <span>{option.count} task{option.count === 1 ? '' : 's'} / {option.detail}</span>
          </button>
        {/each}
      </section>

      <details class="target-create">
        <summary>New task</summary>
        <div class="target-create-body" aria-label="Create targeted task">
          <header>
            <div>
              <p>Target</p>
              <h2>{selectedAgent ? selectedAgent.name || selectedAgent.id : 'Local homelabd'}</h2>
            </div>
            <span>{onlineAgentItems.length} online</span>
          </header>
          {#if agents.length}
            <label class="hidden" for="agent-select">Remote agent</label>
            <select id="agent-select" bind:value={selectedAgentId} on:change={handleAgentChange}>
              {#each agents as agent}
                <option value={agent.id}>
                  {agent.name || agent.id} / {agent.machine || 'unknown'} / {agent.status}
                </option>
              {/each}
            </select>
            <label class="hidden" for="workdir-select">Remote directory</label>
            <select
              id="workdir-select"
              bind:value={selectedWorkdirId}
              disabled={!selectedWorkdirs.length}
              on:change={handleAgentChange}
            >
              {#each selectedWorkdirs as workdir}
                <option value={workdir.id}>{workdirLabel(workdir)} / {workdir.path}</option>
              {/each}
            </select>
          {/if}
          <form on:submit|preventDefault={createTargetedTask}>
            <label class="hidden" for="new-task-goal">New task goal</label>
            <textarea
              id="new-task-goal"
              bind:value={newTaskDraft}
              rows="3"
              placeholder={selectedAgent ? 'Describe remote work' : 'Describe local work'}
              disabled={creatingTask}
            ></textarea>
            {#if selectedAgent}
              <label class="context-confirm">
                <input type="checkbox" bind:checked={contextAcknowledged} disabled={!selectedAgent} />
                <span>Run on <strong>{selectedContextLabel}</strong></span>
              </label>
            {/if}
            <button
              type="submit"
              disabled={creatingTask ||
                !newTaskDraft.trim() ||
                Boolean(selectedAgent && (!selectedWorkdir || !contextAcknowledged))}
            >
              {creatingTask ? 'Creating' : selectedAgent ? 'Create remote task' : 'Create local task'}
            </button>
          </form>
        </div>
      </details>

      <footer>{apiBase}</footer>
    </aside>

    <main class="workbench" data-mobile-hidden={mobilePanel !== 'detail'} aria-label="Selected task record">
      {#if notice}
        <section class={`notice ${notice.tone}`} aria-live="polite">
          <div>
            <strong>{notice.title}</strong>
            <p>{notice.detail}</p>
          </div>
          <button type="button" on:click={clearNotice}>Dismiss</button>
        </section>
      {/if}

      {#if currentTask}
        <article class="task-record">
          <header class="record-header">
            <button type="button" class="back-to-queue" on:click={() => (mobilePanel = 'queue')}>
              Queue
            </button>
            <div>
              <p>Selected task</p>
              <h2>{taskSummaryTitle(currentTask)}</h2>
            </div>
            <span class={`status ${taskTone(currentTask)}`}>{statusLabel(currentTask.status)}</span>
          </header>

          <section class={`action-panel ${currentPrimaryAction.tone}`} aria-label="Task actions">
            <div>
              <span class={`dot ${taskTone(currentTask)}`} aria-hidden="true"></span>
              <div>
                <h3>{currentPrimaryAction.label}</h3>
                <p>{currentPrimaryAction.detail}</p>
              </div>
            </div>
            {#if currentPrimaryAction.type !== 'none'}
              <button
                type="button"
                class="primary-action"
                disabled={actionLoading !== '' || approvalLoading !== ''}
                on:click={performPrimaryAction}
              >
                {currentPrimaryAction.type === 'task'
                  ? operationButtonText(currentPrimaryAction.operation)
                  : approvalLoading === approvalLoadingKey(currentPrimaryAction.operation, currentPrimaryAction.approval.id)
                    ? 'Approving'
                    : currentPrimaryAction.label}
              </button>
            {:else}
              <button type="button" class="primary-action" disabled={refreshing} on:click={() => void refreshState()}>
                {refreshing ? 'Syncing' : 'Sync'}
              </button>
            {/if}
          </section>

          {#if currentTask.status === 'blocked' || currentTask.status === 'failed' || currentTask.status === 'conflict_resolution' || currentSecondaryOperations.includes('retry')}
            <section class="action-form" aria-label="Retry settings">
              <label>
                <span>Retry backend</span>
                <select bind:value={retryBackend}>
                  <option value="codex">Codex</option>
                  <option value="claude">Claude</option>
                  <option value="gemini">Gemini</option>
                </select>
              </label>
              <label>
                <span>Retry instruction</span>
                <textarea
                  rows="3"
                  bind:value={retryInstruction}
                  placeholder="Optional retry instruction"
                ></textarea>
              </label>
            </section>
          {/if}

          {#if currentTask.status === 'awaiting_verification' || currentSecondaryOperations.includes('reopen')}
            <section class="action-form" aria-label="Reopen reason">
              <label>
                <span>Reopen reason</span>
                <textarea rows="2" bind:value={reopenReason} placeholder="Optional reason"></textarea>
              </label>
            </section>
          {/if}

          {#if currentSecondaryOperations.length || currentPendingApproval}
            <section class="secondary-actions" aria-label="Secondary task actions">
              {#if currentPendingApproval}
                <button
                  type="button"
                  disabled={approvalLoading !== ''}
                  on:click={() => void performApprovalAction(currentPendingApproval, 'deny')}
                >
                  {approvalLoading === approvalLoadingKey('deny', currentPendingApproval.id) ? 'Denying' : 'Deny approval'}
                </button>
              {/if}
              {#each currentSecondaryOperations as operation}
                <button
                  type="button"
                  class:danger={operation === 'delete' || operation === 'cancel'}
                  disabled={actionLoading !== ''}
                  on:click={() => void performTaskOperation(operation)}
                >
                  {operationButtonText(operation)}
                </button>
              {/each}
            </section>
          {/if}

          <section class="record-summary" aria-label="Task summary">
            <div>
              <span>ID</span>
              <strong>{shortID(currentTask.id)}</strong>
            </div>
            <div>
              <span>Owner</span>
              <strong>{currentTask.assigned_to || 'unassigned'}</strong>
            </div>
            <div>
              <span>Target</span>
              <strong>{targetLabel(currentTask)}</strong>
            </div>
            <div>
              <span>Started</span>
              <strong>{compactTime(taskStartedAt(currentTask))}</strong>
            </div>
            <div>
              <span>Runtime</span>
              <strong>{compactDuration(taskRuntimeMs(currentTask))}</strong>
            </div>
            <div>
              <span>Updated</span>
              <strong>{compactTime(currentTask.updated_at)}</strong>
            </div>
          </section>

          <section class="state-machine" aria-label="Workflow state">
            <div>
              <span>Workflow state</span>
              <strong>{statusLabel(currentTask.status)}</strong>
            </div>
            <p>{taskStateDescription(currentTask.status)}</p>
            <small>Next: {taskStateTransitions(currentTask.status)}</small>
          </section>

          {#if currentTask.workspace}
            <section class="workspace-path" aria-label="Workspace path">
              <span>Workspace</span>
              <code>{currentTask.workspace}</code>
            </section>
          {/if}

          {#if currentTask.target?.mode === 'remote'}
            <section class="execution-context" aria-label="Execution context">
              <span>Remote execution context</span>
              <strong>{currentTask.target.machine || currentTask.target.agent_id}</strong>
              <code>{currentTask.target.workdir}</code>
              <small>Agent {currentTask.target.agent_id} / backend {currentTask.target.backend || 'default'}</small>
            </section>
          {/if}

          {#if currentTask.result}
            <section class="task-result" aria-label="Task result">
              <h3>Result</h3>
              <p>{currentTask.result}</p>
            </section>
          {/if}

          <section class="diff-review" aria-label="Task diff">
            <header>
              <div>
                <p>Changes vs main</p>
                <h3>
                  {#if currentTaskDiff}
                    {currentTaskDiff.summary.files} file{currentTaskDiff.summary.files === 1 ? '' : 's'}
                    <span>+{currentTaskDiff.summary.additions} / -{currentTaskDiff.summary.deletions}</span>
                  {:else}
                    Diff
                  {/if}
                </h3>
              </div>
              <div class="diff-controls" aria-label="Diff controls">
                <button
                  type="button"
                  class:active={diffMode === 'split'}
                  on:click={() => (diffMode = 'split')}
                >
                  Split
                </button>
                <button
                  type="button"
                  class:active={diffMode === 'unified'}
                  on:click={() => (diffMode = 'unified')}
                >
                  Unified
                </button>
                <button
                  type="button"
                  disabled={diffLoadingTaskId === currentTask.id || isRemoteTask(currentTask)}
                  on:click={() => !isRemoteTask(currentTask) && void refreshTaskDiff(currentTask.id)}
                >
                  {diffLoadingTaskId === currentTask.id ? 'Loading' : 'Refresh'}
                </button>
              </div>
            </header>

            {#if diffError}
              <p class="error" role="alert">{diffError}</p>
            {/if}

            {#if isRemoteTask(currentTask)}
              <p class="empty">Remote diffs are recorded by the remote agent.</p>
            {:else if diffLoadingTaskId === currentTask.id && !currentTaskDiff}
              <p class="empty">Loading task diff...</p>
            {:else if currentTaskDiff && !currentTaskDiff.raw_diff.trim()}
              <p class="empty">No diff found between this task branch and current main.</p>
            {:else if currentTaskDiff}
              <div class="diff-meta">
                <span>{currentTaskDiff.base_label || 'main'} -> {currentTaskDiff.head_label || shortID(currentTaskDiff.task_id)}</span>
                {#if currentTaskDiff.base_ref && currentTaskDiff.head_ref}
                  <code>{currentTaskDiff.base_ref.slice(0, 8)}...{currentTaskDiff.head_ref.slice(0, 8)}</code>
                {/if}
              </div>

              <div class="diff-layout">
                <nav class="diff-file-list" aria-label="Changed files">
                  {#each currentDiffFiles as file}
                    <button
                      type="button"
                      class:selected={diffFileKey(file) === selectedDiffFilePath}
                      on:click={() => (selectedDiffFilePath = diffFileKey(file))}
                    >
                      <span>{diffFileTitle(file)}</span>
                      <small>{diffStatusLabel(file.status)} / +{file.additions} / -{file.deletions}</small>
                    </button>
                  {/each}
                </nav>

                <article class="diff-file" aria-label="Selected file diff">
                  {#if currentDiffFile}
                    <header>
                      <div>
                        <strong>{diffFileTitle(currentDiffFile)}</strong>
                        <small>{diffStatusLabel(currentDiffFile.status)}</small>
                      </div>
                      <span>+{currentDiffFile.additions} / -{currentDiffFile.deletions}</span>
                    </header>

                    {#if currentDiffFile.binary}
                      <p class="empty compact">Binary diff metadata is available, but binary content is not rendered inline.</p>
                    {/if}

                    {#if diffMode === 'split'}
                      <div class="diff-scroll" data-mode="split">
                        <div class="split-diff" role="table" aria-label="Split diff">
                          {#each currentSplitRows as row}
                            {#if row.kind === 'hunk' || row.kind === 'meta'}
                              <div class={`split-row full ${row.kind}`} role="row">
                                <code>{row.label}</code>
                              </div>
                            {:else}
                              <div class={`split-row ${row.kind}`} role="row">
                                <span class="line-no old">{row.left?.oldNumber ?? ''}</span>
                                <code class:blank={!row.left}>
                                  {#if row.left}
                                    <span class="marker">{diffLineMarker(row.left)}</span>
                                    {#each diffLineSegments(row.left, row.kind === 'change' ? row.right : undefined, 'left') as segment}
                                      <span class:changed={segment.changed}>{segment.text}</span>
                                    {/each}
                                  {/if}
                                </code>
                                <span class="line-no new">{row.right?.newNumber ?? ''}</span>
                                <code class:blank={!row.right}>
                                  {#if row.right}
                                    <span class="marker">{diffLineMarker(row.right)}</span>
                                    {#each diffLineSegments(row.right, row.kind === 'change' ? row.left : undefined, 'right') as segment}
                                      <span class:changed={segment.changed}>{segment.text}</span>
                                    {/each}
                                  {/if}
                                </code>
                              </div>
                            {/if}
                          {/each}
                        </div>
                      </div>
                    {:else}
                      <div class="diff-scroll" data-mode="unified">
                        <div class="unified-diff" role="table" aria-label="Unified diff">
                          {#each currentDiffFile.headerLines as line}
                            <div class="diff-row meta" role="row">
                              <span class="line-no"></span>
                              <span class="line-no"></span>
                              <code>{line}</code>
                            </div>
                          {/each}
                          {#each currentDiffFile.hunks as hunk}
                            <div class="diff-row hunk" role="row">
                              <span class="line-no"></span>
                              <span class="line-no"></span>
                              <code>{hunk.header}</code>
                            </div>
                            {#each hunk.lines as line}
                              <div class={`diff-row ${line.kind}`} role="row">
                                <span class="line-no old">{line.oldNumber ?? ''}</span>
                                <span class="line-no new">{line.newNumber ?? ''}</span>
                                <code>
                                  <span class="marker">{diffLineMarker(line)}</span>{line.content}
                                </code>
                              </div>
                            {/each}
                          {/each}
                        </div>
                      </div>
                    {/if}
                  {:else}
                    <p class="empty">Select a changed file to inspect.</p>
                  {/if}
                </article>
              </div>
            {:else}
              <p class="empty">Select a task to load its branch diff.</p>
            {/if}
          </section>

          <section class="worker-runs" aria-label="Worker runs">
            <header>
              <div>
                <p>Worker trace</p>
                <h3>{currentTaskRuns.length} run{currentTaskRuns.length === 1 ? '' : 's'}</h3>
              </div>
            </header>

            {#if currentTaskRuns.length === 0}
              <p class="empty">No external worker runs recorded for this task.</p>
            {:else}
              <div class="run-list">
                {#each currentTaskRuns as run}
                  <article class={`worker-run ${runStatusTone(run)}`}>
                    <header>
                      <span class={`dot ${runStatusTone(run)}`} aria-hidden="true"></span>
                      <div>
                        <strong>{run.backend}</strong>
                        <small>{shortID(run.id)} / {run.status} / {compactTime(run.startedAt)}</small>
                      </div>
                      {#if run.artifact}
                        <span class="artifact-pill">artifact</span>
                      {/if}
                    </header>
                    <p class="run-command">{runCommand(run)}</p>
                    {#if run.artifact?.path}
                      <p class="run-artifact-path">{run.artifact.path}</p>
                    {/if}
                    {#if run.error}
                      <p class="run-error">{run.error}</p>
                    {/if}
                    {#if runOutput(run)}
                      <pre>{runOutput(run)}</pre>
                    {:else}
                      <p class="empty compact">No output captured yet.</p>
                    {/if}
                  </article>
                {/each}
              </div>
            {/if}
          </section>

          <section class="activity" aria-label="Task activity">
            <header>
              <div>
                <p>Task activity</p>
                <h3>{currentTaskEvents.length} recent event{currentTaskEvents.length === 1 ? '' : 's'}</h3>
              </div>
            </header>
            {#if currentTaskEvents.length === 0}
              <p class="empty">No task-specific events loaded yet.</p>
            {:else}
              <ol>
                {#each currentTaskEvents as event}
                  <li>
                    <time>{compactTime(event.time)}</time>
                    <div>
                      <strong>{eventLabel(event)}</strong>
                      <span>{event.actor}</span>
                      {#if eventDetail(event)}
                        <p>{eventDetail(event)}</p>
                      {/if}
                    </div>
                  </li>
                {/each}
              </ol>
            {/if}
          </section>

          {#if currentTask.plan}
            <section class="task-plan" aria-label="Task plan">
              <header>
                <div>
                  <p>Reviewed plan</p>
                  <h3>{currentTask.plan.summary}</h3>
                </div>
                <span>{planStatusLabel(currentTask.plan.status)}</span>
              </header>
              {#if currentTask.plan.steps?.length}
                <ol>
                  {#each currentTask.plan.steps as step}
                    <li>
                      <strong>{step.title}</strong>
                      {#if step.detail}
                        <p>{step.detail}</p>
                      {/if}
                    </li>
                  {/each}
                </ol>
              {/if}
              {#if currentTask.plan.risks?.length}
                <div class="plan-risks">
                  <strong>Risks</strong>
                  <ul>
                    {#each currentTask.plan.risks as risk}
                      <li>{risk}</li>
                    {/each}
                  </ul>
                </div>
              {/if}
              {#if currentTask.plan.review}
                <p class="plan-review">{currentTask.plan.review}</p>
              {/if}
            </section>
          {/if}

          <section class="task-input" aria-label="Original task input">
            <h3>Original input</h3>
            <p>{taskInputText(currentTask)}</p>
          </section>
        </article>
      {:else}
        <section class="empty-record">
          <span class="dot gray" aria-hidden="true"></span>
          <div>
            <h2>Select a task</h2>
            <p>Use the queue to inspect status, review changes, and run direct actions.</p>
          </div>
        </section>
      {/if}
    </main>
  </div>
</div>

<style>
  :global(html),
  :global(body),
  :global(body > div) {
    height: 100%;
  }

  :global(body) {
    margin: 0;
    color: var(--text, #172033);
    background: var(--bg, #f5f7fb);
    font-family:
      Inter, ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI",
      sans-serif;
  }

  button,
  input,
  select,
  textarea {
    font: inherit;
  }

  button {
    cursor: pointer;
  }

  button:disabled {
    cursor: not-allowed;
    opacity: 0.58;
  }

  .tasks-page {
    display: grid;
    grid-template-rows: auto minmax(0, 1fr);
    height: 100dvh;
    background: var(--bg, #f5f7fb);
  }

  .mobile-tabs {
    display: none;
  }

  .shell {
    display: grid;
    grid-template-columns: minmax(20rem, 25rem) minmax(0, 1fr);
    min-height: 0;
    overflow: hidden;
  }

  .task-pane,
  .workbench {
    min-height: 0;
    overflow: hidden;
  }

  .task-pane {
    display: grid;
    grid-template-rows: auto auto auto minmax(0, 1fr) auto auto auto auto;
    gap: 0.75rem;
    padding: 1rem;
    border-right: 1px solid var(--border-soft, #dde4ef);
    background: var(--surface, #ffffff);
  }

  .task-header,
  .record-header,
  .triage,
  .task-row,
  .approval-list article,
  .action-panel,
  .secondary-actions,
  .notice {
    display: flex;
    align-items: center;
  }

  .task-header {
    justify-content: space-between;
    gap: 0.75rem;
  }

  .task-header p,
  .task-header h1,
  .task-header span,
  .record-header p,
  .record-header h2,
  .action-panel h3,
  .action-panel p,
  .approval-list p,
  .empty,
  footer {
    margin: 0;
  }

  .task-header p,
  .task-header span,
  .record-header p,
  .record-summary span,
  .workspace-path span,
  .activity header p,
  .worker-runs header p,
  .diff-review header p,
  .task-plan header p,
  .plan-risks > strong,
  .action-form span {
    color: var(--muted, #64748b);
    font-size: 0.72rem;
    font-weight: 800;
    letter-spacing: 0.06em;
    text-transform: uppercase;
  }

  .task-header h1,
  .record-header h2 {
    color: var(--text-strong, #111827);
    font-size: 1.25rem;
    line-height: 1.15;
  }

  .task-header button,
  .triage button,
  .queue-groups button,
  .mini-actions button,
  .secondary-actions button,
  .diff-controls button,
  .diff-file-list button,
  .primary-action,
  .notice button,
  .back-to-queue,
  .target-create summary {
    min-height: 2.55rem;
    padding: 0 0.75rem;
    border: 1px solid var(--border, #cbd5e1);
    border-radius: 0.55rem;
    color: var(--text, #243047);
    background: var(--surface, #ffffff);
    font-size: 0.84rem;
    font-weight: 800;
  }

  .task-header button:hover:not(:disabled),
  .triage button:hover,
  .queue-groups button:hover,
  .mini-actions button:hover:not(:disabled),
  .secondary-actions button:hover:not(:disabled),
  .diff-controls button:hover:not(:disabled),
  .diff-file-list button:hover:not(:disabled),
  .primary-action:hover:not(:disabled),
  .notice button:hover,
  .back-to-queue:hover,
  .target-create summary:hover {
    border-color: var(--accent, #2563eb);
    background: var(--surface-hover, #eef5ff);
  }

  .triage {
    gap: 0.5rem;
  }

  .triage button {
    flex: 1;
    display: grid;
    gap: 0.1rem;
    min-width: 0;
    padding: 0.65rem;
    border-color: var(--border-soft, #e2e8f0);
    background: var(--surface-muted, #f8fafc);
    text-align: left;
  }

  .triage button.active,
  .queue-groups button.active,
  .diff-controls button.active {
    border-color: var(--accent, #2563eb);
    color: var(--accent, #2563eb);
    background: var(--surface-hover, #eef5ff);
  }

  .triage strong {
    color: var(--text-strong, #0f172a);
    font-size: 1.1rem;
  }

  .triage span,
  footer {
    color: var(--muted, #64748b);
    font-size: 0.74rem;
  }

  input,
  select,
  textarea {
    box-sizing: border-box;
    width: 100%;
    border: 1px solid var(--border, #cbd5e1);
    border-radius: 0.65rem;
    color: var(--text-strong, #111827);
    background: var(--surface, #ffffff);
  }

  input,
  select {
    min-height: 2.65rem;
    padding: 0 0.75rem;
  }

  textarea {
    min-height: 4.2rem;
    max-height: 12rem;
    padding: 0.75rem;
    line-height: 1.45;
    resize: vertical;
  }

  input:focus,
  select:focus,
  textarea:focus,
  button:focus-visible,
  summary:focus-visible {
    border-color: var(--accent, #2563eb);
    outline: 3px solid rgb(37 99 235 / 0.16);
  }

  .task-list {
    display: grid;
    align-content: start;
    gap: 0.35rem;
    min-height: 0;
    overflow-y: auto;
    padding-right: 0.15rem;
  }

  .task-row {
    gap: 0.7rem;
    width: 100%;
    min-width: 0;
    min-height: 4.2rem;
    padding: 0.72rem;
    border: 1px solid transparent;
    border-radius: 0.75rem;
    color: inherit;
    background: transparent;
    text-align: left;
  }

  .task-row:hover,
  .task-row.selected {
    border-color: var(--border-soft, #d7e3f5);
    background: var(--surface-muted, #f3f7fc);
  }

  .task-copy {
    display: grid;
    gap: 0.18rem;
    min-width: 0;
  }

  .task-copy strong {
    overflow: hidden;
    color: var(--text-strong, #111827);
    font-size: 0.9rem;
    line-height: 1.25;
    text-overflow: ellipsis;
  }

  .task-copy small {
    display: flex;
    align-items: center;
    gap: 0.4rem;
    min-width: 0;
    color: var(--muted, #64748b);
    font-size: 0.76rem;
  }

  .task-copy em {
    overflow: hidden;
    color: var(--muted, #475569);
    font-size: 0.73rem;
    font-style: normal;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .task-copy small > span:first-child {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .status {
    flex: 0 0 auto;
    padding: 0.16rem 0.48rem;
    border-radius: 999px;
    font-size: 0.68rem;
    font-weight: 850;
    line-height: 1.25;
  }

  .status.red {
    color: #991b1b;
    background: #fee2e2;
  }

  .status.amber {
    color: #92400e;
    background: #fef3c7;
  }

  .status.blue {
    color: #1d4ed8;
    background: #dbeafe;
  }

  .status.green {
    color: #166534;
    background: #dcfce7;
  }

  .status.gray {
    color: #475569;
    background: #e2e8f0;
  }

  .dot {
    flex: 0 0 auto;
    width: 0.72rem;
    height: 0.72rem;
    border-radius: 999px;
    background: #94a3b8;
    box-shadow: 0 0 0 3px rgb(148 163 184 / 0.18);
  }

  .dot.red {
    background: #ef4444;
    box-shadow: 0 0 0 3px rgb(239 68 68 / 0.18);
  }

  .dot.amber {
    background: #f59e0b;
    box-shadow: 0 0 0 3px rgb(245 158 11 / 0.2);
  }

  .dot.blue {
    background: #3b82f6;
    box-shadow: 0 0 0 3px rgb(59 130 246 / 0.18);
  }

  .dot.green {
    background: #22c55e;
    box-shadow: 0 0 0 3px rgb(34 197 94 / 0.18);
  }

  .empty {
    padding: 1rem;
    color: var(--muted, #64748b);
    text-align: center;
  }

  .sync-alert,
  .notice {
    gap: 0.75rem;
    justify-content: space-between;
    padding: 0.75rem;
    border: 1px solid #fecaca;
    border-radius: 0.75rem;
    color: #7f1d1d;
    background: #fef2f2;
  }

  .notice {
    margin: 1rem 1.25rem 0;
  }

  .notice.success {
    border-color: #bbf7d0;
    color: #166534;
    background: #f0fdf4;
  }

  .notice.info {
    border-color: #bfdbfe;
    color: #1e40af;
    background: #eff6ff;
  }

  .notice strong,
  .notice p,
  .sync-alert strong,
  .sync-alert span {
    margin: 0;
  }

  .notice p,
  .sync-alert span {
    overflow-wrap: anywhere;
    font-size: 0.82rem;
    line-height: 1.35;
  }

  .approval-list,
  .queue-groups {
    display: grid;
    gap: 0.45rem;
  }

  .approval-list h2,
  .queue-groups h2 {
    margin: 0;
    color: var(--text, #374151);
    font-size: 0.84rem;
  }

  .approval-list article {
    align-items: flex-start;
    gap: 0.6rem;
    padding: 0.7rem;
    border: 1px solid #fde68a;
    border-radius: 0.75rem;
    background: #fffbeb;
  }

  .approval-list strong {
    color: #713f12;
    font-size: 0.88rem;
  }

  .approval-list small {
    margin-left: 0.35rem;
    color: #b45309;
    font-size: 0.72rem;
  }

  .approval-list p {
    margin-top: 0.2rem;
    color: #92400e;
    font-size: 0.78rem;
    line-height: 1.35;
  }

  .mini-actions,
  .secondary-actions {
    flex-wrap: wrap;
    gap: 0.45rem;
    margin-top: 0.55rem;
  }

  .queue-groups button {
    display: grid;
    gap: 0.15rem;
    width: 100%;
    min-height: 3rem;
    padding: 0.55rem 0.65rem;
    text-align: left;
  }

  .queue-groups strong {
    color: var(--text-strong, #111827);
    font-size: 0.84rem;
  }

  .queue-groups span {
    color: var(--muted, #64748b);
    font-size: 0.72rem;
    line-height: 1.25;
  }

  .target-create {
    border: 1px solid var(--border-soft, #dbe7f5);
    border-radius: 0.8rem;
    background: var(--surface-muted, #f8fbff);
  }

  .target-create summary {
    display: flex;
    align-items: center;
    list-style: none;
    border: 0;
    border-radius: 0.8rem;
    background: transparent;
  }

  .target-create summary::-webkit-details-marker {
    display: none;
  }

  .target-create-body {
    display: grid;
    gap: 0.55rem;
    padding: 0 0.75rem 0.75rem;
  }

  .target-create-body header {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: 0.7rem;
  }

  .target-create-body header p,
  .target-create-body header h2,
  .target-create-body header span {
    margin: 0;
  }

  .target-create-body header p,
  .target-create-body header span {
    color: var(--muted, #64748b);
    font-size: 0.7rem;
    font-weight: 800;
    letter-spacing: 0.08em;
    text-transform: uppercase;
  }

  .target-create-body header h2 {
    color: var(--text-strong, #111827);
    font-size: 0.95rem;
  }

  .target-create form {
    display: grid;
    gap: 0.5rem;
  }

  .target-create button[type='submit'],
  .primary-action {
    border-color: var(--accent, #2563eb);
    color: #ffffff;
    background: var(--accent, #2563eb);
  }

  .context-confirm {
    display: grid;
    grid-template-columns: auto minmax(0, 1fr);
    align-items: start;
    gap: 0.5rem;
    padding: 0.55rem;
    border: 1px solid #f59e0b;
    border-radius: 0.55rem;
    color: #713f12;
    background: #fffbeb;
    font-size: 0.78rem;
    line-height: 1.35;
  }

  .context-confirm input {
    width: 1rem;
    min-height: 1rem;
    margin: 0.1rem 0 0;
    accent-color: #b45309;
  }

  footer {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .workbench {
    min-width: 0;
    overflow-y: auto;
    background: var(--bg, #eef2f7);
  }

  .task-record {
    min-width: 0;
    padding-bottom: 1.25rem;
  }

  .record-header {
    gap: 0.75rem;
    min-width: 0;
    padding: 1.1rem 1.25rem 0.8rem;
    border-bottom: 1px solid var(--border-soft, #dde4ef);
    background: var(--surface, #ffffff);
  }

  .record-header > div {
    flex: 1 1 auto;
    min-width: 0;
  }

  .record-header h2 {
    overflow-wrap: anywhere;
  }

  .back-to-queue {
    display: none;
  }

  .action-panel,
  .record-summary,
  .workspace-path,
  .execution-context,
  .diff-review,
  .task-result,
  .task-plan,
  .task-input,
  .state-machine,
  .worker-runs,
  .activity,
  .empty-record,
  .action-form,
  .secondary-actions {
    margin: 1rem 1.25rem 0;
    border: 1px solid var(--border-soft, #e2e8f0);
    border-radius: 0.85rem;
    background: var(--surface, #ffffff);
  }

  .action-panel {
    justify-content: space-between;
    gap: 0.75rem;
    padding: 0.85rem;
  }

  .action-panel > div {
    display: flex;
    align-items: flex-start;
    gap: 0.7rem;
    min-width: 0;
  }

  .action-panel h3 {
    color: var(--text-strong, #111827);
    font-size: 0.95rem;
  }

  .action-panel p {
    margin-top: 0.15rem;
    color: var(--text, #475569);
    font-size: 0.84rem;
    line-height: 1.35;
  }

  .action-panel.warning {
    border-color: #fde68a;
    background: #fffbeb;
  }

  .action-panel.danger {
    border-color: #fecaca;
    background: #fff7f7;
  }

  .primary-action {
    flex: 0 0 auto;
    min-width: 8rem;
  }

  .action-form {
    display: grid;
    gap: 0.75rem;
    padding: 0.85rem;
  }

  .action-form label {
    display: grid;
    gap: 0.35rem;
  }

  .secondary-actions {
    padding: 0.75rem;
  }

  .secondary-actions button.danger {
    border-color: #fecaca;
    color: #991b1b;
    background: #fff7f7;
  }

  .record-summary {
    display: grid;
    grid-template-columns: repeat(6, minmax(7rem, 1fr));
    gap: 0.65rem;
    padding: 0.85rem;
  }

  .record-summary div {
    min-width: 0;
    padding: 0.65rem;
    border: 1px solid var(--border-soft, #e2e8f0);
    border-radius: 0.65rem;
    background: var(--surface-muted, #f8fafc);
  }

  .record-summary strong {
    display: block;
    margin-top: 0.2rem;
    overflow: hidden;
    color: var(--text-strong, #111827);
    font-size: 0.88rem;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .state-machine {
    display: grid;
    gap: 0.35rem;
    padding: 0.85rem;
  }

  .state-machine div {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 0.75rem;
  }

  .state-machine span,
  .state-machine small {
    color: var(--muted, #64748b);
    font-size: 0.72rem;
    font-weight: 800;
    letter-spacing: 0.06em;
    text-transform: uppercase;
  }

  .state-machine strong {
    color: var(--text-strong, #111827);
    font-size: 0.9rem;
  }

  .state-machine p {
    margin: 0;
    color: var(--text, #334155);
    font-size: 0.88rem;
    line-height: 1.4;
  }

  .workspace-path,
  .task-result,
  .task-input {
    padding: 0.85rem;
  }

  .workspace-path code {
    display: block;
    margin-top: 0.35rem;
    overflow-wrap: anywhere;
    color: var(--text, #334155);
    font-size: 0.82rem;
  }

  .execution-context {
    display: grid;
    gap: 0.25rem;
    padding: 0.85rem;
    border-color: #f59e0b;
    background: #fffbeb;
  }

  .execution-context span {
    color: #92400e;
    font-size: 0.72rem;
    font-weight: 900;
    letter-spacing: 0.08em;
    text-transform: uppercase;
  }

  .execution-context code {
    overflow-wrap: anywhere;
  }

  .task-result {
    max-height: 13rem;
    overflow-y: auto;
  }

  .task-result h3,
  .task-result p,
  .task-input h3,
  .task-input p {
    margin: 0;
  }

  .task-result h3,
  .task-input h3 {
    color: var(--text-strong, #111827);
    font-size: 0.92rem;
  }

  .task-result p,
  .task-input p {
    margin-top: 0.4rem;
    color: var(--text, #475569);
    font-size: 0.88rem;
    line-height: 1.45;
    overflow-wrap: anywhere;
    white-space: pre-wrap;
  }

  .diff-review {
    overflow: hidden;
    --diff-bg: var(--surface, #ffffff);
    --diff-bg-muted: var(--surface-muted, #f8fafc);
    --diff-bg-subtle: #fbfdff;
    --diff-bg-hover: var(--surface-hover, #eef5ff);
    --diff-border: var(--border-soft, #e2e8f0);
    --diff-border-strong: var(--border, #cbd5e1);
    --diff-text: var(--text, #243047);
    --diff-text-strong: var(--text-strong, #111827);
    --diff-muted: var(--muted, #64748b);
    --diff-selected-bg: #eff6ff;
    --diff-selected-border: #93c5fd;
    --diff-accent-text: #1d4ed8;
    --diff-add-bg: #ecfdf3;
    --diff-add-gutter-bg: #dcfce7;
    --diff-add-text: #166534;
    --diff-delete-bg: #fff1f2;
    --diff-delete-gutter-bg: #fee2e2;
    --diff-delete-text: #991b1b;
    --diff-hunk-bg: #eff6ff;
    --diff-hunk-text: #1e40af;
    --diff-changed-bg: rgb(250 204 21 / 0.38);
    border-color: var(--diff-border);
    color: var(--diff-text);
    background: var(--diff-bg);
  }

  .diff-review > header,
  .diff-file > header {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: 0.75rem;
    padding: 0.85rem;
    border-bottom: 1px solid var(--diff-border);
  }

  .diff-review h3,
  .diff-file strong,
  .diff-file small,
  .diff-file header span {
    margin: 0;
  }

  .diff-review h3 {
    margin-top: 0.1rem;
    color: var(--diff-text-strong);
    font-size: 0.95rem;
  }

  .diff-review h3 span,
  .diff-file small,
  .diff-file header span,
  .diff-meta {
    color: var(--diff-muted);
    font-size: 0.76rem;
    font-weight: 750;
  }

  .diff-controls {
    display: flex;
    flex-wrap: wrap;
    justify-content: flex-end;
    gap: 0.45rem;
  }

  .diff-controls button,
  .diff-file-list button {
    border-color: var(--diff-border-strong);
    color: var(--diff-text);
    background: var(--diff-bg);
  }

  .diff-controls button.active {
    border-color: var(--diff-selected-border);
    color: var(--diff-accent-text);
    background: var(--diff-selected-bg);
  }

  .diff-meta {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 0.75rem;
    padding: 0.65rem 0.85rem;
    border-bottom: 1px solid var(--diff-border);
    background: var(--diff-bg-muted);
  }

  .diff-meta code {
    font-family:
      "SFMono-Regular", Consolas, "Liberation Mono", Menlo, monospace;
    font-size: 0.72rem;
  }

  .diff-layout {
    display: grid;
    grid-template-columns: minmax(11rem, 16rem) minmax(0, 1fr);
    min-height: 0;
  }

  .diff-file-list {
    display: grid;
    align-content: start;
    gap: 0.35rem;
    max-height: 32rem;
    overflow: auto;
    padding: 0.75rem;
    border-right: 1px solid var(--diff-border);
    background: var(--diff-bg-subtle);
  }

  .diff-file-list button {
    display: grid;
    grid-template-rows: auto auto;
    align-content: center;
    gap: 0.16rem;
    width: 100%;
    min-width: 0;
    min-height: 3.15rem;
    padding: 0.55rem 0.6rem;
    border-color: transparent;
    text-align: left;
  }

  .diff-file-list button.selected {
    border-color: var(--diff-selected-border);
    background: var(--diff-selected-bg);
  }

  .diff-file-list span,
  .diff-file strong {
    display: block;
    min-width: 0;
    overflow: hidden;
    color: var(--diff-text-strong);
    line-height: 1.25;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .diff-file-list small {
    display: block;
    min-width: 0;
    overflow: hidden;
    color: var(--diff-muted);
    font-size: 0.7rem;
    line-height: 1.3;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .diff-file {
    min-width: 0;
    overflow: hidden;
  }

  .diff-file > header {
    align-items: center;
    padding: 0.7rem 0.85rem;
  }

  .diff-file > header > div {
    display: grid;
    min-width: 0;
  }

  .diff-scroll {
    max-height: 34rem;
    overflow: auto;
    background: var(--diff-bg);
  }

  .split-diff,
  .unified-diff {
    width: 100%;
    min-width: 0;
    color: var(--diff-text);
    font-family:
      "SFMono-Regular", Consolas, "Liberation Mono", Menlo, monospace;
    font-size: 0.76rem;
    line-height: 1.45;
  }

  .split-row,
  .diff-row {
    display: grid;
    min-height: 1.55rem;
    border-top: 1px solid var(--diff-border);
  }

  .split-row {
    grid-template-columns: 3.4rem minmax(0, 1fr) 3.4rem minmax(0, 1fr);
  }

  .diff-row {
    grid-template-columns: 3.4rem 3.4rem minmax(0, 1fr);
  }

  .split-row.full {
    grid-template-columns: minmax(0, 1fr);
  }

  .split-row.full code {
    padding-left: 0.8rem;
  }

  .line-no,
  .split-row code,
  .diff-row code {
    min-width: 0;
    padding: 0.18rem 0.55rem;
  }

  .line-no {
    color: var(--diff-muted);
    background: var(--diff-bg-muted);
    text-align: right;
    user-select: none;
    white-space: nowrap;
  }

  .split-row code,
  .diff-row code {
    overflow: visible;
    overflow-wrap: anywhere;
    white-space: pre-wrap;
  }

  .split-row code.blank {
    background: var(--diff-bg-muted);
  }

  .marker {
    display: inline-block;
    width: 1.1rem;
    color: var(--diff-muted);
    user-select: none;
  }

  .split-row.add code:last-child,
  .split-row.change code:last-child,
  .diff-row.add code {
    background: var(--diff-add-bg);
  }

  .split-row.delete code:nth-child(2),
  .split-row.change code:nth-child(2),
  .diff-row.delete code {
    background: var(--diff-delete-bg);
  }

  .diff-row.add .line-no.new,
  .split-row.add .line-no.new,
  .split-row.change .line-no.new {
    color: var(--diff-add-text);
    background: var(--diff-add-gutter-bg);
  }

  .diff-row.delete .line-no.old,
  .split-row.delete .line-no.old,
  .split-row.change .line-no.old {
    color: var(--diff-delete-text);
    background: var(--diff-delete-gutter-bg);
  }

  .split-row.hunk,
  .diff-row.hunk {
    color: var(--diff-hunk-text);
    background: var(--diff-hunk-bg);
    font-weight: 800;
  }

  .split-row.meta,
  .diff-row.meta {
    color: var(--diff-muted);
    background: var(--diff-bg-muted);
  }

  .changed {
    border-radius: 0.2rem;
    background: var(--diff-changed-bg);
  }

  :global(html[data-theme='dark'] .diff-review) {
    --diff-bg: #0b1120;
    --diff-bg-muted: #111827;
    --diff-bg-subtle: #101827;
    --diff-bg-hover: #243047;
    --diff-border: #263244;
    --diff-border-strong: #334155;
    --diff-text: #dbe7f6;
    --diff-text-strong: #f8fafc;
    --diff-muted: #9fb0c7;
    --diff-selected-bg: #10254a;
    --diff-selected-border: #1d4ed8;
    --diff-accent-text: #bfdbfe;
    --diff-add-bg: #0f2f22;
    --diff-add-gutter-bg: #164e32;
    --diff-add-text: #bbf7d0;
    --diff-delete-bg: #3a1418;
    --diff-delete-gutter-bg: #7f1d1d;
    --diff-delete-text: #fecaca;
    --diff-hunk-bg: #10254a;
    --diff-hunk-text: #bfdbfe;
    --diff-changed-bg: rgb(202 138 4 / 0.42);
  }

  .worker-runs,
  .activity,
  .task-plan {
    overflow: hidden;
  }

  .worker-runs > header,
  .activity header,
  .task-plan header {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: 0.75rem;
    padding: 0.85rem;
    border-bottom: 1px solid var(--border-soft, #e2e8f0);
  }

  .worker-runs h3,
  .activity h3,
  .task-plan h3 {
    margin: 0.1rem 0 0;
    color: var(--text-strong, #111827);
    font-size: 0.95rem;
    line-height: 1.35;
  }

  .run-list {
    display: grid;
    gap: 0.75rem;
    padding: 0.85rem;
  }

  .worker-run {
    overflow: hidden;
    border: 1px solid var(--border-soft, #e2e8f0);
    border-radius: 0.75rem;
    background: var(--surface-muted, #f8fafc);
  }

  .worker-run.blue {
    border-color: #bfdbfe;
    background: #eff6ff;
  }

  .worker-run.green {
    border-color: #bbf7d0;
    background: #f0fdf4;
  }

  .worker-run.amber {
    border-color: #fde68a;
    background: #fffbeb;
  }

  .worker-run.red {
    border-color: #fecaca;
    background: #fff7f7;
  }

  .worker-run > header {
    display: flex;
    align-items: center;
    gap: 0.75rem;
    padding: 0.75rem 0.8rem 0;
  }

  .worker-run strong,
  .worker-run small {
    display: block;
  }

  .worker-run strong {
    color: var(--text-strong, #111827);
    font-size: 0.88rem;
  }

  .worker-run small,
  .run-command,
  .run-artifact-path {
    color: var(--muted, #64748b);
    font-size: 0.76rem;
  }

  .artifact-pill {
    margin-left: auto;
    border: 1px solid var(--border, #cbd5e1);
    border-radius: 999px;
    padding: 0.15rem 0.45rem;
    color: var(--text, #475569);
    background: var(--surface, #ffffff);
    font-size: 0.68rem;
    font-weight: 850;
  }

  .run-command,
  .run-artifact-path,
  .run-error {
    margin: 0;
    padding: 0.45rem 0.8rem 0;
    overflow-wrap: anywhere;
  }

  .run-artifact-path {
    font-family:
      "SFMono-Regular", Consolas, "Liberation Mono", Menlo, monospace;
  }

  .run-error {
    color: #991b1b;
    font-size: 0.8rem;
    font-weight: 750;
  }

  .worker-run pre {
    max-height: 15rem;
    margin: 0.65rem 0 0;
    overflow: auto;
    padding: 0.8rem;
    border-top: 1px solid rgb(148 163 184 / 0.28);
    color: #0f172a;
    background: rgb(255 255 255 / 0.72);
    font-family:
      "SFMono-Regular", Consolas, "Liberation Mono", Menlo, monospace;
    font-size: 0.78rem;
    line-height: 1.45;
    white-space: pre-wrap;
  }

  .empty.compact {
    padding: 0.7rem 0.8rem 0.8rem;
    font-size: 0.8rem;
    text-align: left;
  }

  .activity ol,
  .task-plan ol,
  .task-plan ul {
    margin: 0;
  }

  .activity ol {
    display: grid;
    padding: 0;
    list-style: none;
  }

  .activity li {
    display: grid;
    grid-template-columns: 4.5rem minmax(0, 1fr);
    gap: 0.85rem;
    padding: 0.8rem;
    border-top: 1px solid var(--border-soft, #f1f5f9);
  }

  .activity time {
    color: var(--muted, #64748b);
    font-size: 0.76rem;
  }

  .activity li strong,
  .activity li span {
    display: block;
  }

  .activity li strong {
    color: var(--text-strong, #172033);
    font-size: 0.86rem;
  }

  .activity li span {
    margin-top: 0.1rem;
    color: var(--muted, #64748b);
    font-size: 0.74rem;
  }

  .activity li p {
    margin: 0.35rem 0 0;
    color: var(--text, #334155);
    font-size: 0.82rem;
    line-height: 1.4;
    overflow-wrap: anywhere;
  }

  .task-plan header span {
    flex: 0 0 auto;
    border-radius: 999px;
    background: #dbeafe;
    color: #1d4ed8;
    padding: 0.22rem 0.5rem;
    font-size: 0.72rem;
    font-weight: 800;
  }

  .task-plan ol {
    display: grid;
    gap: 0.75rem;
    padding: 0.85rem 0.85rem 0.85rem 2rem;
  }

  .task-plan li {
    color: var(--text, #475569);
    font-size: 0.88rem;
    line-height: 1.45;
    overflow-wrap: anywhere;
  }

  .task-plan li strong {
    display: block;
    color: var(--text-strong, #111827);
  }

  .plan-risks,
  .plan-review {
    border-top: 1px solid var(--border-soft, #e2e8f0);
    padding: 0.85rem;
  }

  .plan-risks ul {
    display: grid;
    gap: 0.35rem;
    margin-top: 0.45rem;
    padding-left: 1rem;
  }

  .plan-review {
    margin: 0;
    color: var(--text, #475569);
    font-size: 0.86rem;
    line-height: 1.45;
    overflow-wrap: anywhere;
  }

  .empty-record {
    align-items: flex-start;
    gap: 0.75rem;
    padding: 1rem;
  }

  .empty-record h2,
  .empty-record p {
    margin: 0;
  }

  .empty-record h2 {
    color: var(--text-strong, #111827);
    font-size: 1rem;
  }

  .empty-record p {
    margin-top: 0.25rem;
    color: var(--muted, #64748b);
    line-height: 1.4;
  }

  .hidden {
    position: absolute;
    width: 1px;
    height: 1px;
    overflow: hidden;
    clip: rect(0 0 0 0);
    clip-path: inset(50%);
    white-space: nowrap;
  }

  @media (max-width: 1180px) {
    .record-summary {
      grid-template-columns: repeat(3, minmax(0, 1fr));
    }
  }

  @media (max-width: 760px) {
    :global(html),
    :global(body) {
      overflow: auto;
    }

    .tasks-page {
      display: block;
      min-height: 100dvh;
      height: auto;
      padding-top: 3.75rem;
    }

    :global(.navbar) {
      position: fixed !important;
      top: 0 !important;
      right: 0;
      left: 0;
    }

    .mobile-tabs {
      position: sticky;
      top: 3.75rem;
      z-index: 10;
      display: grid;
      grid-template-columns: 1fr 1fr;
      gap: 0.5rem;
      padding: 0.6rem 0.75rem;
      border-bottom: 1px solid var(--border-soft, #dbe3ef);
      background: var(--surface, #ffffff);
    }

    .mobile-tabs button {
      min-height: 2.7rem;
      border: 1px solid var(--border, #cbd5e1);
      border-radius: 0.65rem;
      color: var(--text, #243047);
      background: var(--surface-muted, #f8fafc);
      font-weight: 850;
    }

    .mobile-tabs button.active {
      border-color: var(--accent, #2563eb);
      color: #ffffff;
      background: var(--accent, #2563eb);
    }

    .mobile-tabs span {
      margin-left: 0.25rem;
      font-size: 0.78rem;
    }

    .shell {
      display: block;
      overflow: visible;
    }

    .task-pane[data-mobile-hidden='true'],
    .workbench[data-mobile-hidden='true'] {
      display: none;
    }

    .task-pane,
    .workbench {
      overflow: visible;
    }

    .task-pane {
      display: grid;
      grid-template-rows: auto auto auto minmax(18rem, auto) auto auto auto auto;
      padding: 0.75rem;
      border-right: 0;
    }

    .task-header {
      align-items: flex-start;
    }

    .task-header h1 {
      font-size: 1.1rem;
    }

    .triage {
      gap: 0.35rem;
    }

    .triage button {
      min-height: 3.15rem;
      padding: 0.5rem;
    }

    .triage strong {
      font-size: 1rem;
    }

    .task-list {
      overflow: visible;
      padding-right: 0;
    }

    .task-row {
      align-items: flex-start;
      min-height: 4.5rem;
      padding: 0.65rem;
    }

    .task-copy strong {
      display: -webkit-box;
      -webkit-box-orient: vertical;
      -webkit-line-clamp: 2;
      line-clamp: 2;
    }

    .task-copy small {
      flex-wrap: wrap;
      gap: 0.25rem;
    }

    .workbench {
      background: var(--bg, #eef2f7);
    }

    .notice,
    .action-panel,
    .record-summary,
    .workspace-path,
    .execution-context,
    .diff-review,
    .task-result,
    .task-plan,
    .task-input,
    .state-machine,
    .worker-runs,
    .activity,
    .empty-record,
    .action-form,
    .secondary-actions {
      margin: 0.75rem;
    }

    .record-header {
      padding: 0.85rem 0.75rem;
    }

    .back-to-queue {
      display: inline-flex;
      align-items: center;
    }

    .record-header h2 {
      font-size: 1.05rem;
      white-space: normal;
    }

    .action-panel {
      align-items: stretch;
      flex-direction: column;
    }

    .primary-action {
      width: 100%;
      min-height: 2.9rem;
    }

    .record-summary {
      grid-template-columns: repeat(2, minmax(0, 1fr));
    }

    .diff-review > header,
    .diff-meta,
    .diff-file > header {
      flex-direction: column;
      align-items: stretch;
    }

    .diff-controls {
      justify-content: flex-start;
    }

    .diff-layout {
      grid-template-columns: minmax(0, 1fr);
    }

    .diff-file-list {
      display: flex;
      overflow-x: auto;
      border-right: 0;
      border-bottom: 1px solid var(--diff-border);
    }

    .diff-file-list button {
      flex: 0 0 min(16rem, 78vw);
    }

    .diff-scroll {
      max-height: 28rem;
    }

    .split-row {
      grid-template-columns: 3rem minmax(0, 1fr);
    }

    .diff-row {
      grid-template-columns: 3rem 3rem minmax(0, 1fr);
    }

    .activity li {
      grid-template-columns: 1fr;
      gap: 0.25rem;
    }
  }
</style>
