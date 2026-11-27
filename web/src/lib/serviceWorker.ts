"use client";

// Registers the offline service worker (public/sw.js). Production bundles
// only — the Vite dev server has its own module graph and a worker there
// would only confuse HMR. Kill switch: localStorage
// "leafen.sw.disabled" = "1" unregisters and stops re-registering.

import { reloadKeepingPlace } from "@/lib/reloadKeepingPlace";
import { i18next } from "@/i18n";
import { onAppInteractiveForOfflineWarm } from "@/lib/appInteractive";
import { reloadWhenAtRest } from "@/lib/reloadAtRest";
import { logProductError } from "@/lib/safeErrorLog";
import { onServerAnswersAgain } from "@/lib/serverAnswers";
import { entryPath } from "@/lib/keptBuild";
import { afterStart, whenStartSettled } from "@/lib/startSettled";
import { updateNoticeWordsReady } from "@/lib/updateReadyNotice";
import { hurryWaitingWorker } from "@/lib/workerTakeOver";

const DISABLE_KEY = "leafen.sw.disabled";
const WARM_OFFLINE_MESSAGE = "leafen:warm-offline-assets";
const START_SETTLED_MESSAGE = "leafen:start-settled";
const BUILD_CHECK_MS = 60_000;
let registeredWorker: ServiceWorkerRegistration | undefined;
const trackedWorkers = new WeakSet<ServiceWorker>();
let offlineWarmRequested = false;
let offlineWarmScheduled = false;

function swDisabled(): boolean {
  try {
    return window.localStorage.getItem(DISABLE_KEY) === "1";
  } catch {
    return false;
  }
}

// The worker self-activates (skipWaiting/clients.claim in sw.js), but an open
// tab keeps running the OLD bundle — and the new precache may no longer hold
// the old hashed chunks it lazy-loads. Tell the user instead of leaving them
// on a stale (and potentially chunk-404ing) UI until a manual reload.
export type ServiceWorkerUpdateReadyNotifier = (input: {
  message: string;
  reloadLabel: string;
  reload: () => void;
}) => void;

function notifyUpdateReady(notify: ServiceWorkerUpdateReadyNotifier) {
  // Its words come once i18next holds them (updateNoticeWordsReady).
  void updateNoticeWordsReady().then(() => notify({
    message: i18next.t("serviceWorker:updateReady"),
    reloadLabel: i18next.t("serviceWorker:reload"),
    reload: reloadKeepingPlace,
  }));
  // A tab behind others moves over by itself; one in front stays with its
  // notice until the person takes it or leaves the tab.
  reloadWhenAtRest({ shown: false, why: "new-build" });
}

// A new build shows only to one who asks (2026-11-05: tabs left open knew
// nothing until a new tab opened): when a tab shows again, each minute it
// shows, and when the server answers after being away, as it is after an
// upgrade.
function whenBuildMayHaveChanged(check: () => void) {
  const checkShown = () => {
    if (document.visibilityState === "visible") check();
  };
  document.addEventListener("visibilitychange", checkShown);
  window.setInterval(checkShown, BUILD_CHECK_MS);
  onServerAnswersAgain(check);
}

// Without a worker, as at a plain http address where a browser gives none,
// the build's file list says the same.
function watchBuildWithoutWorker(notify: ServiceWorkerUpdateReadyNotifier) {
  let known: unknown;
  let told = false;
  const check = () => {
    if (told) return;
    void fetch("/sw-precache.json", { cache: "no-store" })
      .then((response) => response.ok ? response.json() as Promise<{ version?: unknown }> : null)
      .then((list) => {
        if (typeof list?.version !== "string" || told) return;
        known ??= list.version;
        if (list.version === known) return;
        told = true;
        notifyUpdateReady(notify);
      })
      .catch(() => {});
  };
  check();
  whenBuildMayHaveChanged(check);
}

// Pages come from the server first, so a tab opened after the server moved
// to a new build already runs it while the worker it met is the old one: the
// new worker's install is no news to that tab (2026-11-05, a first visit was
// told of a new version). The build's file list names the tab's own entry.
async function tabRunsServedBuild(): Promise<boolean> {
  const path = entryPath();
  if (!path) return false;
  try {
    const response = await fetch("/sw-precache.json", { cache: "no-store" });
    if (!response.ok) return false;
    const list = await response.json() as { assets?: unknown };
    return Array.isArray(list.assets) && list.assets.includes(path);
  } catch {
    return false;
  }
}

// A worker that stops before it has installed, as when the server went away
// while it kept the build's files, is said in the diagnostic log; the page
// registers again when it shows or the server answers (registerWorker).
function watchForUpdates(
  registration: ServiceWorkerRegistration,
  notify: ServiceWorkerUpdateReadyNotifier,
) {
  const track = (worker: ServiceWorker | null) => {
    // A first install is the registration's worker as it resolves and the
    // one its "updatefound" names after: watched once.
    if (!worker || trackedWorkers.has(worker)) return;
    trackedWorkers.add(worker);
    let installed = false;
    worker.addEventListener("statechange", () => {
      if (worker.state === "redundant" && !installed) {
        logProductError("warn", "service-worker.install", new Error("The offline worker stopped before it installed."), { errorCode: "install_failed" });
      }
      if (worker.state !== "installed") return;
      installed = true;
      // "installed" with an existing controller = an UPDATE finished installing
      // (first-ever install has no controller and needs no prompt).
      if (navigator.serviceWorker.controller) {
        hurryWaitingWorker(registration);
        void tabRunsServedBuild().then((current) => { if (!current) notifyUpdateReady(notify); });
      }
    });
  };
  hurryWaitingWorker(registration);
  // A page that started with a new worker already waiting may run the build
  // before it, from the worker still in charge (a reload of the person's own
  // the server was slow to answer): it learns of the new build as one that
  // saw it install does.
  if (registration.waiting && navigator.serviceWorker.controller) {
    void tabRunsServedBuild().then((current) => { if (!current) notifyUpdateReady(notify); });
  }
  track(registration.installing);
  registration.addEventListener("updatefound", () => track(registration.installing));
}

