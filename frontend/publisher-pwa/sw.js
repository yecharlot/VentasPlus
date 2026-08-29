/* VentasPlus SW v5 — Share Target */
const CACHE = 'ventasplus-v6';
const ASSETS = [
  '/',
  '/index.html',
  '/share-target.html',
  '/manifest.webmanifest',
  '/icon-192.png',
  '/icon-512.png',
];

self.addEventListener('install', (event) => {
  event.waitUntil(
    caches.open(CACHE).then((c) => c.addAll(ASSETS)).then(() => self.skipWaiting())
  );
});

self.addEventListener('activate', (event) => {
  event.waitUntil(
    caches.keys().then((keys) =>
      Promise.all(keys.filter((k) => k !== CACHE).map((k) => caches.delete(k)))
    ).then(() => self.clients.claim())
  );
});

self.addEventListener('fetch', (event) => {
  const url = new URL(event.request.url);

  if (
    event.request.method === 'POST' &&
    (url.pathname === '/share-target.html' || url.pathname.endsWith('/share-target.html'))
  ) {
    event.respondWith(handleShareTarget(event.request));
    return;
  }

  if (event.request.method !== 'GET') return;

  event.respondWith(
    fetch(event.request)
      .then((res) => {
        if (res.ok && url.origin === self.location.origin) {
          const copy = res.clone();
          caches.open(CACHE).then((c) => c.put(event.request, copy)).catch(() => {});
        }
        return res;
      })
      .catch(() =>
        caches.match(event.request).then((r) => r || caches.match('/index.html'))
      )
  );
});

async function handleShareTarget(request) {
  try {
    const form = await request.formData();
    const title = form.get('title') || '';
    const text = form.get('text') || '';
    const sharedUrl = form.get('url') || '';
    // distintos nombres según el emisor
    let media =
      form.get('media') ||
      form.get('file') ||
      form.get('image') ||
      form.get('files');
    if (!media) {
      for (const [k, v] of form.entries()) {
        if (v && typeof v === 'object' && v.size && String(v.type || '').startsWith('image/')) {
          media = v;
          break;
        }
      }
    }
    let dataUrl = '';
    if (media && typeof media === 'object' && media.size) {
      dataUrl = await blobToDataURL(media);
    }
    await idbSet('pendingShare', {
      title: String(title || ''),
      text: String(text || ''),
      url: String(sharedUrl || ''),
      imageDataUrl: dataUrl,
      ts: Date.now(),
    });
  } catch (e) {
    console.error('share-target', e);
  }
  return Response.redirect('/index.html?shared=1', 303);
}

function blobToDataURL(blob) {
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () => resolve(reader.result);
    reader.onerror = reject;
    reader.readAsDataURL(blob);
  });
}

function idbSet(key, value) {
  return new Promise((resolve, reject) => {
    const open = indexedDB.open('ventasplus-share', 1);
    open.onupgradeneeded = () => {
      const db = open.result;
      if (!db.objectStoreNames.contains('kv')) db.createObjectStore('kv');
    };
    open.onsuccess = () => {
      const db = open.result;
      const tx = db.transaction('kv', 'readwrite');
      tx.objectStore('kv').put(value, key);
      tx.oncomplete = () => resolve();
      tx.onerror = () => reject(tx.error);
    };
    open.onerror = () => reject(open.error);
  });
}
