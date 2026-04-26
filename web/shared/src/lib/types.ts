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
  started_at?: string;
  stopped_at?: string;
  due_at?: string;
  parent_id?: string;
  context_ids?: string[];
  workspace?: string;
  result?: string;
  plan?: HomelabdTaskPlan;
}

export interface HomelabdTaskPlan {
  status: string;
  summary: string;
  steps: HomelabdTaskPlanStep[];
  risks?: string[];
  review?: string;
  created_at: string;
  reviewed_at?: string;
}

export interface HomelabdTaskPlanStep {
  title: string;
  detail?: string;
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

export interface HealthdSample {
  time: string;
  good: boolean;
  cpu_usage_percent: number;
  memory_usage_percent: number;
  memory_used_bytes: number;
  memory_total_bytes: number;
  load1: number;
  load5: number;
  load15: number;
  system_uptime_seconds: number;
  process_uptime_seconds: number;
  goroutines: number;
}

export interface HealthdCheckResult {
  name: string;
  type: string;
  status: 'healthy' | 'warning' | 'critical' | string;
  message: string;
  latency_ms: number;
  last_checked: string;
}

export interface HealthdProcessStatus {
  name: string;
  type: string;
  status: 'healthy' | 'warning' | 'critical' | string;
  message: string;
  pid?: number;
  addr?: string;
  started_at?: string;
  last_seen: string;
  ttl_seconds: number;
  metadata?: Record<string, string>;
}

export interface HealthdSLOReport {
  name: string;
  target_percent: number;
  window_seconds: number;
  good_events: number;
  total_events: number;
  sli_percent: number;
  error_budget_remaining_percent: number;
  burn_rate: number;
  status: 'healthy' | 'warning' | 'critical' | string;
  violations?: string[];
}

export interface HealthdNotification {
  id: string;
  time: string;
  severity: 'info' | 'warn' | 'page' | string;
  status: 'firing' | 'resolved' | string;
  source: string;
  message: string;
  delivered?: string[];
}

export interface HealthdSnapshot {
  status: 'healthy' | 'warning' | 'critical' | string;
  started_at: string;
  uptime_seconds: number;
  window_seconds: number;
  current: HealthdSample;
  samples: HealthdSample[];
  checks: HealthdCheckResult[];
  processes: HealthdProcessStatus[];
  slos: HealthdSLOReport[];
  notifications: HealthdNotification[];
}

export type SupervisorAppState =
  | 'stopped'
  | 'starting'
  | 'running'
  | 'stopping'
  | 'failed'
  | string;

export interface SupervisorAppStatus {
  name: string;
  type: string;
  state: SupervisorAppState;
  desired: string;
  pid?: number;
  restarts: number;
  exit_code?: number;
  message: string;
  started_at?: string;
  stopped_at?: string;
  updated_at: string;
  start_order: number;
  restart: string;
  health_url?: string;
  last_health?: string;
  last_output?: string;
  working_dir?: string;
  command: string;
  args?: string[];
  environment?: Record<string, string>;
}

export interface SupervisorSnapshot {
  status: 'healthy' | 'warning' | 'critical' | string;
  started_at: string;
  restartable: boolean;
  restart_hint?: string;
  restart_after?: string;
  apps: SupervisorAppStatus[];
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
