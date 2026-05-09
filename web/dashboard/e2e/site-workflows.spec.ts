import { expect, test } from '@playwright/test';
import type { Page } from '@playwright/test';

test.describe.configure({ timeout: 90_000 });

const now = '2026-04-28T12:00:00.000Z';
const taskID = 'task_20260428_120000_11111111';
const workflowID = 'workflow_20260428_120000_22222222';
const chatTranscriptStorageKey = 'homelabd.dashboard.chatTranscript.v4';
const longSuggestedTaskGoal = [
  'Improve task suggestion handling',
  'preserve repository scan context, reviewed plan details, validation expectations, and operator constraints',
  'keep the final risk note because it explains why truncation can drop vital task input',
  'include the complete model-generated investigation trail and handoff notes'
].join(' '.repeat(12));
const longSuggestedTaskCommand = `new ${longSuggestedTaskGoal}`;
const longSuggestedTaskAction = longSuggestedTaskCommand.replace(/\s+/g, ' ');
const chatScrollTranscript = Array.from({ length: 24 }, (_, index) => ({
  id: `scroll-regression-${index}`,
  role: index % 2 === 0 ? 'assistant' : 'user',
  content: [
    `Scroll regression message ${index + 1}`,
    '',
    'The transcript is intentionally tall so browser UAT can scroll the chat pane while keeping the dashboard navigation reachable.'
  ].join('\n'),
  source: 'program',
  time: '12:00'
}));

const task = {
  id: taskID,
  title: 'Review queue behaviour on mobile',
  goal: 'Keep the task queue usable on desktop and mobile.',
  status: 'awaiting_approval',
  assigned_to: 'codex',
  priority: 5,
  created_at: now,
  updated_at: now,
  merge_queue_position: 2,
  merge_queue_entered_at: now,
  result: 'ReviewerAgent checks passed.',
  plan: {
    status: 'reviewed',
    summary: 'Validate task queue, detail, and action state.',
    steps: [{ title: 'Inspect queue', detail: 'Check selected task behaviour.' }],
    risks: ['Long content can overflow narrow screens.'],
    review: 'Plan is ready.',
    created_at: now,
    reviewed_at: now
  }
};

const restartTask = {
  ...task,
  id: 'task_20260428_120500_33333333',
  title: 'Queue restart gate failed',
  goal: 'Keep the task queue restart gate visible on desktop and mobile.',
  status: 'awaiting_restart',
  updated_at: '2026-04-28T12:05:00.000Z',
  merge_queue_position: 1,
  merge_queue_entered_at: now,
  result: 'merged after approval approval_1; post-merge restart pending',
  restart_required: ['dashboard'],
  restart_completed: ['homelabd'],
  restart_status: 'failed',
  restart_current: 'dashboard',
  restart_last_error: 'dashboard health check failed after restart'
};

const queuedTask = {
  ...task,
  id: 'task_20260428_120700_44444444',
  title: 'Queued docs follow-up',
  goal: 'Keep merge queue reordering visible without moving the restart gate.',
  status: 'ready_for_review',
  updated_at: '2026-04-28T12:01:00.000Z',
  merge_queue_position: 3,
  result: 'external agent finished; ready for review.'
};

const workflow = {
  id: workflowID,
  name: 'Deploy homelab dashboard',
  goal: 'Validate a dashboard release candidate.',
  status: 'running',
  steps: [
    { id: 'step_1', name: 'Run checks', kind: 'tool', tool: 'bun.check' },
    { id: 'step_2', name: 'Run browser UAT', kind: 'tool', tool: 'bun.uat.site' }
  ],
  estimate: {
    steps: 2,
    estimated_llm_calls: 0,
    estimated_tool_calls: 2,
    workflow_calls: 0,
    waits: 0,
    estimated_seconds: 180,
    estimated_minutes: 3,
    summary: 'Two automated checks.'
  },
  created_at: now,
  updated_at: now
};

const assistantCatalogue = {
  name: 'Assistant',
  summary: 'A life-improving operating layer for briefs, planning, research, workflows, memory, and safe action.',
  updated_at: now,
  principles: [
    { name: 'Plan before action', summary: 'Preview state-changing work before execution.' }
  ],
  activities: [
    {
      id: 'prepare-decision',
      name: 'Research a decision',
      area: 'research',
      cadence: 'On demand',
      description: 'Investigate options and compare trade-offs.',
      outcome: 'A sourced decision brief with risks and next action.',
      capability_ids: ['research-prepare']
    },
    {
      id: 'start-day',
      name: 'Start my day',
      area: 'focus',
      cadence: 'Daily',
      description: 'Summarise priorities and blockers.',
      outcome: 'A short morning brief with focus blocks.',
      capability_ids: ['brief-prioritise']
    }
  ],
  capabilities: [
    {
      id: 'brief-prioritise',
      name: 'Brief and prioritise',
      area: 'focus',
      summary: 'Produce daily and situation-specific briefs from task state and health signals.',
      promise: 'Tell me what matters now.',
      cadence: 'Daily',
      autonomy: 'observe',
      inputs: ['Tasks', 'Health'],
      outputs: ['Priority list', 'Blockers'],
      surfaces: [{ label: 'Inspect Tasks', href: '/tasks', surface: 'tasks' }],
      ux_pattern_ids: ['mission-control'],
      safeguards: ['Show source counts'],
      workflow_template: {
        name: 'Assistant daily brief',
        goal: 'Create a concise daily brief with priorities and risks.',
        steps: [{ name: 'Write brief', kind: 'llm', prompt: 'Summarise the day.' }]
      }
    },
    {
      id: 'research-prepare',
      name: 'Research and prepare',
      area: 'research',
      summary: 'Run sourced research for decisions, meetings, purchases, travel, and investigations.',
      promise: 'Give me a brief that is current, cited, comparable, and ready to act on.',
      cadence: 'On demand',
      autonomy: 'plan',
      inputs: ['Question', 'Web sources', 'Docs'],
      outputs: ['Sourced brief', 'Comparison', 'Recommendation'],
      surfaces: [{ label: 'Open Chat', href: '/chat', surface: 'chat' }],
      ux_pattern_ids: ['source-tray', 'confidence-signals'],
      safeguards: ['Show sources and recency', 'Separate facts from inference'],
      workflow_template: {
        name: 'Assistant research brief',
        goal: 'Research the question, compare options, cite sources, and recommend next action.',
        steps: [
          { name: 'Search current sources', kind: 'tool', tool: 'internet.search' },
          { name: 'Synthesize decision brief', kind: 'llm', prompt: 'Compare options.' }
        ]
      }
    }
  ],
  ux_patterns: [
    {
      id: 'mission-control',
      name: 'Mission control',
      summary: 'Show active outcomes and decisions in one scan-friendly surface.',
      applies_to: 'Briefs',
      implementation: 'Use count badges, status text, and source links.'
    },
    {
      id: 'source-tray',
      name: 'Source tray',
      summary: 'Keep citations and retrieved material close to the answer.',
      applies_to: 'Research',
      implementation: 'Separate sourced facts from model inference.'
    },
    {
      id: 'confidence-signals',
      name: 'Confidence signals',
      summary: 'Show uncertainty and missing data.',
      applies_to: 'Research',
      implementation: 'Use open questions and escalation prompts.'
    }
  ],
  research_sources: [],
  filters: {
    areas: [
      { value: 'all', label: 'All areas', count: 2 },
      { value: 'focus', label: 'Daily focus', count: 1 },
      { value: 'research', label: 'Research', count: 1 }
    ]
  }
};

