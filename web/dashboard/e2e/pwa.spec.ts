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

  const manifestResponse = await request.get('/manifest.webmanifest');
  expect(manifestResponse.ok()).toBe(true);
  expect(manifestResponse.headers()['content-type']).toContain('application/manifest+json');

  const manifest = await manifestResponse.json();
  expect(manifest.name).toBe('homelabd Dashboard');
  expect(manifest.short_name).toBe('homelabd');
  expect(manifest.start_url).toBe('/chat');
  expect(manifest.scope).toBe('/');
  expect(manifest.display).toBe('standalone');

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
