import { defineConfig, devices } from '@playwright/test';
import { chromiumExecutablePath } from './scripts/chromium-executable.mjs';
import {
  playwrightExpectTimeout,
  playwrightTestTimeout,
  playwrightWebServerTimeout,
  worktreePort
} from './scripts/playwright-settings';

const port = Number(process.env.PLAYWRIGHT_PORT || worktreePort(process.cwd()));
const baseURL = `http://127.0.0.1:${port}`;
const executablePath = chromiumExecutablePath();
const launchOptions = {
  ...(executablePath ? { executablePath } : {}),
  chromiumSandbox: false,
  args: ['--disable-breakpad', '--disable-crash-reporter', '--disable-dev-shm-usage']
};

export default defineConfig({
  testDir: './e2e',
  timeout: playwrightTestTimeout(process.env),
  workers: 1,
  expect: {
    timeout: playwrightExpectTimeout(process.env)
  },
  webServer: {
    command: `bun run dev -- --host 127.0.0.1 --port ${port} --strictPort`,
    url: `${baseURL}/chat`,
    timeout: playwrightWebServerTimeout(process.env),
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
