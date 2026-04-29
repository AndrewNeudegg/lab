import { expect, test } from '@playwright/test';

const installTerminalMocks = async (page) => {
  const state = {
    created: 0,
    deleted: [] as string[],
    sessionGets: 0,
    sent: [] as string[],
    resumeFailuresRemaining: 0,
    resumeFailureStatus: 503
  };
  const sessions = new Map();

  await page.context().addInitScript(() => {
    const encoder = new TextEncoder();
    window.__terminalSockets = [];
    window.__terminalEventSources = [];

    class MockEventSource extends EventTarget {
      static CONNECTING = 0;
      static OPEN = 1;
      static CLOSED = 2;

      readyState = MockEventSource.CONNECTING;
      onopen = null;
      onerror = null;

      constructor(url) {
        super();
        this.url = url;
        window.__terminalEventSources.push(this);
        setTimeout(() => {
          if (this.readyState === MockEventSource.CLOSED) {
            return;
          }
          this.readyState = MockEventSource.OPEN;
          this.onopen?.(new Event('open'));
          this.dispatchEvent(new MessageEvent('ready', { data: '{}' }));
          this.emit('ready\r\n\u001b[31mred\u001b[0m\r\n');
        }, 20);
      }

      close() {
        this.readyState = MockEventSource.CLOSED;
      }

      fail() {
        this.readyState = MockEventSource.CLOSED;
        this.onerror?.(new Event('error'));
      }

      emit(text) {
        this.dispatchEvent(new MessageEvent('output', {
          data: JSON.stringify({ type: 'output', seq: Date.now(), data: text })
        }));
      }
    }

    window.EventSource = MockEventSource;

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

  await page.context().route('**/api/terminal/sessions', async (route) => {
    state.created += 1;
    const id = state.created === 1 ? 'term_test' : `term_test_${state.created}`;
    const session = {
      id,
      shell: '/bin/sh',
      cwd: `/workspace/${state.created}`,
      created_at: '2026-04-26T00:00:00Z',
      persistent: true
    };
    sessions.set(id, session);
    await route.fulfill({
      status: 201,
      contentType: 'application/json',
      body: JSON.stringify(session)
    });
  });

  await page.context().route('**/api/terminal/sessions/*/resize', async (route) => {
    await route.fulfill({ status: 200, contentType: 'application/json', body: '{"ok":true,"cols":100,"rows":30}' });
  });

  await page.context().route('**/api/terminal/sessions/*/input', async (route) => {
    const body = JSON.parse(route.request().postData() || '{}');
    state.sent.push(String(body.data || ''));
    await route.fulfill({ status: 200, contentType: 'application/json', body: '{"ok":true}' });
  });

  await page.context().route(/\/api\/terminal\/sessions\/[^/]+$/, async (route) => {
    const id = route.request().url().split('/').pop();
    if (route.request().method() === 'GET') {
      state.sessionGets += 1;
      if (state.resumeFailuresRemaining > 0) {
        state.resumeFailuresRemaining -= 1;
        await route.fulfill({
          status: state.resumeFailureStatus,
          contentType: 'application/json',
          body: JSON.stringify({ error: 'temporary terminal API outage' })
        });
        return;
      }
      const session = sessions.get(id);
      await route.fulfill({
        status: session ? 200 : 404,
        contentType: 'application/json',
        body: JSON.stringify(session || { error: 'not found' })
      });
      return;
    }
    if (route.request().method() === 'DELETE') {
      state.deleted.push(id);
      sessions.delete(id);
      await route.fulfill({ status: 200, contentType: 'application/json', body: '{"closed":true}' });
      return;
    }
    await route.fulfill({ status: 405, contentType: 'application/json', body: '{"error":"method not allowed"}' });
  });

  await page.context().route('**/api/agents', async (route) => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        agents: [
          {
            id: 'desk',
            name: 'Desk',
            machine: 'desk.local',
            status: 'online',
            last_seen: '2026-04-26T00:00:00Z',
            metadata: { terminal_base_url: 'http://desk.local:18083' }
          },
          {
            id: 'offline',
            name: 'Offline',
            machine: 'offline.local',
            status: 'offline',
            last_seen: '2026-04-26T00:00:00Z',
            metadata: { terminal_base_url: 'http://offline.local:18083' }
          }
        ]
      })
    });
  });

  await page.context().route('**/api/tasks', async (route) => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        tasks: [
          {
            id: 'task_terminal_nav',
            title: 'Terminal navigation check',
            goal: 'Exercise route changes while a terminal is alive.',
            status: 'queued',
            assigned_to: 'codex',
            created_at: '2026-04-28T00:00:00Z',
            updated_at: '2026-04-28T00:00:00Z'
          }
        ]
      })
    });
  });
  await page.context().route('**/api/approvals', async (route) => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ approvals: [] })
    });
  });

  await page.context().route('**/api/tasks/*/runs', async (route) => {
    await route.fulfill({ status: 200, contentType: 'application/json', body: '{"runs":[]}' });
  });

  await page.context().route('**/api/tasks/*/diff', async (route) => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: '{"task_id":"task_terminal_nav","raw_diff":"","summary":{"files":0,"additions":0,"deletions":0},"files":[]}'
    });
  });

  return state;
};

