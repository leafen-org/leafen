import type { Session } from "@/types";
import { changeKeptSignIn, followKeptSignIn } from "./keptSignIns";
import { serverScopedName, serverScopedNameFor } from "./serverOrigin";
import { publishSessionGenerationReset } from "./sessionGeneration";
import { keepTabSignIn, tabSignIn } from "./tabSignIn";

const SESSION_KEY = "leafen:v2:session";
const OTHER_SESSIONS_KEY = "leafen:v2:other-sessions";

// The active server's sign-in, and the other accounts signed in on that server
// beside it, the one used last at the end (lib/serverAccounts.ts).
const sessionKey = () => serverScopedName(SESSION_KEY);
const otherSessionsKey = () => serverScopedName(OTHER_SESSIONS_KEY);

// Where sign-ins are kept: the origin's storage, or a packaged client's
// window memory, which its shell signs in again on every start or which the
// app keeps in the platform's secret storage, so that no credential of it
// stays in the page's storage. There `kept` names the profile the window's
// sign-ins are kept for and what the window last read or wrote there.
let memory: Map<string, string> | null = null;
let kept: { profile: string; known: string } | null = null;

/** A stored sign-in, or null for what isn't one. */
export function sessionFromStored(value: string | null | undefined): Session | null {
  try {
    const session = value ? JSON.parse(value) as Session : null;
    return session && typeof session.token === "string" && typeof session.user?.id === "string" ? session : null;
  } catch {
    return null;
  }
}

/** The other accounts a sign-in the app kept holds beside it. */
export function otherSessionsIn(kept: Session | null): Session[] {
  const others = (kept as { otherSessions?: unknown } | null)?.otherSessions;
  return Array.isArray(others) ? others.flatMap((other) => sessionFromStored(JSON.stringify(other)) ?? []) : [];
}

/**
 * Keeps the active server's sign-in in this window's memory only, starting
 * from one the app kept, and, given the profile the app keeps it for (with
 * what it read there), each change goes back there (lib/keptSignIns.ts) and
 * the window takes the accounts its other tabs sign in or out beside its own.
 * A tab of the app starts in the account it showed, while that one is still
 * kept (tabSignIn.ts), else in the one used last.
 */
export function keepSessionsInMemory(options: {
  initial?: Session | null;
  profile?: string;
  kept?: string;
} = {}): void {
  if (memory) return;
  memory = new Map();
  if (options.initial) {
    const first = { ...options.initial, otherSessions: undefined };
    const own = options.profile && tabSignIn()?.profile === options.profile ? tabSignIn()?.user : "";
    const beside = otherSessionsIn(options.initial);
    const active = (own && beside.find((other) => other.user.id === own)) || first;
    // The one used last stays at the end, as a switch leaves it.
    const others = active === first ? beside : [...beside.filter((other) => other !== active), first];
    memory.set(sessionKey(), JSON.stringify(active));
    if (others.length) memory.set(otherSessionsKey(), JSON.stringify(others));
  }
  kept = options.profile ? { profile: options.profile, known: options.kept ?? "" } : null;
  if (kept) {
    noteTabSignIn();
    followKeptChanges(kept);
  }
  try {
    localStorage.removeItem(sessionKey());
  } catch {
    // Nothing kept there to remove.
  }
}

function storedValue(): string | null {
  return memory ? memory.get(sessionKey()) ?? null : localStorage.getItem(sessionKey());
}

function storeNamed(name: string, value: string | null): void {
  if (memory) {
    if (value === null) memory.delete(name);
    else memory.set(name, value);
  } else if (value === null) localStorage.removeItem(name);
  else localStorage.setItem(name, value);
}

function storeValue(value: string | null): void {
  storeNamed(sessionKey(), value);
  if (memory) noteTabSignIn();
}

// A tab of the app keeps the account it shows to start in again.
function noteTabSignIn(): void {
  if (kept) keepTabSignIn(kept.profile, loadSession()?.user.id ?? "");
}

/** The other accounts signed in on the active server, the one used last at the end. */
export function otherSessions(): Session[] {
  try {
    const value = memory ? memory.get(otherSessionsKey()) : localStorage.getItem(otherSessionsKey());
    return value ? otherSessionsIn({ otherSessions: JSON.parse(value) } as unknown as Session) : [];
  } catch {
    return [];
  }
}

function storeOthers(others: Session[]): void {
  storeNamed(otherSessionsKey(), others.length ? JSON.stringify(others) : null);
}

// The app keeps a server's sign-ins as one secret: the active one's, with
// the others in it (one used last stands in for an active one signed out).
function keptValue(): string | null {
  const active = loadSession();
  const others = otherSessions();
  const first = active ?? others.at(-1);
  if (!first) return null;
  const beside = active ? others : others.slice(0, -1);
  return JSON.stringify(beside.length ? { ...first, otherSessions: beside } : first);
}

