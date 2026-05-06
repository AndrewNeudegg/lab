import { describe, expect, test } from 'bun:test';
import { readFileSync } from 'node:fs';

const pageSource = readFileSync(new URL('./+page.svelte', import.meta.url), 'utf8');

describe('assistant page composition', () => {
  test('keeps run selection URL-addressable like other record pages', () => {
    expect(pageSource).toContain('assistantRunURL');
    expect(pageSource).toContain('const currentRunRouteId = () =>');
    expect(pageSource).toContain('const navigateToRun = (runId: string, replaceState = false) =>');
    expect(pageSource).toContain("void goto(next, { keepFocus: true, noScroll: true, replaceState })");
    expect(pageSource).toContain('const navigateToRunOverview = (replaceState = true) =>');
    expect(pageSource).toContain("void goto('/assistant', { keepFocus: true, noScroll: true, replaceState })");
    expect(pageSource).toContain('href={assistantRunURL(run.id)}');
    expect(pageSource).toContain("on:click={() => navigateToRunOverview()}");
  });
});
