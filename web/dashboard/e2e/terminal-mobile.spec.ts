import { expect, test } from '@playwright/test';

const routeTerminal = async (page, output: string, requests: { path: string; body: string }[] = []) => {
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
      body: `event: output\ndata: ${JSON.stringify({ type: 'output', data: output })}\n\n`
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
};

test('terminal mobile sends command and control keys without horizontal overflow', async ({ page }) => {
  const requests: { path: string; body: string }[] = [];
  await routeTerminal(page, 'ready\n', requests);

  await page.goto('/terminal');
  await expect(page.getByText('Operator shell')).toBeVisible();
  await expect(page.getByRole('heading', { name: '/workspace' })).toBeVisible();
  await expect(page.getByText('ready')).toBeVisible();

  const command = page.getByRole('textbox', { name: 'Command' });
  await command.fill('pwd');
  await page.getByRole('button', { name: 'Send' }).click();
  await page.getByRole('button', { name: /Ctrl-C/ }).click();
  await page.getByRole('button', { name: /Ctrl-D/ }).click();
  await page.getByRole('button', { name: /Ctrl-Z/ }).click();
  await page.getByRole('button', { name: /Tab/ }).click();
  await page.getByRole('button', { name: /Up/ }).click();

  const bodies = requests.map((request) => {
    try {
      return JSON.parse(request.body);
    } catch {
      return {};
    }
  });
  expect(bodies.some((body) => body.data === 'pwd\n')).toBe(true);
  expect(bodies.some((body) => body.signal === 'interrupt')).toBe(true);
  expect(bodies.some((body) => body.data === '\u0004')).toBe(true);
  expect(bodies.some((body) => body.data === '\u001a')).toBe(true);
  expect(bodies.some((body) => body.data === '\t')).toBe(true);
  expect(bodies.some((body) => body.data === '\u001b[A')).toBe(true);

  const overflow = await page.evaluate(() => ({
    bodyWidth: document.body.scrollWidth,
    viewport: window.innerWidth,
    outputHeight: Math.round(document.querySelector('.terminal-output')?.getBoundingClientRect().height || 0),
    controlsHeight: Math.round(document.querySelector('.terminal-controls')?.getBoundingClientRect().height || 0)
  }));
  expect(overflow.bodyWidth, JSON.stringify(overflow)).toBeLessThanOrEqual(overflow.viewport + 2);
  expect(overflow.outputHeight, JSON.stringify(overflow)).toBeGreaterThan(240);
  expect(overflow.controlsHeight, JSON.stringify(overflow)).toBeLessThan(175);
});

test('terminal keeps large du-style output inside the scroll pane', async ({ page }) => {
  const largeOutput = Array.from({ length: 600 }, (_, index) => {
    const path = `/nix/store/${String(index).padStart(4, '0')}-very-long-package-name-with-a-path-that-should-not-widen-the-page/share/doc/examples/nested/${'segment-'.repeat(12)}${index}`;
    return `${index * 4096}\t${path}`;
  }).join('\n');
  await routeTerminal(page, largeOutput);

  await page.goto('/terminal');
  await expect(page.getByText('/nix/store/0000')).toBeVisible();

  const layout = await page.evaluate(() => {
    const output = document.querySelector('.terminal-output') as HTMLElement | null;
    const panel = document.querySelector('.terminal-panel') as HTMLElement | null;
    return {
      bodyWidth: document.body.scrollWidth,
      viewport: window.innerWidth,
      bodyHeight: document.body.scrollHeight,
      viewportHeight: window.innerHeight,
      outputClientHeight: output?.clientHeight || 0,
      outputScrollHeight: output?.scrollHeight || 0,
      outputClientWidth: output?.clientWidth || 0,
      outputScrollWidth: output?.scrollWidth || 0,
      panelHeight: Math.round(panel?.getBoundingClientRect().height || 0)
    };
  });

  expect(layout.bodyWidth, JSON.stringify(layout)).toBeLessThanOrEqual(layout.viewport + 2);
  expect(layout.bodyHeight, JSON.stringify(layout)).toBeLessThanOrEqual(layout.viewportHeight + 2);
  expect(layout.outputScrollHeight, JSON.stringify(layout)).toBeGreaterThan(layout.outputClientHeight * 5);
  expect(layout.outputScrollWidth, JSON.stringify(layout)).toBeLessThanOrEqual(layout.outputClientWidth + 2);
  expect(layout.panelHeight, JSON.stringify(layout)).toBeLessThanOrEqual(layout.viewportHeight);
});
