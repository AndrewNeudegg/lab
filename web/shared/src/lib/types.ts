export interface User {
  id: string;
  name: string;
}

export interface FetchClientOptions extends RequestInit {
  baseUrl?: string;
  fetcher?: typeof fetch;
  retries?: number;
  retryDelayMs?: number;
}

export type FetchClient = <TResponse>(
  path: string,
  options?: FetchClientOptions
) => Promise<TResponse>;

export type ChatRole = 'user' | 'assistant';
export type ChatDeliveryStatus = 'failed';

export interface ChatTranscriptMessage {
  id: string;
  role: ChatRole;
  content: string;
  time: string;
  source?: string;
  actions?: string[];
  attachments?: HomelabdTaskAttachment[];
  delivery_status?: ChatDeliveryStatus;
  delivery_error?: string;
}

export type QuickAction = 'help' | 'status' | 'tasks' | 'agents' | 'approvals';

export interface HomelabdMessageRequest {
  from?: string;
  content: string;
  attachments?: HomelabdTaskAttachment[];
}

export interface HomelabdMessageResponse {
  reply: string;
  source?: string;
}

export type TaskStatus =
  | 'queued'
  | 'running'
  | 'blocked'
  | 'conflict_resolution'
  | 'ready_for_review'
  | 'awaiting_approval'
  | 'awaiting_restart'
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
  depends_on?: string[];
  blocked_by?: string[];
  graph_phase?: string;
  target?: HomelabdTaskTarget;
  acceptance_criteria?: HomelabdAcceptanceCriterion[];
  attachments?: HomelabdTaskAttachment[];
  restart_required?: string[];
  restart_completed?: string[];
  restart_status?: 'pending' | 'running' | 'complete' | 'failed' | string;
  restart_current?: string;
  restart_last_error?: string;
  auto_recovery_attempts?: number;
  auto_recovery_last_at?: string;
  workspace?: string;
  result?: string;
  plan?: HomelabdTaskPlan;
}

export interface HomelabdTaskAttachment {
  id?: string;
  name: string;
  content_type: string;
  size: number;
  data_url?: string;
  text?: string;
  created_at?: string;
}

export interface HomelabdTaskTarget {
  mode?: string;
  agent_id?: string;
  machine?: string;
  workdir_id?: string;
  workdir?: string;
  backend?: string;
}

