import { loadSession } from "./authSessionStore";
import { serverScopedName } from "./serverOrigin";

// The product's browser storage, and whose each value is.
//
// A device value is how this browser shows the app to whoever uses it: the
// sidebar's width and folded parts, a peek's width, the theme. A person value
// is what one person chose or did on one server: their recent searches and
// icons, the color they last used, the filters they hid, their language and
// display settings. Every account signed in on the same browser keeps its
// own person values, and with no one signed in there are none to read.
//
// A browser that refuses storage — a private window, blocked site data —
// reads nothing and keeps nothing here, and the app works on without it.
//
// What keeps its own storage, as each owns what it keeps and how: the
// server's sign-in (authSessionStore), the server profiles (profileRegistry),
// the local lock (localLockGate), the durable stores and their cross-tab
// notices (localDurableStoreBox, durableJobMemory, outboxCrossTabInvalidation),
// page analytics' anonymous
// visitor (pageAnalyticsBrowserRuntime), the language read before the app
// starts (i18n, kept per person), and the diagnostic switches (the service
// worker's off switch, the presence debug log).

function local(): Storage | null {
  try {
    return typeof window === "undefined" ? null : window.localStorage;
  } catch {
    return null;
  }
}

function read(key: string): string | null {
  try {
    return local()?.getItem(key) ?? null;
  } catch {
    return null;
  }
}

function write(key: string, value: string | null) {
  try {
    const storage = local();
    if (!storage) return;
    if (value === null) storage.removeItem(key);
    else storage.setItem(key, value);
  } catch {
    // Full or refused: the value stays for this visit only.
  }
}

/** What a stored text holds as JSON, or null for nothing or what isn't JSON. */
export function parseStored(value: string | null): unknown {
  if (value === null) return null;
  try {
    return JSON.parse(value);
  } catch {
    return null;
  }
}

/** A value this browser keeps for whoever uses it. */
export function deviceValue(name: string): string | null {
  return read(name);
}

/** Keeps a value for whoever uses this browser; null takes it out. */
export function storeDeviceValue(name: string, value: string | null): void {
  write(name, value);
}

export type PersonValueOptions = {
  /** Whose value: the person signed in to the active server when left out. */
  userId?: string | null;
  /** What it is within the person's values: a workspace, a view, a block. */
  part?: string;
};

function personKey(name: string, options: PersonValueOptions): string | null {
  const userId = (options.userId === undefined ? loadSession()?.user.id : options.userId)?.trim();
  if (!userId) return null;
  return serverScopedName(`${name}:${encodeURIComponent(userId)}${options.part === undefined ? "" : `:${options.part}`}`);
}

/**
 * A value a person keeps on this browser. Where the value was kept before
 * it was the person's (`previous`), the first person to read it takes it,
 * and no one else sees it after.
 */
export function personValue(
  name: string,
  options: PersonValueOptions & { previous?: readonly string[] } = {},
): string | null {
  const key = personKey(name, options);
  if (!key) return null;
  const value = read(key);
  if (value !== null) return value;
  for (const previous of options.previous ?? []) {
    if (previous === key) continue;
    const kept = read(previous);
    if (kept === null) continue;
    write(key, kept);
    write(previous, null);
    return kept;
  }
  return null;
}

/** Keeps a value for a person on this browser; null takes it out. */
export function storePersonValue(name: string, value: string | null, options: PersonValueOptions = {}): void {
  const key = personKey(name, options);
  if (key) write(key, value);
}

/**
 * Takes out every value a person kept on this browser, as their account is
 * deleted. Signing out keeps them: they are the person's alone, and come
 * back when they sign in again.
 */
export function removePersonValues(userId: string): void {
  const storage = local();
  const marker = `:${encodeURIComponent(userId.trim())}`;
  if (!storage || marker === ":") return;
  try {
    const keys: string[] = [];
    for (let index = 0; index < storage.length; index += 1) {
      const key = storage.key(index);
      if (key?.includes(marker)) keys.push(key);
    }
    for (const key of keys) storage.removeItem(key);
  } catch {
    // What the browser keeps of the person goes with its site data.
  }
}

/**
 * Lets go of what the retired generation before Leafen kept in this browser
 * ("hanji.*" names, as the workspace and person it last showed), which
 * nothing reads.
 */
export function forgetRetiredGenerationValues(): void {
  const storage = local();
  if (!storage) return;
  try {
    const keys: string[] = [];
    for (let index = 0; index < storage.length; index += 1) {
      const key = storage.key(index);
      if (key?.startsWith("hanji.")) keys.push(key);
    }
    for (const key of keys) storage.removeItem(key);
  } catch {
    // Refused storage keeps them, unread.
  }
}
