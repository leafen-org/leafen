// The server accounts this client knows, however many, each with
// every workspace and app its list names: the active one's, kept
// up to date while it is signed in, and the others it has opened or added,
// each with what the workspace menu shows of it and where it was last. It
// lives in the page's own storage and holds no server's sign-in; a page a
// server serves keeps another server's list key here, which reads only the
// person's workspace and app list there (lib/switcherLists.ts).

import { keepTabSignIn, tabSignIn } from "./tabSignIn";

export const LOCAL_PROFILE_ID = "local";

const REGISTRY_KEY = "leafen:v2:profiles";
const ACTIVE_KEY = "leafen:v2:active-profile";
const OPEN_WORKSPACE_KEY = "leafen:v2:open-workspace";
const CREATE_WORKSPACE_KEY = "leafen:v2:create-workspace";

const MAX_TEXT = 200;
const MAX_PATH = 512;

export type ProfileWorkspace = {
  id: string;
  name: string;
  icon?: string;
  guest?: boolean;
};

export type ProfileApp = { slug: string; name: Record<string, string> };

/** How the last ask for a server's list went: "" fine or not asked yet. */
export type ProfileListState = "" | "unknown" | "off" | "failed";

const LIST_KEY_PATTERN = /^lsk_[A-Za-z0-9]{43}$/u;

export type ServerProfile = {
  /** "local" for a packaged client's own server, otherwise the server's origin. */
  id: string;
  /** The account there, once signed in; "" before. */
  email: string;
  name: string;
  workspaces: ProfileWorkspace[];
  lastWorkspaceId: string;
  /** The page address it was last at, on its own server. */
  lastPath: string;
  addedAt: number;
  /** The key that reads the list there, for a page another server serves. */
  listKey: string;
  apps: ProfileApp[];
  /** Its workspaces with unread notifications. */
  unread: string[];
  avatar: string;
  listState: ProfileListState;
  checkedAt: number;
  /** Where it is reached now, "" at its ID: a standby pair's other server once that took over, or the address its server moved to (reachedAt). */
  address: string;
  /** Its standby pair's other server, from its runtime settings; "" for none. */
  pairAddress: string;
  /** The server's name, as its administrators named it, and its alias, from the server. */
  serverName: string;
  serverAlias: string;
  /** The person's own name for the server, shown before the server's. */
  label: string;
};

type ProfileStorage = Pick<Storage, "getItem" | "setItem" | "removeItem">;

function pageStorage(): ProfileStorage | null {
  try {
    return globalThis.localStorage ?? null;
  } catch {
    return null;
  }
}

/** A server's profile ID: its origin, or "" for an address that isn't a web server's. */
export function serverProfileId(address: string): string {
  try {
    const url = new URL(address.trim());
    return url.protocol === "https:" || url.protocol === "http:" ? url.origin : "";
  } catch {
    return "";
  }
}

function text(value: unknown): string {
  return typeof value === "string" ? value.slice(0, MAX_TEXT) : "";
}

const MAX_NAME = 40;

function nameText(value: unknown): string {
  return typeof value === "string" ? value.trim().slice(0, MAX_NAME) : "";
}

function aliasText(value: unknown): string {
  return typeof value === "string" && /^[a-z]{1,30}$/u.test(value) ? value : "";
}

export function workspaceOf(value: unknown): ProfileWorkspace | null {
  if (!value || typeof value !== "object") return null;
  const item = value as Record<string, unknown>;
  const id = text(item.id);
  if (!id) return null;
  return {
    id,
    name: text(item.name),
    ...(typeof item.icon === "string" && item.icon ? { icon: text(item.icon) } : {}),
    ...(item.guest === true ? { guest: true } : {}),
  };
}

export function validListKey(value: unknown): value is string {
  return typeof value === "string" && LIST_KEY_PATTERN.test(value);
}

export function appOf(value: unknown): ProfileApp | null {
  if (!value || typeof value !== "object") return null;
  const item = value as Record<string, unknown>;
  const slug = text(item.slug);
  if (!/^[a-z0-9-]{1,64}$/u.test(slug) || !item.name || typeof item.name !== "object") return null;
  const name: Record<string, string> = {};
  for (const [language, label] of Object.entries(item.name as Record<string, unknown>).slice(0, 16)) {
    if (typeof label === "string") name[language.slice(0, 16)] = text(label);
  }
  return { slug, name };
}

