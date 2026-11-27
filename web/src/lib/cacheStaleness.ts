import { serverScopedName } from "./serverOrigin";

/**
 * What changes here is kept as the cached copy a moment later, so a reload
 * just after a change drew the copy from before it (2026-11-02: a row put in
 * the trash came back for a second, its relations with it). Each change
 * marks its copy at once with its time, in storage that outlives a reload;
 * keeping the copy clears a mark no later than what it kept: a server's
 * answer, what it read from its start; what is shown here, what is still
 * shown as the keeping starts — a page's change marks the lists' copy as it
 * is sent, after the lists show it. A list the server's change check brought
 * is kept as one changed here is: taken for a whole read, which keeps itself,
 * it was never kept and its marks stayed till the next whole read (2026-11-15:
 * a page put in the trash or renamed, its mark there 70 s on).
 * A marked copy waits for the server and shows only if that can't answer
 * (localFirstSnapshotLifecycle's `cacheBehind`).
 * Each account keeps its own marks, as it keeps its own copies (kept by
 * person and server): another account's copy kept here no longer clears the
 * mark of this one's, and the marks of an account on a server since replaced
 * at the same address are never read again (2026-11-15: 127.0.0.1 kept 21
 * marks of pages on earlier test servers). Marks from before were kept for
 * every account: still read for each, they clear as before.
 */
const KEY = "leafen:cache-behind";

/** The cached copy of the app's lists (useAuthenticatedBootstrap). */
export const BOOTSTRAP_CACHE = "bootstrap";
/** The cached copy of a page's content graph (useLocalFirstContentGraph). */
export const pageGraphCache = (pageId: string) => `page:${pageId}`;

function read(name: string): Record<string, number> {
  try {
    const value = JSON.parse(localStorage.getItem(name) ?? "{}");
    return value && typeof value === "object" ? value as Record<string, number> : {};
  } catch {
    return {};
  }
}

function write(name: string, marks: Record<string, number>) {
  try {
    if (Object.keys(marks).length) localStorage.setItem(name, JSON.stringify(marks));
    else localStorage.removeItem(name);
  } catch {
    // Without storage the copy is drawn as before.
  }
}

const accountMarks = (principalId: string) => serverScopedName(`${KEY}:${principalId}`);
const sharedMarks = () => serverScopedName(KEY);

/** The cached copy is behind what was shown here at `at`. */
export function markCacheBehind(principalId: string, cache: string, at: number) {
  write(accountMarks(principalId), { ...read(accountMarks(principalId)), [cache]: at });
}

/** The copy now holds what was shown at `since` or later. */
export function clearCacheBehind(principalId: string, cache: string, since: number) {
  for (const name of [accountMarks(principalId), sharedMarks()]) {
    const marks = read(name);
    if (!marks[cache] || marks[cache] > since) continue;
    const { [cache]: _cleared, ...rest } = marks;
    write(name, rest);
  }
}

/** Whether the cached copy may be older than what was shown here. */
export function cacheBehind(principalId: string, cache: string) {
  return Boolean(read(accountMarks(principalId))[cache] || read(sharedMarks())[cache]);
}
