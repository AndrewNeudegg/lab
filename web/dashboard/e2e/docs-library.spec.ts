import { expect, test } from '@playwright/test';
import type { Page } from '@playwright/test';

const expectNoHorizontalOverflow = async (page: Page) => {
  const overflow = await page.evaluate(() => ({
    bodyWidth: document.body.scrollWidth,
    viewport: window.innerWidth
  }));
  expect(overflow.bodyWidth, JSON.stringify(overflow)).toBeLessThanOrEqual(overflow.viewport + 2);
};

test('docs library supports navigation, markdown rendering, table of contents, and search', async ({
  page
}) => {
  await page.setViewportSize({ width: 1440, height: 1000 });
  await page.goto('/docs');

  await expect(
    page.getByRole('navigation', { name: 'Primary' }).getByRole('link', { name: 'Docs' })
  ).toHaveAttribute('aria-current', 'page');
  await expect(page.getByRole('heading', { name: 'Dashboard', exact: true })).toBeVisible();
  const diagram = page.locator('.content .mermaid-diagram').first();
  await expect(diagram).toHaveAttribute('data-mermaid-status', 'rendered', { timeout: 15_000 });
  await expect(diagram.locator('svg')).toBeVisible();
  await expect(page.getByText('./docs/dashboard.md')).toBeVisible();
  await expect.poll(async () => page.locator('#docs-list a').count()).toBeGreaterThanOrEqual(6);
  await expect(page.locator('.content .markdown a[href^="https://developer.apple.com"]')).toHaveCount(
    2
  );

  await page.locator('#docs-list a', { hasText: 'Task Workflow' }).click();
  await expect(page).toHaveURL(/\/docs\/task-workflow$/);
  await expect(page.getByRole('heading', { name: 'Task Workflow', exact: true })).toBeVisible();
  await expect(page.locator('#docs-list a[aria-current="page"]')).toContainText('Task Workflow');
  await expect(page.locator('.content pre code').first()).toContainText('reopen');

  const toc = page.getByRole('navigation', { name: 'On this page' });
  await expect(toc.getByRole('link', { name: 'States' })).toBeVisible();
  await toc.getByRole('link', { name: 'States' }).click();
  await expect(page).toHaveURL(/#states$/);

  await page.getByRole('searchbox', { name: 'Search documentation' }).fill('operator interface');
  await expect(page.locator('#docs-list a')).toHaveCount(1);
  await expect(page.locator('#docs-list a')).toContainText('homelabctl');
  await expectNoHorizontalOverflow(page);
});

test('docs library remains usable on mobile', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto('/docs/chat-commands');

  await expect(page.getByRole('heading', { name: 'Chat Commands', exact: true })).toBeVisible();
  await expect(
    page.locator('.content pre code').filter({ hasText: 'reflect on our recent interaction' })
  ).toBeVisible();
  await expect(page.getByRole('combobox', { name: 'Jump to document' })).toBeVisible();

  const searchbox = page.getByRole('searchbox', { name: 'Search documentation' });
  await searchbox.fill('operator interface');
  await expect(page.locator('#docs-list a')).toHaveCount(1);
  await searchbox.fill('');
  await expect(page.locator('#docs-result-count')).toContainText('9 documents');

  await page.getByRole('button', { name: 'Menu' }).click();
  await expect(
    page.getByRole('navigation', { name: 'Primary mobile' }).getByRole('link', { name: 'Docs' })
  ).toHaveAttribute('aria-current', 'page');
  await page.getByRole('button', { name: 'Menu' }).click();

  await page.getByRole('combobox', { name: 'Jump to document' }).selectOption('dashboard');
  await expect(page).toHaveURL(/\/docs\/dashboard$/);
  await expect(page.getByRole('heading', { name: 'Dashboard', exact: true })).toBeVisible();
  await expect(page.locator('#docs-list a[aria-current="page"]')).toContainText('Dashboard');

  await page.goto('/docs/chat-commands');
  await expect(page.locator('.content .mermaid-diagram svg')).toBeVisible();

  await page.getByRole('searchbox', { name: 'Search documentation' }).fill('operator interface');
  await expect(page.locator('#docs-list a')).toHaveCount(1);
  await expect(page.locator('#docs-list a')).toContainText('homelabctl');
  const docsListMetrics = await page.locator('#docs-list').evaluate((element) => ({
    scrollWidth: element.scrollWidth,
    clientWidth: element.clientWidth
  }));
  expect(docsListMetrics.scrollWidth, JSON.stringify(docsListMetrics)).toBeLessThanOrEqual(
    docsListMetrics.clientWidth + 2
  );
  await expectNoHorizontalOverflow(page);
});
