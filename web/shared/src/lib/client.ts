import type {
  AssistantCatalogue,
  AssistantCatalogueOptions,
  FetchClient,
  FetchClientOptions,
  HomelabdApprovalsResponse,
  HomelabdAgentsResponse,
  HomelabdClearChatRequest,
  HomelabdClearChatResponse,
  HomelabdClient,
  HomelabdClientOptions,
  HomelabdCreateTaskRequest,
  HomelabdCreateTaskResponse,
  HomelabdAskKnowledgeSpaceRequest,
  HomelabdAskKnowledgeSpaceResponse,
  HomelabdCreateKnowledgeResearchRunRequest,
  HomelabdCreateKnowledgeResearchRunResponse,
  HomelabdCreateKnowledgeSpaceRequest,
  HomelabdCreateKnowledgeSpaceResponse,
  HomelabdDeleteKnowledgeSourceResponse,
  HomelabdDeleteKnowledgeSpaceResponse,
  HomelabdCreateWorkflowRequest,
  HomelabdAddKnowledgeSourceRequest,
  HomelabdAddKnowledgeSourceResponse,
  HomelabdEventsResponse,
  HomelabdKnowledgeSpace,
  HomelabdKnowledgeSpacesResponse,
  HomelabdMergeQueueMoveRequest,
  HomelabdMessageRequest,
  HomelabdMessageResponse,
  HomelabdQueryKnowledgeSpaceRequest,
  HomelabdQueryKnowledgeSpaceResponse,
  HomelabdResearchKnowledgeSpaceRequest,
  HomelabdResearchKnowledgeSpaceResponse,
  HomelabdResumeKnowledgeResearchRunResponse,
  HomelabdSettingsResponse,
  HomelabdTaskActionResponse,
  HomelabdTaskDiffResponse,
  HomelabdTaskReopenRequest,
  HomelabdTaskRetryRequest,
  HomelabdTaskRunsResponse,
  HomelabdTasksResponse,
  HomelabdUpdateKnowledgeSpaceRequest,
  HomelabdUpdateKnowledgeSpaceResponse,
  HomelabdUpdateSettingsRequest,
  HomelabdWorkflow,
  HomelabdWorkflowActionResponse,
  HomelabdWorkflowsResponse,
  SupervisorSnapshot
} from './types';

export const DEFAULT_HOMELABD_API_BASE = 'http://127.0.0.1:18080';
export const DEFAULT_HEALTHD_API_BASE = 'http://127.0.0.1:18081';
export const DEFAULT_SUPERVISORD_API_BASE = 'http://127.0.0.1:18082';

const DEFAULT_SAFE_REQUEST_RETRIES = 2;
const DEFAULT_RETRY_DELAY_MS = 400;
const MAX_RETRY_DELAY_MS = 5000;
const RETRYABLE_STATUS_CODES = new Set([408, 429, 500, 502, 503, 504]);

class HttpRequestError extends Error {}

const delay = (milliseconds: number) =>
  new Promise((resolve) => setTimeout(resolve, milliseconds));

const requestMethod = (method?: string) => (method || 'GET').toUpperCase();

const isSafeRequestMethod = (method: string) => method === 'GET' || method === 'HEAD';

const isAbortError = (error: unknown) =>
  error instanceof DOMException && (error.name === 'AbortError' || error.name === 'TimeoutError');

const retryDelay = (attempt: number, retryAfter: string | null, baseDelayMs: number) => {
  if (retryAfter) {
    const seconds = Number(retryAfter);
    if (Number.isFinite(seconds) && seconds >= 0) {
      return Math.min(seconds * 1000, MAX_RETRY_DELAY_MS);
    }
    const dateDelay = Date.parse(retryAfter) - Date.now();
    if (Number.isFinite(dateDelay) && dateDelay > 0) {
      return Math.min(dateDelay, MAX_RETRY_DELAY_MS);
    }
  }
  return Math.min(baseDelayMs * 2 ** attempt, MAX_RETRY_DELAY_MS);
};