const assistantRun = {
  id: 'arun_site',
  status: 'completed',
  decision: 'recommend',
  trigger: { kind: 'schedule', label: 'Scheduled proactive check' },
  autonomy: 'propose',
  goal: 'Review current homelabd state and recommend useful next actions.',
  summary: 'Task queue signals suggest one useful follow-up.',
  changed: ['Reviewed tasks, workflows, health, supervisor, and knowledge spaces.'],
  concerns: [
    {
      title: 'Restart gate still needs review',
      detail: 'A dashboard restart gate is waiting after merge.',
      severity: 'warning',
      surface: 'tasks',
      object_id: restartTask.id,
      object_url: `/tasks?task=${restartTask.id}`
    }
  ],
  opportunities: [],
  recommended_actions: [
    {
      id: 'action_1',
      kind: 'task',
      title: 'Review restart gate',
      rationale: 'The queue contains an awaiting-restart task with a failed restart status.',
      priority: 'high',
      risk: 'low',
      target_surface: 'tasks',
      task_goal: 'Review the dashboard restart gate and decide the recovery path.',
      status: 'recommended'
    }
  ],
  receipts: [{ kind: 'decision', message: 'Recommended 1 actions.', created_at: now }],
  snapshot: {
    generated_at: now,
    task_counts: { awaiting_approval: 1, awaiting_restart: 1, ready_for_review: 1 },
    attention_tasks: [
      {
        id: restartTask.id,
        title: restartTask.title,
        status: restartTask.status,
        summary: restartTask.result,
        url: `/tasks?task=${restartTask.id}`
      }
    ],
    pending_approvals: 1,
    workflow_counts: { running: 1 },
    remote_agent_counts: { online: 1 },
    health: { status: 'healthy', items: [] },
    supervisor: { status: 'healthy', items: [] },
    recent_events: [{ id: 'evt_site', type: 'task.awaiting_restart', actor: 'Codex', time: now }]
  },
  created_at: now,
  started_at: now,
  finished_at: now,
  updated_at: now
};

const assistantGoal = {
  id: 'goal_site_daily',
  title: 'Daily brief',
  objective: 'Keep the daily brief current and point out unanswered mail.',
  details: 'Respect focus blocks and ask before sending messages.',
  kind: 'routine',
  execution_mode: 'guided',
  status: 'active',
  priority: 'high',
  autonomy: 'observe',
  cadence: 'daily',
  success_criteria: ['Brief is ready before 08:30.'],
  constraints: ['Do not send messages without approval.'],
  linked_tasks: ['task_goal_site'],
  progress_summary: 'Daily brief Goal is due for review.',
  created_by: 'operator',
  created_at: now,
  updated_at: now,
  last_checked_at: now,
  next_check_at: now
};

const assistantAutopilotGoal = {
  id: 'goal_site_grid',
  title: 'Grid rebuild',
  objective: 'Build a replacement for AG Grid with editing, validation, and keyboard support.',
  details: 'Use the remote project and keep phases visible to the operator.',
  kind: 'build',
  execution_mode: 'autopilot',
  status: 'active',
  priority: 'high',
  autonomy: 'create_tasks',
  cadence: '',
  target: { mode: 'remote', project_id: 'remote1', agent_id: 'remote1-agent', workdir_id: 'remote1' },
  autopilot: {
    status: 'running',
    budget_tasks: -1,
    tasks_started: 2,
    current_task_id: 'task_goal_grid_core',
    current_phase_id: 'phase_02_core',
    last_decision_id: 'gdec_goal_grid_next'
  },
  plan: {
    status: 'active',
    summary: 'Supervisor plan for Grid rebuild',
    current_phase_id: 'phase_02_core',
    phases: [
      {
        id: 'phase_01_foundation',
        title: 'Establish the architecture and baseline',
        objective: 'Inspect the target repo and create the first usable foundation.',
        status: 'completed',
        task_ids: ['task_goal_grid_foundation'],
        evidence: ['Foundation complete']
      },
      {
        id: 'phase_02_core',
        title: 'Build the core capability',
        objective: 'Implement rendering, editing, and validation needed for end-to-end use.',
        status: 'in_progress',
        depends_on: ['phase_01_foundation'],
        task_ids: ['task_goal_grid_core']
      }
    ],
    created_at: now,
    updated_at: now
  },
  linked_tasks: ['task_goal_grid_foundation', 'task_goal_grid_core'],
  progress_summary: 'Foundation complete; core rendering is next.',
  created_by: 'operator',
  created_at: now,
  updated_at: now,
  next_check_at: ''
};

const assistantGoalDecision = {
  id: 'gdec_goal_grid_next',
  goal_id: assistantAutopilotGoal.id,
  decision: 'create_task',
  summary: 'Create the next task for plan phase `phase_02_core`.',
  rationale: 'The selected phase is the next incomplete, dependency-ready phase in the Goal plan.',
  phase_id: 'phase_02_core',
  task_id: 'task_goal_grid_core',
  evidence: ['task gridfound: Foundation complete'],
  created_at: now
};

const assistantGoalTaskReport = {
  id: 'greport_goal_grid_foundation',
  goal_id: assistantAutopilotGoal.id,
  task_id: 'task_goal_grid_foundation',
  phase_id: 'phase_01_foundation',
  title: 'Foundation task',
  status: 'done',
  summary: 'Foundation complete',
  advanced_goal: true,
  phase_complete: true,
  goal_complete: false,
  changed_files: ['src/grid.ts'],
  validation: ['bun test'],
  follow_ups: ['Build core rendering'],
  blockers: [],
  questions: [],
  diff_files: 1,
  additions: 80,
  deletions: 0,
  created_at: now
};

const assistantGoalWatch = {
  id: 'gwatch_site_daily',
  goal_id: assistantGoal.id,
  title: 'Morning mail watch',
  kind: 'reminder',
  source: 'operator',
  status: 'active',
  severity: 'info',
  cadence: 'daily',
  condition: 'Look for unanswered mail before the daily brief.',
  suggested_action: 'Prepare the morning brief with unanswered mail called out.',
  created_at: now,
  updated_at: now
};

const assistantGoalNote = {
  id: 'gnote_site_daily',
  goal_id: assistantGoal.id,
  kind: 'progress',
  title: 'Progress update',
  body: 'The first brief watch is active and waiting for the next check.',
  created_by: 'assistant',
  created_at: now
};

const knowledgeSource = {
  id: 'ksrc_20260428_120000_33333333',
  title: 'Source transparency notes',
  kind: 'text',
  content: 'Source-grounded reports should keep evidence visible beside generated claims.',
  summary: 'Source-grounded reports should keep evidence visible beside generated claims.',
  key_terms: ['source', 'evidence', 'reports'],
  questions: ['What does this source show about evidence?'],
  word_count: 8,
  ingestion: { state: 'ready', stage: 'indexed', message: 'Source is indexed.', completed_at: now },
  chunks: [{
    id: 'chunk_1',
    source_id: 'ksrc_20260428_120000_33333333',
    source_title: 'Source transparency notes',
    index: 0,
    citation_label: 'S1.1',
    text: 'Source-grounded reports should keep evidence visible beside generated claims.',
    terms: ['source', 'evidence'],
    word_count: 8
  }],
  created_at: now,
  updated_at: now
};