function profileOf(value: unknown): ServerProfile | null {
  if (!value || typeof value !== "object") return null;
  const item = value as Record<string, unknown>;
  const id = item.id === LOCAL_PROFILE_ID ? LOCAL_PROFILE_ID : serverProfileId(text(item.id));
  if (!id || (id !== LOCAL_PROFILE_ID && id !== item.id)) return null;
  const path = typeof item.lastPath === "string" && item.lastPath.startsWith("/") ? item.lastPath.slice(0, MAX_PATH) : "";
  return {
    id,
    email: text(item.email),
    name: text(item.name),
    workspaces: (Array.isArray(item.workspaces) ? item.workspaces : [])
      .map(workspaceOf)
      .filter((workspace): workspace is ProfileWorkspace => workspace !== null),
    lastWorkspaceId: text(item.lastWorkspaceId),
    lastPath: path,
    addedAt: typeof item.addedAt === "number" && Number.isFinite(item.addedAt) ? item.addedAt : 0,
    listKey: validListKey(item.listKey) ? item.listKey : "",
    apps: (Array.isArray(item.apps) ? item.apps : []).map(appOf)
      .filter((app): app is ProfileApp => app !== null),
    unread: (Array.isArray(item.unread) ? item.unread : []).map(text).filter(Boolean),
    avatar: text(item.avatar),
    listState: item.listState === "unknown" || item.listState === "off" || item.listState === "failed" ? item.listState : "",
    checkedAt: typeof item.checkedAt === "number" && Number.isFinite(item.checkedAt) ? item.checkedAt : 0,
    address: id === LOCAL_PROFILE_ID ? "" : serverProfileId(text(item.address)),
    pairAddress: id === LOCAL_PROFILE_ID ? "" : serverProfileId(text(item.pairAddress)),
    serverName: nameText(item.serverName),
    serverAlias: aliasText(item.serverAlias),
    label: nameText(item.label),
  };
}

/** The profiles this client knows, in the order they were added. */
export function listProfiles(store = pageStorage()): ServerProfile[] {
  try {
    const parsed = JSON.parse(store?.getItem(REGISTRY_KEY) ?? "[]") as unknown;
    if (!Array.isArray(parsed)) return [];
    const seen = new Set<string>();
    return parsed
      .map(profileOf)
      .filter((profile): profile is ServerProfile => {
        if (!profile || seen.has(profile.id)) return false;
        seen.add(profile.id);
        return true;
      })
      .sort((left, right) => left.addedAt - right.addedAt);
  } catch {
    return [];
  }
}

// Whatever shows the list reads it again when it changes, here or in
// another tab (2026-10-28: a server account removed in settings stayed in
// the workspace menu until the page was loaded again).
const profileListeners = new Set<() => void>();
let followingOtherTabs = false;

/** Calls `listener` whenever the list of server accounts changes. */
export function subscribeProfiles(listener: () => void): () => void {
  profileListeners.add(listener);
  if (!followingOtherTabs && typeof window !== "undefined") {
    followingOtherTabs = true;
    window.addEventListener("storage", (event) => {
      if (event.key === REGISTRY_KEY || event.key === null) for (const each of [...profileListeners]) each();
    });
  }
  return () => { profileListeners.delete(listener); };
}

function saveProfiles(profiles: ServerProfile[], store: ProfileStorage | null): void {
  try {
    store?.setItem(REGISTRY_KEY, JSON.stringify(profiles));
  } catch {
    // The menu shows what it knew; nothing else depends on it.
  }
  for (const listener of [...profileListeners]) listener();
}

