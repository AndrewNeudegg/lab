import { expect, test } from '@playwright/test';

test('dashboard exposes installable PWA metadata', async ({ page, request }) => {
  await page.goto('/chat');

  await expect(page.locator('link[rel="manifest"]')).toHaveAttribute(
    'href',
    '/manifest.webmanifest'
  );
  await expect(page.locator('link[rel="icon"]')).toHaveAttribute(
    'href',
    '/icons/homelab-dashboard.svg'
  );
  await expect(page.locator('meta[name="theme-color"]').first()).toHaveAttribute('content');
  await expect(page.locator('meta[name="apple-mobile-web-app-capable"]')).toHaveAttribute(
    'content',
    'yes'
  );
  await expect(page.locator('meta[name="apple-mobile-web-app-title"]')).toHaveAttribute(
    'content',
    'homelabd'
  );

  const manifestResponse = await request.get('/manifest.webmanifest');
  expect(manifestResponse.ok()).toBe(true);
  expect(manifestResponse.headers()['content-type']).toContain('application/manifest+json');

  const manifest = await manifestResponse.json();
  expect(manifest.name).toBe('homelabd Dashboard');
  expect(manifest.short_name).toBe('homelabd');
  expect(manifest.start_url).toBe('/chat');
  expect(manifest.scope).toBe('/');
  expect(manifest.display).toBe('standalone');
  expect(manifest.display_override).toEqual(['standalone', 'browser']);
  expect(manifest.lang).toBe('en');

  const icons = manifest.icons as Array<{
    src: string;
    sizes: string;
    type: string;
    purpose?: string;
  }>;
  expect(icons).toEqual(
    expect.arrayContaining([
      expect.objectContaining({ sizes: '192x192', type: 'image/png', purpose: 'any' }),
      expect.objectContaining({ sizes: '512x512', type: 'image/png', purpose: 'any' }),
      expect.objectContaining({ sizes: '512x512', type: 'image/png', purpose: 'maskable' })
    ])
  );

  for (const icon of icons) {
    const iconResponse = await request.get(icon.src);
    expect(iconResponse.ok()).toBe(true);
    expect(iconResponse.headers()['content-type']).toContain(icon.type);
  }
});

test('dashboard install action follows the browser install prompt lifecycle', async ({ page }) => {
  await page.addInitScript(() => {
    class MockBeforeInstallPromptEvent extends Event {
      promptCalls = 0;
      userChoice = Promise.resolve({ outcome: 'accepted', platform: 'web' });

      async prompt() {
        this.promptCalls += 1;
        (window as Window & { __pwaPromptCalls?: number }).__pwaPromptCalls = this.promptCalls;
      }
    }

    (window as Window & { __dispatchPwaInstallPrompt?: () => void }).__dispatchPwaInstallPrompt =
      () => {
        window.dispatchEvent(new MockBeforeInstallPromptEvent('beforeinstallprompt'));
      };
  });

  await page.goto('/chat');
  await page.evaluate(() =>
    (window as Window & { __dispatchPwaInstallPrompt: () => void }).__dispatchPwaInstallPrompt()
  );

  const install = page.getByRole('button', { name: 'Install app' });
  await expect(install).toBeVisible();
  await install.click();
  await expect
    .poll(() => page.evaluate(() => (window as Window & { __pwaPromptCalls?: number }).__pwaPromptCalls))
    .toBe(1);
  await expect(install).toBeHidden();
});

test('dashboard update action asks a waiting service worker to take over', async ({ page }) => {
  await page.addInitScript(() => {
    const waitingWorker = new EventTarget() as ServiceWorker & {
      postMessage: (message: unknown) => void;
    };
    Object.defineProperty(waitingWorker, 'state', { value: 'installed' });
    waitingWorker.postMessage = (message: unknown) => {
      (window as Window & { __pwaUpdateMessage?: unknown }).__pwaUpdateMessage = message;
    };

    const registration = new EventTarget() as ServiceWorkerRegistration & {
      update: () => Promise<void>;
    };
    Object.defineProperty(registration, 'waiting', { value: waitingWorker });
    Object.defineProperty(registration, 'installing', { value: null });
    registration.update = async () => undefined;

    const serviceWorker = new EventTarget() as ServiceWorkerContainer;
    Object.defineProperty(serviceWorker, 'ready', { value: Promise.resolve(registration) });
    Object.defineProperty(serviceWorker, 'controller', { value: {} });
    Object.defineProperty(navigator, 'serviceWorker', {
      configurable: true,
      value: serviceWorker
    });
  });

  await page.goto('/chat');
  const update = page.getByRole('button', { name: 'Update app' });
  await expect(update).toBeVisible();
  await update.click();
  await expect
    .poll(() =>
      page.evaluate(() => (window as Window & { __pwaUpdateMessage?: unknown }).__pwaUpdateMessage)
    )
    .toEqual({ type: 'SKIP_WAITING' });
});
