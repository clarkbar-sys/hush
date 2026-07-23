// hush console service worker — just enough to make the console installable as
// an Android PWA and to open offline. It caches the app shell (the single-page
// index.html and its icons) and stays entirely out of the way of the live API:
// /api/* is never cached, so the fleet you see is always fresh.
//
// Bump CACHE when the shell changes so clients pull the new version; activate
// sweeps away older caches.
const CACHE = 'hush-shell-v2';
const SHELL = [
  '/',
  '/manifest.webmanifest',
  '/icon-192.png',
  '/icon-512.png',
  '/icon-192-maskable.png',
  '/icon-512-maskable.png',
  '/apple-touch-icon.png',
];

self.addEventListener('install', (event) => {
  // Precache the shell, then take over without waiting for old tabs to close.
  event.waitUntil(
    caches.open(CACHE).then((c) => c.addAll(SHELL)).then(() => self.skipWaiting()),
  );
});

self.addEventListener('activate', (event) => {
  event.waitUntil(
    caches
      .keys()
      .then((keys) => Promise.all(keys.filter((k) => k !== CACHE).map((k) => caches.delete(k))))
      .then(() => self.clients.claim()),
  );
});

// A tapped notification — the "page" the riff pager sends — focuses the already
// open console if one is around, otherwise opens a fresh window at the launcher.
self.addEventListener('notificationclick', (event) => {
  event.notification.close();
  event.waitUntil(
    self.clients.matchAll({ type: 'window', includeUncontrolled: true }).then((wins) => {
      for (const w of wins) {
        if ('focus' in w) return w.focus();
      }
      return self.clients.openWindow('/');
    }),
  );
});

// Web Push handler, here for the day a real push server (VAPID keys +
// subscriptions on hush-control) lands. Nothing sends these yet — the riff
// pager's "Try me" calls showNotification straight from the page so it works
// with no backend — but wiring it now means a future server just works.
self.addEventListener('push', (event) => {
  let data = { title: '*RIFF*', body: 'riff together' };
  try {
    if (event.data) data = { ...data, ...event.data.json() };
  } catch (_) {
    /* non-JSON payload: keep the defaults */
  }
  event.waitUntil(
    self.registration.showNotification(data.title, {
      body: data.body,
      icon: '/icon-192.png',
      badge: '/icon-192.png',
      tag: data.tag || 'riff-page',
      renotify: true,
    }),
  );
});

self.addEventListener('fetch', (event) => {
  const req = event.request;
  const url = new URL(req.url);

  // Only touch same-origin GETs. Everything else (API POSTs, cross-origin
  // media proxied through control, etc.) goes straight to the network.
  if (req.method !== 'GET' || url.origin !== self.location.origin) return;

  // The live API must never be cached — always hit the network so the console
  // reflects the real fleet, and surface errors to the app as they happen.
  if (url.pathname.startsWith('/api/')) return;

  // Navigations: network-first so a reachable control always wins, falling
  // back to the cached shell so the installed app still opens when offline.
  if (req.mode === 'navigate') {
    event.respondWith(
      fetch(req).catch(() => caches.match('/', { ignoreSearch: true })),
    );
    return;
  }

  // Static shell assets (icons, manifest): cache-first, and warm the cache on
  // first miss so a later offline launch has them.
  event.respondWith(
    caches.match(req).then(
      (hit) =>
        hit ||
        fetch(req).then((res) => {
          if (res.ok && res.type === 'basic') {
            const copy = res.clone();
            caches.open(CACHE).then((c) => c.put(req, copy));
          }
          return res;
        }),
    ),
  );
});
