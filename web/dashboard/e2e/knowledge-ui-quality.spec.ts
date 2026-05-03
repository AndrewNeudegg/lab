import AxeBuilder from '@axe-core/playwright';
import { expect, test } from '@playwright/test';
import type { Locator, Page } from '@playwright/test';

const now = '2026-04-28T12:00:00.000Z';
const longResearchToken =
  'research-overflow-regression-source-with-a-very-long-unbroken-identifier-for-mobile-and-desktop-layout-checks';

const knowledgeSource = {
  id: 'ksrc_20260428_120000_33333333',
  title: 'Source transparency notes',
  kind: 'text',
  content:
    '## Review flow\n\nSource-grounded reports should keep **evidence visible** beside generated claims.\n\n```mermaid\nflowchart LR\n  Source --> Evidence\n  Evidence --> Claim\n```',
  summary: 'Source-grounded reports should keep **evidence visible** beside generated claims.',
  key_terms: ['source', 'evidence', 'reports'],
  questions: ['What does this source show about evidence?'],
  claims: [{ id: 'claim_1', text: 'Evidence should stay visible beside generated claims.', importance: 'high' }],
  entities: [{ name: 'Knowledge Space', type: 'product', description: 'Research corpus' }],
  reliability_notes: ['Operator-provided source text.'],
  word_count: 8,
  provenance: {
    content_hash: 'sha256:test',
    snapshot_path: 'snapshots/kspace/ksrc.txt',
    extractor: 'plain-text'
  },
  ingestion: {
    state: 'ready',
    stage: 'indexed',
    message: 'Source is indexed and available for retrieval.',
    completed_at: now
  },
  sections: [
    {
      id: 'section_1',
      source_id: 'ksrc_20260428_120000_33333333',
      source_title: 'Source transparency notes',
      index: 0,
      heading: 'Review flow',
      text: 'Source-grounded reports should keep evidence visible beside generated claims.',
      terms: ['source', 'evidence', 'review'],
      word_count: 8
    }
  ],
  chunks: [
    {
      id: 'chunk_1',
      source_id: 'ksrc_20260428_120000_33333333',
      source_title: 'Source transparency notes',
      section_id: 'section_1',
      section_title: 'Review flow',
      index: 0,
      citation_label: 'S1.1',
      text: 'Source-grounded reports should keep evidence visible beside generated claims.',
      terms: ['source', 'evidence'],
      semantic_terms: ['review', 'claim', 'evidence'],
      word_count: 8
    }
  ],
  created_at: now,
  updated_at: now
};

const knowledgeReport = {
  id: 'kreport_20260428_120000_44444444',
  question: 'How should evidence be reviewed?',
  mode: 'research',
  answer:
    '## Evidence review\n\nAnswering "How should evidence be reviewed?" from 1 stored source:\n\n- [S1] Keep **evidence** visible beside generated claims.\n\n```mermaid\nflowchart LR\n  Source --> Evidence\n  Evidence --> Claim\n```',
  key_findings: ['[S1] Keep evidence visible beside generated claims.'],
  evidence: [
    {
      id: 'evidence_01',
      source_id: knowledgeSource.id,
      source_title: knowledgeSource.title,
      section_id: 'section_1',
      section_title: 'Review flow',
      citation_label: 'S1',
      excerpt: 'Source-grounded reports should keep **evidence visible** beside generated claims.',
      terms: ['evidence'],
      source_summary: 'Source-grounded reports should keep evidence visible beside generated claims.',
      retrieval: 'hybrid',
      lexical_score: 3,
      semantic_score: 2,
      score: 18
    }
  ],
  gaps: ['Only stored Knowledge Space sources were used for this report.'],
  provider: 'openai',
  model: 'gpt-5.2',
  usage: { input_tokens: 320, output_tokens: 120, total_tokens: 440 },
  created_at: now
};

