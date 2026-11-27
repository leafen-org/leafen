// The app opens without the server: installing keeps this build's shell and
// what it starts with (sw-precache.json "bootAssets", the shell only with all
// of them), and once the app has drawn the page asks for the rest of its
// modules ("assets"), kept while the person works. Hashed build files never
// change, so a kept one answers and others are kept as they come. A new build
// is a new worker, which keeps all its files before it takes over, and drops
// the build before once every open window runs the new one.
const BUILD_GENERATION = /*__LEAFEN_SW_BUILD_GENERATION__*/ "dev";
const SHELL = "/";
const CACHE_PREFIX = "leafen-v2-";
const BUILD_CACHE = `${CACHE_PREFIX}build-${BUILD_GENERATION}`;
const FONT_CACHE = `${CACHE_PREFIX}fonts`;
const COVER_CACHE = `${CACHE_PREFIX}covers`;
const PRECACHE_LIST = "/sw-precache.json";
// A build's list, kept in its own cache once every file on it is kept there:
// the page reads it to know its code is on this device (lib/keptBuild.ts).
// No page asks for this address, so it never answers for the server's list.
const KEPT_LIST = `${PRECACHE_LIST}?kept`;
const WARM_OFFLINE_MESSAGE = "leafen:warm-offline-assets";
const TAKE_OVER_MESSAGE = "leafen:take-over";
const START_SETTLED_MESSAGE = "leafen:start-settled";
// How long a window may take to say its start has settled before keeping
// goes on without it: a page settles within ten seconds (lib/startSettled.ts),
// and one of a build before this, which doesn't answer, started long ago.
const START_WAIT_MS = 10_000;
// Files fetched at once while keeping the rest: enough to finish in seconds
// on a near server, few enough to leave the page's own requests room.
const WARM_AT_ONCE = 4;
// Copies between caches here take no connection.
const COPY_AT_ONCE = 8;
const OWNED_FONT_PATH_PREFIX = "/fonts/leafen/";
// The gallery's covers, at versioned addresses pages keep (lib/covers.ts).
const GALLERY_COVER_PATH_PREFIX = "/covers/";
// Pictures the app gives as icons, at versioned addresses too (a new task database's).
const OWNED_ICON_PATH_PREFIX = "/icons/v1/";
const SILENT_SERVER_WAIT_MS = 4000;
const ownedFetches = new Map();
let warming = null;
let warmedAll = false;
// The open windows that said which build they run (by the entry module they
// started from) and whether it is this one.
const windowsOnThisBuild = new Map();

function earlierBuild(key) {
  return key.startsWith(CACHE_PREFIX) && key !== BUILD_CACHE && key !== FONT_CACHE && key !== COVER_CACHE;
}

// Responses vary by Origin for CORS, which a page's module and font requests
// carry and the kept copies don't; these same-origin files are the same
// either way.
const KEPT = { ignoreVary: true };

// Files that never change at their address, the app's own fonts, the
// gallery's covers and its own icon pictures, are kept apart from any build as they are first shown,
// and answer from here after (2026-11-11: offline a page kept for offline
// use showed no gallery cover, as nothing kept one).
async function readOwnedFile(request, cacheName) {
  const cached = await caches.match(request, KEPT);
  if (cached) return cached;

  let pending = ownedFetches.get(request.url);
  if (!pending) {
    pending = fetch(request).then(async (response) => {
      if (response.ok) {
        const cache = await caches.open(cacheName);
        await cache.put(request, response.clone());
      }
      return response;
    }).finally(() => ownedFetches.delete(request.url));
    ownedFetches.set(request.url, pending);
  }
  return (await pending).clone();
}

// The server first; without it (failing, or silent for a few seconds, as on a
// network that stopped without failing), what is kept here, which then says
// what it can't read. With nothing kept, the server's answer in its time.
function serverOrKept(request, kept, answered = (response) => response) {
  const answer = fetch(request).then(answered, async () => (await kept()) ?? Response.error());
  const silent = new Promise((resolve) => { setTimeout(resolve, SILENT_SERVER_WAIT_MS); })
    .then(async () => (await kept()) ?? answer);
  return Promise.race([answer, silent]);
}

