import type { LocalSecretBoxSessionSnapshot } from "./localSecretBox";
import { randomUUID } from "./browserCrypto";

// Where an unlocked key waits out a reload: the service worker's memory, kept
// alive while the tab is open, under the tab's own name. It never reaches
// disk, as a tab's history does in the browser's session files (2026-11-05:
// the key came back after the browser was closed and opened), so closing the
// browser or the last tab locks again, a new tab asks, and a reload doesn't.
// Where no service worker runs, a reload asks.

const TAB_KEY = "leafen.local-lock.tab";
const HOLD = "leafen-local-lock-hold";
const READ = "leafen-local-lock-read";
const DROP = "leafen-local-lock-drop";
const KEEP = "leafen-local-lock-keep";
// Sooner than a service worker with nothing to do is stopped.
const KEEP_EVERY_MS = 20_000;
const READ_LIMIT_MS = 400;

let keeper = 0;
// Counted at each drop, so a hold still waiting for the worker gives up once
// the keys were dropped meanwhile.
let drops = 0;

function tabToken(): string {
  try {
    let token = window.sessionStorage.getItem(TAB_KEY);
    if (!token) {
      // Through the one random-ID boundary (browserCrypto.ts): plain http
      // addresses (a NAS, a second device on the network) have no browser one.
      token = randomUUID();
      window.sessionStorage.setItem(TAB_KEY, token);
    }
    return token;
  } catch {
    return "";
  }
}

async function activeWorker(): Promise<ServiceWorker | null> {
  try {
    const registration = await navigator.serviceWorker?.getRegistration("/");
    return registration?.active ?? null;
  } catch {
    return null;
  }
}

// A key unlocked before the page's worker first runs — a first visit, whose
// worker registers once its start has settled (serviceWorker.ts) — waits for
// it: read then as no worker here, it was never held, and the reload after
// asked again although the worker ran by then (2026-11-18).
export function holdUnlockedKey(userId: string, snapshot: LocalSecretBoxSessionSnapshot): void {
  const token = tabToken();
  const workers = typeof navigator === "undefined" ? undefined : navigator.serviceWorker;
  if (!token || !workers) return;
  const since = drops;
  void workers.ready.then((registration) => {
    const worker = registration.active;
    if (!worker || drops !== since) return;
    worker.postMessage({ type: HOLD, key: `${token}:${userId}`, snapshot });
    keeper ||= window.setInterval(() => void activeWorker().then((active) => active?.postMessage({ type: KEEP })), KEEP_EVERY_MS);
  });
}

export function dropUnlockedKeys(): void {
  drops += 1;
  window.clearInterval(keeper);
  keeper = 0;
  const token = tabToken();
  if (!token) return;
  void activeWorker().then((worker) => worker?.postMessage({ type: DROP, prefix: `${token}:` }));
}

export async function readHeldKey(userId: string): Promise<unknown> {
  const token = tabToken();
  const worker = token ? await activeWorker() : null;
  if (!worker) return null;
  return new Promise((resolve) => {
    const channel = new MessageChannel();
    const timer = window.setTimeout(() => resolve(null), READ_LIMIT_MS);
    channel.port1.onmessage = (event) => {
      window.clearTimeout(timer);
      resolve(event.data ?? null);
    };
    worker.postMessage({ type: READ, key: `${token}:${userId}` }, [channel.port2]);
  });
}
