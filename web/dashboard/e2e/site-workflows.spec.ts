import { expect, test } from '@playwright/test';
import type { Page } from '@playwright/test';

test.describe.configure({ timeout: 90_000 });

const now = '2026-04-28T12:00:00.000Z';
const taskID = 'task_20260428_120000_11111111';
const workflowID = 'workflow_20260428_120000_22222222';
const chatTranscriptStorageKey = 'homelabd.dashboard.chatTranscript.v4';
const longSuggestedTaskGoal = [
  'Improve task suggestion handling',
  'preserve repository scan context, reviewed plan details, validation expectations, and operator constraints',
  'keep the final risk note because it explains why truncation can drop vital task input',
  'include the complete model-generated investigation trail and handoff notes'
].join(' '.repeat(12));
const longSuggestedTaskCommand = `new ${longSuggestedTaskGoal}`;
const longSuggestedTaskAction = longSuggestedTaskCommand.replace(/\s+/g, ' ');
const chatScrollTranscript = Array.from({ length: 24 }, (_, index) => ({
  id: `scroll-regression-${index}`,
  role: index % 2 === 0 ? 'assistant' : 'user',
  content: [
    `Scroll regression message ${index + 1}`,
    '',
    'The transcript is intentionally tall so browser UAT can scroll the chat pane while keeping the dashboard navigation reachable.'
  ].join('\n'),
  source: 'program',
  time: '12:00'
}));

const task = {
  id: taskID,
  title: 'Review queue behaviour on mobile',
  goal: 'Keep the task queue usable on desktop and mobile.',
  status: 'awaiting_approval',
  assigned_to: 'codex',
  priority: 5,
  created_at: now,
  updated_at: now,
  merge_queue_position: 2,
  merge_queue_entered_at: now,
  result: 'ReviewerAgent checks passed.',
  plan: {
    status: 'reviewed',
    summary: 'Validate task queue, detail, and action state.',
    steps: [{ title: 'Inspect queue', detail: 'Check selected task behaviour.' }],
    risks: ['Long content can overflow narrow screens.'],
    review: 'Plan is ready.',
    created_at: now,
    reviewed_at: now
  }
};

const restartTask = {
  ...task,
  id: 'task_20260428_120500_33333333',
  title: 'Queue restart gate failed',
  goal: 'Keep the task queue restart gate visible on desktop and mobile.',
  status: 'awaiting_restart',
  updated_at: '2026-04-28T12:05:00.000Z',
  merge_queue_position: 1,
  merge_queue_entered_at: now,
  result: 'merged after approval approval_1; post-merge restart pending',
  restart_required: ['dashboard'],
  restart_completed: ['homelabd'],
  restart_status: 'failed',
  restart_current: 'dashboard',
  restart_last_error: 'dashboard health check failed after restart'
};

const queuedTask = {
  ...task,
  id: 'task_20260428_120700_44444444',
  title: 'Queued docs follow-up',
  goal: 'Keep merge queue reordering visible without moving the restart gate.',
  status: 'ready_for_review',
  updated_at: '2026-04-28T12:01:00.000Z',
  merge_queue_position: 3,
  result: 'external agent finished; ready for review.'
};

const workflow = {
  id: workflowID,
  name: 'Deploy homelab dashboard',
  goal: 'Validate a dashboard release candidate.',
  status: 'running',
  steps: [
    { id: 'step_1', name: 'Run checks', kind: 'tool', tool: 'bun.check' },
    { id: 'step_2', name: 'Run browser UAT', kind: 'tool', tool: 'bun.uat.site' }
  ],
  estimate: {
    steps: 2,
    estimated_llm_calls: 0,
    estimated_tool_calls: 2,
    workflow_calls: 0,
    waits: 0,
    estimated_seconds: 180,
    estimated_minutes: 3,
    summary: 'Two automated checks.'
  },
  created_at: now,
  updated_at: now
};

const knowledgeSource = {
  id: 'ksrc_20260428_120000_33333333',
  title: 'Source transparency notes',
  kind: 'text',
  content: 'Source-grounded reports should keep evidence visible beside generated claims.',
  summary: 'Source-grounded reports should keep evidence visible beside generated claims.',
  key_terms: ['source', 'evidence', 'reports'],
  questions: ['What does this source show about evidence?'],
  word_count: 8,
  created_at: now,
  updated_at: now
};

