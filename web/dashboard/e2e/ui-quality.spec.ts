import AxeBuilder from '@axe-core/playwright';
import { expect, test } from '@playwright/test';
import type { Page } from '@playwright/test';

const now = '2026-04-28T12:00:00.000Z';
const taskID = 'task_20260428_120000_uiux1111';
const themeStorageKey = 'homelabd.dashboard.theme';

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
const closedBlockingTaskFlow = {
  role: 'blocking_task',
  title: 'This task is blocking Goal Autopilot',
  decision_label: 'Decide whether to resume the Goal',
  decision_detail:
    'This task is already closed, but its report left a Goal-level blocker. Choose whether the current evidence is acceptable, or reopen the task with the missing requirement.',
  show_blocking_task_link: false,
  show_resume_goal_action: false,
  show_check_goal_action: true,
  decision_choices: [
    {
      id: 'accept_current',
      kind: 'resume',
      title: 'Accept current evidence',
      detail: 'Use when the blocker is acceptable and the Goal can continue.',
      action_label: 'Accept and resume'
    },
    {
      id: 'require_more',
      kind: 'reopen',
      title: 'Not acceptable: require more work',
      detail: 'Use when the answer is no: reopen the task with a clear missing requirement.',
      action_label: 'Reopen with this answer',
      default_instruction:
        'Not acceptable. Require the stricter path before resuming Autopilot: Should the supervisor accept the current evidence or require an independent comparison? Do not close or resume the Goal until the missing evidence, comparison, implementation, or product decision is completed and reported back with validation.'
    },
    {
      id: 'custom',
      kind: 'custom',
      title: 'Answer another way',
      detail: 'Write the exact instruction or product decision that should guide the next run.',
      action_label: 'Reopen with custom answer'
    }
  ]
};

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
    created_at: now,
    flow: closedBlockingTaskFlow
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
    blocking_task_url: `/tasks?task=${blockerTaskID}`,
    flow: {
      role: 'waiting_on_blocking_task',
      title: 'Goal is blocked by task block111',
      decision_label: 'Open the blocking task',
      decision_detail:
        'This task belongs to the blocked Goal, but the decision is on the linked blocking task.',
      show_blocking_task_link: true,
      show_resume_goal_action: false,
      show_check_goal_action: false
    }
  }
};

const openQuestionTaskID = 'task_20260428_121000_goalq111';
const openQuestionTask = {
  ...blockerTask,
  id: openQuestionTaskID,
  status: 'done',
  title: 'Audit One UI accessibility evidence',
  result: 'Task closed after reporting a Goal-level product decision.',
  goal_blocker_trace: {
    ...blockerTask.goal_blocker_trace,
    source_type: 'open_questions',
    source_id: 'goal_ui_quality',
    source_task_id: openQuestionTaskID,
    source_task_url: `/tasks?task=${openQuestionTaskID}`,
    blocking_task_id: undefined,
    blocking_task_url: undefined,
    reason:
      'Goal has an unanswered operator question: Will the product owner support all four AT/platform combinations, or should explicit waivers be recorded?',
    operator_action: 'Answer the Goal question, then resume Autopilot.',
    questions: [
      'Will the product owner support all four AT/platform combinations, or should explicit waivers be recorded?'
    ],
    blockers: ['Manual assistive-technology UAT is not available from this Linux worktree.'],
    flow: {
      role: 'goal_question',
      title: 'Goal is blocked by an open question',
      question:
        'Will the product owner support all four AT/platform combinations, or should explicit waivers be recorded?',
      decision_label: 'Answer the Goal question',
      decision_detail:
        'Record the product decision on the Goal so Autopilot can continue with that answer.',
      show_blocking_task_link: false,
      show_resume_goal_action: false,
      show_check_goal_action: true,
      decision_choices: [
        {
          id: 'require_full',
          kind: 'answer',
          title: 'Require the full requirement',
          detail: 'Use when the missing evidence or stricter path remains required before completion.',
          action_label: 'Record answer and resume',
          default_instruction:
            'The full requirement remains in scope for this Goal. Do not claim the Goal complete until this is satisfied with evidence: Will the product owner support all four AT/platform combinations, or should explicit waivers be recorded?'
        },
        {
          id: 'record_waiver',
          kind: 'answer',
          title: 'Record a waiver or deferment',
          detail:
            'Use when the product owner accepts that some requirement is unsupported or deferred for now.',
          action_label: 'Record waiver and resume',
          default_instruction:
            'Record an explicit product-owner waiver or deferment for the unsupported or untested requirement, then continue Autopilot using that decision as acceptance context: Will the product owner support all four AT/platform combinations, or should explicit waivers be recorded?'
        },
        {
          id: 'custom',
          kind: 'answer',
          title: 'Answer another way',
          detail: 'Write the exact product decision or operator instruction that should guide the next run.',
          action_label: 'Record custom answer'
        }
      ]
    }
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
    follow_ups: ['Rerun the public-doc challenge after mapping the feature categories.'],
    flow: {
      role: 'agent_repair',
      title: 'Autopilot found repair work',
      decision_label: 'No human decision needed',
      decision_detail:
        'Map every current Enterprise feature category to implemented evidence, accepted exclusion, or explicit gap severity.',
      show_blocking_task_link: false,
      show_resume_goal_action: true,
      show_check_goal_action: true
    }
  }
};

