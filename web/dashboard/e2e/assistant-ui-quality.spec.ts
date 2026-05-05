import AxeBuilder from '@axe-core/playwright';
import { expect, test } from '@playwright/test';
import type { Page } from '@playwright/test';

const now = '2026-04-28T12:00:00.000Z';

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
      status: 'recommended',
      seen_count: 2,
      created_task_id: '',
      snoozed_until: ''
    }
  ],
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
  await page.route(/\/api\/tasks(?:\?.*)?$/, async (route) => {
    await route.fulfill({ json: { tasks: [] } });
  });
  await page.route(/\/api\/approvals$/, async (route) => {
    await route.fulfill({ json: { approvals: [] } });
  });
};

const mockAssistantApis = async (page: Page) => {
  await mockShellApis(page);
  const runs = [clone(assistantRun)];
  await page.route(/\/api\/assistant\/runs(?:\/.*)?(?:\?.*)?$/, async (route) => {
    const url = new URL(route.request().url());
    const parts = url.pathname.split('/').filter(Boolean);
    const actionIndex = parts.indexOf('actions');
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
        }
        if (body.feedback === 'snooze') {
          action.snoozed_until = '2026-04-29T12:00:00.000Z';
        }
      }
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
    const runID = parts[parts.length - 1] || '';
    if (runID && runID !== 'runs') {
      await route.fulfill({ json: runs.find((run) => run.id === runID) || runs[0] });
      return;
    }
    await route.fulfill({ json: { runs } });
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
      await mockAssistantApis(page);
      await page.goto('/assistant');
      await expectAssistantReady(page);

      await expect(page.getByRole('heading', { name: 'Useful outcomes' })).toBeVisible();
      await expect(page.getByRole('heading', { name: 'Runs' })).toBeVisible();
      await expect(page.getByText('2 capabilities')).toBeVisible();
      await page.getByRole('button', { name: /Scheduled proactive check/ }).click();
      await expect(page.getByRole('heading', { name: 'Scheduled proactive check' })).toBeInViewport();
      await expect(page.getByRole('heading', { name: 'Recommended actions' })).toBeVisible();
      await expect(page.getByText('Review blocked deploy')).toBeVisible();
      await expect(page.getByText('2 sightings')).toBeVisible();
      const recommendationActions = page.getByRole('group', {
        name: 'Recommendation actions for Review blocked deploy'
      });
      await recommendationActions.getByRole('button', { name: 'Useful' }).click();
      await expect(page.getByRole('status')).toContainText('Marked recommendation as useful.');
      await expect(page.getByText('Marked useful')).toBeVisible();
      await recommendationActions.getByRole('button', { name: 'Create task' }).click();
      await expect(page.getByRole('status')).toContainText('Created task from recommendation.');
      await expect(page.getByRole('link', { name: 'Open created task' })).toHaveAttribute(
        'href',
        '/tasks?task=task_from_assistant'
      );
      await expectNoVisualArtifacts(page);
      await expectNoAxeViolations(page);
      await expect(page).toHaveScreenshot(`assistant-proactive-actions-${viewport.name}.png`, {
        fullPage: !viewport.mobile,
        animations: 'disabled'
      });
      await page.getByRole('button', { name: 'Run proactive Assistant check' }).click();
      await expect(page.getByRole('status')).toContainText('Assistant run completed.');
      await expect(page.getByRole('heading', { name: 'Operator requested proactive check' })).toBeInViewport();
      await page.getByRole('button', { name: /Research a decision/ }).click();
      await expect(page.getByRole('heading', { name: 'Research and prepare' })).toBeInViewport();
      await expect(page.locator('.detail-header .status')).toHaveText('Plan and propose');
      await expect(page.getByRole('navigation', { name: 'Related assistant surfaces' })).toContainText(
        'Open Chat'
      );
      await expect(page.getByRole('button', { name: /Research and prepare/ })).toHaveAttribute(
        'aria-pressed',
        'true'
      );

      await expectNoVisualArtifacts(page);
      await expectNoAxeViolations(page);
      await expect(page).toHaveScreenshot(`assistant-ui-quality-${viewport.name}.png`, {
        fullPage: !viewport.mobile,
        animations: 'disabled'
      });
    });

    test('keeps filtered empty states recoverable', async ({ page }) => {
      await mockAssistantApis(page);
      await page.goto('/assistant');
      await expectAssistantReady(page);

      const search = page.getByRole('searchbox', { name: 'Search' });
      await search.fill('missing');
      await expect(page.getByText('No capabilities match this view.')).toBeVisible();
      await page.getByRole('button', { name: 'Clear filters' }).first().click();
      await expectAssistantReady(page);
      await expect(search).toHaveValue('');
      await expect(page.getByRole('button', { name: /Research and prepare/ })).toBeVisible();
    });
  });
}
