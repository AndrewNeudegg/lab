import { expect, test } from '@playwright/test';
import type { Page } from '@playwright/test';

const docsRenderTimeoutMs = 60_000;

const expectNoHorizontalOverflow = async (page: Page) => {
  const overflow = await page.evaluate(() => ({
    bodyWidth: document.body.scrollWidth,
    viewport: window.innerWidth
  }));
  expect(overflow.bodyWidth, JSON.stringify(overflow)).toBeLessThanOrEqual(overflow.viewport + 2);
};

const expectDocsLibraryBelowNavbar = async (page: Page) => {
  const metrics = await page.evaluate(() => {
    const navbar = document.querySelector<HTMLElement>('.navbar');
    const library = document.querySelector<HTMLElement>('.library');
    if (!navbar || !library) {
      return null;
    }
    return {
      navbarBottom: navbar.getBoundingClientRect().bottom,
      libraryTop: library.getBoundingClientRect().top
    };
  });
  expect(metrics).not.toBeNull();
  expect(metrics!.libraryTop, JSON.stringify(metrics)).toBeGreaterThanOrEqual(
    metrics!.navbarBottom
  );
};

const expectDocsLibraryClearOfArticle = async (page: Page) => {
  const metrics = await page.evaluate(() => {
    const library = document.querySelector<HTMLElement>('.library');
    if (!library) {
      return null;
    }
    const toRect = (rect: DOMRect) => ({
      top: rect.top,
      right: rect.right,
      bottom: rect.bottom,
      left: rect.left,
      width: rect.width,
      height: rect.height
    });
    const intersects = (a: ReturnType<typeof toRect>, b: ReturnType<typeof toRect>) =>
      a.width > 0 &&
      a.height > 0 &&
      b.width > 0 &&
      b.height > 0 &&
      a.left < b.right - 1 &&
      a.right > b.left + 1 &&
      a.top < b.bottom - 1 &&
      a.bottom > b.top + 1;
    const inViewport = (rect: ReturnType<typeof toRect>) =>
      rect.bottom > 0 &&
      rect.right > 0 &&
      rect.top < window.innerHeight &&
      rect.left < window.innerWidth;
    const libraryRect = toRect(library.getBoundingClientRect());
    const overlaps = Array.from(
      document.querySelectorAll<HTMLElement>('.article-header, .content .markdown > *')
    )
      .map((element) => ({
        label: (element.textContent || element.tagName).trim().replace(/\s+/g, ' ').slice(0, 80),
        rect: toRect(element.getBoundingClientRect())
      }))
      .filter((entry) => inViewport(entry.rect) && intersects(libraryRect, entry.rect))
      .slice(0, 5);
    return {
      scrollY: window.scrollY,
      viewport: { width: window.innerWidth, height: window.innerHeight },
      libraryPosition: getComputedStyle(library).position,
      library: libraryRect,
      overlaps
    };
  });
  expect(metrics).not.toBeNull();
  expect(metrics!.overlaps, JSON.stringify(metrics)).toEqual([]);
};

const expandDocsNavigation = async (page: Page) => {
  const toggle = page.getByRole('button', { name: 'Expand docs navigation' });
  await expect(toggle).toBeVisible();
  await toggle.click();
  await expect(page.getByRole('button', { name: 'Collapse docs navigation' })).toHaveAttribute(
    'aria-expanded',
    'true'
  );
};

