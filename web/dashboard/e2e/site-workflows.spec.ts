import { expect, test } from '@playwright/test';
import type { Page } from '@playwright/test';

const now = '2026-04-28T12:00:00.000Z';
const taskID = 'task_20260428_120000_11111111';
const workflowID = 'workflow_20260428_120000_22222222';

const task = {
  id: taskID,
  title: 'Review queue behaviour on mobile',
  goal: 'Keep the task queue usable on desktop and mobile.',
  status: 'awaiting_approval',
  assigned_to: 'codex',
  priority: 5,
  created_at: now,
  updated_at: now,
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
  result: 'merged after approval approval_1; post-merge restart pending',
  restart_required: ['dashboard'],
  restart_completed: ['homelabd'],
  restart_status: 'failed',
  restart_current: 'dashboard',
  restart_last_error: 'dashboard health check failed after restart'
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

const mockDashboardApis = async (page: Page) => {
  await installTerminalMocks(page);
  await page.route(/\/api\/message$/, async (route) => {
    await route.fulfill({ json: { reply: 'Status: `tasks` and `workflow list` are available.', source: 'program' } });
  });
  await page.route(/\/api\/tasks$/, async (route) => {
    await route.fulfill({ json: { tasks: [restartTask, task] } });
  });
  await page.route(/\/api\/tasks\/[^/]+\/runs$/, async (route) => {
    await route.fulfill({ json: { runs: [] } });
  });
  await page.route(/\/api\/tasks\/[^/]+\/diff$/, async (route) => {
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
  await page.route(/\/api\/tasks\/[^/]+\/(?:run|review|accept|restart|reopen|cancel|retry|delete)$/, async (route) => {
    await route.fulfill({ json: { reply: 'task action accepted' } });
  });
  await page.route(/\/api\/approvals(?:\/[^/]+\/(?:approve|deny))?$/, async (route) => {
    await route.fulfill({ json: route.request().url().includes('/approve') ? { reply: 'approved' } : { approvals: [] } });
  });
  await page.route(/\/api\/events(?:\?.*)?$/, async (route) => {
    await route.fulfill({ json: { events: [] } });
  });
  await page.route(/\/api\/agents$/, async (route) => {
    await route.fulfill({ json: { agents: [] } });
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
    const contentRoots = Array.from(
      document.querySelectorAll('main, .task-pane, .chat-card, .docs-shell, .workflow-page, .terminal-panel, .app-shell')
    )
      .map((element) => {
        const rect = element.getBoundingClientRect();
        const style = getComputedStyle(element);
        return style.display === 'none' || style.visibility === 'hidden' ? 0 : Math.round(rect.height);
      })
      .filter((height) => height > 0);
    const escaped = Array.from(document.querySelectorAll('h1,h2,h3,p,a,button,summary,label,span,strong'))
      .filter((element) => {
        if (element.closest('.xterm') || hasScrollableXAncestor(element)) {
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
    return {
      bodyWidth: document.body.scrollWidth,
      docWidth: document.documentElement.scrollWidth,
      viewport: window.innerWidth,
      contentHeight: Math.max(...contentRoots, 0),
      escaped,
      clippedButtons
    };
  });
  expect(metrics.bodyWidth, JSON.stringify(metrics)).toBeLessThanOrEqual(metrics.viewport + 2);
  expect(metrics.docWidth, JSON.stringify(metrics)).toBeLessThanOrEqual(metrics.viewport + 2);
  expect(metrics.contentHeight, JSON.stringify(metrics)).toBeGreaterThan(40);
  expect(metrics.escaped, JSON.stringify(metrics)).toEqual([]);
  expect(metrics.clippedButtons, JSON.stringify(metrics)).toEqual([]);
};

const exerciseRoute = async (page: Page, route: string, mobile: boolean) => {
  if (mobile && route !== '/') {
    await page.getByRole('button', { name: 'Menu' }).click();
    await expect(page.getByRole('navigation', { name: 'Primary mobile' })).toBeVisible();
    await page.getByRole('button', { name: 'Menu' }).click();
  }
  if (route === '/' || route === '/chat') {
    await page.getByRole('textbox', { name: 'Message' }).fill('status');
    await page.getByRole('button', { name: 'Send' }).click();
    await expect(page.getByText('Status:')).toBeVisible();
  } else if (route === '/tasks') {
    await page.getByPlaceholder('Search tasks').fill('queue');
    await page.locator('.task-row').first().click();
    const taskActions = page.getByRole('region', { name: 'Task actions' });
    await expect(taskActions).toBeVisible();
    await expect(page.getByText('Post-merge restart', { exact: true })).toBeVisible();
    await taskActions.getByRole('button', { name: 'Restart', exact: true }).click();
    await expect(page.getByText('task action accepted')).toBeVisible();
    if (mobile) {
      await page.getByRole('button', { name: 'Back to queue' }).click();
      await expect(page.locator('.task-pane')).toBeVisible();
    }
  } else if (route === '/workflows') {
    await page.getByPlaceholder('Search workflows').fill('Deploy');
    await page.getByRole('button', { name: /Deploy homelab dashboard/ }).click();
    await page
      .locator('[aria-label="Workflow actions"]')
      .getByRole('button', { name: 'Run', exact: true })
      .click();
    await expect(page.getByText('workflow started')).toBeVisible();
  } else if (route.startsWith('/docs')) {
    await page.getByRole('searchbox', { name: 'Search documentation' }).fill('remote');
    await expect(page.locator('#docs-list')).toBeVisible();
  } else if (route === '/terminal') {
    await expect(page.locator('.xterm')).toBeVisible();
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

const routes = ['/', '/chat', '/tasks', '/workflows', '/terminal', '/docs', '/docs/dashboard', '/healthd', '/supervisord'];

for (const viewport of [
  { name: 'desktop', width: 1440, height: 1000, mobile: false },
  { name: 'mobile', width: 390, height: 844, mobile: true }
]) {
  test.describe(`site workflows on ${viewport.name}`, () => {
    test.use({ viewport: { width: viewport.width, height: viewport.height }, isMobile: viewport.mobile, hasTouch: viewport.mobile });

    for (const route of routes) {
      test(`${route} renders without visual artefacts and supports its core workflow`, async ({ page }, testInfo) => {
        await mockDashboardApis(page);
        await page.goto(route);
        if (route === '/') {
          await expect(page).toHaveURL(/\/chat$/);
        }
        await exerciseRoute(page, route, viewport.mobile);
        await expectNoVisualArtifacts(page);
        await testInfo.attach(`${viewport.name}-${route.replaceAll('/', '-') || 'root'}.png`, {
          body: await page.screenshot({ fullPage: true }),
          contentType: 'image/png'
        });
      });
    }
  });
}
