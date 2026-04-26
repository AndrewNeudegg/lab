import {
  pendingActionableApprovals,
  taskIsActive,
  taskNeedsQueueAction
} from '@homelab/shared';
import type {
  HomelabdApproval,
  HomelabdEvent,
  HomelabdRunArtifact,
  HomelabdTask
} from '@homelab/shared';

export type TaskFilter = 'attention' | 'active' | 'all';
export type TaskQueueFilter = 'all' | 'local' | `agent:${string}`;

export interface TaskQueueViewInput {
  tasks: HomelabdTask[];
  approvals: HomelabdApproval[];
  events: HomelabdEvent[];
  taskFilter: TaskFilter;
  queueFilter: TaskQueueFilter;
  taskSearch: string;
  selectedTaskId: string;
}

export interface TaskQueueView {
  pendingApprovalItems: HomelabdApproval[];
  attentionTaskItems: HomelabdTask[];
  activeTaskItems: HomelabdTask[];
  visibleTaskItems: HomelabdTask[];
  selectedTaskId: string;
  currentTask?: HomelabdTask;
  currentTaskEvents: HomelabdEvent[];
}

export interface WorkerTraceRun {
  id: string;
  backend: string;
  status: string;
  startedAt: string;
  finishedAt?: string;
  command?: string[];
  output: string;
  error?: string;
  artifact?: HomelabdRunArtifact;
  events: HomelabdEvent[];
  active: boolean;
}

const normalizeSearch = (value: string) => value.trim().toLowerCase();

const taskMatchesSearch = (task: HomelabdTask, search: string) => {
  const query = normalizeSearch(search);
  if (!query) {
    return true;
  }
  return [
    task.id,
    task.title,
    task.goal,
    task.status,
    task.assigned_to,
    task.target?.agent_id,
    task.target?.machine,
    task.target?.workdir,
    task.plan?.summary
  ]
    .join(' ')
    .toLowerCase()
    .includes(query);
};

const taskMatchesQueue = (task: HomelabdTask, queueFilter: TaskQueueFilter) => {
  if (queueFilter === 'all') {
    return true;
  }
  const mode = task.target?.mode || 'local';
  if (queueFilter === 'local') {
    return mode !== 'remote';
  }
  const agentID = queueFilter.slice('agent:'.length);
  return mode === 'remote' && task.target?.agent_id === agentID;
};

const visibleTasksForFilter = (
  tasks: HomelabdTask[],
  approvals: HomelabdApproval[],
  taskFilter: TaskFilter,
  queueFilter: TaskQueueFilter,
  taskSearch: string
) => {
  const queueTasks = tasks.filter((task) => taskMatchesQueue(task, queueFilter));
  const filtered =
    taskFilter === 'attention'
      ? queueTasks.filter((task) => taskNeedsQueueAction(task, approvals))
      : taskFilter === 'active'
        ? queueTasks.filter(taskIsActive)
        : queueTasks;

  return filtered.filter((task) => taskMatchesSearch(task, taskSearch));
};

const eventsForTask = (events: HomelabdEvent[], taskID: string) =>
  events
    .filter((event) => event.task_id === taskID)
    .sort((left, right) => Date.parse(right.time) - Date.parse(left.time))
    .slice(0, 80);

const payloadRecord = (event: HomelabdEvent): Record<string, unknown> =>
  event.payload && typeof event.payload === 'object' ? (event.payload as Record<string, unknown>) : {};

const stringPayload = (event: HomelabdEvent, key: string) => {
  const value = payloadRecord(event)[key];
  return typeof value === 'string' ? value : '';
};

