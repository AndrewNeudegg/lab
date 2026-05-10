import AxeBuilder from '@axe-core/playwright';
import { expect, test } from '@playwright/test';
import type { Page } from '@playwright/test';

const now = '2026-05-01T08:15:00.000Z';

const seededSessions = [
  {
    id: 'chat_ui_review_current',
    title: 'Tighten chat page controls',
    messages: [
      {
        id: 'user-1',
        role: 'user',
        content: 'The chat page feels too spread out and the controls are hard to scan.',
        time: '08:10'
      },
      {
        id: 'assistant-2',
        role: 'assistant',
        source: 'program',
        content:
          'Status:\n\n- Chat history stays available.\n- Composer actions are compact icon controls.\n- Prompt shortcuts remain secondary.\n\nNext: review the desktop and mobile layout before merging.',
        time: '08:11',
        stats: {
          model_turns: 1,
          tool_calls: 2,
          total_tokens: 128,
          elapsed_ms: 1200
        }
      }
    ],
    created_at: now,
    updated_at: now
  },
  {
    id: 'chat_ui_review_previous',
    title: 'Previous incident follow-up',
    messages: [
      {
        id: 'user-1',
        role: 'user',
        content: 'Summarise the latest task queue.',
        time: '07:40'
      }
    ],
    created_at: '2026-05-01T07:40:00.000Z',
    updated_at: '2026-05-01T07:40:00.000Z'
  }
];

const mockShellApis = async (page: Page) => {
  await page.route(/\/api\/tasks\/attention\/?(?:\?.*)?$/, async (route) => {
    await route.fulfill({ json: { attention: { red: 0, amber: 0, total: 0 } } });
  });
  await page.route(/\/api\/tasks(?:\?.*)?$/, async (route) => {
    await route.fulfill({ json: { tasks: [] } });
  });
  await page.route(/\/api\/approvals$/, async (route) => {
    await route.fulfill({ json: { approvals: [] } });
  });
};

const seedChat = async (page: Page) => {
  await page.goto('/chat');
  await page.evaluate((sessions) => {
    localStorage.setItem('homelabd.dashboard.chatSessions.v1', JSON.stringify(sessions));
    localStorage.setItem('homelabd.dashboard.activeChatSession.v1', sessions[0].id);
    localStorage.removeItem('homelabd.dashboard.chatTranscript.v4');
    localStorage.removeItem('homelabd.dashboard.chatDraft.v1');
  }, seededSessions);
  await page.reload();
  await expect(page.locator('.chat-card')).toHaveAttribute('data-ready', 'true');
};

const expectNoAxeViolations = async (page: Page) => {
  const results = await new AxeBuilder({ page }).include('.chat-card').analyze();
  expect(
    results.violations.map((violation) => ({
      id: violation.id,
      impact: violation.impact,
      help: violation.help,
      targets: violation.nodes.map((node) => node.target)
    }))
  ).toEqual([]);
};

