import { describe, expect, test } from 'bun:test';
import type { HomelabdKnowledgeSpace } from '@homelab/shared';
import {
  compactKnowledgeID,
  filterKnowledgeSpaces,
  knowledgeMarkdownPreview,
  knowledgeSpacesFromResponse,
  latestReport,
  panelItemCount,
  selectKnowledgeSpace,
  spaceSourceCount,
  spaceWordCount,
  sourceSelectionSummary
} from './view-model';

const space = (
  id: string,
  title: string,
  updatedAt: string,
  overrides: Partial<HomelabdKnowledgeSpace> = {}
): HomelabdKnowledgeSpace => ({
  id,
  title,
  insight: { source_count: 0, word_count: 0 },
  created_at: updatedAt,
  updated_at: updatedAt,
  ...overrides
});

describe('knowledge view model', () => {
  test('filters and sorts spaces by text from titles, sources, and terms', () => {
    const spaces = [
      space('kspace_old', 'Release notes', '2026-04-28T09:00:00Z', {
        sources: [{ id: 's1', title: 'Operational checklist' } as never]
      }),
      space('kspace_new', 'Research hub', '2026-04-29T09:00:00Z', {
        insight: { source_count: 1, word_count: 500, key_terms: ['retrieval'] }
      })
    ];

    expect(filterKnowledgeSpaces(spaces, '').map((item) => item.id)).toEqual([
      'kspace_new',
      'kspace_old'
    ]);
    expect(filterKnowledgeSpaces(spaces, 'checklist').map((item) => item.id)).toEqual([
      'kspace_old'
    ]);
    expect(filterKnowledgeSpaces(spaces, 'retrieval').map((item) => item.id)).toEqual([
      'kspace_new'
    ]);
  });

  test('selects routed, existing, or first visible space', () => {
    const spaces = [
      space('kspace_a', 'A', '2026-04-28T09:00:00Z'),
      space('kspace_b', 'B', '2026-04-29T09:00:00Z')
    ];

    expect(selectKnowledgeSpace(spaces, spaces, 'kspace_a', 'kspace_b')).toBe('kspace_b');
    expect(selectKnowledgeSpace(spaces, spaces, 'kspace_a', '')).toBe('kspace_a');
    expect(selectKnowledgeSpace(spaces, [spaces[1]], 'missing', '')).toBe('kspace_b');
  });

  test('normalises empty Knowledge Space list responses', () => {
    const spaces = [space('kspace_a', 'A', '2026-04-28T09:00:00Z')];

    expect(knowledgeSpacesFromResponse({ spaces })).toBe(spaces);
    expect(knowledgeSpacesFromResponse({ spaces: null })).toEqual([]);
    expect(knowledgeSpacesFromResponse(undefined)).toEqual([]);
    expect(() =>
      knowledgeSpacesFromResponse({ spaces: {} as never })
    ).toThrow('Knowledge Space response did not include a spaces array.');
  });

  test('derives counts, compact ids, and latest report', () => {
    const item = space('kspace_20260430_abcd1234', 'Research', '2026-04-29T09:00:00Z', {
      insight: { source_count: 2, word_count: 42 },
      reports: [
        { id: 'r1', question: 'old', mode: 'brief', answer: 'old', created_at: '2026-04-28T09:00:00Z' },
        { id: 'r2', question: 'new', mode: 'research', answer: 'new', created_at: '2026-04-29T09:00:00Z' }
      ]
    });

    expect(compactKnowledgeID(item.id)).toBe('abcd1234');
    expect(spaceSourceCount(item)).toBe(2);
    expect(spaceWordCount(item)).toBe(42);
    expect(latestReport(item)?.id).toBe('r2');
  });

  test('summarises panel counts and selected research sources', () => {
    const item = space('kspace_20260430_abcd1234', 'Research', '2026-04-29T09:00:00Z', {
      insight: { source_count: 2, word_count: 42 },
      reports: [
        { id: 'r1', question: 'old', mode: 'brief', answer: 'old', created_at: '2026-04-28T09:00:00Z' }
      ],
      research_runs: [
        {
          id: 'run1',
          objective: 'Compare sources',
          depth: 'standard',
          status: 'completed',
          mode: 'research',
          created_at: '2026-04-28T09:00:00Z',
          updated_at: '2026-04-28T09:00:00Z'
        }
      ]
    });

    expect(panelItemCount('sources', item)).toBe(2);
    expect(panelItemCount('ask', item)).toBe(2);
    expect(panelItemCount('runs', item)).toBe(1);
    expect(panelItemCount('artefacts', item)).toBe(1);
    expect(sourceSelectionSummary(0, 0)).toBe('No sources available');
    expect(sourceSelectionSummary(0, 2)).toBe('No sources selected');
    expect(sourceSelectionSummary(1, 2)).toBe('1/2 sources selected');
    expect(sourceSelectionSummary(2, 2)).toBe('All 2 sources selected');
  });

  test('builds plain previews from Markdown artefacts without Mermaid source text', () => {
    expect(
      knowledgeMarkdownPreview(
        '## Evidence review\n\n- Keep **evidence** visible.\n\n```mermaid\nflowchart LR\n  A --> B\n```'
      )
    ).toBe('Evidence review Keep evidence visible.');
  });
});
