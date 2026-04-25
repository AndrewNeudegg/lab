import type {
  FetchClient,
  FetchClientOptions,
  HomelabdApprovalsResponse,
  HomelabdClient,
  HomelabdClientOptions,
  HomelabdEventsResponse,
  HomelabdMessageRequest,
  HomelabdMessageResponse,
  HomelabdTasksResponse
} from './types';

export const DEFAULT_HOMELABD_API_BASE = 'http://127.0.0.1:18080';

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
