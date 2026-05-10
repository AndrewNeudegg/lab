import AxeBuilder from '@axe-core/playwright';
import { expect, test } from '@playwright/test';
import type { Page } from '@playwright/test';

const now = '2026-04-28T12:00:00.000Z';
const taskID = 'task_20260428_120000_uiux1111';

const task = {
  id: taskID,
  title: 'Review task page UI/UX workflow',
  goal: 'Keep the task page UI/UX workflow explicit and reviewable.',
  status: 'awaiting_approval',
  assigned_to: 'codex',
  priority: 5,
  created_at: now,
  updated_at: now,
  result: 'ReviewerAgent checks passed.',
  plan: {
    status: 'reviewed',
    summary: 'Use the required UI/UX workflow for the task page.',
    steps: [
      {
        title: 'Apply UI/UX brief',
        detail: 'Reuse task page patterns and verify the selected task detail.'
      }
    ],
    risks: ['A missing brief can let visual review become subjective.'],
    review: 'ReviewerAgent checked the UI/UX brief before execution.',
    created_at: now,
    reviewed_at: now,
    ui_ux_brief: {
      operator_goal: 'Review task UI work without extra prompting.',
      primary_workflow: 'Open a task, inspect the reviewed plan, and verify UI evidence.',
      surfaces: ['/tasks'],
      existing_pattern: 'Task list-detail layout and reviewed-plan disclosure.',
      desktop_layout: 'Keep queue and selected task detail visible together.',
      mobile_layout: 'Show one task workflow at a time below the pinned navbar.',
      states: ['loading', 'empty', 'error', 'disabled', 'long content', 'success'],
      accessibility: ['accessible names', 'keyboard focus', 'contrast', 'target size'],
      validation: ['desktop axe', 'mobile axe', 'desktop visual baseline', 'mobile visual baseline']
    }
  }
};

const freezeTime = async (page: Page) => {
  await page.addInitScript((fixedNow) => {
    const RealDate = Date;
    class FixedDate extends RealDate {
      constructor(...args: ConstructorParameters<typeof Date>) {
        if (args.length === 0) {
          super(fixedNow);
          return;
        }
        super(...args);
      }
      static now() {
        return new RealDate(fixedNow).getTime();
      }
    }
    globalThis.Date = FixedDate as DateConstructor;
  }, now);
};

const mockTaskApis = async (page: Page) => {
  await freezeTime(page);
  await page.route(/\/api\/tasks\/attention\/?(?:\?.*)?$/, async (route) => {
    await route.fulfill({ json: { attention: { red: 0, amber: 0, total: 0 } } });
  });
  await page.route(/\/api\/tasks(?:\?.*)?$/, async (route) => {
    await route.fulfill({ json: { tasks: [task] } });
  });
  await page.route(/\/api\/settings$/, async (route) => {
    await route.fulfill({ json: { settings: { auto_merge_enabled: false } } });
  });
  await page.route(/\/api\/approvals$/, async (route) => {
    await route.fulfill({
      json: {
        approvals: [
          {
            id: 'approval_uiux',
            task_id: taskID,
            tool: 'git.merge_approved',
            status: 'pending',
            reason: 'merge reviewed task branch into repo root',
            created_at: now
          }
        ]
      }
    });
  });
  await page.route(/\/api\/events(?:\?.*)?$/, async (route) => {
    await route.fulfill({ json: { events: [] } });
  });
	  await page.route(/\/api\/agents$/, async (route) => {
	    await route.fulfill({ json: { agents: [] } });
	  });
	  await page.route(/\/api\/workspaces$/, async (route) => {
	    await route.fulfill({ json: { workspaces: [] } });
	  });
	  await page.route(/\/api\/tasks\/[^/]+\/runs$/, async (route) => {
    await route.fulfill({ json: { runs: [] } });
  });
  await page.route(/\/api\/tasks\/[^/]+\/diff$/, async (route) => {
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
};

const expectNoAxeViolations = async (page: Page) => {
  const results = await new AxeBuilder({ page }).include('main').analyze();
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
  test.describe(`UI quality gate on ${viewport.name}`, () => {
    test.use({
      viewport: { width: viewport.width, height: viewport.height },
      isMobile: viewport.mobile,
      hasTouch: viewport.mobile
    });

    test('tasks page has reviewed UI/UX evidence, axe coverage, and a visual baseline', async ({
      page
    }) => {
      await mockTaskApis(page);
      await page.goto('/tasks');
      await page.getByRole('link', { name: /Review task page UI\/UX workflow/ }).click();
      await expect(page.getByRole('heading', { name: 'Review task page UI/UX workflow' })).toBeVisible();
      await page.locator('details.task-plan > summary').click();
      const briefHeading = page.getByText('UI/UX brief', { exact: true });
      await expect(briefHeading).toBeVisible();
      await expect(page.getByText('desktop axe')).toBeVisible();
      await expect(page.getByText('mobile axe')).toBeVisible();
      await expect(page.getByText('desktop visual baseline')).toBeVisible();
      await expect(page.getByText('mobile visual baseline')).toBeVisible();
      await briefHeading.scrollIntoViewIfNeeded();

      await expectNoAxeViolations(page);
      await expect(page).toHaveScreenshot(`tasks-ui-quality-${viewport.name}.png`, {
        fullPage: true,
        animations: 'disabled'
      });
    });
  });
}
