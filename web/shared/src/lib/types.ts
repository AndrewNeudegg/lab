export interface User {
  id: string;
  name: string;
}

export interface FetchClientOptions extends RequestInit {
  baseUrl?: string;
  fetcher?: typeof fetch;
}

export type FetchClient = <TResponse>(
  path: string,
  options?: FetchClientOptions
) => Promise<TResponse>;

export type ChatRole = 'user' | 'assistant';

export interface ChatTranscriptMessage {
  id: string;
  role: ChatRole;
  content: string;
  time: string;
  source?: string;
  actions?: string[];
}

export type QuickAction = 'help' | 'status' | 'tasks' | 'agents' | 'approvals';

export interface HomelabdMessageRequest {
  from?: string;
  content: string;
}

export interface HomelabdMessageResponse {
  reply: string;
  source?: string;
}

export type TaskStatus =
  | 'queued'
  | 'running'
  | 'blocked'
  | 'ready_for_review'
  | 'awaiting_approval'
  | 'awaiting_verification'
  | 'done'
  | 'failed'
  | 'cancelled';

export interface HomelabdTask {
  id: string;
  title: string;
  goal: string;
  status: TaskStatus | string;
  assigned_to: string;
  priority: number;
  created_at: string;
  updated_at: string;
  due_at?: string;
  parent_id?: string;
  context_ids?: string[];
  workspace?: string;
  result?: string;
}

export interface HomelabdTasksResponse {
  tasks: HomelabdTask[];
}

export type ApprovalStatus = 'pending' | 'granted' | 'denied' | string;

export interface HomelabdApproval {
  id: string;
  task_id?: string;
  tool: string;
  args?: unknown;
  reason: string;
  status: ApprovalStatus;
  created_at: string;
  updated_at: string;
}

export interface HomelabdApprovalsResponse {
  approvals: HomelabdApproval[];
}

export interface HomelabdEvent {
  id: string;
  time: string;
  type: string;
  actor: string;
  task_id?: string;
  parent_id?: string;
  payload?: unknown;
}

export interface HomelabdEventsResponse {
  events: HomelabdEvent[];
}

export interface HomelabdClient {
  sendMessage(request: HomelabdMessageRequest): Promise<HomelabdMessageResponse>;
  listTasks(): Promise<HomelabdTasksResponse>;
  listApprovals(): Promise<HomelabdApprovalsResponse>;
  listEvents(options?: { date?: string; limit?: number }): Promise<HomelabdEventsResponse>;
}

export interface HomelabdClientOptions {
  baseUrl?: string;
  fetcher?: typeof fetch;
}
