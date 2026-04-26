import { expect, test } from '@playwright/test';
import type { Page } from '@playwright/test';

const themeStorageKey = 'homelabd.dashboard.theme';
const now = '2026-04-26T15:00:00.000Z';

type ThemePage = {
  name: string;
  path: string;
  surfaces: string[];
  ready(page: Page): Promise<void>;
  prepare?(page: Page): Promise<void>;
};

const mockHealthdSnapshot = {
  status: 'healthy',
  started_at: '2026-04-26T12:00:00.000Z',
  uptime_seconds: 10_800,
  window_seconds: 300,
  current: {
    time: now,
    good: true,
    cpu_usage_percent: 18.4,
    memory_usage_percent: 42.7,
    memory_used_bytes: 2_400_000_000,
    memory_total_bytes: 8_000_000_000,
    load1: 0.42,
    load5: 0.39,
    load15: 0.35,
    system_uptime_seconds: 193_000,
    process_uptime_seconds: 10_800,
    goroutines: 24
  },
  samples: [
    {
      time: '2026-04-26T14:56:00.000Z',
      good: true,
      cpu_usage_percent: 13,
      memory_usage_percent: 40,
      memory_used_bytes: 2_100_000_000,
      memory_total_bytes: 8_000_000_000,
      load1: 0.28,
      load5: 0.31,
      load15: 0.33,
      system_uptime_seconds: 192_760,
      process_uptime_seconds: 10_560,
      goroutines: 22
    },
    {
      time: now,
      good: true,
      cpu_usage_percent: 18.4,
      memory_usage_percent: 42.7,
      memory_used_bytes: 2_400_000_000,
      memory_total_bytes: 8_000_000_000,
      load1: 0.42,
      load5: 0.39,
      load15: 0.35,
      system_uptime_seconds: 193_000,
      process_uptime_seconds: 10_800,
      goroutines: 24
    }
  ],
  processes: [
    {
      name: 'homelabd',
      type: 'service',
      status: 'healthy',
      message: 'accepting requests',
      pid: 2401,
      addr: '127.0.0.1:18080',
      started_at: '2026-04-26T12:00:00.000Z',
      last_seen: now,
      ttl_seconds: 30
    },
    {
      name: 'healthd',
      type: 'daemon',
      status: 'warning',
      message: 'slow sample',
      pid: 2402,
      addr: '127.0.0.1:18081',
      started_at: '2026-04-26T12:00:00.000Z',
      last_seen: now,
      ttl_seconds: 30
    }
  ],
  slos: [
    {
      name: 'Dashboard availability',
      target_percent: 99.9,
      window_seconds: 300,
      good_events: 299,
      total_events: 300,
      sli_percent: 99.667,
      error_budget_remaining_percent: 66.7,
      burn_rate: 0.8,
      status: 'healthy'
    }
  ],
  checks: [
    {
      name: 'HTTP probe',
      type: 'http',
      status: 'healthy',
      message: '200 OK',
      latency_ms: 12,
      last_checked: now
    }
  ],
  notifications: [
    {
      id: 'notification_theme',
      time: now,
      severity: 'info',
      status: 'resolved',
      source: 'healthd',
      message: 'sample notification',
      delivered: []
    }
  ]
};

const mockSupervisorSnapshot = {
  status: 'healthy',
  started_at: '2026-04-26T12:00:00.000Z',
  restartable: true,
  restart_hint: 'systemctl restart homelabd-supervisord',
  apps: [
    {
      name: 'dashboard',
      type: 'service',
      state: 'running',
      desired: 'running',
      pid: 3100,
      restarts: 1,
      message: 'serving requests',
      started_at: '2026-04-26T12:00:00.000Z',
      updated_at: now,
      start_order: 10,
      restart: 'always',
      health_url: 'http://127.0.0.1:5173/health',
      last_output: 'dashboard ready',
      working_dir: '/workspace',
      command: 'bun',
      args: ['run', 'dev']
    }
  ]
};