export const apiFetch: FetchClient = async <TResponse>(
  path: string,
  options: FetchClientOptions = {}
): Promise<TResponse> => {
  const {
    baseUrl = '/api',
    fetcher = fetch,
    headers,
    retries,
    retryDelayMs = DEFAULT_RETRY_DELAY_MS,
    ...init
  } = options;
  const normalizedPath = path.startsWith('/') ? path : `/${path}`;
  const requestHeaders = new Headers(headers);
  const method = requestMethod(init.method);
  const maxRetries = Math.max(
    0,
    retries === undefined && isSafeRequestMethod(method) ? DEFAULT_SAFE_REQUEST_RETRIES : retries || 0
  );

  if (init.body && !requestHeaders.has('content-type')) {
    requestHeaders.set('content-type', 'application/json');
  }

  for (let attempt = 0; attempt <= maxRetries; attempt += 1) {
    try {
      const response = await fetcher(`${baseUrl}${normalizedPath}`, {
        ...init,
        headers: requestHeaders
      });

      if (!response.ok) {
        const details = await response.text();
        const suffix = details ? `: ${details}` : '';
        const message = `Request failed with ${response.status} ${response.statusText}${suffix}`;
        if (
          attempt < maxRetries &&
          RETRYABLE_STATUS_CODES.has(response.status) &&
          isSafeRequestMethod(method)
        ) {
          await delay(retryDelay(attempt, response.headers.get('retry-after'), retryDelayMs));
          continue;
        }
        throw new HttpRequestError(message);
      }

      return response.json() as Promise<TResponse>;
    } catch (error) {
      if (error instanceof HttpRequestError) {
        throw error;
      }
      if (attempt < maxRetries && isSafeRequestMethod(method) && !isAbortError(error)) {
        await delay(retryDelay(attempt, null, retryDelayMs));
        continue;
      }
      throw error;
    }
  }

  throw new Error('Request failed before a response was received.');
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
    getAssistant(options: AssistantCatalogueOptions = {}) {
      const params = new URLSearchParams();
      if (options.search?.trim()) {
        params.set('q', options.search.trim());
      }
      if (options.area?.trim() && options.area !== 'all') {
        params.set('area', options.area.trim());
      }
      const query = params.toString() ? `?${params.toString()}` : '';
      return apiFetch<AssistantCatalogue>(`/assistant${query}`, {
        baseUrl,
        fetcher
      });
    },
    clearChat(request: HomelabdClearChatRequest) {
      return apiFetch<HomelabdClearChatResponse>('/chat/clear', {
        baseUrl,
        fetcher,
        method: 'POST',
        body: JSON.stringify(request)
      });
    },
    createTask(request: HomelabdCreateTaskRequest) {
      return apiFetch<HomelabdCreateTaskResponse>('/tasks', {
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
    getSettings() {
      return apiFetch<HomelabdSettingsResponse>('/settings', {
        baseUrl,
        fetcher
      });
    },
    updateSettings(request: HomelabdUpdateSettingsRequest) {
      return apiFetch<HomelabdSettingsResponse>('/settings', {
        baseUrl,
        fetcher,
        method: 'POST',
        body: JSON.stringify(request)
      });
    },
    createKnowledgeSpace(request: HomelabdCreateKnowledgeSpaceRequest) {
      return apiFetch<HomelabdCreateKnowledgeSpaceResponse>('/knowledge/spaces', {
        baseUrl,
        fetcher,
        method: 'POST',
        body: JSON.stringify(request)
      });
    },
    listKnowledgeSpaces() {
      return apiFetch<HomelabdKnowledgeSpacesResponse>('/knowledge/spaces', {
        baseUrl,
        fetcher
      });
    },
    getKnowledgeSpace(spaceId: string) {
      return apiFetch<HomelabdKnowledgeSpace>(`/knowledge/spaces/${encodeURIComponent(spaceId)}`, {
        baseUrl,
        fetcher
      });
    },
    updateKnowledgeSpace(spaceId: string, request: HomelabdUpdateKnowledgeSpaceRequest) {
      return apiFetch<HomelabdUpdateKnowledgeSpaceResponse>(
        `/knowledge/spaces/${encodeURIComponent(spaceId)}`,
        {
          baseUrl,
          fetcher,
          method: 'PATCH',
          body: JSON.stringify(request)
        }
      );
    },
    deleteKnowledgeSpace(spaceId: string) {
      return apiFetch<HomelabdDeleteKnowledgeSpaceResponse>(
        `/knowledge/spaces/${encodeURIComponent(spaceId)}`,
        {
          baseUrl,
          fetcher,
          method: 'DELETE'
        }
      );
    },
    addKnowledgeSource(spaceId: string, request: HomelabdAddKnowledgeSourceRequest) {
      return apiFetch<HomelabdAddKnowledgeSourceResponse>(
        `/knowledge/spaces/${encodeURIComponent(spaceId)}/sources`,
        {
          baseUrl,
          fetcher,
          method: 'POST',
          body: JSON.stringify(request)
        }
      );
    },
    deleteKnowledgeSource(spaceId: string, sourceId: string) {
      return apiFetch<HomelabdDeleteKnowledgeSourceResponse>(
        `/knowledge/spaces/${encodeURIComponent(spaceId)}/sources/${encodeURIComponent(sourceId)}`,
        {
          baseUrl,
          fetcher,
          method: 'DELETE'
        }
      );
    },
    researchKnowledgeSpace(spaceId: string, request: HomelabdResearchKnowledgeSpaceRequest) {
      return apiFetch<HomelabdResearchKnowledgeSpaceResponse>(
        `/knowledge/spaces/${encodeURIComponent(spaceId)}/research`,
        {
          baseUrl,
          fetcher,
          method: 'POST',
          body: JSON.stringify(request)
        }
      );
    },
    queryKnowledgeSpace(spaceId: string, request: HomelabdQueryKnowledgeSpaceRequest) {
      return apiFetch<HomelabdQueryKnowledgeSpaceResponse>(
        `/knowledge/spaces/${encodeURIComponent(spaceId)}/query`,
        {
          baseUrl,
          fetcher,
          method: 'POST',
          body: JSON.stringify(request)
        }
      );
    },
    askKnowledgeSpace(spaceId: string, request: HomelabdAskKnowledgeSpaceRequest) {
      return apiFetch<HomelabdAskKnowledgeSpaceResponse>(
        `/knowledge/spaces/${encodeURIComponent(spaceId)}/ask`,
        {
          baseUrl,
          fetcher,
          method: 'POST',
          body: JSON.stringify(request)
        }
      );
    },
    createKnowledgeResearchRun(spaceId: string, request: HomelabdCreateKnowledgeResearchRunRequest) {
      return apiFetch<HomelabdCreateKnowledgeResearchRunResponse>(
        `/knowledge/spaces/${encodeURIComponent(spaceId)}/research-runs`,
        {
          baseUrl,
          fetcher,
          method: 'POST',
          body: JSON.stringify(request)
        }
      );
    },
    resumeKnowledgeResearchRun(spaceId: string, runId: string) {
      return apiFetch<HomelabdResumeKnowledgeResearchRunResponse>(
        `/knowledge/spaces/${encodeURIComponent(spaceId)}/research-runs/${encodeURIComponent(runId)}/resume`,
        {
          baseUrl,
          fetcher,
          method: 'POST'
        }
      );
    },
    createWorkflow(request: HomelabdCreateWorkflowRequest) {
      return apiFetch<HomelabdWorkflowActionResponse>('/workflows', {
        baseUrl,
        fetcher,
        method: 'POST',
        body: JSON.stringify(request)
      });
    },
    listWorkflows() {
      return apiFetch<HomelabdWorkflowsResponse>('/workflows', {
        baseUrl,
        fetcher
      });
    },
    getWorkflow(workflowId: string) {
      return apiFetch<HomelabdWorkflow>(`/workflows/${encodeURIComponent(workflowId)}`, {
        baseUrl,
        fetcher
      });
    },
    runWorkflow(workflowId: string) {
      return apiFetch<HomelabdWorkflowActionResponse>(
        `/workflows/${encodeURIComponent(workflowId)}/run`,
        {
          baseUrl,
          fetcher,
          method: 'POST'
        }
      );
    },
    listAgents() {
      return apiFetch<HomelabdAgentsResponse>('/agents', {
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
    getTaskDiff(taskId: string) {
      return apiFetch<HomelabdTaskDiffResponse>(`/tasks/${encodeURIComponent(taskId)}/diff`, {
        baseUrl,
        fetcher
      });
    },
    runTask(taskId: string) {
      return apiFetch<HomelabdTaskActionResponse>(`/tasks/${encodeURIComponent(taskId)}/run`, {
        baseUrl,
        fetcher,
        method: 'POST'
      });
    },
    reviewTask(taskId: string) {
      return apiFetch<HomelabdTaskActionResponse>(`/tasks/${encodeURIComponent(taskId)}/review`, {
        baseUrl,
        fetcher,
        method: 'POST'
      });
    },
    acceptTask(taskId: string) {
      return apiFetch<HomelabdTaskActionResponse>(`/tasks/${encodeURIComponent(taskId)}/accept`, {
        baseUrl,
        fetcher,
        method: 'POST'
      });
    },
    restartTask(taskId: string) {
      return apiFetch<HomelabdTaskActionResponse>(`/tasks/${encodeURIComponent(taskId)}/restart`, {
        baseUrl,
        fetcher,
        method: 'POST'
      });
    },
    reopenTask(taskId: string, request: HomelabdTaskReopenRequest = {}) {
      return apiFetch<HomelabdTaskActionResponse>(`/tasks/${encodeURIComponent(taskId)}/reopen`, {
        baseUrl,
        fetcher,
        method: 'POST',
        body: JSON.stringify(request)
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
    moveTaskInMergeQueue(taskId: string, request: HomelabdMergeQueueMoveRequest) {
      return apiFetch<HomelabdTaskActionResponse>(
        `/tasks/${encodeURIComponent(taskId)}/merge-queue`,
        {
          baseUrl,
          fetcher,
          method: 'POST',
          body: JSON.stringify(request)
        }
      );
    },
    deleteTask(taskId: string) {
      return apiFetch<HomelabdTaskActionResponse>(`/tasks/${encodeURIComponent(taskId)}/delete`, {
        baseUrl,
        fetcher,
        method: 'POST'
      });
    },
    approveApproval(approvalId: string) {
      return apiFetch<HomelabdTaskActionResponse>(`/approvals/${encodeURIComponent(approvalId)}/approve`, {
        baseUrl,
        fetcher,
        method: 'POST'
      });
    },
    denyApproval(approvalId: string) {
      return apiFetch<HomelabdTaskActionResponse>(`/approvals/${encodeURIComponent(approvalId)}/deny`, {
        baseUrl,
        fetcher,
        method: 'POST'
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