const mockNavbarTaskApis = async (page: Page) => {
  await page.route(/\/api\/tasks\/?(?:\?.*)?$/, async (route) => {
    await route.fulfill({ json: { tasks: [] } });
  });
  await page.route(/\/api\/approvals\/?(?:\?.*)?$/, async (route) => {
    await route.fulfill({ json: { approvals: [] } });
  });
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

test('docs library supports navigation, markdown rendering, table of contents, and search', async ({
  page
}, testInfo) => {
  testInfo.setTimeout(120_000);
  await mockNavbarTaskApis(page);
  await page.setViewportSize({ width: 1440, height: 1000 });
  await page.goto('/docs');

  await expect(
    page.getByRole('navigation', { name: 'Primary' }).getByRole('link', { name: 'Docs' })
  ).toHaveAttribute('aria-current', 'page');
  await expect(page.getByRole('heading', { name: 'Dashboard', exact: true })).toBeVisible();
  const diagram = page.locator('.content .mermaid-diagram').first();
  await expect(diagram).toHaveAttribute('data-mermaid-status', 'rendered', {
    timeout: docsRenderTimeoutMs
  });
  await expect(diagram.locator('svg')).toBeVisible({ timeout: docsRenderTimeoutMs });
  await expect(page.getByText('./docs/dashboard.md')).toBeVisible();
  await expectDocsLibraryClearOfArticle(page);
  await expect.poll(async () => page.locator('#docs-list a').count()).toBeGreaterThanOrEqual(6);
  await expect(page.locator('.content .markdown a[href^="https://developer.apple.com"]')).toHaveCount(
    2
  );
  await page.getByRole('button', { name: 'Switch to dark mode' }).click();
  await expect(diagram).toHaveAttribute('data-mermaid-status', 'rendered');
  await expect(diagram).toHaveAttribute('data-mermaid-rendered', /^dark:/);

  await page.locator('#docs-list a', { hasText: 'Task Workflow' }).click();
  await expect(page).toHaveURL(/\/docs\/task-workflow$/);
  await expect(page.getByRole('heading', { name: 'Task Workflow', exact: true })).toBeVisible();
  await expect(page.locator('#docs-list a[aria-current="page"]')).toContainText('Task Workflow');
  await expect(page.locator('.content pre code').first()).toContainText('reopen');

  await page.locator('#docs-list a', { hasText: 'Diagramming And Brand Colours' }).click();
  await expect(page).toHaveURL(/\/docs\/diagramming-and-brand-colours$/);
  await expect(
    page.getByRole('heading', { name: 'Diagramming And Brand Colours', exact: true })
  ).toBeVisible();
  await expectDocsLibraryClearOfArticle(page);
  await expect(page.locator('.content .mermaid-diagram svg')).toBeVisible();
  await expect
    .poll(() =>
      page.locator('.content .mermaid-diagram svg').evaluate((element) => element.outerHTML)
    )
    .toContain('#60a5fa');

  await page.goto('/docs/task-workflow');
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
  await mockNavbarTaskApis(page);
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto('/docs/chat-commands');

  await expect(page.getByRole('heading', { name: 'Chat Commands', exact: true })).toBeVisible();
  await expect(
    page.locator('.content pre code').filter({ hasText: 'reflect on our recent interaction' })
  ).toBeVisible();
  await expectDocsLibraryBelowNavbar(page);
  const docsNavigationToggle = page.getByRole('button', { name: 'Expand docs navigation' });
  await expect(docsNavigationToggle).toBeVisible();
  await expect(docsNavigationToggle).toHaveAttribute('aria-expanded', 'false');
  await expect(page.getByRole('combobox', { name: 'Jump to document' })).toBeHidden();
  await expect(page.locator('.docs-shell')).toHaveAttribute('data-docs-library-ready', 'true');
  await expectDocsLibraryClearOfArticle(page);

  await page.evaluate(() => window.scrollTo(0, 260));
  await expectDocsLibraryClearOfArticle(page);
  await page.evaluate(() => window.scrollTo(0, 0));
  await expectDocsLibraryBelowNavbar(page);
  await expandDocsNavigation(page);
  await expect(page.getByRole('combobox', { name: 'Jump to document' })).toBeVisible();
  await expectDocsLibraryClearOfArticle(page);

  const mobileNav = await openMobileMenu(page);
  await expect(mobileNav.getByRole('link', { name: 'Docs' })).toHaveAttribute(
    'aria-current',
    'page'
  );
  await page.getByRole('button', { name: 'Menu' }).click();
  await expect(mobileNav).toBeHidden();

  const jump = page.getByRole('combobox', { name: 'Jump to document' });
  await jump.selectOption('dashboard');
  await jump.dispatchEvent('change');
  await expect(page).toHaveURL(/\/docs\/dashboard$/);
  await expect(page.getByRole('heading', { name: 'Dashboard', exact: true })).toBeVisible();
  await expect(page.locator('#docs-list a[aria-current="page"]')).toContainText('Dashboard');

  await page.goto('/docs/chat-commands');
  await expect(page.locator('.content .mermaid-diagram svg')).toBeVisible();

  await expandDocsNavigation(page);
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
