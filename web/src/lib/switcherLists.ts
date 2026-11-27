import { request } from "@/apiTransport";
import type { SwitcherKeyIssued, SwitcherList } from "@/authWire.generated";
import type { Session } from "@/types";
import { followAddressChange } from "./addressChangeFollow";
import { sessionFromStored, storedSessionOf } from "./authSessionStore";
import { isPackagedClient } from "./packagedClient";
import { packagedCredentials } from "./packagedCredentials";
import {
  alignProfileOrder,
  applyProfileList,
  listProfiles,
  LOCAL_PROFILE_ID,
  markProfileList,
  profileOrder,
  reachedAt,
  requestWorkspaceCreate,
  requestWorkspaceOpen,
  serverProfileId,
  setProfileListKey,
  validListKey,
  type ServerProfile,
} from "./profileRegistry";
import { refreshOtherAccountLists } from "./serverAccounts";
import { serverIdentity } from "./serverOrigin";

// Other servers' workspace lists in the switcher (the multi-server
// decision's 2026-10-07 addition). A page a server serves holds a list key
// for each other server: it reads the person's workspaces and apps there and
// nothing else. Keys come by a hop to the other server, which asks the person
// first, and move along when the person goes from one server's page to
// another's, in the address's fragment — never sent to a server — so every
// server's page lists the same. A packaged client asks with the sign-in it
// keeps for each server instead.

const HAND_OFF = "leafen-switch";
const CONNECT_PENDING = "leafen:v2:switcher-connect";
const CONNECT_REQUEST = "leafen:v2:switcher-request";
const MAX_KEYS = 32;
const ASK_EVERY_MS = 30_000;
// How long another server's request to see this list waits for its sign-in
// here: one left unanswered isn't asked at a sign-in much later (2026-11-06:
// a request left in a tab came up half an hour on, at a sign-in for nothing
// of the kind).
const CONNECT_REQUEST_MS = 15 * 60_000;

export type ListKeyEntry = { server: string; key: string };

/** A server's page asking this one for its list (the hop's first half), with when it added each account. */
export type ConnectRequest = { from: string; state: string; keys: ListKeyEntry[]; order?: unknown; at?: number };