const knowledgeReport = {
  id: 'kreport_20260428_120000_44444444',
  question: 'How should evidence be reviewed?',
  mode: 'research',
  answer: 'Answering "How should evidence be reviewed?" from 1 stored source:\n- [S1] Keep evidence visible beside generated claims.',
  key_findings: ['[S1] Keep evidence visible beside generated claims.'],
  evidence: [{
    id: 'evidence_01',
    source_id: knowledgeSource.id,
    source_title: knowledgeSource.title,
    citation_label: 'S1',
    excerpt: 'Source-grounded reports should keep evidence visible beside generated claims.',
    terms: ['evidence'],
    score: 3
  }],
  gaps: ['Only stored Knowledge Space sources were used for this report.'],
  created_at: now
};

const knowledgeSpace = {
  id: 'kspace_20260428_120000_55555555',
  title: 'Research synthesis',
  objective: 'Keep source-grounded research easy to review.',
  sources: [knowledgeSource],
  reports: [knowledgeReport],
  insight: {
    source_count: 1,
    word_count: 8,
    key_terms: ['source', 'evidence', 'reports'],
    suggested_questions: ['What does this space show about source?'],
    updated_at: now
  },
  created_at: now,
  updated_at: now
};

const healthSnapshot = {
  status: 'healthy',
  started_at: now,
  uptime_seconds: 7200,
  window_seconds: 300,
  current: {
    time: now,
    good: true,
    cpu_usage_percent: 17,
    memory_usage_percent: 43,
    memory_used_bytes: 8_200_000_000,
    memory_total_bytes: 19_000_000_000,
    load1: 0.42,
    load5: 0.36,
    load15: 0.31,
    system_uptime_seconds: 238000,
    process_uptime_seconds: 7200,
    goroutines: 41
  },
  samples: [],
  checks: [{ name: 'homelabd', type: 'http', status: 'healthy', message: 'ready', latency_ms: 12, last_checked: now }],
  processes: [{ name: 'dashboard', type: 'service', status: 'healthy', message: 'serving', pid: 4321, addr: '127.0.0.1:5173', started_at: now, last_seen: now, ttl_seconds: 30 }],
  slos: [{ name: 'dashboard availability', target_percent: 99.5, window_seconds: 300, good_events: 60, total_events: 60, sli_percent: 100, error_budget_remaining_percent: 100, burn_rate: 0, status: 'healthy' }],
  notifications: []
};

const supervisorSnapshot = {
  status: 'healthy',
  started_at: now,
  restartable: true,
  restart_hint: 'go run ./cmd/supervisord',
  apps: [{
    name: 'dashboard',
    type: 'web',
    state: 'running',
    desired: 'running',
    pid: 1203,
    restarts: 1,
    message: 'serving dashboard',
    started_at: now,
    updated_at: now,
    start_order: 30,
    restart: 'always',
    command: 'bun',
    args: ['run', 'dev']
  }]
};

