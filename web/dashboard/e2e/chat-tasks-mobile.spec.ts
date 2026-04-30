import { expect, test } from '@playwright/test';
import type { Page } from '@playwright/test';

const typedMessage = 'mobile input must keep every typed character 12345';

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

  await page.route(/\/api\/tasks(?:\?.*)?$/, async (route) => {
    await route.fulfill({ json: { tasks } });
  });
  let autoMergeEnabled = false;
  await page.route('**/api/settings', async (route) => {
    if (route.request().method() === 'POST') {
      const body = route.request().postDataJSON() as { auto_merge_enabled?: boolean };
      autoMergeEnabled = Boolean(body.auto_merge_enabled);
    }
    await route.fulfill({ json: { settings: { auto_merge_enabled: autoMergeEnabled } } });
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
  await page.locator('.triage button').filter({ hasText: 'All' }).click();
  await page.getByRole('link', { name: /Document mobile task flow/ }).click();
  await expect(page).toHaveURL(/\/tasks\?task=task_20260426_150200_33333333$/);
  await expect(page.getByRole('heading', { name: 'Document mobile task flow' })).toBeVisible();

  await page.goBack();
  await expect(page).toHaveURL(/\/tasks\?task=task_20260426_150000_11111111$/);
  await expect(page.getByRole('heading', { name: 'Review queue behavior on mobile' })).toBeVisible();
});

test('tasks mobile switches between queue and selected task detail', async ({ page }) => {
  test.setTimeout(60_000);
  await mockTaskApi(page);
  await page.goto('/tasks');

  const rows = page.getByRole('link', { name: /Review queue behavior on mobile/ });
  const queue = page.locator('.task-pane');
  const detail = page.locator('.workbench');
  await expect(page.getByRole('navigation', { name: 'Task panels' })).toHaveCount(0);
  await expect(page.getByText('Pending approvals')).toHaveCount(0);
  await expect(rows).toHaveCount(1, { timeout: 45_000 });
  await expect(queue).toBeVisible();
  await expect(detail).not.toBeVisible();
  const autoMerge = page.getByRole('switch', { name: 'Auto merge reviewed queue-head tasks' });
  await expect(autoMerge).toHaveAttribute('aria-checked', 'false');
  await autoMerge.click();
  await expect(autoMerge).toHaveAttribute('aria-checked', 'true');

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
  await expect(queue).toBeVisible();
  await expect(detail).not.toBeVisible();
  await expect(rows).toHaveCount(1);
  await expect(rows.first()).toBeVisible();

  const overflow = await page.evaluate(() => ({
    bodyWidth: document.body.scrollWidth,
    viewport: window.innerWidth
  }));
  expect(overflow.bodyWidth, JSON.stringify(overflow)).toBeLessThanOrEqual(overflow.viewport + 2);
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

test('chat renders Mermaid diagrams in assistant replies', async ({ page }) => {
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
  await expect(diagram.locator('svg')).toBeVisible();
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
