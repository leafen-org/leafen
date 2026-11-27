// One index by id of a list of pages, made once for that list and shared by
// every place that looks its pages up. The product's list is a new array
// whenever a page in it changes, so an index kept for an array is never out
// of date. Each place made its own as it drew: some 34 places, 10–20 ms each
// over a 44,000-page workspace at CPU ×2, again on every redraw of the
// product (2026-11-14).

type Identified = { readonly id: string };

const maps = new WeakMap<readonly Identified[], ReadonlyMap<string, Identified>>();
const records = new WeakMap<readonly Identified[], Readonly<Record<string, Identified>>>();

/** The list's pages by id, as a map. */
export function pageMapOf<T extends Identified>(pages: readonly T[]): ReadonlyMap<string, T> {
  let map = maps.get(pages);
  if (!map) {
    const made = new Map<string, Identified>();
    for (const page of pages) made.set(page.id, page);
    map = made;
    maps.set(pages, map);
  }
  return map as ReadonlyMap<string, T>;
}

/** The list's pages by id, as a record (never changed: copy it to add to it). */
export function pagesByIdOf<T extends Identified>(pages: readonly T[]): Readonly<Record<string, T>> {
  let record = records.get(pages);
  if (!record) {
    const made: Record<string, Identified> = {};
    for (const page of pages) made[page.id] = page;
    record = made;
    records.set(pages, record);
  }
  return record as Readonly<Record<string, T>>;
}

/** A page of the list by its id. */
export function pageLookup<T extends Identified>(pages: readonly T[]): (id: string) => T | undefined {
  const map = pageMapOf(pages);
  return (id) => map.get(id);
}

/** How a list was made from another: its pages replaced in place, left out and added at its end. */
export type ListEdits<T> = {
  /** Pages that are new records of the old list's in the same order: their indexes in the old list and in the new. */
  readonly replaced: readonly (readonly [number, number])[];
  /** Pages of the old list the new one left out. */
  readonly removed: readonly T[];
  /** Indexes in the new list of pages the old one lacked, all at its end. */
  readonly added: readonly number[];
};

/**
 * How `after` was made from `before`, when it is `before` with a few pages
 * left out, some replaced by new records in their places and a few added at
 * its end, the rest the very same pages in the same order: a place that
 * works a result out of every page of a list can then work out only those
 * again. Null when a page moved, or when more than `most` came or went
 * (2026-11-15: a page put in the trash elsewhere left a 47,000-page
 * workspace's live list one page shorter, and every such place went over all
 * of it again, 177–262 ms at CPU ×2).
 */
export function listEditsOf<T extends Identified>(before: readonly T[] | null | undefined, after: readonly T[], most = 64): ListEdits<T> | null {
  if (!before) return null;
  const replaced: [number, number][] = [], removed: T[] = [];
  let index = 0, at = 0;
  while (index < before.length && at < after.length) {
    const was = before[index], now = after[at];
    if (was === now || was.id === now.id) {
      if (was !== now) replaced.push([index, at]);
      index++;
      at++;
      continue;
    }
    removed.push(was);
    if (removed.length > most) return null;
    index++;
  }
  for (; index < before.length; index++) {
    removed.push(before[index]);
    if (removed.length > most) return null;
  }
  const added: number[] = [];
  for (; at < after.length; at++) added.push(at);
  if (added.length > most) return null;
  if (added.length && removed.length) {
    // A page that went and came back at the end moved.
    const gone = new Set(removed.map(({ id }) => id));
    if (added.some((place) => gone.has(after[place].id))) return null;
  }
  return { replaced, removed, added };
}

type Placed = Identified & { readonly parentId?: string | null; readonly inTrash?: boolean };

const trashedIds = new WeakMap<readonly Placed[], ReadonlySet<string>>();
// The list the trash was last worked out for: a list made from it by a change
// that moves no page and puts none in or out of the trash — a title, an icon,
// a time — has the same pages in the trash (2026-11-14: each title saved in a
// 44,000-page workspace walked every page up again, some 60–150 ms at CPU ×2).
let lastTrashed: { pages: readonly Placed[]; trashed: ReadonlySet<string> } | null = null;

