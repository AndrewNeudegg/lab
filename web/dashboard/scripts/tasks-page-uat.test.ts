import { describe, expect, test } from 'bun:test';
import { readFileSync } from 'node:fs';

const scriptSource = readFileSync(new URL('./tasks-page-uat.mjs', import.meta.url), 'utf8');

describe('tasks page UAT flow', () => {
  test('selects the all queue before asserting task rows', () => {
    const allClickIndex = scriptSource.indexOf("innerText.includes('All'))?.click()");
    const rowAssertionIndex = scriptSource.indexOf("assert(afterAll.rows > 0");

    expect(allClickIndex).toBeGreaterThan(-1);
    expect(rowAssertionIndex).toBeGreaterThan(-1);
    expect(allClickIndex).toBeLessThan(rowAssertionIndex);
  });

  test('checks the button-only task surface instead of the old chat command panel', () => {
    expect(scriptSource).toContain('old chat command panel still rendered');
    expect(scriptSource).toContain('chat composer still rendered on tasks page');
    expect(scriptSource).toContain('no direct action buttons rendered for selected task');
    expect(scriptSource).toContain('mobile tasks page still renders chat command controls');
  });

  test('exercises manual task pane sync against the browser network layer', () => {
    expect(scriptSource).toContain('manual Sync did not reload all task pane data sources');
    expect(scriptSource).toContain("path === '/api/tasks'");
    expect(scriptSource).toContain('manual Sync did not leave a selected visible task');
    expect(scriptSource).toContain('manual Sync freshness timestamp did not include seconds');
  });

  test('checks diff labels, wrapping, dark theme, and mode buttons in the browser', () => {
    expect(scriptSource).toContain('changed file list labels were empty or visually collapsed');
    expect(scriptSource).toContain('Unified diff control did not become active');
    expect(scriptSource).toContain('split diff code cells do not preserve and wrap whitespace');
    expect(scriptSource).toContain('long split diff line did not wrap to multiple visual lines');
    expect(scriptSource).toContain("localStorage.setItem('homelabd.dashboard.theme', 'dark')");
    expect(scriptSource).toContain('diff panel kept light-mode backgrounds in dark mode');
  });

  test('checks mobile Queue and Task tabs plus selected item changes', () => {
    expect(scriptSource).toContain('mobile Queue tab did not become active');
    expect(scriptSource).toContain('mobile task tap did not switch to Task tab');
    expect(scriptSource).toContain('mobile selected task did not show action buttons');
    expect(scriptSource).toContain('mobile Queue button in detail did not return to queue');
    expect(scriptSource).toContain('mobile selected detail has horizontal overflow');
  });
});