const remoteRunningTaskID = 'task_20260428_121100_remote1';
const remoteRunningTask = {
  ...task,
  id: remoteRunningTaskID,
  title: 'Run remote browser UAT',
  goal: 'Validate the selected remote task controls in dark mode.',
  status: 'running',
  assigned_to: 'remote:desk',
  started_at: now,
  result: 'Remote worker is still running.',
  remote_attempt: {
    id: 'attempt_20260428_121100_remote1',
    agent_id: 'desk',
    machine: 'desk-uat',
    backend: 'codex',
    workdir: '/srv/desk/homelabd',
    workdir_id: 'homelabd',
    state: 'running',
    status: 'running',
    offered_at: now,
    acknowledged_at: now,
    started_at: now,
    deadline_at: '2026-04-28T13:02:00Z'
  },
  target: {
    mode: 'remote',
    project_id: 'dashboard',
    agent_id: 'desk',
    machine: 'desk-uat',
    workdir_id: 'homelabd',
    workdir: '/srv/desk/homelabd',
    backend: 'codex'
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
  const remoteAgents = Array.from(
    new Map(
      taskItems
        .filter((item) => (item as any).target?.mode === 'remote' && (item as any).target?.agent_id)
        .map((item) => {
          const target = (item as any).target;
          return [
            target.agent_id,
            {
              id: target.agent_id,
              name: `${target.agent_id} agent`,
              machine: target.machine || target.agent_id,
              status: 'online'
            }
          ];
        })
    ).values()
  );
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
  await page.route(/\/api\/assistant\/goals\/[^/]+\/questions\/answer(?:\?.*)?$/, async (route) => {
    const body = route.request().postDataJSON() as { answer?: string };
    await route.fulfill({
      json: {
        reply: 'Goal question answered and Autopilot resumed.',
        timeline: {
          goal: {
            id: 'goal_ui_quality',
            status: 'active',
            open_questions: [],
            progress_summary: `Latest operator answer: ${body.answer || ''}`,
            autopilot: { status: 'running', tasks_started: 12, budget_tasks: 50 }
          },
          events: []
        }
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
  await page.route(/\/api\/agents\/?(?:\?.*)?$/, async (route) => {
    await route.fulfill({ json: { agents: remoteAgents } });
  });
  await page.route(/\/api\/workspaces\/?(?:\?.*)?$/, async (route) => {
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

const collectReadableStyles = async (page: Page, selectors: string[]) =>
  page.evaluate((surfaceSelectors) => {
    const parseRuntimeColor = (value: string) => {
      const parts = value.match(/[\d.]+/g)?.map(Number) ?? [0, 0, 0];
      return {
        r: parts[0] ?? 0,
        g: parts[1] ?? 0,
        b: parts[2] ?? 0
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
    const contrast = (left: number, right: number) => {
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
          contrast: 0
        };
      }
      const rect = element.getBoundingClientRect();
      const styles = getComputedStyle(element);
      const backgroundLuminance = luminance(parseRuntimeColor(styles.backgroundColor));
      const textLuminance = luminance(parseRuntimeColor(styles.color));
      return {
        selector,
        found: true,
        visible: rect.width > 0 && rect.height > 0 && styles.visibility !== 'hidden',
        backgroundColor: styles.backgroundColor,
        color: styles.color,
        backgroundLuminance,
        contrast: contrast(backgroundLuminance, textLuminance)
      };
    });
  }, selectors);

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

    test('remote execution context and stop action are dark themed', async ({ page }) => {
      await page.addInitScript((key) => {
        localStorage.setItem(key, 'dark');
      }, themeStorageKey);
      await mockTaskApis(page, [remoteRunningTask]);
      await page.goto('/tasks');
      await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark');
      await expect(page.locator('.theme-toggle').first()).toHaveAttribute(
        'data-theme-toggle-ready',
        'true'
      );

      await page
        .getByRole('region', { name: 'Task filters' })
        .getByRole('button', { name: /Running/ })
        .click();
      await page.getByRole('link', { name: /Run remote browser UAT/ }).click();
      await expect(page.getByRole('heading', { name: 'Run remote browser UAT' })).toBeVisible();

      const actions = page.getByRole('region', { name: 'Task actions', exact: true });
      const stopButton = actions.getByRole('button', { name: 'Stop worker' });
      await expect(stopButton).toBeVisible();
      await expect(stopButton).toBeEnabled();

      const context = page.getByLabel('Execution context');
      await expect(context).toContainText('Remote execution context');
      await expect(context).toContainText('desk-uat');
      await expect(context).toContainText('/srv/desk/homelabd');
      await expect(context).toContainText('backend codex');

      const attempt = page.getByLabel('Remote attempt');
      await expect(attempt).toContainText('Running remotely');
      await expect(attempt).toContainText('acknowledged this attempt');
      await expect(attempt).toContainText('Deadline');

      const styles = await collectReadableStyles(page, [
        '.execution-context',
        '.remote-attempt',
        '.secondary-actions button.danger'
      ]);
      for (const style of styles) {
        expect(style.found, `${style.selector} should exist`).toBe(true);
        expect(style.visible, `${style.selector} should be visible`).toBe(true);
        expect(
          style.backgroundLuminance,
          `${style.selector} should use a dark background: ${JSON.stringify(style)}`
        ).toBeLessThan(0.12);
        expect(
          style.contrast,
          `${style.selector} should keep readable text: ${JSON.stringify(style)}`
        ).toBeGreaterThan(3);
      }

      await expectNoAxeViolations(page);
      await context.scrollIntoViewIfNeeded();
      await expect(page).toHaveScreenshot(`tasks-remote-dark-context-${viewport.name}.png`, {
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
        page.getByLabel('Selected task record').getByText('Goal resumed')
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

    test('open Goal questions are answered on the Goal instead of reopening a closed task', async ({ page }) => {
      await mockTaskApis(page, [openQuestionTask]);
      await page.goto(`/tasks?task=${openQuestionTaskID}`);
      const blockerTrace = page.getByLabel('Goal blocker trace');
      await expect(page.getByRole('heading', { name: 'Audit One UI accessibility evidence' })).toBeVisible();
      await expect(blockerTrace).toContainText('Goal is blocked by an open question');
      await expect(blockerTrace).toContainText('Answer the Goal question');
      await expect(blockerTrace.getByRole('link', { name: 'Open blocking task' })).toHaveCount(0);
      await blockerTrace.getByRole('button', { name: /Record a waiver or deferment/ }).click();
      await expect(blockerTrace.getByLabel('Answer for Autopilot')).toHaveValue(/product-owner waiver/);
      await blockerTrace.getByRole('button', { name: 'Record waiver and resume' }).click();
      await expect(
        page.getByLabel('Selected task record').getByText('Goal question answered', { exact: true })
      ).toBeVisible();
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