const knowledgeReport = {
  id: 'kreport_20260428_120000_44444444',
  question: 'How should evidence be reviewed?',
  mode: 'research',
  answer: 'Answering "How should evidence be reviewed?" from 1 stored source:\n- [S1] Keep evidence visible beside generated claims.',
  key_findings: ['[S1] Keep evidence visible beside generated claims.'],
  evidence: [{
    id: 'evidence_01',
    source_id: knowledgeSource.id,
    source_title: knowledgeSource.title,
    citation_label: 'S1',
    excerpt: 'Source-grounded reports should keep evidence visible beside generated claims.',
    terms: ['evidence'],
    score: 3
  }],
  gaps: ['Only stored Knowledge Space sources were used for this report.'],
  created_at: now
};

const knowledgeSpace = {
  id: 'kspace_20260428_120000_55555555',
  title: 'Research synthesis',
  objective: 'Keep source-grounded research easy to review.',
  sources: [knowledgeSource],
  reports: [knowledgeReport],
  research_runs: [],
  insight: {
    source_count: 1,
    word_count: 8,
    key_terms: ['source', 'evidence', 'reports'],
    suggested_questions: ['What does this space show about source?'],
    updated_at: now
  },
  created_at: now,
  updated_at: now
};

const healthSnapshot = {
  status: 'healthy',
  started_at: now,
  uptime_seconds: 7200,
  window_seconds: 300,
  current: {
    time: now,
    good: true,
    cpu_usage_percent: 17,
    memory_usage_percent: 43,
    memory_used_bytes: 8_200_000_000,
    memory_total_bytes: 19_000_000_000,
    load1: 0.42,
    load5: 0.36,
    load15: 0.31,
    system_uptime_seconds: 238000,
    process_uptime_seconds: 7200,
    goroutines: 41
  },
  samples: [],
  checks: [{ name: 'homelabd', type: 'http', status: 'healthy', message: 'ready', latency_ms: 12, last_checked: now }],
  processes: [{ name: 'dashboard', type: 'service', status: 'healthy', message: 'serving', pid: 4321, addr: '127.0.0.1:5173', started_at: now, last_seen: now, ttl_seconds: 30 }],
  slos: [{ name: 'dashboard availability', target_percent: 99.5, window_seconds: 300, good_events: 60, total_events: 60, sli_percent: 100, error_budget_remaining_percent: 100, burn_rate: 0, status: 'healthy' }],
  notifications: []
};

const supervisorSnapshot = {
  status: 'healthy',
  started_at: now,
  restartable: true,
  restart_hint: 'go run ./cmd/supervisord',
  apps: [{
    name: 'dashboard',
    type: 'web',
    state: 'running',
    desired: 'running',
    pid: 1203,
    restarts: 1,
    message: 'serving dashboard',
    started_at: now,
    updated_at: now,
    start_order: 30,
    restart: 'always',
    command: 'bun',
    args: ['run', 'dev']
  }]
};

const installTerminalMocks = async (page: Page) => {
  await page.addInitScript(() => {
    class MockEventSource extends EventTarget {
      static CONNECTING = 0;
      static OPEN = 1;
      static CLOSED = 2;
      readyState = MockEventSource.CONNECTING;
      onopen: ((event: Event) => void) | null = null;
      onerror: ((event: Event) => void) | null = null;
      url: string;
      constructor(url: string) {
        super();
        this.url = url;
        setTimeout(() => {
          this.readyState = MockEventSource.OPEN;
          this.onopen?.(new Event('open'));
          this.dispatchEvent(new MessageEvent('output', {
            data: JSON.stringify({ type: 'output', seq: 1, data: 'ready\\r\\n' })
          }));
        }, 20);
      }
      close() {
        this.readyState = MockEventSource.CLOSED;
      }
    }
    window.EventSource = MockEventSource as typeof EventSource;
  });
};

const mockScreenCaptureUnavailable = async (page: Page) => {
  await page.addInitScript(() => {
    Object.defineProperty(navigator, 'mediaDevices', {
      configurable: true,
      value: {
        getDisplayMedia: async () => {
          throw new Error('Screen capture is not available in this browser.');
        }
      }
    });
  });
};