test('terminal mobile accepts direct typing and control keys without horizontal overflow', async ({ page }) => {
  const state = await installTerminalMocks(page);

  await page.goto('/terminal');
  await expect(page.getByText('Operator PTY')).toHaveCount(0);
  await expect(page.getByText('Click in the terminal and type normally')).toHaveCount(0);
  await expect(page.getByRole('button', { name: 'Terminal 1', exact: true })).toBeVisible();
  await expect(page.locator('.xterm')).toBeVisible();

  await page.locator('.xterm').click();
  await expect(page.getByLabel('Session target')).toContainText('homelabd local');
  await expect(page.getByLabel('Session target')).toContainText('Desk');
  await expect(page.getByLabel('Session target')).not.toContainText('Offline');
  await page.keyboard.type('pwd');
  await page.keyboard.press('Enter');
  await page.keyboard.press('ArrowLeft');
  await page.keyboard.press('ArrowRight');
  await page.keyboard.press('ArrowUp');
  await page.keyboard.press('ArrowDown');
  await expect.poll(() => state.sent.join('')).toContain('\u001b[D');
  const physicalSent = state.sent;
  expect(physicalSent).toContain('\u001b[D');
  expect(physicalSent).toContain('\u001b[C');
  expect(physicalSent).toContain('\u001b[A');
  expect(physicalSent).toContain('\u001b[B');
  await page.getByRole('button', { name: /Ctrl-C/ }).click();
  await page.getByRole('button', { name: /Ctrl-D/ }).click();
  await page.getByRole('button', { name: /Ctrl-Z/ }).click();
  await page.getByRole('button', { name: /Tab/ }).click();
  await page.getByRole('button', { name: '←' }).click();
  await page.getByRole('button', { name: '→' }).click();
  await page.getByRole('button', { name: '↑' }).click();
  await page.getByRole('button', { name: '↓' }).click();

  await expect.poll(() => state.sent.join('')).toContain('pwd');
  const sent = state.sent;
  expect(sent).toContain('\u0003');
  expect(sent).toContain('\u0004');
  expect(sent).toContain('\u001a');
  expect(sent).toContain('\t');
  expect(sent).toContain('\u001b[D');
  expect(sent).toContain('\u001b[C');
  expect(sent).toContain('\u001b[A');
  expect(sent).toContain('\u001b[B');
  await expect(page.getByText('Interrupt foreground job')).toHaveCount(0);

  const layout = await page.evaluate(() => ({
    bodyWidth: document.body.scrollWidth,
    viewport: window.innerWidth,
    terminalHeight: Math.round(document.querySelector('.terminal-host')?.getBoundingClientRect().height || 0),
    controlsHeight: Math.round(document.querySelector('.terminal-controls')?.getBoundingClientRect().height || 0),
    eventSources: window.__terminalEventSources.map((source) => source.url)
  }));
  expect(layout.bodyWidth, JSON.stringify(layout)).toBeLessThanOrEqual(layout.viewport + 2);
  expect(layout.terminalHeight, JSON.stringify(layout)).toBeGreaterThan(240);
  expect(layout.controlsHeight, JSON.stringify(layout)).toBeLessThan(100);
  expect(layout.eventSources.some((url) => url.includes('/api/terminal/sessions/term_test/events')), JSON.stringify(layout)).toBe(true);
});