function sameTrashShape(before: readonly Placed[], after: readonly Placed[]) {
  if (before.length !== after.length) return false;
  for (let index = 0; index < after.length; index++) {
    const was = before[index], now = after[index];
    if (was === now) continue;
    if (was.id !== now.id || (was.parentId ?? null) !== (now.parentId ?? null) || Boolean(was.inTrash) !== Boolean(now.inTrash)) return false;
  }
  return true;
}

/**
 * Whether `pages` is the last list with pages added at its end out of the
 * trash and under no page in it, and the rest as before — their places and
 * marks the same (listEditsOf): the same pages are in the trash
 * (2026-11-15: each page made elsewhere in a 47,000-page workspace walked
 * every page up again, some 35 ms at CPU ×2).
 */
function sameTrashWithNewPages(last: { pages: readonly Placed[]; trashed: ReadonlySet<string> }, pages: readonly Placed[]) {
  const edits = listEditsOf(last.pages, pages);
  if (!edits || edits.removed.length || !edits.added.length) return false;
  for (const [was, now] of edits.replaced) {
    const before = last.pages[was], after = pages[now];
    if ((before.parentId ?? null) !== (after.parentId ?? null) || Boolean(before.inTrash) !== Boolean(after.inTrash)) return false;
  }
  // A page added in the trash, or under a page that is, changes it, as may
  // one a page listed before hangs under: then every page is walked again.
  let byId: ReadonlyMap<string, Placed> | null = null;
  for (const at of edits.added) {
    let page: Placed | undefined = pages[at];
    for (let steps = 0; page; steps++) {
      if (page.inTrash || last.trashed.has(page.id) || steps > 256) return false;
      if (!page.parentId) break;
      byId ??= pageMapOf(pages);
      page = byId.get(page.parentId);
    }
  }
  return true;
}

/**
 * The ids of the list's pages in the trash, themselves or under a page that
 * is (pageTrashRoot's answer for each), worked out once for the list: each
 * page's way up is walked once, not again for every page under it.
 */
export function trashedPageIdsOf<T extends Placed>(pages: readonly T[]): ReadonlySet<string> {
  const kept = trashedIds.get(pages);
  if (kept) return kept;
  if (lastTrashed && (sameTrashShape(lastTrashed.pages, pages) || sameTrashWithNewPages(lastTrashed, pages))) {
    trashedIds.set(pages, lastTrashed.trashed);
    lastTrashed = { pages, trashed: lastTrashed.trashed };
    return lastTrashed.trashed;
  }
  const byId = pageMapOf(pages);
  // Each page's answer once known; null while its way up is being walked (a
  // chain that comes back on itself ends there, as pageTrashRoot's).
  const known = new Map<string, boolean | null>();
  const trashed = new Set<string>();
  const path: string[] = [];
  for (const page of pages) {
    let result = known.get(page.id);
    if (result === undefined || result === null) {
      path.length = 0;
      let current: T | undefined = page;
      result = false;
      while (current) {
        const cached = known.get(current.id);
        if (cached !== undefined) {
          result = cached ?? false;
          break;
        }
        path.push(current.id);
        known.set(current.id, null);
        if (current.inTrash) {
          result = true;
          break;
        }
        current = current.parentId ? byId.get(current.parentId) : undefined;
      }
      for (const id of path) known.set(id, result);
    }
    if (result) trashed.add(page.id);
  }
  trashedIds.set(pages, trashed);
  lastTrashed = { pages, trashed };
  return trashed;
}

/**
 * Whether a page is out of the trash, by the list it was found in: the one
 * rule every list of pages to find, pick or open from goes by — search's
 * titles and visits, link, scope and target pickers. A page under a page in
 * the trash went there with it (trashedPageIdsOf), as Notion's search and
 * menus leave it out. A page the list hasn't — one a server search or a
 * library read gave — is out unless it or the page holding it is in.
 */
export function outOfTrashIn<T extends Placed>(pages: readonly T[]): (page: Placed) => boolean {
  const trashed = trashedPageIdsOf(pages);
  return (page) => !page.inTrash && !trashed.has(page.id) && !(page.parentId && trashed.has(page.parentId));
}