/** Keeps what the active server's account looks like, as it stands. */
export function rememberProfile(
  update: Pick<ServerProfile, "id" | "email" | "name" | "workspaces" | "lastWorkspaceId"> & { lastPath?: string },
  now = Date.now(),
  store = pageStorage(),
): void {
  const next = profileOf({ ...update, addedAt: now });
  if (!next) return;
  const profiles = listProfiles(store);
  const known = profiles.find((profile) => profile.id === next.id);
  const merged: ServerProfile = {
    ...next,
    lastPath: update.lastPath === undefined ? known?.lastPath ?? "" : next.lastPath,
    addedAt: known?.addedAt ?? now,
    listKey: known?.listKey ?? "",
    apps: known?.apps ?? [],
    unread: known?.unread ?? [],
    avatar: known?.avatar ?? "",
    listState: known?.listState ?? "",
    checkedAt: known?.checkedAt ?? 0,
    address: known?.address ?? "",
    pairAddress: known?.pairAddress ?? "",
    serverName: known?.serverName ?? "",
    serverAlias: known?.serverAlias ?? "",
    label: known?.label ?? "",
  };
  saveProfiles(known ? profiles.map((profile) => profile.id === next.id ? merged : profile) : [...profiles, merged], store);
}

/** Keeps a server's name and alias, as it says them. */
export function noteServerName(id: string, server: { name: string; alias: string }, store = pageStorage()): void {
  const profiles = listProfiles(store);
  const known = profiles.find((profile) => profile.id === id);
  const serverName = nameText(server.name), serverAlias = aliasText(server.alias);
  if (!known || (known.serverName === serverName && known.serverAlias === serverAlias)) return;
  saveProfiles(profiles.map((profile) => profile.id === id ? { ...profile, serverName, serverAlias } : profile), store);
}

/** Keeps the person's own name for a server, "" to show the server's. */
export function setProfileLabel(id: string, label: string, store = pageStorage()): void {
  const profiles = listProfiles(store);
  if (!profiles.some((profile) => profile.id === id)) return;
  saveProfiles(profiles.map((profile) => profile.id === id ? { ...profile, label: nameText(label) } : profile), store);
}

/**
 * Adds a server to sign in on, before its account is known; a server already
 * known at that address — its ID, or where it is reached now — is that
 * profile, not another (2026-11-15: the app's "add account" at the address
 * its server had moved to made a second account for the same server).
 */
export function addServerProfile(origin: string, now = Date.now(), store = pageStorage()): ServerProfile | null {
  const id = serverProfileId(origin);
  if (!id) return null;
  const profiles = listProfiles(store);
  const known = profileAt(id, store);
  if (known) return known;
  const added: ServerProfile = {
    id, email: "", name: "", workspaces: [], lastWorkspaceId: "", lastPath: "", addedAt: now,
    listKey: "", apps: [], unread: [], avatar: "", listState: "", checkedAt: 0, address: "", pairAddress: "",
    serverName: "", serverAlias: "", label: "",
  };
  saveProfiles([...profiles, added], store);
  return added;
}

/**
 * Where a profile's server is reached now: its ID — the address it was
 * added at — unless its standby pair's other server took over or the server
 * moved to another address. What shows a server account's address shows
 * this one, never the ID (2026-11-15: after the move, settings said the
 * account was in use at the old address while the app worked at the new).
 */
export function reachedAt(profile: Pick<ServerProfile, "id" | "address">): string {
  return profile.address || profile.id;
}

/** The known profile a server at an origin is: the one with that ID, else the one reached there now. */
export function profileAt(origin: string, store = pageStorage()): ServerProfile | null {
  const id = serverProfileId(origin);
  const profiles = id ? listProfiles(store) : [];
  return profiles.find((profile) => profile.id === id) ?? profiles.find((profile) => reachedAt(profile) === id) ?? null;
}

/** Where the profile with an ID is reached now (reachedAt). */
export function profileAddress(id: string, store = pageStorage()): string {
  const known = listProfiles(store).find((profile) => profile.id === id);
  return known ? reachedAt(known) : id;
}

/** Keeps the other server of a profile's standby pair, as its runtime settings say now. */
export function notePairAddress(id: string, pairAddress: string, store = pageStorage()): void {
  const pair = serverProfileId(pairAddress);
  const profiles = listProfiles(store);
  const known = profiles.find((profile) => profile.id === id);
  if (!known || known.id === LOCAL_PROFILE_ID || known.pairAddress === pair || pair === reachedAt(known)) return;
  saveProfiles(profiles.map((profile) => profile.id === id ? { ...profile, pairAddress: pair } : profile), store);
}