const knowledgeRun = {
  id: 'krun_20260428_120000_22222222',
  objective: 'Track evidence review patterns',
  scope: 'Stored corpus',
  depth: 'standard',
  status: 'completed',
  mode: 'research',
  discover_sources: false,
  plan: {
    rewritten_objective: 'Track evidence review patterns across the stored corpus.',
    search_queries: ['evidence visible generated claims'],
    steps: ['Retrieve cited chunks', 'Synthesize report']
  },
  source_ids: [knowledgeSource.id],
  report_id: knowledgeReport.id,
  sources_examined: 1,
  evidence_count: 1,
  provider: 'openai',
  model: 'gpt-5.2',
  usage: { input_tokens: 280, output_tokens: 90, total_tokens: 370 },
  events: [
    {
      id: 'krun_event_1',
      stage: 'retrieval',
      message: 'Retrieved matching corpus chunks from **indexed sources**.',
      created_at: now
    }
  ],
  coverage: [
    {
      id: 'coverage_01',
      topic: 'Evidence review report',
      status: 'covered',
      source_ids: [knowledgeSource.id],
      evidence_count: 1,
      notes: 'One cited evidence chunk covers the report.'
    }
  ],
  research_loops: [
    {
      id: 'loop_01',
      index: 1,
      query: 'Track evidence review patterns',
      queries: ['evidence visible generated claims'],
      status: 'completed',
      decision: 'continue',
      stop_reason: 'Stored evidence was useful, but an external source was still needed.',
      candidate_ids: ['candidate_existing', 'candidate_rejected'],
      source_ids: [knowledgeSource.id],
      accepted_count: 1,
      rejected_count: 1,
      failed_count: 0,
      evidence_count: 1,
      supported_claims: ['Evidence should stay visible beside generated claims.'],
      gaps: ['Needs an online corroborating source.'],
      follow_up_queries: ['external evidence review source transparency'],
      started_at: now,
      finished_at: now
    },
    {
      id: 'loop_02',
      index: 2,
      query: 'Track evidence review patterns',
      queries: ['external evidence review source transparency'],
      status: 'completed',
      decision: 'complete',
      stop_reason: 'Coverage is sufficient for the evidence review answer.',
      candidate_ids: ['candidate_existing'],
      source_ids: [knowledgeSource.id],
      accepted_count: 1,
      rejected_count: 0,
      failed_count: 0,
      evidence_count: 1,
      coverage: ['Evidence review report'],
      supported_claims: ['Cited evidence supports the final answer.'],
      gaps: [],
      follow_up_queries: [],
      started_at: now,
      finished_at: now
    }
  ],
  stop_reason: 'Coverage is sufficient for the evidence review answer.',
  source_candidates: [
    {
      id: 'candidate_existing',
      query: 'evidence visible generated claims',
      kind: 'web',
      provider: 'searxng',
      title: 'Source transparency notes',
      url: `https://example.com/source-transparency/${longResearchToken}`,
      domain: 'example.com',
      snippet: 'Evidence should stay visible beside generated claims.',
      content_type: 'text/html',
      fetched: true,
      extraction_state: 'text',
      extraction_message: 'The source directly supports the run.',
      word_count: 8,
      usefulness: 'accept',
      relevance_score: 91,
      coverage: ['Evidence review report'],
      source_id: knowledgeSource.id,
      status: 'accepted'
    },
    {
      id: 'candidate_rejected',
      query: 'evidence visible generated claims',
      kind: 'web',
      provider: 'searxng',
      title: `Unrelated event calendar ${longResearchToken}`,
      url: `https://example.com/events/${longResearchToken}`,
      domain: 'example.com',
      snippet: 'Event dates and venue logistics.',
      content_type: 'text/html',
      fetched: true,
      extraction_state: 'text',
      extraction_message: 'This source is unrelated to evidence review.',
      word_count: 6,
      usefulness: 'reject',
      relevance_score: 3,
      status: 'rejected'
    }
  ],
  workspace_path: `runs/kspace/krun/${longResearchToken}`,
  created_at: now,
  updated_at: now,
  started_at: now,
  finished_at: now
};

