import { defineConfig, devices } from '@playwright/test';
import {
  playwrightExpectTimeout,
  playwrightTestTimeout,
  playwrightWebServerTimeout,
  worktreePort
} from './scripts/playwright-settings';

const port = Number(process.env.PLAYWRIGHT_PORT || worktreePort(process.cwd()));
const baseURL = `http://127.0.0.1:${port}`;
const executablePath =
  process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE ||
  (process.env.HOMELAB_PLAYWRIGHT_USE_SYSTEM_CHROME === '1' ? process.env.CHROME_BIN : undefined);
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
