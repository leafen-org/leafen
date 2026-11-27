import type { SwitcherList } from "@/authWire.generated";
import type { Session } from "@/types";
import {
  addSession,
  dropOtherSession,
  followOtherSessions,
  loadSession,
  otherSessions,
  renewOtherSession,
  sameSessionSignIn,
  switchToOtherSession,
} from "./authSessionStore";
import { startPageOverAt } from "./packagedClient";
import {
  appOf,
  requestWorkspaceCreate,
  requestWorkspaceOpen,
  workspaceOf,
  type ProfileApp,
  type ProfileWorkspace,
} from "./profileRegistry";
import { markSignOutHere } from "./remoteSignOut";
import { serverOrigin, serverScopedName } from "./serverOrigin";

// More than one account on the active server (a profile is a server and an
// account, 2026-09-24), as Notion's "계정 추가하기": the account signed in now
// stays signed in while another signs in, and one is active at a time.
// authSessionStore.ts holds their sign-ins; here is what the workspace menu
// shows of each. Switching makes the other one active and starts the page
// over, as moving between servers does.

const ACCOUNTS_KEY = "leafen:v2:server-accounts";
const ADDING_KEY = "leafen:v2:adding-account";
const PROFILE_ID_PREFIX = "account:";
const MAX_PATH = 512;

export type ServerAccount = {
  userId: string;
  email: string;
  name: string;
  workspaces: ProfileWorkspace[];
  apps: ProfileApp[];
  unread: string[];
  /** The page address it was last at. */
  lastPath: string;
  addedAt: number;
};

/** The ID the workspace menu knows another account on this server by. */
export function accountProfileId(userId: string): string {
  return `${PROFILE_ID_PREFIX}${userId}`;
}

/** The account an ID of the workspace menu names, or "" for a server's. */
export function accountOfProfileId(id: string): string {
  return id.startsWith(PROFILE_ID_PREFIX) ? id.slice(PROFILE_ID_PREFIX.length) : "";
}

const listeners = new Set<() => void>();
const announce = () => { for (const listener of [...listeners]) listener(); };

/** Calls `listener` when the accounts on this server, or the adding of one, change — here or in another tab of the app. */
export function subscribeServerAccounts(listener: () => void): () => void {
  listeners.add(listener);
  const stop = followOtherSessions(listener);
  return () => {
    listeners.delete(listener);
    stop();
  };
}

function stored(): ServerAccount[] {
  try {
    const parsed = JSON.parse(localStorage.getItem(serverScopedName(ACCOUNTS_KEY)) ?? "[]") as unknown;
    if (!Array.isArray(parsed)) return [];
    return parsed.flatMap((value: Partial<ServerAccount> | null) => typeof value?.userId === "string" ? [{
      userId: value.userId,
      email: typeof value.email === "string" ? value.email : "",
      name: typeof value.name === "string" ? value.name : "",
      workspaces: (Array.isArray(value.workspaces) ? value.workspaces : []).flatMap((item) => workspaceOf(item) ?? []),
      apps: (Array.isArray(value.apps) ? value.apps : []).flatMap((item) => appOf(item) ?? []),
      unread: (Array.isArray(value.unread) ? value.unread : []).filter((id) => typeof id === "string"),
      lastPath: typeof value.lastPath === "string" && value.lastPath.startsWith("/") ? value.lastPath.slice(0, MAX_PATH) : "",
      addedAt: typeof value.addedAt === "number" && Number.isFinite(value.addedAt) ? value.addedAt : 0,
    }] : []);
  } catch {
    return [];
  }
}

function save(accounts: ServerAccount[]): void {
  try {
    localStorage.setItem(serverScopedName(ACCOUNTS_KEY), JSON.stringify(accounts));
  } catch {
    // The menu shows what it knew; nothing else depends on it.
  }
  announce();
}

function blank(session: Session, addedAt: number): ServerAccount {
  return {
    userId: session.user.id, email: session.user.email ?? "", name: session.user.name ?? "",
    workspaces: [], apps: [], unread: [], lastPath: "", addedAt,
  };
}

function update(userId: string, change: (account: ServerAccount) => ServerAccount): void {
  const accounts = stored();
  if (accounts.some((account) => account.userId === userId)) {
    save(accounts.map((account) => account.userId === userId ? change(account) : account));
  }
}

/** The accounts signed in on this server, the active one among them, in the order they were added. */
export function listServerAccounts(): ServerAccount[] {
  const known = stored();
  return [loadSession(), ...otherSessions()].flatMap((session) => session
    ? [known.find((account) => account.userId === session.user.id) ?? blank(session, Date.now())] : [])
    .sort((left, right) => left.addedAt - right.addedAt);
}

