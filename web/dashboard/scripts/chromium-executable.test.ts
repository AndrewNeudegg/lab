import { describe, expect, test } from 'bun:test';
import { delimiter, join } from 'node:path';
import { chromiumExecutablePath, findChromiumOnPath } from './chromium-executable.mjs';

describe('Chromium executable resolution', () => {
  test('keeps explicit Playwright executable override first', () => {
    expect(
      chromiumExecutablePath({
        PLAYWRIGHT_CHROMIUM_EXECUTABLE: '/custom/chrome',
        CHROME_BIN: '/ignored/chromium',
        PATH: ''
      })
    ).toBe('/custom/chrome');
  });

  test('can opt out of system Chromium discovery', () => {
    expect(
      chromiumExecutablePath({
        HOMELAB_PLAYWRIGHT_USE_SYSTEM_CHROME: '0',
        CHROME_BIN: '/nix/store/chromium',
        PATH: '/bin'
      })
    ).toBeUndefined();
  });

  test('finds Chromium on PATH when Playwright managed browser lacks runtime libraries', () => {
    const binDir = '/nix/store/example/bin';
    const candidate = join(binDir, 'chromium');
    const pathValue = ['/usr/bin', binDir].join(delimiter);

    expect(
      findChromiumOnPath({
        pathValue,
        canExecute: (path) => path === candidate
      })
    ).toBe(candidate);
    expect(
      chromiumExecutablePath(
        { PATH: pathValue },
        {
          canExecute: (path: string) => path === candidate
        }
      )
    ).toBe(candidate);
  });
});