export const buildWorkerTraceRuns = (
  events: HomelabdEvent[],
  artifacts: HomelabdRunArtifact[] = []
): WorkerTraceRun[] => {
  const runs = new Map<string, WorkerTraceRun>();
  const ensureRun = (id: string, seed: Partial<WorkerTraceRun> = {}) => {
    const existing = runs.get(id);
    if (existing) {
      return existing;
    }
    const run: WorkerTraceRun = {
      id,
      backend: seed.backend || 'worker',
      status: seed.status || 'running',
      startedAt: seed.startedAt || '',
      finishedAt: seed.finishedAt,
      command: seed.command,
      output: seed.output || '',
      error: seed.error,
      artifact: seed.artifact,
      events: seed.events || [],
      active: seed.active ?? true
    };
    runs.set(id, run);
    return run;
  };

  for (const artifact of artifacts) {
    ensureRun(artifact.id, {
      backend: artifact.backend,
      status: artifact.status || 'completed',
      startedAt: artifact.started_at || artifact.time || '',
      finishedAt: artifact.finished_at,
      command: artifact.command,
      output: artifact.output || '',
      error: artifact.error,
      artifact,
      active: artifact.status === 'running'
    });
  }

  for (const event of events) {
    if (!event.type.startsWith('agent.delegate.')) {
      continue;
    }
    const id = stringPayload(event, 'id') || stringPayload(event, 'run_id');
    if (!id) {
      continue;
    }
    const run = ensureRun(id, {
      backend: stringPayload(event, 'backend') || event.actor,
      startedAt: event.time
    });
    run.events = [...run.events, event].sort(
      (left, right) => Date.parse(left.time) - Date.parse(right.time)
    );
    if (!run.startedAt || Date.parse(event.time) < Date.parse(run.startedAt)) {
      run.startedAt = event.time;
    }
    if (event.type === 'agent.delegate.output') {
      if (run.artifact && run.output === (run.artifact.output || '')) {
        run.output = '';
      }
      run.output += stringPayload(event, 'text');
      run.backend = stringPayload(event, 'backend') || run.backend;
    }
    if (event.type === 'agent.delegate.completed') {
      run.status = 'completed';
      run.active = false;
      run.finishedAt = event.time;
    }
    if (event.type === 'agent.delegate.failed') {
      run.status = 'failed';
      run.active = false;
      run.finishedAt = event.time;
      run.error = stringPayload(event, 'error') || run.error;
    }
    if (event.type === 'agent.delegate.ignored') {
      run.status = 'ignored';
      run.active = false;
      run.finishedAt = event.time;
    }
  }

  return [...runs.values()].sort((left, right) => Date.parse(right.startedAt) - Date.parse(left.startedAt));
};

export const selectTaskForQueue = (
  tasks: HomelabdTask[],
  approvals: HomelabdApproval[],
  taskFilter: TaskFilter,
  queueFilter: TaskQueueFilter,
  taskSearch: string,
  selectedTaskId: string
) => {
  const visibleTaskItems = visibleTasksForFilter(tasks, approvals, taskFilter, queueFilter, taskSearch);
  if (selectedTaskId && visibleTaskItems.some((task) => task.id === selectedTaskId)) {
    return selectedTaskId;
  }
  return visibleTaskItems[0]?.id || '';
};

export const createTaskQueueView = ({
  tasks,
  approvals,
  events,
  taskFilter,
  queueFilter,
  taskSearch,
  selectedTaskId
}: TaskQueueViewInput): TaskQueueView => {
  const pendingApprovalItems = pendingActionableApprovals(approvals, tasks);
  const attentionTaskItems = tasks.filter((task) => taskNeedsQueueAction(task, approvals));
  const activeTaskItems = tasks.filter(taskIsActive);
  const visibleTaskItems = visibleTasksForFilter(tasks, approvals, taskFilter, queueFilter, taskSearch);
  const normalizedSelectedTaskId = selectTaskForQueue(
    tasks,
    approvals,
    taskFilter,
    queueFilter,
    taskSearch,
    selectedTaskId
  );
  const currentTask = visibleTaskItems.find((task) => task.id === normalizedSelectedTaskId);

  return {
    pendingApprovalItems,
    attentionTaskItems,
    activeTaskItems,
    visibleTaskItems,
    selectedTaskId: normalizedSelectedTaskId,
    currentTask,
    currentTaskEvents: currentTask ? eventsForTask(events, currentTask.id) : []
  };
};
