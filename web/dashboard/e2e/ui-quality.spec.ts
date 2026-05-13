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

const blockerTaskID = 'task_20260428_120500_block111';
const blockerTask = {
  ...task,
  id: blockerTaskID,
  goal_id: 'goal_ui_quality',
  execution_mode: 'autopilot',
  goal_kind: 'build',
  status: 'done',
  title: 'Validate Goal blocker copy',
  goal: 'Make blocked Autopilot tasks explain what is blocking progress.',
  result: 'Goal blocker trace rendered for operator review.',
  goal_blocker_trace: {
    status: 'blocked',
    source_type: 'task_report',
    source_id: 'greport_ui_quality',
    goal_id: 'goal_ui_quality',
    phase_id: 'phase_ui_quality',
    phase_title: 'Review blocking UX',
    blocking_task_id: blockerTaskID,
    review_decision: 'needs_validation',
    reason: 'Task block111 needs validation evidence before Autopilot can continue.',
    operator_action: 'Complete the missing validation or accept that it is not required, then resume Autopilot.',
    source_url: '/assistant?goal=goal_ui_quality',
    blocking_task_url: `/tasks?task=${blockerTaskID}`,
    questions: [
      'Should the supervisor accept the current evidence or require an independent comparison?'
    ],
    blockers: ['Browser validation is required before the Goal can resume.'],
    follow_ups: ['Run the task-page browser UAT and attach the result.'],
    created_at: now
  }
};

const waitingTaskID = 'task_20260428_120700_wait1111';
const waitingGoalTask = {
  ...blockerTask,
  id: waitingTaskID,
  status: 'blocked',
  title: 'Build UI blocked by validation',
  result: 'Waiting for the validation task before Autopilot can continue.',
  goal_blocker_trace: {
    ...blockerTask.goal_blocker_trace,
    blocking_task_id: blockerTaskID,
    blocking_task_url: `/tasks?task=${blockerTaskID}`
  }
};

