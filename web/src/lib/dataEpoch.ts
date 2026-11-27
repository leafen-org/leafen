import { serverPath, serverScopedName } from "@/lib/serverOrigin";

// Which history of the server's data this device's copies are from. A
// restore, or taking one back, gives the server's data a new epoch
// (data_epoch.go): a device whose copies are from before would carry that
// history back in with its writes and live edits, so the server refuses
// them, and the device keeps its copies aside, unsent, until its person
// lets them go and takes the server's anew. Each account keeps its own, as
// each keeps its own copies.

const HEALTH_PATH = "/api/leafen/v1/health";
// A live connection that keeps failing asks the server's epoch at most
// this often.
const CHECK_INTERVAL_MS = 10_000;

type Listener = (epoch: string | null) => void;

const listeners = new Set<Listener>();
const known = new Map<string, string | null>();
let restored = false;
let restoredEpoch: string | null = null;
let lastCheck = 0;

function storageKey(userId: string): string {
  return serverScopedName(`leafen:v2:data-epoch:${userId}`);
}

// Let go in one tab, this device's copies are gone for all of them: the others
// start anew too, rather than each asking again (2026-11-05).
if (typeof window !== "undefined") {
  window.addEventListener("storage", ({ key, newValue }) => {
    if (!restored || !newValue || !key?.includes("leafen:v2:data-epoch:")) return;
    if (restoredEpoch !== null && newValue !== restoredEpoch) return;
    window.setTimeout(() => window.location.reload(), Math.random() * 3000);
  });
}

/**
 * The epoch an account's copies here are from, as its writes and live
 * connections name it — kept even once the server is known to have moved
 * on, so the server goes on refusing them until the copies are let go.
 */
export function dataEpochFor(userId: string): string | null {
  if (!known.has(userId)) {
    let stored: string | null = null;
    try {
      stored = window.localStorage.getItem(storageKey(userId));
    } catch {
      stored = null;
    }
    known.set(userId, stored);
  }
  return known.get(userId) ?? null;
}

/** Keeps the epoch an account's copies here are now from. */
export function rememberDataEpoch(userId: string, epoch: string): void {
  try {
    window.localStorage.setItem(storageKey(userId), epoch);
  } catch {
    // A device that can't keep it learns it again at the next start.
  }
  known.set(userId, epoch);
  restored = false;
  restoredEpoch = null;
}

/** Whether the server is known to be in another history than the copies. */
export function serverWasRestored(): boolean {
  return restored;
}

/** The server's epoch, once a restore was noticed and it was told. */
export function restoredServerEpoch(): string | null {
  return restoredEpoch;
}

/** Tells the listeners that the server's data is in another history. */
export function reportServerRestored(epoch: string | null): void {
  const changed = !restored || (epoch !== null && epoch !== restoredEpoch);
  restored = true;
  if (epoch !== null) restoredEpoch = epoch;
  if (!changed) return;
  for (const listener of listeners) listener(restoredEpoch);
}

/**
 * Brings back the question of what to do with the copies, once its notice
 * was put off ("나중에"), as the paused notice offers it.
 */
export function askAboutRestoredServer(): void {
  if (!restored) return;
  for (const listener of listeners) listener(restoredEpoch);
}

// Whether the question about the copies is on screen: the paused notice
// waits behind it, so the same thing isn't said twice (2026-11-06), and
// shows once it is put off.
let questionShown = false;
const questionListeners = new Set<(shown: boolean) => void>();

export function setRestoredQuestionShown(shown: boolean): void {
  if (questionShown === shown) return;
  questionShown = shown;
  for (const listener of questionListeners) listener(shown);
}

export function restoredQuestionShown(): boolean {
  return questionShown;
}

export function onRestoredQuestionShown(listener: (shown: boolean) => void): () => void {
  questionListeners.add(listener);
  return () => {
    questionListeners.delete(listener);
  };
}

export function onServerRestored(listener: Listener): () => void {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
}

/**
 * What the server's start of a session says its epoch is: the first one an
 * account sees on this device is kept, another than the kept one means
 * the server was restored.
 */
export function observeServerDataEpoch(userId: string, epoch: string | undefined): void {
  if (!epoch) return;
  const kept = dataEpochFor(userId);
  if (!kept) {
    rememberDataEpoch(userId, epoch);
    return;
  }
  if (kept !== epoch) reportServerRestored(epoch);
}

// The window that had the server restored (or the restore undone) notes the
// epoch its copies are from and the one the server came back with, as the
// server answers again; on opening afresh there it takes the server's new
// data without asking, when this device holds nothing the server never got
// (ServerRestoredDialog). Any other window, or device, asks.
const RESTORE_CHOSEN_LASTS_MS = 10 * 60 * 1000;

function restoreChosenKey(userId: string): string {
  return serverScopedName(`leafen:v2:restore-chosen:${userId}`);
}

/** Notes that this window had the server restored, back as epoch now. */
export function noteRestoreChosenHere(userId: string, epoch: string | null): void {
  const from = dataEpochFor(userId);
  try {
    if (!from || !epoch || epoch === from) {
      window.localStorage.removeItem(restoreChosenKey(userId));
      return;
    }
    window.localStorage.setItem(restoreChosenKey(userId), JSON.stringify({ from, to: epoch, at: Date.now() }));
  } catch {
    // A window that can't note it asks, as any other.
  }
}

const restoreChosenTaken = new Set<string>();

/**
 * Whether this window had the server restored, from the copies this device
 * holds, to the epoch it is known to be in now (or not known yet). The note
 * is let go once read; what it said holds for the rest of this page's life.
 */
export function takeRestoreChosenHere(userId: string): boolean {
  if (restoreChosenTaken.has(userId)) return true;
  type Noted = { from?: unknown; to?: unknown; at?: unknown } | null;
  const noted = ((): Noted => {
    try {
      const raw = window.localStorage.getItem(restoreChosenKey(userId));
      if (raw === null) return null;
      window.localStorage.removeItem(restoreChosenKey(userId));
      return JSON.parse(raw) as Noted;
    } catch {
      return null;
    }
  })();
  // A refused write may tell of the restore before the server's epoch is
  // known here: the note's own epoch then stands for it.
  const chosen = noted?.from === dataEpochFor(userId) && (restoredEpoch === null || noted?.to === restoredEpoch)
    && typeof noted?.to === "string" && typeof noted?.at === "number" && Date.now() - noted.at < RESTORE_CHOSEN_LASTS_MS;
  if (chosen) restoreChosenTaken.add(userId);
  return chosen;
}

/** The server's epoch as its health answer tells it, or null. */
export async function readServerDataEpoch(signal?: AbortSignal): Promise<string | null> {
  const response = await fetch(serverPath(HEALTH_PATH), { cache: "no-store", signal });
  if (!response.ok) return null;
  const body = await response.json().catch(() => null) as { dataEpoch?: unknown } | null;
  return typeof body?.dataEpoch === "string" && body.dataEpoch ? body.dataEpoch : null;
}

/**
 * Asks the server's epoch when an account's live connection fails, as a
 * refused one tells nothing else: a server restored meanwhile refuses it
 * for good.
 */
export function checkServerDataEpochSoon(userId: string): void {
  const now = Date.now();
  const expected = dataEpochFor(userId);
  if (!expected || restored || now - lastCheck < CHECK_INTERVAL_MS) return;
  lastCheck = now;
  void readServerDataEpoch().then((epoch) => {
    if (epoch && dataEpochFor(userId) === expected && epoch !== expected) reportServerRestored(epoch);
  }).catch(() => undefined);
}