// What the app keeps changed elsewhere — in another of its tabs, or by an
// agent's sign-in: this window goes on in its own account, with every
// account kept there beside it.
function takeKept(now: string): void {
  const held = sessionFromStored(now);
  const active = loadSession();
  const all = held ? [...otherSessionsIn(held), { ...held, otherSessions: undefined }] : [];
  storeOthers(all.filter((other) => other.user.id !== active?.user.id));
}

const othersWatchers = new Set<() => void>();

/** Calls `change` when the accounts beside this window's change from another tab or window of the app; returns the way to stop. */
export function followOtherSessions(change: () => void): () => void {
  othersWatchers.add(change);
  return () => { othersWatchers.delete(change); };
}

// Another tab or window of the app changed what it keeps: this window takes
// the accounts kept beside its own, read in turn with its own changes, and
// the menu shows them when they are others than it showed.
function followKeptChanges(into: { profile: string; known: string }): void {
  followKeptSignIn(into.profile, () => {
    const before = otherSessions().map((other) => other.user.id).join(" ");
    void changeKeptSignIn(into.profile, (now) => {
      if (kept === into && now !== undefined && now !== into.known) {
        takeKept(now);
        into.known = now;
      }
      return undefined;
    }).then(() => {
      if (otherSessions().map((other) => other.user.id).join(" ") === before) return;
      for (const change of [...othersWatchers]) change();
    }, () => undefined);
  });
}

// In the app a change of the window's sign-ins starts from what the app
// keeps now and goes back there in one write, after the changes before it
// (lib/keptSignIns.ts). One whose write fails stays in the window, and its
// next change writes it again; one that must be kept rejects instead.
async function writeKept(apply: () => boolean, mustKeep = false): Promise<boolean> {
  const into = kept;
  if (!into) return apply();
  let applied = false;
  let failed = false;
  try {
    const after = await changeKeptSignIn(into.profile, (now) => {
      if (now !== undefined && now !== into.known) takeKept(now);
      try {
        applied = apply();
      } catch (error) {
        failed = true;
        throw error;
      }
      return applied ? keptValue() : undefined;
    });
    if (after !== undefined) into.known = after;
  } catch (error) {
    if (failed || mustKeep) throw error;
  }
  return applied;
}

export function loadSession(): Session | null {
  try {
    return sessionFromStored(storedValue());
  } catch {
    return null;
  }
}

/**
 * Another server profile's sign-in a packaged client keeps in the page's
 * storage, as that server's own page would, or null.
 */
export function storedSessionOf(identity: string): Session | null {
  try {
    const value = localStorage.getItem(serverScopedNameFor(SESSION_KEY, identity));
    return value ? (JSON.parse(value) as Session) : null;
  } catch {
    return null;
  }
}

/** Forgets another server profile's sign-in kept in the page's storage. */
export function forgetSessionOf(identity: string): void {
  try {
    localStorage.removeItem(serverScopedNameFor(SESSION_KEY, identity));
  } catch {
    // Nothing kept there to forget.
  }
}

function saveSessionValue(signedIn: Session | null): void {
  const previous = loadSession();
  // An account signed out goes on in the one used before it, as Notion's.
  const others = otherSessions();
  const session = signedIn ?? others.at(-1) ?? null;
  if (!signedIn && session) storeOthers(others.slice(0, -1));
  if (
    previous?.token !== session?.token
    || previous?.user.id !== session?.user.id
  ) publishSessionGenerationReset();
  storeValue(session ? JSON.stringify(session) : null);
}

let writeDatabase: Promise<IDBDatabase> | null = null;

// Even on HTTP origins without Web Locks, read/write transactions serialize
// tabs. This empty store holds no credentials or second copy of the session.
function sessionWriteDatabase(): Promise<IDBDatabase> {
  writeDatabase ??= new Promise<IDBDatabase>((resolve, reject) => {
    const request = window.indexedDB.open("leafen:session-write-lock", 1);
    request.onupgradeneeded = () => request.result.createObjectStore("write");
    request.onerror = () => { writeDatabase = null; reject(request.error); };
    request.onsuccess = () => {
      const database = request.result;
      database.onversionchange = () => { database.close(); writeDatabase = null; };
      resolve(database);
    };
  });
  return writeDatabase;
}