const mockTasks = [
  {
    id: 'task_20260426_150000_11111111',
    title: 'Audit dashboard theme modes',
    goal: 'Verify light and dark surfaces on dashboard pages.',
    status: 'awaiting_approval',
    assigned_to: 'codex',
    priority: 5,
    created_at: now,
    updated_at: now,
    result: 'waiting for approval',
    plan: {
      status: 'reviewed',
      summary: 'Check every dashboard route.',
      steps: [{ title: 'Toggle theme', detail: 'Verify visible surfaces.' }],
      risks: ['Theme selectors can miss route-specific panels.'],
      review: 'Plan is ready.',
      created_at: now,
      reviewed_at: now
    }
  },
  {
    id: 'task_20260426_150100_22222222',
    title: 'Keep worker trace visible',
    goal: 'Keep worker controls readable.',
    status: 'running',
    assigned_to: 'codex',
    priority: 5,
    created_at: now,
    updated_at: now
  }
];

const mockDashboardApis = async (page: Page) => {
  await page.route(/\/api\/message$/, async (route) => {
    await route.fulfill({ json: { reply: 'theme check acknowledged', source: 'program' } });
  });
  await page.route(/\/api\/tasks\/?(?:\?.*)?$/, async (route) => {
    await route.fulfill({ json: { tasks: mockTasks } });
  });
  await page.route(/\/api\/tasks\/[^/]+\/runs\/?(?:\?.*)?$/, async (route) => {
    await route.fulfill({
      json: {
        runs: [
          {
            id: 'run_theme',
            kind: 'worker',
            task_id: mockTasks[0].id,
            backend: 'codex',
            workspace: '/workspace',
            status: 'running',
            command: ['bun', 'test'],
            output: 'running theme regression',
            started_at: now
          }
        ]
      }
    });
  });
  await page.route(/\/api\/approvals\/?(?:\?.*)?$/, async (route) => {
    await route.fulfill({
      json: {
        approvals: [
          {
            id: 'approval_20260426_150000_aaaaaaaa',
            task_id: mockTasks[0].id,
            tool: 'exec_command',
            reason: 'Run browser UAT',
            status: 'pending',
            created_at: now,
            updated_at: now
          }
        ]
      }
    });
  });
  await page.route(/\/api\/events\/?(?:\?.*)?$/, async (route) => {
    await route.fulfill({ json: { events: [] } });
  });

  await page.route(/\/terminal\/sessions$/, async (route) => {
    await route.fulfill({
      status: 201,
      contentType: 'application/json',
      body: JSON.stringify({ id: 'term_theme', shell: '/bin/sh', cwd: '/workspace' })
    });
  });
  await page.route(/\/terminal\/sessions\/term_theme\/events$/, async (route) => {
    await route.fulfill({
      status: 200,
      contentType: 'text/event-stream',
      body: [
        'event: ready',
        'data: {}',
        '',
        'event: output',
        `data: ${JSON.stringify({ type: 'output', data: 'ready\\n' })}`,
        '',
        ''
      ].join('\n')
    });
  });
  await page.route(/\/terminal\/sessions\/term_theme\/input$/, async (route) => {
    await route.fulfill({ json: { ok: true } });
  });
  await page.route(/\/terminal\/sessions\/term_theme\/signal$/, async (route) => {
    await route.fulfill({ json: { ok: true } });
  });
  await page.route(/\/terminal\/sessions\/term_theme$/, async (route) => {
    await route.fulfill({ json: { closed: true } });
  });

  await page.route(/\/supervisord-api\/supervisord$/, async (route) => {
    await route.fulfill({ json: mockSupervisorSnapshot });
  });
  await page.route(/\/supervisord-api\/supervisord\/restart$/, async (route) => {
    await route.fulfill({ json: { reply: 'supervisor restart queued' } });
  });
  await page.route(
    /\/supervisord-api\/supervisord\/apps\/[^/]+\/(?:start|stop|restart)$/,
    async (route) => {
      await route.fulfill({ json: mockSupervisorSnapshot });
    }
  );

  await page.route(/\/healthd-api\/healthd(?:\?.*)?$/, async (route) => {
    await route.fulfill({ json: mockHealthdSnapshot });
  });
  await page.route(/\/healthd-api\/healthd\/checks\/run$/, async (route) => {
    await route.fulfill({ json: mockHealthdSnapshot });
  });
};