const baseKnowledgeSpace = {
  id: 'kspace_20260428_120000_55555555',
  title: 'Research synthesis',
  objective: 'Keep source-grounded research easy to review.',
  sources: [knowledgeSource],
  reports: [knowledgeReport],
  research_runs: [knowledgeRun],
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
  const researchRunRequests: Array<{
    objective?: string;
    depth?: string;
    mode?: string;
    discover_sources?: boolean;
    source_ids?: string[];
  }> = [];

  await page.route(/\/api\/tasks(?:\?.*)?$/, async (route) => {
    await route.fulfill({ json: { tasks: [] } });
  });
  await page.route(/\/api\/approvals$/, async (route) => {
    await route.fulfill({ json: { approvals: [] } });
  });
  await page.route(/\/api\/knowledge\/spaces$/, async (route) => {
    await route.fulfill({ json: { spaces: knowledgeSpaces } });
  });
  await page.route(/\/api\/knowledge\/spaces\/[^/]+$/, async (route) => {
    const url = new URL(route.request().url());
    const spaceId = decodeURIComponent(url.pathname.split('/').at(-1) || '');
    const method = route.request().method();
    const space = knowledgeSpaces.find((item) => item.id === spaceId);
    if (!space && method !== 'DELETE') {
      await route.fulfill({ status: 404, json: { error: 'space not found' } });
      return;
    }
    if (method === 'GET') {
      await route.fulfill({ json: space });
      return;
    }
    if (method === 'PATCH') {
      const body = route.request().postDataJSON() as {
        title?: string;
        objective?: string;
        description?: string;
      };
      const updated = {
        ...space!,
        title: body.title ?? space!.title,
        objective: body.objective ?? space!.objective,
        description: body.description ?? space!.description,
        updated_at: now
      };
      knowledgeSpaces = knowledgeSpaces.map((item) => (item.id === spaceId ? updated : item));
      await route.fulfill({ json: { space: updated, reply: 'Knowledge Space updated' } });
      return;
    }
    if (method === 'DELETE') {
      knowledgeSpaces = knowledgeSpaces.filter((item) => item.id !== spaceId);
      await route.fulfill({ json: { space_id: spaceId, reply: 'Knowledge Space deleted' } });
      return;
    }
    await route.fulfill({ status: 405, json: { error: 'method not allowed' } });
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
      ingestion: { state: 'ready', stage: 'indexed', message: 'Source is indexed.', completed_at: now },
      chunks: [
        {
          id: 'ksrc_created_chunk_001',
          source_id: 'ksrc_created',
          source_title: body.title || 'Added source',
          index: 0,
          citation_label: 'CREA.1',
          text: body.content || 'Added source summary.',
          word_count: (body.content || '').split(/\s+/).filter(Boolean).length
        }
      ],
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
    await route.fulfill({ status: 201, json: { space: updated, source, reply: 'Source analysed' } });
  });
  await page.route(/\/api\/knowledge\/spaces\/[^/]+\/sources\/[^/]+$/, async (route) => {
    const url = new URL(route.request().url());
    const parts = url.pathname.split('/');
    const spaceId = decodeURIComponent(parts.at(-3) || '');
    const sourceId = decodeURIComponent(parts.at(-1) || '');
    if (route.request().method() !== 'DELETE') {
      await route.fulfill({ status: 405, json: { error: 'method not allowed' } });
      return;
    }
    const current = knowledgeSpaces.find((item) => item.id === spaceId);
    if (!current) {
      await route.fulfill({ status: 404, json: { error: 'space not found' } });
      return;
    }
    const sources = (current.sources || []).filter((source) => source.id !== sourceId);
    const updated = {
      ...current,
      sources,
      insight: {
        ...current.insight,
        source_count: sources.length,
        word_count: sources.reduce((total, source) => total + (source.word_count || 0), 0),
        updated_at: now
      },
      updated_at: now
    };
    knowledgeSpaces = knowledgeSpaces.map((item) => (item.id === spaceId ? updated : item));
    await route.fulfill({ json: { space: updated, source_id: sourceId, reply: 'Source deleted' } });
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
  await page.route(/\/api\/knowledge\/spaces\/[^/]+\/query$/, async (route) => {
    await route.fulfill({
      json: {
        result: {
          query: 'evidence',
          terms: ['evidence'],
          evidence: knowledgeReport.evidence,
          created_at: now
        },
        reply: 'Corpus query completed.'
      }
    });
  });
  await page.route(/\/api\/knowledge\/spaces\/[^/]+\/ask$/, async (route) => {
    const body = route.request().postDataJSON() as { question?: string };
    await route.fulfill({
      json: {
        result: {
          question: body.question || knowledgeReport.question,
          answer: knowledgeReport.answer,
          key_findings: knowledgeReport.key_findings,
          evidence: knowledgeReport.evidence,
          gaps: knowledgeReport.gaps,
          provider: 'openai',
          model: 'gpt-5.2',
          usage: { input_tokens: 210, output_tokens: 70, total_tokens: 280 },
          created_at: now
        },
        reply: 'Grounded answer created.'
      }
    });
  });
  await page.route(/\/api\/knowledge\/spaces\/[^/]+\/research-runs$/, async (route) => {
    const body = route.request().postDataJSON() as {
      objective?: string;
      depth?: string;
      mode?: string;
      discover_sources?: boolean;
      source_ids?: string[];
    };
    researchRunRequests.push(body);
    const run = {
      ...knowledgeRun,
      id: 'krun_created',
      objective: body.objective || knowledgeRun.objective,
      depth: body.depth || 'standard',
      status: 'queued',
      discover_sources: Boolean(body.discover_sources),
      source_candidates: body.discover_sources
        ? [
            {
              id: 'candidate_created',
              query: body.objective || knowledgeRun.objective,
              kind: 'web',
              provider: 'searxng',
              title: 'Evidence review source',
              url: 'https://example.com/evidence-review',
              domain: 'example.com',
              snippet: 'Research runs preserve cited evidence for review.',
              content_type: 'text/html',
              fetched: true,
              extraction_state: 'text',
              extraction_message: 'Queued for source evaluation.',
              word_count: 12,
              usefulness: 'accept',
              relevance_score: 82,
              coverage: ['Evidence review report'],
              source_id: 'ksrc_discovered',
              status: 'accepted'
            }
          ]
        : [],
      research_loops: body.discover_sources
        ? [
            {
              id: 'loop_created',
              index: 1,
              query: body.objective || knowledgeRun.objective,
              queries: [body.objective || knowledgeRun.objective],
              status: 'searching',
              accepted_count: 1,
              rejected_count: 0,
              failed_count: 0,
              evidence_count: 0,
              follow_up_queries: [],
              started_at: now
            }
          ]
        : [],
      workspace_path: 'runs/kspace/krun_created',
      stop_reason: undefined,
      report_id: undefined,
      evidence_count: 0,
      events: [
        {
          id: 'krun_created_event_1',
          stage: 'queued',
          message: 'Research run queued for language model planning.',
          created_at: now
        }
      ],
      created_at: now,
      updated_at: now
    };
    const current = knowledgeSpaces[0];
    const updated = {
      ...current,
      research_runs: [run, ...(current.research_runs || [])],
      updated_at: now
    };
    knowledgeSpaces = [updated];
    await route.fulfill({ status: 201, json: { space: updated, run, reply: 'Research run queued.' } });
  });

  return { researchRunRequests };
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

const expectNoHorizontalOverflow = async (page: Page, selectors: string[]) => {
  const overflowing = await page.evaluate((targetSelectors) => {
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
    return targetSelectors.flatMap((selector) =>
      Array.from(document.querySelectorAll(selector))
        .filter((element) => {
          if (isHidden(element)) {
            return false;
          }
          const rect = element.getBoundingClientRect();
          return rect.width > 0 && rect.height > 0 && element.scrollWidth > element.clientWidth + 2;
        })
        .map((element) => ({
          selector,
          text: (element.textContent || element.getAttribute('aria-label') || '').trim().slice(0, 120),
          scrollWidth: element.scrollWidth,
          clientWidth: element.clientWidth
        }))
    );
  }, selectors);
  expect(overflowing, JSON.stringify(overflowing)).toEqual([]);
};

const openDetailsIfClosed = async (locator: Locator) => {
  const isOpen = await locator.evaluate((element) => element.hasAttribute('open'));
  if (!isOpen) {
    await locator.locator('summary').click();
  }
  await expect(locator).toHaveAttribute('open', '');
};

const expectHorizontallyInsideViewport = async (page: Page, locator: Locator) => {
  await expect(locator).toBeVisible();
  const box = await locator.boundingBox();
  const viewport = page.viewportSize();
  expect(box).not.toBeNull();
  expect(viewport).not.toBeNull();
  expect(box!.x, JSON.stringify(box)).toBeGreaterThanOrEqual(-1);
  expect(box!.x + box!.width, JSON.stringify({ box, viewport })).toBeLessThanOrEqual(
    viewport!.width + 1
  );
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

      if (viewport.mobile) {
        await expect(page.locator('.space-list')).toBeHidden();
        await expect(page.getByLabel('Knowledge Space mobile controls')).toBeVisible();
        await expect(page.getByRole('button', { name: 'Sync Knowledge Spaces' })).toBeVisible();
        await expect(page.getByRole('button', { name: 'Browse Knowledge Spaces' })).toBeVisible();
        await expect(page.getByRole('button', { name: 'Create Knowledge Space' })).toBeVisible();
        await expect(page.getByRole('button', { name: 'More Knowledge Space options' })).toBeVisible();
        await page.getByRole('button', { name: 'Browse Knowledge Spaces' }).click();
        await expect(page.getByRole('region', { name: 'Browse Knowledge Spaces' })).toBeVisible();
        await expect(page.getByRole('searchbox', { name: 'Search Knowledge Space' })).toBeVisible();
        await page.getByRole('button', { name: 'Hide Knowledge Space browser' }).click();
      } else {
        await page.getByRole('link', { name: /Research synthesis/ }).click();
      }
      await expect(page.getByRole('heading', { name: 'Research synthesis' })).toBeInViewport();
      await expect(page.getByRole('heading', { name: 'Processed sources' })).toBeVisible();
      await expect(page.getByRole('heading', { name: 'Source transparency notes' })).toBeVisible();
      await expect(page.locator('.source-card .markdown strong').filter({ hasText: 'evidence visible' }).first()).toBeHidden();
      await expect(page.locator('details.source-details').first()).toBeHidden();
      await expect(page.locator('.source-card .source-meta').first()).toBeHidden();
      await expect(page).toHaveScreenshot(`knowledge-sources-collapsed-${viewport.name}.png`, {
        fullPage: !viewport.mobile,
        animations: 'disabled'
      });
      await page.locator('details.source-card > summary').first().click();
      await expect(page.locator('.source-card .markdown strong').filter({ hasText: 'evidence visible' }).first()).toBeVisible();
      await expect(page.locator('details.source-details').first()).toContainText('Evidence, metadata, and full text');
      await page.locator('details.source-details > summary').click();
      await expect(page.locator('.source-card').first()).toContainText('Sections');
      await expect(page.locator('.source-card').first()).toContainText('Review flow');
      await page.locator('details.source-content > summary').click();
      await expect(page.getByRole('heading', { name: 'Review flow' })).toBeVisible();
      await expect(page.locator('.source-card .mermaid-diagram[data-mermaid-status="rendered"]')).toBeVisible();
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
      await expect(
        (viewport.mobile ? page.getByLabel('Knowledge Space detail') : page.getByLabel('Knowledge Space list')).getByText(
          'Source analysed'
        )
      ).toBeVisible();
      await expect(page.getByRole('heading', { name: 'Review notes' })).toBeVisible();
    });

    test('uses suggested questions, grounded ask, research, and reports as explicit selectors', async ({ page }) => {
      const api = await mockKnowledgeApis(page);
      await page.goto('/knowledge');
      await expectKnowledgeReady(page);

      if (viewport.mobile) {
        await page.getByRole('button', { name: 'More Knowledge Space options' }).click();
      } else {
        await page.getByRole('link', { name: /Research synthesis/ }).click();
      }
      await page.getByRole('button', { name: 'What does this space show about source?' }).click();
      await expect(page.getByRole('tab', { name: /Ask/ })).toHaveAttribute('aria-selected', 'true');
      await expect(page.getByRole('textbox', { name: 'Question' })).toHaveValue(
        'What does this space show about source?'
      );
      await expect(page.getByRole('tabpanel', { name: 'Ask' }).locator('details.source-picker > summary')).toHaveText(
        'All 1 source selected'
      );
      await page.getByRole('button', { name: 'Ask', exact: true }).click();
      await expect(page.getByRole('article', { name: 'Grounded answer' })).toContainText('[S1]');
      await expect(page.getByRole('article', { name: 'Grounded answer' }).getByRole('heading', { name: 'Evidence review' })).toBeVisible();
      await expect(page.locator('[aria-label="Grounded answer"] .mermaid-diagram[data-mermaid-status="rendered"]')).toBeVisible();
      await page.getByRole('article', { name: 'Grounded answer' }).getByRole('link', { name: 'S1' }).first().click();
      await expect(page.getByRole('tab', { name: /Sources/ })).toHaveAttribute('aria-selected', 'true');
      await expect(page.locator('#knowledge-source-ksrc_20260428_120000_33333333')).toHaveAttribute('open', '');

      await page.getByRole('tab', { name: /Research/ }).click();
      const selectedResearch = page.getByRole('article', { name: 'Selected research' });
      const finalAnswer = page.locator('[aria-label="Research final answer"]');
      const researchPlan = page.locator('[aria-label="Research plan"]');
      const researchEvidence = page.locator('[aria-label="Research evidence"]');
      const researchCoverage = page.locator('[aria-label="Research coverage"]');
      const sourceCandidates = page.locator('[aria-label="Discovered source candidates"]');
      const researchEvents = page.locator('[aria-label="Research events"]');
      await expect(selectedResearch).toContainText('Final answer');
      await expect(finalAnswer).toHaveAttribute('open', '');
      await expect(finalAnswer.getByRole('heading', { name: 'Evidence review' })).toBeVisible();
      await expect(page.locator('[aria-label="Research key findings"]')).not.toHaveAttribute('open', '');
      await expect(researchPlan).not.toHaveAttribute('open', '');
      await expect(researchEvidence).not.toHaveAttribute('open', '');
      await expect(researchCoverage).not.toHaveAttribute('open', '');
      await expect(sourceCandidates).not.toHaveAttribute('open', '');
      await expect(researchEvents).not.toHaveAttribute('open', '');
      await expect(selectedResearch).toContainText('Stop reason');
      await expect(selectedResearch).toContainText('Loop 1');
      await expect(selectedResearch).toContainText('Loop 2');
      await expectNoVisualArtifacts(page);
      await expectNoHorizontalOverflow(page, [
        '.tabs',
        '#knowledge-panel-runs',
        '[aria-label="Selected research"]',
        '[aria-label="Research final answer"]'
      ]);
      await expectNoAxeViolations(page);
      if (viewport.mobile) {
        await page.locator('[aria-label="Research final answer"]').scrollIntoViewIfNeeded();
      }
      await expect(page).toHaveScreenshot(`knowledge-research-run-${viewport.name}.png`, {
        fullPage: !viewport.mobile,
        animations: 'disabled',
        maxDiffPixels: 100
      });
      await researchPlan.locator('summary').click();
      await expect(researchPlan.getByText('evidence visible generated claims')).toBeVisible();
      const loops = page.locator('[aria-label="Research loops"] details');
      await expect(loops).toHaveCount(2);
      await loops.first().locator('summary').click();
      await expect(loops.first().getByText('external evidence review source transparency')).toBeVisible();
      await researchEvidence.locator('summary').click();
      await expect(researchEvidence.getByText('hybrid retrieval')).toBeVisible();
      await expect(researchEvidence.getByText('Review flow')).toBeVisible();
      await researchCoverage.locator('summary').click();
      await expect(researchCoverage.getByText('covered')).toBeVisible();
      await sourceCandidates.locator('summary').click();
      await expect(sourceCandidates.getByText('rejected')).toBeVisible();
      await expect(sourceCandidates.getByText(longResearchToken)).toBeVisible();
      await researchEvents.locator('summary').click();
      await expect(researchEvents.getByText('indexed sources')).toBeVisible();
      await expectNoHorizontalOverflow(page, [
        '#knowledge-panel-runs',
        '[aria-label="Selected research"]',
        '[aria-label="Research plan"]',
        '[aria-label="Research evidence"]',
        '[aria-label="Discovered source candidates"]'
      ]);

      await page.locator('#knowledge-panel-runs').getByLabel('Question or research goal').fill('Compare evidence review');
      await expect(page.getByLabel('Search web and academic sources')).toBeChecked();
      await page.getByRole('button', { name: 'Start research' }).click();
      await expect(page.getByRole('article', { name: 'Selected research' })).toContainText(
        'Compare evidence review'
      );
      expect(api.researchRunRequests).toHaveLength(1);
      expect(api.researchRunRequests[0]).toMatchObject({
        objective: 'Compare evidence review',
        mode: 'research',
        discover_sources: true,
        source_ids: [knowledgeSource.id]
      });
      await expect(page.getByRole('article', { name: 'Selected research' })).toContainText('Queued');
      await expect(page.getByRole('article', { name: 'Selected research' })).toContainText('Loop 1');
      await expect(page.getByRole('button', { name: /Compare evidence review/ })).toHaveCount(0);
      await expect(page.getByRole('button', { name: /Track evidence review patterns/ })).toHaveCount(1);
      const queuedCandidates = page.locator('[aria-label="Discovered source candidates"]');
      await openDetailsIfClosed(queuedCandidates);
      await expect(queuedCandidates.getByText('example.com')).toBeVisible();
      await expectNoVisualArtifacts(page);

      await page.getByRole('tab', { name: /Reports/ }).click();
      await page.getByRole('button', { name: /How should evidence be reviewed/ }).first().click();
      await expect(page.getByRole('tab', { name: /Reports/ })).toHaveAttribute('aria-selected', 'true');
      const selectedReport = page.getByRole('article', { name: 'Selected artefact' });
      await expect(selectedReport).toContainText('[S1]');
      await expect(page.locator('[aria-label="Report answer"]')).toHaveAttribute('open', '');
      await expect(page.locator('[aria-label="Report key findings"]')).not.toHaveAttribute('open', '');
      await expect(page.locator('[aria-label="Report evidence"]')).not.toHaveAttribute('open', '');
      await expect(page.locator('[aria-label="Report gaps"]')).not.toHaveAttribute('open', '');
      await expect(selectedReport.getByRole('heading', { name: 'Evidence review' })).toBeVisible();
      await expectNoHorizontalOverflow(page, [
        '#knowledge-panel-artefacts',
        '[aria-label="Selected artefact"]',
        '[aria-label="Report answer"]'
      ]);
      await expectNoVisualArtifacts(page);
      await expectNoAxeViolations(page);
      if (viewport.mobile) {
        await page.locator('[aria-label="Report answer"]').scrollIntoViewIfNeeded();
      }
      await expect(page).toHaveScreenshot(`knowledge-report-${viewport.name}.png`, {
        fullPage: !viewport.mobile,
        animations: 'disabled',
        maxDiffPixels: 100
      });
      await page.locator('[aria-label="Report evidence"] summary').click();
      await expect(page.locator('[aria-label="Report evidence"]').getByText('Review flow')).toBeVisible();
    });

    test('renames spaces and deletes sources or spaces through explicit confirmations', async ({ page }) => {
      await mockKnowledgeApis(page);
      await page.goto('/knowledge');
      await expectKnowledgeReady(page);

      if (viewport.mobile) {
        await expect(page.getByLabel('Space', { exact: true })).toBeVisible();
        await page.getByRole('button', { name: 'More Knowledge Space options' }).click();
      } else {
        await page.getByRole('link', { name: /Research synthesis/ }).click();
      }

      await page.getByRole('button', { name: 'Rename' }).click();
      const editSpace = page.getByRole('region', { name: 'Edit Knowledge Space' });
      await expect(editSpace).toBeVisible();
      await editSpace.getByLabel('Space title').fill('Research corpus');
      await editSpace.getByLabel('Objective').fill('Keep source-grounded research easy to manage.');
      await page.getByRole('button', { name: 'Save changes' }).click();
      await expect(page.getByRole('heading', { name: 'Research corpus' })).toBeVisible();
      await expect(
        (viewport.mobile ? page.getByLabel('Knowledge Space detail') : page.getByLabel('Knowledge Space list')).getByText(
          'Knowledge Space updated'
        )
      ).toBeVisible();

      await page.locator('details.source-card > summary').first().click();
      await page.getByRole('button', { name: 'Delete source Source transparency notes' }).click();
      const deleteSourcePanel = page.getByRole('region', {
        name: 'Delete source Source transparency notes confirmation'
      });
      await expect(deleteSourcePanel).toBeVisible();
      await expectHorizontallyInsideViewport(page, deleteSourcePanel);
      await expectNoVisualArtifacts(page);
      await expectNoAxeViolations(page);
      await expect(page).toHaveScreenshot(`knowledge-delete-source-confirm-${viewport.name}.png`, {
        fullPage: !viewport.mobile,
        animations: 'disabled',
        maxDiffPixels: 100
      });
      await deleteSourcePanel.getByRole('button', { name: 'Delete source' }).click();
      await expect(
        (viewport.mobile ? page.getByLabel('Knowledge Space detail') : page.getByLabel('Knowledge Space list')).getByText(
          'Source deleted'
        )
      ).toBeVisible();
      await expect(page.getByRole('heading', { name: 'Source transparency notes' })).toHaveCount(0);
      await expect(page.getByText('No sources have been analysed. Add text or a URL before asking questions.')).toBeVisible();

      if (viewport.mobile) {
        await page.getByRole('button', { name: 'More Knowledge Space options' }).click();
      }
      await page.getByRole('button', { name: 'Delete space' }).click();
      const deleteSpacePanel = page.getByRole('region', { name: 'Delete Knowledge Space confirmation' });
      await expect(deleteSpacePanel).toContainText('Research corpus');
      await expectHorizontallyInsideViewport(page, deleteSpacePanel);
      await expectNoVisualArtifacts(page);
      await expectNoAxeViolations(page);
      await expect(page).toHaveScreenshot(`knowledge-delete-space-confirm-${viewport.name}.png`, {
        fullPage: !viewport.mobile,
        animations: 'disabled',
        maxDiffPixels: 100
      });
      await deleteSpacePanel.getByRole('button', { name: 'Delete space' }).click();
      await expect(page.getByLabel('Knowledge Space list').getByText('Knowledge Space deleted')).toBeVisible();
      await expect(page.getByText('No Knowledge Space selected')).toBeVisible();
      await expect(page.getByText('No Knowledge Spaces yet.')).toBeVisible();
    });

    test('keeps filtered empty states recoverable', async ({ page }) => {
      await mockKnowledgeApis(page);
      await page.goto('/knowledge');
      await expectKnowledgeReady(page);

      if (viewport.mobile) {
        await page.getByRole('button', { name: 'Browse Knowledge Spaces' }).click();
      }
      const search = page.getByRole('searchbox', { name: 'Search Knowledge Space' });
      await search.fill('missing');
      await expect(
        (viewport.mobile ? page.getByRole('region', { name: 'Browse Knowledge Spaces' }) : page.getByLabel('Knowledge Space list')).getByText(
          'No Knowledge Space matches this search.'
        )
      ).toBeVisible();
      await page.getByRole('button', { name: 'Clear search', exact: true }).click();
      await expect(search).toHaveValue('');
      await expect(page.getByRole('link', { name: /Research synthesis/ })).toBeVisible();
    });
  });
}
