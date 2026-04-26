import { expect, test } from '@playwright/test';

const installTerminalMocks = async (page) => {
  await page.addInitScript(() => {
    const encoder = new TextEncoder();
    window.__terminalSent = [];
    window.__terminalSockets = [];

    class MockWebSocket {
      static CONNECTING = 0;
      static OPEN = 1;
      static CLOSING = 2;
      static CLOSED = 3;

      readyState = MockWebSocket.CONNECTING;
      binaryType = 'arraybuffer';
      onopen = null;
      onmessage = null;
      onerror = null;
      onclose = null;

      constructor(url) {
        this.url = url;
        window.__terminalSockets.push(this);
        setTimeout(() => {
          this.readyState = MockWebSocket.OPEN;
          this.onopen?.(new Event('open'));
          this.emit('ready\r\n\u001b[31mred\u001b[0m\r\n');
        }, 20);
      }

      send(data) {
        window.__terminalSent.push(String(data));
        if (String(data).includes('pwd')) {
          this.emit('/workspace\r\n');
        }
      }

      close() {
        this.readyState = MockWebSocket.CLOSED;
        this.onclose?.(new CloseEvent('close'));
      }

      emit(text) {
        this.onmessage?.({ data: encoder.encode(text).buffer });
      }
    }

    window.WebSocket = MockWebSocket;
  });

  await page.route('**/api/terminal/sessions', async (route) => {
    await route.fulfill({
      status: 201,
      contentType: 'application/json',
      body: JSON.stringify({
        id: 'term_test',
        shell: '/bin/sh',
        cwd: '/workspace',
        created_at: '2026-04-26T00:00:00Z'
      })
    });
  });

  await page.route('**/api/terminal/sessions/term_test/resize', async (route) => {
    await route.fulfill({ status: 200, contentType: 'application/json', body: '{"ok":true,"cols":100,"rows":30}' });
  });

  await page.route('**/api/terminal/sessions/term_test', async (route) => {
    await route.fulfill({ status: 200, contentType: 'application/json', body: '{"closed":true}' });
  });
};

test('terminal mobile accepts direct typing and control keys without horizontal overflow', async ({ page }) => {
  await installTerminalMocks(page);

  await page.goto('/terminal');
  await expect(page.getByText('Operator PTY')).toBeVisible();
  await expect(page.locator('.xterm')).toBeVisible();

  await page.locator('.xterm').click();
  await page.keyboard.type('pwd');
  await page.keyboard.press('Enter');
  await page.getByRole('button', { name: /Ctrl-C/ }).click();
  await page.getByRole('button', { name: /Ctrl-D/ }).click();
  await page.getByRole('button', { name: /Ctrl-Z/ }).click();
  await page.getByRole('button', { name: /Tab/ }).click();

  await expect.poll(async () => page.evaluate(() => window.__terminalSent.join(''))).toContain('pwd');
  const sent = await page.evaluate(() => window.__terminalSent);
  expect(sent).toContain('\u0003');
  expect(sent).toContain('\u0004');
  expect(sent).toContain('\u001a');
  expect(sent).toContain('\t');

  const layout = await page.evaluate(() => ({
    bodyWidth: document.body.scrollWidth,
    viewport: window.innerWidth,
    terminalHeight: Math.round(document.querySelector('.terminal-host')?.getBoundingClientRect().height || 0),
    controlsHeight: Math.round(document.querySelector('.terminal-controls')?.getBoundingClientRect().height || 0),
    sockets: window.__terminalSockets.map((socket) => socket.url)
  }));
  expect(layout.bodyWidth, JSON.stringify(layout)).toBeLessThanOrEqual(layout.viewport + 2);
  expect(layout.terminalHeight, JSON.stringify(layout)).toBeGreaterThan(240);
  expect(layout.controlsHeight, JSON.stringify(layout)).toBeLessThan(100);
  expect(layout.sockets.some((url) => url.includes('/api/terminal/sessions/term_test/ws')), JSON.stringify(layout)).toBe(true);
});

test('terminal keeps large du-style output inside the terminal viewport', async ({ page }) => {
  await installTerminalMocks(page);
  await page.goto('/terminal');
  await expect(page.locator('.xterm')).toBeVisible();

  await page.evaluate(() => {
    const socket = window.__terminalSockets[0];
    const largeOutput = Array.from({ length: 600 }, (_, index) => {
      const path = `/nix/store/${String(index).padStart(4, '0')}-very-long-package-name-with-a-path-that-should-not-widen-the-page/share/doc/examples/nested/${'segment-'.repeat(12)}${index}`;
      return `${index * 4096}\t${path}`;
    }).join('\r\n');
    socket.emit(largeOutput);
  });

  const layout = await page.evaluate(() => {
    const host = document.querySelector('.terminal-host') as HTMLElement | null;
    const viewport = document.querySelector('.xterm-viewport') as HTMLElement | null;
    const panel = document.querySelector('.terminal-panel') as HTMLElement | null;
    return {
      bodyWidth: document.body.scrollWidth,
      viewportWidth: window.innerWidth,
      bodyHeight: document.body.scrollHeight,
      viewportHeight: window.innerHeight,
      hostHeight: host?.clientHeight || 0,
      xtermOverflowY: viewport ? getComputedStyle(viewport).overflowY : '',
      xtermClientHeight: viewport?.clientHeight || 0,
      panelHeight: Math.round(panel?.getBoundingClientRect().height || 0)
    };
  });

  expect(layout.bodyWidth, JSON.stringify(layout)).toBeLessThanOrEqual(layout.viewportWidth + 2);
  expect(layout.bodyHeight, JSON.stringify(layout)).toBeLessThanOrEqual(layout.viewportHeight + 2);
  expect(layout.hostHeight, JSON.stringify(layout)).toBeGreaterThan(240);
  expect(layout.xtermOverflowY, JSON.stringify(layout)).toMatch(/auto|scroll/);
  expect(layout.xtermClientHeight, JSON.stringify(layout)).toBeGreaterThan(240);
  expect(layout.panelHeight, JSON.stringify(layout)).toBeLessThanOrEqual(layout.viewportHeight);
});
