import { describe, expect, test } from 'bun:test';
import {
  buildDocsLibrary,
  docsSlugFromPath,
  extractDocsHeadings,
  filterDocs,
  findDocBySlug
} from './library';

describe('docs library', () => {
  const docs = buildDocsLibrary([
    {
      path: '../../../../../docs/task-workflow.md',
      content: '# Task Workflow\n\nHow tasks move through review.\n\n## States\n\n- queued\n\n## States\n\nrepeat'
    },
    {
      path: '../../../../../docs/chat-commands.md',
      content: '# Chat Commands\n\nOperate the built-in chat app.\n\n## Messages\n\nreflect on our recent interaction'
    }
  ]);

  test('builds stable slugs and metadata from docs paths', () => {
    expect(docsSlugFromPath('../../../../../docs/chat-commands.md')).toBe('chat-commands');
    expect(docs.map((doc) => doc.slug)).toEqual(['chat-commands', 'task-workflow']);
    expect(docs.find((doc) => doc.slug === 'task-workflow')?.summary).toBe(
      'How tasks move through review.'
    );
  });

  test('extracts heading anchors for table of contents links', () => {
    expect(extractDocsHeadings('## States\n\n### Review Gate\n\n## States')).toEqual([
      { id: 'states', level: 2, title: 'States' },
      { id: 'review-gate', level: 3, title: 'Review Gate' },
      { id: 'states-2', level: 2, title: 'States' }
    ]);
  });

  test('filters across titles, summaries, paths, and content', () => {
    expect(filterDocs(docs, 'reflect').map((doc) => doc.slug)).toEqual(['chat-commands']);
    expect(filterDocs(docs, 'review').map((doc) => doc.slug)).toEqual(['task-workflow']);
    expect(filterDocs(docs, '').map((doc) => doc.slug)).toEqual([
      'chat-commands',
      'task-workflow'
    ]);
  });

  test('finds documents by nested-safe slug', () => {
    expect(findDocBySlug(docs, '/task-workflow/')?.title).toBe('Task Workflow');
    expect(findDocBySlug(docs, 'missing')).toBeUndefined();
  });
});
