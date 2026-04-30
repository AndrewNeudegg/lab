import { defineConfig, devices } from '@playwright/test';
import { chromiumExecutablePath } from './scripts/chromium-executable.mjs';

const port = Number(process.env.PLAYWRIGHT_PORT || worktreePort(process.cwd()));
const baseURL = `http://127.0.0.1:${port}`;
const webServerTimeout = Number(process.env.PLAYWRIGHT_WEB_SERVER_TIMEOUT || 120_000);
const testTimeout = Number(process.env.PLAYWRIGHT_TEST_TIMEOUT || 120_000);
const expectTimeout = Number(process.env.PLAYWRIGHT_EXPECT_TIMEOUT || 20_000);
const executablePath = chromiumExecutablePath();
const launchOptions = {
  ...(executablePath ? { executablePath } : {}),
  chromiumSandbox: false,
  args: ['--disable-breakpad', '--disable-crash-reporter', '--disable-dev-shm-usage']
};
function worktreePort(cwd: string) {
  let hash = 0;
  for (const char of cwd) {
    hash = (hash * 31 + char.charCodeAt(0)) >>> 0;
  }
  return 31_000 + (hash % 8_000);
}

export default defineConfig({
  testDir: './e2e',
  timeout: testTimeout,
  workers: 1,
  expect: {
    timeout: expectTimeout
  },
  webServer: {
    command: `bun run dev -- --host 127.0.0.1 --port ${port} --strictPort`,
    url: `${baseURL}/chat`,
    timeout: webServerTimeout,
    reuseExistingServer: false
  },
  use: {
    baseURL,
    trace: 'on-first-retry',
    launchOptions
  },
  projects: [
    {
      name: 'mobile-chromium',
      use: {
        ...devices['Pixel 5'],
        launchOptions
      }
    }
  ]
});
