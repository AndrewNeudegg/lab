import {
  pendingActionableApprovals,
  taskIsActive,
  taskNeedsQueueAction
} from '@homelab/shared';
import type { HomelabdApproval, HomelabdEvent, HomelabdTask } from '@homelab/shared';

export type TaskFilter = 'attention' | 'active' | 'all';

export interface TaskQueueViewInput {
  tasks: HomelabdTask[];
  approvals: HomelabdApproval[];
  events: HomelabdEvent[];
  taskFilter: TaskFilter;
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

const normalizeSearch = (value: string) => value.trim().toLowerCase();

const taskMatchesSearch = (task: HomelabdTask, search: string) => {
  const query = normalizeSearch(search);
  if (!query) {
    return true;
  }
  return [task.id, task.title, task.goal, task.status, task.assigned_to, task.plan?.summary]
    .join(' ')
    .toLowerCase()
    .includes(query);
};

const visibleTasksForFilter = (
  tasks: HomelabdTask[],
  approvals: HomelabdApproval[],
  taskFilter: TaskFilter,
  taskSearch: string
) => {
  const filtered =
    taskFilter === 'attention'
      ? tasks.filter((task) => taskNeedsQueueAction(task, approvals))
      : taskFilter === 'active'
        ? tasks.filter(taskIsActive)
        : tasks;

  return filtered.filter((task) => taskMatchesSearch(task, taskSearch));
};

const eventsForTask = (events: HomelabdEvent[], taskID: string) =>
  events
    .filter((event) => event.task_id === taskID)
    .sort((left, right) => Date.parse(right.time) - Date.parse(left.time))
    .slice(0, 80);

export const selectTaskForQueue = (
  tasks: HomelabdTask[],
  approvals: HomelabdApproval[],
  taskFilter: TaskFilter,
  taskSearch: string,
  selectedTaskId: string
) => {
  const visibleTaskItems = visibleTasksForFilter(tasks, approvals, taskFilter, taskSearch);
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
  taskSearch,
  selectedTaskId
}: TaskQueueViewInput): TaskQueueView => {
  const pendingApprovalItems = pendingActionableApprovals(approvals, tasks);
  const attentionTaskItems = tasks.filter((task) => taskNeedsQueueAction(task, approvals));
  const activeTaskItems = tasks.filter(taskIsActive);
  const visibleTaskItems = visibleTasksForFilter(tasks, approvals, taskFilter, taskSearch);
  const normalizedSelectedTaskId = selectTaskForQueue(
    tasks,
    approvals,
    taskFilter,
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
