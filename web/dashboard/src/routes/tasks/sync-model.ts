export const collectionFromResponse = <T>(label: string, key: string, response: unknown): T[] => {
  if (Array.isArray(response)) {
    return response as T[];
  }
  if (response && typeof response === 'object') {
    const value = (response as Record<string, unknown>)[key];
    if (Array.isArray(value)) {
      return value as T[];
    }
  }
  throw new Error(`${label} response did not contain a ${key} array.`);
};

export const errorMessage = (err: unknown, fallback: string) =>
  err instanceof Error ? err.message : fallback;

export const sustainedTaskSyncFailureCount = 3;

export type TaskSyncIndicatorTone = 'connected' | 'temporary-error' | 'sustained-error';

export type TaskSyncIndicatorState = {
  tone: TaskSyncIndicatorTone;
  dotTone: 'green' | 'amber' | 'red';
  label: string;
  detail: string;
  title: string;
};

export const taskSyncIndicatorState = ({
  refreshing,
  lastRefresh,
  failureCount,
  issue
}: {
  refreshing: boolean;
  lastRefresh: string;
  failureCount: number;
  issue: string;
}): TaskSyncIndicatorState => {
  const hasSuccessfulUpdate = Boolean(lastRefresh);
  const lastSuccessful = hasSuccessfulUpdate
    ? `Last successful update ${lastRefresh}`
    : 'No successful update yet';
  const failureTitle = issue ? `${lastSuccessful}. ${issue}` : lastSuccessful;

  if (failureCount >= sustainedTaskSyncFailureCount) {
    return {
      tone: 'sustained-error',
      dotTone: 'red',
      label: 'Connection error',
      detail: hasSuccessfulUpdate ? lastSuccessful : 'No successful update',
      title: failureTitle
    };
  }

  if (failureCount > 0) {
    return {
      tone: 'temporary-error',
      dotTone: 'amber',
      label: refreshing ? 'Reconnecting' : 'Disconnected',
      detail: hasSuccessfulUpdate ? `${lastSuccessful}; retrying` : 'Retrying task API',
      title: failureTitle
    };
  }

  if (!hasSuccessfulUpdate) {
    return {
      tone: 'temporary-error',
      dotTone: 'amber',
      label: 'Connecting',
      detail: 'Waiting for first update',
      title: 'Waiting for the first successful task API update.'
    };
  }

  return {
    tone: 'connected',
    dotTone: 'green',
    label: 'Connected',
    detail: `Updated ${lastRefresh}`,
    title: refreshing ? `Updating task data. Last update ${lastRefresh}.` : `Task data updated ${lastRefresh}.`
  };
};

export type RefreshTimers = {
  setTimeout: (handler: () => void, timeoutMs: number) => ReturnType<typeof setTimeout>;
  clearTimeout: (timer: ReturnType<typeof setTimeout>) => void;
};

export function withRefreshTimeout<T>(
  label: string,
  operation: Promise<T>,
  timeoutMs: number,
  timers: RefreshTimers = globalThis
): Promise<T> {
  return new Promise((resolve, reject) => {
    const timer = timers.setTimeout(
      () => reject(new Error(`${label} timed out after ${timeoutMs / 1000}s.`)),
      timeoutMs
    );

    operation.then(
      (value) => {
        timers.clearTimeout(timer);
        resolve(value);
      },
      (err) => {
        timers.clearTimeout(timer);
        reject(err);
      }
    );
  });
}

export const taskListEmptyMessage = ({
  apiBase,
  refreshing,
  taskLoadError,
  totalTasks
}: {
  apiBase: string;
  refreshing: boolean;
  taskLoadError: string;
  totalTasks: number;
}) => {
  if (taskLoadError) {
    return totalTasks > 0
      ? 'Showing the last loaded tasks. Sync failed before a fresh task list arrived.'
      : `Task sync failed before any tasks loaded from ${apiBase}/tasks.`;
  }
  if (refreshing && totalTasks === 0) {
    return 'Loading tasks...';
  }
  if (totalTasks === 0) {
    return `No tasks returned from ${apiBase}/tasks.`;
  }
  return 'No tasks match the current filters.';
};
