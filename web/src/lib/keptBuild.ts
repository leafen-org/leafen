// Whether this device keeps the build the page runs: its offline worker
// (public/sw.js) keeps a build's list beside its files once every one of them
// is kept. A worker in charge isn't enough: right after the server moves to a
// new build, a page of the new build starts under the worker of the build
// before, which keeps none of the new build's changed files.

const BUILD_CACHE_PREFIX = "leafen-v2-build-";
const KEPT_LIST = "/sw-precache.json?kept";

/** The build's entry module the page started from, as the build's list names it. */
export function entryPath(): string | null {
  const entry = document.querySelector<HTMLScriptElement>('script[type="module"][src]');
  return entry ? new URL(entry.src, window.location.href).pathname : null;
}

let kept: Promise<boolean> | undefined;

/** Whether every file of the page's build is kept on this device (asked once a page). */
export function buildKeptHere(): Promise<boolean> {
  kept ??= readBuildKept().catch(() => false);
  return kept;
}

async function readBuildKept(): Promise<boolean> {
  const entry = entryPath();
  if (!entry || typeof caches === "undefined" || !navigator.serviceWorker?.controller) return false;
  for (const name of await caches.keys()) {
    if (!name.startsWith(BUILD_CACHE_PREFIX)) continue;
    const list = await (await caches.open(name)).match(KEPT_LIST);
    if (!list) continue;
    const assets = (await list.json() as { assets?: unknown }).assets;
    if (Array.isArray(assets) && assets.includes(entry)) return true;
  }
  return false;
}