for (const viewport of [
  { name: 'desktop', width: 1440, height: 1000, mobile: false },
  { name: 'mobile', width: 390, height: 844, mobile: true }
]) {
  test.describe(`chat UI quality on ${viewport.name}`, () => {
    test.use({
      viewport: { width: viewport.width, height: viewport.height },
      isMobile: viewport.mobile,
      hasTouch: viewport.mobile
    });

    test('keeps useful chat space dominant and tool controls compact', async ({ page }) => {
      await mockShellApis(page);
      await seedChat(page);

      await expect(page.getByRole('heading', { name: 'Tighten chat page controls' })).toBeVisible();
      await expect(page.getByRole('button', { name: 'New chat' })).toBeVisible();
      if (viewport.mobile) {
        await expect(page.getByRole('button', { name: 'Clear all chats' })).toBeHidden();
      } else {
        await expect(page.getByRole('button', { name: 'Clear all chats' })).toBeVisible();
      }
      await expect(page.getByRole('button', { name: 'Clear current chat' })).toBeVisible();
      await expect(page.getByRole('button', { name: 'Attach' })).toBeVisible();
      await expect(page.getByRole('button', { name: 'Send' })).toBeDisabled();

      const metrics = await page.evaluate(() => {
        const messages = document.querySelector('.messages') as HTMLElement | null;
        const composer = document.querySelector('.composer') as HTMLElement | null;
        const composerRow = document.querySelector('.composer-row') as HTMLElement | null;
        const promptActions = document.querySelector('.prompt-actions') as HTMLElement | null;
        const sessionSidebar = document.querySelector('.session-sidebar') as HTMLElement | null;
        const chatToolbar = document.querySelector('.chat-toolbar') as HTMLElement | null;
        const composerRowStyle = composerRow ? getComputedStyle(composerRow) : null;
        const iconButtons = Array.from(
          document.querySelectorAll(
            '.session-sidebar-actions button, .chat-toolbar-actions button, .attach-button, .composer-buttons button'
          )
        ).filter((button) => {
          const rect = button.getBoundingClientRect();
          return rect.width > 0 && rect.height > 0;
        });
        return {
          bodyWidth: document.body.scrollWidth,
          viewport: window.innerWidth,
          messagesHeight: messages?.getBoundingClientRect().height ?? 0,
          composerHeight: composer?.getBoundingClientRect().height ?? 0,
          composerRowBorderWidth: Number.parseFloat(composerRowStyle?.borderTopWidth || '0'),
          promptHeight: promptActions?.getBoundingClientRect().height ?? 0,
          sessionSidebarHeight: sessionSidebar?.getBoundingClientRect().height ?? 0,
          chatToolbarHeight: chatToolbar?.getBoundingClientRect().height ?? 0,
          iconButtonText: iconButtons.map((button) => button.textContent?.trim() || ''),
          iconButtonMinSize: Math.min(
            ...iconButtons.map((button) => {
              const rect = button.getBoundingClientRect();
              return Math.min(rect.width, rect.height);
            })
          )
        };
      });
      expect(metrics.bodyWidth, JSON.stringify(metrics)).toBeLessThanOrEqual(metrics.viewport + 2);
      expect(metrics.messagesHeight, JSON.stringify(metrics)).toBeGreaterThan(metrics.composerHeight * 3);
      expect(metrics.promptHeight, JSON.stringify(metrics)).toBeLessThan(56);
      expect(metrics.composerRowBorderWidth, JSON.stringify(metrics)).toBeGreaterThanOrEqual(1);
      expect(metrics.iconButtonText, JSON.stringify(metrics)).toEqual(
        viewport.mobile ? ['', '', '', ''] : ['', '', '', '', '']
      );
      expect(metrics.iconButtonMinSize, JSON.stringify(metrics)).toBeGreaterThanOrEqual(32);
      if (viewport.mobile) {
        expect(metrics.sessionSidebarHeight, JSON.stringify(metrics)).toBeLessThanOrEqual(58);
        expect(metrics.chatToolbarHeight, JSON.stringify(metrics)).toBeLessThanOrEqual(48);
        expect(metrics.composerHeight, JSON.stringify(metrics)).toBeLessThanOrEqual(76);
        expect(metrics.promptHeight, JSON.stringify(metrics)).toBeLessThanOrEqual(42);
      }

      await expectNoAxeViolations(page);
      await expect(page).toHaveScreenshot(`chat-ui-quality-${viewport.name}.png`, {
        fullPage: true,
        animations: 'disabled'
      });
    });

    test('keeps session switching separate from composing on mobile', async ({ page }) => {
      test.skip(!viewport.mobile, 'Mobile-only focus behaviour.');
      await mockShellApis(page);
      await seedChat(page);

      const textbox = page.getByRole('textbox', { name: 'Message' });
      await expect(textbox).toBeVisible();
      await expect(textbox).not.toBeFocused();

      await page.getByRole('button', { name: 'Previous incident follow-up' }).click();
      await expect(page.getByRole('heading', { name: 'Previous incident follow-up' })).toBeVisible();
      await expect(textbox).not.toBeFocused();

      await page.getByRole('button', { name: 'Tighten chat page controls' }).click();
      await expect(page.getByRole('heading', { name: 'Tighten chat page controls' })).toBeVisible();
      await expect(textbox).not.toBeFocused();
    });

    test('uses prompt chips for the right interaction job', async ({ page }) => {
      await mockShellApis(page);
      await seedChat(page);

      const textbox = page.getByRole('textbox', { name: 'Message' });
      await page.getByRole('button', { name: 'Start work' }).click();
      await expect(textbox).toHaveValue('create a task to ');
      await expect(textbox).toBeFocused();
      await expect(page.locator('.message.user')).toHaveCount(1);
    });
  });
}
