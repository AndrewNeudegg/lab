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
});
