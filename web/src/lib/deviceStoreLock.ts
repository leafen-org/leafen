// A person's store on this device — the changes kept for the server (the
// outbox) and the copies kept to open without it (the record cache) — is
// used by one tab at a time, under one Web Lock. A tab waits for it only so
// long: another tab left holding it (2026-10-16: a tab open across a server
// upgrade held it, and a new tab's first screen waited on it forever) then
// doesn't hold this one up. Having waited once, a tab's next steps ask only
// briefly until they have the lock again, so a stopped tab doesn't cost every
// step the same wait (2026-11-05: five seconds each held a page's body and
// what was typed on it). The step fails as an unavailable store does — the
// server's answer is read without the kept copy, and a change goes straight
// to the server — and the next step asks for the lock again.
//
// The wait is for another tab only (giveUpPastOthers): the time this tab's
// own steps hold the store, and the time its own thread was too busy to look,
// don't count against it. A step of this tab waits as long as the steps
// before it here take; a change is never dropped because this tab was still
// keeping a large copy (2026-11-19: offline, a 100 x 300 table pasted drew on
// screen, the page's copy held the store four to ten seconds, the change gave
// up its keep after one, went to the unreachable server and was gone once
// back).
//
// The store's database is waited for so too: a tab stopped (in the debugger,
// frozen or busy) in the middle of one of its transactions holds the database
// though not the lock, and every other tab's changes waited on it as long
// (2026-11-08: over 40 seconds, none of them sent meanwhile). A transaction
// that started is this tab's own work, however long it takes.
//
// The lane that sends what the outbox keeps is one tab's at a time as well,
// but one that keeps it past the wait has it taken over (2026-11-08: a
// stopped tab held it, and another tab's kept changes waited behind it while
// its new ones went past them out of order). The tab taking it over sends
// what the outbox keeps in its order; the one it was taken from hears so and
// stops before its next change, and a change both sent the server takes once,
// by its mutation.

const DEVICE_STORE_LOCK_WAIT_MS = 1_000;
const HELD_LOCK_WAIT_MS = 100;
const HELD_LOCK_REMEMBERED_MS = 10_000;
const DATABASE_WAIT_MS = 1_000;
const HELD_DATABASE_WAIT_MS = 300;
const LANE_TAKEOVER_MS = 2_000;
const heldUntil = new Map<string, number>();
const databaseHeldUntil = new Map<string, number>();
// What of the store this tab's own steps hold now — the lock, the lane, the
// database in a transaction — and when they last let each go.
const holdingHere = new Map<string, number>();
const letGoHereAt = new Map<string, number>();

export interface DeviceStoreLockManager {
  request<T>(
    name: string,
    options: { mode: "exclusive"; signal?: AbortSignal; steal?: boolean },
    callback: () => Promise<T>,
  ): Promise<T>;
}

function waitExceeded(): DOMException {
  return new DOMException("Another tab kept this device's store past the wait.", "TimeoutError");
}

function holdHere(key: string): void {
  holdingHere.set(key, (holdingHere.get(key) ?? 0) + 1);
}

function letGoHere(key: string): void {
  const left = (holdingHere.get(key) ?? 1) - 1;
  if (left > 0) holdingHere.set(key, left);
  else holdingHere.delete(key);
  letGoHereAt.set(key, Date.now());
}

/**
 * Calls `giveUp` once `wait` passed with `key` held by another tab only.
 * While this tab holds it the wait goes on; once this tab let it go, what
 * holds it next has the whole wait from then; and a look this tab's busy
 * thread made late (a visible page's timer firing past half the wait after
 * it was due) looks again a wait later, as an answer that came meanwhile
 * may be waiting behind it. Returns what ends the watch.
 */
function giveUpPastOthers(key: string, wait: number, giveUp: () => void): () => void {
  let timer: ReturnType<typeof setTimeout> | undefined;
  let ended = false;
  const lookIn = (after: number) => {
    const due = Date.now() + after;
    timer = setTimeout(() => {
      if (ended) return;
      const now = Date.now();
      const sinceLetGo = now - (letGoHereAt.get(key) ?? Number.NEGATIVE_INFINITY);
      if (holdingHere.has(key)) lookIn(wait);
      else if (sinceLetGo < wait) lookIn(wait - sinceLetGo);
      else if (now - due > wait / 2 && globalThis.document?.visibilityState !== "hidden") lookIn(wait);
      else {
        ended = true;
        giveUp();
      }
    }, after);
  };
  lookIn(wait);
  return () => {
    ended = true;
    clearTimeout(timer);
  };
}

/** A signal another tab's holding `key` past the wait aborts (giveUpPastOthers). */
function abortedPastOthers(key: string, wait: number): { signal?: AbortSignal; stop: () => void } {
  if (typeof AbortController !== "function") return { stop: () => undefined };
  const waiting = new AbortController();
  return { signal: waiting.signal, stop: giveUpPastOthers(key, wait, () => waiting.abort(waitExceeded())) };
}