// This build's list, or null for a build made without one (it keeps the shell only).
let buildList = null;
async function precacheList() {
  if (BUILD_GENERATION === "dev") return null;
  if (buildList) return buildList;
  const response = await fetch(PRECACHE_LIST, { cache: "no-store" });
  if (!response.ok) throw new Error("The offline file list is unavailable.");
  const list = await response.json();
  // A newer build went up meanwhile: its own worker keeps its files.
  if (list.version !== BUILD_GENERATION) throw new Error("The offline file list belongs to another build.");
  buildList = list;
  return list;
}

// Every module of this build, so that any screen opens without the server:
// what is kept already is skipped, the rest comes a few at a time at low
// priority. Without the server (or a file gone) it stops, and the page asks
// again later; once all is kept, asking costs nothing.
async function warmOfflineAssets() {
  if (warmedAll) return;
  const list = await precacheList().catch(() => null);
  if (list) await keepAllAssets(list);
}

async function keepAllAssets(list) {
  const cache = await caches.open(BUILD_CACHE);
  const kept = new Set((await cache.keys()).map((request) => new URL(request.url).pathname));
  const waiting = await copyKept(cache, list.assets.filter((path) => !kept.has(path)));
  let stopped = false;
  const keepNext = async () => {
    for (let path = waiting.shift(); path !== undefined && !stopped; path = waiting.shift()) {
      const response = await fetchOnceStarted(path, { priority: "low" }).catch(() => null);
      if (!response?.ok) {
        stopped = true;
        return;
      }
      await cache.put(path, response).catch(() => { stopped = true; });
    }
  };
  await Promise.all(Array.from({ length: WARM_AT_ONCE }, keepNext));
  if (stopped) return;
  warmedAll = true;
  await cache.put(KEPT_LIST, new Response(JSON.stringify(list), { headers: { "Content-Type": "application/json" } })).catch(() => {});
}

// A hashed build file a cache here holds, the build before's or this build's
// own as the worker before kept it for a page of this build, is the same
// file: copied, not fetched again. The rest is given back. A new build of
// 419 files, 152 of them changed, fetched all 419 while its first tab
// fetched its own (2026-11-18).
async function copyKept(cache, paths) {
  const rest = [];
  const copyNext = async () => {
    for (let path = paths.shift(); path !== undefined; path = paths.shift()) {
      const copy = path.startsWith("/assets/") ? await caches.match(path, KEPT) : undefined;
      if (!copy || !(await cache.put(path, copy).then(() => true, () => false))) rest.push(path);
    }
  };
  await Promise.all(Array.from({ length: COPY_AT_ONCE }, copyNext));
  return rest;
}

// What this worker fetches waits for the start of every open window, each
// asked once, so the files a page draws with come first: fetched while a new
// build's first tab started, its own modules shared the connections and the
// link, and its page showed seconds later on a NAS (2026-11-18). A window
// answers once its start has settled (lib/serviceWorker.ts); one that doesn't
// is waited for START_WAIT_MS at most.
const startsSettled = new Map();

async function windowsStarted() {
  const windows = await self.clients.matchAll({ type: "window", includeUncontrolled: true });
  await Promise.all(windows.map((client) => {
    let settled = startsSettled.get(client.id);
    if (!settled) {
      settled = new Promise((resolve) => {
        const answer = new MessageChannel();
        const timer = setTimeout(resolve, START_WAIT_MS);
        answer.port1.onmessage = () => {
          clearTimeout(timer);
          resolve();
        };
        client.postMessage({ type: START_SETTLED_MESSAGE }, [answer.port2]);
      });
      startsSettled.set(client.id, settled);
    }
    return settled;
  }));
}

async function fetchOnceStarted(path, init) {
  await windowsStarted();
  return fetch(path, init);
}

