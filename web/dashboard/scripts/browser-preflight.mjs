import { chromium } from '@playwright/test';

const executablePath =
  process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE ||
  (process.env.HOMELAB_PLAYWRIGHT_USE_SYSTEM_CHROME === '1' ? process.env.CHROME_BIN : undefined);

const launchOptions = {
  headless: true,
  chromiumSandbox: false,
  args: ['--disable-breakpad', '--disable-crash-reporter', '--disable-dev-shm-usage'],
  ...(executablePath ? { executablePath } : {})
};

const hintFor = (message) => {
  if (/shared libraries|cannot open shared object file|lib.*not found/i.test(message)) {
    return [
      'Chromium is missing runtime libraries.',
      'Run browser UAT through the Nix dev shell: nix develop -c bun run --cwd web uat:site',
      'Remote agents must run the same command in their selected remote workdir.'
    ];
  }
  if (/Operation not permitted|SIGTRAP|sandbox_host_linux|crashpad|setsockopt/i.test(message)) {
    return [
      'The current execution sandbox is blocking Chromium process syscalls.',
      'Run this browser UAT on a worker where headless Chromium is permitted, normally via nix develop.',
      'Do not fall back to the production dashboard or restart supervised services.'
    ];
  }
  return [
    'Run browser UAT through the Nix dev shell: nix develop -c bun run --cwd web uat:site',
    'If this is a remote task, validate on the selected remote worker and report the command, URL, and port.'
  ];
};

try {
  const browser = await chromium.launch(launchOptions);
  const page = await browser.newPage();
  await page.setContent('<main><h1>browser preflight ok</h1></main>');
  await page.locator('h1').textContent();
  await browser.close();
  console.log('Headless Chromium preflight passed.');
} catch (error) {
  const message = error instanceof Error ? error.message : String(error);
  console.error('Headless Chromium preflight failed.');
  console.error(message);
  for (const hint of hintFor(message)) {
    console.error(`- ${hint}`);
  }
  process.exit(1);
}