test('terminal tabs can be renamed, added, closed, and reattached after reload', async ({ page }) => {
  const state = await installTerminalMocks(page);

  await page.goto('/terminal');
  await expect(page.locator('.xterm')).toBeVisible();
  await page.getByRole('button', { name: 'Terminal 1', exact: true }).click();
  await page.getByLabel('Rename terminal tab').fill('Ops');
  await page.keyboard.press('Enter');
  await expect(page.getByRole('button', { name: 'Ops', exact: true })).toBeVisible();

  await page.getByRole('button', { name: 'Add terminal tab' }).click();
  await expect(page.getByRole('button', { name: 'Terminal 2', exact: true })).toBeVisible();
  await expect.poll(() => state.created).toBe(2);

  await page.getByRole('button', { name: 'Ops', exact: true }).click();
  await expect(page.getByLabel('Rename terminal tab')).toHaveCount(0);
  await expect(page.getByRole('button', { name: 'Ops', exact: true })).toHaveAttribute('aria-current', 'page');
  await page.getByRole('button', { name: 'Ops', exact: true }).click();
  await expect(page.getByLabel('Rename terminal tab')).toBeVisible();
  await page.keyboard.press('Escape');
  await page.getByRole('button', { name: 'Terminal 2', exact: true }).click();

  await page.getByRole('button', { name: 'Close Terminal 2' }).click();
  await expect(page.getByRole('button', { name: 'Terminal 2', exact: true })).toHaveCount(0);
  expect(state.deleted).toContain('term_test_2');

  await page.reload();
  await expect(page.locator('.xterm')).toBeVisible();
  await expect(page.getByRole('button', { name: 'Ops', exact: true })).toBeVisible();
  await expect.poll(() => state.sessionGets).toBeGreaterThan(0);

  const eventSources = await page.evaluate(() => window.__terminalEventSources.map((source) => source.url));
  expect(eventSources.some((url) => url.includes('/api/terminal/sessions/term_test/events')), JSON.stringify(eventSources)).toBe(true);
});

test('terminal reconnects the same session after a dropped event stream', async ({ page }) => {
  const state = await installTerminalMocks(page);

  await page.goto('/terminal');
  await expect(page.locator('.xterm')).toBeVisible();
  await expect(page.getByText('Connected')).toBeVisible();
  await page.locator('.xterm').click();

  await page.evaluate(() => window.__terminalEventSources[0].fail());
  await expect(page.getByText('Reconnecting', { exact: true })).toBeVisible();
  await page.keyboard.type('echo queued while offline');
  await page.keyboard.press('Enter');
  await expect.poll(async () => page.evaluate(() => window.__terminalEventSources.length)).toBeGreaterThan(1);
  await expect(page.getByText('Connected')).toBeVisible();
  await expect.poll(() => state.sent.join('')).toContain('echo queued while offline');

  expect(state.created).toBe(1);
  await page.keyboard.type('echo after reconnect');
  await page.keyboard.press('Enter');
  await expect.poll(() => state.sent.join('')).toContain('echo after reconnect');

  const eventSources = await page.evaluate(() => window.__terminalEventSources.map((source) => source.url));
  expect(eventSources.filter((url) => url.includes('/api/terminal/sessions/term_test/events')).length).toBeGreaterThan(1);
});