// What a build starts with: the hashed files a cache here holds copied, the
// rest fetched together with the shell and root files, so the shell is kept
// only with all of them (keepShell).
async function keepBootAssets(cache, list) {
  const paths = list ? list.bootAssets : [SHELL];
  const hashed = paths.filter((path) => path.startsWith("/assets/"));
  const missing = await copyKept(cache, hashed);
  await windowsStarted();
  await cache.addAll([...missing, ...paths.filter((path) => !path.startsWith("/assets/"))]);
}

// The build before stays while this one doesn't keep all its files or a
// window still runs that build, so a screen it opened before opens without
// the server (2026-11-11: right after a switch the earlier build's files went
// at once, while this one kept only what it starts with for 20 seconds and
// more). A window says which build it runs when it asks for the warm-up.
async function dropEarlierBuilds() {
  if (!warmedAll || !inCharge()) return;
  const windows = await self.clients.matchAll({ type: "window" });
  if (windows.some((client) => windowsOnThisBuild.get(client.id) !== true)) return;
  const keys = await caches.keys();
  if (!inCharge()) return;
  await Promise.all(keys.filter(earlierBuild).map((key) => caches.delete(key)));
}

// Only the worker in charge lets other builds go, and none while a newer one
// is on its way: one being replaced, told by a tab still on its build as the
// server came back, let the new build's files go while the new worker kept
// them, and that one took over without its shell, so a reload without the
// server showed the browser's error page (2026-11-11).
function inCharge() {
  const { active, installing, waiting } = self.registration;
  return !installing && !waiting && (!self.serviceWorker || active === self.serviceWorker);
}

// This build's shell is kept before it takes over, kept again if it went,
// while the server still gives this build.
async function keepShell() {
  if (await caches.match(SHELL, { ...KEPT, cacheName: BUILD_CACHE })) return;
  warmedAll = false;
  const list = await precacheList().catch(() => undefined);
  if (list === undefined) return;
  await (await caches.open(BUILD_CACHE)).addAll(list ? list.bootAssets : [SHELL]).catch(() => {});
}

self.addEventListener("install", (event) => {
  event.waitUntil((async () => {
    const list = await precacheList();
    // The open windows' starts first, then what they fetched is copied.
    await windowsStarted();
    const cache = await caches.open(BUILD_CACHE);
    await keepBootAssets(cache, list);
    // Over an earlier build, all of this one's files are kept before it
    // takes over, as far as the server gives them; a first visit keeps the
    // rest once its page has drawn.
    if (list && (await caches.keys()).some(earlierBuild)) await keepAllAssets(list);
  })());
  self.skipWaiting();
});

self.addEventListener("activate", (event) => {
  event.waitUntil((async () => {
    // Builds before the last one go; the last one goes once nothing needs it.
    const earlier = (await caches.keys()).filter(earlierBuild);
    await Promise.all(earlier.slice(0, -1).map((key) => caches.delete(key)));
    await keepShell();
    await self.clients.claim();
  })());
});

// Unlocked local-lock keys waiting out a reload, in memory only, by tab
// (lib/localLockHold.ts). They go when this worker stops.
const MAX_HELD_LOCK_KEYS = 32;
const heldLockKeys = new Map();

self.addEventListener("message", (event) => {
  const type = event.data && event.data.type;
  if (type === "leafen-local-lock-hold") {
    heldLockKeys.delete(event.data.key);
    heldLockKeys.set(event.data.key, event.data.snapshot);
    if (heldLockKeys.size > MAX_HELD_LOCK_KEYS) heldLockKeys.delete(heldLockKeys.keys().next().value);
    return;
  }
  if (type === "leafen-local-lock-read") {
    event.ports[0]?.postMessage(heldLockKeys.get(event.data.key) ?? null);
    return;
  }
  if (type === "leafen-local-lock-drop") {
    for (const key of [...heldLockKeys.keys()]) if (key.startsWith(event.data.prefix)) heldLockKeys.delete(key);
    return;
  }
  if (type === "leafen-local-lock-keep") return;
  // A tab asks a waiting worker again once the worker in charge runs, so
  // that the browser tells that one to stop at its first quiet moment
  // (lib/serviceWorker.ts).
  if (type === TAKE_OVER_MESSAGE) {
    self.skipWaiting();
    return;
  }
  if (type !== WARM_OFFLINE_MESSAGE) return;
  const told = precacheList().catch(() => null).then((list) => {
    if (event.source?.id) windowsOnThisBuild.set(event.source.id, Boolean(list?.assets.includes(event.data.entry)));
  });
  warming ??= warmOfflineAssets().finally(() => { warming = null; });
  event.waitUntil(Promise.all([told, warming]).then(dropEarlierBuilds));
});