const installTerminalMocks = async (page: Page) => {
  await page.addInitScript(() => {
    class MockEventSource extends EventTarget {
      static CONNECTING = 0;
      static OPEN = 1;
      static CLOSED = 2;
      readyState = MockEventSource.CONNECTING;
      onopen: ((event: Event) => void) | null = null;
      onerror: ((event: Event) => void) | null = null;
      url: string;
      constructor(url: string) {
        super();
        this.url = url;
        setTimeout(() => {
          this.readyState = MockEventSource.OPEN;
          this.onopen?.(new Event('open'));
          this.dispatchEvent(new MessageEvent('output', {
            data: JSON.stringify({ type: 'output', seq: 1, data: 'ready\\r\\n' })
          }));
        }, 20);
      }
      close() {
        this.readyState = MockEventSource.CLOSED;
      }
    }
    window.EventSource = MockEventSource as typeof EventSource;
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

const mockDashboardApis = async (page: Page) => {
  await installTerminalMocks(page);
  let autoMergeEnabled = false;
  let knowledgeSpaces = [knowledgeSpace];
  await page.route(/\/api\/message$/, async (route) => {
    const body = route.request().postDataJSON() as { content?: string };
    if (body.content === longSuggestedTaskAction) {
      await route.fulfill({
        json: {
          reply: 'Created task from full suggested brief.',
          source: 'program',
          stats: { model_turns: 1, tool_calls: 1, total_tokens: 64, elapsed_ms: 730 }
        }
      });
      return;
    }
    await route.fulfill({
      json: {
        reply: [
          'Status: `tasks` and `workflow list` are available.',
          `Action: \`${longSuggestedTaskCommand}\``,
          '',
          '```mermaid',
          'flowchart LR',
          '  Chat[Chat] --> Tasks[Tasks]',
          '  Tasks --> Review[Review]',
          '```'
        ].join('\n'),
        source: 'program',
        stats: { model_turns: 1, tool_calls: 2, total_tokens: 128, elapsed_ms: 1234 }
      }
    });
  });
  await page.route(/\/api\/tasks\/?(?:\?.*)?$/, async (route) => {
    await route.fulfill({ json: { tasks: [queuedTask, restartTask, task] } });
  });
  await page.route(/\/api\/settings\/?(?:\?.*)?$/, async (route) => {
    if (route.request().method() === 'POST') {
      const body = route.request().postDataJSON() as { auto_merge_enabled?: boolean };
      autoMergeEnabled = Boolean(body.auto_merge_enabled);
    }
    await route.fulfill({ json: { settings: { auto_merge_enabled: autoMergeEnabled } } });
  });
  await page.route(/\/api\/tasks\/[^/]+\/runs\/?(?:\?.*)?$/, async (route) => {
    await route.fulfill({ json: { runs: [] } });
  });
  await page.route(/\/api\/tasks\/[^/]+\/diff\/?(?:\?.*)?$/, async (route) => {
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
  await page.route(/\/api\/tasks\/[^/]+\/(?:run|review|merge-queue|accept|restart|reopen|cancel|retry|delete)\/?(?:\?.*)?$/, async (route) => {
    await route.fulfill({ json: { reply: 'task action accepted' } });
  });
  await page.route(/\/api\/approvals(?:\/[^/]+\/(?:approve|deny))?\/?(?:\?.*)?$/, async (route) => {
    await route.fulfill({ json: route.request().url().includes('/approve') ? { reply: 'approved' } : { approvals: [] } });
  });
  await page.route(/\/api\/events(?:\?.*)?$/, async (route) => {
    await route.fulfill({ json: { events: [] } });
  });
  await page.route(/\/api\/agents\/?(?:\?.*)?$/, async (route) => {
    await route.fulfill({ json: { agents: [] } });
  });
  await page.route(/\/api\/workflows$/, async (route) => {
    if (route.request().method() === 'POST') {
      await route.fulfill({ status: 201, json: { workflow, reply: 'created workflow' } });
      return;
    }
    await route.fulfill({ json: { workflows: [workflow] } });
  });
  await page.route(/\/api\/workflows\/[^/]+\/run$/, async (route) => {
    await route.fulfill({ json: { workflow: { ...workflow, status: 'completed' }, reply: 'workflow started' } });
  });
  await page.route(/\/api\/knowledge\/spaces$/, async (route) => {
    if (route.request().method() === 'POST') {
      const body = route.request().postDataJSON() as { title?: string; objective?: string; description?: string };
      const created = {
        ...knowledgeSpace,
        id: 'kspace_created',
        title: body.title || 'New space',
        objective: body.objective,
        description: body.description,
        sources: [],
        reports: [],
        insight: { source_count: 0, word_count: 0, key_terms: [], suggested_questions: [], updated_at: now },
        created_at: now,
        updated_at: now
      };
      knowledgeSpaces = [created, ...knowledgeSpaces];
      await route.fulfill({ status: 201, json: { space: created, reply: 'Knowledge Space created' } });
      return;
    }
    await route.fulfill({ json: { spaces: knowledgeSpaces } });
  });
  await page.route(/\/api\/knowledge\/spaces\/[^/]+$/, async (route) => {
    await route.fulfill({ json: knowledgeSpaces[0] });
  });
  await page.route(/\/api\/knowledge\/spaces\/[^/]+\/sources$/, async (route) => {
    const body = route.request().postDataJSON() as { title?: string; kind?: string; content?: string; uri?: string };
    const source = {
      ...knowledgeSource,
      id: 'ksrc_created',
      title: body.title || 'Added source',
      kind: body.kind || 'text',
      uri: body.uri,
      content: body.content || '',
      summary: body.content || 'Added source summary.',
      word_count: (body.content || '').split(/\s+/).filter(Boolean).length,
      created_at: now,
      updated_at: now
    };
    const current = knowledgeSpaces[0];
    const updated = {
      ...current,
      sources: [source, ...(current.sources || [])],
      insight: {
        ...current.insight,
        source_count: (current.sources || []).length + 1,
        word_count: (current.insight?.word_count || 0) + source.word_count,
        key_terms: ['source', 'evidence', 'review'],
        updated_at: now
      },
      updated_at: now
    };
    knowledgeSpaces = [updated, ...knowledgeSpaces.slice(1)];
    await route.fulfill({ status: 201, json: { space: updated, source, reply: 'Source processed' } });
  });
  await page.route(/\/api\/knowledge\/spaces\/[^/]+\/research$/, async (route) => {
    const body = route.request().postDataJSON() as { question?: string; mode?: string };
    const report = {
      ...knowledgeReport,
      id: 'kreport_created',
      question: body.question || knowledgeReport.question,
      mode: body.mode || 'research',
      created_at: now
    };
    const current = knowledgeSpaces[0];
    const updated = {
      ...current,
      reports: [report, ...(current.reports || [])],
      updated_at: now
    };
    knowledgeSpaces = [updated, ...knowledgeSpaces.slice(1)];
    await route.fulfill({ json: { space: updated, report, reply: 'Research report created' } });
  });
  await page.route(/\/api\/terminal\/sessions$/, async (route) => {
    await route.fulfill({ status: 201, json: { id: 'term_site', shell: '/bin/sh', cwd: '/workspace', created_at: now, persistent: true } });
  });
  await page.route(/\/api\/terminal\/sessions\/[^/]+(?:\/(?:resize|input|events))?$/, async (route) => {
    await route.fulfill({ json: { id: 'term_site', shell: '/bin/sh', cwd: '/workspace', created_at: now, persistent: true } });
  });
  await page.route(/\/healthd-api\/healthd(?:\/checks\/run)?(?:\?.*)?$/, async (route) => {
    await route.fulfill({ json: healthSnapshot });
  });
  await page.route(/\/supervisord-api\/supervisord(?:\/restart)?$/, async (route) => {
    await route.fulfill({ json: route.request().url().endsWith('/restart') ? { reply: 'restart scheduled' } : supervisorSnapshot });
  });
  await page.route(/\/supervisord-api\/supervisord\/apps\/[^/]+\/(?:start|stop|restart)$/, async (route) => {
    await route.fulfill({ json: supervisorSnapshot });
  });
};

const expectNoVisualArtifacts = async (page: Page) => {
  const metrics = await page.evaluate(() => {
    const hasScrollableXAncestor = (element: Element) => {
      let parent = element.parentElement;
      while (parent && parent !== document.body) {
        const style = getComputedStyle(parent);
        const scrollable = ['auto', 'scroll'].includes(style.overflowX);
        if (scrollable && parent.scrollWidth > parent.clientWidth + 2) {
          return true;
        }
        parent = parent.parentElement;
      }
      return false;
    };
    const isHidden = (element: Element) => {
      let current: Element | null = element;
      while (current && current !== document.body) {
        const style = getComputedStyle(current);
        if (style.display === 'none' || style.visibility === 'hidden') {
          return true;
        }
        current = current.parentElement;
      }
      return false;
    };
    const contentRoots = Array.from(
      document.querySelectorAll('main, .task-pane, .chat-card, .docs-shell, .workflow-page, .knowledge-page, .terminal-panel, .app-shell')
    )
      .map((element) => {
        const rect = element.getBoundingClientRect();
        const style = getComputedStyle(element);
        return style.display === 'none' || style.visibility === 'hidden' ? 0 : Math.round(rect.height);
      })
      .filter((height) => height > 0);
    const escaped = Array.from(document.querySelectorAll('h1,h2,h3,p,a,button,summary,label,span,strong'))
      .filter((element) => {
        if (element.closest('.xterm') || hasScrollableXAncestor(element) || isHidden(element)) {
          return false;
        }
        const rect = element.getBoundingClientRect();
        return rect.width > 0 && rect.height > 0 && (rect.left < -2 || rect.right > window.innerWidth + 2);
      })
      .map((element) => (element.textContent || '').trim().replace(/\s+/g, ' ').slice(0, 80));
    const clippedButtons = Array.from(document.querySelectorAll('button,summary'))
      .filter((element) => {
        const rect = element.getBoundingClientRect();
        return rect.width > 0 && rect.height > 0 && (element.scrollWidth > element.clientWidth + 2 || element.scrollHeight > element.clientHeight + 2);
      })
      .map((element) => (element.textContent || element.getAttribute('aria-label') || '').trim().replace(/\s+/g, ' ').slice(0, 80));
    const navbar = document.querySelector('.navbar');
    const navbarRect = navbar?.getBoundingClientRect();
    const navbarBottom = navbarRect && navbarRect.height > 0 ? navbarRect.bottom : null;
    const navbarOverlaps =
      navbarBottom === null || window.scrollY > 2
        ? []
        : Array.from(document.querySelectorAll('main, .shell, .docs-shell, .workflow-page, .knowledge-page, .terminal-panel'))
            .filter((element) => {
              if (isHidden(element)) {
                return false;
              }
              const rect = element.getBoundingClientRect();
              return rect.width > 0 && rect.height > 0 && rect.top < navbarBottom - 1;
            })
            .map((element) => ({
              label: `${element.tagName.toLowerCase()}${element.className ? `.${String(element.className).replace(/\s+/g, '.')}` : ''}`,
              top: Math.round(element.getBoundingClientRect().top),
              navbarBottom: Math.round(navbarBottom)
            }));
    return {
      bodyWidth: document.body.scrollWidth,
      docWidth: document.documentElement.scrollWidth,
      viewport: window.innerWidth,
      contentHeight: Math.max(...contentRoots, 0),
      escaped,
      clippedButtons,
      navbarOverlaps
    };
  });
  expect(metrics.bodyWidth, JSON.stringify(metrics)).toBeLessThanOrEqual(metrics.viewport + 2);
  expect(metrics.docWidth, JSON.stringify(metrics)).toBeLessThanOrEqual(metrics.viewport + 2);
  expect(metrics.contentHeight, JSON.stringify(metrics)).toBeGreaterThan(40);
  expect(metrics.escaped, JSON.stringify(metrics)).toEqual([]);
  expect(metrics.clippedButtons, JSON.stringify(metrics)).toEqual([]);
  expect(metrics.navbarOverlaps, JSON.stringify(metrics)).toEqual([]);
};

const openMobileMenu = async (page: Page) => {
  const menu = page.getByRole('button', { name: 'Menu' });
  const nav = page.getByRole('navigation', { name: 'Primary mobile' });
  await menu.click();
  try {
    await expect(nav).toBeVisible({ timeout: 3_000 });
  } catch {
    await menu.click();
    await expect(nav).toBeVisible();
  }
  return nav;
};

const seedChatScrollTranscript = async (page: Page) => {
  await page.addInitScript(
    ({ key, messages }) => {
      localStorage.setItem(key, JSON.stringify(messages));
      localStorage.removeItem('homelabd.dashboard.chatDraft.v1');
    },
    { key: chatTranscriptStorageKey, messages: chatScrollTranscript }
  );
};

const expectChatNavbarPinned = async (page: Page) => {
  const metrics = await page.evaluate(async () => {
    const navbar = document.querySelector('.navbar');
    const messages = document.querySelector('.messages');
    const composer = document.querySelector('.composer');
    if (messages) {
      messages.scrollTop = messages.scrollHeight;
    }
    window.scrollTo(0, document.body.scrollHeight);
    await new Promise<void>((resolve) => {
      requestAnimationFrame(() => requestAnimationFrame(() => resolve()));
    });
    const navbarRect = navbar?.getBoundingClientRect();
    const messagesRect = messages?.getBoundingClientRect();
    const composerRect = composer?.getBoundingClientRect();
    return {
      navbarTop: navbarRect?.top ?? null,
      navbarBottom: navbarRect?.bottom ?? null,
      navbarPosition: navbar ? getComputedStyle(navbar).position : null,
      messagesTop: messagesRect?.top ?? null,
      messagesScrollTop: messages?.scrollTop ?? 0,
      messagesScrollHeight: messages?.scrollHeight ?? 0,
      messagesClientHeight: messages?.clientHeight ?? 0,
      composerBottom: composerRect?.bottom ?? null,
      windowScrollY: window.scrollY,
      viewportHeight: window.innerHeight
    };
  });
  expect(metrics.navbarPosition, JSON.stringify(metrics)).toBe('fixed');
  expect(
    Math.abs(metrics.navbarTop ?? Number.POSITIVE_INFINITY),
    JSON.stringify(metrics)
  ).toBeLessThanOrEqual(1);
  expect(metrics.messagesScrollHeight, JSON.stringify(metrics)).toBeGreaterThan(
    metrics.messagesClientHeight
  );
  expect(metrics.messagesScrollTop, JSON.stringify(metrics)).toBeGreaterThan(0);
  expect(metrics.windowScrollY, JSON.stringify(metrics)).toBeLessThanOrEqual(8);
  expect(metrics.messagesTop ?? 0, JSON.stringify(metrics)).toBeGreaterThanOrEqual(
    (metrics.navbarBottom ?? 0) - 1
  );
  expect(metrics.composerBottom ?? 0, JSON.stringify(metrics)).toBeLessThanOrEqual(
    metrics.viewportHeight + 1
  );
};

const expectTaskNavAttention = async (page: Page, mobile: boolean) => {
  const taskLinkName = 'Tasks, 3 review items need attention';
  if (mobile) {
    await expect(page.locator('.mobile-nav .attention-badge.warning')).toHaveText('3');
    const mobileNav = await openMobileMenu(page);
    await expect(mobileNav.getByRole('link', { name: taskLinkName })).toBeVisible();
    await expect(page.locator('.mobile-nav .attention-badge.warning')).toHaveText('3');
    await expect(page.locator('.mobile-menu-scrim')).toBeVisible();
    await page.mouse.click(20, (page.viewportSize()?.height ?? 844) - 24);
    await expect(mobileNav).toBeHidden();
    return;
  }
  await expect(
    page.getByRole('navigation', { name: 'Primary' }).getByRole('link', { name: taskLinkName })
  ).toBeVisible();
  await expect(page.locator('.desktop-nav .attention-badge.warning')).toHaveText('3');
};

const exerciseRoute = async (page: Page, route: string, mobile: boolean) => {
  await expectTaskNavAttention(page, mobile);
  if (route === '/' || route === '/chat') {
    await page.getByRole('textbox', { name: 'Message' }).fill('status');
    await page.getByRole('button', { name: 'Send' }).click();
    await expect(page.getByText('Status:')).toBeVisible();
    const longSuggestion = page.getByRole('button', { name: longSuggestedTaskAction });
    await expect(longSuggestion).toBeVisible();
    await longSuggestion.click();
    await expect(page.getByText('Created task from full suggested brief.')).toBeVisible();
    await expect(page.getByText('1 model turn · 2 tool calls · 128 tokens · 1.2 s elapsed')).toBeVisible();
    await expect(page.locator('.message .mermaid-diagram svg').last()).toBeVisible();
    if (route === '/chat') {
      await expectChatNavbarPinned(page);
    }
  } else if (route === '/tasks') {
    await page.waitForLoadState('networkidle');
    await page.getByPlaceholder('Search tasks').fill('queue');
    const mergeQueue = page.locator('[aria-label="Merge queue"]');
    await expect(mergeQueue).toBeVisible();
    await expect(mergeQueue).toContainText('Merge queue');
    const queueMove = mergeQueue.getByRole('button', {
      name: /Move Queued docs follow-up up in merge queue/
    });
    await expect(queueMove).toBeVisible();
    const autoMerge = mergeQueue.getByRole('switch', {
      name: 'Auto merge reviewed queue-head tasks'
    });
    await expect(autoMerge).toHaveAttribute('aria-checked', 'false');
    await autoMerge.click();
    await expect(autoMerge).toHaveAttribute('aria-checked', 'true');
    await queueMove.click();
    const queueNotice = mobile ? page.locator('.task-pane .queue-notice') : page.locator('.workbench .notice');
    await expect(queueNotice.getByText('Merge queue updated')).toBeVisible();
    await page.locator('.task-row').first().click();
    const taskActions = page.getByRole('region', { name: 'Task actions' });
    await expect(taskActions).toBeVisible();
    await expect(page.getByText('Post-merge restart', { exact: true })).toBeVisible();
    await taskActions.getByRole('button', { name: 'Restart', exact: true }).click();
    await expect(page.locator('.workbench .notice').getByText('task action accepted')).toBeVisible();
    if (mobile) {
      await page.getByRole('button', { name: 'Back to queue' }).click();
      await expect(page.locator('.task-pane')).toBeVisible();
    }
  } else if (route === '/workflows') {
    await page.getByPlaceholder('Search workflows').fill('Deploy');
    await page.getByRole('link', { name: /Deploy homelab dashboard/ }).click();
    await page
      .locator('[aria-label="Workflow actions"]')
      .getByRole('button', { name: 'Run', exact: true })
      .click();
    await expect(page.getByText('workflow started')).toBeVisible();
  } else if (route === '/knowledge') {
    await page.getByPlaceholder('Search Knowledge Space').fill('Research');
    await page.getByRole('link', { name: /Research synthesis/ }).click();
    await page.getByRole('tab', { name: 'Sources' }).click();
    await page.getByLabel('Source title').fill('Review notes');
    await page.getByLabel('Source text').fill('Evidence should stay visible when teams review generated claims.');
    await page.getByRole('button', { name: 'Add source' }).click();
    await expect(page.getByText('Source processed')).toBeVisible();
    await page.getByRole('tab', { name: 'Research' }).click();
    await page.getByLabel('Question').fill('How should evidence be reviewed?');
    await page.getByLabel('Mode', { exact: true }).selectOption('brief');
    await page.getByRole('button', { name: 'Run' }).click();
    await expect(page.getByText('Research report created')).toBeVisible();
    await expect(page.locator('[aria-label="Report evidence"]')).toContainText('[S1]');
  } else if (route.startsWith('/docs')) {
    if (mobile) {
      const docsNavigationToggle = page.getByRole('button', { name: 'Expand docs navigation' });
      await expect(docsNavigationToggle).toBeVisible();
      await docsNavigationToggle.click();
    }
    await page.getByRole('searchbox', { name: 'Search documentation' }).fill('remote');
    await expect(page.locator('#docs-list')).toBeVisible();
    await expect(
      page.locator(
        '.mermaid-diagram[data-mermaid-status="pending"], .mermaid-diagram[data-mermaid-status="rendering"]'
      )
    ).toHaveCount(0, { timeout: 15_000 });
  } else if (route === '/terminal') {
    await expect(page.locator('.xterm')).toBeVisible({ timeout: 20_000 });
    await page.getByRole('button', { name: 'Add terminal tab' }).click();
    await expect(page.locator('.terminal-tab')).toHaveCount(2);
  } else if (route === '/healthd') {
    await page.getByRole('button', { name: 'Run checks' }).click();
    await expect(page.getByRole('heading', { name: 'CPU usage' })).toBeVisible();
  } else if (route === '/supervisord') {
    await page.getByRole('button', { name: 'Refresh' }).click();
    await expect(page.getByRole('region', { name: 'Supervised applications' })).toBeVisible();
  }
};

const routes = [
  '/',
  '/chat',
  '/tasks',
  '/knowledge',
  '/workflows',
  '/terminal',
  '/docs',
  '/docs/dashboard',
  '/docs/agent-tools',
  '/healthd',
  '/supervisord'
];
const docsRouteTimeoutMs = 120_000;

test.describe('knowledge empty state', () => {
  test.use({ viewport: { width: 480, height: 897 }, isMobile: true, hasTouch: true });

  test('handles a null spaces response and submits page-context help', async ({ page }) => {
    await mockScreenCaptureUnavailable(page);
    const pageErrors: string[] = [];
    let helpTaskBody: any;
    page.on('pageerror', (err) => pageErrors.push(err.message));

    await page.route(/\/api\/knowledge\/spaces$/, async (route) => {
      await route.fulfill({ json: { spaces: null } });
    });
    await page.route(/\/api\/tasks(?:\?.*)?$/, async (route) => {
      if (route.request().method() === 'POST') {
        helpTaskBody = route.request().postDataJSON();
        await route.fulfill({ status: 201, json: { reply: 'created help task' } });
        return;
      }
      await route.fulfill({ json: { tasks: [] } });
    });
    await page.route(/\/api\/approvals(?:\?.*)?$/, async (route) => {
      await route.fulfill({ json: { approvals: [] } });
    });

    await page.goto('/knowledge');
    await expect(page.getByText('No Knowledge Space matches this view.')).toBeVisible();
    await expect(page.getByText('No Knowledge Space selected')).toBeVisible();
    await expect(page.getByText(/response\.spaces is null|Symbol\.iterator/)).toHaveCount(0);
    expect(pageErrors).toEqual([]);

    await page.locator('.help-button').click();
    const dialog = page.locator('dialog.help-dialog');
    await expect(dialog).toBeVisible();
    await dialog.getByRole('textbox', { name: 'More detail' }).fill('Knowledge page showed a raw null spaces error.');
    await dialog.getByRole('button', { name: 'Submit help task' }).click();
    await expect(dialog).not.toBeVisible();

    expect(helpTaskBody.goal).toContain('Dashboard help task from /knowledge');
    expect(helpTaskBody.goal).toContain('Knowledge page showed a raw null spaces error.');
    expect(helpTaskBody.attachments).toHaveLength(1);
    const browserContext = JSON.parse(helpTaskBody.attachments[0].text);
    expect(browserContext.visible_text).toContain('No Knowledge Space selected');
    expect(browserContext.visible_text).not.toMatch(/response\.spaces is null|Symbol\.iterator/);
  });
});

for (const viewport of [
  { name: 'desktop', width: 1440, height: 1000, mobile: false },
  { name: 'mobile', width: 390, height: 844, mobile: true }
]) {
  test.describe(`site workflows on ${viewport.name}`, () => {
    test.use({ viewport: { width: viewport.width, height: viewport.height }, isMobile: viewport.mobile, hasTouch: viewport.mobile });

    for (const route of routes) {
      test(`${route} renders without visual artefacts and supports its core workflow`, async ({ page }, testInfo) => {
        if (route.startsWith('/docs')) {
          testInfo.setTimeout(docsRouteTimeoutMs);
        }
        await mockDashboardApis(page);
        const taskSettingsReady =
          route === '/tasks'
            ? page.waitForResponse(
                (response) =>
                  response.url().endsWith('/api/settings') &&
                  response.request().method() === 'GET'
              )
            : Promise.resolve();
        if (route === '/chat') {
          await seedChatScrollTranscript(page);
        }
        await page.goto(route);
        await taskSettingsReady;
        if (route === '/') {
          await expect(page).toHaveURL(/\/chat$/);
        }
        await exerciseRoute(page, route, viewport.mobile);
        await expectNoVisualArtifacts(page);
        await testInfo.attach(`${viewport.name}-${route.replaceAll('/', '-') || 'root'}.png`, {
          body: await page.screenshot({ fullPage: !route.startsWith('/docs') }),
          contentType: 'image/png'
        });
      });
    }

    if (viewport.mobile) {
      test('mobile SPA navigation to /healthd keeps Health content below the navbar', async ({ page }, testInfo) => {
        await mockDashboardApis(page);
        await page.goto('/chat');

        const chatNav = await openMobileMenu(page);
        await chatNav.getByRole('link', { name: /Tasks/ }).click();
        await expect(page).toHaveURL(/\/tasks$/);
        await expect(page.locator('.task-row')).toHaveCount(3, { timeout: 15_000 });

        const tasksNav = await openMobileMenu(page);
        await tasksNav.getByRole('link', { name: 'Health' }).click();
        await expect(page).toHaveURL(/\/healthd$/);
        await expect(page.locator('.toolbar .status')).toHaveText('healthy');

        await page.getByRole('button', { name: 'Run checks' }).click();
        await expect(page.getByRole('heading', { name: 'CPU usage' })).toBeVisible();

        const metrics = await page.evaluate(() => {
          const navbar = document.querySelector('.navbar');
          const toolbar = document.querySelector('.toolbar');
          const status = document.querySelector('.toolbar .status');
          const navbarRect = navbar?.getBoundingClientRect();
          return {
            navbarPosition: navbar ? getComputedStyle(navbar).position : '',
            navbarBottom: navbarRect?.bottom ?? 0,
            toolbarTop: toolbar?.getBoundingClientRect().top ?? 0,
            statusTop: status?.getBoundingClientRect().top ?? 0,
            bodyWidth: document.body.scrollWidth,
            viewport: window.innerWidth
          };
        });
        expect(metrics.navbarPosition, JSON.stringify(metrics)).toBe('sticky');
        expect(metrics.toolbarTop, JSON.stringify(metrics)).toBeGreaterThanOrEqual(metrics.navbarBottom - 1);
        expect(metrics.statusTop, JSON.stringify(metrics)).toBeGreaterThanOrEqual(metrics.navbarBottom - 1);
        expect(metrics.bodyWidth, JSON.stringify(metrics)).toBeLessThanOrEqual(metrics.viewport + 2);
        await testInfo.attach('mobile-chat-tasks-healthd-navbar.png', {
          body: await page.screenshot({ fullPage: true }),
          contentType: 'image/png'
        });
      });
    }
  });
}
