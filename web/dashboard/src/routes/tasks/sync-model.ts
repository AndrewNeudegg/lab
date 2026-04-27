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
