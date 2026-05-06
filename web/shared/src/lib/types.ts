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
  buttons?: string[];
  stats?: ChatInteractionStats;
  attachments?: HomelabdTaskAttachment[];
  delivery_status?: ChatDeliveryStatus;
  delivery_error?: string;
}

export interface ChatInteractionStats {
  model_turns?: number;
  tool_calls?: number;
  input_tokens?: number;
  output_tokens?: number;
  total_tokens?: number;
  elapsed_ms?: number;
}

export type QuickAction = 'help' | 'status' | 'tasks' | 'agents' | 'approvals';

export interface HomelabdMessageRequest {
  from?: string;
  content: string;
  conversation_id?: string;
  attachments?: HomelabdTaskAttachment[];
}

export interface HomelabdMessageResponse {
  id?: string;
  reply: string;
  source?: string;
  buttons?: string[];
  stats?: ChatInteractionStats;
}

export interface HomelabdClearChatRequest {
  conversation_id?: string;
  all?: boolean;
}

export interface HomelabdClearChatResponse {
  reply: string;
  conversation_id?: string;
  all?: boolean;
  removed_events?: number;
  removed_log_entries?: number;
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
  | 'no_change_required'
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
  merge_queue_position?: number;
  merge_queue_entered_at?: string;
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
  ui_ux_brief?: HomelabdUIUXBrief;
  created_at: string;
  reviewed_at?: string;
}

export interface HomelabdTaskPlanStep {
  title: string;
  detail?: string;
}

export interface HomelabdUIUXBrief {
  operator_goal: string;
  primary_workflow: string;
  surfaces: string[];
  existing_pattern: string;
  desktop_layout: string;
  mobile_layout: string;
  states: string[];
  accessibility: string[];
  validation: string[];
}

export interface HomelabdTasksResponse {
  tasks: HomelabdTask[];
}

export interface HomelabdRuntimeSettings {
  auto_merge_enabled: boolean;
}

export interface HomelabdSettingsResponse {
  settings: HomelabdRuntimeSettings;
}