const mockDashboardApis = async (page: Page) => {
  await installTerminalMocks(page);
  let autoMergeEnabled = false;
  let knowledgeSpaces = [knowledgeSpace];
  await page.route(/\/api\/message$/, async (route) => {
    const body = route.request().postDataJSON() as { content?: string };
    if (body.content === longSuggestedTaskAction) {
      await route.fulfill({
        json: {
          reply: 'Created task from full suggested brief.',
          source: 'program',
          stats: { model_turns: 1, tool_calls: 1, total_tokens: 64, elapsed_ms: 730 }
        }
      });
      return;
    }
    await route.fulfill({
      json: {
        reply: [
          'Status: `tasks` and `workflow list` are available.',
          `Action: \`${longSuggestedTaskCommand}\``,
          '',
          '```mermaid',
          'flowchart LR',
          '  Chat[Chat] --> Tasks[Tasks]',
          '  Tasks --> Review[Review]',
          '```'
        ].join('\n'),
        source: 'program',
        stats: { model_turns: 1, tool_calls: 2, total_tokens: 128, elapsed_ms: 1234 }
      }
    });
  });
  const assistantRuns = [assistantRun];
  const assistantGoals: any[] = [assistantGoal, assistantAutopilotGoal];
  const assistantGoalWatches: any[] = [assistantGoalWatch];
  const assistantGoalNotes: any[] = [assistantGoalNote];
  const assistantGoalDecisions: any[] = [assistantGoalDecision];
  const assistantGoalTaskReports: any[] = [assistantGoalTaskReport];
  const assistantGoalTimeline = (goal: any) => ({
    goal,
    watches: assistantGoalWatches.filter((watch) => watch.goal_id === goal.id),
    signals: [],
    notes: assistantGoalNotes.filter((note) => note.goal_id === goal.id),
    assessments: [],
    decisions: assistantGoalDecisions.filter((decision) => decision.goal_id === goal.id),
    task_reports: assistantGoalTaskReports.filter((report) => report.goal_id === goal.id)
  });
  await page.route(/\/api\/assistant\/runs(?:\/[^?]+)?(?:\?.*)?$/, async (route) => {
    const url = new URL(route.request().url());
    const runID = url.pathname.split('/').pop() || '';
    if (route.request().method() === 'POST') {
      const created = {
        ...assistantRun,
        id: 'arun_site_manual',
        trigger: { kind: 'manual', label: 'Operator requested proactive check' },
        summary: 'Manual proactive check completed.',
        updated_at: now
      };
      assistantRuns.unshift(created);
      await route.fulfill({ status: 201, json: { reply: 'Assistant run completed.', run: created } });
      return;
    }
    if (runID && runID !== 'runs') {
      await route.fulfill({ json: assistantRuns.find((run) => run.id === runID) || assistantRuns[0] });
      return;
    }
    await route.fulfill({ json: { runs: assistantRuns } });
  });
  await page.route(/\/api\/assistant\/signals(?:\/.*)?(?:\?.*)?$/, async (route) => {
    await route.fulfill({ json: { signals: [] } });
  });
  await page.route(/\/api\/assistant\/goals(?:\/.*)?(?:\?.*)?$/, async (route) => {
    const url = new URL(route.request().url());
    const parts = url.pathname.split('/').filter(Boolean);
    const goalIndex = parts.indexOf('goals');
    const goalID = goalIndex >= 0 ? parts[goalIndex + 1] || '' : '';
    const suffix = goalIndex >= 0 ? parts[goalIndex + 2] || '' : '';
    const goal = assistantGoals.find((candidate) => candidate.id === goalID) || assistantGoals[0];
    if (route.request().method() === 'POST' && !goalID) {
      const body = route.request().postDataJSON() as {
        title?: string;
        objective?: string;
        cadence?: string;
        autonomy?: string;
        kind?: string;
        execution_mode?: string;
      };
      const created = {
        ...assistantGoal,
        id: 'goal_site_created',
        title: body.title || 'Created Goal',
        objective: body.objective || body.title || 'Keep this Goal alive.',
        kind: body.kind || 'build',
        execution_mode: body.execution_mode || 'guided',
        autopilot: undefined,
        cadence: body.cadence || 'daily',
        autonomy: body.autonomy || 'observe',
        linked_tasks: [],
        progress_summary: 'Goal is waiting for its first Assistant assessment.',
        created_by: 'dashboard',
        created_at: now,
        updated_at: now,
        last_checked_at: '',
        next_check_at: now
      };
      assistantGoals.unshift(created);
      assistantGoalNotes.unshift({
        ...assistantGoalNote,
        id: 'gnote_site_created',
        goal_id: created.id,
        title: 'Goal created',
        body: 'Dashboard created this Goal for proactive review.'
      });
      await route.fulfill({ status: 201, json: assistantGoalTimeline(created) });
      return;
    }
    if (route.request().method() === 'POST' && suffix === 'check') {
      const created = {
        ...assistantRun,
        id: 'arun_site_goal',
        goal_id: goal.id,
        trigger: { kind: 'goal', label: `Goal check: ${goal.title}` },
        goal: goal.objective,
        summary: `Goal check completed for ${goal.title}.`,
        updated_at: now
      };
      assistantRuns.unshift(created);
      goal.last_checked_at = now;
      goal.next_check_at = '2026-04-29T12:00:00.000Z';
      goal.progress_summary = `Checked by ${created.id}; next review is scheduled.`;
      goal.updated_at = now;
      await route.fulfill({ json: { reply: 'Goal check completed.', run: created } });
      return;
    }
    if (route.request().method() === 'PATCH' && goalID) {
      const body = route.request().postDataJSON() as { status?: string };
      if (body.status) {
        goal.status = body.status;
        goal.updated_at = now;
      }
      await route.fulfill({ json: assistantGoalTimeline(goal) });
      return;
    }
    if (goalID) {
      await route.fulfill({ json: assistantGoalTimeline(goal) });
      return;
    }
    await route.fulfill({ json: { goals: assistantGoals } });
  });
  await page.route(/\/api\/assistant(?:\?.*)?$/, async (route) => {
    const url = new URL(route.request().url());
    const area = url.searchParams.get('area') || 'all';
    const query = (url.searchParams.get('q') || '').toLowerCase();
    const capabilities = assistantCatalogue.capabilities.filter((capability) => {
      if (area !== 'all' && capability.area !== area) {
        return false;
      }
      if (!query) {
        return true;
      }
      return [capability.name, capability.summary, capability.promise, capability.area]
        .join(' ')
        .toLowerCase()
        .includes(query);
    });
    const capabilityIDs = new Set(capabilities.map((capability) => capability.id));
    await route.fulfill({
      json: {
        ...assistantCatalogue,
        capabilities,
        activities: assistantCatalogue.activities.filter((activity) => {
          if (area !== 'all' && activity.area !== area) {
            return false;
          }
          return !query || activity.capability_ids.some((id) => capabilityIDs.has(id));
        })
      }
    });
  });
  await page.route(/\/api\/chat\/clear$/, async (route) => {
    await route.fulfill({
      json: {
        reply: 'Cleared chat conversation.',
        conversation_id: 'chat_test',
        removed_events: 4,
        removed_log_entries: 4
      }
    });
  });
  await page.route(/\/api\/tasks\/?(?:\?.*)?$/, async (route) => {
    await route.fulfill({ json: { tasks: [queuedTask, restartTask, task] } });
  });
  await page.route(/\/api\/settings\/?(?:\?.*)?$/, async (route) => {
    if (route.request().method() === 'POST') {
      const body = route.request().postDataJSON() as { auto_merge_enabled?: boolean };
      autoMergeEnabled = Boolean(body.auto_merge_enabled);
    }
    await route.fulfill({ json: { settings: { auto_merge_enabled: autoMergeEnabled } } });
  });
  await page.route(/\/api\/tasks\/[^/]+\/runs\/?(?:\?.*)?$/, async (route) => {
    await route.fulfill({ json: { runs: [] } });
  });
  await page.route(/\/api\/tasks\/[^/]+\/diff\/?(?:\?.*)?$/, async (route) => {
    await route.fulfill({
      json: {
        task_id: taskID,
        raw_diff: '',
        summary: { files: 0, additions: 0, deletions: 0 },
        files: [],
        generated_at: now
      }
    });
  });
  await page.route(/\/api\/tasks\/[^/]+\/(?:run|review|merge-queue|accept|restart|reopen|cancel|retry|delete)\/?(?:\?.*)?$/, async (route) => {
    await route.fulfill({ json: { reply: 'task action accepted' } });
  });
  await page.route(/\/api\/approvals(?:\/[^/]+\/(?:approve|deny))?\/?(?:\?.*)?$/, async (route) => {
    await route.fulfill({ json: route.request().url().includes('/approve') ? { reply: 'approved' } : { approvals: [] } });
  });
  await page.route(/\/api\/events(?:\?.*)?$/, async (route) => {
    await route.fulfill({ json: { events: [] } });
  });
	  await page.route(/\/api\/agents\/?(?:\?.*)?$/, async (route) => {
	    await route.fulfill({ json: { agents: [] } });
	  });
	  await page.route(/\/api\/workspaces\/?(?:\?.*)?$/, async (route) => {
	    await route.fulfill({ json: { workspaces: [] } });
	  });
	  await page.route(/\/api\/workflows$/, async (route) => {
    if (route.request().method() === 'POST') {
      await route.fulfill({ status: 201, json: { workflow, reply: 'created workflow' } });
      return;
    }
    await route.fulfill({ json: { workflows: [workflow] } });
  });
  await page.route(/\/api\/workflows\/[^/]+\/run$/, async (route) => {
    await route.fulfill({ json: { workflow: { ...workflow, status: 'completed' }, reply: 'workflow started' } });
  });
  await page.route(/\/api\/knowledge\/spaces$/, async (route) => {
    if (route.request().method() === 'POST') {
      const body = route.request().postDataJSON() as { title?: string; objective?: string; description?: string };
      const created = {
        ...knowledgeSpace,
        id: 'kspace_created',
        title: body.title || 'New space',
        objective: body.objective,
        description: body.description,
        sources: [],
        reports: [],
        research_runs: [],
        insight: { source_count: 0, word_count: 0, key_terms: [], suggested_questions: [], updated_at: now },
        created_at: now,
        updated_at: now
      };
      knowledgeSpaces = [created, ...knowledgeSpaces];
      await route.fulfill({ status: 201, json: { space: created, reply: 'Knowledge Space created' } });
      return;
    }
    await route.fulfill({ json: { spaces: knowledgeSpaces } });
  });
  await page.route(/\/api\/knowledge\/spaces\/[^/]+$/, async (route) => {
    await route.fulfill({ json: knowledgeSpaces[0] });
  });
  await page.route(/\/api\/knowledge\/spaces\/[^/]+\/sources$/, async (route) => {
    const body = route.request().postDataJSON() as { title?: string; kind?: string; content?: string; uri?: string };
    const source = {
      ...knowledgeSource,
      id: 'ksrc_created',
      title: body.title || 'Added source',
      kind: body.kind || 'text',
      uri: body.uri,
      content: body.content || '',
      summary: body.content || 'Added source summary.',
      word_count: (body.content || '').split(/\s+/).filter(Boolean).length,
      ingestion: { state: 'ready', stage: 'indexed', message: 'Source is indexed.', completed_at: now },
      chunks: [{
        id: 'ksrc_created_chunk_001',
        source_id: 'ksrc_created',
        source_title: body.title || 'Added source',
        index: 0,
        citation_label: 'CREA.1',
        text: body.content || 'Added source summary.',
        word_count: (body.content || '').split(/\s+/).filter(Boolean).length
      }],
      created_at: now,
      updated_at: now
    };
    const current = knowledgeSpaces[0];
    const updated = {
      ...current,
      sources: [source, ...(current.sources || [])],
      insight: {
        ...current.insight,
        source_count: (current.sources || []).length + 1,
        word_count: (current.insight?.word_count || 0) + source.word_count,
        key_terms: ['source', 'evidence', 'review'],
        updated_at: now
      },
      updated_at: now
    };
    knowledgeSpaces = [updated, ...knowledgeSpaces.slice(1)];
    await route.fulfill({ status: 201, json: { space: updated, source, reply: 'Source indexed' } });
  });
  await page.route(/\/api\/knowledge\/spaces\/[^/]+\/research$/, async (route) => {
    const body = route.request().postDataJSON() as { question?: string; mode?: string };
    const report = {
      ...knowledgeReport,
      id: 'kreport_created',
      question: body.question || knowledgeReport.question,
      mode: body.mode || 'research',
      created_at: now
    };
    const current = knowledgeSpaces[0];
    const updated = {
      ...current,
      reports: [report, ...(current.reports || [])],
      updated_at: now
    };
    knowledgeSpaces = [updated, ...knowledgeSpaces.slice(1)];
    await route.fulfill({ json: { space: updated, report, reply: 'Research report created' } });
  });
  await page.route(/\/api\/knowledge\/spaces\/[^/]+\/ask$/, async (route) => {
    const body = route.request().postDataJSON() as { question?: string };
    const report = {
      ...knowledgeReport,
      id: 'kreport_ask',
      question: body.question || knowledgeReport.question,
      mode: 'ask',
      created_at: now
    };
    const current = knowledgeSpaces[0];
    const updated = {
      ...current,
      reports: [report, ...(current.reports || [])],
      updated_at: now
    };
    knowledgeSpaces = [updated, ...knowledgeSpaces.slice(1)];
    await route.fulfill({
      json: {
        space: updated,
        result: {
          question: report.question,
          answer: report.answer,
          key_findings: report.key_findings,
          evidence: report.evidence,
          gaps: report.gaps,
          created_at: report.created_at
        },
        report,
        reply: 'Grounded answer saved.'
      }
    });
  });
  await page.route(/\/api\/knowledge\/spaces\/[^/]+\/query$/, async (route) => {
    await route.fulfill({
      json: {
        result: {
          query: 'evidence',
          terms: ['evidence'],
          evidence: knowledgeReport.evidence,
          created_at: now
        },
        reply: 'Corpus query completed.'
      }
    });
  });
  await page.route(/\/api\/knowledge\/spaces\/[^/]+\/research-runs$/, async (route) => {
    const body = route.request().postDataJSON() as { objective?: string; depth?: string };
    const run = {
      id: 'krun_created',
      objective: body.objective || 'Research run',
      depth: body.depth || 'standard',
      status: 'completed',
      mode: 'research',
      sources_examined: 1,
      evidence_count: 1,
      events: [{ id: 'krun_event_1', stage: 'retrieval', message: 'Retrieved matching corpus chunks.', created_at: now }],
      created_at: now,
      updated_at: now
    };
    const report = { ...knowledgeReport, id: 'kreport_run', run_id: run.id, created_at: now };
    const current = knowledgeSpaces[0];
    const updated = {
      ...current,
      research_runs: [run, ...(current.research_runs || [])],
      reports: [report, ...(current.reports || [])],
      updated_at: now
    };
    knowledgeSpaces = [updated, ...knowledgeSpaces.slice(1)];
    await route.fulfill({ status: 201, json: { space: updated, run, report, reply: 'Research run completed.' } });
  });
  await page.route(/\/api\/terminal\/sessions$/, async (route) => {
    await route.fulfill({ status: 201, json: { id: 'term_site', shell: '/bin/sh', cwd: '/workspace', created_at: now, persistent: true } });
  });
  await page.route(/\/api\/terminal\/sessions\/[^/]+(?:\/(?:resize|input|events))?$/, async (route) => {
    await route.fulfill({ json: { id: 'term_site', shell: '/bin/sh', cwd: '/workspace', created_at: now, persistent: true } });
  });
  await page.route(/\/healthd-api\/healthd(?:\/checks\/run)?(?:\?.*)?$/, async (route) => {
    await route.fulfill({ json: healthSnapshot });
  });
  await page.route(/\/supervisord-api\/supervisord(?:\/restart)?$/, async (route) => {
    await route.fulfill({ json: route.request().url().endsWith('/restart') ? { reply: 'restart scheduled' } : supervisorSnapshot });
  });
  await page.route(/\/supervisord-api\/supervisord\/apps\/[^/]+\/(?:start|stop|restart)$/, async (route) => {
    await route.fulfill({ json: supervisorSnapshot });
  });
};