/**
 * Moves a profile to its pair's other server, which took over: the profile
 * keeps its ID, and with it the sign-in and everything the client stored of
 * it, and the server it was at becomes the pair's other one. Answers the
 * address it is reached at now, "" when it has no pair.
 */
export function moveProfileToPair(id: string, store = pageStorage()): string {
  const profiles = listProfiles(store);
  const known = profiles.find((profile) => profile.id === id);
  if (!known?.pairAddress) return "";
  const address = known.pairAddress;
  const was = reachedAt(known);
  saveProfiles(profiles.map((profile) => profile.id === id
    ? { ...profile, address: address === profile.id ? "" : address, pairAddress: was }
    : profile), store);
  return address;
}

/**
 * Moves a profile to the address its server moved to (2026-10-20): the
 * profile keeps its ID, and with it the sign-in and everything the client
 * stored of it.
 */
export function moveProfileAddress(id: string, address: string, store = pageStorage()): void {
  const origin = serverProfileId(address);
  const profiles = listProfiles(store);
  if (!origin || id === LOCAL_PROFILE_ID || !profiles.some((profile) => profile.id === id)) return;
  saveProfiles(profiles.map((profile) => profile.id === id ? { ...profile, address: origin === profile.id ? "" : origin } : profile), store);
}

/** Keeps the page address a known profile was last at, on its own server. */
export function rememberLastPath(id: string, path: string, store = pageStorage()): void {
  if (!path.startsWith("/")) return;
  const profiles = listProfiles(store);
  if (!profiles.some((profile) => profile.id === id)) return;
  saveProfiles(profiles.map((profile) => profile.id === id ? { ...profile, lastPath: path.slice(0, MAX_PATH) } : profile), store);
}

/** Keeps another server's list key, adding the server when it is new. */
export function setProfileListKey(id: string, key: string, now = Date.now(), store = pageStorage()): void {
  const server = validListKey(key) ? addServerProfile(id, now, store) : null;
  if (!server) return;
  saveProfiles(listProfiles(store).map((profile) => (
    profile.id === server.id ? { ...profile, listKey: key, listState: "" } : profile
  )), store);
}

/** When each account was added, as one server's page knows it. */
export type ProfileOrder = Array<[string, number]>;

export function profileOrder(store = pageStorage()): ProfileOrder {
  return listProfiles(store).map((profile) => [profile.id, profile.addedAt]);
}

/**
 * Takes another server's page's times for the accounts both know, keeping
 * the earlier, so every server's page lists its accounts in one order: the
 * order they were first added, as Notion's accounts keep theirs.
 */
export function alignProfileOrder(order: unknown, store = pageStorage()): void {
  if (!Array.isArray(order)) return;
  const earliest = new Map<string, number>();
  for (const item of order.slice(0, 64)) {
    if (!Array.isArray(item) || typeof item[0] !== "string" || typeof item[1] !== "number" || !Number.isFinite(item[1]) || item[1] <= 0) continue;
    const id = serverProfileId(item[0]);
    if (id) earliest.set(id, Math.min(item[1], earliest.get(id) ?? Infinity));
  }
  const profiles = listProfiles(store);
  if (!profiles.some((profile) => (earliest.get(profile.id) ?? Infinity) < profile.addedAt)) return;
  saveProfiles(profiles.map((profile) => {
    const at = earliest.get(profile.id);
    return at !== undefined && at < profile.addedAt ? { ...profile, addedAt: at } : profile;
  }), store);
}

/** What a server's list answered: its name, its account, workspaces and apps. */
export type ProfileList = {
  server?: { name: string; alias: string };
  account: { name: string; email: string; avatar: string };
  workspaces: Array<ProfileWorkspace & { unread?: boolean }>;
  apps: ProfileApp[];
};

export function applyProfileList(id: string, list: ProfileList, now = Date.now(), store = pageStorage()): void {
  saveProfiles(listProfiles(store).map((profile) => {
    if (profile.id !== id) return profile;
    const workspaces = list.workspaces.map(workspaceOf)
      .filter((workspace): workspace is ProfileWorkspace => workspace !== null);
    return {
      ...profile,
      email: text(list.account.email), name: text(list.account.name), avatar: text(list.account.avatar), workspaces,
      apps: list.apps.map(appOf).filter((app): app is ProfileApp => app !== null),
      unread: list.workspaces.filter((workspace) => workspace.unread).map((workspace) => text(workspace.id)),
      ...(list.server ? { serverName: nameText(list.server.name), serverAlias: aliasText(list.server.alias) } : {}),
      listState: "", checkedAt: now,
    };
  }), store);
}

