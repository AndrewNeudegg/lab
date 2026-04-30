import { describe, expect, test } from 'bun:test';
import {
  DEFAULT_EXPECT_TIMEOUT_MS,
  DEFAULT_TEST_TIMEOUT_MS,
  DEFAULT_WEB_SERVER_TIMEOUT_MS,
  playwrightExpectTimeout,
  playwrightTestTimeout,
  playwrightWebServerTimeout,
  worktreePort
} from './playwright-settings';

describe('Playwright settings', () => {
  test('uses a worktree-stable isolated port range', () => {
    const port = worktreePort('/home/lab/lab/workspaces/task_example');

    expect(port).toBe(worktreePort('/home/lab/lab/workspaces/task_example'));
    expect(port).toBeGreaterThanOrEqual(31_000);
    expect(port).toBeLessThan(39_000);
  });

  test('keeps enough startup budget for cold Vite dependency optimisation', () => {
    expect(playwrightWebServerTimeout({})).toBe(DEFAULT_WEB_SERVER_TIMEOUT_MS);
    expect(playwrightWebServerTimeout({ PLAYWRIGHT_WEB_SERVER_TIMEOUT: '180000' })).toBe(
      180_000
    );
    expect(playwrightWebServerTimeout({ PLAYWRIGHT_WEB_SERVER_TIMEOUT_MS: '150000' })).toBe(
      150_000
    );
    expect(playwrightWebServerTimeout({ PLAYWRIGHT_WEB_SERVER_TIMEOUT_MS: '0' })).toBe(
      DEFAULT_WEB_SERVER_TIMEOUT_MS
    );
  });

  test('keeps enough per-test and assertion budget for lazy route bundles', () => {
    expect(playwrightTestTimeout({})).toBe(DEFAULT_TEST_TIMEOUT_MS);
    expect(playwrightTestTimeout({ PLAYWRIGHT_TEST_TIMEOUT: '180000' })).toBe(180_000);
    expect(playwrightTestTimeout({ PLAYWRIGHT_TEST_TIMEOUT_MS: '150000' })).toBe(150_000);
    expect(playwrightTestTimeout({ PLAYWRIGHT_TEST_TIMEOUT: '-1' })).toBe(
      DEFAULT_TEST_TIMEOUT_MS
    );

    expect(playwrightExpectTimeout({})).toBe(DEFAULT_EXPECT_TIMEOUT_MS);
    expect(playwrightExpectTimeout({ PLAYWRIGHT_EXPECT_TIMEOUT: '30000' })).toBe(30_000);
    expect(playwrightExpectTimeout({ PLAYWRIGHT_EXPECT_TIMEOUT_MS: '25000' })).toBe(25_000);
    expect(playwrightExpectTimeout({ PLAYWRIGHT_EXPECT_TIMEOUT: 'bad' })).toBe(
      DEFAULT_EXPECT_TIMEOUT_MS
    );
  });
});