const expectNoVisualArtifacts = async (page: Page) => {
  const metrics = await page.evaluate(() => {
    const hasScrollableXAncestor = (element: Element) => {
      let parent = element.parentElement;
      while (parent && parent !== document.body) {
        const style = getComputedStyle(parent);
        const scrollable = ['auto', 'scroll'].includes(style.overflowX);
        if (scrollable && parent.scrollWidth > parent.clientWidth + 2) {
          return true;
        }
        parent = parent.parentElement;
      }
      return false;
    };
    const isHidden = (element: Element) => {
      let current: Element | null = element;
      while (current && current !== document.body) {
        const style = getComputedStyle(current);
        if (style.display === 'none' || style.visibility === 'hidden') {
          return true;
        }
        current = current.parentElement;
      }
      return false;
    };
    const contentRoots = Array.from(
      document.querySelectorAll('main, .assistant-page, .task-pane, .chat-card, .docs-shell, .workflow-page, .knowledge-page, .terminal-panel, .app-shell')
    )
      .map((element) => {
        const rect = element.getBoundingClientRect();
        const style = getComputedStyle(element);
        return style.display === 'none' || style.visibility === 'hidden' ? 0 : Math.round(rect.height);
      })
      .filter((height) => height > 0);
    const escaped = Array.from(document.querySelectorAll('h1,h2,h3,p,a,button,summary,label,span,strong'))
      .filter((element) => {
        if (element.closest('.xterm') || hasScrollableXAncestor(element) || isHidden(element)) {
          return false;
        }
        const rect = element.getBoundingClientRect();
        return rect.width > 0 && rect.height > 0 && (rect.left < -2 || rect.right > window.innerWidth + 2);
      })
      .map((element) => (element.textContent || '').trim().replace(/\s+/g, ' ').slice(0, 80));
    const clippedButtons = Array.from(document.querySelectorAll('button,summary'))
      .filter((element) => {
        const rect = element.getBoundingClientRect();
        return rect.width > 0 && rect.height > 0 && (element.scrollWidth > element.clientWidth + 2 || element.scrollHeight > element.clientHeight + 2);
      })
      .map((element) => (element.textContent || element.getAttribute('aria-label') || '').trim().replace(/\s+/g, ' ').slice(0, 80));
    const navbar = document.querySelector('.navbar');
    const navbarRect = navbar?.getBoundingClientRect();
    const navbarBottom = navbarRect && navbarRect.height > 0 ? navbarRect.bottom : null;
    const navbarOverlaps =
      navbarBottom === null || window.scrollY > 2
        ? []
        : Array.from(document.querySelectorAll('main, .shell, .docs-shell, .workflow-page, .knowledge-page, .terminal-panel'))
            .filter((element) => {
              if (isHidden(element)) {
                return false;
              }
              const rect = element.getBoundingClientRect();
              return rect.width > 0 && rect.height > 0 && rect.top < navbarBottom - 1;
            })
            .map((element) => ({
              label: `${element.tagName.toLowerCase()}${element.className ? `.${String(element.className).replace(/\s+/g, '.')}` : ''}`,
              top: Math.round(element.getBoundingClientRect().top),
              navbarBottom: Math.round(navbarBottom)
            }));
    return {
      bodyWidth: document.body.scrollWidth,
      docWidth: document.documentElement.scrollWidth,
      viewport: window.innerWidth,
      contentHeight: Math.max(...contentRoots, 0),
      escaped,
      clippedButtons,
      navbarOverlaps
    };
  });
  expect(metrics.bodyWidth, JSON.stringify(metrics)).toBeLessThanOrEqual(metrics.viewport + 2);
  expect(metrics.docWidth, JSON.stringify(metrics)).toBeLessThanOrEqual(metrics.viewport + 2);
  expect(metrics.contentHeight, JSON.stringify(metrics)).toBeGreaterThan(40);
  expect(metrics.escaped, JSON.stringify(metrics)).toEqual([]);
  expect(metrics.clippedButtons, JSON.stringify(metrics)).toEqual([]);
  expect(metrics.navbarOverlaps, JSON.stringify(metrics)).toEqual([]);
};

