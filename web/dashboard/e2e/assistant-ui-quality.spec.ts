import AxeBuilder from '@axe-core/playwright';
import { expect, test } from '@playwright/test';
import type { Page } from '@playwright/test';

const now = '2026-04-28T12:00:00.000Z';
const themeStorageKey = 'homelabd.dashboard.theme';

const assistantCatalogue = {
  name: 'Assistant',
  summary:
    'A life-improving operating layer for briefs, planning, research, workflows, memory, and safe action.',
  updated_at: now,
  principles: [{ name: 'Plan before action', summary: 'Preview state-changing work before execution.' }],
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
      summary:
        'Run sourced research for decisions, meetings, purchases, travel, and investigations without hiding evidence.',
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
  id: 'arun_focus',
  status: 'completed',
  decision: 'recommend',
  trigger: { kind: 'schedule', label: 'Scheduled proactive check' },
  autonomy: 'propose',
  goal: 'Review current homelabd state and recommend useful next actions.',
  summary: 'Task and health signals suggest one useful follow-up.',
  changed: ['Reviewed tasks, workflows, health, supervisor, and knowledge spaces.'],
  concerns: [
    {
      title: 'Blocked deploy needs attention',
      detail: 'A blocked task has been waiting for an operator decision.',
      severity: 'warning',
      surface: 'tasks',
      object_id: 'task_blocked',
      object_url: '/tasks?task=task_blocked'
    }
  ],
  opportunities: [
    {
      title: 'Turn the daily scan into a reusable workflow',
      detail: 'The same proactive review can run on a schedule with receipts.',
      severity: 'info',
      surface: 'workflows',
      object_id: 'workflow_daily'
    }
  ],
  recommended_actions: [
    {
      id: 'action_1',
      fingerprint: 'sig_blocked_deploy',
      kind: 'task',
      title: 'Review blocked deploy',
      rationale: 'The deploy is blocked and has a clear operator decision point.',
      priority: 'high',
      risk: 'low',
      target_surface: 'tasks',
      task_goal: 'Review the blocked deploy and decide the next step.',
      contract_id: 'task',
      contract: {
        id: 'task',
        capability: 'tasks',
        action_kind: 'task',
        autonomy_ceiling: 'create_tasks',
        risk: 'low',
        requires_approval: true,
        explanation: 'Task actions are allowed only when grounded in known evidence and bounded by a clear task goal.'
      },
      plan: {
        status: 'approval_required',
        summary: 'Harness prepared this task action for operator approval.',
        requires_approval: true,
        steps: [
          { title: 'Bind recommendation to snapshot evidence', surface: 'tasks', mode: 'check', status: 'passed' },
          { title: 'Create bounded follow-up task', surface: 'tasks', mode: 'mutation', status: 'approval_required' }
        ],
        receipts: [
          { kind: 'contract_checked', message: 'Contract "task" constrained action kind "task".' },
          { kind: 'approval_required', message: 'Operator approval is required before execution.' }
        ]
      },
      status: 'recommended',
      seen_count: 2,
      created_task_id: '',
      snoozed_until: ''
    }
  ],
  route: {
    capability: 'tasks',
    decision: 'propose_task',
    reason: 'The deploy is blocked and has a clear operator decision point.',
    next_step: 'Operator review is required before work is created.',
    autonomy: 'propose',
    requires_approval: true
  },
  compiler: {
    status: 'accepted',
    source: 'model',
    summary: 'Harness accepted the model decision after schema, evidence, safety, and routing checks.',
    checks: ['schema_parse', 'signal_enrichment', 'evidence_citations', 'safe_actions', 'duplicate_actions', 'capability_route'],
    scorecard: {
      score: 96,
      grade: 'high',
      json_valid: true,
      json_repaired: false,
      fallback_used: false,
      signal_count: 2,
      active_signal_count: 2,
      kept_action_count: 1,
      rejected_action_count: 0,
      plan_preview_count: 1
    },
    policy_hints: [
      {
        fingerprint: 'sig_blocked_deploy',
        source: 'tasks',
        kind: 'task_blocked',
        effect: 'boost_new_sightings',
        reason: 'Prior useful feedback makes new sightings more likely to be worth surfacing.',
        seen_count: 2,
        useful_count: 1
      }
    ]
  },
  receipts: [
    {
      kind: 'trigger',
      message: 'Assistant run started from Scheduled proactive check.',
      created_at: now
    },
    {
      kind: 'decision',
      message: 'Recommended 1 actions from 1 concerns and 1 opportunities.',
      created_at: now
    }
  ],
  snapshot: {
    generated_at: now,
    task_counts: { blocked: 1, running: 1, done: 2 },
    attention_tasks: [
      {
        id: 'task_blocked',
        title: 'Blocked deploy',
        status: 'blocked',
        summary: 'Waiting on operator decision.',
        url: '/tasks?task=task_blocked'
      }
    ],
    pending_approvals: 1,
    workflow_counts: { completed: 2, running: 1 },
    recent_workflows: [
      {
        id: 'workflow_daily',
        title: 'Daily review',
        status: 'running',
        summary: 'Scheduled context review.',
        url: '/workflows?workflow=workflow_daily'
      }
    ],
    knowledge_spaces: [
      {
        id: 'kspace_ops',
        title: 'Operations memory',
        summary: '6 sources, 2 reports',
        url: '/knowledge?space=kspace_ops'
      }
    ],
    remote_agent_counts: { online: 2 },
    health: { status: 'warning', items: [{ id: 'disk', title: 'Disk', status: 'warning' }] },
    supervisor: { status: 'healthy', items: [] },
    recent_events: [{ id: 'evt_1', type: 'task.blocked', actor: 'Codex', time: now }]
  },
  provider: 'test-provider',
  model: 'test-model',
  usage: { input_tokens: 90, output_tokens: 30, total_tokens: 120 },
  created_at: now,
  started_at: now,
  finished_at: now,
  updated_at: now
};

