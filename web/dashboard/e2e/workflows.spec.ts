import { expect, test } from '@playwright/test';
import type { Page } from '@playwright/test';

const now = '2026-04-28T00:00:00Z';

const estimate = (steps = 1, llm = 1, tools = 0, waits = 0) => ({
  steps,
  estimated_llm_calls: llm,
  estimated_tool_calls: tools,
  workflow_calls: 0,
  waits,
  estimated_seconds: llm * 45 + tools * 30 + waits * 120,
  estimated_minutes: Math.ceil((llm * 45 + tools * 30 + waits * 120) / 60),
  summary: `${steps} step(s), ${llm} LLM call(s), ${tools} tool call(s), ${waits} wait(s), about ${Math.ceil(
    (llm * 45 + tools * 30 + waits * 120) / 60
  )}m`
});

const mockWorkflowApi = async (page: Page) => {
  let createBody: { name: string; goal?: string; description?: string; steps?: unknown[] } | undefined;
  let workflows = [
    {
      id: 'workflow_20260428_000000_11111111',
      name: 'Research bundle',
      goal: 'Gather sources and summarise risk.',
      status: 'draft',
      steps: [{ id: 'step_01', name: 'Summarise', kind: 'llm', prompt: 'Summarise the sources.' }],
      estimate: estimate(),
      created_at: now,
      updated_at: now
    },
    {
      id: 'workflow_20260428_000100_22222222',
      name: 'Deployment watch',
      goal: 'Wait until deployment health is green.',
      status: 'waiting',
      steps: [
        {
          id: 'step_01',
          name: 'Health gate',
          kind: 'wait',
          condition: 'healthd reports healthy',
          timeout_seconds: 120
        }
      ],
      estimate: estimate(1, 0, 0, 1),
      created_at: now,
      updated_at: now
    }
  ];

  await page.route('**/api/workflows/*/run', async (route) => {
    const id = route.request().url().split('/').at(-2) || workflows[0].id;
    workflows = workflows.map((workflow) =>
      workflow.id === id
        ? {
            ...workflow,
            status: 'completed',
            updated_at: now,
            last_run: {
              id: 'wfrun_20260428_000200_33333333',
              workflow_id: id,
              status: 'completed',
              current_step: workflow.steps.length,
              started_at: now,
              finished_at: now,
              outputs: workflow.steps.map((step) => ({
                step_id: step.id,
                step_name: step.name,
                kind: step.kind,
                status: 'completed',
                summary: step.kind === 'tool' ? 'Tool completed: internet.search' : 'Step completed.',
                started_at: now,
                finished_at: now
              }))
            }
          }
        : workflow
    );
    const workflow = workflows.find((item) => item.id === id) || workflows[0];
    await route.fulfill({ json: { workflow, reply: `Workflow ${id} completed` } });
  });

  await page.route('**/api/workflows', async (route) => {
    if (route.request().method() === 'POST') {
      createBody = route.request().postDataJSON() as {
        name: string;
        goal?: string;
        description?: string;
        steps?: unknown[];
      };
      const created = {
        id: 'workflow_20260428_000300_44444444',
        name: createBody.name,
        goal: createBody.goal,
        description: createBody.description,
        status: 'draft',
        steps: [
          {
            id: 'step_01',
            name: 'Search',
            kind: 'tool',
            tool: 'internet.search',
            args: { query: 'agent workflow design' }
          }
        ],
        estimate: estimate(1, 0, 1, 0),
        created_at: now,
        updated_at: now
      };
      workflows = [created, ...workflows];
      await route.fulfill({ status: 201, json: { workflow: created, reply: 'Created workflow 44444444' } });
      return;
    }
    await route.fulfill({ json: { workflows } });
  });

  return {
    createBody: () => createBody
  };
};

const expectNoHorizontalOverflow = async (page: Page) => {
  const overflow = await page.evaluate(() => ({
    bodyWidth: document.body.scrollWidth,
    viewport: window.innerWidth
  }));
  expect(overflow.bodyWidth, JSON.stringify(overflow)).toBeLessThanOrEqual(overflow.viewport + 2);
};

test('workflows page creates, selects, estimates, and runs a workflow', async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 1000 });
  const api = await mockWorkflowApi(page);

  await page.goto('/workflows');
  await expect(
    page.getByRole('navigation', { name: 'Primary' }).getByRole('link', { name: 'Workflows' })
  ).toHaveAttribute('aria-current', 'page');
  await expect(page.getByRole('button', { name: /Research bundle/ })).toBeVisible();
  await expect(page.getByRole('region', { name: 'Workflow cost estimate' })).toContainText('LLM');
  await expect(page.getByRole('region', { name: 'Workflow cost estimate' })).toContainText('Runtime');

  await page.getByText('New workflow').click();
  await page.getByLabel('Name').fill('Created workflow');
  await page.getByLabel('Goal').fill('Search for agent workflow UI patterns.');
  await page
    .getByRole('textbox', { name: 'Steps' })
    .fill('tool | Search | internet.search | {"query":"agent workflow design"}');
  await page.getByRole('button', { name: 'Create' }).click();

  expect(api.createBody()).toEqual({
    name: 'Created workflow',
    goal: 'Search for agent workflow UI patterns.',
    steps: [{ name: 'Search', kind: 'tool', tool: 'internet.search', args: { query: 'agent workflow design' } }]
  });
  await expect(page.getByRole('button', { name: /Created workflow/ })).toBeVisible();
  await expect(page.getByRole('region', { name: 'Workflow cost estimate' })).toContainText('Tools');
  await expect(page.getByRole('region', { name: 'Workflow cost estimate' })).toContainText('1');

  await page.getByRole('button', { name: 'Run' }).click();
  await expect(page.getByRole('region', { name: 'Workflow detail' })).toContainText('completed');
  await expect(page.locator('[aria-label="Workflow run"]')).toContainText('Tool completed');
  await expectNoHorizontalOverflow(page);
});

test('workflows page remains usable on mobile', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await mockWorkflowApi(page);

  await page.goto('/workflows');
  await expect(page.getByRole('heading', { name: 'Workflows' })).toBeVisible();
  await page.getByRole('button', { name: 'Menu' }).click();
  await expect(
    page.getByRole('navigation', { name: 'Primary mobile' }).getByRole('link', { name: 'Workflows' })
  ).toHaveAttribute('aria-current', 'page');
  await page.getByRole('button', { name: 'Menu' }).click();

  await page.getByRole('button', { name: 'All' }).click();
  await page.getByRole('searchbox', { name: 'Search workflows' }).fill('deployment');
  await expect(page.getByRole('button', { name: /Deployment watch/ })).toBeVisible();
  await page.getByRole('button', { name: /Deployment watch/ }).click();
  await expect(page.getByRole('region', { name: 'Workflow detail' })).toContainText('Health gate');
  await expectNoHorizontalOverflow(page);
});
