import { isPackagedClient } from "./packagedClient";
import { forgetProfile, listProfiles, profileAddress, serverProfileId, validListKey } from "./profileRegistry";
import { serverIdentity, serverOrigin } from "./serverOrigin";

// Signing out of another server's account from a page a server serves (the
// browser sign-out decision of 2026-10-30, answer A). This page goes to that
// server's page with the list key that server gave it; there the request is
// taken as real only when the key still reads that server's list and the
// asking server is one it knows, its own sign-in ends as its menu's
// "로그아웃" ends it, and it comes back here, which drops it from the menu.
// "모든 계정에서 로그아웃" visits each in turn, passes over one it cannot
// reach, and signs out here last. The key still reads lists and nothing more
//: it rides in the address's fragment and goes to no server but in
// its own list's request.

const OUTGOING = "leafen:v2:remote-sign-out";
const INCOMING = "leafen:v2:remote-sign-out-request";
const HERE = "leafen:v2:sign-out-here";
const REQUEST_PATH = "/sign-out-request";
const RETURN_PATH = "/sign-out-request/return";
const REACH_MS = 5_000;

/** Fired when this page should sign out, its other accounts done. */
export const SIGN_OUT_HERE_EVENT = "leafen:sign-out-here";

type Outgoing = { state: string; current?: string; queue: string[]; here: boolean; returnPath: string };
type Incoming = { from: string; state: string; key: string };

function store(): Storage | null {
  try {
    return globalThis.sessionStorage ?? null;
  } catch {
    return null;
  }
}

function read<T>(name: string): T | null {
  try {
    return JSON.parse(store()?.getItem(name) ?? "null") as T | null;
  } catch {
    return null;
  }
}

function listUrl(server: string): URL {
  return new URL("/api/leafen/v1/switcher/list", server);
}

function randomState(): string {
  return Array.from(crypto.getRandomValues(new Uint8Array(18)), (byte) => byte.toString(16).padStart(2, "0")).join("");
}

/**
 * What this page is to sign out of as it comes up: the account it opens
 * ("one", another account on this server signing out through its own page,
 * lib/serverAccounts.ts) or every account on this server, one after another ("all").
 */
export type SignOutHere = "one" | "all";

export function markSignOutHere(which: SignOutHere): void {
  store()?.setItem(HERE, which);
}

/** Asks this page to sign out of every account here once it is up, or now if it is. */
export function signOutHere(): void {
  markSignOutHere("all");
  window.dispatchEvent(new Event(SIGN_OUT_HERE_EVENT));
}

/** What this page was asked to sign out of; asking once. */
export function takeSignOutHere(): SignOutHere | "" {
  const asked = store()?.getItem(HERE);
  store()?.removeItem(HERE);
  return asked === "one" || asked === "all" ? asked : "";
}

/**
 * Signs out of these other servers' accounts, then here too when `here`;
 * settles when none is left to visit (the page has gone otherwise).
 */
export function startRemoteSignOut(servers: string[], here: boolean): Promise<void> {
  const pending: Outgoing = { state: randomState(), queue: servers, here,
    returnPath: `${window.location.pathname}${window.location.search}` };
  store()?.setItem(OUTGOING, JSON.stringify(pending));
  return continueRemoteSignOut();
}

/** Goes to the next server to sign out of, passing over one it cannot reach. */
async function continueRemoteSignOut(): Promise<void> {
  const pending = read<Outgoing>(OUTGOING);
  if (!pending) return;
  for (let next = pending.queue.shift(); next; next = pending.queue.shift()) {
    const profile = listProfiles().find((candidate) => candidate.id === next);
    // Without a key this page holds no sign-in there to end.
    if (!profile?.listKey) {
      forgetProfile(next);
      continue;
    }
    let status = 0;
    try {
      status = (await fetch(listUrl(profileAddress(next)), { headers: { authorization: `Bearer ${profile.listKey}` },
        cache: "no-store", signal: AbortSignal.timeout(REACH_MS) })).status;
    } catch {
      status = 0;
    }
    // A key it no longer reads with leaves nothing to end from here.
    if (status === 401 || status === 403) {
      forgetProfile(next);
      continue;
    }
    if (status < 200 || status >= 300) continue;
    store()?.setItem(OUTGOING, JSON.stringify({ ...pending, current: next }));
    const url = new URL(REQUEST_PATH, profileAddress(next));
    url.searchParams.set("from", serverIdentity());
    url.searchParams.set("state", pending.state);
    url.hash = `key=${encodeURIComponent(profile.listKey)}`;
    window.location.assign(url.toString());
    return;
  }
  store()?.removeItem(OUTGOING);
  if (pending.here) signOutHere();
}

/**
 * At a page's start: another server's request to sign out here, kept to be
 * checked, or a server's answer to this page's request, which drops it from
 * the menu and goes on to the next.
 */
export function absorbRemoteSignOutAddress(): void {
  if (typeof window === "undefined" || isPackagedClient()) return;
  const { pathname, search, hash } = window.location;
  const query = new URLSearchParams(search);
  if (pathname === REQUEST_PATH) {
    const key = new URLSearchParams(hash.replace(/^#/u, "")).get("key") ?? "";
    const request: Incoming = { from: query.get("from") ?? "", state: (query.get("state") ?? "").slice(0, 64), key };
    if (request.from && request.state) store()?.setItem(INCOMING, JSON.stringify(request));
    window.history.replaceState(null, "", "/");
    return;
  }
  if (pathname !== RETURN_PATH) return;
  const pending = read<Outgoing>(OUTGOING);
  if (!pending || pending.state !== query.get("state")) {
    window.history.replaceState(null, "", "/");
    return;
  }
  if (pending.current && !query.get("error")) forgetProfile(pending.current);
  store()?.setItem(OUTGOING, JSON.stringify({ ...pending, current: undefined }));
  window.history.replaceState(null, "", pending.returnPath.startsWith("/") ? pending.returnPath : "/");
  void continueRemoteSignOut();
}

/**
 * Another server's request to sign out here, once: where to go back to, and
 * whether it is real — its key reads this server's list. A request from a
 * server this page doesn't know goes nowhere.
 */
export async function takeRemoteSignOutRequest(): Promise<{ back: string; real: boolean } | null> {
  const request = read<Incoming>(INCOMING);
  store()?.removeItem(INCOMING);
  if (!request) return null;
  const from = serverProfileId(request.from);
  if (!from || from === serverIdentity() || !listProfiles().some((profile) => profile.id === from)) return null;
  let real = false;
  if (validListKey(request.key)) {
    try {
      real = (await fetch(listUrl(serverOrigin()), { headers: { authorization: `Bearer ${request.key}` }, cache: "no-store" })).ok;
    } catch {
      real = false;
    }
  }
  const back = new URL(RETURN_PATH, profileAddress(from));
  back.searchParams.set("state", request.state);
  if (!real) back.searchParams.set("error", "refused");
  return { back: back.toString(), real };
}
