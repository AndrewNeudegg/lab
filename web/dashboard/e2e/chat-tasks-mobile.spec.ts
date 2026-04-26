import { expect, test } from '@playwright/test';
import type { Page } from '@playwright/test';

const typedMessage = 'mobile input must keep every typed character 12345';

const mockTaskApi = async (page: Page) => {
  const now = new Date('2026-04-26T15:00:00Z').toISOString();
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
      result: 'waiting for approval'
    },
    {
      id: 'task_20260426_150100_22222222',
      title: 'Run dashboard checks',
      goal: 'Run check and browser tests for the dashboard.',
      status: 'running',
      assigned_to: 'codex',
      priority: 5,
      created_at: now,
      updated_at: now
    },
    {
      id: 'task_20260426_150200_33333333',
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
    await route.fulfill({ json: { approvals: [] } });
  });
  await page.route('**/api/events?**', async (route) => {
    await route.fulfill({ json: { events: [] } });
  });
};


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

test('tasks mobile keeps the queue visible after task selection', async ({ page }) => {
  await mockTaskApi(page);
  await page.goto('/tasks');

  const rows = page.locator('.task-row');
  const queue = page.locator('.task-pane');
  await expect(rows).toHaveCount(1);

  const queueBox = await queue.boundingBox();
  const viewport = page.viewportSize();
  expect(queueBox?.height ?? 0).toBeGreaterThan(0);
  expect(queueBox?.height ?? 0).toBeLessThanOrEqual((viewport?.height ?? 844) * 0.56);

  await rows.first().click();
  await expect(queue).not.toHaveClass(/collapsed/);
  await expect(rows).toHaveCount(1);
  await expect(page.getByRole('button', { name: /Hide queue/ })).toBeVisible();

  await page.getByRole('button', { name: /Hide queue/ }).click();
  await expect(queue).toHaveClass(/collapsed/);
  await expect(page.getByRole('button', { name: /Show queue/ })).toBeVisible();
  await expect(rows.first()).not.toBeVisible();

  await page.getByRole('button', { name: /Show queue/ }).click();
  await expect(queue).not.toHaveClass(/collapsed/);
  await expect(rows).toHaveCount(1);
  await expect(rows.first()).toBeVisible();

  const overflow = await page.evaluate(() => ({
    bodyWidth: document.body.scrollWidth,
    viewport: window.innerWidth
  }));
  expect(overflow.bodyWidth, JSON.stringify(overflow)).toBeLessThanOrEqual(overflow.viewport + 2);
});

test('tasks mobile keeps command text while changing queue selection', async ({ page }) => {
  await page.goto('/tasks');
  await expect(page.getByRole('textbox', { name: 'Task command' })).toBeVisible();

  const input = page.getByRole('textbox', { name: 'Task command' });
  await input.fill('');
  await input.pressSequentially(typedMessage);
  await expect(input).toHaveValue(typedMessage);

  const allButton = page.getByRole('button', { name: /All/ });
  await allButton.click();
  await expect(input).toHaveValue(typedMessage);

  const firstTask = page.locator('.task-row').first();
  if (await firstTask.count()) {
    await firstTask.click();
    await expect(input).toHaveValue(typedMessage);
  }

  await expect(page.locator('.draft-preview')).toHaveCount(0);
  const overflow = await page.evaluate(() => ({
    bodyWidth: document.body.scrollWidth,
    viewport: window.innerWidth
  }));
  expect(overflow.bodyWidth, JSON.stringify(overflow)).toBeLessThanOrEqual(overflow.viewport + 2);
});