export interface HomelabdUpdateSettingsRequest {
  auto_merge_enabled?: boolean;
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

export type AssistantAutonomy = 'observe' | 'plan' | 'confirm' | 'automatic' | string;

export interface AssistantFilterOption {
  value: string;
  label: string;
  count: number;
}

export interface AssistantFilters {
  areas: AssistantFilterOption[];
}

export interface AssistantPrinciple {
  name: string;
  summary: string;
}

export interface AssistantActivity {
  id: string;
  name: string;
  area: string;
  cadence: string;
  description: string;
  outcome: string;
  capability_ids: string[];
  search_terms?: string[];
}

export interface AssistantActionLink {
  label: string;
  href: string;
  surface: string;
}

export interface AssistantWorkflowTemplate {
  name: string;
  description?: string;
  goal: string;
  steps: HomelabdWorkflowStep[];
}

export interface AssistantCapability {
  id: string;
  name: string;
  area: string;
  summary: string;
  promise: string;
  cadence: string;
  autonomy: AssistantAutonomy;
  inputs: string[];
  outputs: string[];
  surfaces: AssistantActionLink[];
  ux_pattern_ids: string[];
  safeguards: string[];
  workflow_template: AssistantWorkflowTemplate;
  search_terms?: string[];
}

export interface AssistantUXPattern {
  id: string;
  name: string;
  summary: string;
  applies_to: string;
  implementation: string;
}

export interface AssistantResearchSource {
  title: string;
  url: string;
  summary: string;
}

export interface AssistantCatalogue {
  name: string;
  summary: string;
  updated_at: string;
  principles: AssistantPrinciple[];
  activities: AssistantActivity[];
  capabilities: AssistantCapability[];
  ux_patterns: AssistantUXPattern[];
  research_sources: AssistantResearchSource[];
  filters: AssistantFilters;
}

export interface AssistantCatalogueOptions {
  search?: string;
  area?: string;
}

export type AssistantRunStatus = 'running' | 'completed' | 'failed' | string;
export type AssistantRunDecision = 'no_op' | 'recommend' | 'created_tasks' | string;

export interface AssistantRunRequest {
  trigger_kind?: string;
  trigger_label?: string;
  goal?: string;
  autonomy?: string;
}

export interface AssistantRunTrigger {
  kind: string;
  label: string;
}

export interface AssistantRunFinding {
  title: string;
  detail?: string;
  severity?: string;
  surface?: string;
  object_id?: string;
  object_url?: string;
}

export interface AssistantRunAction {
  id: string;
  fingerprint?: string;
  kind: string;
  title: string;
  rationale: string;
  priority?: string;
  risk?: string;
  target_surface?: string;
  task_goal?: string;
  knowledge_query?: string;
  workflow_hint?: string;
  status?: string;
  created_task_id?: string;
  seen_count?: number;
  useful_count?: number;
  snoozed_until?: string;
}

export interface AssistantRunReceipt {
  kind: string;
  message: string;
  object_id?: string;
  object_url?: string;
  created_at: string;
}

export interface AssistantRunObjectRef {
  id: string;
  title: string;
  status?: string;
  summary?: string;
  url?: string;
}

export interface AssistantRunSystemSnapshot {
  status?: string;
  error?: string;
  items?: AssistantRunObjectRef[];
}

export interface AssistantRunEventRef {
  id: string;
  type: string;
  actor?: string;
  task_id?: string;
  summary?: string;
  time: string;
}

export interface AssistantRunSignal {
  id: string;
  fingerprint: string;
  kind: string;
  title: string;
  detail?: string;
  why_now?: string;
  severity?: string;
  surface?: string;
  object_id?: string;
  object_url?: string;
  score: number;
  confidence?: string;
  priority?: string;
  action_kind?: string;
  rationale?: string;
  task_goal?: string;
  evidence?: AssistantRunSignalEvidence[];
  safe_actions?: string[];
  suggested_next_step?: string;
  suppressed?: boolean;
  suppression_reason?: string;
  seen_count?: number;
  useful_count?: number;
  created_task_id?: string;
  snoozed_until?: string;
}

export interface AssistantSignalSubmitRequest {
  fingerprint?: string;
  source?: string;
  kind?: string;
  title: string;
  detail?: string;
  why_now?: string;
  severity?: string;
  surface?: string;
  object_id?: string;
  object_url?: string;
  score?: number;
  confidence?: string;
  priority?: string;
  action_kind?: string;
  rationale?: string;
  task_goal?: string;
  evidence?: AssistantRunSignalEvidence[];
  safe_actions?: string[];
  suggested_next_step?: string;
  observed_at?: string;
  expires_at?: string;
  ttl_seconds?: number;
}

export interface AssistantSignalCandidate extends AssistantRunSignal {
  source?: string;
  first_observed_at?: string;
  last_observed_at?: string;
  expires_at?: string;
  created_at?: string;
  updated_at?: string;
}

export interface AssistantSignalsResponse {
  signals: AssistantSignalCandidate[];
}

export interface AssistantSignalResponse {
  signal: AssistantSignalCandidate;
}

export interface AssistantRunSignalEvidence {
  source?: string;
  kind?: string;
  title: string;
  detail?: string;
  object_id?: string;
  object_url?: string;
  observed_at?: string;
  weight?: number;
}

export interface AssistantRunSnapshot {
  generated_at: string;
  signals?: AssistantRunSignal[];
  task_counts?: Record<string, number>;
  attention_tasks?: AssistantRunObjectRef[];
  pending_approvals?: number;
  workflow_counts?: Record<string, number>;
  recent_workflows?: AssistantRunObjectRef[];
  knowledge_spaces?: AssistantRunObjectRef[];
  remote_agent_counts?: Record<string, number>;
  health?: AssistantRunSystemSnapshot;
  supervisor?: AssistantRunSystemSnapshot;
  recent_events?: AssistantRunEventRef[];
}

export interface AssistantRunUsage {
  input_tokens?: number;
  output_tokens?: number;
  total_tokens?: number;
}

export interface AssistantRun {
  id: string;
  status: AssistantRunStatus;
  decision: AssistantRunDecision;
  trigger: AssistantRunTrigger;
  autonomy: string;
  goal?: string;
  summary: string;
  changed?: string[];
  concerns?: AssistantRunFinding[];
  opportunities?: AssistantRunFinding[];
  recommended_actions?: AssistantRunAction[];
  receipts?: AssistantRunReceipt[];
  snapshot: AssistantRunSnapshot;
  error?: string;
  provider?: string;
  model?: string;
  usage?: AssistantRunUsage;
  created_at: string;
  started_at?: string;
  finished_at?: string;
  updated_at: string;
}

export interface AssistantRunsResponse {
  runs: AssistantRun[];
}

export interface AssistantRunActionResponse {
  reply: string;
  run: AssistantRun;
}

export interface AssistantRunActionUpdateRequest {
  feedback: 'useful' | 'dismiss' | 'snooze' | 'create_task' | string;
  snooze_seconds?: number;
}

export interface HomelabdKnowledgeSpace {
  id: string;
  title: string;
  description?: string;
  objective?: string;
  sources?: HomelabdKnowledgeSource[];
  reports?: HomelabdKnowledgeReport[];
  research_runs?: HomelabdKnowledgeResearchRun[];
  insight: HomelabdKnowledgeInsight;
  created_by?: string;
  created_at: string;
  updated_at: string;
}

export interface HomelabdKnowledgeInsight {
  source_count: number;
  word_count: number;
  key_terms?: string[];
  suggested_questions?: string[];
  updated_at?: string;
}

export interface HomelabdKnowledgeSource {
  id: string;
  title: string;
  kind: 'text' | 'url' | 'file' | 'note' | 'email' | 'mcp' | string;
  uri?: string;
  content: string;
  summary: string;
  key_terms?: string[];
  questions?: string[];
  claims?: HomelabdKnowledgeSourceClaim[];
  entities?: HomelabdKnowledgeSourceEntity[];
  reliability_notes?: string[];
  word_count: number;
  provenance?: HomelabdKnowledgeSourceProvenance;
  ingestion?: HomelabdKnowledgeSourceIngestion;
  sections?: HomelabdKnowledgeSourceSection[];
  chunks?: HomelabdKnowledgeSourceChunk[];
  created_at: string;
  updated_at: string;
}

export interface HomelabdKnowledgeSourceClaim {
  id: string;
  text: string;
  importance?: string;
}

export interface HomelabdKnowledgeSourceEntity {
  name: string;
  type?: string;
  description?: string;
}

export interface HomelabdKnowledgeSourceProvenance {
  uri?: string;
  canonical_uri?: string;
  content_type?: string;
  content_hash?: string;
  byte_count?: number;
  snapshot_path?: string;
  fetched_at?: string;
  extractor?: string;
}

export interface HomelabdKnowledgeSourceIngestion {
  state?: 'ready' | 'failed' | 'processing' | string;
  stage?: string;
  message?: string;
  error?: string;
  started_at?: string;
  completed_at?: string;
}

export interface HomelabdKnowledgeSourceChunk {
  id: string;
  source_id: string;
  source_title: string;
  section_id?: string;
  section_title?: string;
  index: number;
  citation_label: string;
  text: string;
  terms?: string[];
  semantic_terms?: string[];
  word_count: number;
}

export interface HomelabdKnowledgeSourceSection {
  id: string;
  source_id: string;
  source_title: string;
  index: number;
  heading: string;
  text: string;
  terms?: string[];
  word_count: number;
}

export interface HomelabdKnowledgeReport {
  id: string;
  run_id?: string;
  question: string;
  mode: 'research' | 'brief' | 'study' | 'ask' | string;
  answer: string;
  key_findings?: string[];
  evidence?: HomelabdKnowledgeEvidence[];
  gaps?: string[];
  provider?: string;
  model?: string;
  usage?: HomelabdKnowledgeTokenUsage;
  created_at: string;
}

export interface HomelabdKnowledgeEvidence {
  id: string;
  source_id: string;
  source_title: string;
  source_kind?: string;
  source_uri?: string;
  chunk_id?: string;
  section_id?: string;
  section_title?: string;
  citation_label: string;
  excerpt: string;
  terms?: string[];
  source_summary?: string;
  retrieval?: string;
  lexical_score?: number;
  semantic_score?: number;
  score: number;
}

export interface HomelabdKnowledgeQueryResult {
  query: string;
  terms?: string[];
  evidence: HomelabdKnowledgeEvidence[];
  created_at: string;
}

export interface HomelabdKnowledgeAskResult {
  question: string;
  answer: string;
  key_findings?: string[];
  evidence?: HomelabdKnowledgeEvidence[];
  gaps?: string[];
  provider?: string;
  model?: string;
  usage?: HomelabdKnowledgeTokenUsage;
  created_at: string;
}

export interface HomelabdKnowledgeResearchRun {
  id: string;
  objective: string;
  scope?: string;
  depth: 'quick' | 'standard' | 'deep' | string;
  status:
    | 'queued'
    | 'planning'
    | 'discovering'
    | 'retrieving'
    | 'reading'
    | 'synthesizing'
    | 'reviewing'
    | 'completed'
    | 'failed'
    | 'cancelled'
    | string;
  question?: string;
  mode: 'research' | 'brief' | 'study' | string;
  plan?: HomelabdKnowledgeResearchPlan;
  discover_sources?: boolean;
  source_candidates?: HomelabdKnowledgeSourceCandidate[];
  research_loops?: HomelabdKnowledgeResearchLoop[];
  coverage?: HomelabdKnowledgeResearchCoverage[];
  source_ids?: string[];
  report_id?: string;
  sources_examined?: number;
  evidence_count?: number;
  provider?: string;
  model?: string;
  usage?: HomelabdKnowledgeTokenUsage;
  workspace_path?: string;
  error?: string;
  stop_reason?: string;
  events?: HomelabdKnowledgeResearchRunEvent[];
  created_at: string;
  updated_at: string;
  started_at?: string;
  finished_at?: string;
}

export interface HomelabdKnowledgeResearchLoop {
  id: string;
  index: number;
  query: string;
  queries?: string[];
  status: string;
  decision?: string;
  stop_reason?: string;
  candidate_ids?: string[];
  source_ids?: string[];
  accepted_count?: number;
  rejected_count?: number;
  failed_count?: number;
  evidence_count?: number;
  coverage?: string[];
  supported_claims?: string[];
  gaps?: string[];
  follow_up_queries?: string[];
  usage?: HomelabdKnowledgeTokenUsage;
  started_at?: string;
  finished_at?: string;
}

export interface HomelabdKnowledgeSourceCandidate {
  id: string;
  query?: string;
  kind?: string;
  provider?: string;
  title: string;
  url?: string;
  domain?: string;
  snippet?: string;
  content_type?: string;
  fetched?: boolean;
  extraction_state?: string;
  extraction_message?: string;
  word_count?: number;
  usefulness?: string;
  relevance_score?: number;
  coverage?: string[];
  source_id?: string;
  status: string;
  error?: string;
}

export interface HomelabdKnowledgeResearchCoverage {
  id: string;
  topic: string;
  status: string;
  source_ids?: string[];
  evidence_count?: number;
  notes?: string;
}

export interface HomelabdKnowledgeResearchPlan {
  rewritten_objective?: string;
  clarifying_questions?: string[];
  search_queries?: string[];
  steps?: string[];
  expected_outputs?: string[];
}

export interface HomelabdKnowledgeResearchRunEvent {
  id: string;
  stage: string;
  message: string;
  created_at: string;
}

export interface HomelabdKnowledgeTokenUsage {
  input_tokens?: number;
  output_tokens?: number;
  total_tokens?: number;
}

export interface HomelabdKnowledgeSpacesResponse {
  spaces: HomelabdKnowledgeSpace[];
}

export interface HomelabdCreateKnowledgeSpaceRequest {
  title: string;
  description?: string;
  objective?: string;
}

export interface HomelabdCreateKnowledgeSpaceResponse {
  space: HomelabdKnowledgeSpace;
  reply: string;
}

export interface HomelabdUpdateKnowledgeSpaceRequest {
  title?: string;
  description?: string;
  objective?: string;
}

export interface HomelabdUpdateKnowledgeSpaceResponse {
  space: HomelabdKnowledgeSpace;
  reply: string;
}

export interface HomelabdDeleteKnowledgeSpaceResponse {
  space_id: string;
  reply: string;
}

export interface HomelabdAddKnowledgeSourceRequest {
  title: string;
  kind?: string;
  uri?: string;
  content?: string;
}

export interface HomelabdAddKnowledgeSourceResponse {
  space: HomelabdKnowledgeSpace;
  source: HomelabdKnowledgeSource;
  reply: string;
}

export interface HomelabdDeleteKnowledgeSourceResponse {
  space: HomelabdKnowledgeSpace;
  source_id: string;
  reply: string;
}

export interface HomelabdResearchKnowledgeSpaceRequest {
  question: string;
  mode?: 'research' | 'brief' | 'study' | string;
  source_ids?: string[];
}

export interface HomelabdResearchKnowledgeSpaceResponse {
  space: HomelabdKnowledgeSpace;
  report: HomelabdKnowledgeReport;
  reply: string;
}

export interface HomelabdQueryKnowledgeSpaceRequest {
  query: string;
  source_ids?: string[];
  limit?: number;
}

export interface HomelabdQueryKnowledgeSpaceResponse {
  result: HomelabdKnowledgeQueryResult;
  reply: string;
}

export interface HomelabdAskKnowledgeSpaceRequest {
  question: string;
  source_ids?: string[];
  limit?: number;
}

export interface HomelabdAskKnowledgeSpaceResponse {
  space: HomelabdKnowledgeSpace;
  result: HomelabdKnowledgeAskResult;
  report: HomelabdKnowledgeReport;
  reply: string;
}

export interface HomelabdCreateKnowledgeResearchRunRequest {
  objective: string;
  scope?: string;
  depth?: 'quick' | 'standard' | 'deep' | string;
  question?: string;
  mode?: 'research' | 'brief' | 'study' | string;
  source_ids?: string[];
  discover_sources?: boolean;
}

export interface HomelabdCreateKnowledgeResearchRunResponse {
  space: HomelabdKnowledgeSpace;
  run: HomelabdKnowledgeResearchRun;
  report?: HomelabdKnowledgeReport;
  reply: string;
}

export interface HomelabdResumeKnowledgeResearchRunResponse {
  space: HomelabdKnowledgeSpace;
  run: HomelabdKnowledgeResearchRun;
  report?: HomelabdKnowledgeReport;
  reply: string;
}

export interface HomelabdTaskRetryRequest {
  backend?: string;
  instruction?: string;
}

export interface HomelabdMergeQueueMoveRequest {
  direction: 'up' | 'down';
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
  getAssistant(options?: AssistantCatalogueOptions): Promise<AssistantCatalogue>;
  listAssistantRuns(): Promise<AssistantRunsResponse>;
  getAssistantRun(runId: string): Promise<AssistantRun>;
  startAssistantRun(request?: AssistantRunRequest): Promise<AssistantRunActionResponse>;
  listAssistantSignals(): Promise<AssistantSignalsResponse>;
  submitAssistantSignal(request: AssistantSignalSubmitRequest): Promise<AssistantSignalResponse>;
  updateAssistantRunAction(
    runId: string,
    actionId: string,
    request: AssistantRunActionUpdateRequest
  ): Promise<AssistantRunActionResponse>;
  clearChat(request: HomelabdClearChatRequest): Promise<HomelabdClearChatResponse>;
  createTask(request: HomelabdCreateTaskRequest): Promise<HomelabdCreateTaskResponse>;
  listTasks(): Promise<HomelabdTasksResponse>;
  getSettings(): Promise<HomelabdSettingsResponse>;
  updateSettings(request: HomelabdUpdateSettingsRequest): Promise<HomelabdSettingsResponse>;
  createKnowledgeSpace(
    request: HomelabdCreateKnowledgeSpaceRequest
  ): Promise<HomelabdCreateKnowledgeSpaceResponse>;
  listKnowledgeSpaces(): Promise<HomelabdKnowledgeSpacesResponse>;
  getKnowledgeSpace(spaceId: string): Promise<HomelabdKnowledgeSpace>;
  updateKnowledgeSpace(
    spaceId: string,
    request: HomelabdUpdateKnowledgeSpaceRequest
  ): Promise<HomelabdUpdateKnowledgeSpaceResponse>;
  deleteKnowledgeSpace(spaceId: string): Promise<HomelabdDeleteKnowledgeSpaceResponse>;
  addKnowledgeSource(
    spaceId: string,
    request: HomelabdAddKnowledgeSourceRequest
  ): Promise<HomelabdAddKnowledgeSourceResponse>;
  deleteKnowledgeSource(
    spaceId: string,
    sourceId: string
  ): Promise<HomelabdDeleteKnowledgeSourceResponse>;
  researchKnowledgeSpace(
    spaceId: string,
    request: HomelabdResearchKnowledgeSpaceRequest
  ): Promise<HomelabdResearchKnowledgeSpaceResponse>;
  queryKnowledgeSpace(
    spaceId: string,
    request: HomelabdQueryKnowledgeSpaceRequest
  ): Promise<HomelabdQueryKnowledgeSpaceResponse>;
  askKnowledgeSpace(
    spaceId: string,
    request: HomelabdAskKnowledgeSpaceRequest
  ): Promise<HomelabdAskKnowledgeSpaceResponse>;
  createKnowledgeResearchRun(
    spaceId: string,
    request: HomelabdCreateKnowledgeResearchRunRequest
  ): Promise<HomelabdCreateKnowledgeResearchRunResponse>;
  resumeKnowledgeResearchRun(
    spaceId: string,
    runId: string
  ): Promise<HomelabdResumeKnowledgeResearchRunResponse>;
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
  moveTaskInMergeQueue(
    taskId: string,
    request: HomelabdMergeQueueMoveRequest
  ): Promise<HomelabdTaskActionResponse>;
  deleteTask(taskId: string): Promise<HomelabdTaskActionResponse>;
  approveApproval(approvalId: string): Promise<HomelabdTaskActionResponse>;
  denyApproval(approvalId: string): Promise<HomelabdTaskActionResponse>;
}

export interface HomelabdClientOptions {
  baseUrl?: string;
  fetcher?: typeof fetch;
}