async function withSessionWrite(write: () => boolean): Promise<boolean> {
  const scope = sessionKey();
  const apply = () => sessionKey() === scope && write();
  if (memory) return writeKept(apply);
  // Holders run without pausing, so the lock comes quickly; one that doesn't
  // within the wait (a tab frozen holding it) doesn't hold sign-in up: the
  // write goes ahead (2026-10-17).
  if (window.navigator.locks) {
    const signal = typeof AbortSignal.timeout === "function" ? AbortSignal.timeout(3_000) : undefined;
    return window.navigator.locks.request(scope, signal ? { signal } : {}, apply)
      .catch((error: unknown) => {
        if (error instanceof DOMException && error.name === "TimeoutError") return apply();
        throw error;
      });
  }
  const database = await sessionWriteDatabase();
  return new Promise<boolean>((resolve, reject) => {
    const transaction = database.transaction("write", "readwrite");
    let result = false;
    const request = transaction.objectStore("write").get(scope);
    request.onsuccess = () => {
      try { result = apply(); }
      catch (error) { transaction.abort(); reject(error); }
    };
    transaction.oncomplete = () => resolve(result);
    transaction.onabort = () => reject(transaction.error ?? new Error("Session storage unavailable."));
  });
}

export function saveSession(session: Session | null, isCurrent: () => boolean = () => true): Promise<boolean> {
  return withSessionWrite(() => {
    if (!isCurrent()) return false;
    saveSessionValue(session);
    return true;
  });
}

// A sign-in becomes the active one, with the accounts held here beside it; it
// answers the earlier sign-ins of the same account it takes the place of.
function takeAsActive(next: Session): Session[] {
  const current = loadSession();
  const held = [...otherSessions(), ...(current ? [current] : [])];
  storeOthers(held.filter((other) => other.user.id !== next.user.id));
  saveSessionValue(next);
  return held.filter((other) => other.user.id === next.user.id && !sameSessionSignIn(other, next));
}

/** Signs in as another account, keeping the one signed in now beside it. */
export function addSession(next: Session, isCurrent: () => boolean = () => true): Promise<boolean> {
  return withSessionWrite(() => {
    if (!isCurrent()) return false;
    takeAsActive(next);
    return true;
  });
}

/**
 * Keeps a sign-in as a server profile's active one in the app, with the
 * accounts kept there beside it (an agent's sign-in, lib/agentCodeSignIn.ts):
 * through this window's sign-ins when they are that profile's, so that
 * nothing the window writes after goes back over it. It answers the earlier
 * sign-ins of the same account it took the place of, and rejects when the
 * app couldn't keep it.
 */
export async function keepSignInFor(profile: string, next: Session): Promise<Session[]> {
  let replaced: Session[] = [];
  if (memory && kept?.profile === profile) {
    await writeKept(() => { replaced = takeAsActive(next); return true; }, true);
    return replaced;
  }
  await changeKeptSignIn(profile, (now) => {
    if (now === undefined) throw new Error("What the app keeps couldn't be read.");
    const held = sessionFromStored(now);
    const all = held ? [...otherSessionsIn(held), { ...held, otherSessions: undefined }] : [];
    const others = all.filter((other) => other.user.id !== next.user.id);
    replaced = all.filter((other) => other.user.id === next.user.id && !sameSessionSignIn(other, next));
    return JSON.stringify(others.length ? { ...next, otherSessions: others } : next);
  });
  return replaced;
}

/** Writes this window's sign-ins to what the app keeps; it rejects when the app couldn't keep them. */
export async function keepSessionInApp(): Promise<void> {
  if (!memory || !kept) throw new Error("The app keeps no logins for this window.");
  await writeKept(() => true, true);
}

/** Makes another account signed in on this server the active one, keeping this one beside it. */
export function switchToOtherSession(userId: string): Promise<boolean> {
  return withSessionWrite(() => {
    const others = otherSessions();
    const target = others.find((other) => other.user.id === userId);
    if (!target) return false;
    const current = loadSession();
    storeOthers([...others.filter((other) => other !== target), ...(current ? [current] : [])]);
    saveSessionValue(target);
    return true;
  });
}

/**
 * Another tab of the app set the server up, signed in as `userId`, while
 * this one showed the setup screen with no sign-in of its own: this tab
 * takes that sign-in, kept beside its own by then, as its own and answers
 * it. Null where this tab has one, where it isn't kept, and in a browser,
 * whose tabs share the stored sign-in and follow it (followStoredSession).
 */
export async function takeSignInMadeElsewhere(userId: string): Promise<Session | null> {
  if (!memory || loadSession()) return null;
  return await switchToOtherSession(userId) && loadSession()?.user.id === userId ? loadSession() : null;
}

/** Forgets another account's sign-in here. */
export function dropOtherSession(userId: string): Promise<boolean> {
  return withSessionWrite(() => {
    storeOthers(otherSessions().filter((other) => other.user.id !== userId));
    return true;
  });
}

const SESSION_RENEWED_EVENT = "leafen:session-renewed";

/** The sign-in a token belongs to (its session claim), or "" for none. */
export function tokenSessionId(token: string | undefined): string {
  try {
    const payload = (token ?? "").split(".")[1] ?? "";
    const claims = JSON.parse(atob(payload.replace(/-/g, "+").replace(/_/g, "/"))) as { sessionId?: unknown };
    return typeof claims.sessionId === "string" ? claims.sessionId : "";
  } catch {
    return "";
  }
}