const openMobileMenu = async (page: Page) => {
  const menu = page.getByRole('button', { name: 'Menu' });
  const nav = page.getByRole('navigation', { name: 'Primary mobile' });
  await menu.click();
  try {
    await expect(nav).toBeVisible({ timeout: 3_000 });
  } catch {
    await menu.click();
    await expect(nav).toBeVisible();
  }
  return nav;
};

const seedChatScrollTranscript = async (page: Page) => {
  await page.addInitScript(
    ({ key, messages }) => {
      localStorage.setItem(key, JSON.stringify(messages));
      localStorage.removeItem('homelabd.dashboard.chatDraft.v1');
    },
    { key: chatTranscriptStorageKey, messages: chatScrollTranscript }
  );
};

const expectChatNavbarPinned = async (page: Page) => {
  const metrics = await page.evaluate(async () => {
    const navbar = document.querySelector('.navbar');
    const messages = document.querySelector('.messages');
    const composer = document.querySelector('.composer');
    if (messages) {
      messages.scrollTop = messages.scrollHeight;
    }
    window.scrollTo(0, document.body.scrollHeight);
    await new Promise<void>((resolve) => {
      requestAnimationFrame(() => requestAnimationFrame(() => resolve()));
    });
    const navbarRect = navbar?.getBoundingClientRect();
    const messagesRect = messages?.getBoundingClientRect();
    const composerRect = composer?.getBoundingClientRect();
    return {
      navbarTop: navbarRect?.top ?? null,
      navbarBottom: navbarRect?.bottom ?? null,
      navbarPosition: navbar ? getComputedStyle(navbar).position : null,
      messagesTop: messagesRect?.top ?? null,
      messagesScrollTop: messages?.scrollTop ?? 0,
      messagesScrollHeight: messages?.scrollHeight ?? 0,
      messagesClientHeight: messages?.clientHeight ?? 0,
      composerBottom: composerRect?.bottom ?? null,
      windowScrollY: window.scrollY,
      viewportHeight: window.innerHeight
    };
  });
  expect(metrics.navbarPosition, JSON.stringify(metrics)).toBe('fixed');
  expect(
    Math.abs(metrics.navbarTop ?? Number.POSITIVE_INFINITY),
    JSON.stringify(metrics)
  ).toBeLessThanOrEqual(1);
  expect(metrics.messagesScrollHeight, JSON.stringify(metrics)).toBeGreaterThan(
    metrics.messagesClientHeight
  );
  expect(metrics.messagesScrollTop, JSON.stringify(metrics)).toBeGreaterThan(0);
  expect(metrics.windowScrollY, JSON.stringify(metrics)).toBeLessThanOrEqual(8);
  expect(metrics.messagesTop ?? 0, JSON.stringify(metrics)).toBeGreaterThanOrEqual(
    (metrics.navbarBottom ?? 0) - 1
  );
  expect(metrics.composerBottom ?? 0, JSON.stringify(metrics)).toBeLessThanOrEqual(
    metrics.viewportHeight + 1
  );
};

const expectTaskNavAttention = async (page: Page, mobile: boolean) => {
  const taskLinkName = 'Tasks, 3 review items need attention';
  if (mobile) {
    await expect(page.locator('.mobile-nav .attention-badge.warning')).toHaveText('3');
    const mobileNav = await openMobileMenu(page);
    await expect(mobileNav.getByRole('link', { name: taskLinkName })).toBeVisible();
    await expect(page.locator('.mobile-nav .attention-badge.warning')).toHaveText('3');
    await expect(page.locator('.mobile-menu-scrim')).toBeVisible();
    await page.mouse.click(20, (page.viewportSize()?.height ?? 844) - 24);
    await expect(mobileNav).toBeHidden();
    return;
  }
  await expect(
    page.getByRole('navigation', { name: 'Primary' }).getByRole('link', { name: taskLinkName })
  ).toBeVisible();
  await expect(page.locator('.desktop-nav .attention-badge.warning')).toHaveText('3');
};

