import { packagedCredentials } from "./packagedCredentials";

// The app keeps a server profile's sign-ins as one secret in its secret
// storage: the active one's, with the others signed in there beside it
// (authSessionStore.ts). Every change of it goes through here, one at a
// time — in the app's other tabs too, where the window has Web Locks — each
// made from what is kept at that moment and written once (2026-11-13: adding
// an account wrote twice without waiting, and the earlier write landing last
// lost the account at the next start).

// How long a change waits for another tab's: holders only read and write the
// secret, so a wait this long is a tab stopped holding it, which doesn't hold
// this one up.
const LOCK_WAIT_MS = 3_000;

const chains = new Map<string, Promise<unknown>>();

// The app's other tabs and windows hear of each change, by the profile's
// name only, and read what is kept again (authSessionStore.ts), as a
// browser's tabs hear of a change of the page's storage.
const NOTICES = "leafen:kept-sign-ins";
let notices: BroadcastChannel | null | undefined;

function noticeChannel(): BroadcastChannel | null {
  notices ??= typeof BroadcastChannel === "function" ? new BroadcastChannel(NOTICES) : null;
  return notices;
}

/** Calls `changed` when another tab or window of the app changes what it keeps for a profile; returns the way to stop. */
export function followKeptSignIn(profile: string, changed: () => void): () => void {
  const channel = noticeChannel();
  if (!channel) return () => undefined;
  const take = (event: MessageEvent<unknown>) => { if (event.data === profile) changed(); };
  channel.addEventListener("message", take);
  return () => channel.removeEventListener("message", take);
}

/**
 * A change of what a profile keeps: `now` is what is kept ("" for nothing),
 * or undefined when it couldn't be read; it answers the value to keep, null
 * to keep nothing, or undefined to leave it as it is. A change that throws
 * leaves it too.
 */
export type KeptSignInChange = (now: string | undefined) => string | null | undefined;

async function acrossTabs<T>(profile: string, work: () => Promise<T>): Promise<T> {
  const locks = typeof navigator === "undefined" ? undefined : navigator.locks;
  if (!locks) return work();
  const signal = typeof AbortSignal.timeout === "function" ? AbortSignal.timeout(LOCK_WAIT_MS) : undefined;
  let started = false;
  const held = () => { started = true; return work(); };
  try {
    return await locks.request(`leafen:kept-sign-in:${profile}`, signal ? { signal } : {}, held);
  } catch (error) {
    if (!started && error instanceof DOMException && (error.name === "TimeoutError" || error.name === "AbortError")) return work();
    throw error;
  }
}

/**
 * Changes what the app keeps for a profile, after the changes asked before
 * it. It answers what is kept after ("" for nothing; undefined when it was
 * left and couldn't be read), or rejects when the app keeps no sign-ins, the
 * change threw, or the write failed.
 */
export function changeKeptSignIn(profile: string, change: KeptSignInChange): Promise<string | undefined> {
  const run = async () => {
    const credentials = await packagedCredentials();
    if (!credentials) throw new Error("This app keeps no logins.");
    return acrossTabs(profile, async () => {
      const now = await credentials.load(profile).then((value) => value, () => undefined);
      const next = change(now);
      if (next === undefined || (next ?? "") === now) return now;
      if (next) await credentials.save(profile, next);
      else await credentials.forget(profile);
      noticeChannel()?.postMessage(profile);
      return next ?? "";
    });
  };
  const result = (chains.get(profile) ?? Promise.resolve()).then(run, run);
  chains.set(profile, result.catch(() => undefined));
  return result;
}
