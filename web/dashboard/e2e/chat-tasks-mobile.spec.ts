import { expect, test } from '@playwright/test';
import type { Page } from '@playwright/test';

const typedMessage = 'mobile input must keep every typed character 12345';
const taskLoadTimeoutMs = 15_000;
const sleep = (milliseconds: number) => new Promise((resolve) => setTimeout(resolve, milliseconds));

const mockScreenCapture = async (page: Page) => {
  await page.addInitScript(() => {
    const mediaDevices = navigator.mediaDevices || {};
    Object.defineProperty(navigator, 'mediaDevices', {
      configurable: true,
      value: {
        ...mediaDevices,
        getDisplayMedia: async () => new MediaStream()
      }
    });
    Object.defineProperty(HTMLVideoElement.prototype, 'videoWidth', {
      configurable: true,
      get: () => 320
    });
    Object.defineProperty(HTMLVideoElement.prototype, 'videoHeight', {
      configurable: true,
      get: () => 180
    });
    HTMLMediaElement.prototype.play = async () => undefined;
    CanvasRenderingContext2D.prototype.drawImage = () => undefined;
    HTMLCanvasElement.prototype.toDataURL = () => 'data:image/png;base64,AAAA';
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

const mockTaskApi = async (
  page: Page,
  onCreateTask?: (body: any) => void,
  extraTasks: any[] = [],
  options: { taskListDelayMs?: (requestCount: number) => number } = {}
) => {
  const now = new Date('2026-04-26T15:00:00Z').toISOString();
  const plan = {
    status: 'reviewed',
    summary: 'Plan to keep the task queue visible on mobile.',
    steps: [
      { title: 'Inspect mobile layout', detail: 'Check task queue and selected record behavior.' },
      { title: 'Validate in browser', detail: 'Run a mobile viewport check after changes.' }
    ],
    risks: ['Mobile layout can regress when queue state changes.'],
    review: 'Plan includes inspect and browser validation stages.',
    created_at: now,
    reviewed_at: now
  };
  const tasks = [
    {
      id: 'task_20260426_150000_11111111',
      title: 'Review queue behavior on mobile',
      goal: 'Keep the task queue visible after selecting a task on a narrow screen.',
      status: 'awaiting_approval',
      assigned_to: 'codex',
      priority: 5,
      created_at: now,
      updated_at: now,
      result: 'waiting for approval',
      attachments: [
        {
          id: 'att_context',
          name: 'browser-context.json',
          content_type: 'application/json',
          size: 17,
          text: '{"url":"/tasks"}'
        }
      ],
      plan
    },
    {
      id: 'task_20260426_150100_22222222',
      title: 'Resolve blocked dashboard check',
      goal: 'Investigate the failed dashboard check.',
      status: 'failed',
      assigned_to: 'codex',
      priority: 5,
      created_at: now,
      updated_at: now,
      result: 'check failed'
    },
    {
      id: 'task_20260426_150200_22222222',
      title: 'Run dashboard checks',
      goal: 'Run check and browser tests for the dashboard.',
      status: 'running',
      assigned_to: 'codex',
      priority: 5,
      created_at: now,
      updated_at: now
    },
    {
      id: 'task_20260426_150300_33333333',
      title: 'Document mobile task flow',
      goal: 'Update the dashboard documentation for the mobile task queue.',
      status: 'done',
      assigned_to: 'codex',
      priority: 5,
      created_at: now,
      updated_at: now,
      result: 'complete'
    }
  ];
  const taskRecords = [...tasks, ...extraTasks];

  let taskListReads = 0;
  await page.context().route(/\/api\/tasks(?:\?.*)?$/, async (route) => {
    if (route.request().method() === 'POST') {
      onCreateTask?.(JSON.parse(route.request().postData() || '{}'));
      await route.fulfill({ status: 201, json: { reply: 'created help task' } });
      return;
    }
    taskListReads += 1;
    const delayMs = options.taskListDelayMs?.(taskListReads) || 0;
    if (delayMs > 0) {
      await sleep(delayMs);
    }
    await route.fulfill({ json: { tasks: taskRecords } });
  });
  let autoMergeEnabled = false;
  await page.route(/\/api\/settings$/, async (route) => {
    if (route.request().method() === 'POST') {
      const body = JSON.parse(route.request().postData() || '{}') as {
        auto_merge_enabled?: boolean;
      };
      autoMergeEnabled = Boolean(body.auto_merge_enabled);
    }
    await route.fulfill({ json: { settings: { auto_merge_enabled: autoMergeEnabled } } });
  });
  await page.route(/\/api\/approvals$/, async (route) => {
    await route.fulfill({ json: { approvals: [approvalFor(tasks[0].id)] } });
  });
  await page.route(/\/api\/events(?:\?.*)?$/, async (route) => {
    await route.fulfill({ json: { events: [] } });
  });
  await page.route(/\/api\/agents$/, async (route) => {
    await route.fulfill({ json: { agents: [] } });
  });
  await page.route(/\/api\/tasks\/[^/]+\/runs$/, async (route) => {
    await route.fulfill({ json: { runs: [] } });
  });
  await page.route(/\/api\/tasks\/[^/]+\/diff$/, async (route) => {
    await route.fulfill({
      json: {
        task_id: tasks[0].id,
        raw_diff: '',
        summary: { files: 0, additions: 0, deletions: 0 },
        files: [],
        generated_at: now
      }
    });
  });
};

const mockAcceptTaskApi = async (page: Page) => {
  const now = new Date('2026-04-26T15:10:00Z').toISOString();
  const taskID = 'task_20260426_151000_44444444';
  let accepted = false;
  const currentTask = () => ({
    id: taskID,
    title: 'Verify accepted task toast placement',
    goal: 'Accepting the task should not move the back-to-queue control.',
    status: accepted ? 'done' : 'awaiting_verification',
    assigned_to: 'codex',
    priority: 5,
    created_at: now,
    updated_at: now,
    result: accepted ? 'accepted by human' : 'merged and awaiting verification'
  });

  await page.route(/\/api\/tasks(?:\?.*)?$/, async (route) => {
    await route.fulfill({ json: { tasks: [currentTask()] } });
  });
  await page.route(`**/api/tasks/${taskID}/accept`, async (route) => {
    accepted = true;
    await route.fulfill({
      json: {
        reply:
          'Accepted task. This backend detail is deliberately longer than the toast should need.'
      }
    });
  });
  await page.route(/\/api\/settings$/, async (route) => {
    await route.fulfill({ json: { settings: { auto_merge_enabled: false } } });
  });
  await page.route(/\/api\/approvals$/, async (route) => {
    await route.fulfill({ json: { approvals: [] } });
  });
  await page.route(/\/api\/events(?:\?.*)?$/, async (route) => {
    await route.fulfill({ json: { events: [] } });
  });
  await page.route(/\/api\/agents$/, async (route) => {
    await route.fulfill({ json: { agents: [] } });
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
};

const approvalFor = (taskID: string) => ({
  id: 'approval_20260426_150000_11111111',
  task_id: taskID,
  tool: 'git.merge_approved',
  reason: 'merge reviewed task branch into repo root',
  status: 'pending',
  created_at: '2026-04-26T15:00:00Z',
  updated_at: '2026-04-26T15:00:00Z'
});


test('chat mobile keeps typed draft text through layout changes', async ({ page }) => {
  await page.goto('/chat');
  await expect(page.getByRole('textbox', { name: 'Message' })).toBeVisible();

  const input = page.getByRole('textbox', { name: 'Message' });
  await input.fill('');
  await input.pressSequentially(typedMessage);
  await expect(input).toHaveValue(typedMessage);

  await page.getByRole('button', { name: 'Menu' }).click();
  await expect(input).toHaveValue(typedMessage);
  await page.setViewportSize({ width: 390, height: 844 });
  await expect(input).toHaveValue(typedMessage);

  const overflow = await page.evaluate(() => ({
    bodyWidth: document.body.scrollWidth,
    viewport: window.innerWidth
  }));
  expect(overflow.bodyWidth, JSON.stringify(overflow)).toBeLessThanOrEqual(overflow.viewport + 2);
});

test('created task chat reply links to the task with SPA navigation', async ({ page }) => {
  await mockTaskApi(page);
  await page.route('**/api/message', async (route) => {
    await route.fulfill({
      json: {
        reply:
          'Created queued task [Review queue behavior on mobile](/tasks?task=task_20260426_150000_11111111).',
        source: 'program'
      }
    });
  });

  await page.goto('/chat');
  await page.evaluate(() => {
    (window as Window & { __spaMarker?: string }).__spaMarker = 'same-document';
  });
  await page.getByRole('textbox', { name: 'Message' }).fill('new review queue behavior on mobile');
  await page.getByRole('button', { name: 'Send' }).click();
  const taskLink = page.getByRole('link', { name: 'Review queue behavior on mobile' });
  await expect(taskLink).toBeVisible();

  await taskLink.click();
  await expect(page).toHaveURL(/\/tasks\?task=task_20260426_150000_11111111$/, {
    timeout: 20_000
  });
  await expect(page.getByRole('heading', { name: 'Review queue behavior on mobile' })).toBeVisible({
    timeout: 20_000
  });
  await expect(page.locator('.workbench')).toBeVisible();
  await expect
    .poll(() => page.evaluate(() => (window as Window & { __spaMarker?: string }).__spaMarker))
    .toBe('same-document');

  await page.getByRole('button', { name: 'Back to queue' }).click();
  await expect(page).toHaveURL(/\/tasks$/);
  await page.locator('.triage button').filter({ hasText: 'All' }).click();
  await page.getByRole('link', { name: /Document mobile task flow/ }).click();
  await expect(page).toHaveURL(/\/tasks\?task=task_20260426_150300_33333333$/);
  await expect(page.getByRole('heading', { name: 'Document mobile task flow' })).toBeVisible();

  await page.goBack();
  await expect(page).toHaveURL(/\/tasks$/);
  await expect(page.locator('.task-pane')).toBeVisible();
  await expect(page.locator('.workbench')).not.toBeVisible();
  await expect(page.locator('.task-row.selected')).toHaveCount(0);
});

test('tasks overview remains the browser Back target after selecting a task', async ({ page }) => {
  await mockTaskApi(page);

  await page.goto('/chat');
  await page.getByRole('button', { name: 'Menu' }).click();
  await page.getByRole('link', { name: /Tasks/ }).click();
  await expect(page).toHaveURL(/\/tasks$/);
  await expect(page.locator('.task-pane')).toBeVisible();
  await expect(page.locator('.workbench')).not.toBeVisible();
  await expect(page.locator('.task-row.selected')).toHaveCount(0);

  await page.getByRole('link', { name: /Review queue behavior on mobile/ }).click();
  await expect(page).toHaveURL(/\/tasks\?task=task_20260426_150000_11111111$/);
  await expect(page.getByRole('heading', { name: 'Review queue behavior on mobile' })).toBeVisible();
  await expect(page.locator('.workbench')).toBeVisible();

  await page.goBack();
  await expect(page).toHaveURL(/\/tasks$/);
  await expect(page.locator('.task-pane')).toBeVisible();
  await expect(page.locator('.workbench')).not.toBeVisible();
  await expect(page.locator('.task-row.selected')).toHaveCount(0);
});

test('tasks mobile switches between queue and selected task detail', async ({ page }) => {
  test.setTimeout(60_000);
  await mockTaskApi(page);
  await page.goto('/tasks');

  const rows = page.locator('.task-row');
  const reviewRow = page.getByRole('link', { name: /Review queue behavior on mobile/ });
  const queue = page.locator('.task-pane');
  const detail = page.locator('.workbench');
  await expect(page.getByRole('navigation', { name: 'Task panels' })).toHaveCount(0);
  await expect(page.getByText('Pending approvals')).toHaveCount(0);
  await expect(page.getByRole('heading', { name: '2 need attention' })).toBeVisible({
    timeout: taskLoadTimeoutMs
  });
  await expect(page.locator('.task-header button')).toHaveCount(0);
  await expect(page.locator('.sync-status')).toHaveAttribute('data-sync-status', 'connected', {
    timeout: taskLoadTimeoutMs
  });
  await expect(page.locator('.sync-status')).toContainText('Connected');
  await expect(rows).toHaveCount(2, { timeout: taskLoadTimeoutMs });
  await expect(reviewRow).toHaveCount(1, { timeout: taskLoadTimeoutMs });
  await expect(queue).toBeVisible();
  await expect(detail).not.toBeVisible();
  await expect(page.locator('.task-row.selected')).toHaveCount(0);
  const autoMerge = page.getByRole('switch', { name: 'Auto merge reviewed queue-head tasks' });
  await expect(autoMerge).toHaveAttribute('aria-checked', 'false');
  await autoMerge.click();
  await expect(autoMerge).toHaveAttribute('aria-checked', 'true');

  await page.getByRole('button', { name: 'Menu' }).click();
  await expect(
    page.getByRole('link', {
      name: 'Tasks, 1 urgent item, 1 review item need attention'
    })
  ).toBeVisible();
  await expect(page.locator('.mobile-nav .attention-badge.critical')).toHaveText('1');
  await expect(page.locator('.mobile-nav .attention-badge.warning')).toHaveText('1');
  await expect(page.locator('.mobile-menu-scrim')).toBeVisible();
  await page.mouse.click(20, (page.viewportSize()?.height ?? 844) - 24);
  await expect(page.getByRole('navigation', { name: 'Primary mobile' })).toBeHidden();

  const queueMetrics = await page.evaluate(() => {
    const navbar = document.querySelector('.navbar');
    const heading = document.querySelector('.task-header h1');
    const sync = document.querySelector('.sync-status');
    return {
      navbarBottom: navbar?.getBoundingClientRect().bottom ?? 0,
      headingTop: heading?.getBoundingClientRect().top ?? 0,
      syncTop: sync?.getBoundingClientRect().top ?? 0
    };
  });
  expect(queueMetrics.headingTop, JSON.stringify(queueMetrics)).toBeGreaterThanOrEqual(
    queueMetrics.navbarBottom
  );
  expect(queueMetrics.syncTop, JSON.stringify(queueMetrics)).toBeGreaterThanOrEqual(
    queueMetrics.navbarBottom
  );

  await reviewRow.click();
  await expect(queue).not.toBeVisible();
  await expect(detail).toBeVisible();
  await expect(page.getByRole('region', { name: 'Task actions', exact: true })).toContainText(
    'Approve merge'
  );
  await expect(page.getByRole('region', { name: 'Task attachments' })).toContainText(
    'browser-context.json'
  );
  await expect(page.getByRole('button', { name: 'Back to queue' })).toBeVisible();
  const backStyle = await page.locator('.back-to-queue').evaluate((element) => {
    const style = getComputedStyle(element);
    return {
      background: style.backgroundColor,
      borderTopWidth: style.borderTopWidth
    };
  });
  expect(backStyle.borderTopWidth).toBe('0px');
  expect(backStyle.background).toBe('rgba(0, 0, 0, 0)');
  await expect(page.locator('[aria-label="Worker runs"]')).not.toHaveAttribute('open', '');
  await page.locator('[aria-label="Task plan"] summary').click();
  await expect(page.locator('[aria-label="Task plan"]')).toContainText('Reviewed plan');
  await expect(page.locator('[aria-label="Task plan"]')).toContainText(
    'Inspect mobile layout'
  );
  await expect(page.locator('.command-panel, .composer, #message')).toHaveCount(0);

  await page.getByRole('button', { name: 'Back to queue' }).click();
  await expect(page).toHaveURL(/\/tasks$/);
  await expect(queue).toBeVisible();
  await expect(detail).not.toBeVisible();
  await expect(rows.first()).toBeVisible();
  expect(await rows.count()).toBeGreaterThanOrEqual(2);
  await expect(page.locator('.task-row.selected')).toHaveCount(0);

  const overflow = await page.evaluate(() => ({
    bodyWidth: document.body.scrollWidth,
    viewport: window.innerWidth
  }));
  expect(overflow.bodyWidth, JSON.stringify(overflow)).toBeLessThanOrEqual(overflow.viewport + 2);

  await page.getByRole('textbox', { name: 'Search tasks' }).fill('no matching mobile tasks');
  await expect(page.getByText('No tasks match the current filters.')).toBeVisible();
  const emptyQueueScroll = await page.evaluate(() => {
    const taskList = document.querySelector('.task-list') as HTMLElement | null;
    const taskPane = document.querySelector('.task-pane') as HTMLElement | null;
    const footer = document.querySelector('.task-pane footer') as HTMLElement | null;
    window.scrollTo(0, document.body.scrollHeight);
    return {
      scrollY: window.scrollY,
      pageScrollHeight: document.scrollingElement?.scrollHeight ?? document.documentElement.scrollHeight,
      pageClientHeight: document.scrollingElement?.clientHeight ?? document.documentElement.clientHeight,
      taskListHeight: Math.round(taskList?.getBoundingClientRect().height || 0),
      taskListOverflowY: taskList ? getComputedStyle(taskList).overflowY : '',
      taskPaneOverflowY: taskPane ? getComputedStyle(taskPane).overflowY : '',
      footerBottom: footer?.getBoundingClientRect().bottom ?? 0
    };
  });
  expect(emptyQueueScroll.scrollY, JSON.stringify(emptyQueueScroll)).toBeLessThanOrEqual(1);
  expect(emptyQueueScroll.taskListHeight, JSON.stringify(emptyQueueScroll)).toBeGreaterThan(0);
  expect(emptyQueueScroll.taskListOverflowY, JSON.stringify(emptyQueueScroll)).toBe('auto');
  expect(emptyQueueScroll.taskPaneOverflowY, JSON.stringify(emptyQueueScroll)).toBe('hidden');
  expect(emptyQueueScroll.pageScrollHeight, JSON.stringify(emptyQueueScroll)).toBeLessThanOrEqual(
    emptyQueueScroll.pageClientHeight + 1
  );
  expect(emptyQueueScroll.footerBottom, JSON.stringify(emptyQueueScroll)).toBeLessThanOrEqual(
    emptyQueueScroll.pageClientHeight + 1
  );
});

test('tasks mobile returns to the queue when Accept empties the current filter', async ({ page }) => {
  test.setTimeout(45_000);
  const now = new Date('2026-04-26T15:00:00Z').toISOString();
  const taskID = 'task_20260426_151500_accept1';
  let accepted = false;
  const task = () => ({
    id: taskID,
    title: 'Verify empty attention queue',
    goal: 'Accepting this task should leave Attention empty without hiding the queue.',
    status: accepted ? 'done' : 'awaiting_verification',
    assigned_to: 'codex',
    priority: 5,
    created_at: now,
    updated_at: now,
    result: accepted ? 'accepted by human' : 'ready for verification'
  });

  await page.route(/\/api\/tasks\/task_20260426_151500_accept1\/accept$/, async (route) => {
    accepted = true;
    await route.fulfill({ json: { reply: 'accepted empty attention task' } });
  });
  await page.route(/\/api\/tasks(?:\?.*)?$/, async (route) => {
    await route.fulfill({ json: { tasks: [task()] } });
  });
  await page.route(/\/api\/settings$/, async (route) => {
    await route.fulfill({ json: { settings: { auto_merge_enabled: false } } });
  });
  await page.route(/\/api\/approvals$/, async (route) => {
    await route.fulfill({ json: { approvals: [] } });
  });
  await page.route(/\/api\/events(?:\?.*)?$/, async (route) => {
    await route.fulfill({ json: { events: [] } });
  });
  await page.route(/\/api\/agents$/, async (route) => {
    await route.fulfill({ json: { agents: [] } });
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

  await page.goto('/tasks');
  await expect(page.getByRole('heading', { name: '1 need attention' })).toBeVisible({
    timeout: taskLoadTimeoutMs
  });
  await page.getByRole('link', { name: /Verify empty attention queue/ }).click();
  await expect(page.locator('.task-pane')).not.toBeVisible();
  await expect(page.locator('.workbench')).toBeVisible();

  await page.getByRole('button', { name: 'Accept', exact: true }).click();
  await expect(page).toHaveURL(/\/tasks$/);
  await expect(page.getByRole('heading', { name: '0 need attention' })).toBeVisible();
  await expect(page.locator('.task-pane')).toBeVisible();
  await expect(page.locator('.workbench')).not.toBeVisible();
  await expect(page.locator('.task-row')).toHaveCount(0);
  await expect(page.getByText('No tasks match the current filters.')).toBeVisible();
  await expect(page.locator('.empty-record')).not.toBeVisible();
});

test('tasks mobile keeps Running selected after background sync', async ({ page }) => {
  test.setTimeout(30_000);
  let taskListRequests = 0;
  page.on('request', (request) => {
    const url = new URL(request.url());
    if (request.method() === 'GET' && url.pathname === '/api/tasks') {
      taskListRequests += 1;
    }
  });
  await mockTaskApi(page);
  await page.goto('/tasks?task=task_20260426_150000_11111111');
  await expect(page.getByRole('heading', { name: 'Review queue behavior on mobile' })).toBeVisible({
    timeout: taskLoadTimeoutMs
  });
  await page.getByRole('button', { name: 'Back to queue' }).click();

  await page.locator('.triage button').filter({ hasText: 'Running' }).click();
  await expect(page.locator('.triage button.active')).toContainText('Running');
  await expect(page.getByRole('link', { name: /Run dashboard checks/ })).toBeVisible();

  const requestsAfterClick = taskListRequests;
  await expect
    .poll(() => taskListRequests, { timeout: 12_000 })
    .toBeGreaterThan(requestsAfterClick);
  await expect(page.locator('.triage button.active')).toContainText('Running');
  await expect(page.getByRole('link', { name: /Run dashboard checks/ })).toBeVisible();
});

test('tasks sync status replaces manual refresh and pulses during automatic updates', async ({
  page
}) => {
  test.setTimeout(35_000);
  let taskListRequests = 0;
  page.on('request', (request) => {
    const url = new URL(request.url());
    if (request.method() === 'GET' && url.pathname === '/api/tasks') {
      taskListRequests += 1;
    }
  });
  await mockTaskApi(page, undefined, [], {
    taskListDelayMs: (requestCount) => (requestCount > 1 ? 2500 : 0)
  });

  await page.goto('/tasks');
  await expect(page.locator('.task-header button')).toHaveCount(0);
  await expect(page.locator('.sync-status')).toHaveAttribute('data-sync-status', 'connected', {
    timeout: taskLoadTimeoutMs
  });
  await expect(page.locator('.sync-status')).toContainText('Connected');

  const requestsAfterFirstUpdate = taskListRequests;
  await expect
    .poll(() => taskListRequests, { timeout: 12_000 })
    .toBeGreaterThan(requestsAfterFirstUpdate);
  await expect(page.locator('.sync-status.refreshing .dot.pulse')).toBeVisible();
  await expect(page.locator('.sync-status')).toHaveAttribute('data-sync-status', 'connected');
});

test('tasks mobile preserves triage filters when returning from task detail', async ({ page }) => {
  await mockTaskApi(page);
  await page.goto('/tasks');

  const queue = page.locator('.task-pane');
  const detail = page.locator('.workbench');
  const activeFilter = page.locator('.triage button.active');
  const reviewRow = page.getByRole('link', { name: /Review queue behavior on mobile/ });
  const runningRow = page.getByRole('link', { name: /Run dashboard checks/ });

  await expect(reviewRow).toBeVisible({ timeout: taskLoadTimeoutMs });
  await expect(activeFilter).toContainText('Attention');
  await reviewRow.click();
  await expect(detail).toBeVisible();
  await expect(page.getByRole('heading', { name: 'Review queue behavior on mobile' })).toBeVisible();
  await page.getByRole('button', { name: 'Back to queue' }).click();
  await expect(page).toHaveURL(/\/tasks$/);
  await expect(queue).toBeVisible();
  await expect(detail).not.toBeVisible();
  await expect(activeFilter).toContainText('Attention');
  await expect(reviewRow).toBeVisible();
  await expect(page.locator('.task-row.selected')).toHaveCount(0);

  await page.locator('.triage button').filter({ hasText: 'Running' }).click();
  await expect(activeFilter).toContainText('Running');
  await expect(runningRow).toBeVisible();
  await runningRow.click();
  await expect(page).toHaveURL(/\/tasks\?task=task_20260426_150200_22222222$/);
  await expect(detail).toBeVisible();
  await expect(page.getByRole('heading', { name: 'Run dashboard checks' })).toBeVisible();
  await page.goBack();
  await expect(page).toHaveURL(/\/tasks$/);
  await expect(queue).toBeVisible();
  await expect(detail).not.toBeVisible();
  await expect(activeFilter).toContainText('Running');
  await expect(runningRow).toBeVisible();
  await expect(page.locator('.task-row.selected')).toHaveCount(0);

  await runningRow.click();
  await expect(page.getByRole('heading', { name: 'Run dashboard checks' })).toBeVisible();
  await page.getByRole('button', { name: 'Back to queue' }).click();
  await expect(page).toHaveURL(/\/tasks$/);
  await expect(queue).toBeVisible();
  await expect(detail).not.toBeVisible();
  await expect(activeFilter).toContainText('Running');
  await expect(runningRow).toBeVisible();
  await expect(page.locator('.task-row.selected')).toHaveCount(0);
});

test('tasks status pills describe queued review without implying action', async ({ page }) => {
  const now = new Date('2026-04-26T15:05:00Z').toISOString();
  await mockTaskApi(page, undefined, [
    {
      id: 'task_20260426_150500_44444444',
      title: 'Queue review gate copy',
      goal: 'Show system-owned review gates without sounding like an operator action.',
      status: 'ready_for_review',
      assigned_to: 'codex',
      priority: 5,
      created_at: now,
      updated_at: now,
      merge_queue_position: 1,
      result: 'external agent finished; ready for review.'
    }
  ]);

  await page.goto('/tasks');
  const reviewGateRow = page.getByRole('link', { name: /Queue review gate copy/ });
  await expect(reviewGateRow).toBeVisible({ timeout: taskLoadTimeoutMs });
  await expect(reviewGateRow.locator('.status')).toHaveText('queued for review');
  await expect(reviewGateRow.locator('.status')).not.toHaveText(/ready for review/i);

  await reviewGateRow.click();
  await expect(page.locator('.record-header .status')).toHaveText('queued for review');
  const actions = page.getByRole('region', { name: 'Task actions', exact: true });
  await expect(actions).toContainText('Queued for review');
  await expect(actions).toContainText('No action needed');
  await expect(actions).not.toContainText('Ready for review');
});

test('accepted task feedback is a non-reflowing toast on mobile', async ({ page }) => {
  await mockAcceptTaskApi(page);
  await page.goto('/tasks?task=task_20260426_151000_44444444');

  await expect(page.getByRole('heading', { name: 'Verify accepted task toast placement' })).toBeVisible({
    timeout: taskLoadTimeoutMs
  });
  const backButton = page.getByRole('button', { name: 'Back to queue' });
  await expect(backButton).toBeVisible();
  const beforeBack = await backButton.boundingBox();
  expect(beforeBack).not.toBeNull();

  await page.getByRole('button', { name: 'Accept' }).click();
  const toast = page.getByRole('status');
  await expect(toast).toContainText('Accept submitted');
  await expect(toast).toContainText('accepted. Task is now done.');
  await expect(page.locator('.workbench > .notice')).toHaveCount(0);
  await expect(page.getByRole('region', { name: 'Task actions', exact: true })).toContainText(
    'Reopen'
  );

  const afterBack = await backButton.boundingBox();
  expect(afterBack).not.toBeNull();
  expect(Math.abs(afterBack!.y - beforeBack!.y)).toBeLessThanOrEqual(1);
  const toastLayout = await page.locator('.toast-notice').evaluate((element) => {
    const style = getComputedStyle(element);
    return {
      position: style.position,
      bodyWidth: document.body.scrollWidth,
      viewport: window.innerWidth
    };
  });
  expect(toastLayout.position).toBe('fixed');
  expect(toastLayout.bodyWidth, JSON.stringify(toastLayout)).toBeLessThanOrEqual(
    toastLayout.viewport + 2
  );
});

test('chat supports file uploads and sends attachment context', async ({ page }) => {
  let requestBody: any;
  await page.route('**/api/message', async (route) => {
    requestBody = JSON.parse(route.request().postData() || '{}');
    await route.fulfill({
      json: {
        reply: 'received attachment\n\n```mermaid\nflowchart LR\n  Chat[Chat] --> Task[Task]\n```',
        source: 'program'
      }
    });
  });
  await page.goto('/chat');
  await expect(page.getByRole('textbox', { name: 'Message' })).toBeVisible();

  await page.locator('#chat-attachments').setInputFiles({
    name: 'notes.txt',
    mimeType: 'text/plain',
    buffer: Buffer.from('steps to reproduce')
  });
  await expect(page.getByLabel('Pending attachments')).toContainText('notes.txt');
  await page.getByRole('textbox', { name: 'Message' }).fill('please inspect this');
  await page.getByRole('button', { name: 'Send' }).click();

  expect(requestBody.content).toBe('please inspect this');
  expect(requestBody.attachments).toHaveLength(1);
  expect(requestBody.attachments[0].name).toBe('notes.txt');
  expect(requestBody.attachments[0].text).toBe('steps to reproduce');
  await expect(page.getByLabel('Message attachments')).toContainText('notes.txt');
  await expect(page.getByText('received attachment')).toBeVisible();
  await expect(page.locator('.message:not(.user) .mermaid-diagram svg')).toBeVisible();
});

test('chat marks failed sends inline and retries the original message', async ({ page }) => {
  let attempts = 0;
  const sentBodies: any[] = [];
  await page.route('**/api/message', async (route) => {
    attempts += 1;
    sentBodies.push(JSON.parse(route.request().postData() || '{}'));
    if (attempts === 1) {
      await route.fulfill({ status: 503, body: 'offline' });
      return;
    }
    await route.fulfill({ json: { reply: 'retry received', source: 'program' } });
  });

  await page.goto('/chat');
  await expect(page.getByRole('textbox', { name: 'Message' })).toBeVisible();

  const text = 'send this when the connection returns';
  await page.getByRole('textbox', { name: 'Message' }).fill(text);
  await page.getByRole('button', { name: 'Send' }).click();

  const failedMessage = page.locator('.message.user').filter({ hasText: text });
  await expect(failedMessage).toContainText('Message failed to send');
  await expect(failedMessage).toHaveClass(/failed/);
  await expect(page.locator('.error')).toHaveCount(0);

  await page.getByRole('button', { name: 'Menu' }).click();
  await page.getByRole('button', { name: 'Switch to dark mode' }).click();
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark');
  await expect
    .poll(() => failedMessage.evaluate((element) => getComputedStyle(element).backgroundColor))
    .toBe('rgb(31, 41, 55)');
  await page.getByRole('button', { name: 'Menu' }).click();

  await failedMessage.getByRole('button', { name: 'Resend failed message' }).click();
  await expect(page.getByText('retry received')).toBeVisible();
  await expect(failedMessage).not.toContainText('Message failed to send');
  await expect(failedMessage).not.toHaveClass(/failed/);
  expect(attempts).toBe(2);
  expect(sentBodies.map((body) => body.content)).toEqual([text, text]);
});

test('chat renders Mermaid diagrams with the brand palette in light and dark modes', async ({
  page
}) => {
  await page.addInitScript(() => {
    localStorage.setItem('homelabd.dashboard.theme', 'light');
  });
  await page.route('**/api/message', async (route) => {
    await route.fulfill({
      json: {
        reply:
          'diagram-regression\n\n```mermaid\nflowchart LR\n  A[Request] --> B[Task]\n  B --> C[Review]\n```',
        source: 'program'
      }
    });
  });
  await page.goto('/chat');
  await expect(page.getByRole('textbox', { name: 'Message' })).toBeVisible();

  await page.getByRole('textbox', { name: 'Message' }).fill('show me the task flow');
  await page.getByRole('button', { name: 'Send' }).click();

  const message = page.locator('.message').filter({ hasText: 'diagram-regression' });
  const diagram = message.locator('.mermaid-diagram');
  await expect(diagram).toHaveAttribute('data-mermaid-status', 'rendered');
  const svg = diagram.locator('svg');
  await expect(svg).toBeVisible();
  await expect.poll(() => svg.evaluate((element) => element.outerHTML)).toContain('#2563eb');

  await page.getByRole('button', { name: 'Menu' }).click();
  const darkToggle = page.getByRole('button', { name: /Switch to dark mode/ });
  await expect(darkToggle).toHaveAttribute('data-theme-toggle-ready', 'true');
  await darkToggle.click();
  await expect(diagram).toHaveAttribute('data-mermaid-status', 'rendered');
  await expect.poll(() => svg.evaluate((element) => element.outerHTML)).toContain('#60a5fa');

  const metrics = await diagram.evaluate((element) => ({
    scrollWidth: element.scrollWidth,
    clientWidth: element.clientWidth,
    bodyWidth: document.body.scrollWidth,
    viewport: window.innerWidth
  }));
  expect(metrics.scrollWidth, JSON.stringify(metrics)).toBeLessThanOrEqual(
    metrics.clientWidth + 2
  );
  expect(metrics.bodyWidth, JSON.stringify(metrics)).toBeLessThanOrEqual(metrics.viewport + 2);
});

test('mobile navbar help button creates a task with captured context', async ({ page }) => {
  await mockScreenCapture(page);
  let requestBody: any;
  await page.context().route(/\/api\/tasks(?:\?.*)?$/, async (route) => {
    requestBody = JSON.parse(route.request().postData() || '{}');
    await route.fulfill({ status: 201, json: { reply: 'created help task' } });
  });
  await page.goto('/chat');
  await expect(page.getByRole('textbox', { name: 'Message' })).toBeVisible();
  await expect(page.locator('.help-button')).toBeVisible();

  await page.locator('.help-button').click();
  const dialog = page.locator('dialog.help-dialog');
  await expect(dialog).toBeVisible();
  await dialog.getByRole('textbox', { name: 'More detail' }).fill('The queue button did nothing.');
  await dialog.getByRole('button', { name: 'Submit help task' }).click();

  expect(requestBody.goal).toContain('The queue button did nothing.');
  expect(requestBody.attachments).toHaveLength(2);
  expect(requestBody.attachments[0].name).toBe('browser-context.json');
  expect(requestBody.attachments[0].text).toContain('"url"');
  expect(requestBody.attachments[1].name).toBe('dashboard-screenshot.png');
  expect(requestBody.attachments[1].data_url).toBe('data:image/png;base64,AAAA');
});

test('navbar help stays available on desktop and submits without screen capture support', async ({
  page
}) => {
  await mockScreenCaptureUnavailable(page);
  let requestBody: any;
  await mockTaskApi(page, (body) => {
    requestBody = body;
  });
  const expectHelpButtonInViewport = async () => {
    const helpLayout = await page.locator('.help-button').evaluate((button) => {
      const rect = button.getBoundingClientRect();
      return {
        right: rect.right,
        viewport: window.innerWidth,
        bodyWidth: document.body.scrollWidth
      };
    });
    expect(helpLayout.right, JSON.stringify(helpLayout)).toBeLessThanOrEqual(
      helpLayout.viewport + 1
    );
    expect(helpLayout.bodyWidth, JSON.stringify(helpLayout)).toBeLessThanOrEqual(
      helpLayout.viewport + 2
    );
  };

  await page.setViewportSize({ width: 1280, height: 900 });
  await page.goto('/tasks');
  await expect(
    page.getByRole('link', { name: /Review queue behavior on mobile/ })
  ).toBeVisible();
  await expect(page.locator('.desktop-nav')).toBeVisible();
  await expect(page.locator('.help-button')).toBeVisible();
  await expectHelpButtonInViewport();

  await page.setViewportSize({ width: 980, height: 900 });
  await expect(page.locator('.desktop-nav')).not.toBeVisible();
  await expect(page.getByRole('button', { name: 'Menu' })).toBeVisible();
  await expect(page.locator('.help-button')).toBeVisible();
  await expectHelpButtonInViewport();

  await page.locator('.help-button').click();
  const dialog = page.locator('dialog.help-dialog');
  await expect(dialog).toBeVisible();
  await expect(dialog).toContainText('Screenshot capture is unavailable');
  await dialog.getByRole('textbox', { name: 'More detail' }).fill('Desktop help should stay available.');
  await dialog.getByRole('button', { name: 'Submit help task' }).click();

  expect(requestBody.goal).toContain('Desktop help should stay available.');
  expect(requestBody.attachments).toHaveLength(1);
  expect(requestBody.attachments[0].name).toBe('browser-context.json');
});

test('tasks mobile has no task chat composer and keeps new-task text stable', async ({ page }) => {
  await mockTaskApi(page);
  await page.goto('/tasks');
  await expect(page.locator('.command-panel, .composer, #message')).toHaveCount(0);

  await page.locator('.target-create summary').click();
  const goal = page.getByRole('textbox', { name: 'New task goal' });
  await goal.fill('');
  await goal.pressSequentially(typedMessage);
  await expect(goal).toHaveValue(typedMessage);

  await page.locator('.triage button').filter({ hasText: 'All' }).click();
  await expect(page.locator('#new-task-goal')).toHaveValue(typedMessage);

  await expect(page.locator('.draft-preview')).toHaveCount(0);
  const overflow = await page.evaluate(() => ({
    bodyWidth: document.body.scrollWidth,
    viewport: window.innerWidth
  }));
  expect(overflow.bodyWidth, JSON.stringify(overflow)).toBeLessThanOrEqual(overflow.viewport + 2);
});

test('tasks mobile keeps page fixed and lets the task list own vertical scrolling', async ({ page }) => {
  const now = new Date('2026-04-26T15:00:00Z').toISOString();
  await mockTaskApi(
    page,
    undefined,
    Array.from({ length: 12 }, (_, index) => ({
      id: `task_20260426_151${String(index).padStart(2, '0')}_extra${index}`,
      title: `Completed background task ${index + 1}`,
      goal: 'Provide enough task rows for mobile queue scrolling.',
      status: 'done',
      assigned_to: 'codex',
      priority: 5,
      created_at: now,
      updated_at: now,
      result: 'complete'
    }))
  );
  await page.goto('/tasks');
  await expect(page.getByRole('button', { name: 'Menu' })).toBeVisible();
  const allFilter = page.locator('.triage button').filter({ hasText: 'All' });
  await expect(allFilter).toContainText('16', { timeout: taskLoadTimeoutMs });
  await allFilter.click();
  await expect(allFilter).toHaveClass(/active/);
  await expect(page.locator('.task-row')).toHaveCount(16, { timeout: taskLoadTimeoutMs });

  const scrollMetrics = await page.evaluate(() => {
    const navbar = document.querySelector('.navbar') as HTMLElement | null;
    const taskList = document.querySelector('.task-list') as HTMLElement | null;
    const taskPane = document.querySelector('.task-pane') as HTMLElement | null;
    if (taskList) {
      taskList.scrollTop = taskList.scrollHeight;
    }
    window.scrollTo(0, document.body.scrollHeight);
    return {
      windowScrollY: window.scrollY,
      navbarTop: navbar?.getBoundingClientRect().top ?? Number.POSITIVE_INFINITY,
      taskListScrollTop: Math.round(taskList?.scrollTop || 0),
      taskListScrollable: Math.round((taskList?.scrollHeight || 0) - (taskList?.clientHeight || 0)),
      taskListOverflowY: taskList ? getComputedStyle(taskList).overflowY : '',
      taskPaneOverflowY: taskPane ? getComputedStyle(taskPane).overflowY : '',
      bodyWidth: document.body.scrollWidth,
      viewport: window.innerWidth
    };
  });
  expect(scrollMetrics.windowScrollY, JSON.stringify(scrollMetrics)).toBeLessThanOrEqual(1);
  expect(scrollMetrics.navbarTop, JSON.stringify(scrollMetrics)).toBeLessThanOrEqual(1);
  expect(scrollMetrics.taskListScrollable, JSON.stringify(scrollMetrics)).toBeGreaterThan(0);
  expect(scrollMetrics.taskListScrollTop, JSON.stringify(scrollMetrics)).toBeGreaterThan(0);
  expect(scrollMetrics.taskListOverflowY, JSON.stringify(scrollMetrics)).toBe('auto');
  expect(scrollMetrics.taskPaneOverflowY, JSON.stringify(scrollMetrics)).toBe('hidden');
  expect(scrollMetrics.bodyWidth, JSON.stringify(scrollMetrics)).toBeLessThanOrEqual(
    scrollMetrics.viewport + 2
  );
});