const assistantSignal = {
  id: 'sig_chat_quality',
  fingerprint: 'sig_chat_quality',
  source: 'chat',
  kind: 'chat_quality_feedback',
  title: 'Review subpar chat answer',
  detail: 'Operator feedback flagged a poor answer.',
  why_now: 'The operator marked the reply as not useful.',
  severity: 'warning',
  surface: 'chat',
  object_id: 'message_assistant_1',
  object_url: '/chat#message-assistant-1',
  score: 88,
  confidence: 'high',
  priority: 'high',
  action_kind: 'task',
  rationale: 'Poor Assistant replies should feed improvement work.',
  task_goal: 'Review the chat exchange and improve the response path.',
  evidence: [
    {
      source: 'chat',
      kind: 'user_feedback',
      title: 'Operator feedback',
      detail: 'The response was marked as not useful.',
      object_id: 'message_assistant_1',
      weight: 88
    }
  ],
  safe_actions: ['create_task', 'useful', 'snooze', 'dismiss'],
  suggested_next_step: 'Create follow-up work to inspect the exchange.',
  seen_count: 2,
  useful_count: 0,
  first_observed_at: now,
  last_observed_at: now,
  expires_at: '2026-05-05T12:00:00.000Z',
  created_at: now,
  updated_at: now
};

const assistantGoal = {
  id: 'goal_daily_brief',
  title: 'Daily brief',
  objective: 'Keep the daily brief current and point out unanswered mail.',
  details: 'Respect focus blocks, cite evidence, and do not send mail without approval.',
  kind: 'routine',
  execution_mode: 'guided',
  status: 'active',
  priority: 'high',
  autonomy: 'observe',
  cadence: 'daily',
  success_criteria: ['Brief is ready before 08:30.', 'Unanswered mail is surfaced.'],
  constraints: ['Do not send messages without approval.'],
  linked_tasks: ['task_goal_brief'],
  progress_summary: 'Last brief was reviewed; the next check is due.',
  created_by: 'operator',
  created_at: now,
  updated_at: now,
  last_checked_at: now,
  next_check_at: now
};

const blockedGoalID = 'goal_grid_rebuild';
const blockedGoalBlockerTrace = {
  source_type: 'task_report',
  source_id: 'greport_task_goal_grid_core',
  source_url: `/assistant?goal=${blockedGoalID}`,
  goal_id: blockedGoalID,
  phase_id: 'phase_03_parity',
  phase_title: 'Expand feature parity',
  blocking_task_id: 'task_goal_grid_core',
  blocking_task_url: '/tasks?task=task_goal_grid_core',
  review_decision: 'blocked_with_progress',
  reason:
    'Task grid_core reported blocker: npm pack --dry-run cannot run because npm is unavailable in this environment.',
  operator_action: 'Open the blocking task, resolve or accept the blocker, then resume Autopilot.',
  blockers: ['npm pack --dry-run cannot run because npm is unavailable.'],
  followups: ['Install npm in the task environment or accept a different package validation path.'],
  created_at: now
};

const blockedAssistantGoal = {
  id: blockedGoalID,
  title: 'Grid rebuild',
  objective: 'Build a clean-room AG Grid replacement with enterprise behaviours and browser validation.',
  details: 'Work in phases and keep the operator informed when the environment blocks validation.',
  kind: 'build',
  execution_mode: 'autopilot',
  status: 'blocked',
  priority: 'high',
  autonomy: 'create_tasks',
  cadence: 'manual',
  linked_tasks: ['task_goal_grid_core'],
  progress_summary: 'Autopilot is blocked by missing package validation tooling.',
  blocker_trace: blockedGoalBlockerTrace,
  autopilot: {
    status: 'blocked',
    budget_tasks: 500,
    tasks_started: 110,
    current_task_id: 'task_goal_grid_core'
  },
  created_by: 'operator',
  created_at: now,
  updated_at: now,
  last_checked_at: now,
  next_check_at: ''
};

const assistantGoalWatch = {
  id: 'gwatch_daily_mail',
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
  id: 'gnote_daily_progress',
  goal_id: assistantGoal.id,
  kind: 'progress',
  title: 'Progress update',
  body: 'The first brief watch is active and waiting for the next check.',
  created_by: 'assistant',
  created_at: now
};

const assistantGoalAssessment = {
  id: 'gassess_daily_brief',
  goal_id: assistantGoal.id,
  run_id: assistantRun.id,
  status: 'on_track',
  summary: 'Daily brief Goal is ready for the next proactive check.',
  created_at: now
};

const clone = <T>(value: T): T => JSON.parse(JSON.stringify(value)) as T;

const freezeTime = async (page: Page) => {
  await page.addInitScript((fixedNow) => {
    const RealDate = Date;
    class FixedDate extends RealDate {
      constructor(...args: ConstructorParameters<typeof Date>) {
        if (args.length === 0) {
          super(fixedNow);
          return;
        }
        super(...args);
      }
      static now() {
        return new RealDate(fixedNow).getTime();
      }
    }
    globalThis.Date = FixedDate as DateConstructor;
  }, now);
};

const mockShellApis = async (page: Page) => {
  await freezeTime(page);
  await page.route(/\/api\/tasks\/attention\/?(?:\?.*)?$/, async (route) => {
    await route.fulfill({ json: { attention: { red: 0, amber: 0, total: 0 } } });
  });
  await page.route(/\/api\/tasks(?:\?.*)?$/, async (route) => {
    await route.fulfill({ json: { tasks: [] } });
  });
  await page.route(/\/api\/workspaces(?:\?.*)?$/, async (route) => {
    await route.fulfill({
      json: {
        workspaces: [
          {
            id: 'desk:remote1',
            project_id: 'remote1',
            agent_id: 'desk',
            agent_name: 'Desk',
            machine: 'desk.local',
            status: 'online',
            workdir_id: 'remote1',
            workdir: '/srv/remote1',
            repo_url: 'git@example.com:remote1.git',
            branch: 'main',
            labels: ['uat']
          }
        ]
      }
    });
  });
  await page.route(/\/api\/approvals$/, async (route) => {
    await route.fulfill({ json: { approvals: [] } });
  });
};