export function markProfileList(id: string, state: ProfileListState, now = Date.now(), store = pageStorage()): void {
  saveProfiles(listProfiles(store).map((profile) => (
    profile.id === id ? { ...profile, listState: state, checkedAt: now, ...(state === "unknown" ? { listKey: "" } : {}) } : profile
  )), store);
}

/** Drops a profile from the menu. */
export function forgetProfile(id: string, store = pageStorage()): void {
  saveProfiles(listProfiles(store).filter((profile) => profile.id !== id), store);
  // Opened last, it gives way to the client's own server; a tab of the app
  // that shows it goes on with that one (activeProfileId).
  if (lastActiveProfileId(store) === id) storeActiveProfileId(LOCAL_PROFILE_ID, store);
}

// The profile opened last, which a page starts in unless its tab of the app
// keeps its own.
function lastActiveProfileId(store: ProfileStorage | null): string {
  try {
    const stored = store?.getItem(ACTIVE_KEY) ?? "";
    return stored === LOCAL_PROFILE_ID ? LOCAL_PROFILE_ID : serverProfileId(stored) || LOCAL_PROFILE_ID;
  } catch {
    return LOCAL_PROFILE_ID;
  }
}

function storeActiveProfileId(id: string, store: ProfileStorage | null): void {
  try {
    if (id === LOCAL_PROFILE_ID || !serverProfileId(id)) store?.removeItem(ACTIVE_KEY);
    else store?.setItem(ACTIVE_KEY, serverProfileId(id));
  } catch {
    // The client opens its own server.
  }
}

/**
 * A packaged client's active profile: the one its tab of the app shows,
 * while the client still knows it (tabSignIn.ts), else the one opened last —
 * its own server unless another was chosen.
 */
export function activeProfileId(store = pageStorage()): string {
  const own = store === pageStorage() ? tabSignIn()?.profile : undefined;
  if (own && (own === LOCAL_PROFILE_ID || listProfiles(store).some((profile) => profile.id === own))) return own;
  return lastActiveProfileId(store);
}

/** Opens a profile from now on: the one opened last, and the one this tab of the app shows. */
export function setActiveProfileId(id: string, store = pageStorage()): void {
  storeActiveProfileId(id, store);
  if (store === pageStorage()) keepTabSignIn(id === LOCAL_PROFILE_ID ? id : serverProfileId(id) || LOCAL_PROFILE_ID, "");
}

/** The workspace a switch asked to open, once: the next start takes it. */
export function requestWorkspaceOpen(workspaceId: string, store = pageStorage()): void {
  try {
    if (workspaceId) store?.setItem(OPEN_WORKSPACE_KEY, workspaceId.slice(0, MAX_TEXT));
  } catch {
    // The client opens where it would.
  }
}

/** Another account's "+ New workspace": its page opens the new workspace window as it starts. */
export function requestWorkspaceCreate(store = pageStorage()): void {
  try {
    store?.setItem(CREATE_WORKSPACE_KEY, "1");
  } catch {
    // The page opens as it would.
  }
}

export function takeWorkspaceCreateRequest(store = pageStorage()): boolean {
  try {
    const asked = store?.getItem(CREATE_WORKSPACE_KEY) === "1";
    store?.removeItem(CREATE_WORKSPACE_KEY);
    return asked;
  } catch {
    return false;
  }
}

export function peekWorkspaceOpenRequest(store = pageStorage()): string {
  try {
    return store?.getItem(OPEN_WORKSPACE_KEY) ?? "";
  } catch {
    return "";
  }
}

export function takeWorkspaceOpenRequest(store = pageStorage()): string {
  try {
    const requested = store?.getItem(OPEN_WORKSPACE_KEY) ?? "";
    store?.removeItem(OPEN_WORKSPACE_KEY);
    return requested;
  } catch {
    return "";
  }
}
