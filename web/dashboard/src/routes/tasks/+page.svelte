<script lang="ts">
  import { onMount, tick } from 'svelte';
  import {
    createHomelabdClient,
    Navbar,
    taskInputText,
    taskRuntimeMs,
    taskIsActive,
    taskIsTerminal,
    taskStateDescription,
    taskStateTransitions,
    taskStartedAt,
    taskSummaryTitle,
    type ChatRole,
    type ChatTranscriptMessage,
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
    selectTaskForQueue,
    type TaskFilter,
    type TaskQueueFilter,
    type TaskQueueView,
    type WorkerTraceRun
  } from './view-model';

  type PromptAction = {
    label: string;
    command: string;
    hint: string;
  };
  type TaskAction = {
    label: string;
    command: string;
    reason: string;
  };
  type DiffMode = 'split' | 'unified';

  const apiBase = import.meta.env.VITE_HOMELABD_API_BASE || '/api';
  const client = createHomelabdClient({ baseUrl: apiBase });
  const transcriptStorageKey = 'homelabd.dashboard.chatTranscript.v3';
  const taskRefPattern = /^(?:[a-f0-9]{6,12}|task_\d{8}_\d{6}_[a-f0-9]{8})$/i;
  const approvalRefPattern = /^approval_\d{8}_\d{6}_[a-f0-9]{8}$/i;
  const promptActions: PromptAction[] = [
    {
      label: 'Brief me',
      command: 'brief me on what needs my attention',
      hint: 'Triage the queue'
    },
    {
      label: 'Find blockers',
      command: 'what is blocked or waiting on me?',
      hint: 'Surface decisions'
    },
    {
      label: 'Improve loop',
      command: 'reflect on our recent interaction and propose one improvement',
      hint: 'Self-improve'
    }
  ];
  const welcomeMessage: ChatTranscriptMessage = {
    id: 'welcome',
    role: 'assistant',
    content: 'Ready. Pick a task on the left, or give me a new instruction.',
    source: 'program',
    actions: ['status', 'tasks'],
    time: 'Now'
  };

  let draft = '';
  let newTaskDraft = '';
  let loading = false;
  let creatingTask = false;
  let contextAcknowledged = false;
  let refreshing = false;
  let error = '';
  let diffError = '';
  let taskActionLoading = '';
  let diffLoadingTaskId = '';
  let messageId = 0;
  let taskFilter: TaskFilter = 'attention';
  let queueFilter: TaskQueueFilter = 'all';
  let taskSearch = '';
  let selectedTaskId = '';
  let selectedDiffFilePath = '';
  let loadedDiffTaskId = '';
  let diffMode: DiffMode = 'split';
  let lastRefresh = '';
  let taskQueueOpen = true;
  let commandPanelOpen = true;
  let inputEl: HTMLTextAreaElement | undefined;
  let messagesEl: HTMLElement | undefined;
  let messages: ChatTranscriptMessage[] = [welcomeMessage];
  let tasks: HomelabdTask[] = [];
  let agents: HomelabdRemoteAgent[] = [];
  let approvals: HomelabdApproval[] = [];
  let events: HomelabdEvent[] = [];
  let taskRuns: Record<string, HomelabdRunArtifact[]> = {};
  let taskDiffs: Record<string, HomelabdTaskDiffResponse> = {};
  let refreshStatePromise: Promise<void> | undefined;
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
  let selectedAgentId = '';
  let selectedWorkdirId = '';
  let onlineAgentItems: HomelabdRemoteAgent[] = [];
  let selectedAgent: HomelabdRemoteAgent | undefined;
  let selectedWorkdirs: HomelabdRemoteAgentWorkdir[] = [];
  let selectedWorkdir: HomelabdRemoteAgentWorkdir | undefined;
  let queueOptions: { id: TaskQueueFilter; label: string; count: number; detail: string }[] = [];
  let selectedContextLabel = 'Local homelabd workspace';

  const timeLabel = () =>
    new Date().toLocaleTimeString([], {
      hour: '2-digit',
      minute: '2-digit'
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
    return `${machine} · ${dir}`;
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
    return normalized.length > max ? `${normalized.slice(0, max)}…` : normalized;
  };

  const isTranscriptMessage = (value: unknown): value is ChatTranscriptMessage => {
    if (!value || typeof value !== 'object') {
      return false;
    }
    const candidate = value as Record<string, unknown>;
    const validRole = candidate.role === 'user' || candidate.role === 'assistant';
    const validSource = candidate.source === undefined || typeof candidate.source === 'string';
    const validActions =
      candidate.actions === undefined ||
      (Array.isArray(candidate.actions) &&
        candidate.actions.every((action) => typeof action === 'string'));

    return (
      typeof candidate.id === 'string' &&
      validRole &&
      typeof candidate.content === 'string' &&
      typeof candidate.time === 'string' &&
      validSource &&
      validActions
    );
  };

  const loadStoredMessages = () => {
    try {
      const stored = localStorage.getItem(transcriptStorageKey);
      if (!stored) {
        return [welcomeMessage];
      }
      const parsed = JSON.parse(stored);
      if (!Array.isArray(parsed)) {
        return [welcomeMessage];
      }
      const restored = parsed.filter(isTranscriptMessage);
      return restored.length > 0 ? restored : [welcomeMessage];
    } catch {
      return [welcomeMessage];
    }
  };

  const persistMessages = () => {
    try {
      localStorage.setItem(transcriptStorageKey, JSON.stringify(messages.slice(-100)));
    } catch {
      error = 'Chat history could not be persisted locally.';
    }
  };

  const sourceLabel = (source = 'program') => {
    switch (source.toLowerCase()) {
      case 'gemini':
        return 'Gemini';
      case 'openai':
        return 'OpenAI';
      default:
        return source;
    }
  };

  const isSafeCommand = (command: string) => {
    if (!command || command.includes('<') || command.endsWith(':') || command.length > 260) {
      return false;
    }

    const parts = command.split(/\s+/);
    const verb = parts[0]?.toLowerCase() || '';

    if (parts.length === 1) {
      return ['help', 'tasks', 'status', 'agents', 'approvals'].includes(verb);
    }

    if (['new', 'task'].includes(verb)) {
      return command.length > verb.length + 1;
    }

    if (
      [
        'show',
        'run',
        'ux',
        'work',
        'start',
        'review',
        'diff',
        'test',
        'cancel',
        'stop',
        'delete',
        'remove',
        'rm',
        'accept',
        'verify'
      ].includes(verb)
    ) {
      return parts.length === 2 && taskRefPattern.test(parts[1]);
    }

    if (verb === 'reopen') {
      return parts.length >= 2 && taskRefPattern.test(parts[1]);
    }

    if (['approve', 'deny'].includes(verb)) {
      return parts.length === 2 && approvalRefPattern.test(parts[1]);
    }

    if (verb === 'delegate') {
      return (
        parts.length >= 4 &&
        taskRefPattern.test(parts[1]) &&
        parts[2]?.toLowerCase() === 'to' &&
        ['codex', 'claude', 'gemini', 'ux'].includes(parts[3]?.toLowerCase())
      );
    }

    return false;
  };

  const extractCommands = (content: string): string[] => {
    const commands = new Set<string>();
    for (const match of content.matchAll(/`([^`\n]+)`/g)) {
      const command = match[1].trim().replace(/\s+/g, ' ');
      if (isSafeCommand(command)) {
        commands.add(command);
      }
    }
    for (const line of content.split('\n')) {
      const command = line
        .trim()
        .replace(/^[>-]\s*/, '')
        .replace(/[.。]$/, '')
        .replace(/\s+/g, ' ');
      if (isSafeCommand(command)) {
        commands.add(command);
      }
    }
    return [...commands].slice(0, 5);
  };

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
  $: if (currentTask?.id && !isRemoteTask(currentTask) && currentTask.id !== loadedDiffTaskId) {
    loadedDiffTaskId = currentTask.id;
    selectedDiffFilePath = '';
    void refreshTaskDiff(currentTask.id);
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
    { id: 'all', label: 'All queues', count: tasks.length, detail: 'Every local and remote target' },
    {
      id: 'local',
      label: 'Local homelabd',
      count: tasks.filter((task) => task.target?.mode !== 'remote').length,
      detail: 'Runs in homelabd workspaces'
    },
    ...agents.map((agent) => ({
      id: `agent:${agent.id}` as TaskQueueFilter,
      label: agent.name || agent.id,
      count: tasks.filter((task) => task.target?.mode === 'remote' && task.target?.agent_id === agent.id)
        .length,
      detail: `${agent.machine || 'unknown machine'} · ${agent.status}`
    }))
  ];
  $: selectedContextLabel =
    selectedAgent && selectedWorkdir
      ? `${selectedAgent.name || selectedAgent.id} on ${selectedAgent.machine || 'unknown machine'} in ${selectedWorkdir.path}`
      : 'Local homelabd workspace';

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
    file.oldPath && file.oldPath !== file.path ? `${file.oldPath} → ${file.path}` : file.path;

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

  const refreshTaskDiff = async (taskId: string) => {
    if (!taskId) {
      return;
    }
    diffLoadingTaskId = taskId;
    diffError = '';
    try {
      const result = await client.getTaskDiff(taskId);
      taskDiffs = { ...taskDiffs, [taskId]: result };
    } catch (err) {
      diffError = err instanceof Error ? err.message : 'Unable to load task diff.';
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
      const result = await client.listTaskRuns(taskId);
      taskRuns = { ...taskRuns, [taskId]: result.runs };
    } catch (err) {
      error = err instanceof Error ? err.message : 'Unable to load worker runs.';
    }
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

  const taskPrimaryAction = (task?: HomelabdTask): TaskAction => {
    if (!task) {
      return {
        label: 'Brief me',
        command: 'brief me on what needs my attention',
        reason: 'No task is selected, so the safest next step is triage.'
      };
    }
    const id = shortID(task.id);
    switch (task.status) {
      case 'ready_for_review':
        return {
          label: 'Run review gate',
          command: `review ${id}`,
          reason: 'Runs checks and premerge. It moves to awaiting approval if clean, or blocked if not. It will not restart a worker automatically.'
        };
      case 'awaiting_verification':
        return {
          label: 'Accept result',
          command: `accept ${id}`,
          reason: 'The change is merged; human verification closes the task if the behavior is correct.'
        };
      case 'blocked':
      case 'failed':
        return {
          label: 'Delegate fix',
          command: `delegate ${id} to codex fix or finish this task`,
          reason: 'This task cannot complete cleanly without rework from a stronger coding agent.'
        };
      case 'conflict_resolution':
        return {
          label: 'Resolve conflict',
          command: `delegate ${id} to codex resolve the main-branch conflict`,
          reason: 'The task branch did not reconcile with current main and needs manual conflict resolution.'
        };
      case 'awaiting_approval':
        return {
          label: 'Review approval',
          command: 'approvals',
          reason: 'The runtime is waiting for permission before it can perform a gated action.'
        };
      case 'running':
        return {
          label: 'Show progress',
          command: `show ${id}`,
          reason: 'A worker currently owns this task. Do not restart it unless it is stale or clearly blocked.'
        };
      case 'queued':
        return {
          label: 'Show queue state',
          command: `show ${id}`,
          reason: 'The task is waiting for the supervisor to assign a worker.'
        };
      default:
        if (taskIsTerminal(task)) {
          return {
            label: 'Show details',
            command: `show ${id}`,
            reason: 'No action is required unless the result looks wrong.'
          };
        }
        return {
          label: 'Continue work',
          command: `run ${id}`,
          reason: 'Only use this if no worker is currently running. Active external workers update the task when they finish.'
        };
    }
  };

  const secondaryTaskActions = (task?: HomelabdTask) => {
    if (!task) {
      return ['status'];
    }
    const id = shortID(task.id);
    const primary = taskPrimaryAction(task).command;
    const actions = new Set<string>([`show ${id}`]);
    if (!taskIsTerminal(task)) {
      actions.add(`ux ${id}`);
      actions.add(`delegate ${id} to codex`);
      actions.add(`delete ${id}`);
    }
    if (task.status === 'awaiting_verification') {
      actions.add(`reopen ${id} needs rework`);
    }
    return [...actions].filter((action) => action && action !== primary).slice(0, 4);
  };

  const focusInput = () => {
    requestAnimationFrame(() => inputEl?.focus());
  };

  const scrollMessages = () => {
    void tick().then(() => {
      if (messagesEl) {
        messagesEl.scrollTop = messagesEl.scrollHeight;
      }
    });
  };

  const addMessage = (role: ChatRole, content: string, source?: string) => {
    messageId += 1;
    messages = [
      ...messages,
      {
        id: `${role}-${messageId}`,
        role,
        content,
        source,
        actions: role === 'assistant' ? extractCommands(content) : undefined,
        time: timeLabel()
      }
    ];
    persistMessages();
    scrollMessages();
  };

  const refreshState = () => {
    if (refreshStatePromise) {
      return refreshStatePromise;
    }

    refreshing = true;
    refreshStatePromise = (async () => {
      const [taskResult, approvalResult, eventResult, agentResult] = await Promise.allSettled([
        client.listTasks(),
        client.listApprovals(),
        client.listEvents({ limit: 500 }),
        client.listAgents()
      ]);

      if (taskResult.status === 'fulfilled') {
        tasks = [...taskResult.value.tasks].sort(
          (left, right) => Date.parse(right.updated_at) - Date.parse(left.updated_at)
        );
      } else {
        error = taskResult.reason instanceof Error ? taskResult.reason.message : 'Unable to load tasks.';
      }

      if (approvalResult.status === 'fulfilled') {
        approvals = [...approvalResult.value.approvals].sort(
          (left, right) => Date.parse(right.updated_at) - Date.parse(left.updated_at)
        );
      } else {
        error =
          approvalResult.reason instanceof Error
            ? approvalResult.reason.message
            : 'Unable to load approvals.';
      }

      if (eventResult.status === 'fulfilled') {
        events = eventResult.value.events;
      }

      if (agentResult.status === 'fulfilled') {
        agents = agentResult.value.agents;
      }

      lastRefresh = timeLabel();
      if (selectedTaskId && !tasks.some((task) => task.id === selectedTaskId)) {
        selectedTaskId = '';
      }
      if (selectedTaskId) {
        await refreshTaskRuns(selectedTaskId);
        const selected = tasks.find((task) => task.id === selectedTaskId);
        if (!isRemoteTask(selected)) {
          await refreshTaskDiff(selectedTaskId);
        }
      }
    })().finally(() => {
      refreshing = false;
      refreshStatePromise = undefined;
    });

    return refreshStatePromise;
  };

  onMount(() => {
    messages = loadStoredMessages();
    messageId = messages.length;
    scrollMessages();
    void refreshState();
    const interval = window.setInterval(() => {
      void refreshState();
    }, 8000);
    return () => window.clearInterval(interval);
  });

  const sendMessage = async (content = draft) => {
    const trimmed = content.trim();
    if (!trimmed || loading) {
      return;
    }

    draft = '';
    error = '';
    addMessage('user', trimmed);
    loading = true;

    try {
      const response = await client.sendMessage({
        from: 'dashboard',
        content: trimmed
      });
      addMessage('assistant', response.reply || 'No reply returned.', response.source || 'program');
      await refreshState();
    } catch (err) {
      error = err instanceof Error ? err.message : 'Unable to reach homelabd.';
    } finally {
      loading = false;
      if (commandPanelOpen) {
        focusInput();
      }
    }
  };

  const sendCommand = (command: string) => {
    setCommandPanelOpen(true);
    void sendMessage(command);
  };

  const createTargetedTask = async () => {
    const goal = newTaskDraft.trim();
    if (!goal || creatingTask) {
      return;
    }
    creatingTask = true;
    error = '';
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
      addMessage('assistant', response.reply || 'Task created.', 'program');
      await refreshState();
    } catch (err) {
      error = err instanceof Error ? err.message : 'Unable to create task.';
    } finally {
      creatingTask = false;
    }
  };

  const selectTask = (id: string) => {
    selectedTaskId = id;
    loadedDiffTaskId = id;
    selectedDiffFilePath = '';
    void refreshTaskRuns(id);
    const selected = tasks.find((task) => task.id === id);
    if (!isRemoteTask(selected)) {
      void refreshTaskDiff(id);
    }
  };

  const performTaskAction = async (action: 'cancel' | 'retry') => {
    if (!currentTask || taskActionLoading) {
      return;
    }
    taskActionLoading = action;
    error = '';
    const taskId = currentTask.id;
    try {
      const response =
        action === 'cancel'
          ? await client.cancelTask(taskId)
          : await client.retryTask(taskId);
      addMessage('assistant', response.reply || `${action} submitted.`, 'program');
      await refreshState();
      await refreshTaskRuns(taskId);
      if (!isRemoteTask(currentTask)) {
        await refreshTaskDiff(taskId);
      }
    } catch (err) {
      error = err instanceof Error ? err.message : `Unable to ${action} task.`;
    } finally {
      taskActionLoading = '';
    }
  };

  const setTaskFilter = (filter: TaskFilter) => {
    taskFilter = filter;
    selectedTaskId = selectTaskForQueue(
      tasks,
      approvals,
      filter,
      queueFilter,
      taskSearch,
      selectedTaskId
    );
  };

  const setQueueFilter = (filter: TaskQueueFilter) => {
    queueFilter = filter;
    selectedTaskId = selectTaskForQueue(
      tasks,
      approvals,
      taskFilter,
      filter,
      taskSearch,
      selectedTaskId
    );
  };

  const handleAgentChange = () => {
    contextAcknowledged = false;
  };

  const setCommandPanelOpen = (open: boolean) => {
    commandPanelOpen = open;
    if (open) {
      focusInput();
    }
  };

  const toggleCommandPanel = () => {
    setCommandPanelOpen(!commandPanelOpen);
  };

  const handleComposerKeydown = (event: KeyboardEvent) => {
    if (event.key === 'Enter' && !event.shiftKey && !event.isComposing) {
      event.preventDefault();
      void sendMessage();
    }
  };
</script>

<svelte:head>
  <title>homelabd Tasks</title>
  <meta
    name="description"
    content="Task-focused homelabd task interface"
  />
</svelte:head>

<div class="tasks-page">
<Navbar title="Tasks" subtitle="homelabd" current="/tasks" />

<div class="shell">
  <aside class="task-pane" class:collapsed={!taskQueueOpen} aria-label="Tasks">
    <header class="task-header">
      <div>
        <p>Task queue</p>
        <h1>Task queue</h1>
        <span>Synced {lastRefresh || 'never'}</span>
      </div>
      <nav aria-label="Dashboard views">
        <button
          type="button"
          class="queue-toggle"
          aria-controls="task-sidebar-content"
          aria-expanded={taskQueueOpen}
          on:click={() => (taskQueueOpen = !taskQueueOpen)}
        >
          {taskQueueOpen ? 'Hide queue' : `Show queue (${visibleTaskItems.length})`}
        </button>
        <button type="button" disabled={refreshing} on:click={() => void refreshState()}>
          {refreshing ? 'Syncing' : 'Sync'}
        </button>
      </nav>
    </header>

    <div id="task-sidebar-content" class="task-sidebar-content">
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

      <section class="queue-groups" aria-label="Execution queues">
        <h2>Execution queues</h2>
        {#each queueOptions as option}
          <button
            type="button"
            class:active={queueFilter === option.id}
            on:click={() => setQueueFilter(option.id)}
          >
            <strong>{option.label}</strong>
            <span>{option.count} task{option.count === 1 ? '' : 's'} · {option.detail}</span>
          </button>
        {/each}
      </section>

      <label class="hidden" for="task-search">Search tasks</label>
      <input id="task-search" bind:value={taskSearch} placeholder="Search tasks…" />

      <section class="target-create" aria-label="Create targeted task">
        <header>
          <div>
            <p>New task target</p>
            <h2>{selectedAgent ? selectedAgent.name || selectedAgent.id : 'Local homelabd'}</h2>
          </div>
          <span>{onlineAgentItems.length} online</span>
        </header>
        {#if agents.length}
          <label class="hidden" for="agent-select">Remote agent</label>
          <select id="agent-select" bind:value={selectedAgentId} on:change={handleAgentChange}>
            {#each agents as agent}
              <option value={agent.id}>
                {agent.name || agent.id} · {agent.machine || 'unknown'} · {agent.status}
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
              <option value={workdir.id}>{workdirLabel(workdir)} · {workdir.path}</option>
            {/each}
          </select>
        {:else}
          <p class="target-empty">No remote agents have checked in. Creating a task here uses the local graph.</p>
        {/if}
        <form on:submit|preventDefault={createTargetedTask}>
          <label class="hidden" for="new-task-goal">New task goal</label>
          <textarea
            id="new-task-goal"
            bind:value={newTaskDraft}
            rows="3"
            placeholder={selectedAgent ? 'Describe work for the selected machine and directory.' : 'Describe local homelabd work.'}
            disabled={creatingTask}
          ></textarea>
          <label class="context-confirm">
            <input type="checkbox" bind:checked={contextAcknowledged} disabled={!selectedAgent} />
            <span>Run exactly on <strong>{selectedContextLabel}</strong></span>
          </label>
          <button
            type="submit"
            disabled={creatingTask ||
              !newTaskDraft.trim() ||
              Boolean(selectedAgent && (!selectedWorkdir || !contextAcknowledged))}
          >
            {creatingTask ? 'Creating' : selectedAgent ? 'Create remote task' : 'Create local task'}
          </button>
        </form>
      </section>

      {#if pendingApprovalItems.length}
        <section class="approval-list" aria-label="Pending approvals">
          <h2>Needs decision</h2>
          {#each pendingApprovalItems as approval}
            <article>
              <span class="dot amber"></span>
              <div>
                <strong>{approval.tool}</strong>
                <small>{shortID(approval.id)}</small>
                <p>{truncate(approval.reason, 96)}</p>
                <div class="mini-actions">
                  <button type="button" disabled={loading} on:click={() => sendCommand(`approve ${approval.id}`)}>
                    approve
                  </button>
                  <button type="button" disabled={loading} on:click={() => sendCommand(`deny ${approval.id}`)}>
                    deny
                  </button>
                </div>
              </div>
            </article>
          {/each}
        </section>
      {/if}

      <section class="task-list" aria-label="Task list">
        {#if visibleTaskItems.length === 0}
          <p class="empty">No matching tasks.</p>
        {:else}
          {#each visibleTaskItems as task}
            <button
              type="button"
              class:selected={currentTask?.id === task.id}
              class="task-row"
              on:click={() => selectTask(task.id)}
            >
              <span class={`dot ${taskTone(task)}`} aria-hidden="true"></span>
              <span class="task-copy">
                <strong>{truncate(task.title || task.goal || task.id, 82)}</strong>
                <small>
                  <span>{shortID(task.id)} · updated {compactTime(task.updated_at)}</span>
                  <span class={`status ${taskTone(task)}`}>{statusLabel(task.status)}</span>
                </small>
                {#if task.target?.mode === 'remote'}
                  <em>{targetLabel(task)}</em>
                {/if}
              </span>
            </button>
          {/each}
        {/if}
      </section>

      <footer>{apiBase}</footer>
    </div>
  </aside>

  <main class="workbench" aria-label="Selected task record">
    <section class="task-record">
      <header class="record-header">
        <div>
          <p>Selected task</p>
          {#if currentTask}
            <h2>{taskSummaryTitle(currentTask)}</h2>
          {:else}
            <h2>No task selected</h2>
          {/if}
        </div>
      </header>

      {#if currentTask}
        <section class="record-summary" aria-label="Task summary">
          <div>
            <span>ID</span>
            <strong>{shortID(currentTask?.id)}</strong>
          </div>
          <div>
            <span>Status</span>
            <strong>
              <span class={`dot ${taskTone(currentTask)}`} aria-hidden="true"></span>
              {statusLabel(currentTask?.status)}
            </strong>
          </div>
          <div>
            <span>Owner</span>
            <strong>{currentTask?.assigned_to || 'unassigned'}</strong>
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
            <strong>{compactTime(currentTask?.updated_at)}</strong>
          </div>
        </section>

        <section class="state-machine" aria-label="Workflow state">
          <div>
            <span>Workflow state</span>
            <strong>{statusLabel(currentTask?.status)}</strong>
          </div>
          <p>{taskStateDescription(currentTask?.status)}</p>
          <small>Expected transition: {taskStateTransitions(currentTask?.status)}</small>
        </section>

        <section class={`next-step ${taskTone(currentTask)}`}>
          <span class={`dot ${taskTone(currentTask)}`} aria-hidden="true"></span>
          <div>
            <h3>{taskPrimaryAction(currentTask).label}</h3>
            <p>{taskPrimaryAction(currentTask).reason}</p>
          </div>
          <button
            type="button"
            class="primary-action"
            disabled={loading}
            on:click={() => sendCommand(taskPrimaryAction(currentTask).command)}
          >
            {taskPrimaryAction(currentTask).label}
          </button>
        </section>

        <section class="record-actions" aria-label="Task actions">
          {#each secondaryTaskActions(currentTask) as action}
            <button type="button" disabled={loading} on:click={() => sendCommand(action)}>
              {action}
            </button>
          {/each}
        </section>

        {#if currentTask?.workspace}
          <section class="workspace-path" aria-label="Workspace path">
            <span>Workspace</span>
            <code>{currentTask?.workspace}</code>
          </section>
        {/if}

        {#if currentTask?.target?.mode === 'remote'}
          <section class="execution-context" aria-label="Execution context">
            <span>Remote execution context</span>
            <strong>{currentTask.target.machine || currentTask.target.agent_id}</strong>
            <code>{currentTask.target.workdir}</code>
            <small>Agent {currentTask.target.agent_id} · backend {currentTask.target.backend || 'default'}</small>
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
                disabled={diffLoadingTaskId === currentTask?.id || isRemoteTask(currentTask)}
                on:click={() =>
                  currentTask?.id && !isRemoteTask(currentTask) && void refreshTaskDiff(currentTask.id)}
              >
                {diffLoadingTaskId === currentTask?.id ? 'Loading' : 'Refresh'}
              </button>
            </div>
          </header>

          {#if diffError}
            <p class="error" role="alert">{diffError}</p>
          {/if}

          {#if isRemoteTask(currentTask)}
            <p class="empty">Remote task diffs are recorded by the remote agent. Inspect the result, worker trace, or remote checkout for changed files and validation.</p>
          {:else if diffLoadingTaskId === currentTask?.id && !currentTaskDiff}
            <p class="empty">Loading task diff…</p>
          {:else if currentTaskDiff && !currentTaskDiff.raw_diff.trim()}
            <p class="empty">No diff found between this task branch and current main.</p>
          {:else if currentTaskDiff}
            <div class="diff-meta">
              <span>{currentTaskDiff.base_label || 'main'} → {currentTaskDiff.head_label || shortID(currentTaskDiff.task_id)}</span>
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
                    <small>{diffStatusLabel(file.status)} · +{file.additions} / -{file.deletions}</small>
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

        {#if currentTask?.result}
          <section class="task-result">
            <h3>Result</h3>
            <p>{currentTask?.result}</p>
          </section>
        {/if}

        <section class="worker-runs" aria-label="Worker runs">
          <header>
            <div>
              <p>Worker trace</p>
              <h3>{currentTaskRuns.length} run{currentTaskRuns.length === 1 ? '' : 's'}</h3>
            </div>
            <div class="worker-controls">
              <button
                type="button"
                disabled={taskActionLoading !== '' || !taskIsActive(currentTask)}
                on:click={() => void performTaskAction('cancel')}
              >
                {taskActionLoading === 'cancel' ? 'Stopping' : 'Stop'}
              </button>
              <button
                type="button"
                disabled={taskActionLoading !== '' || taskIsActive(currentTask)}
                on:click={() => void performTaskAction('retry')}
              >
                {taskActionLoading === 'retry' ? 'Retrying' : 'Retry'}
              </button>
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
                      <small>{shortID(run.id)} · {run.status} · {compactTime(run.startedAt)}</small>
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

        {#if currentTask?.plan}
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
      {:else}
        <section class="empty-record">
          <span class="dot gray" aria-hidden="true"></span>
          <div>
            <h2>Select a task to inspect it.</h2>
            <p>The right pane is a task record: summary, next action, result, and task-scoped activity.</p>
          </div>
        </section>
      {/if}
    </section>

    <section class="command-panel" class:collapsed={!commandPanelOpen} aria-label="Task command helper">
      <header>
        <div>
          <p>Task command</p>
          <h2>Act on this queue</h2>
        </div>
        <div class="command-header-actions">
          <span>For longer conversation, open Chat.</span>
          <button
            type="button"
            aria-expanded={commandPanelOpen}
            on:click={toggleCommandPanel}
          >
            {commandPanelOpen ? 'Collapse' : 'Open command'}
          </button>
        </div>
      </header>

      {#if commandPanelOpen}
        <section class="messages" bind:this={messagesEl} aria-live="polite">
          {#each messages.slice(-4) as message (message.id)}
            <article class="message" class:user={message.role === 'user'}>
              <div class="meta">
                <span>{message.role === 'user' ? 'You' : `homelabd - ${sourceLabel(message.source)}`}</span>
                <time>{message.time}</time>
              </div>
              <p>{message.content}</p>
              {#if message.role === 'assistant' && message.actions?.length}
                <div class="message-actions">
                  {#each message.actions as action}
                    <button type="button" disabled={loading} on:click={() => sendCommand(action)}>
                      {action}
                    </button>
                  {/each}
                </div>
              {/if}
            </article>
          {/each}

          {#if loading}
            <article class="message pending">
              <div class="meta">
                <span>homelabd - working</span>
                <time>Now</time>
              </div>
              <p>Working…</p>
            </article>
          {/if}
        </section>

        {#if error}
          <p class="error" role="alert">{error}</p>
        {/if}

        <section class="prompt-actions" aria-label="Prompt shortcuts">
          {#each promptActions as action}
            <button type="button" disabled={loading} on:click={() => sendCommand(action.command)}>
              <strong>{action.label}</strong>
              <span>{action.hint}</span>
            </button>
          {/each}
        </section>

        <form class="composer" on:submit|preventDefault={() => void sendMessage()}>
          <label class="hidden" for="message">Task command</label>
          <textarea
            id="message"
            bind:this={inputEl}
            bind:value={draft}
            autocomplete="off"
            placeholder="Send a task command, or switch to Chat for general conversation."
            disabled={loading}
            rows="3"
            on:keydown={handleComposerKeydown}
          ></textarea>
          <button type="submit" disabled={loading || !draft.trim()}>
            {loading ? 'Sending' : 'Send'}
          </button>
        </form>
      {:else}
        {#if error}
          <p class="error" role="alert">{error}</p>
        {/if}
        {#if loading}
          <article class="message pending compact-pending">
            <div class="meta">
              <span>homelabd - working</span>
              <time>Now</time>
            </div>
            <p>Working…</p>
          </article>
        {/if}
      {/if}
    </section>
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
    color: #172033;
    background: #f5f7fb;
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
  }

  .shell {
    display: grid;
    grid-template-columns: minmax(18rem, 24rem) minmax(0, 1fr);
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
    grid-template-rows: auto minmax(0, 1fr);
    gap: 0.75rem;
    padding: 1rem;
    border-right: 1px solid #dde4ef;
    background: #ffffff;
  }

  .task-sidebar-content {
    display: flex;
    flex-direction: column;
    gap: 0.75rem;
    min-height: 0;
  }

  .task-header,
  .record-header,
  .triage,
  .task-row,
  .next-step,
  .record-summary,
  .record-actions,
  .empty-record,
  .command-panel header,
  .approval-list article {
    display: flex;
    align-items: center;
  }

  .task-header {
    justify-content: space-between;
    gap: 0.75rem;
  }

  .task-header nav {
    display: flex;
    align-items: center;
    justify-content: flex-end;
    gap: 0.5rem;
  }

  .task-header p,
  .task-header h1,
  .task-header span,
  .record-header p,
  .record-header h2,
  .command-panel header p,
  .command-panel header h2,
  .next-step h3,
  .next-step p,
  .activity h3,
  .activity p,
  .message p,
  .approval-list p,
  .empty,
  footer {
    margin: 0;
  }

  .task-header p,
  .record-header p,
  .command-panel header p,
  .task-header span {
    color: #6b7280;
    font-size: 0.74rem;
    font-weight: 800;
    letter-spacing: 0.08em;
    text-transform: uppercase;
  }

  .task-header h1,
  .record-header h2,
  .command-panel header h2 {
    color: #111827;
    font-size: 1.35rem;
    line-height: 1.15;
  }

  .task-header button,
  .queue-toggle,
  .triage button,
  .mini-actions button,
  .record-actions button,
  .diff-controls button,
  .diff-file-list button,
  .worker-controls button,
  .next-step button,
  .message-actions button,
  .prompt-actions button {
    min-height: 2rem;
    padding: 0 0.7rem;
    border: 1px solid #cbd5e1;
    border-radius: 0.55rem;
    color: #243047;
    background: #ffffff;
    font-size: 0.82rem;
    font-weight: 750;
    text-decoration: none;
  }

  .task-header button:hover:not(:disabled),
  .queue-toggle:hover:not(:disabled),
  .triage button:hover,
  .mini-actions button:hover:not(:disabled),
  .record-actions button:hover:not(:disabled),
  .diff-controls button:hover:not(:disabled),
  .diff-file-list button:hover:not(:disabled),
  .worker-controls button:hover:not(:disabled),
  .next-step button:hover:not(:disabled),
  .message-actions button:hover:not(:disabled),
  .prompt-actions button:hover:not(:disabled) {
    border-color: #93b4e8;
    background: #eef5ff;
  }

  .triage {
    gap: 0.5rem;
  }

  .queue-toggle {
    display: none;
  }

  .triage button {
    flex: 1;
    min-width: 0;
    padding: 0.65rem;
    border: 1px solid #e2e8f0;
    border-radius: 0.75rem;
    background: #f8fafc;
    text-align: left;
  }

  .triage strong,
  .triage span {
    display: block;
  }

  .triage strong {
    color: #0f172a;
    font-size: 1.2rem;
  }

  .triage span,
  footer {
    color: #64748b;
    font-size: 0.74rem;
  }

  .mini-actions,
  .record-actions,
  .message-actions,
  .prompt-actions {
    display: flex;
    flex-wrap: wrap;
    gap: 0.45rem;
  }

  .triage button.active {
    border-color: #2563eb;
    background: #eff6ff;
  }

  .triage button.active span,
  .triage button.active strong {
    color: #1d4ed8;
  }

  input,
  select,
  textarea {
    box-sizing: border-box;
    width: 100%;
    border: 1px solid #cbd5e1;
    border-radius: 0.75rem;
    color: #111827;
    background: #ffffff;
  }

  input {
    min-height: 2.4rem;
    padding: 0 0.75rem;
  }

  select {
    min-height: 2.35rem;
    padding: 0 0.6rem;
  }

  textarea {
    min-height: 4.3rem;
    max-height: 11rem;
    padding: 0.8rem 0.9rem;
    line-height: 1.45;
    resize: vertical;
  }

  input:focus,
  select:focus,
  textarea:focus {
    border-color: #2563eb;
    outline: 3px solid rgb(37 99 235 / 0.14);
  }

  .approval-list {
    display: grid;
    gap: 0.5rem;
  }

  .queue-groups {
    display: grid;
    gap: 0.35rem;
  }

  .queue-groups h2 {
    margin: 0;
    color: #374151;
    font-size: 0.82rem;
  }

  .queue-groups button {
    display: grid;
    gap: 0.15rem;
    width: 100%;
    min-height: 2.8rem;
    padding: 0.55rem 0.65rem;
    border: 1px solid #e2e8f0;
    border-radius: 0.65rem;
    color: #243047;
    background: #ffffff;
    text-align: left;
  }

  .queue-groups button.active {
    border-color: #2563eb;
    background: #eff6ff;
  }

  .queue-groups strong {
    color: #111827;
    font-size: 0.83rem;
  }

  .queue-groups span {
    color: #64748b;
    font-size: 0.72rem;
    line-height: 1.25;
  }

  .target-create {
    display: grid;
    gap: 0.55rem;
    padding: 0.75rem;
    border: 1px solid #dbe7f5;
    border-radius: 0.8rem;
    background: #f8fbff;
  }

  .target-create header {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: 0.7rem;
  }

  .target-create header p,
  .target-create header h2,
  .target-create header span,
  .target-empty {
    margin: 0;
  }

  .target-create header p,
  .target-create header span {
    color: #64748b;
    font-size: 0.7rem;
    font-weight: 800;
    letter-spacing: 0.08em;
    text-transform: uppercase;
  }

  .target-create header h2 {
    color: #111827;
    font-size: 0.95rem;
    line-height: 1.2;
  }

  .target-create form {
    display: grid;
    gap: 0.5rem;
  }

  .target-create button {
    min-height: 2.3rem;
    border: 1px solid #1d4ed8;
    border-radius: 0.55rem;
    color: #ffffff;
    background: #2563eb;
    font-weight: 800;
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

  .target-empty {
    color: #64748b;
    font-size: 0.8rem;
    line-height: 1.35;
  }

  .approval-list h2 {
    margin: 0;
    color: #374151;
    font-size: 0.82rem;
  }

  .approval-list article {
    align-items: flex-start;
    gap: 0.6rem;
    padding: 0.7rem;
    border: 1px solid #fde68a;
    border-radius: 0.8rem;
    background: #fffbeb;
  }

  .approval-list strong {
    display: inline-block;
    color: #713f12;
    font-size: 0.88rem;
  }

  .approval-list small {
    margin-left: 0.35rem;
    color: #b45309;
    font-size: 0.72rem;
  }

  .approval-list p {
    margin: 0.2rem 0 0.5rem;
    color: #92400e;
    font-size: 0.78rem;
    line-height: 1.35;
  }

  .task-list {
    display: grid;
    align-content: start;
    gap: 0.35rem;
    flex: 1 1 auto;
    min-height: 0;
    overflow-y: auto;
    padding-right: 0.2rem;
  }

  .task-row {
    gap: 0.7rem;
    width: 100%;
    min-width: 0;
    padding: 0.72rem;
    border: 1px solid transparent;
    border-radius: 0.8rem;
    color: inherit;
    background: transparent;
    text-align: left;
  }

  .task-row:hover,
  .task-row.selected {
    border-color: #d7e3f5;
    background: #f3f7fc;
  }

  .task-copy {
    display: grid;
    gap: 0.18rem;
    min-width: 0;
  }

  .task-copy strong {
    overflow: hidden;
    color: #111827;
    font-size: 0.9rem;
    line-height: 1.25;
    text-overflow: ellipsis;
  }

  .task-copy small {
    display: flex;
    align-items: center;
    gap: 0.4rem;
    min-width: 0;
    color: #64748b;
    font-size: 0.76rem;
  }

  .task-copy em {
    overflow: hidden;
    color: #475569;
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
    padding: 0.12rem 0.42rem;
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

  .dot.gray {
    background: #94a3b8;
  }

  .empty {
    padding: 1rem;
    color: #64748b;
    text-align: center;
  }

  footer {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .workbench {
    display: grid;
    grid-template-rows: minmax(0, 1fr) auto;
    min-width: 0;
    background: #eef2f7;
  }

  .task-record {
    min-width: 0;
    min-height: 0;
    overflow-y: auto;
    background: #f8fafc;
  }

  .record-header {
    justify-content: space-between;
    gap: 1rem;
    min-width: 0;
    padding: 1.1rem 1.25rem 0.7rem;
    background: #ffffff;
  }

  .record-header > div {
    min-width: 0;
  }

  .record-header h2 {
    overflow-wrap: anywhere;
    white-space: normal;
  }

  .record-summary {
    flex-wrap: wrap;
    gap: 0.65rem;
    padding: 0 1.25rem 1rem;
    border-bottom: 1px solid #dde4ef;
    background: #ffffff;
  }

  .record-summary div {
    min-width: 8rem;
    padding: 0.65rem 0.75rem;
    border: 1px solid #e2e8f0;
    border-radius: 0.75rem;
    background: #f8fafc;
  }

  .record-summary span,
  .workspace-path span,
  .activity header p,
  .command-panel header span {
    display: block;
    color: #64748b;
    font-size: 0.72rem;
    font-weight: 800;
    letter-spacing: 0.06em;
    text-transform: uppercase;
  }

  .record-summary strong {
    display: flex;
    align-items: center;
    gap: 0.45rem;
    margin-top: 0.2rem;
    color: #111827;
    font-size: 0.9rem;
  }

  .next-step {
    align-items: flex-start;
    gap: 0.7rem;
    margin: 1rem 1.25rem 0;
    padding: 0.85rem;
    border: 1px solid #dde4ef;
    border-radius: 0.9rem;
    background: #ffffff;
  }

  .next-step.red {
    background: #fff7f7;
  }

  .next-step.amber {
    background: #fffbeb;
  }

  .next-step.blue {
    background: #eff6ff;
  }

  .next-step.green {
    background: #f0fdf4;
  }

  .next-step h3 {
    color: #111827;
    font-size: 0.92rem;
  }

  .next-step p {
    margin-top: 0.15rem;
    color: #475569;
    font-size: 0.82rem;
    line-height: 1.35;
  }

  .next-step .primary-action {
    margin-left: auto;
    border-color: #2563eb;
    color: #ffffff;
    background: #2563eb;
    white-space: nowrap;
  }

  .next-step .primary-action:hover:not(:disabled) {
    border-color: #1d4ed8;
    background: #1d4ed8;
  }

  .record-actions {
    padding: 0.75rem 1.25rem 0;
  }

  .workspace-path,
  .execution-context,
  .diff-review,
  .task-result,
  .task-plan,
  .task-input,
  .state-machine,
  .worker-runs,
  .activity,
  .empty-record {
    margin: 1rem 1.25rem 0;
    border: 1px solid #e2e8f0;
    border-radius: 0.9rem;
    background: #ffffff;
  }

  .workspace-path {
    padding: 0.8rem;
  }

  .execution-context {
    display: grid;
    gap: 0.25rem;
    padding: 0.8rem;
    border-width: 2px;
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

  .execution-context strong {
    color: #713f12;
  }

  .execution-context code {
    overflow-wrap: anywhere;
    color: #111827;
    font-size: 0.86rem;
  }

  .execution-context small {
    color: #92400e;
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

  .diff-review header p,
  .diff-review h3,
  .diff-file strong,
  .diff-file small,
  .diff-file header span {
    margin: 0;
  }

  .diff-review header p {
    color: var(--diff-muted);
    font-size: 0.72rem;
    font-weight: 800;
    letter-spacing: 0.06em;
    text-transform: uppercase;
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

  .diff-controls button {
    border-color: var(--diff-border-strong);
    color: var(--diff-text);
    background: var(--diff-bg);
  }

  .diff-controls button:hover:not(:disabled),
  .diff-file-list button:hover:not(:disabled) {
    border-color: var(--diff-selected-border);
    background: var(--diff-bg-hover);
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
    border-radius: 0.55rem;
    color: var(--diff-text);
    background: transparent;
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

  .diff-file-list span {
    font-size: 0.8rem;
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

  .split-diff {
    min-width: 0;
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
    color: #64748b;
    font-size: 0.72rem;
    font-weight: 800;
    letter-spacing: 0.06em;
    text-transform: uppercase;
  }

  .state-machine strong {
    color: #111827;
    font-size: 0.9rem;
  }

  .state-machine p {
    margin: 0;
    color: #334155;
    font-size: 0.88rem;
    line-height: 1.4;
  }

  .workspace-path code {
    display: block;
    margin-top: 0.35rem;
    overflow-wrap: anywhere;
    color: #334155;
    font-size: 0.82rem;
  }

  .task-result {
    max-height: 10rem;
    overflow-y: auto;
    padding: 0.8rem;
  }

  .task-plan {
    overflow: hidden;
  }

  .task-plan header {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: 0.75rem;
    padding: 0.85rem;
    border-bottom: 1px solid #e2e8f0;
  }

  .task-plan header p,
  .task-plan h3,
  .task-plan ol,
  .task-plan ul,
  .task-plan li,
  .task-plan .plan-review {
    margin: 0;
  }

  .task-plan header p,
  .task-plan header span,
  .plan-risks > strong {
    color: #64748b;
    font-size: 0.72rem;
    font-weight: 800;
    letter-spacing: 0.06em;
    text-transform: uppercase;
  }

  .task-plan header span {
    flex: 0 0 auto;
    border-radius: 999px;
    background: #dbeafe;
    color: #1d4ed8;
    padding: 0.22rem 0.5rem;
  }

  .task-plan h3 {
    margin-top: 0.15rem;
    color: #111827;
    font-size: 0.95rem;
    line-height: 1.35;
    overflow-wrap: anywhere;
  }

  .task-plan ol {
    display: grid;
    gap: 0.75rem;
    padding: 0.85rem 0.85rem 0.85rem 2rem;
  }

  .task-plan li {
    color: #475569;
    font-size: 0.88rem;
    line-height: 1.45;
    overflow-wrap: anywhere;
  }

  .task-plan li strong {
    display: block;
    color: #111827;
    font-size: 0.9rem;
  }

  .task-plan li p,
  .plan-risks li,
  .plan-review {
    color: #475569;
    font-size: 0.86rem;
    line-height: 1.45;
    overflow-wrap: anywhere;
  }

  .plan-risks,
  .plan-review {
    border-top: 1px solid #e2e8f0;
    padding: 0.85rem;
  }

  .plan-risks ul {
    display: grid;
    gap: 0.35rem;
    margin-top: 0.45rem;
    padding-left: 1rem;
  }

  .task-result h3,
  .task-result p,
  .task-input h3,
  .task-input p {
    margin: 0;
  }

  .task-result h3,
  .task-input h3 {
    color: #111827;
    font-size: 0.9rem;
  }

  .task-result p,
  .task-input p {
    margin-top: 0.4rem;
    color: #475569;
    font-size: 0.88rem;
    line-height: 1.45;
    overflow-wrap: anywhere;
    white-space: pre-wrap;
  }

  .activity {
    overflow: hidden;
  }

  .worker-runs {
    overflow: hidden;
  }

  .worker-runs > header,
  .worker-run > header {
    display: flex;
    align-items: center;
    gap: 0.75rem;
  }

  .worker-runs > header {
    justify-content: space-between;
    padding: 0.85rem;
    border-bottom: 1px solid #e2e8f0;
  }

  .worker-runs header p,
  .worker-runs h3,
  .worker-run p {
    margin: 0;
  }

  .worker-runs header p {
    color: #64748b;
    font-size: 0.72rem;
    font-weight: 800;
    letter-spacing: 0.06em;
    text-transform: uppercase;
  }

  .worker-runs h3 {
    margin-top: 0.1rem;
    color: #111827;
    font-size: 0.95rem;
  }

  .worker-controls {
    display: flex;
    flex-wrap: wrap;
    justify-content: flex-end;
    gap: 0.45rem;
  }

  .run-list {
    display: grid;
    gap: 0.75rem;
    padding: 0.85rem;
  }

  .worker-run {
    overflow: hidden;
    border: 1px solid #e2e8f0;
    border-radius: 0.75rem;
    background: #f8fafc;
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
    padding: 0.75rem 0.8rem 0;
  }

  .worker-run strong,
  .worker-run small {
    display: block;
  }

  .worker-run strong {
    color: #111827;
    font-size: 0.88rem;
  }

  .worker-run small,
  .run-command,
  .run-artifact-path {
    color: #64748b;
    font-size: 0.76rem;
  }

  .artifact-pill {
    margin-left: auto;
    border: 1px solid #cbd5e1;
    border-radius: 999px;
    padding: 0.15rem 0.45rem;
    color: #475569;
    background: #ffffff;
    font-size: 0.68rem;
    font-weight: 850;
  }

  .run-command,
  .run-artifact-path,
  .run-error {
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

  .task-input {
    margin-bottom: 1.25rem;
    padding: 0.8rem;
  }

  .activity header {
    padding: 0.85rem;
    border-bottom: 1px solid #e2e8f0;
  }

  .activity h3 {
    margin-top: 0.1rem;
    color: #111827;
    font-size: 0.95rem;
  }

  .activity ol {
    display: grid;
    gap: 0;
    margin: 0;
    padding: 0;
    list-style: none;
  }

  .activity li {
    display: grid;
    grid-template-columns: 4.5rem minmax(0, 1fr);
    gap: 0.85rem;
    padding: 0.8rem;
    border-top: 1px solid #f1f5f9;
  }

  .activity time {
    color: #64748b;
    font-size: 0.76rem;
  }

  .activity li strong,
  .activity li span {
    display: block;
  }

  .activity li strong {
    color: #172033;
    font-size: 0.86rem;
  }

  .activity li span {
    margin-top: 0.1rem;
    color: #64748b;
    font-size: 0.74rem;
  }

  .activity li p {
    margin-top: 0.35rem;
    color: #334155;
    font-size: 0.82rem;
    line-height: 1.4;
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
    color: #111827;
    font-size: 1rem;
  }

  .empty-record p {
    margin-top: 0.25rem;
    color: #64748b;
    line-height: 1.4;
  }

  .command-panel {
    display: grid;
    grid-template-rows: auto minmax(0, auto) auto auto;
    max-height: 42dvh;
    border-top: 2px solid #cbd5e1;
    background: #ffffff;
    box-shadow: 0 -10px 24px rgb(15 23 42 / 0.08);
  }

  .command-panel.collapsed {
    grid-template-rows: auto;
    max-height: none;
  }

  .command-panel header {
    justify-content: space-between;
    gap: 1rem;
    padding: 0.85rem 1.25rem 0.45rem;
  }

  .command-panel header h2 {
    font-size: 1rem;
  }

  .command-header-actions {
    display: flex;
    align-items: center;
    justify-content: flex-end;
    gap: 0.75rem;
  }

  .command-header-actions button {
    position: relative;
    z-index: 1;
    flex: 0 0 auto;
    min-height: 2rem;
    padding: 0 0.7rem;
    border: 1px solid #cbd5e1;
    border-radius: 0.55rem;
    color: #243047;
    background: #ffffff;
    font: inherit;
    font-size: 0.82rem;
    font-weight: 750;
  }

  .compact-pending {
    margin: 0 1.25rem 0.75rem;
  }

  .messages {
    display: flex;
    flex-direction: column;
    gap: 0.8rem;
    min-height: 0;
    overflow-y: auto;
    padding: 0.35rem 1.25rem 0.75rem;
  }

  .message {
    display: grid;
    gap: 0.45rem;
    max-width: min(48rem, 92%);
    padding: 0.8rem 0.9rem;
    border: 1px solid #e2e8f0;
    border-radius: 0.8rem;
    background: #ffffff;
    box-shadow: 0 1px 2px rgb(15 23 42 / 0.04);
  }

  .message.user {
    align-self: flex-end;
    border-color: #bbf7d0;
    background: #f0fdf4;
  }

  .message.pending {
    color: #475569;
    border-style: dashed;
  }

  .meta {
    display: flex;
    align-items: baseline;
    justify-content: space-between;
    gap: 0.75rem;
    color: #64748b;
    font-size: 0.74rem;
  }

  .meta span {
    color: #243047;
    font-weight: 800;
  }

  .message p {
    color: #172033;
    line-height: 1.48;
    overflow-wrap: anywhere;
    white-space: pre-wrap;
  }

  .error {
    margin: 0;
    padding: 0.75rem 1.25rem;
    border-top: 1px solid #fecaca;
    color: #991b1b;
    background: #fef2f2;
    overflow-wrap: anywhere;
  }

  .prompt-actions {
    padding: 0 1.25rem 0.75rem;
  }

  .prompt-actions button {
    display: grid;
    gap: 0.1rem;
    min-width: 8rem;
    padding-block: 0.45rem;
    text-align: left;
  }

  .prompt-actions strong {
    color: #111827;
    font-size: 0.8rem;
  }

  .prompt-actions span {
    color: #64748b;
    font-size: 0.7rem;
    font-weight: 650;
  }

  .composer {
    display: grid;
    grid-template-columns: minmax(0, 1fr) auto;
    gap: 0.75rem;
    padding: 1rem 1.25rem;
    border-top: 1px solid #dde4ef;
    background: #ffffff;
  }

  .composer button[type='submit'] {
    min-width: 5.75rem;
    min-height: 4.3rem;
    border: 0;
    border-radius: 0.75rem;
    color: #ffffff;
    background: #2563eb;
    font-weight: 850;
  }

  .composer button[type='submit']:hover:not(:disabled) {
    background: #1d4ed8;
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

  @media (max-width: 760px) {
    :global(html),
    :global(body) {
      overflow: auto;
    }

    .tasks-page {
      height: auto;
      min-height: 100dvh;
      grid-template-rows: auto auto;
      padding-top: 3.75rem;
    }

    :global(.navbar) {
      position: fixed !important;
      top: 0 !important;
      right: 0;
      left: 0;
    }

    .shell {
      display: grid;
      grid-template-columns: minmax(0, 1fr);
      min-height: 0;
      height: auto;
      overflow: visible;
    }

    .tasks-page {
      height: auto;
      min-height: 100dvh;
    }

    .task-pane {
      display: grid;
      max-height: min(54dvh, 28rem);
      padding: 0.75rem;
      border-right: 0;
      border-bottom: 1px solid #dde4ef;
      overflow: hidden;
    }

    .task-header {
      align-items: flex-start;
      gap: 0.6rem;
    }

    .task-header h1 {
      font-size: 1.12rem;
    }

    .task-header nav {
      flex-wrap: wrap;
    }

    .task-pane.collapsed {
      position: sticky;
      top: 0;
      z-index: 5;
      max-height: none;
      box-shadow: 0 8px 18px rgb(15 23 42 / 0.08);
    }

    .task-pane.collapsed .task-sidebar-content {
      display: none;
    }

    .task-sidebar-content {
      overflow: hidden;
    }

    .triage {
      gap: 0.35rem;
    }

    .triage button {
      padding: 0.5rem;
    }

    .triage strong {
      font-size: 1rem;
    }

    .task-row {
      align-items: flex-start;
      padding: 0.58rem;
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

    .queue-toggle {
      display: inline-flex;
      align-items: center;
    }

    .workbench {
      display: block;
      min-height: 0;
      overflow: visible;
    }

    .task-record {
      overflow: visible;
    }

    .record-header h2 {
      white-space: normal;
    }

    .record-summary {
      display: grid;
      grid-template-columns: 1fr 1fr;
    }

    .record-summary div {
      min-width: 0;
    }

    .next-step {
      flex-direction: column;
    }

    .next-step .primary-action {
      margin-left: 0;
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
      border-bottom: 1px solid #e2e8f0;
    }

    .diff-file-list button {
      flex: 0 0 min(16rem, 78vw);
    }

    .diff-scroll {
      max-height: 28rem;
    }

    .command-panel {
      max-height: none;
    }

    .command-panel header {
      align-items: flex-start;
      flex-direction: column;
    }

    .activity li {
      grid-template-columns: 1fr;
      gap: 0.25rem;
    }

    .composer {
      grid-template-columns: 1fr;
    }

    .composer button[type='submit'] {
      min-height: 2.8rem;
    }

    .message {
      max-width: 100%;
    }
  }
</style>
