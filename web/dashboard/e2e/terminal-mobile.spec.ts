import { expect, test } from '@playwright/test';

test('terminal mobile sends command and control keys without horizontal overflow', async ({ page }) => {
  const requests: { path: string; body: string }[] = [];

  await page.route('**/api/terminal/sessions', async (route) => {
    requests.push({ path: route.request().url(), body: route.request().postData() || '' });
    await route.fulfill({
      status: 201,
      contentType: 'application/json',
      body: JSON.stringify({ id: 'term_test', shell: '/bin/sh', cwd: '/workspace' })
    });
  });

  await page.route('**/api/terminal/sessions/term_test/events', async (route) => {
    await route.fulfill({
      status: 200,
      contentType: 'text/event-stream',
      body: 'event: output\ndata: {"type":"output","data":"ready\\n"}\n\n'
    });
  });

  await page.route('**/api/terminal/sessions/term_test/input', async (route) => {
    requests.push({ path: route.request().url(), body: route.request().postData() || '' });
    await route.fulfill({ status: 200, contentType: 'application/json', body: '{"ok":true}' });
  });

  await page.route('**/api/terminal/sessions/term_test/signal', async (route) => {
    requests.push({ path: route.request().url(), body: route.request().postData() || '' });
    await route.fulfill({ status: 200, contentType: 'application/json', body: '{"ok":true}' });
  });

  await page.route('**/api/terminal/sessions/term_test', async (route) => {
    await route.fulfill({ status: 200, contentType: 'application/json', body: '{"closed":true}' });
  });

  await page.goto('/terminal');
  await expect(page.getByRole('heading', { name: 'Shell' })).toBeVisible();
  await expect(page.getByText('ready')).toBeVisible();

  const command = page.getByRole('textbox', { name: 'Command' });
  await command.fill('pwd');
  await page.getByRole('button', { name: 'Send' }).click();
  await page.getByRole('button', { name: 'Ctrl-C' }).click();
  await page.getByRole('button', { name: 'Ctrl-D' }).click();
  await page.getByRole('button', { name: 'Ctrl-Z' }).click();
  await page.getByRole('button', { name: 'Tab' }).click();
  await page.getByRole('button', { name: 'Up' }).click();

  expect(requests.some((request) => request.body.includes('pwd\\n'))).toBe(true);
  expect(requests.some((request) => request.body.includes('interrupt'))).toBe(true);
  expect(requests.some((request) => request.body.includes('\\\\u0004'))).toBe(true);
  expect(requests.some((request) => request.body.includes('\\\\u001a'))).toBe(true);
  expect(requests.some((request) => request.body.includes('\\\\t'))).toBe(true);
  expect(requests.some((request) => request.body.includes('\\\\u001b[A'))).toBe(true);

  const overflow = await page.evaluate(() => ({
    bodyWidth: document.body.scrollWidth,
    viewport: window.innerWidth
  }));
  expect(overflow.bodyWidth, JSON.stringify(overflow)).toBeLessThanOrEqual(overflow.viewport + 2);
});
