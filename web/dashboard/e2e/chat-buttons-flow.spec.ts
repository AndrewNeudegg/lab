import AxeBuilder from '@axe-core/playwright';
import { expect, test } from '@playwright/test';
import type { Page } from '@playwright/test';

const fixedNow = '2026-05-01T08:20:00.000Z';

const freezeTime = async (page: Page) => {
  await page.addInitScript((now) => {
    const RealDate = Date;
    class FixedDate extends RealDate {
      constructor(...args: ConstructorParameters<typeof Date>) {
        if (args.length === 0) {
          super(now);
          return;
        }
        super(...args);
      }
      static now() {
        return new RealDate(now).getTime();
      }
    }
    globalThis.Date = FixedDate as DateConstructor;
  }, fixedNow);
};

const mockApis = async (page: Page) => {
  await freezeTime(page);
  let releaseYes = () => {};
  const yesGate = new Promise<void>((resolve) => {
    releaseYes = resolve;
  });
  await page.route(/\/api\/tasks\/attention\/?(?:\?.*)?$/, async (route) => {
    await route.fulfill({ json: { attention: { red: 0, amber: 0, total: 0 } } });
  });
  await page.route(/\/api\/tasks(?:\?.*)?$/, async (route) => {
    await route.fulfill({ json: { tasks: [] } });
  });
  await page.route(/\/api\/approvals$/, async (route) => {
    await route.fulfill({ json: { approvals: [] } });
  });
  await page.route(/\/api\/message$/, async (route) => {
    const body = route.request().postDataJSON() as { content?: string };
    if (body.content === 'Yes') {
      await yesGate;
      await route.fulfill({
        json: {
          reply: 'You chose Yes.',
          source: 'program',
          stats: { model_turns: 1, elapsed_ms: 610 }
        }
      });
      return;
    }
    await route.fulfill({
      json: {
        reply: 'Choose the next step.',
        source: 'program',
        buttons: ['Yes', 'No', 'Ask me later after reviewing the task queue'],
        stats: { model_turns: 1, elapsed_ms: 540 }
      }
    });
  });
  return {
    releaseYes
  };
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
  test.describe(`chat reply buttons on ${viewport.name}`, () => {
    test.use({
      viewport: { width: viewport.width, height: viewport.height },
      isMobile: viewport.mobile,
      hasTouch: viewport.mobile
    });

    test('sends clicked assistant button text as the next chat message', async ({ page }) => {
      const api = await mockApis(page);
      await page.goto('/chat');

      await page.getByRole('textbox', { name: 'Message' }).fill('Need a decision');
      await page.getByRole('button', { name: 'Send' }).click();
      await expect(page.getByText('Choose the next step.')).toBeVisible();

      const yesButton = page.getByRole('button', { name: 'Yes' });
      await expect(yesButton).toBeVisible();
      await expect(page.getByRole('button', { name: 'No' })).toBeVisible();
      await expect(
        page.getByRole('button', { name: 'Ask me later after reviewing the task queue' })
      ).toBeVisible();

      await yesButton.focus();
      await expect(yesButton).toBeFocused();
      await page.keyboard.press('Enter');
      await expect(yesButton).toBeDisabled();

      api.releaseYes();
      await expect(page.locator('.message.user').filter({ hasText: 'Yes' })).toBeVisible();
      await expect(page.getByText('You chose Yes.')).toBeVisible();
      await expectNoAxeViolations(page);

      const metrics = await page.evaluate(() => {
        const buttons = Array.from(document.querySelectorAll('.message-actions button'));
        const messageActions = document.querySelector('.message-actions');
        return {
          bodyWidth: document.body.scrollWidth,
          viewport: window.innerWidth,
          buttonCount: buttons.length,
          minButtonHeight: Math.min(
            ...buttons.map((button) => button.getBoundingClientRect().height)
          ),
          actionsRight: messageActions?.getBoundingClientRect().right ?? 0
        };
      });
      expect(metrics.bodyWidth, JSON.stringify(metrics)).toBeLessThanOrEqual(metrics.viewport + 2);
      expect(metrics.buttonCount, JSON.stringify(metrics)).toBeGreaterThanOrEqual(3);
      expect(metrics.minButtonHeight, JSON.stringify(metrics)).toBeGreaterThanOrEqual(28);
      expect(metrics.actionsRight, JSON.stringify(metrics)).toBeLessThanOrEqual(metrics.viewport);

      await expect(page).toHaveScreenshot(`chat-buttons-flow-${viewport.name}.png`, {
        fullPage: true,
        animations: 'disabled'
      });
    });
  });
}