test('terminal reattaches the same session after navigating to tasks and back', async ({ page }) => {
  const state = await installTerminalMocks(page);

  await page.goto('/terminal');
  await expect(page.locator('.xterm')).toBeVisible();
  await expect(page.getByText('Connected')).toBeVisible();
  await expect.poll(() => state.created).toBe(1);

  await page.evaluate(() => window.__terminalEventSources[0].emit('before tasks nav\r\n'));
  await page.goto('/tasks');
  await expect(page.getByRole('complementary', { name: 'Task queue' })).toBeVisible();

  await page.goto('/terminal');
  await expect(page.locator('.xterm')).toBeVisible();
  await expect(page.getByText('Connected')).toBeVisible();
  await expect.poll(() => state.sessionGets).toBeGreaterThan(0);
  expect(state.created).toBe(1);

  await page.locator('.xterm').click();
  await page.keyboard.type('echo after tasks page');
  await page.keyboard.press('Enter');
  await expect.poll(() => state.sent.join('')).toContain('echo after tasks page');

  const eventSources = await page.evaluate(() => window.__terminalEventSources.map((source) => source.url));
  expect(eventSources.some((url) => url.includes('/api/terminal/sessions/term_test/events')), JSON.stringify(eventSources)).toBe(true);
});

test('terminal reopens the same session after the browser window closes', async ({ page, context }) => {
  const state = await installTerminalMocks(page);

  await page.goto('/terminal');
  await expect(page.locator('.xterm')).toBeVisible();
  await expect(page.getByText('Connected')).toBeVisible();
  await expect.poll(() => state.created).toBe(1);

  await page.close();
  const reopened = await context.newPage();
  await reopened.goto('/terminal');
  await expect(reopened.locator('.xterm')).toBeVisible();
  await expect(reopened.getByText('Connected')).toBeVisible();
  await expect.poll(() => state.sessionGets).toBeGreaterThan(0);
  expect(state.created).toBe(1);

  await reopened.locator('.xterm').click();
  await reopened.keyboard.type('echo after browser reopen');
  await reopened.keyboard.press('Enter');
  await expect.poll(() => state.sent.join('')).toContain('echo after browser reopen');
});

test('terminal preserves a stored session through initial API outages', async ({ page }) => {
  const state = await installTerminalMocks(page);

  await page.goto('/terminal');
  await expect(page.locator('.xterm')).toBeVisible();
  await expect(page.getByText('Connected')).toBeVisible();
  await expect.poll(() => state.created).toBe(1);

  state.resumeFailuresRemaining = 2;
  await page.reload();
  await expect(page.locator('.xterm')).toBeVisible();
  await expect(page.getByText('Reconnecting', { exact: true })).toBeVisible();
  await expect(page.getByText('Connected')).toBeVisible({ timeout: 8_000 });
  await expect.poll(() => state.sessionGets).toBeGreaterThanOrEqual(3);
  expect(state.created).toBe(1);

  const stored = await page.evaluate(() => JSON.parse(window.localStorage.getItem('homelab-terminal-tabs:v1') || '{}'));
  expect(stored.tabs?.[0]?.session?.id).toBe('term_test');
});

test('terminal queues input while the browser is offline and flushes on online recovery', async ({ page, context }) => {
  const state = await installTerminalMocks(page);

  await page.goto('/terminal');
  await expect(page.locator('.xterm')).toBeVisible();
  await expect(page.getByText('Connected')).toBeVisible();
  await page.locator('.xterm').click();

  await context.setOffline(true);
  await page.evaluate(() => window.__terminalEventSources[0].fail());
  await expect(page.getByText('Reconnecting', { exact: true })).toBeVisible();
  await page.keyboard.type('echo typed with no signal');
  await page.keyboard.press('Enter');
  await expect(page.getByText('Connected', { exact: true })).toHaveCount(0);

  await context.setOffline(false);
  await expect(page.getByText('Connected')).toBeVisible({ timeout: 8_000 });
  await expect.poll(() => state.sent.join('')).toContain('echo typed with no signal');
  expect(state.created).toBe(1);
});

test('terminal keeps large du-style output inside the terminal viewport', async ({ page }) => {
  await installTerminalMocks(page);
  await page.goto('/terminal');
  await expect(page.locator('.xterm')).toBeVisible();

  await page.evaluate(() => {
    const source = window.__terminalEventSources[0];
    const largeOutput = Array.from({ length: 600 }, (_, index) => {
      const path = `/nix/store/${String(index).padStart(4, '0')}-very-long-package-name-with-a-path-that-should-not-widen-the-page/share/doc/examples/nested/${'segment-'.repeat(12)}${index}`;
      return `${index * 4096}\t${path}`;
    }).join('\r\n');
    source.emit(largeOutput);
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