/**
 * Runs `work` holding the store's lock, once it's had within the wait — longer
 * for a step a person asked for (the passphrase lock's custody).
 */
export async function requestDeviceStoreLock<T>(
  locks: DeviceStoreLockManager,
  name: string,
  work: () => Promise<T>,
  waitMs?: number,
): Promise<T> {
  const wait = waitMs ?? (Date.now() < (heldUntil.get(name) ?? 0) ? HELD_LOCK_WAIT_MS : DEVICE_STORE_LOCK_WAIT_MS);
  const { signal, stop } = abortedPastOthers(name, wait);
  try {
    return await locks.request(name, signal ? { mode: "exclusive", signal } : { mode: "exclusive" }, async () => {
      stop();
      heldUntil.delete(name);
      holdHere(name);
      try {
        return await work();
      } finally {
        letGoHere(name);
      }
    });
  } catch (error) {
    if (isDeviceStoreLockWaitExceeded(error)) heldUntil.set(name, Date.now() + HELD_LOCK_REMEMBERED_MS);
    throw error;
  } finally {
    stop();
  }
}

/**
 * Runs `work` holding the outbox's sending lane, taking it over from another
 * tab that kept it past the wait; this tab's own work keeping it, which goes
 * on as this tab runs, is waited for. `taken` tells the work that another tab
 * took the lane over meanwhile: it ends before its next change, and what it
 * did stands.
 */
export async function requestDeviceStoreLane<T>(
  locks: DeviceStoreLockManager,
  name: string,
  work: (taken: AbortSignal) => Promise<T>,
): Promise<T> {
  const taken = new AbortController();
  let working: Promise<T> | null = null;
  const hold = async (options: { mode: "exclusive"; signal?: AbortSignal; steal?: boolean }) => {
    try {
      return await locks.request(name, options, () => {
        holdHere(name);
        return working = work(taken.signal);
      });
    } catch (error) {
      if (!working) throw error;
      // Taken over while working (or the work failed): it ends on its own.
      taken.abort(error);
      return working;
    } finally {
      if (working) letGoHere(name);
    }
  };
  for (;;) {
    const { signal, stop } = abortedPastOthers(name, LANE_TAKEOVER_MS);
    if (!signal) return hold({ mode: "exclusive" });
    try {
      return await hold({ mode: "exclusive", signal });
    } catch (error) {
      // Only a wait for the lane goes on here; the work's own failure is the step's.
      if (working || !isDeviceStoreLockWaitExceeded(error)) throw error;
    } finally {
      stop();
    }
    if (!holdingHere.has(name)) return hold({ mode: "exclusive", steal: true });
  }
}

/** How long one of the store's database transactions may wait for the database before it is given up. */
export function deviceStoreDatabaseWaitMs(name: string): number {
  return Date.now() < (databaseHeldUntil.get(name) ?? 0) ? HELD_DATABASE_WAIT_MS : DATABASE_WAIT_MS;
}

/**
 * Watches a transaction on the store's database `name`, which `giveUp` gives
 * up once it waited past the wait for the database another tab holds. Once
 * one of its requests is answered it started: it is this tab's own work from
 * then, however long it takes, and this tab's others wait for it as for this
 * tab's hold of the lock (giveUpPastOthers). Returns what to call once the
 * transaction ended.
 */
export function watchDeviceStoreTransaction(name: string, transaction: IDBTransaction, giveUp: () => void): () => void {
  const key = `database:${name}`;
  const stop = giveUpPastOthers(key, deviceStoreDatabaseWaitMs(name), giveUp);
  let started = false;
  const start = () => {
    if (started) return;
    started = true;
    stop();
    holdHere(key);
  };
  // A request's answer goes through its transaction on its way to it.
  const listens = typeof transaction.addEventListener === "function";
  if (listens) {
    transaction.addEventListener("success", start, { capture: true });
    transaction.addEventListener("error", start, { capture: true });
  }
  return () => {
    stop();
    if (listens) {
      transaction.removeEventListener("success", start, { capture: true });
      transaction.removeEventListener("error", start, { capture: true });
    }
    if (started) letGoHere(key);
  };
}

/**
 * Notes a transaction given up past the wait, so the next ones wait only
 * briefly, and returns the error its step fails with: the one a step that
 * waited for the lock past the wait fails with.
 */
export function deviceStoreDatabaseKept(name: string): DOMException {
  databaseHeldUntil.set(name, Date.now() + HELD_LOCK_REMEMBERED_MS);
  return waitExceeded();
}

/** Notes a transaction that ended within the wait: the database is free again. */
export function deviceStoreDatabaseFree(name: string): void {
  databaseHeldUntil.delete(name);
}

/** Whether a step failed because another tab kept the store's lock or database past the wait. */
export function isDeviceStoreLockWaitExceeded(error: unknown): boolean {
  return error instanceof DOMException && error.name === "TimeoutError";
}