const themePages: ThemePage[] = [
  {
    name: 'chat',
    path: '/chat',
    surfaces: ['.chat-card', '.message', '.prompt-actions button', '.composer'],
    async ready(page) {
      await expect(page.getByRole('textbox', { name: 'Message' })).toBeVisible();
      await expect(page.getByText('This is global chat')).toBeVisible();
    }
  },
  {
    name: 'tasks',
    path: '/tasks',
    surfaces: [
      '.task-pane',
      '.triage button',
      '.task-row.selected',
      '.task-record',
      '.command-panel'
    ],
    async ready(page) {
      await expect(page.getByRole('heading', { name: 'Task queue' })).toBeVisible();
      await expect(page.getByText('Audit dashboard theme modes')).toBeVisible();
    },
    async prepare(page) {
      await page.getByRole('button', { name: /All/ }).click();
      await page.locator('.task-row').first().click();
      await expect(page.locator('.task-row.selected')).toBeVisible();
    }
  },
  {
    name: 'terminal',
    path: '/terminal',
    surfaces: ['.terminal-panel', '.terminal-header', '.terminal-notice', '.terminal-composer'],
    async ready(page) {
      await expect(page.getByText('Operator shell')).toBeVisible();
      await expect(page.getByRole('heading', { name: '/workspace' })).toBeVisible();
      await expect(page.getByText('ready')).toBeVisible();
    }
  },
  {
    name: 'supervisord',
    path: '/supervisord',
    surfaces: ['.hero', '.app', 'code'],
    async ready(page) {
      await expect(page.getByText('Process control')).toBeVisible();
      await expect(page.getByRole('heading', { name: 'dashboard' })).toBeVisible();
    }
  },
  {
    name: 'healthd',
    path: '/healthd',
    surfaces: ['.toolbar', '.metric', '.chart-panel', '.process', '.slo', '.check', '.notifications'],
    async ready(page) {
      await expect(page.getByRole('heading', { name: 'Processes' })).toBeVisible();
      await expect(page.locator('.process').filter({ hasText: 'homelabd' })).toBeVisible();
      await expect(page.locator('.process')).toHaveCount(2);
    }
  }
];

const initLightTheme = async (page: Page) => {
  await page.addInitScript((key) => {
    localStorage.setItem(key, 'light');
  }, themeStorageKey);
};

const waitForThemeRuntime = async (page: Page, mode = 'light') => {
  await expect(page.locator('.theme-toggle').first()).toHaveAttribute(
    'data-theme-toggle-ready',
    'true'
  );
  await expect
    .poll(() => page.evaluate(() => document.documentElement.style.colorScheme))
    .toBe(mode);
};

const readyThemeToggle = async (page: Page, name: string | RegExp) => {
  const toggle = page.getByRole('button', { name });
  await expect(toggle).toHaveAttribute('data-theme-toggle-ready', 'true');
  return toggle;
};

const collectSurfaceStyles = async (page: Page, selectors: string[]) =>
  page.evaluate((surfaceSelectors) => {
    const parseRuntimeColor = (value: string) => {
      const parts = value.match(/[\d.]+/g)?.map(Number) ?? [0, 0, 0];
      return {
        r: parts[0] ?? 0,
        g: parts[1] ?? 0,
        b: parts[2] ?? 0,
        a: parts[3] ?? 1
      };
    };
    const luminance = ({ r, g, b }: ReturnType<typeof parseRuntimeColor>) => {
      const channels = [r, g, b].map((channel) => {
        const normalized = channel / 255;
        return normalized <= 0.03928
          ? normalized / 12.92
          : ((normalized + 0.055) / 1.055) ** 2.4;
      });
      return 0.2126 * channels[0] + 0.7152 * channels[1] + 0.0722 * channels[2];
    };
    const ratio = (left: number, right: number) => {
      const [lighter, darker] = left >= right ? [left, right] : [right, left];
      return (lighter + 0.05) / (darker + 0.05);
    };

    return surfaceSelectors.map((selector) => {
      const element = document.querySelector(selector) as HTMLElement | null;
      if (!element) {
        return {
          selector,
          found: false,
          visible: false,
          backgroundColor: '',
          color: '',
          backgroundLuminance: 1,
          contrast: 1
        };
      }
      const rect = element.getBoundingClientRect();
      const styles = getComputedStyle(element);
      const background = parseRuntimeColor(styles.backgroundColor);
      const text = parseRuntimeColor(styles.color);
      const backgroundLuminance = luminance(background);
      const textLuminance = luminance(text);
      return {
        selector,
        found: true,
        visible: rect.width > 0 && rect.height > 0 && styles.visibility !== 'hidden',
        backgroundColor: styles.backgroundColor,
        color: styles.color,
        backgroundLuminance,
        contrast: ratio(backgroundLuminance, textLuminance)
      };
    });
  }, selectors);

