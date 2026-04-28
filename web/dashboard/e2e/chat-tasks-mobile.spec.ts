import { expect, test } from '@playwright/test';
import type { Page } from '@playwright/test';

const typedMessage = 'mobile input must keep every typed character 12345';
const taskLoadTimeoutMs = 15_000;

const mockTaskApi = async (page: Page) => {
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

  await page.route('**/api/tasks', async (route) => {
    await route.fulfill({ json: { tasks } });
  });
  await page.route('**/api/approvals', async (route) => {
    await route.fulfill({ json: { approvals: [approvalFor(tasks[0].id)] } });
  });
  await page.route('**/api/events?**', async (route) => {
    await route.fulfill({ json: { events: [] } });
  });
  await page.route('**/api/agents', async (route) => {
    await route.fulfill({ json: { agents: [] } });
  });
  await page.route('**/api/tasks/*/runs', async (route) => {
    await route.fulfill({ json: { runs: [] } });
  });
  await page.route('**/api/tasks/*/diff', async (route) => {
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

test('tasks mobile switches between queue and selected task detail', async ({ page }) => {
  await mockTaskApi(page);
  await page.goto('/tasks');

  const rows = page.locator('.task-row');
  const queue = page.locator('.task-pane');
  const detail = page.locator('.workbench');
  await expect(page.getByRole('navigation', { name: 'Task panels' })).toHaveCount(0);
  await expect(page.getByText('Pending approvals')).toHaveCount(0);
  await expect(page.getByRole('heading', { name: '2 need action' })).toBeVisible({
    timeout: taskLoadTimeoutMs
  });
  await expect(rows).toHaveCount(2, { timeout: taskLoadTimeoutMs });
  await expect(queue).toBeVisible();
  await expect(detail).not.toBeVisible();

  await page.getByRole('button', { name: 'Menu' }).click();
  await expect(
    page.getByRole('link', {
      name: 'Tasks, 1 urgent item, 1 review item need attention'
    })
  ).toBeVisible();
  await expect(page.locator('.mobile-nav .attention-badge.critical')).toHaveText('1');
  await expect(page.locator('.mobile-nav .attention-badge.warning')).toHaveText('1');
  await page.getByRole('button', { name: 'Menu' }).click();

  const queueMetrics = await page.evaluate(() => {
    const navbar = document.querySelector('.navbar');
    const heading = document.querySelector('.task-header h1');
    const sync = document.querySelector('.task-header button');
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

  await rows.first().click();
  await expect(queue).not.toBeVisible();
  await expect(detail).toBeVisible();
  await expect(page.getByRole('region', { name: 'Task actions', exact: true })).toContainText(
    'Approve merge'
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
  await expect(queue).toBeVisible();
  await expect(detail).not.toBeVisible();
  await expect(rows).toHaveCount(2);
  await expect(rows.first()).toBeVisible();

  const overflow = await page.evaluate(() => ({
    bodyWidth: document.body.scrollWidth,
    viewport: window.innerWidth
  }));
  expect(overflow.bodyWidth, JSON.stringify(overflow)).toBeLessThanOrEqual(overflow.viewport + 2);
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

test('tasks mobile keeps navbar sticky while scrolling', async ({ page }) => {
  await mockTaskApi(page);
  await page.goto('/tasks');
  await expect(page.getByRole('button', { name: 'Menu' })).toBeVisible();

  const beforeScroll = await page.locator('.navbar').boundingBox();
  expect(beforeScroll?.y ?? Number.POSITIVE_INFINITY).toBeLessThanOrEqual(1);

  await page.evaluate(() => window.scrollTo(0, document.body.scrollHeight));
  await page.waitForTimeout(100);

  const afterScroll = await page.locator('.navbar').boundingBox();
  expect(afterScroll?.y ?? Number.POSITIVE_INFINITY).toBeLessThanOrEqual(1);
});
