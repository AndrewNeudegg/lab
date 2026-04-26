import type {
  FetchClient,
  FetchClientOptions,
  HomelabdApprovalsResponse,
  HomelabdClient,
  HomelabdClientOptions,
  HomelabdEventsResponse,
  HomelabdMessageRequest,
  HomelabdMessageResponse,
  HomelabdTaskActionResponse,
  HomelabdTaskRetryRequest,
  HomelabdTaskRunsResponse,
  HomelabdTasksResponse,
  SupervisorSnapshot
} from './types';

export const DEFAULT_HOMELABD_API_BASE = 'http://127.0.0.1:18080';
export const DEFAULT_HEALTHD_API_BASE = 'http://127.0.0.1:18081';
export const DEFAULT_SUPERVISORD_API_BASE = 'http://127.0.0.1:18082';

export const apiFetch: FetchClient = async <TResponse>(
  path: string,
  options: FetchClientOptions = {}
): Promise<TResponse> => {
  const { baseUrl = '/api', fetcher = fetch, headers, ...init } = options;
  const normalizedPath = path.startsWith('/') ? path : `/${path}`;
  const requestHeaders = new Headers(headers);

  if (init.body && !requestHeaders.has('content-type')) {
    requestHeaders.set('content-type', 'application/json');
  }

  const response = await fetcher(`${baseUrl}${normalizedPath}`, {
    ...init,
    headers: requestHeaders
  });

  if (!response.ok) {
    const details = await response.text();
    const suffix = details ? `: ${details}` : '';
    throw new Error(`Request failed with ${response.status} ${response.statusText}${suffix}`);
  }

  return response.json() as Promise<TResponse>;
};

export const createHomelabdClient = (
  options: HomelabdClientOptions = {}
): HomelabdClient => {
  const { baseUrl = DEFAULT_HOMELABD_API_BASE, fetcher } = options;

  return {
    sendMessage(request: HomelabdMessageRequest) {
      return apiFetch<HomelabdMessageResponse>('/message', {
        baseUrl,
        fetcher,
        method: 'POST',
        body: JSON.stringify(request)
      });
    },
    listTasks() {
      return apiFetch<HomelabdTasksResponse>('/tasks', {
        baseUrl,
        fetcher
      });
    },
    listTaskRuns(taskId: string) {
      return apiFetch<HomelabdTaskRunsResponse>(`/tasks/${encodeURIComponent(taskId)}/runs`, {
        baseUrl,
        fetcher
      });
    },
    cancelTask(taskId: string) {
      return apiFetch<HomelabdTaskActionResponse>(`/tasks/${encodeURIComponent(taskId)}/cancel`, {
        baseUrl,
        fetcher,
        method: 'POST'
      });
    },
    retryTask(taskId: string, request: HomelabdTaskRetryRequest = {}) {
      return apiFetch<HomelabdTaskActionResponse>(`/tasks/${encodeURIComponent(taskId)}/retry`, {
        baseUrl,
        fetcher,
        method: 'POST',
        body: JSON.stringify(request)
      });
    },
    listApprovals() {
      return apiFetch<HomelabdApprovalsResponse>('/approvals', {
        baseUrl,
        fetcher
      });
    },
    listEvents(options: { date?: string; limit?: number } = {}) {
      const { date, limit } = options;
      const params = new URLSearchParams();
      if (date) {
        params.set('date', date);
      }
      if (limit) {
        params.set('limit', String(limit));
      }
      const query = params.toString() ? `?${params.toString()}` : '';
      return apiFetch<HomelabdEventsResponse>(`/events${query}`, {
        baseUrl,
        fetcher
      });
    }
  };
};

export const createSupervisorClient = (options: HomelabdClientOptions = {}) => {
  const { baseUrl = DEFAULT_SUPERVISORD_API_BASE, fetcher } = options;
  return {
    snapshot() {
      return apiFetch<SupervisorSnapshot>('/supervisord', { baseUrl, fetcher });
    },
    restartSelf() {
      return apiFetch<{ reply: string }>('/supervisord/restart', {
        baseUrl,
        fetcher,
        method: 'POST'
      });
    },
    start(name: string) {
      return apiFetch<SupervisorSnapshot>(`/supervisord/apps/${encodeURIComponent(name)}/start`, {
        baseUrl,
        fetcher,
        method: 'POST'
      });
    },
    stop(name: string) {
      return apiFetch<SupervisorSnapshot>(`/supervisord/apps/${encodeURIComponent(name)}/stop`, {
        baseUrl,
        fetcher,
        method: 'POST'
      });
    },
    restart(name: string) {
      return apiFetch<SupervisorSnapshot>(`/supervisord/apps/${encodeURIComponent(name)}/restart`, {
        baseUrl,
        fetcher,
        method: 'POST'
      });
    }
  };
};