const agentRepairTaskID = 'task_20260428_120900_repair1';
const agentRepairTask = {
  ...blockerTask,
  id: agentRepairTaskID,
  status: 'blocked',
  title: 'Map enterprise parity scope',
  result: 'Autopilot found an actionable gap-fix task.',
  goal_blocker_trace: {
    ...blockerTask.goal_blocker_trace,
    status: 'needs_agent_repair',
    resolver: 'agent',
    human_action_required: false,
    blocking_task_id: agentRepairTaskID,
    blocking_task_url: `/tasks?task=${agentRepairTaskID}`,
    reason:
      'Autopilot found more repair work: open high gap ggap_scope prevents Goal completion.',
    next_action:
      'Map every current Enterprise feature category to implemented evidence, accepted exclusion, or explicit gap severity.',
    operator_action:
      'No human decision is required. Autopilot should record this as repair work and create the next gap-fix task.',
    questions: [],
    blockers: ['Open high gap ggap_scope prevents credible completion.'],
    follow_ups: ['Rerun the public-doc challenge after mapping the feature categories.']
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

const mockTaskApis = async (page: Page, taskItems = [task]) => {
  await freezeTime(page);
  await page.route(/\/api\/tasks\/attention\/?(?:\?.*)?$/, async (route) => {
    await route.fulfill({ json: { attention: { red: 0, amber: taskItems.length, total: taskItems.length } } });
  });
  await page.route(/\/api\/tasks\/[^/]+$/, async (route) => {
    const taskId = new URL(route.request().url()).pathname.split('/').pop() || '';
    await route.fulfill({ json: taskItems.find((item) => item.id === taskId) || taskItems[0] });
  });
  await page.route(/\/api\/tasks(?:\?.*)?$/, async (route) => {
    await route.fulfill({ json: { tasks: taskItems } });
  });
  await page.route(/\/api\/settings$/, async (route) => {
    await route.fulfill({ json: { settings: { auto_merge_enabled: false } } });
  });
  await page.route(/\/api\/approvals$/, async (route) => {
    const approvalTask = taskItems.find((item) => item.status === 'awaiting_approval');
    await route.fulfill({
      json: {
        approvals: approvalTask
          ? [
              {
                id: 'approval_uiux',
                task_id: approvalTask.id,
                tool: 'git.merge_approved',
                status: 'pending',
                reason: 'merge reviewed task branch into repo root',
                created_at: now
              }
            ]
          : []
      }
    });
  });
  await page.route(/\/api\/assistant\/goals\/[^/]+\/check$/, async (route) => {
    await route.fulfill({ json: { reply: 'Goal check queued.', run: { id: 'arun_ui_quality' } } });
  });
  await page.route(/\/api\/assistant\/goals\/[^/]+\/autopilot\/resume$/, async (route) => {
    await route.fulfill({
      json: {
        reply: 'Autopilot resumed for goal_ui_quality.',
        timeline: { goal: { id: 'goal_ui_quality' }, events: [] }
      }
    });
  });
  await page.route(/\/api\/tasks\/[^/]+\/reopen$/, async (route) => {
    const body = route.request().postDataJSON() as { reason?: string };
    await route.fulfill({
      json: {
        reply: `Reopened with answer: ${body.reason || 'missing reason'}`
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
        task_id: taskItems[0].id,
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

    test('Goal blocker trace shows the blocking reason, action, and links', async ({ page }) => {
      await mockTaskApis(page, [blockerTask]);
      await page.goto(`/tasks?task=${blockerTaskID}`);
      await expect(page.getByRole('heading', { name: 'Validate Goal blocker copy' })).toBeVisible();
      const blockerTrace = page.getByLabel('Goal blocker trace');
      await expect(blockerTrace).toContainText('This task is blocking Goal Autopilot');
      await expect(blockerTrace).toContainText('Decision needed');
      await expect(blockerTrace).toContainText('Decide whether to resume the Goal');
      await expect(blockerTrace).toContainText('Task block111 needs validation evidence');
      await expect(blockerTrace).toContainText('Complete the missing validation');
      await expect(blockerTrace.getByRole('button', { name: /Accept current evidence/ })).toBeVisible();
      await expect(blockerTrace.getByRole('button', { name: /Not acceptable: require more work/ })).toBeVisible();
      await expect(blockerTrace.getByRole('button', { name: /Answer another way/ })).toBeVisible();
      await expect(blockerTrace.getByRole('link', { name: 'Open Goal' })).toHaveAttribute(
        'href',
        '/assistant?goal=goal_ui_quality'
      );
      await expect(blockerTrace.getByRole('button', { name: 'Check Goal now' })).toBeVisible();

      await expectNoAxeViolations(page);
      await blockerTrace.scrollIntoViewIfNeeded();
      await expect(page).toHaveScreenshot(`tasks-goal-blocker-${viewport.name}.png`, {
        fullPage: true,
        animations: 'disabled'
      });
    });

    test('Goal blocker navigation lands on a clear blocker decision', async ({ page }) => {
      await mockTaskApis(page, [waitingGoalTask, blockerTask]);
      await page.goto(`/tasks?task=${waitingTaskID}`);
      let blockerTrace = page.getByLabel('Goal blocker trace');
      await expect(page.getByRole('heading', { name: 'Build UI blocked by validation' })).toBeVisible();
      await expect(blockerTrace).toContainText('Open the blocking task');
      await expect(blockerTrace.getByRole('button', { name: 'Resume Autopilot' })).toHaveCount(0);

      await blockerTrace.getByRole('link', { name: 'Open blocking task' }).click();
      await expect(page).toHaveURL(new RegExp(`task=${blockerTaskID}`));
      blockerTrace = page.getByLabel('Goal blocker trace');
      await expect(page.getByRole('heading', { name: 'Validate Goal blocker copy' })).toBeVisible();
      await expect(blockerTrace).toContainText('Decide whether to resume the Goal');

      await blockerTrace.getByRole('button', { name: /Accept current evidence/ }).click();
      await expect(blockerTrace.getByLabel('Goal blocker answer')).toContainText('Accept current evidence');
      await blockerTrace.getByRole('button', { name: 'Accept and resume' }).click();
      await expect(
        page.getByLabel('Selected task record').getByText('Goal resume requested')
      ).toBeVisible();
    });

    test('Goal blocker answer can reject the current evidence with a typed instruction', async ({
      page
    }) => {
      await mockTaskApis(page, [blockerTask]);
      await page.goto(`/tasks?task=${blockerTaskID}`);
      const blockerTrace = page.getByLabel('Goal blocker trace');
      await blockerTrace.getByRole('button', { name: /Not acceptable: require more work/ }).click();
      const answer = blockerTrace.getByLabel('Goal blocker answer');
      await expect(answer).toContainText('Not acceptable: require more work');
      await expect(answer.getByLabel('Instruction for the next run')).toHaveValue(/Not acceptable\./);
      await answer.getByRole('button', { name: 'Reopen with this answer' }).click();
      await expect(
        page.getByLabel('Selected task record').getByText('Task reopened with answer')
      ).toBeVisible();

      await blockerTrace.getByRole('button', { name: /Answer another way/ }).click();
      await blockerTrace
        .getByLabel('Instruction for the next run')
        .fill('Compare against the licensed AG Grid reference fixture before closing Phase 04.');
      await expect(blockerTrace.getByRole('button', { name: 'Reopen with custom answer' })).toBeEnabled();
    });

    test('agent-resolvable Goal blockers say Autopilot owns the next step', async ({ page }) => {
      await mockTaskApis(page, [agentRepairTask]);
      await page.goto(`/tasks?task=${agentRepairTaskID}`);
      await expect(page.getByRole('heading', { name: 'Map enterprise parity scope' })).toBeVisible();
      const blockerTrace = page.getByLabel('Goal blocker trace');
      await expect(blockerTrace).toContainText('Autopilot found repair work');
      await expect(blockerTrace).toContainText('Next autonomous step');
      await expect(blockerTrace).toContainText('No human decision needed');
      await expect(blockerTrace).toContainText('Map every current Enterprise feature category');
      await expect(blockerTrace.getByRole('button', { name: 'Let Autopilot repair' })).toBeVisible();
      await expect(blockerTrace.getByRole('button', { name: /Accept current evidence/ })).toHaveCount(0);
      await expectNoAxeViolations(page);
    });
  });
}