const exerciseRoute = async (page: Page, route: string, mobile: boolean) => {
  await expectTaskNavAttention(page, mobile);
  if (route === '/assistant') {
    await expect(page.getByRole('heading', { name: '1 decision' })).toBeVisible();
    const goalsPanel = page.getByLabel('Assistant Goals');
    await expect(goalsPanel.getByRole('heading', { name: 'Goals' })).toBeVisible();
    const dailyGoal = goalsPanel.getByRole('button', { name: /Daily brief/ });
    await expect(dailyGoal).toBeVisible();
    await dailyGoal.click();
    await expect(page.getByLabel('Selected Assistant Goal')).toContainText('Morning mail watch');
    if (mobile) {
      await page.getByRole('button', { name: 'Back to Goal list' }).click();
    }
    const gridGoal = goalsPanel.getByRole('button', { name: /Grid rebuild/ });
    await expect(gridGoal).toBeVisible();
    await gridGoal.click();
    const selectedGoalRecord = page.getByLabel('Selected Assistant Goal');
    await expect(selectedGoalRecord).toContainText('Supervisor plan for Grid rebuild');
    await expect(selectedGoalRecord).toContainText('Build the core capability');
    await expect(selectedGoalRecord).toContainText('Decision trail');
    await expect(selectedGoalRecord).toContainText('Foundation complete');
    await expect(selectedGoalRecord).toContainText('Build core rendering');
    if (mobile) {
      await page.getByRole('button', { name: 'Back to Goal list' }).click();
    }
    await goalsPanel.getByRole('button', { name: 'Create Goal' }).click();
    const goalForm = goalsPanel.locator('form[aria-label="Create Assistant Goal"]');
    await goalForm.getByLabel('Title').fill('Inbox follow-up');
    await goalForm.getByLabel('Objective').fill('Keep unanswered inbox items visible until resolved.');
    await goalForm.getByRole('button', { name: 'Create Goal' }).click();
    await expect(page.getByLabel('Selected Assistant Goal')).toContainText(
      'Keep unanswered inbox items visible until resolved.'
    );
    if (mobile) {
      await page.getByRole('button', { name: 'Back to Goal list' }).click();
    }
    await expect(goalsPanel.getByRole('status')).toContainText('Build Goal created in Guided mode.');
    await expect(goalsPanel.getByText('Inbox follow-up')).toBeVisible();
    await page.getByRole('link', { name: /Scheduled proactive check/ }).click();
    await expect(page).toHaveURL(/\/assistant\?run=arun_site$/);
    await expect(page.getByRole('heading', { name: 'Scheduled proactive check' })).toBeVisible();
    await expect(page.getByRole('heading', { name: 'Recommended actions' })).toBeVisible();
    if (mobile) {
      await page.getByRole('button', { name: 'Back to runs' }).click();
      await expect(page).toHaveURL(/\/assistant$/);
    }
    await page.getByRole('button', { name: 'Run proactive Assistant check' }).click();
    await expect(page).toHaveURL(/\/assistant\?run=arun_site_manual$/);
    await expect(page.getByRole('status', { name: 'Assistant run status' })).toContainText(
      'Assistant run completed.'
    );
    if (mobile) {
      await page.getByRole('button', { name: 'Back to runs' }).click();
      await expect(page).toHaveURL(/\/assistant$/);
    }
    await expect(page.getByRole('link', { name: 'Open Assistant documentation' })).toHaveAttribute(
      'href',
      '/docs/dashboard#assistant'
    );
    await expect(page.getByText('Capabilities, triggers, and safeguards')).toBeVisible();
  } else if (route === '/' || route === '/chat') {
    await page.getByRole('textbox', { name: 'Message' }).fill('status');
    await page.getByRole('button', { name: 'Send' }).click();
    await expect(page.getByText('Status:')).toBeVisible();
    const longSuggestion = page.getByRole('button', { name: longSuggestedTaskAction });
    await expect(longSuggestion).toBeVisible();
    await longSuggestion.click();
    await expect(page.getByText('Created task from full suggested brief.')).toBeVisible();
    await expect(page.getByText('1 model turn · 2 tool calls · 128 tokens · 1.2 s elapsed')).toBeVisible();
    await expect(page.locator('.message .mermaid-diagram svg').last()).toBeVisible();
    if (route === '/chat') {
      await expectChatNavbarPinned(page);
      const clearRequest = page.waitForRequest((request) =>
        request.url().endsWith('/api/chat/clear') && request.method() === 'POST'
      );
      page.once('dialog', async (dialog) => {
        expect(dialog.message()).toContain('Clear this chat?');
        await dialog.accept();
      });
      await page.getByRole('button', { name: 'Clear current chat' }).click();
      const request = await clearRequest;
      const body = request.postDataJSON() as { conversation_id?: string };
      expect(body.conversation_id).toMatch(/^chat_/);
      await expect(page.locator('.message')).toHaveCount(0);
      await expect(page.getByRole('status').getByRole('heading', { name: 'New chat' })).toBeVisible();
    }
  } else if (route === '/tasks') {
    await page.waitForLoadState('networkidle');
    await page.getByPlaceholder('Search tasks').fill('queue');
    const mergeQueue = page.locator('[aria-label="Merge queue"]');
    await expect(mergeQueue).toBeVisible();
    await expect(mergeQueue).toContainText('Merge queue');
    const queueMove = mergeQueue.getByRole('button', {
      name: /Move Queued docs follow-up up in merge queue/
    });
    await expect(queueMove).toBeVisible();
    const autoMerge = mergeQueue.getByRole('switch', {
      name: 'Auto merge reviewed queue-head tasks'
    });
    await expect(autoMerge).toHaveAttribute('aria-checked', 'false');
    await autoMerge.click();
    await expect(autoMerge).toHaveAttribute('aria-checked', 'true');
    await queueMove.click();
    const queueNotice = mobile ? page.locator('.task-pane .queue-notice') : page.locator('.workbench .notice');
    await expect(queueNotice.getByText('Merge queue updated')).toBeVisible();
    await page.locator('.task-row').first().click();
    const taskActions = page.getByRole('region', { name: 'Task actions' });
    await expect(taskActions).toBeVisible();
    await expect(page.getByText('Post-merge restart', { exact: true })).toBeVisible();
    await taskActions.getByRole('button', { name: 'Restart', exact: true }).click();
    await expect(page.locator('.workbench .notice').getByText('task action accepted')).toBeVisible();
    if (mobile) {
      await page.getByRole('button', { name: 'Back to queue' }).click();
      await expect(page.locator('.task-pane')).toBeVisible();
    }
  } else if (route === '/workflows') {
    await page.getByPlaceholder('Search workflows').fill('Deploy');
    await page.getByRole('link', { name: /Deploy homelab dashboard/ }).click();
    await page
      .locator('[aria-label="Workflow actions"]')
      .getByRole('button', { name: 'Run', exact: true })
      .click();
    await expect(page.getByText('workflow started')).toBeVisible();
  } else if (route === '/knowledge') {
    const knowledgeNoticeScope = mobile ? page.getByLabel('Knowledge Space detail') : page.getByLabel('Knowledge Space list');
    if (mobile) {
      await expect(page.getByLabel('Knowledge Space mobile controls')).toBeVisible();
    } else {
      await page.getByPlaceholder('Search spaces').fill('Research');
      await page.getByRole('link', { name: /Research synthesis/ }).click();
    }
    await page.getByRole('tab', { name: 'Sources' }).click();
    await page.locator('details.add-source > summary').click();
    await page.getByLabel('Source title').fill('Review notes');
    await page.getByLabel('Source text').fill('Evidence should stay visible when teams review generated claims.');
    await page.locator('.source-form button[type="submit"]').click();
    await expect(knowledgeNoticeScope.getByText('Source indexed')).toBeVisible();
    await page.getByRole('tab', { name: /Research/ }).click();
    await page.getByRole('group', { name: 'Research action' }).getByLabel('Ask a question').check();
    await page.getByRole('textbox', { name: 'Question' }).fill('How should evidence be reviewed?');
    await page.getByRole('button', { name: 'Ask question' }).click();
    await expect(knowledgeNoticeScope.getByText('Grounded answer saved.')).toBeVisible();
    await expect(page.getByRole('tab', { name: /Reports/ })).toHaveAttribute('aria-selected', 'true');
    const askReport = page.locator('#knowledge-report-kreport_ask');
    await expect(askReport.locator('[aria-label="Report evidence"]')).not.toHaveAttribute('open', '');
    await askReport.locator('[aria-label="Report evidence"] summary').click();
    await expect(askReport.locator('[aria-label="Report evidence"]')).toContainText('[S1]');
    await page.getByRole('tab', { name: /Research/ }).click();
    const newResearch = page.locator('[aria-label="New research"]');
    if ((await newResearch.getAttribute('open')) === null) {
      await newResearch.locator(':scope > summary').click();
    }
    await page.getByRole('group', { name: 'Research action' }).getByLabel('Run research').check();
    await page.locator('#knowledge-panel-runs').getByLabel('Question or research goal').fill('Compare evidence handling');
    await page.getByRole('button', { name: 'Start research' }).click();
    await expect(knowledgeNoticeScope.getByText('Research completed.')).toBeVisible();
    await expect(page.getByRole('article', { name: 'Selected research' })).toContainText('Compare evidence handling');
  } else if (route.startsWith('/docs')) {
    if (mobile) {
      const docsNavigationToggle = page.getByRole('button', { name: 'Expand docs navigation' });
      await expect(docsNavigationToggle).toBeVisible();
      await docsNavigationToggle.click();
    }
    await page.getByRole('searchbox', { name: 'Search documentation' }).fill('remote');
    await expect(page.locator('#docs-list')).toBeVisible();
    await expect(
      page.locator(
        '.mermaid-diagram[data-mermaid-status="pending"], .mermaid-diagram[data-mermaid-status="rendering"]'
      )
    ).toHaveCount(0, { timeout: 15_000 });
  } else if (route === '/terminal') {
    await expect(page.locator('.xterm')).toBeVisible({ timeout: 20_000 });
    await page.getByRole('button', { name: 'Add terminal tab' }).click();
    await expect(page.locator('.terminal-tab')).toHaveCount(2);
  } else if (route === '/healthd') {
    await page.getByRole('button', { name: 'Run checks' }).click();
    await expect(page.getByRole('heading', { name: 'CPU usage' })).toBeVisible();
  } else if (route === '/supervisord') {
    await page.getByRole('button', { name: 'Refresh' }).click();
    await expect(page.getByRole('region', { name: 'Supervised applications' })).toBeVisible();
  }
};

