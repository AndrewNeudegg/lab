import { expect, test } from '@playwright/test';

const typedMessage = 'mobile input must keep every typed character 12345';


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

test('tasks mobile keeps navbar sticky while scrolling', async ({ page }) => {
  await page.goto('/tasks');
  await expect(page.getByRole('button', { name: 'Menu' })).toBeVisible();

  const beforeScroll = await page.locator('.navbar').boundingBox();
  expect(beforeScroll?.y ?? Number.POSITIVE_INFINITY).toBeLessThanOrEqual(1);

  await page.evaluate(() => window.scrollTo(0, document.body.scrollHeight));
  await page.waitForTimeout(100);

  const afterScroll = await page.locator('.navbar').boundingBox();
  expect(afterScroll?.y ?? Number.POSITIVE_INFINITY).toBeLessThanOrEqual(1);
});