export interface HomelabdAcceptanceCriterion {
  id: string;
  description: string;
  status: string;
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

export interface HomelabdCreateTaskRequest {
  goal: string;
  target?: HomelabdTaskTarget;
  attachments?: HomelabdTaskAttachment[];
}

export interface HomelabdCreateTaskResponse {
  reply: string;
}

export interface HomelabdRemoteAgentWorkdir {
  id: string;
  path: string;
  label?: string;
}

export interface HomelabdRemoteAgent {
  id: string;
  name: string;
  machine: string;
  version?: string;
  status: 'online' | 'offline' | string;
  last_seen: string;
  started_at?: string;
  capabilities?: string[];
  workdirs?: HomelabdRemoteAgentWorkdir[];
  current_task_id?: string;
  metadata?: Record<string, string>;
}

export interface HomelabdAgentsResponse {
  agents: HomelabdRemoteAgent[];
}

export interface HomelabdTaskActionResponse {
  reply: string;
}

export type WorkflowStatus =
  | 'draft'
  | 'running'
  | 'waiting'
  | 'awaiting_approval'
  | 'completed'
  | 'failed'
  | 'cancelled';

export type WorkflowStepKind = 'llm' | 'tool' | 'workflow' | 'wait' | string;

export interface HomelabdWorkflow {
  id: string;
  name: string;
  description?: string;
  goal?: string;
  status: WorkflowStatus | string;
  steps: HomelabdWorkflowStep[];
  estimate: HomelabdWorkflowEstimate;
  created_by?: string;
  created_at: string;
  updated_at: string;
  last_run?: HomelabdWorkflowRun;
}

export interface HomelabdWorkflowStep {
  id?: string;
  name: string;
  kind: WorkflowStepKind;
  prompt?: string;
  tool?: string;
  args?: unknown;
  workflow_id?: string;
  condition?: string;
  timeout_seconds?: number;
  depends_on?: string[];
}

export interface HomelabdWorkflowEstimate {
  steps: number;
  estimated_llm_calls: number;
  estimated_tool_calls: number;
  workflow_calls: number;
  waits: number;
  estimated_seconds: number;
  estimated_minutes: number;
  summary: string;
}

export interface HomelabdWorkflowRun {
  id: string;
  workflow_id: string;
  status: WorkflowStatus | string;
  current_step: number;
  started_at: string;
  finished_at?: string;
  outputs?: HomelabdWorkflowStepOutput[];
  error?: string;
}

export interface HomelabdWorkflowStepOutput {
  step_id: string;
  step_name: string;
  kind: WorkflowStepKind;
  status: WorkflowStatus | string;
  summary?: string;
  tool?: string;
  result?: unknown;
  error?: string;
  started_at: string;
  finished_at?: string;
}

export interface HomelabdWorkflowsResponse {
  workflows: HomelabdWorkflow[];
}

export interface HomelabdCreateWorkflowRequest {
  name: string;
  description?: string;
  goal?: string;
  steps?: HomelabdWorkflowStep[];
}

export interface HomelabdWorkflowActionResponse {
  reply: string;
  workflow: HomelabdWorkflow;
}

export interface HomelabdTaskRetryRequest {
  backend?: string;
  instruction?: string;
}

export interface HomelabdTaskReopenRequest {
  reason?: string;
}

export interface HomelabdRunArtifact {
  id: string;
  kind: string;
  path?: string;
  task_id: string;
  backend: string;
  workspace: string;
  status: string;
  command?: string[];
  output?: string;
  error?: string;
  duration?: number;
  started_at?: string;
  finished_at?: string;
  time?: string;
}

export interface HomelabdTaskRunsResponse {
  runs: HomelabdRunArtifact[];
}

export interface HomelabdTaskDiffSummary {
  files: number;
  additions: number;
  deletions: number;
}

export interface HomelabdTaskDiffFile {
  path: string;
  old_path?: string;
  status: string;
  additions: number;
  deletions: number;
  binary?: boolean;
}

export interface HomelabdTaskDiffResponse {
  task_id: string;
  base_ref?: string;
  base_label?: string;
  head_ref?: string;
  head_label?: string;
  workspace?: string;
  raw_diff: string;
  summary: HomelabdTaskDiffSummary;
  files: HomelabdTaskDiffFile[];
  generated_at: string;
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
  createTask(request: HomelabdCreateTaskRequest): Promise<HomelabdCreateTaskResponse>;
  listTasks(): Promise<HomelabdTasksResponse>;
  createWorkflow(request: HomelabdCreateWorkflowRequest): Promise<HomelabdWorkflowActionResponse>;
  listWorkflows(): Promise<HomelabdWorkflowsResponse>;
  getWorkflow(workflowId: string): Promise<HomelabdWorkflow>;
  runWorkflow(workflowId: string): Promise<HomelabdWorkflowActionResponse>;
  listAgents(): Promise<HomelabdAgentsResponse>;
  listApprovals(): Promise<HomelabdApprovalsResponse>;
  listEvents(options?: { date?: string; limit?: number }): Promise<HomelabdEventsResponse>;
  listTaskRuns(taskId: string): Promise<HomelabdTaskRunsResponse>;
  getTaskDiff(taskId: string): Promise<HomelabdTaskDiffResponse>;
  runTask(taskId: string): Promise<HomelabdTaskActionResponse>;
  reviewTask(taskId: string): Promise<HomelabdTaskActionResponse>;
  acceptTask(taskId: string): Promise<HomelabdTaskActionResponse>;
  restartTask(taskId: string): Promise<HomelabdTaskActionResponse>;
  reopenTask(
    taskId: string,
    request?: HomelabdTaskReopenRequest
  ): Promise<HomelabdTaskActionResponse>;
  cancelTask(taskId: string): Promise<HomelabdTaskActionResponse>;
  retryTask(
    taskId: string,
    request?: HomelabdTaskRetryRequest
  ): Promise<HomelabdTaskActionResponse>;
  deleteTask(taskId: string): Promise<HomelabdTaskActionResponse>;
  approveApproval(approvalId: string): Promise<HomelabdTaskActionResponse>;
  denyApproval(approvalId: string): Promise<HomelabdTaskActionResponse>;
}

export interface HomelabdClientOptions {
  baseUrl?: string;
  fetcher?: typeof fetch;
}