const routes = [
  '/',
  '/assistant',
  '/chat',
  '/tasks',
  '/knowledge',
  '/workflows',
  '/terminal',
  '/docs',
  '/docs/dashboard',
  '/docs/agent-tools',
  '/healthd',
  '/supervisord'
];
const docsRouteTimeoutMs = 120_000;

test.describe('knowledge empty state', () => {
  test.use({ viewport: { width: 480, height: 897 }, isMobile: true, hasTouch: true });

  test('handles a null spaces response and submits page-context help', async ({ page }) => {
    await mockScreenCaptureUnavailable(page);
    const pageErrors: string[] = [];
    let helpTaskBody: any;
    page.on('pageerror', (err) => pageErrors.push(err.message));

    await page.route(/\/api\/knowledge\/spaces$/, async (route) => {
      await route.fulfill({ json: { spaces: null } });
    });
    await page.route(/\/api\/tasks(?:\?.*)?$/, async (route) => {
      if (route.request().method() === 'POST') {
        helpTaskBody = route.request().postDataJSON();
        await route.fulfill({ status: 201, json: { reply: 'created help task' } });
        return;
      }
      await route.fulfill({ json: { tasks: [] } });
    });
    await page.route(/\/api\/approvals(?:\?.*)?$/, async (route) => {
      await route.fulfill({ json: { approvals: [] } });
    });

    await page.goto('/knowledge');
    await expect(page.getByText('No Knowledge Spaces yet.')).toBeVisible();
    await expect(page.getByText('No Knowledge Space selected')).toBeVisible();
    await expect(page.getByText(/response\.spaces is null|Symbol\.iterator/)).toHaveCount(0);
    expect(pageErrors).toEqual([]);

    await page.locator('.help-button').click();
    const dialog = page.locator('dialog.help-dialog');
    await expect(dialog).toBeVisible();
    await dialog.getByRole('textbox', { name: 'More detail' }).fill('Knowledge page showed a raw null spaces error.');
    await dialog.getByRole('button', { name: 'Submit help task' }).click();
    await expect(dialog).not.toBeVisible();

    expect(helpTaskBody.goal).toContain('Dashboard help task from /knowledge');
    expect(helpTaskBody.goal).toContain('Knowledge page showed a raw null spaces error.');
    expect(helpTaskBody.attachments).toHaveLength(1);
    const browserContext = JSON.parse(helpTaskBody.attachments[0].text);
    expect(browserContext.visible_text).toContain('No Knowledge Space selected');
    expect(browserContext.visible_text).not.toMatch(/response\.spaces is null|Symbol\.iterator/);
  });
});

for (const viewport of [
  { name: 'desktop', width: 1440, height: 1000, mobile: false },
  { name: 'mobile', width: 390, height: 844, mobile: true }
]) {
  test.describe(`site workflows on ${viewport.name}`, () => {
    test.use({ viewport: { width: viewport.width, height: viewport.height }, isMobile: viewport.mobile, hasTouch: viewport.mobile });

    for (const route of routes) {
      test(`${route} renders without visual artefacts and supports its core workflow`, async ({ page }, testInfo) => {
        if (route.startsWith('/docs')) {
          testInfo.setTimeout(docsRouteTimeoutMs);
        }
        await mockDashboardApis(page);
        const taskSettingsReady =
          route === '/tasks'
            ? page.waitForResponse(
                (response) =>
                  response.url().endsWith('/api/settings') &&
                  response.request().method() === 'GET'
              )
            : Promise.resolve();
        if (route === '/chat') {
          await seedChatScrollTranscript(page);
        }
        await page.goto(route);
        await taskSettingsReady;
        if (route === '/') {
          await expect(page).toHaveURL(/\/chat$/);
        }
        await exerciseRoute(page, route, viewport.mobile);
        await expectNoVisualArtifacts(page);
        await testInfo.attach(`${viewport.name}-${route.replaceAll('/', '-') || 'root'}.png`, {
          body: await page.screenshot({ fullPage: !route.startsWith('/docs') }),
          contentType: 'image/png'
        });
      });
    }

    if (viewport.mobile) {
      test('mobile SPA navigation to /healthd keeps Health content below the navbar', async ({ page }, testInfo) => {
        await mockDashboardApis(page);
        await page.goto('/chat');

        const chatNav = await openMobileMenu(page);
        await chatNav.getByRole('link', { name: /Tasks/ }).click();
        await expect(page).toHaveURL(/\/tasks$/);
        await expect(page.locator('.task-row')).toHaveCount(3, { timeout: 15_000 });

        const tasksNav = await openMobileMenu(page);
        await tasksNav.getByRole('link', { name: 'Health' }).click();
        await expect(page).toHaveURL(/\/healthd$/);
        await expect(page.locator('.toolbar .status')).toHaveText('healthy');

        await page.getByRole('button', { name: 'Run checks' }).click();
        await expect(page.getByRole('heading', { name: 'CPU usage' })).toBeVisible();

        const metrics = await page.evaluate(() => {
          const navbar = document.querySelector('.navbar');
          const toolbar = document.querySelector('.toolbar');
          const status = document.querySelector('.toolbar .status');
          const navbarRect = navbar?.getBoundingClientRect();
          return {
            navbarPosition: navbar ? getComputedStyle(navbar).position : '',
            navbarBottom: navbarRect?.bottom ?? 0,
            toolbarTop: toolbar?.getBoundingClientRect().top ?? 0,
            statusTop: status?.getBoundingClientRect().top ?? 0,
            bodyWidth: document.body.scrollWidth,
            viewport: window.innerWidth
          };
        });
        expect(metrics.navbarPosition, JSON.stringify(metrics)).toBe('sticky');
        expect(metrics.toolbarTop, JSON.stringify(metrics)).toBeGreaterThanOrEqual(metrics.navbarBottom - 1);
        expect(metrics.statusTop, JSON.stringify(metrics)).toBeGreaterThanOrEqual(metrics.navbarBottom - 1);
        expect(metrics.bodyWidth, JSON.stringify(metrics)).toBeLessThanOrEqual(metrics.viewport + 2);
        await testInfo.attach('mobile-chat-tasks-healthd-navbar.png', {
          body: await page.screenshot({ fullPage: true }),
          contentType: 'image/png'
        });
      });
    }
  });
}
