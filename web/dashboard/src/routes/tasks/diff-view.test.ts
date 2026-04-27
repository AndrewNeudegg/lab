import { describe, expect, test } from 'bun:test';
import { buildSplitRows, inlineChangeSegments, parseUnifiedDiff } from './diff-view';

const rawDiff = [
  'diff --git a/src/app.ts b/src/app.ts',
  'index 1111111..2222222 100644',
  '--- a/src/app.ts',
  '+++ b/src/app.ts',
  '@@ -1,4 +1,5 @@',
  ' import { run } from "./runner";',
  '-const mode = "slow";',
  '+const mode = "fast";',
  '+const retries = 2;',
  ' run(mode);',
  'diff --git a/docs/old.md b/docs/new.md',
  'similarity index 88%',
  'rename from docs/old.md',
  'rename to docs/new.md',
  '--- a/docs/old.md',
  '+++ b/docs/new.md',
  '@@ -1 +1 @@',
  '-Old title',
  '+New title'
].join('\n');

describe('diff view parser', () => {
  test('parses files, hunks, line numbers, and per-file stats', () => {
    const files = parseUnifiedDiff(rawDiff);

    expect(files).toHaveLength(2);
    expect(files[0].path).toBe('src/app.ts');
    expect(files[0].status).toBe('modified');
    expect(files[0].additions).toBe(2);
    expect(files[0].deletions).toBe(1);
    expect(files[0].hunks[0].lines.map((line) => [line.kind, line.oldNumber, line.newNumber])).toEqual([
      ['context', 1, 1],
      ['delete', 2, undefined],
      ['add', undefined, 2],
      ['add', undefined, 3],
      ['context', 3, 4]
    ]);
    expect(files[1].status).toBe('renamed');
    expect(files[1].oldPath).toBe('docs/old.md');
    expect(files[1].path).toBe('docs/new.md');
  });

  test('builds split rows that pair changed lines and keep pure additions separate', () => {
    const [file] = parseUnifiedDiff(rawDiff);
    const rows = buildSplitRows(file);

    expect(rows.map((row) => row.kind)).toEqual(['hunk', 'context', 'change', 'add', 'context']);
    expect(rows[2].left?.content).toBe('const mode = "slow";');
    expect(rows[2].right?.content).toBe('const mode = "fast";');
    expect(rows[3].right?.content).toBe('const retries = 2;');
  });

  test('highlights only the changed inline span', () => {
    const segments = inlineChangeSegments('const mode = "slow";', 'const mode = "fast";');

    expect(segments.left).toEqual([
      { text: 'const mode = "', changed: false },
      { text: 'slow', changed: true },
      { text: '";', changed: false }
    ]);
    expect(segments.right).toEqual([
      { text: 'const mode = "', changed: false },
      { text: 'fast', changed: true },
      { text: '";', changed: false }
    ]);
  });
});