type InWorkspace = Placed & { readonly workspaceId: string };
type WorkspaceLists<T> = { live: T[]; trashed: T[] };
const workspaceListsOf = new WeakMap<readonly InWorkspace[], Map<string, WorkspaceLists<InWorkspace>>>();
// The list and workspace the lists were last made for, with the trash's ids then.
let lastWorkspaceLists: { pages: readonly InWorkspace[]; workspaceId: string; trashed: ReadonlySet<string>; lists: WorkspaceLists<InWorkspace> } | null = null;

/**
 * The last lists with the list's edits (listEditsOf) when the same pages are
 * in the trash: pages left out gone, new records in their places, pages
 * added at the end of the live list (none of them is in the trash).
 */
function editedWorkspaceLists(
  last: NonNullable<typeof lastWorkspaceLists>,
  pages: readonly InWorkspace[],
  edits: ListEdits<InWorkspace>,
  workspaceId: string,
): WorkspaceLists<InWorkspace> {
  const byId = new Map<string, InWorkspace>();
  for (const [, now] of edits.replaced) if (pages[now].workspaceId === workspaceId) byId.set(pages[now].id, pages[now]);
  const gone = new Set(edits.removed.filter((page) => page.workspaceId === workspaceId).map(({ id }) => id));
  const added = edits.added.map((at) => pages[at]).filter((page) => page.workspaceId === workspaceId);
  if (byId.size === 0 && gone.size === 0 && added.length === 0) return last.lists;
  const edited = (list: InWorkspace[]) => (gone.size ? list.filter(({ id }) => !gone.has(id)) : list)
    .map((page) => byId.get(page.id) ?? page);
  // A list of the workspace's live pages alone stays its own live list.
  const whole = last.lists.live === last.pages && edits.removed.length === 0 && added.length === edits.added.length;
  const inTrashTouched = gone.size > 0 || [...byId.keys()].some((id) => last.trashed.has(id));
  return {
    live: whole ? pages as InWorkspace[] : [...edited(last.lists.live), ...added],
    trashed: inTrashTouched ? edited(last.lists.trashed).map((page) => (page.inTrash ? page : { ...page, inTrash: true })) : last.lists.trashed,
  };
}

/**
 * A workspace's pages in a list: those out of the trash, and those in it
 * (trashedPageIdsOf), each of these marked in the trash. A list made from
 * the last one by new records of pages that stay in their workspace, a few
 * pages left out or added at its end, with the same pages in the trash,
 * takes the last lists with those edits (editedWorkspaceLists), not two
 * walks of every page (2026-11-15: each title saved, or page made elsewhere,
 * in a 44,000-page workspace).
 */
export function workspacePageListsOf<T extends InWorkspace>(pages: readonly T[], workspaceId: string): WorkspaceLists<T> {
  let byWorkspace = workspaceListsOf.get(pages);
  const kept = byWorkspace?.get(workspaceId);
  if (kept) return kept as WorkspaceLists<T>;
  const trashed = trashedPageIdsOf(pages);
  const last = lastWorkspaceLists;
  const edits = last && last.workspaceId === workspaceId && last.trashed === trashed ? listEditsOf(last.pages, pages) : null;
  let lists: WorkspaceLists<InWorkspace>;
  if (last && edits && edits.replaced.every(([was, now]) => last.pages[was].workspaceId === pages[now].workspaceId)) {
    lists = editedWorkspaceLists(last, pages, edits, workspaceId);
  } else {
    const live: InWorkspace[] = [], inTrash: InWorkspace[] = [];
    for (const page of pages) {
      if (page.workspaceId !== workspaceId) continue;
      if (!trashed.has(page.id)) live.push(page);
      else inTrash.push(page.inTrash ? page : { ...page, inTrash: true });
    }
    // A list of the workspace's live pages alone is its own live list, so
    // what is worked out once for a list serves both (pageMapOf and the like).
    lists = { live: live.length === pages.length ? pages as readonly InWorkspace[] as InWorkspace[] : live, trashed: inTrash };
  }
  if (!byWorkspace) {
    byWorkspace = new Map();
    workspaceListsOf.set(pages, byWorkspace);
  }
  byWorkspace.set(workspaceId, lists);
  lastWorkspaceLists = { pages, workspaceId, trashed, lists };
  return lists as WorkspaceLists<T>;
}