const assertLightSurfaces = async (page: Page, routeName: string, selectors: string[]) => {
  const styles = await collectSurfaceStyles(page, selectors);
  for (const style of styles) {
    expect(style.found, `${routeName} missing light surface ${style.selector}`).toBe(true);
    expect(style.visible, `${routeName} hidden light surface ${style.selector}`).toBe(true);
    expect(
      style.backgroundLuminance,
      `${routeName} ${style.selector} should use a light surface: ${JSON.stringify(style)}`
    ).toBeGreaterThan(0.72);
    expect(
      style.contrast,
      `${routeName} ${style.selector} should keep readable text in light mode: ${JSON.stringify(style)}`
    ).toBeGreaterThan(3);
  }
  return styles;
};

const assertDarkSurfaces = async (page: Page, routeName: string, selectors: string[]) => {
  const styles = await collectSurfaceStyles(page, selectors);
  for (const style of styles) {
    expect(style.found, `${routeName} missing dark surface ${style.selector}`).toBe(true);
    expect(style.visible, `${routeName} hidden dark surface ${style.selector}`).toBe(true);
    expect(
      style.backgroundLuminance,
      `${routeName} ${style.selector} should use a dark surface: ${JSON.stringify(style)}`
    ).toBeLessThan(0.12);
    expect(
      style.contrast,
      `${routeName} ${style.selector} should keep readable text in dark mode: ${JSON.stringify(style)}`
    ).toBeGreaterThan(3);
  }
  return styles;
};

test.describe('dashboard theme modes on desktop', () => {
  test.use({ viewport: { width: 1280, height: 900 }, isMobile: false, hasTouch: false });

  for (const themePage of themePages) {
    test(`${themePage.name} toggles light and dark surfaces`, async ({ page }) => {
      await mockDashboardApis(page);
      await initLightTheme(page);
      await page.goto(themePage.path);
      await themePage.ready(page);
      await themePage.prepare?.(page);

      await expect(page.locator('html')).toHaveAttribute('data-theme', 'light');
      await waitForThemeRuntime(page, 'light');
      const darkToggle = await readyThemeToggle(page, 'Switch to dark mode');
      await expect(darkToggle).toHaveAttribute('aria-pressed', 'false');
      const lightStyles = await assertLightSurfaces(page, themePage.name, themePage.surfaces);

      await darkToggle.click();
      await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark');
      const lightToggle = await readyThemeToggle(page, 'Switch to light mode');
      await expect(lightToggle).toHaveAttribute('aria-pressed', 'true');
      await expect
        .poll(() => page.evaluate((key) => localStorage.getItem(key), themeStorageKey))
        .toBe('dark');
      await waitForThemeRuntime(page, 'dark');
      const darkStyles = await assertDarkSurfaces(page, themePage.name, themePage.surfaces);

      for (const [index, lightStyle] of lightStyles.entries()) {
        expect(
          darkStyles[index].backgroundColor,
          `${themePage.name} ${lightStyle.selector} background should change between modes`
        ).not.toBe(lightStyle.backgroundColor);
      }

      await lightToggle.click();
      await expect(page.locator('html')).toHaveAttribute('data-theme', 'light');
      await expect(await readyThemeToggle(page, 'Switch to dark mode')).toHaveAttribute(
        'aria-pressed',
        'false'
      );
    });
  }
});

test.describe('dashboard theme modes on mobile', () => {
  test.use({ viewport: { width: 390, height: 844 }, isMobile: true, hasTouch: true });

  test('healthd process cards switch from the mobile menu without page overflow', async ({ page }) => {
    await mockDashboardApis(page);
    await initLightTheme(page);
    await page.goto('/healthd');
    await expect(page.getByRole('heading', { name: 'Processes' })).toBeVisible();
    await expect(page.locator('.process')).toHaveCount(2);
    await waitForThemeRuntime(page, 'light');

    await page.getByRole('button', { name: 'Menu' }).click();
    const darkToggle = await readyThemeToggle(page, 'Switch to dark mode');
    await expect(darkToggle).toBeVisible();
    await darkToggle.click();
    await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark');
    await assertDarkSurfaces(page, 'healthd mobile', ['.process', '.notifications']);

    const overflow = await page.evaluate(() => ({
      bodyWidth: document.body.scrollWidth,
      viewport: window.innerWidth
    }));
    expect(overflow.bodyWidth, JSON.stringify(overflow)).toBeLessThanOrEqual(overflow.viewport + 2);
  });
});