function encode(value: unknown): string {
  const bytes = new TextEncoder().encode(JSON.stringify(value));
  let binary = "";
  for (const byte of bytes) binary += String.fromCharCode(byte);
  return btoa(binary).replace(/\+/gu, "-").replace(/\//gu, "_").replace(/=+$/u, "");
}

function decode(raw: string): unknown {
  try {
    const binary = atob(raw.replace(/-/gu, "+").replace(/_/gu, "/"));
    return JSON.parse(new TextDecoder().decode(Uint8Array.from(binary, (char) => char.charCodeAt(0))));
  } catch {
    return null;
  }
}

function keysOf(value: unknown): ListKeyEntry[] {
  if (!Array.isArray(value)) return [];
  const seen = new Set<string>();
  const keys: ListKeyEntry[] = [];
  for (const item of value.slice(0, MAX_KEYS)) {
    const server = serverProfileId(typeof item?.server === "string" ? item.server : "");
    if (!server || seen.has(server) || !validListKey(item?.key)) continue;
    seen.add(server);
    keys.push({ server, key: item.key });
  }
  return keys;
}

function session(): Storage | null {
  try {
    return globalThis.sessionStorage ?? null;
  } catch {
    return null;
  }
}

function randomState(): string {
  const bytes = crypto.getRandomValues(new Uint8Array(18));
  return Array.from(bytes, (byte) => byte.toString(16).padStart(2, "0")).join("");
}

/** The list keys this page knows, except the one for the server at `except`. */
function knownListKeys(except = ""): ListKeyEntry[] {
  return listProfiles()
    .filter((profile) => profile.listKey && profile.id !== except && reachedAt(profile) !== except && profile.id !== serverIdentity())
    .map((profile) => ({ server: profile.id, key: profile.listKey }))
    .slice(0, MAX_KEYS);
}

/** Keeps keys another page handed over, but none for this server itself. */
function absorbKeys(keys: ListKeyEntry[]): void {
  const self = serverIdentity();
  for (const entry of keys) if (entry.server !== self) setProfileListKey(entry.server, entry.key);
  announce();
}

/** A list key of this server for a page at `holder`, or "" when it gives none. */
async function issueListKey(signedIn: Session, holder: string): Promise<string> {
  try {
    const issued = await request<SwitcherKeyIssued>("/account/switcher-keys", {
      method: "POST", body: JSON.stringify({ holder }),
    }, signedIn);
    return validListKey(issued.key) ? issued.key : "";
  } catch {
    return "";
  }
}

/**
 * Adds another server: hands it this server's key and the others this page
 * knows, and goes there, where the person signs in and allows this page to
 * see their list; that server sends its own key back to /connect/return.
 */
export async function startServerConnect(signedIn: Session, target: string): Promise<void> {
  const self = serverIdentity();
  const own = await issueListKey(signedIn, target);
  const state = randomState();
  session()?.setItem(CONNECT_PENDING, JSON.stringify({
    state, target, returnPath: `${window.location.pathname}${window.location.search}`,
  }));
  const url = new URL("/connect", target);
  url.searchParams.set("from", self);
  url.searchParams.set("state", state);
  url.hash = `offer=${encode([...(own ? [{ server: self, key: own }] : []), ...knownListKeys(target)])}&order=${encode(profileOrder())}`;
  window.location.assign(url.toString());
}

/** The address that goes to another server's page with the keys this page knows, and whether it starts a new workspace there. */
export function handOffUrl(target: string, path: string, workspaceId = "", create = false): string {
  const url = new URL(path || "/", target);
  // What the page's own address carries after "#" (a setup link's code) stays beside it.
  const kept = url.hash.replace(/^#/u, "");
  url.hash = `${kept ? `${kept}&` : ""}${HAND_OFF}=${encode({ workspace: workspaceId, keys: knownListKeys(target), order: profileOrder(), ...(create ? { create: true } : {}) })}`;
  return url.toString();
}

/**
 * At a page's start: takes keys handed over in the address — a switch from
 * another server's page, another server's answer at /connect/return, or a
 * request at /connect — and leaves the address clean.
 */
export function absorbSwitcherAddress(): void {
  if (typeof window === "undefined" || isPackagedClient()) return;
  const { pathname, search, hash } = window.location;
  const fragment = new URLSearchParams(hash.replace(/^#/u, ""));
  const query = new URLSearchParams(search);
  if (pathname === "/connect" && query.get("from") && query.get("state")) {
    const from = serverProfileId(query.get("from") ?? "");
    const request: ConnectRequest = { from, state: (query.get("state") ?? "").slice(0, 64), keys: keysOf(decode(fragment.get("offer") ?? "")),
      order: decode(fragment.get("order") ?? ""), at: Date.now() };
    if (from && from !== serverIdentity()) session()?.setItem(CONNECT_REQUEST, JSON.stringify(request));
    window.history.replaceState(null, "", "/");
    return;
  }
  if (pathname === "/connect/return") {
    let pending: { state?: string; target?: string; returnPath?: string } = {};
    try {
      pending = JSON.parse(session()?.getItem(CONNECT_PENDING) ?? "{}");
    } catch {
      pending = {};
    }
    session()?.removeItem(CONNECT_PENDING);
    const target = serverProfileId(pending.target ?? "");
    if (target && pending.state && pending.state === query.get("state")) {
      const answer = decode(fragment.get("answer") ?? "") as { key?: unknown; keys?: unknown } | null;
      if (answer && validListKey(answer.key)) setProfileListKey(target, answer.key);
      if (query.get("error") === "off") markProfileList(target, "off");
      absorbKeys(keysOf(answer?.keys));
      alignProfileOrder(answer && (answer as { order?: unknown }).order);
    }
    const back = pending.returnPath?.startsWith("/") && !pending.returnPath.startsWith("/connect") ? pending.returnPath : "/";
    window.history.replaceState(null, "", back);
    return;
  }
  if (fragment.has(HAND_OFF)) {
    const handed = decode(fragment.get(HAND_OFF) ?? "") as { workspace?: unknown; keys?: unknown; create?: unknown } | null;
    absorbKeys(keysOf(handed?.keys));
    if (typeof handed?.workspace === "string" && handed.workspace) requestWorkspaceOpen(handed.workspace);
    if (handed?.create === true) requestWorkspaceCreate();
    alignProfileOrder((handed as { order?: unknown } | null)?.order);
    window.history.replaceState(null, "", `${pathname}${search}`);
  }
}

/** A request from another server's page to see this one's list, until answered or left too long. */
export function pendingConnectRequest(): ConnectRequest | null {
  try {
    const stored = JSON.parse(session()?.getItem(CONNECT_REQUEST) ?? "null") as ConnectRequest | null;
    if (!stored?.from || !stored.state) return null;
    if (typeof stored.at !== "number" || Date.now() - stored.at > CONNECT_REQUEST_MS) {
      session()?.removeItem(CONNECT_REQUEST);
      return null;
    }
    return { from: stored.from, state: stored.state, keys: keysOf(stored.keys), order: stored.order, at: stored.at };
  } catch {
    return null;
  }
}

/** Answers the request: allowed, this server's key goes back with the keys this page knows. */
export async function answerConnectRequest(signedIn: Session, connect: ConnectRequest, allow: boolean): Promise<void> {
  session()?.removeItem(CONNECT_REQUEST);
  const url = new URL("/connect/return", connect.from);
  url.searchParams.set("state", connect.state);
  if (!allow) {
    url.searchParams.set("error", "denied");
  } else {
    absorbKeys(connect.keys);
    alignProfileOrder(connect.order);
    const key = await issueListKey(signedIn, connect.from);
    if (key) url.hash = `answer=${encode({ key, keys: knownListKeys(connect.from), order: profileOrder() })}`;
    else url.searchParams.set("error", "off");
  }
  window.location.assign(url.toString());
}

const listeners = new Set<() => void>();
let lastAsked = 0;

function announce(): void {
  for (const listener of listeners) listener();
}

export function subscribeServerLists(listener: () => void): () => void {
  listeners.add(listener);
  return () => { listeners.delete(listener); };
}

async function packagedSignIn(profileId: string): Promise<string> {
  const credentials = await packagedCredentials();
  const kept = credentials ? await credentials.load(profileId).catch(() => "") : "";
  return (sessionFromStored(kept) ?? storedSessionOf(profileId))?.token ?? "";
}

/**
 * Reads a server's list where it is reached. A server whose address changed
 * answers the old one with a redirect, which a request carrying a key can't
 * follow across origins (its preflight is refused): the profile then follows
 * the server to its new address, as the app's active one does, and the list
 * is read there (2026-11-15).
 */
async function readSwitcherList(profile: ServerProfile, bearer: string): Promise<Response> {
  const read = (at: string) => fetch(new URL("/api/leafen/v1/switcher/list", at), {
    headers: { authorization: `Bearer ${bearer}` }, cache: "no-store",
  });
  const at = reachedAt(profile);
  try {
    return await read(at);
  } catch (failure) {
    const moved = await followAddressChange(profile.id, at);
    if (moved === at) throw failure;
    return read(moved);
  }
}

/**
 * Asks each other server for the person's list, at most every 30 seconds
 * unless forced; what each answers updates its group in the menu, and a
 * server that fails keeps what it showed.
 */
export async function refreshServerLists(force = false): Promise<void> {
  const now = Date.now();
  if (!force && now - lastAsked < ASK_EVERY_MS) return;
  lastAsked = now;
  // The app asks with the sign-in it keeps for each server in the keychain, a
  // browser with the list key each server gave it.
  const packaged = isPackagedClient();
  const self = serverIdentity();
  await Promise.all(listProfiles().filter((profile) => profile.id !== self && profile.id !== LOCAL_PROFILE_ID).map(async (profile) => {
    const bearer = packaged ? await packagedSignIn(profile.id) : profile.listKey;
    if (!bearer) return;
    try {
      const response = await readSwitcherList(profile, bearer);
      if (response.ok) {
        const list = await response.json() as SwitcherList;
        applyProfileList(profile.id, {
          server: { name: list.serverName ?? "", alias: list.serverAlias ?? "" },
          account: list.account,
          workspaces: list.workspaces.map(({ id, name, icon, guest, unread }) => ({
            id, name, ...(icon ? { icon } : {}), ...(guest ? { guest: true } : {}), ...(unread ? { unread: true } : {}),
          })),
          apps: list.apps.map(({ slug, name }) => ({ slug, name })),
        });
      } else {
        markProfileList(profile.id, response.status === 401 && !packaged ? "unknown" : response.status === 403 ? "off" : "failed");
      }
    } catch {
      markProfileList(profile.id, "failed");
    }
  }));
  await refreshOtherAccountLists();
  announce();
}