self.addEventListener("fetch", (event) => {
  const requestUrl = new URL(event.request.url);
  if (event.request.method !== "GET" || requestUrl.origin !== self.location.origin || requestUrl.pathname.startsWith("/api/")) return;
  // The git center's pages (/git) are Gitea's, not this app's: never its shell.
  if (requestUrl.pathname === "/git" || requestUrl.pathname.startsWith("/git/")) return;
  if (requestUrl.pathname.startsWith(OWNED_FONT_PATH_PREFIX)) {
    event.respondWith(readOwnedFile(event.request, FONT_CACHE));
    return;
  }
  if (requestUrl.pathname.startsWith(GALLERY_COVER_PATH_PREFIX) || requestUrl.pathname.startsWith(OWNED_ICON_PATH_PREFIX)) {
    event.respondWith(readOwnedFile(event.request, COVER_CACHE));
    return;
  }
  if (event.request.mode === "navigate") {
    const shell = () => caches.match(SHELL, { ...KEPT, cacheName: BUILD_CACHE });
    // A server bringing its data up to date says so (X-Leafen-Updating): its
    // page shows, and opens the app once the update is done.
    event.respondWith(serverOrKept(
      event.request,
      shell,
      async (response) => (response.status >= 500 && !response.headers.has("x-leafen-updating") ? (await shell()) ?? response : response),
    ));
    return;
  }
  if (requestUrl.pathname.startsWith("/assets/")) {
    event.respondWith(caches.match(event.request, KEPT).then((cached) => cached ?? fetch(event.request).then((response) => {
      if (response.ok) {
        const copy = response.clone();
        event.waitUntil(caches.open(BUILD_CACHE).then((cache) => cache.put(event.request, copy)));
      }
      return response;
    })));
    return;
  }
  // Root files that keep their names (icons, the pre-paint script): the
  // server first, the kept copy without it.
  event.respondWith(serverOrKept(event.request, () => caches.match(event.request, KEPT)));
});

// Desktop push: the server sends each unread personal notification once,
// encrypted for this browser. Clicking it opens its page in an open tab or
// a new one.
self.addEventListener("push", (event) => {
  let payload = {};
  try {
    payload = event.data ? event.data.json() : {};
  } catch {
    payload = {};
  }
  const title = typeof payload.title === "string" && payload.title ? payload.title : "Leafen";
  const url = typeof payload.url === "string" && payload.url.startsWith("/") && !payload.url.startsWith("//")
    ? payload.url : "/";
  event.waitUntil(self.registration.showNotification(title, {
    body: typeof payload.body === "string" ? payload.body : "",
    tag: typeof payload.tag === "string" ? payload.tag : undefined,
    icon: "/icon-192.png",
    data: { url },
  }));
});

function openTarget(url) {
  const target = new URL(url || "/", self.location.origin).href;
  return self.clients.matchAll({ type: "window", includeUncontrolled: true }).then((clients) => {
    const open = clients.find((client) => new URL(client.url).origin === self.location.origin);
    if (open) return open.focus().then((client) => client.navigate(target));
    return self.clients.openWindow(target);
  });
}

self.addEventListener("notificationclick", (event) => {
  event.notification.close();
  event.waitUntil(openTarget(event.notification.data?.url));
});
