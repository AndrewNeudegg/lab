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
  slos: HealthdSLOReport[];
  notifications: HealthdNotification[];
}

export interface HomelabdClient {
  sendMessage(request: HomelabdMessageRequest): Promise<HomelabdMessageResponse>;
  getHealthdSnapshot(window?: string): Promise<HealthdSnapshot>;
  runHealthdChecks(): Promise<HealthdSnapshot>;
}

export interface HomelabdClientOptions {
  baseUrl?: string;
  fetcher?: typeof fetch;
}
