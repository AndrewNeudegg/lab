import AxeBuilder from '@axe-core/playwright';
import { expect, test } from '@playwright/test';
import type { Page } from '@playwright/test';

const now = '2026-04-28T12:00:00.000Z';

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
  answer:
    'Answering "How should evidence be reviewed?" from 1 stored source:\n- [S1] Keep evidence visible beside generated claims.',
  key_findings: ['[S1] Keep evidence visible beside generated claims.'],
  evidence: [
    {
      id: 'evidence_01',
      source_id: knowledgeSource.id,
      source_title: knowledgeSource.title,
      citation_label: 'S1',
      excerpt: 'Source-grounded reports should keep evidence visible beside generated claims.',
      terms: ['evidence'],
      score: 3
    }
  ],
  gaps: ['Only stored Knowledge Space sources were used for this report.'],
  created_at: now
};

const baseKnowledgeSpace = {
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

const mockKnowledgeApis = async (page: Page) => {
  await freezeTime(page);
  let knowledgeSpaces = [structuredClone(baseKnowledgeSpace)];

  await page.route(/\/api\/tasks(?:\?.*)?$/, async (route) => {
    await route.fulfill({ json: { tasks: [] } });
  });
  await page.route(/\/api\/approvals$/, async (route) => {
    await route.fulfill({ json: { approvals: [] } });
  });
  await page.route(/\/api\/knowledge\/spaces$/, async (route) => {
    await route.fulfill({ json: { spaces: knowledgeSpaces } });
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
        updated_at: now
      },
      updated_at: now
    };
    knowledgeSpaces = [updated];
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
    knowledgeSpaces = [updated];
    await route.fulfill({ json: { space: updated, report, reply: 'Research report created' } });
  });
};

const expectKnowledgeReady = async (page: Page) => {
  await expect(page.locator('.knowledge-page')).toHaveAttribute('data-ready', 'true');
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

const expectNoVisualArtifacts = async (page: Page) => {
  const metrics = await page.evaluate(() => {
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
    const escaped = Array.from(document.querySelectorAll('h1,h2,h3,p,a,button,label,span,strong,small,summary'))
      .filter((element) => {
        if (isHidden(element) || element.closest('.nav-measure')) {
          return false;
        }
        const rect = element.getBoundingClientRect();
        return rect.width > 0 && rect.height > 0 && (rect.left < -2 || rect.right > window.innerWidth + 2);
      })
      .map((element) => (element.textContent || element.getAttribute('aria-label') || '').trim());
    const clippedControls = Array.from(document.querySelectorAll('button,a,select,input,summary'))
      .filter((element) => {
        const rect = element.getBoundingClientRect();
        return rect.width > 0 && rect.height > 0 && element.scrollWidth > element.clientWidth + 2;
      })
      .map((element) => (element.textContent || element.getAttribute('aria-label') || '').trim());
    return {
      bodyWidth: document.body.scrollWidth,
      docWidth: document.documentElement.scrollWidth,
      viewport: window.innerWidth,
      escaped,
      clippedControls
    };
  });
  expect(metrics.bodyWidth, JSON.stringify(metrics)).toBeLessThanOrEqual(metrics.viewport + 2);
  expect(metrics.docWidth, JSON.stringify(metrics)).toBeLessThanOrEqual(metrics.viewport + 2);
  expect(metrics.escaped, JSON.stringify(metrics)).toEqual([]);
  expect(metrics.clippedControls, JSON.stringify(metrics)).toEqual([]);
};

for (const viewport of [
  { name: 'desktop', width: 1440, height: 1000, mobile: false },
  { name: 'mobile', width: 390, height: 844, mobile: true }
]) {
  test.describe(`Knowledge UI quality on ${viewport.name}`, () => {
    test.use({
      viewport: { width: viewport.width, height: viewport.height },
      isMobile: viewport.mobile,
      hasTouch: viewport.mobile
    });

    test('keeps evidence primary and add-source controls scoped', async ({ page }) => {
      await mockKnowledgeApis(page);
      await page.goto('/knowledge');
      await expectKnowledgeReady(page);

      await page.getByRole('link', { name: /Research synthesis/ }).click();
      await expect(page.getByRole('heading', { name: 'Research synthesis' })).toBeInViewport();
      await expect(page.getByRole('heading', { name: 'Processed sources' })).toBeVisible();
      await expect(page.getByRole('heading', { name: 'Source transparency notes' })).toBeVisible();
      await expect(page.getByLabel('Source title')).toBeHidden();

      await expectNoVisualArtifacts(page);
      await expectNoAxeViolations(page);
      await expect(page).toHaveScreenshot(`knowledge-ui-quality-${viewport.name}.png`, {
        fullPage: !viewport.mobile,
        animations: 'disabled'
      });

      await page.locator('details.add-source > summary').click();
      await page.getByLabel('Source title').fill('Review notes');
      await page.getByLabel('Source text').fill('Evidence should stay visible when teams review generated claims.');
      await page.locator('.source-form button[type="submit"]').click();
      await expect(page.getByText('Source processed')).toBeVisible();
      await expect(page.getByRole('heading', { name: 'Review notes' })).toBeVisible();
    });

    test('uses suggested questions and reports as explicit selectors', async ({ page }) => {
      await mockKnowledgeApis(page);
      await page.goto('/knowledge');
      await expectKnowledgeReady(page);

      await page.getByRole('link', { name: /Research synthesis/ }).click();
      await page.getByRole('button', { name: 'What does this space show about source?' }).click();
      await expect(page.getByRole('tab', { name: /Research/ })).toHaveAttribute('aria-selected', 'true');
      await expect(page.getByRole('textbox', { name: 'Question' })).toHaveValue(
        'What does this space show about source?'
      );
      await expect(page.getByText('All 1 source selected')).toBeVisible();

      await page.getByRole('tab', { name: /Reports/ }).click();
      await page.getByRole('button', { name: /How should evidence be reviewed/ }).click();
      await expect(page.getByRole('tab', { name: /Research/ })).toHaveAttribute('aria-selected', 'true');
      await expect(page.getByRole('article', { name: 'Latest research report' })).toContainText('[S1]');
    });

    test('keeps filtered empty states recoverable', async ({ page }) => {
      await mockKnowledgeApis(page);
      await page.goto('/knowledge');
      await expectKnowledgeReady(page);

      const search = page.getByRole('searchbox', { name: 'Search Knowledge Space' });
      await search.fill('missing');
      await expect(page.getByText('No Knowledge Space matches this search.')).toBeVisible();
      await page.getByRole('button', { name: 'Clear search', exact: true }).click();
      await expect(search).toHaveValue('');
      await expect(page.getByRole('link', { name: /Research synthesis/ })).toBeVisible();
    });
  });
}