// The worker keeps every screen's code once the app has drawn and its start
// has settled (startSettled.ts), while the person works: waiting for ten
// quiet seconds first, and again after each key or click, a person who wrote
// on and then lost the connection had only the screens already opened, and
// another page or settings opened offline showed only "화면을 불러오는 데
// 시간이 걸리고 있습니다" (2026-11-11); begun as the app drew, its four files
// at a time took the connections the page's first reads went out on
// (2026-11-13). The worker fetches a few files at a time, at low priority,
// and skips what it keeps; one that stopped without the server goes on when
// the page asks again — on the next page, a new worker, or the server
// answering again. The tab says which build it runs, so the worker keeps the
// build before for a tab still on it (public/sw.js), and says so again each
// time it shows.
function scheduleOfflineWarm() {
  if (!offlineWarmRequested || !registeredWorker || offlineWarmScheduled) return;
  offlineWarmScheduled = true;
  const run = () => {
    offlineWarmScheduled = false;
    void navigator.serviceWorker.ready
      .then((readyRegistration) => readyRegistration.active?.postMessage({ type: WARM_OFFLINE_MESSAGE, entry: entryPath() }))
      .catch(() => {});
  };
  afterStart(run);
}

function requestOfflineWarm() {
  offlineWarmRequested = true;
  scheduleOfflineWarm();
}

// The worker registers once the page's start has settled: by then the page
// holds the code and styles the worker keeps first. Registered as the page
// loaded, its script, its list and the files it starts with that the page
// hadn't (the shell, English, the manifest) went out while the start's reads
// waited for a connection (2026-11-13). A registration that fails, or a
// worker that stops before it has installed (the server gone while it kept
// the build's files), is said in the diagnostic log and tried again when the
// page shows again or the server answers again, not only as a page loads: a
// web app on a phone's home screen stays open for days. One that has not
// answered in a minute is said too: a home screen web app on iOS can leave a
// registration unanswered until the app is closed and opened again, every
// later one waiting behind it, the page's reloads too (2026-11-12, the server
// gone part way: three minutes and more, then a worker 19 seconds after the
// app was reopened).
const REGISTRATION_SILENT_MS = 60_000;
let registering = false;

function registerWorker(notify: ServiceWorkerUpdateReadyNotifier) {
  const current = registeredWorker;
  if (registering || current?.active || current?.installing || current?.waiting) return;
  registering = true;
  const silent = globalThis.setTimeout(() => {
    logProductError("warn", "service-worker.register", new Error("The offline worker's registration has not answered."), { errorCode: "unanswered" });
  }, REGISTRATION_SILENT_MS);
  navigator.serviceWorker
    .register("/sw.js")
    .then((registration) => {
      if (registration !== registeredWorker) {
        registeredWorker = registration;
        watchForUpdates(registration, notify);
      }
      scheduleOfflineWarm();
    }, (error: unknown) => {
      logProductError("warn", "service-worker.register", error, { errorCode: error instanceof Error ? error.name : undefined });
    })
    .finally(() => {
      globalThis.clearTimeout(silent);
      registering = false;
    });
}

export function registerServiceWorker(notify: ServiceWorkerUpdateReadyNotifier) {
  if (typeof window === "undefined") return;
  if (import.meta.env.DEV) return;
  if (!("serviceWorker" in navigator)) {
    watchBuildWithoutWorker(notify);
    return;
  }
  // A worker about to fetch asks each open window first, so that a page's
  // start has its connections to itself (public/sw.js windowsStarted): the
  // page answers once its start has settled.
  navigator.serviceWorker.addEventListener("message", (event: MessageEvent) => {
    const answer = event.ports[0];
    if ((event.data as { type?: unknown } | null)?.type !== START_SETTLED_MESSAGE || !answer) return;
    whenStartSettled(() => answer.postMessage(true));
  });
  if (swDisabled()) {
    void navigator.serviceWorker
      .getRegistration()
      .then((registration) => registration?.unregister())
      .catch(() => {});
    return;
  }
  const register = () => registerWorker(notify);
  onAppInteractiveForOfflineWarm(requestOfflineWarm);
  onServerAnswersAgain(() => {
    register();
    scheduleOfflineWarm();
  });
  document.addEventListener("visibilitychange", () => {
    if (document.visibilityState !== "visible") return;
    register();
    scheduleOfflineWarm();
  });
  navigator.serviceWorker.addEventListener("controllerchange", scheduleOfflineWarm);
  afterStart(register);
  whenBuildMayHaveChanged(() => void registeredWorker?.update().catch(() => {}));
}