const mockAssistantApis = async (
  page: Page,
  options: { includeFailedRun?: boolean; includeBlockedGoal?: boolean } = {}
) => {
  await mockShellApis(page);
  const archivedRun = {
    ...clone(assistantRun),
    id: 'arun_archived',
    trigger: { kind: 'event', label: 'Archived proactive check' },
    summary: 'Old decision was kept for audit.',
    archived: true,
    archived_at: now,
    archived_by: 'codex',
    archived_reason: 'No longer required.',
    recommended_actions: [
      {
        ...clone(assistantRun.recommended_actions[0]),
        id: 'action_archived',
        status: 'dismissed'
      }
    ]
  };
  const failedRun = {
    ...clone(assistantRun),
    id: 'arun_failed',
    status: 'failed',
    decision: 'no_op',
    trigger: { kind: 'manual', label: 'Failed proactive check' },
    summary: 'Assistant run failed before it could produce a decision.',
    error: 'assistant run returned invalid JSON: unexpected end of JSON input',
    recommended_actions: [],
    route: {
      capability: 'observe',
      decision: 'diagnose_error',
      reason: 'The run did not produce a valid decision.',
      next_step: 'Archive after the failure is understood.',
      autonomy: 'propose'
    },
    compiler: {
      status: 'fallback',
      source: 'deterministic',
      summary: 'Model output was rejected; deterministic fallback produced the decision.',
      rejections: ['model output rejected: assistant run returned invalid JSON']
    },
    receipts: [
      {
        kind: 'error',
        message: 'assistant run returned invalid JSON: unexpected end of JSON input',
        created_at: now
      }
    ]
  };
  const runs = options.includeFailedRun ? [failedRun, clone(assistantRun), archivedRun] : [clone(assistantRun), archivedRun];
  const signals: any[] = [clone(assistantSignal)];
  const goals: any[] = options.includeBlockedGoal
    ? [clone(assistantGoal), clone(blockedAssistantGoal)]
    : [clone(assistantGoal)];
  const watches: any[] = [clone(assistantGoalWatch)];
  const notes: any[] = [clone(assistantGoalNote)];
  const assessments: any[] = [clone(assistantGoalAssessment)];
  const timelineForGoal = (goal: any) => ({
    goal,
    blocker_trace: goal.blocker_trace,
    watches: watches.filter((watch) => watch.goal_id === goal.id),
    signals: [],
    notes: notes.filter((note) => note.goal_id === goal.id),
    assessments: assessments.filter((assessment) => assessment.goal_id === goal.id)
  });
  const actionIsSettled = (action: any) =>
    ['created_task', 'dismissed', 'snoozed', 'useful', 'skipped', 'failed'].includes(action.status || '');
  const archiveIfSettled = (run: any) => {
    if (!run.archived && run.recommended_actions?.length && run.recommended_actions.every(actionIsSettled)) {
      run.archived = true;
      run.archived_at = now;
      run.archived_by = 'assistant-lifecycle';
      run.archived_reason = 'All recommendations are resolved; no operator action remains.';
      run.updated_at = now;
      run.receipts = [
        ...(run.receipts || []),
        {
          kind: 'run_auto_archived',
          message:
            'Archived by Assistant lifecycle policy. Reason: All recommendations are resolved; no operator action remains.',
          created_at: now
        }
      ];
    }
  };
  await page.route(/\/api\/assistant\/runs(?:\/.*)?(?:\?.*)?$/, async (route) => {
    const url = new URL(route.request().url());
    const parts = url.pathname.split('/').filter(Boolean);
    const actionIndex = parts.indexOf('actions');
    const runID = parts[parts.length - 1] || '';
    if (actionIndex > 0 && route.request().method() === 'POST') {
      const runID = parts[actionIndex - 1];
      const actionID = parts[actionIndex + 1];
      const body = route.request().postDataJSON() as { feedback?: string };
      const run = runs.find((candidate) => candidate.id === runID) || runs[0];
      const action = run.recommended_actions.find((candidate) => candidate.id === actionID);
      if (action) {
        action.status = body.feedback === 'create_task' ? 'created_task' : body.feedback || 'recommended';
        if (body.feedback === 'create_task') {
          action.created_task_id = 'task_from_assistant';
          action.plan = {
            ...(action.plan || {}),
            status: 'executed',
            summary: 'Created task task_from_assistant.',
            requires_approval: false
          };
        }
        if (body.feedback === 'snooze') {
          action.snoozed_until = '2026-04-29T12:00:00.000Z';
        }
      }
      archiveIfSettled(run);
      await route.fulfill({
        json: {
          reply:
            body.feedback === 'create_task'
              ? 'Created task from recommendation.'
              : body.feedback === 'snooze'
                ? 'Snoozed recommendation.'
                : body.feedback === 'dismiss'
                  ? 'Dismissed recommendation.'
                  : 'Marked recommendation as useful.',
          run
        }
      });
      return;
    }
    if (route.request().method() === 'PATCH' && runID && runID !== 'runs') {
      const body = route.request().postDataJSON() as {
        archived?: boolean;
        actor?: string;
        reason?: string;
      };
      const run = runs.find((candidate) => candidate.id === runID) || runs[0];
      run.archived = Boolean(body.archived);
      if (run.archived) {
        run.archived_at = now;
        run.archived_by = body.actor || 'dashboard';
        run.archived_reason = body.reason || '';
      } else {
        delete run.archived_at;
        delete run.archived_by;
        delete run.archived_reason;
      }
      run.updated_at = now;
      run.receipts = [
        ...(run.receipts || []),
        {
          kind: run.archived ? 'run_archived' : 'run_restored',
          message: run.archived ? 'Archived Assistant decision.' : 'Restored Assistant decision.',
          created_at: now
        }
      ];
      await route.fulfill({
        json: {
          reply: run.archived ? 'Archived Assistant decision.' : 'Restored Assistant decision.',
          run
        }
      });
      return;
    }
    if (route.request().method() === 'POST' && url.pathname.endsWith('/assistant/runs')) {
      const created = {
        ...clone(assistantRun),
        id: 'arun_manual',
        trigger: { kind: 'manual', label: 'Operator requested proactive check' },
        summary: 'Manual check found one useful follow-up.',
        updated_at: now
      };
      runs.unshift(created);
      await route.fulfill({ status: 201, json: { reply: 'Assistant run completed.', run: created } });
      return;
    }
    if (runID && runID !== 'runs') {
      await route.fulfill({ json: runs.find((run) => run.id === runID) || runs[0] });
      return;
    }
    await route.fulfill({ json: { runs } });
  });
  await page.route(/\/api\/assistant\/signals(?:\/.*)?(?:\?.*)?$/, async (route) => {
    const url = new URL(route.request().url());
    const fingerprint = decodeURIComponent(url.pathname.split('/').filter(Boolean).pop() || '');
    if (route.request().method() === 'PATCH' && fingerprint && fingerprint !== 'signals') {
      const body = route.request().postDataJSON() as { feedback?: string };
      const signal = signals.find((candidate) => candidate.fingerprint === fingerprint) || signals[0];
      if (body.feedback === 'create_task') {
        signal.created_task_id = 'task_from_signal';
        signal.suppressed = true;
        signal.suppression_reason = 'Task already created for this signal.';
      } else if (body.feedback === 'useful') {
        signal.useful_count = (signal.useful_count || 0) + 1;
        signal.suppressed = true;
        signal.suppression_reason = 'Marked useful; cleared from the active inbox until a new sighting arrives.';
      } else if (body.feedback === 'snooze') {
        signal.suppressed = true;
        signal.suppression_reason = 'Snoozed until 2026-04-29T12:00:00Z.';
        signal.snoozed_until = '2026-04-29T12:00:00Z';
      } else if (body.feedback === 'dismiss') {
        signal.suppressed = true;
        signal.suppression_reason = 'Dismissed by operator feedback.';
      }
      await route.fulfill({
        json: {
          reply:
            body.feedback === 'create_task'
              ? 'Created task from signal.'
              : body.feedback === 'snooze'
                ? 'Snoozed signal.'
                : body.feedback === 'dismiss'
                  ? 'Dismissed signal.'
                  : 'Marked signal as useful.',
          signal
        }
      });
      return;
    }
    await route.fulfill({
      json: { signals: signals.filter((signal) => !signal.suppressed && !signal.created_task_id) }
    });
  });
  await page.route(/\/api\/assistant\/goals(?:\/.*)?(?:\?.*)?$/, async (route) => {
    const url = new URL(route.request().url());
    const parts = url.pathname.split('/').filter(Boolean);
    const goalIndex = parts.indexOf('goals');
    const goalID = goalIndex >= 0 ? parts[goalIndex + 1] || '' : '';
    const suffix = goalIndex >= 0 ? parts[goalIndex + 2] || '' : '';
    const goal = goals.find((candidate) => candidate.id === goalID) || goals[0];

    if (route.request().method() === 'POST' && !goalID) {
      const body = route.request().postDataJSON() as {
        title?: string;
        objective?: string;
        cadence?: string;
        autonomy?: string;
        kind?: string;
        execution_mode?: string;
        autopilot?: { budget_tasks?: number };
        target?: unknown;
      };
      const created = {
        ...clone(assistantGoal),
        id: 'goal_created',
        title: body.title || 'Created Goal',
        objective: body.objective || body.title || 'Keep the new Goal alive.',
        kind: body.kind || 'build',
        execution_mode: body.execution_mode || 'guided',
        autopilot:
          body.execution_mode === 'autopilot'
            ? {
                status: 'ready',
                budget_tasks: body.autopilot?.budget_tasks ?? 1,
                tasks_started: 0,
                current_task_id: ''
              }
            : undefined,
        cadence: body.cadence || 'daily',
        autonomy: body.autonomy || 'observe',
        target: body.target,
        linked_tasks: [],
        progress_summary: 'Goal is waiting for its first Assistant assessment.',
        created_by: 'dashboard',
        created_at: now,
        updated_at: now,
        last_checked_at: '',
        next_check_at: now
      };
      const createdNote = {
        ...clone(assistantGoalNote),
        id: 'gnote_created',
        goal_id: created.id,
        title: 'Goal created',
        body: 'Dashboard created this Goal for proactive review.'
      };
      goals.unshift(created);
      notes.unshift(createdNote);
      await route.fulfill({ status: 201, json: timelineForGoal(created) });
      return;
    }

    if (route.request().method() === 'POST' && suffix === 'autopilot') {
      const action = parts[goalIndex + 3] || '';
      const body = route.request().postDataJSON() as { budget_tasks?: number };
      goal.execution_mode = 'autopilot';
      goal.autopilot = {
        ...(goal.autopilot || { tasks_started: 0 }),
        status:
          action === 'pause'
            ? 'paused'
            : action === 'stop'
              ? 'stopped'
              : action === 'resume' || action === 'start'
                ? 'running'
                : 'ready',
        budget_tasks: body.budget_tasks ?? goal.autopilot?.budget_tasks ?? 4,
        tasks_started: action === 'start' ? Math.max(goal.autopilot?.tasks_started || 0, 1) : goal.autopilot?.tasks_started || 0,
        current_task_id: action === 'start' || action === 'resume' ? 'task_goal_autopilot' : goal.autopilot?.current_task_id || ''
      };
      goal.linked_tasks = Array.from(new Set([...(goal.linked_tasks || []), goal.autopilot.current_task_id].filter(Boolean)));
      goal.updated_at = now;
      notes.unshift({
        ...clone(assistantGoalNote),
        id: `gnote_autopilot_${action}`,
        goal_id: goal.id,
        title: `Autopilot ${action}`,
        body: `Autopilot ${action} recorded for this Goal.`,
        kind: 'autopilot',
        task_id: goal.autopilot.current_task_id || ''
      });
      await route.fulfill({
        json: {
          reply: `Autopilot ${action} recorded.`,
          timeline: timelineForGoal(goal)
        }
      });
      return;
    }

    if (route.request().method() === 'POST' && suffix === 'check') {
      const createdRun = {
        ...clone(assistantRun),
        id: 'arun_goal_check',
        goal_id: goal.id,
        trigger: { kind: 'goal', label: `Goal check: ${goal.title}` },
        goal: goal.objective,
        summary: `Goal check completed for ${goal.title}.`,
        updated_at: now
      };
      runs.unshift(createdRun);
      goal.last_checked_at = now;
      goal.next_check_at = '2026-04-29T12:00:00.000Z';
      goal.progress_summary = `Checked by ${createdRun.id}; next review is scheduled.`;
      goal.updated_at = now;
      assessments.unshift({
        ...clone(assistantGoalAssessment),
        id: 'gassess_goal_check',
        goal_id: goal.id,
        run_id: createdRun.id,
        summary: `Goal check completed for ${goal.title}.`
      });
      await route.fulfill({ json: { reply: 'Goal check completed.', run: createdRun } });
      return;
    }

    if (route.request().method() === 'POST' && suffix === 'watches') {
      const body = route.request().postDataJSON() as { title?: string; condition?: string; cadence?: string; kind?: string };
      const createdWatch = {
        ...clone(assistantGoalWatch),
        id: 'gwatch_created',
        goal_id: goal.id,
        title: body.title || body.condition || 'Created watch',
        kind: body.kind || 'watch',
        cadence: body.cadence || '',
        condition: body.condition || '',
        created_at: now,
        updated_at: now
      };
      watches.unshift(createdWatch);
      await route.fulfill({ status: 201, json: timelineForGoal(goal) });
      return;
    }

    if (route.request().method() === 'POST' && suffix === 'notes') {
      const body = route.request().postDataJSON() as { title?: string; body?: string; kind?: string };
      const createdNote = {
        ...clone(assistantGoalNote),
        id: 'gnote_created_manual',
        goal_id: goal.id,
        title: body.title || 'Goal note',
        body: body.body || '',
        kind: body.kind || 'note',
        created_at: now
      };
      notes.unshift(createdNote);
      await route.fulfill({ status: 201, json: timelineForGoal(goal) });
      return;
    }

    if (route.request().method() === 'PATCH' && goalID) {
      const body = route.request().postDataJSON() as {
        title?: string;
        objective?: string;
        details?: string;
        status?: string;
        kind?: string;
        execution_mode?: string;
        target?: unknown;
        autopilot?: { budget_tasks?: number; status?: string };
        autonomy?: string;
        cadence?: string;
      };
      for (const [key, value] of Object.entries(body)) {
        if (key === 'autopilot') {
          goal.autopilot = {
            ...(goal.autopilot || { tasks_started: 0, status: 'ready', current_task_id: '' }),
            ...(value as Record<string, unknown>)
          };
          if (
            goal.autopilot.status === 'budget_exhausted' &&
            (goal.autopilot.budget_tasks || 1) < 0
          ) {
            goal.autopilot.status = 'running';
          }
          continue;
        }
        if (value !== undefined) {
          goal[key] = value;
        }
      }
      goal.updated_at = now;
      await route.fulfill({ json: timelineForGoal(goal) });
      return;
    }

    if (goalID) {
      await route.fulfill({ json: timelineForGoal(goal) });
      return;
    }

    await route.fulfill({ json: { goals } });
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
};

const expectAssistantReady = async (page: Page) => {
  await expect(page.locator('.assistant-page')).toHaveAttribute('data-ready', 'true');
};

const expectNoAxeViolations = async (page: Page) => {
  const results = await new AxeBuilder({ page }).include('main').analyze();
  expect(
    results.violations.map((violation) => ({
      id: violation.id,
      impact: violation.impact,
      help: violation.help,
      targets: violation.nodes.map((node) => node.target)
    }))
  ).toEqual([]);
};

const expectNoVisualArtifacts = async (page: Page) => {
  const metrics = await page.evaluate(() => {
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
    const escaped = Array.from(document.querySelectorAll('h1,h2,h3,p,a,button,label,span,strong,small'))
      .filter((element) => {
        if (isHidden(element) || element.closest('.nav-measure')) {
          return false;
        }
        const rect = element.getBoundingClientRect();
        return rect.width > 0 && rect.height > 0 && (rect.left < -2 || rect.right > window.innerWidth + 2);
      })
      .map((element) => (element.textContent || element.getAttribute('aria-label') || '').trim());
    const clippedControls = Array.from(document.querySelectorAll('button,a,select,input'))
      .filter((element) => {
        const rect = element.getBoundingClientRect();
        return rect.width > 0 && rect.height > 0 && element.scrollWidth > element.clientWidth + 2;
      })
      .map((element) => (element.textContent || element.getAttribute('aria-label') || '').trim());
    return {
      bodyWidth: document.body.scrollWidth,
      docWidth: document.documentElement.scrollWidth,
      viewport: window.innerWidth,
      escaped,
      clippedControls
    };
  });
  expect(metrics.bodyWidth, JSON.stringify(metrics)).toBeLessThanOrEqual(metrics.viewport + 2);
  expect(metrics.docWidth, JSON.stringify(metrics)).toBeLessThanOrEqual(metrics.viewport + 2);
  expect(metrics.escaped, JSON.stringify(metrics)).toEqual([]);
  expect(metrics.clippedControls, JSON.stringify(metrics)).toEqual([]);
};

const initLightTheme = async (page: Page) => {
  await page.addInitScript((key) => {
    localStorage.setItem(key, 'light');
  }, themeStorageKey);
};

const waitForThemeRuntime = async (page: Page, mode = 'light') => {
  await expect(page.locator('.theme-toggle').first()).toHaveAttribute(
    'data-theme-toggle-ready',
    'true'
  );
  await expect
    .poll(() => page.evaluate(() => document.documentElement.style.colorScheme))
    .toBe(mode);
};

const readyThemeToggle = async (page: Page, name: string | RegExp) => {
  const toggle = page.getByRole('button', { name });
  await expect(toggle).toHaveAttribute('data-theme-toggle-ready', 'true');
  return toggle;
};

const switchToDarkTheme = async (page: Page, mobile: boolean) => {
  if (mobile) {
    await page.getByRole('button', { name: 'Menu' }).click();
  }
  const darkToggle = await readyThemeToggle(page, 'Switch to dark mode');
  await darkToggle.click();
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark');
  await waitForThemeRuntime(page, 'dark');
  if (mobile) {
    await page.getByRole('button', { name: 'Menu' }).click();
  }
};

const collectSurfaceStyles = async (page: Page, selectors: string[]) =>
  page.evaluate((surfaceSelectors) => {
    const parseRuntimeColor = (value: string) => {
      const parts = value.match(/[\d.]+/g)?.map(Number) ?? [0, 0, 0];
      return {
        r: parts[0] ?? 0,
        g: parts[1] ?? 0,
        b: parts[2] ?? 0
      };
    };
    const luminance = ({ r, g, b }: ReturnType<typeof parseRuntimeColor>) => {
      const channels = [r, g, b].map((channel) => {
        const normalized = channel / 255;
        return normalized <= 0.03928
          ? normalized / 12.92
          : ((normalized + 0.055) / 1.055) ** 2.4;
      });
      return 0.2126 * channels[0] + 0.7152 * channels[1] + 0.0722 * channels[2];
    };
    const contrast = (left: number, right: number) => {
      const [lighter, darker] = left >= right ? [left, right] : [right, left];
      return (lighter + 0.05) / (darker + 0.05);
    };

    return surfaceSelectors.map((selector) => {
      const element = document.querySelector(selector) as HTMLElement | null;
      if (!element) {
        return { selector, found: false, visible: false, backgroundLuminance: 0, contrast: 0 };
      }
      const rect = element.getBoundingClientRect();
      const style = getComputedStyle(element);
      const backgroundLuminance = luminance(parseRuntimeColor(style.backgroundColor));
      const textLuminance = luminance(parseRuntimeColor(style.color));
      return {
        selector,
        found: true,
        visible: rect.width > 0 && rect.height > 0 && style.visibility !== 'hidden',
        backgroundLuminance,
        contrast: contrast(backgroundLuminance, textLuminance)
      };
    });
  }, selectors);

const expectAssistantThemeSurfaces = async (page: Page, mode: 'light' | 'dark') => {
  const styles = await collectSurfaceStyles(page, [
    '.assistant-workbench',
    '.assistant-record .record-header',
    '.decision-panel',
    '.route-strip',
    '.recommendation-card',
    '.detail-section'
  ]);
  for (const style of styles) {
    expect(style.found, `${mode} missing ${style.selector}`).toBe(true);
    expect(style.visible, `${mode} hidden ${style.selector}`).toBe(true);
    if (mode === 'dark') {
      expect(style.backgroundLuminance, `${mode} ${JSON.stringify(style)}`).toBeLessThan(0.16);
    } else {
      expect(style.backgroundLuminance, `${mode} ${JSON.stringify(style)}`).toBeGreaterThan(0.72);
    }
    expect(style.contrast, `${mode} ${JSON.stringify(style)}`).toBeGreaterThan(3);
  }
};

const frameRecommendationScreenshot = async (page: Page, mobile: boolean) => {
  if (!mobile) {
    return;
  }
  await page.locator('.recommendation-section').evaluate((element) => {
    const navbarBottom = document.querySelector('.navbar')?.getBoundingClientRect().bottom || 0;
    const top = element.getBoundingClientRect().top + window.scrollY - navbarBottom - 8;
    window.scrollTo({ top: Math.max(0, top) });
  });
};

for (const viewport of [
  { name: 'desktop', width: 1440, height: 1000, mobile: false },
  { name: 'mobile', width: 390, height: 844, mobile: true }
]) {
  test.describe(`Assistant UI quality on ${viewport.name}`, () => {
    test.use({
      viewport: { width: viewport.width, height: viewport.height },
      isMobile: viewport.mobile,
      hasTouch: viewport.mobile
    });

    test('selects useful outcomes and keeps the detail usable', async ({ page }) => {
      await initLightTheme(page);
      await mockAssistantApis(page);
      await page.goto('/assistant');
      await expectAssistantReady(page);
      await waitForThemeRuntime(page, 'light');

      await expect(page.getByRole('heading', { name: '1 decision' })).toBeVisible();
      const runTotals = page.getByLabel('Assistant run totals');
      await expect(runTotals.getByText('1 active', { exact: true })).toBeVisible();
      await expect(runTotals.getByText('1 archived', { exact: true })).toBeVisible();
      await expect(runTotals.getByText('1 open', { exact: true })).toBeVisible();
      await expect(runTotals.getByText('1 signals', { exact: true })).toBeVisible();
      const goalsPanel = page.getByLabel('Assistant Goals');
      await expect(goalsPanel.getByRole('heading', { name: 'Goals' })).toBeVisible();
      await expect(goalsPanel.getByText('1 active / 1 due')).toBeVisible();
      await expect(goalsPanel.getByText('Daily brief')).toBeVisible();
      await goalsPanel.getByRole('button', { name: /Daily brief/ }).click();
      const selectedGoalRegion = page.getByLabel('Selected Assistant Goal');
      await expect(selectedGoalRegion.getByRole('heading', { name: 'Daily brief' })).toBeVisible();
      await expect(selectedGoalRegion.getByLabel('Goal objective')).toContainText(
        'Keep the daily brief current'
      );
      await expect(selectedGoalRegion.getByLabel('Goal watches')).toContainText('Morning mail watch');
      if (viewport.mobile) {
        await page.getByRole('button', { name: 'Back to Goal list' }).click();
      }
      await goalsPanel.getByRole('button', { name: 'Create Goal' }).click();
      const goalForm = goalsPanel.locator('form[aria-label="Create Assistant Goal"]');
      await goalForm.getByLabel('Title').fill('Inbox follow-up');
      await goalForm.getByLabel('Objective').fill('Keep unanswered inbox items visible until resolved.');
      await goalForm.getByLabel('Goal type').selectOption('build');
      await goalForm.getByLabel('Execution mode').selectOption('autopilot');
      await expect(goalForm.getByLabel('Autopilot task limit')).toHaveAttribute('min', '-1');
      await goalForm.getByLabel('Autopilot task limit').fill('4');
      await goalForm.getByLabel('Autonomy').selectOption('create_tasks');
      await goalForm.getByLabel('Details').fill('Create bounded tasks only when a response needs work.');
      await goalForm.getByRole('button', { name: 'Create Goal' }).click();
      const createdGoalRegion = page.getByLabel('Selected Assistant Goal');
      await expect(createdGoalRegion).toContainText('Keep unanswered inbox items visible until resolved.');
      await expect(createdGoalRegion).toContainText('Build Goal / Autopilot mode');
      await expect(createdGoalRegion).toContainText('Autopilot Ready / 0/4 tasks');
      await createdGoalRegion.getByRole('button', { name: 'Edit Goal' }).click();
      const editGoalForm = createdGoalRegion.locator('form[aria-label="Edit Assistant Goal"]');
      await expect(editGoalForm.getByLabel('Title')).toHaveValue('Inbox follow-up');
      await editGoalForm
        .getByLabel('Objective')
        .fill('Keep unanswered inbox items visible until resolved and assigned.');
      await editGoalForm.getByLabel('Autopilot task limit').fill('-1');
      await editGoalForm.getByRole('button', { name: 'Save Goal' }).click();
      if (!viewport.mobile) {
        await expect(goalsPanel.getByRole('status')).toContainText('Goal saved.');
      }
      await expect(editGoalForm).toHaveCount(0);
      await expect(createdGoalRegion).toContainText('Keep unanswered inbox items visible until resolved and assigned.');
      await expect(createdGoalRegion).toContainText('Autopilot Ready / 0/unlimited tasks');
      await createdGoalRegion.getByRole('button', { name: 'Start Autopilot' }).click();
      await expect(createdGoalRegion).toContainText('Autopilot Running');
      await expect(createdGoalRegion).toContainText('Autopilot Running / 1/unlimited tasks');
      await expect(createdGoalRegion.getByLabel('Goal linked tasks')).toContainText('Autopilot task');
      if (viewport.mobile) {
        await page.getByRole('button', { name: 'Back to Goal list' }).click();
      }
      await expect(goalsPanel.getByRole('status')).toContainText('Autopilot start recorded.');
      await expect(goalsPanel.getByText('Inbox follow-up')).toBeVisible();
      await expect(page.getByRole('heading', { name: 'Signal inbox' })).toBeVisible();
      await expect(page.getByText('Review subpar chat answer')).toBeVisible();
      const signalActions = page.getByRole('group', { name: 'Signal actions for Review subpar chat answer' });
      await signalActions.getByRole('button', { name: 'Useful' }).click();
      await expect(page.getByRole('status', { name: 'Assistant signal status' })).toContainText(
        'Marked signal as useful.'
      );
      await page.getByRole('button', { name: 'Clear Assistant signal notice' }).click();
      await expect(page.getByRole('status', { name: 'Assistant signal status' })).toHaveCount(0);
      await expect(signalActions).toHaveCount(0);
      await expect(runTotals.getByText('0 signals', { exact: true })).toBeVisible();
      await expect(page.getByText('Signals from chat, tasks, health, workflows, and future sources will appear here.')).toBeVisible();
      const decisionSpaces = page.getByLabel('Assistant decision spaces');
      await expect(decisionSpaces.getByRole('button', { name: /Active/ })).toHaveAttribute(
        'aria-pressed',
        'true'
      );
      await decisionSpaces.getByRole('button', { name: /Archived/ }).click();
      await expect(page).toHaveURL(/\/assistant\?view=archived$/);
      await expect(page.getByRole('link', { name: /Archived proactive check/ })).toBeVisible();
      await decisionSpaces.getByRole('button', { name: /Active/ }).click();
      await expect(page).toHaveURL(/\/assistant$/);
      await expect(page.getByRole('link', { name: 'Open Assistant documentation' })).toHaveAttribute(
        'href',
        '/docs/dashboard#assistant'
      );
      await expect(page.getByText('Capabilities, triggers, and safeguards')).toBeVisible();
      await expect(page.getByText('Assistant reference')).toHaveCount(0);
      if (viewport.mobile) {
        await expect(page.locator('.run-row.selected').first()).toHaveCSS('box-shadow', /inset/);
      }
      await page.getByRole('link', { name: /Scheduled proactive check/ }).click();
      await expect(page).toHaveURL(/\/assistant\?run=arun_focus$/);
      await page.goBack();
      await expect(page).toHaveURL(/\/assistant$/);
      await expect(page.getByRole('link', { name: /Scheduled proactive check/ })).toBeVisible();
      await page.getByRole('link', { name: /Scheduled proactive check/ }).click();
      await expect(page).toHaveURL(/\/assistant\?run=arun_focus$/);
      await expect(page.getByRole('heading', { name: 'Scheduled proactive check' })).toBeInViewport();
      await expect(page.getByRole('heading', { name: '1 recommendation to decide' })).toBeVisible();
      await expect(page.getByRole('heading', { name: 'Recommended actions' })).toBeVisible();
      await expect(page.getByLabel('Assistant capability route')).toContainText('Tasks');
      await expect(page.getByLabel('Assistant decision compiler')).toContainText('Accepted decision');
      await expect(page.getByLabel('Assistant decision compiler')).toContainText('96/100 high');
      await expect(page.getByLabel('Assistant decision compiler')).toContainText('Prior useful feedback');
      const recommendationCard = page.locator('.recommendation-card').filter({ hasText: 'Review blocked deploy' });
      await expect(recommendationCard).toBeVisible();
      await expect(recommendationCard).toContainText('2 sightings');
      await expect(recommendationCard).toContainText('task / approval required / low risk');
      await expect(recommendationCard).toContainText('Harness prepared this task action for operator approval.');
      await expect(page.getByRole('heading', { name: 'What it noticed' })).toBeVisible();
      const selectedRunRegion = page.getByLabel('Selected Assistant run');
      const recommendationActions = page.getByRole('group', {
        name: 'Recommendation actions for Review blocked deploy'
      });
      await recommendationActions.getByRole('button', { name: 'Useful' }).click();
      await expect(selectedRunRegion.getByRole('status', { name: 'Assistant run status' })).toContainText(
        'Marked recommendation as useful.'
      );
      await selectedRunRegion.getByRole('button', { name: 'Clear Assistant run notice' }).click();
      await expect(selectedRunRegion.getByRole('status', { name: 'Assistant run status' })).toHaveCount(0);
      await expect(page).toHaveURL(/\/assistant\?view=archived&run=arun_focus$/);
      await expect(selectedRunRegion.getByRole('heading', { name: 'Archived decision', exact: true })).toBeVisible();
      await expect(page.getByText('Marked useful')).toBeVisible();
      await expect(recommendationActions.getByRole('button', { name: 'Useful' })).toBeDisabled();
      await expectNoVisualArtifacts(page);
      await expectNoAxeViolations(page);
      await expectAssistantThemeSurfaces(page, 'light');
      await frameRecommendationScreenshot(page, viewport.mobile);
      await expect(page).toHaveScreenshot(`assistant-proactive-actions-light-${viewport.name}.png`, {
        fullPage: !viewport.mobile,
        animations: 'disabled'
      });

      await switchToDarkTheme(page, viewport.mobile);
      await expectAssistantThemeSurfaces(page, 'dark');
      await expectNoVisualArtifacts(page);
      await expectNoAxeViolations(page);
      await frameRecommendationScreenshot(page, viewport.mobile);
      await expect(page).toHaveScreenshot(`assistant-proactive-actions-dark-${viewport.name}.png`, {
        fullPage: !viewport.mobile,
        animations: 'disabled'
      });

      if (!viewport.mobile) {
        const lightToggle = await readyThemeToggle(page, 'Switch to light mode');
        await lightToggle.click();
        await expect(page.locator('html')).toHaveAttribute('data-theme', 'light');
        await waitForThemeRuntime(page, 'light');
      }

      await expect(page.getByRole('button', { name: 'Restore Assistant decision' })).toBeVisible();
      await page.getByRole('button', { name: 'Restore Assistant decision' }).click();
      await expect(page.getByRole('status', { name: 'Assistant run status' })).toContainText(
        'Restored Assistant decision.'
      );
      await expect(page).toHaveURL(/\/assistant\?run=arun_focus$/);

      if (viewport.mobile) {
        await page.getByRole('button', { name: 'Back to runs' }).click();
        await expect(page).toHaveURL(/\/assistant$/);
      }
      await page.getByRole('button', { name: 'Run proactive Assistant check' }).click();
      await expect(page).toHaveURL(/\/assistant\?run=arun_manual$/);
      await expect(page.getByRole('status', { name: 'Assistant run status' })).toContainText(
        'Assistant run completed.'
      );
      await expect(page.getByRole('heading', { name: 'Operator requested proactive check' })).toBeInViewport();
      const manualActions = page.getByRole('group', {
        name: 'Recommendation actions for Review blocked deploy'
      });
      await manualActions.getByRole('button', { name: 'Create task' }).click();
      await expect(page.getByRole('status', { name: 'Assistant run status' })).toContainText(
        'Created task from recommendation.'
      );
      await page.getByRole('button', { name: 'Clear Assistant run notice' }).click();
      await expect(page.getByRole('status', { name: 'Assistant run status' })).toHaveCount(0);
      await expect(page).toHaveURL(/\/assistant\?view=archived&run=arun_manual$/);
      await expect(page.getByRole('link', { name: 'Open created task' })).toHaveAttribute(
        'href',
        '/tasks?task=task_from_assistant'
      );
      if (viewport.mobile) {
        await page.getByRole('button', { name: 'Back to runs' }).click();
        await expect(page).toHaveURL(/\/assistant\?view=archived$/);
      }
      await expect(page.getByRole('link', { name: 'Open Assistant documentation' })).toBeVisible();

      await expectNoVisualArtifacts(page);
      await expectNoAxeViolations(page);
      await page.evaluate(() => window.scrollTo({ top: 0 }));
      await expect(page).toHaveScreenshot(`assistant-ui-quality-${viewport.name}.png`, {
        fullPage: !viewport.mobile,
        animations: 'disabled'
      });
    });

    test('keeps blocked Goal actions readable beside long blocker text', async ({ page }) => {
      await initLightTheme(page);
      await mockAssistantApis(page, { includeBlockedGoal: true });
      await page.goto(`/assistant?goal=${blockedGoalID}`);
      await expectAssistantReady(page);

      const selectedGoalRegion = page.locator('article[aria-label="Selected Assistant Goal"]');
      await expect(selectedGoalRegion).toContainText('Grid rebuild');
      const blockerTrace = page.getByLabel('Goal blocker trace');
      await expect(blockerTrace).toBeVisible();
      await expect(blockerTrace).toContainText('Blocked by task');
      await expect(blockerTrace).toContainText('npm pack --dry-run cannot run');
      await expect(blockerTrace.getByRole('link', { name: 'Open blocking task' })).toHaveAttribute(
        'href',
        '/tasks?task=task_goal_grid_core'
      );
      await expect(blockerTrace.getByRole('button', { name: 'Check Goal now' })).toBeVisible();
      await expect(blockerTrace.getByRole('button', { name: 'Resume Autopilot' })).toBeVisible();

      const layout = await blockerTrace.evaluate((panel) => {
        const panelRect = panel.getBoundingClientRect();
        const copyRect = panel.querySelector(':scope > div:first-child')?.getBoundingClientRect();
        const actionRects = Array.from(panel.querySelectorAll('.notice-action')).map((action) => {
          const rect = action.getBoundingClientRect();
          return {
            label: (action.textContent || action.getAttribute('aria-label') || '').trim(),
            left: rect.left,
            right: rect.right,
            top: rect.top,
            bottom: rect.bottom,
            width: rect.width
          };
        });
        const intersects = (left: DOMRect, right: (typeof actionRects)[number]) =>
          left.left < right.right &&
          left.right > right.left &&
          left.top < right.bottom &&
          left.bottom > right.top;
        return {
          panelWidth: panelRect.width,
          outOfBounds: actionRects
            .filter(
              (rect) =>
                rect.left < panelRect.left - 1 ||
                rect.right > panelRect.right + 1 ||
                rect.top < panelRect.top - 1 ||
                rect.bottom > panelRect.bottom + 1
            )
            .map((rect) => rect.label),
          overlapsCopy: copyRect ? actionRects.filter((rect) => intersects(copyRect, rect)).map((rect) => rect.label) : [],
          actionWidths: actionRects.map((rect) => rect.width)
        };
      });
      expect(layout.outOfBounds, JSON.stringify(layout)).toEqual([]);
      expect(layout.overlapsCopy, JSON.stringify(layout)).toEqual([]);
      if (viewport.mobile) {
        expect(
          layout.actionWidths.every((width) => width >= layout.panelWidth - 28),
          JSON.stringify(layout)
        ).toBe(true);
        const headerLayout = await selectedGoalRegion.locator('.record-header').evaluate((header) => {
          const visibleChildren = Array.from(header.children)
            .map((child) => child.getBoundingClientRect())
            .filter((rect) => rect.width > 0 && rect.height > 0)
            .sort((left, right) => left.top - right.top);
          const gaps = visibleChildren.slice(1).map((rect, index) => rect.top - visibleChildren[index].bottom);
          return { gaps, maxGap: Math.max(0, ...gaps) };
        });
        expect(headerLayout.maxGap, JSON.stringify(headerLayout)).toBeLessThanOrEqual(18);
      }

      await expectNoVisualArtifacts(page);
      await expectNoAxeViolations(page);
      await blockerTrace.scrollIntoViewIfNeeded();
      await expect(blockerTrace).toHaveScreenshot(`assistant-goal-blocker-${viewport.name}.png`, {
        animations: 'disabled'
      });
    });

    test('archives active failed runs from the failure notice', async ({ page }) => {
      await initLightTheme(page);
      await mockAssistantApis(page, { includeFailedRun: true });
      await page.goto('/assistant?run=arun_failed');
      await expectAssistantReady(page);

      await expect(page.getByRole('heading', { name: 'Failed proactive check' })).toBeVisible();
      const selectedRunRegion = page.getByLabel('Selected Assistant run');
      await expect(selectedRunRegion.getByRole('alert')).toContainText('invalid JSON');
      await expect(selectedRunRegion.getByLabel('Assistant decision compiler')).toContainText('Deterministic fallback');
      await selectedRunRegion.getByRole('button', { name: 'Archive', exact: true }).click();
      await expect(page).toHaveURL(/\/assistant\?view=archived&run=arun_failed$/);
      await expect(selectedRunRegion.getByRole('status', { name: 'Assistant run status' })).toContainText(
        'Archived Assistant decision.'
      );
      await expect(selectedRunRegion.getByRole('button', { name: 'Restore Assistant decision' })).toBeVisible();
    });

    test('keeps capability reference in docs rather than page controls', async ({ page }) => {
      await initLightTheme(page);
      await mockAssistantApis(page);
      await page.goto('/assistant');
      await expectAssistantReady(page);

      await expect(page.getByText('Assistant reference')).toHaveCount(0);
      await expect(page.getByRole('searchbox', { name: 'Search' })).toHaveCount(0);
      await expect(page.getByText('No capabilities match this view.')).toHaveCount(0);
      await expect(page.getByRole('link', { name: 'Open Assistant documentation' })).toHaveAttribute(
        'href',
        '/docs/dashboard#assistant'
      );
    });
  });
}
