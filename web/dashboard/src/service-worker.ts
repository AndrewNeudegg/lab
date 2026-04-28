/// <reference no-default-lib="true"/>
/// <reference lib="esnext" />
/// <reference lib="webworker" />
/// <reference types="@sveltejs/kit" />

import { build, files, version } from '$service-worker';

const worker = self as unknown as ServiceWorkerGlobalScope;
const assetCacheName = `homelabd-dashboard-assets-${version}`;
const pageCacheName = `homelabd-dashboard-pages-${version}`;
const cacheNames = new Set([assetCacheName, pageCacheName]);
const offlineFallback = '/offline.html';
const apiPrefixes = ['/api', '/healthd-api', '/supervisord-api'];
const appAssets = new Set([...build, ...files]);
const precacheUrls = Array.from(new Set([...appAssets, offlineFallback]));

const isApiRequest = (url: URL) => apiPrefixes.some((prefix) => url.pathname.startsWith(prefix));

const cacheFirst = async (pathname: string) => {
  const cached = await caches.match(pathname);
  if (cached) {
    return cached;
  }
  return fetch(pathname);
};

const networkFirstPage = async (request: Request) => {
  const cache = await caches.open(pageCacheName);
  try {
    const response = await fetch(request);
    if (response.ok && response.type === 'basic') {
      await cache.put(request, response.clone());
    }
    return response;
  } catch (error) {
    const cached = await cache.match(request);
    if (cached) {
      return cached;
    }
    const fallback = await caches.match(offlineFallback);
    if (fallback) {
      return fallback;
    }
    throw error;
  }
};

worker.addEventListener('install', (event: ExtendableEvent) => {
  event.waitUntil(
    (async () => {
      const cache = await caches.open(assetCacheName);
      await cache.addAll(precacheUrls);
      await worker.skipWaiting();
    })()
  );
});

worker.addEventListener('activate', (event: ExtendableEvent) => {
  event.waitUntil(
    (async () => {
      for (const name of await caches.keys()) {
        if (name.startsWith('homelabd-dashboard-') && !cacheNames.has(name)) {
          await caches.delete(name);
        }
      }
      await worker.clients.claim();
    })()
  );
});

worker.addEventListener('fetch', (event: FetchEvent) => {
  const { request } = event;
  const url = new URL(request.url);

  if (request.method !== 'GET' || url.origin !== worker.location.origin || isApiRequest(url)) {
    return;
  }

  if (appAssets.has(url.pathname)) {
    event.respondWith(cacheFirst(url.pathname));
    return;
  }

  if (request.mode === 'navigate') {
    event.respondWith(networkFirstPage(request));
  }
});
