// The offline worker of a new build (public/sw.js) and when it is in charge
// of the pages it serves.

const TAKE_OVER_MESSAGE = "leafen:take-over";

// A new worker takes over (skipWaiting in sw.js) once the one in charge has
// nothing in hand. Chrome tells the one in charge to stop at its first quiet
// moment only if it was running when the new one finished installing; one
// that was just starting, as when the tabs' requests woke it as the server
// came back, stops only after half a minute without a request, which an open
// tab never gives it, and the new worker waited with the old build in charge
// for up to five minutes (2026-11-11). Asked again right after the one in
// charge answered this tab, so that it runs, the new one takes over at its
// first quiet moment.
export function hurryWaitingWorker(registration: ServiceWorkerRegistration) {
  if (!registration.waiting || !navigator.serviceWorker.controller) return;
  void fetch("/sw-precache.json", { cache: "no-store" })
    .catch(() => null)
    .finally(() => registration.waiting?.postMessage({ type: TAKE_OVER_MESSAGE }));
}

/**
 * Whether the page's next load comes from the newest build's worker: none is
 * installing or waiting behind the one in charge, after asking the server for
 * its worker when `ask`. One on its way was hurried as it installed
 * (serviceWorker.ts); asked again on every look, the requests that wake the
 * one in charge kept it from its quiet moment. A page without a worker loads
 * from the server, whose build is the newest.
 */
export async function newestWorkerInCharge(ask: boolean): Promise<boolean> {
  const workers = typeof navigator === "undefined" ? undefined : navigator.serviceWorker;
  if (!workers?.controller) return true;
  const registration = await workers.getRegistration().catch(() => undefined);
  if (!registration) return true;
  if (ask) await registration.update().catch(() => undefined);
  return !registration.installing && !registration.waiting;
}