/** Keeps what the active account looks like; the first on this server takes the server's place in the menu. */
export function rememberServerAccount(session: Session, workspaces: ProfileWorkspace[], firstAddedAt: number): void {
  const accounts = stored();
  const known = accounts.find((account) => account.userId === session.user.id);
  const next = {
    ...(known ?? blank(session, accounts.length ? Date.now() : firstAddedAt)),
    email: session.user.email ?? "", name: session.user.name ?? "", workspaces,
  };
  if (JSON.stringify(known) === JSON.stringify(next)) return;
  save(known ? accounts.map((account) => account === known ? next : account) : [...accounts, next]);
}

/** Drops an account from the menu, once signed out. */
export function forgetServerAccount(userId: string): void {
  const accounts = stored();
  if (accounts.some((account) => account.userId === userId)) save(accounts.filter((account) => account.userId !== userId));
}

/** Where an account goes on: the page it was last at. */
export function serverAccountPath(userId: string): string {
  return stored().find((account) => account.userId === userId)?.lastPath || "/";
}

function rememberPathOf(session: Session | null): void {
  if (!session) return;
  const path = `${window.location.pathname}${window.location.search}`.slice(0, MAX_PATH);
  update(session.user.id, (account) => ({ ...account, lastPath: path }));
}

/** Ends a sign-in on its server, which otherwise ends when it runs out. */
export async function endSignIn(address: string, session: Session): Promise<void> {
  if (!session.sessionId) return;
  try {
    await fetch(new URL(`/api/leafen/v1/account/sessions/${encodeURIComponent(session.sessionId)}`, address), {
      method: "DELETE",
      headers: { authorization: `Bearer ${session.token}` },
    });
  } catch {
    // The sign-in ends on that server when it runs out.
  }
}

/** Whether this tab is signing in another account, which survives a provider's redirect. */
export function addingAccount(): boolean {
  try {
    return sessionStorage.getItem(ADDING_KEY) === "1";
  } catch {
    return false;
  }
}

function setAdding(adding: boolean): void {
  if (addingAccount() === adding) return;
  try {
    if (adding) sessionStorage.setItem(ADDING_KEY, "1");
    else sessionStorage.removeItem(ADDING_KEY);
  } catch {
    // The account signs in or is left in this load.
  }
  announce();
}

export function beginAddingAccount(): void { setAdding(true); }
export function endAddingAccount(): void { setAdding(false); }

/** Takes the sign-in of the account being added; the one signed in now stays beside it. */
export async function addAccountSession(next: Session, isCurrent: () => boolean): Promise<boolean> {
  const current = loadSession();
  rememberPathOf(current);
  // The same account signing in again: this sign-in takes the earlier one's place.
  const earlier = [current, ...otherSessions()].flatMap((session) => (
    session && session.user.id === next.user.id && !sameSessionSignIn(session, next) ? [session] : []
  ));
  if (!await addSession(next, isCurrent)) return false;
  for (const session of earlier) void endSignIn(serverOrigin(), session);
  return true;
}

/** Makes another account on this server the active one and opens it where it was. */
export async function openOtherAccount(userId: string, workspaceId = "", at = "", create = false): Promise<void> {
  rememberPathOf(loadSession());
  const path = at || (workspaceId ? "/" : serverAccountPath(userId));
  if (!await switchToOtherSession(userId)) {
    forgetServerAccount(userId);
    return;
  }
  requestWorkspaceOpen(workspaceId);
  if (create) requestWorkspaceCreate();
  startPageOverAt(path);
}

/**
 * Signs out of another account on this server through its own page, where its
 * unsent changes are asked about as any account's are (useProductSignOut.ts);
 * the page then goes on in the account it came from.
 */
export function signOutOfOtherAccount(userId: string): void {
  markSignOutHere("one");
  void openOtherAccount(userId, "", "/");
}

/**
 * Asks for each other account's list with its own sign-in. One the server no
 * longer takes leaves the menu, and is added again; one that can't be read
 * keeps what it showed, which is what it last knew as the active account.
 */
export async function refreshOtherAccountLists(): Promise<void> {
  await Promise.all(otherSessions().map(async (session) => {
    try {
      const response = await fetch(new URL("/api/leafen/v1/switcher/list", serverOrigin()), {
        headers: { authorization: `Bearer ${session.token}` }, cache: "no-store",
      });
      const renewed = response.headers.get("x-leafen-session-token");
      if (renewed) await renewOtherSession(session, renewed);
      if (response.status === 401) {
        await dropOtherSession(session.user.id);
        forgetServerAccount(session.user.id);
        return;
      }
      if (!response.ok) return;
      const list = await response.json() as SwitcherList;
      const accounts = stored();
      const known = accounts.find((account) => account.userId === session.user.id) ?? blank(session, Date.now());
      const next: ServerAccount = {
        ...known,
        email: list.account.email, name: list.account.name,
        workspaces: list.workspaces.flatMap((item) => workspaceOf(item) ?? []),
        apps: list.apps.flatMap((app) => appOf(app) ?? []),
        unread: list.workspaces.filter((item) => item.unread).map((item) => item.id),
      };
      save([...accounts.filter((account) => account.userId !== session.user.id), next]);
    } catch {
      // It keeps what it showed.
    }
  }));
}
