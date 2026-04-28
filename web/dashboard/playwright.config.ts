import { defineConfig, devices } from '@playwright/test';

const port = Number(process.env.PLAYWRIGHT_PORT || worktreePort(process.cwd()));
const baseURL = `http://127.0.0.1:${port}`;
const executablePath =
  process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE ||
  (process.env.HOMELAB_PLAYWRIGHT_USE_SYSTEM_CHROME === '1' ? process.env.CHROME_BIN : undefined);
const launchOptions = executablePath ? { executablePath } : undefined;

function worktreePort(cwd: string) {
  let hash = 0;
  for (const char of cwd) {
    hash = (hash * 31 + char.charCodeAt(0)) >>> 0;
  }
  return 31_000 + (hash % 8_000);
}

export default defineConfig({
  testDir: './e2e',
  timeout: 30_000,
  workers: 1,
  expect: {
    timeout: 5_000
  },
  webServer: {
    command: `bun run dev -- --host 127.0.0.1 --port ${port} --strictPort`,
    url: `${baseURL}/chat`,
    timeout: 30_000,
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