function sameSignIn(next: Partial<Session> | null, session: Session): boolean {
  const signIn = tokenSessionId(session.token);
  return Boolean(next?.token) && next?.user?.id === session.user.id && signIn !== "" && tokenSessionId(next?.token) === signIn;
}

/** Whether two held credentials belong to the same server sign-in. */
export function sameSessionSignIn(left: Session | null, right: Session | null): boolean {
  return Boolean(left && right && left.user.id === right.user.id
    && (left.token === right.token || sameSignIn(left, right)));
}

/**
 * The sign-in held now for one read earlier — itself, renewed, or as one of
 * the other accounts here — or null once it has ended: work sent later than
 * it was asked for goes with what is held then, never a signed-out token.
 */
export function heldSignIn(session: Session): Session | null {
  return [loadSession(), ...otherSessions()].find((held) => sameSessionSignIn(held, session)) ?? null;
}

/** An older request or sign-out cannot erase a replacement sign-in. */
export function clearSessionIfCurrent(session: Session, exactToken = false): Promise<boolean> {
  return saveSession(null, () => {
    const current = loadSession();
    return sameSessionSignIn(current, session) && (!exactToken || current?.token === session.token);
  });
}

/** Account metadata and credential replies also belong to their admission. */
export function saveSessionIfCurrent(previous: Session, next: Session | null): Promise<boolean> {
  return saveSession(next, () => sameSessionSignIn(loadSession(), previous));
}

/**
 * Tabs on this server share one stored sign-in. A new login or sign-out
 * changes that authority; renewals keep their existing request generation.
 * Read the current value, since a queued event can describe an older write.
 * Each tab of the app keeps its own instead (tabSignIn.ts), with the
 * accounts beside it following the others' (followOtherSessions).
 */
export function followStoredSession(
  heldSession: () => Session | null,
  onChange: (next: Session | null) => void,
): () => void {
  if (memory) return () => undefined;
  const takeCurrent = () => {
    const session = heldSession();
    const next = loadSession();
    if (!next && !session) return;
    if (sameSessionSignIn(next, session)) {
      if (next && session) {
        if (JSON.stringify(next.user) !== JSON.stringify(session.user)) onChange(next);
        else if (next.token !== session.token) void renewSessionToken(session, next.token);
      }
      return;
    }
    publishSessionGenerationReset();
    onChange(next);
  };
  const onStorage = (event: StorageEvent) => {
    if (event.key === null || event.key === sessionKey()) takeCurrent();
  };
  window.addEventListener("storage", onStorage);
  takeCurrent();
  return () => window.removeEventListener("storage", onStorage);
}

/**
 * A renewed sign-in's token, as the server hands it on a request: the
 * session the request came with takes it at once, and so do the stored one
 * and the app's own when they are the same sign-in, without starting the app
 * over as a new sign-in would.
 */
export async function renewSessionToken(session: Session, token: string): Promise<void> {
  if (!token || token === session.token || !sameSignIn({ token, user: session.user }, session)) return;
  session.token = token;
  window.dispatchEvent(new CustomEvent<Session>(SESSION_RENEWED_EVENT, { detail: { ...session } }));
  try {
    await withSessionWrite(() => {
      const stored = loadSession();
      if (!stored || !sameSignIn(stored, session)) return false;
      storeValue(JSON.stringify({ ...stored, token }));
      return true;
    });
  } catch {
    // The stored token stays; it works until its own end.
  }
}

/** Keeps another account's renewed token, as a request with it handed it back. */
export function renewOtherSession(session: Session, token: string): Promise<boolean> {
  return withSessionWrite(() => {
    if (!sameSignIn({ token, user: session.user }, session)) return false;
    storeOthers(otherSessions().map((other) => sameSessionSignIn(other, session) ? { ...other, token } : other));
    return true;
  });
}

/** Keeps a session in step with the same sign-in renewed in this tab or another. */
export function followSessionRenewals(session: Session): () => void {
  const take = (next: Partial<Session> | null) => {
    if (next?.token && next.token !== session.token && sameSignIn(next, session)) session.token = next.token;
  };
  const onStorage = (event: StorageEvent) => {
    if (event.key !== sessionKey() || !event.newValue) return;
    try {
      take(JSON.parse(event.newValue) as Session);
    } catch {
      // Not a session.
    }
  };
  const onRenewed = (event: Event) => take((event as CustomEvent<Session>).detail);
  window.addEventListener("storage", onStorage);
  window.addEventListener(SESSION_RENEWED_EVENT, onRenewed);
  return () => {
    window.removeEventListener("storage", onStorage);
    window.removeEventListener(SESSION_RENEWED_EVENT, onRenewed);
  };
}
