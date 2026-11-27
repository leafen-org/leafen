import type { TextSpan } from "@/lib/types";
import { listEditsOf } from "./pageIndex";

/*
 * The one owner of the names and icons this screen shows of pages, with
 * pageNames.ts (its readers) and graphRowNames.ts (a page body's rows): every
 * place that names another page — a path, a sidebar row, a link, a mention, a
 * relation's chip, a filter's page, a search hit, a notice, a tab — takes the
 * name here, so a page renamed or given a new icon anywhere is named anew
 * everywhere at once, as Notion's one record of a page names it in every
 * place (user, 2026-11-20 on the NAS: a page made from a template was
 * titled, and the filter in its own body still named it "새 페이지" — the
 * body had read its row before the title was typed and held it).
 *
 * What it knows of a page comes from where the screen hears of pages: the
 * workspace's page list as it is drawn (setListedPages), the rows of each
 * page body drawn (graphRowNames.ts), this screen's own change of a page's
 * name (a row's title written in its page or a cell), the pages a read names
 * (a search, a page's preview), and a title being typed — shown wherever the
 * page is named at each key, without the list drawn again at each letter,
 * which drew the whole product, its sidebar, bar and page (2026-11-14:
 * 320–376 ms from a key to its frame in a 44,000-page workspace under the NAS
 * condition at CPU ×2); the list takes the title once saved, and a title
 * typed over the name known stands only while that name stands.
 *
 * Which is newer: the list keeps its own order (its reads, this screen's own
 * changes over them) and stands for its pages, read where it is, not copied
 * (a 44,000-page list changes at each title saved); a name noted otherwise
 * stands over the list's until the list's page changes, unless it is the
 * newer by the page's last edit; this screen's own change stands over a name
 * as old or older; a read stands only over an older one, so a body read
 * before a title was saved doesn't name the page as it was.
 */

/** What names a page where it is shown: its title, the spans spelling it, its icon. */
export type PageNameFields = {
  title: string;
  titleSpans?: readonly TextSpan[];
  icon?: string;
  iconType?: string;
};

/** A page as a place holds it: its ID and what it was named by when the place took it. */
export type HeldPageName = {
  id?: string;
  title?: string | null;
  titleSpans?: readonly TextSpan[] | null;
  icon?: string | null;
  iconType?: string | null;
  updatedAt?: string | null;
};

/** Where a name comes from apart from the list: this screen's own change, or a read of the server. */
export type PageNameSource = "local" | "read";

type Listed = HeldPageName & { id: string };
type Noted = { name: PageNameFields; at?: number };
type Typing = { title: string; over: string };

// The list's pages by ID, as it was last drawn.
let listed: readonly Listed[] | null = null;
const listIndex = new Map<string, Listed>();
// Names noted apart from the list, newer than its own.
const noted = new Map<string, Noted>();
const typed = new Map<string, Typing>();
// The page's version when each object a name was noted from was noted: a
// place holding that very object holds the name as it was then.
const notedAt = new WeakMap<object, number>();
// Each page's name changes, which its places read to know they are drawn anew.
const versions = new Map<string, number>();
let version = 0;
// The last changes, by version: a place drawn before them and told after
// learns which pages they named.
const LOGGED = 512;
let log: string[] = [];

const listeners = new Map<string, Set<() => void>>();
const anyListeners = new Set<(ids: ReadonlySet<string>, by: unknown) => void>();
let pending = new Set<string>();
let pendingBy: unknown = null;
let flushQueued = false;

/** The time of a page's last edit, as a number to compare. */
export function editedAt(updatedAt: string | null | undefined): number | undefined {
  if (!updatedAt) return undefined;
  const at = Date.parse(updatedAt);
  return Number.isNaN(at) ? undefined : at;
}

function sameSpans(left: readonly TextSpan[] | null | undefined, right: readonly TextSpan[] | null | undefined) {
  if (left === right) return true;
  if (!left?.length || !right?.length) return !left?.length && !right?.length;
  return JSON.stringify(left) === JSON.stringify(right);
}

/** Whether two names say the same. */
export function sameName(left: HeldPageName, right: HeldPageName) {
  return (left.title ?? "") === (right.title ?? "") && sameSpans(left.titleSpans, right.titleSpans)
    && (left.icon ?? "") === (right.icon ?? "") && (left.iconType ?? "none") === (right.iconType ?? "none");
}

function fieldsOf(name: HeldPageName): PageNameFields {
  return {
    title: name.title ?? "",
    ...(name.titleSpans?.length ? { titleSpans: name.titleSpans } : {}),
    ...(name.icon !== undefined && name.icon !== null ? { icon: name.icon } : {}),
    ...(name.iconType ? { iconType: name.iconType } : {}),
  };
}

/** The newest name known of the page: one noted apart from the list where it stands, else the list's. */
function newest(id: string): { name: HeldPageName; at: number | undefined; listed: boolean } | undefined {
  const own = noted.get(id), page = listIndex.get(id);
  if (own) return { name: own.name, at: own.at, listed: false };
  return page ? { name: page, at: editedAt(page.updatedAt), listed: true } : undefined;
}

function changed(id: string, by: unknown) {
  version += 1;
  versions.set(id, version);
  log.push(id);
  if (log.length > LOGGED * 2) log = log.slice(-LOGGED);
  if (pending.size && pendingBy !== by) pendingBy = undefined;
  else pendingBy = by;
  pending.add(id);
  if (!flushQueued) {
    flushQueued = true;
    queueMicrotask(flushPageNames);
  }
}

/** Tells every place naming the pages whose names changed; done at once where a change must show in its own frame. */
export function flushPageNames() {
  flushQueued = false;
  if (!pending.size) return;
  const ids = pending, by = pendingBy;
  pending = new Set();
  pendingBy = null;
  for (const id of ids) for (const listener of [...listeners.get(id) ?? []]) listener();
  for (const listener of [...anyListeners]) listener(ids, by);
}

/**
 * The list's page took the place of what was known of it: a name noted
 * apart from it goes unless it is the newer by the page's last edit.
 */
function listChanged(id: string, was: Listed | undefined, now: Listed | undefined, first = false) {
  const own = noted.get(id);
  if (own && now) {
    const at = editedAt(now.updatedAt);
    if (own.at !== undefined && at !== undefined && own.at > at) return;
    noted.delete(id);
    if (!sameName(own.name, now)) changed(id, undefined);
    return;
  }
  // The first list names pages nothing named before.
  if (own || first) return;
  if (!was || !now || !sameName(was, now)) changed(id, undefined);
}

/**
 * The workspace's pages as its list is drawn now. A list made from the last
 * one by new records of a few pages has only those taken again
 * (listEditsOf), as a 44,000-page workspace's list changes at each title
 * saved.
 */
export function setListedPages(pages: readonly Listed[]) {
  if (pages === listed) return;
  const before = listed, edits = listEditsOf(before, pages);
  const take = (page: Listed) => {
    const was = listIndex.get(page.id);
    if (was === page) return;
    listIndex.set(page.id, page);
    listChanged(page.id, was, page, !before);
  };
  if (before && edits) {
    for (const [, now] of edits.replaced) take(pages[now]);
    for (const page of edits.removed) {
      if (listIndex.get(page.id) !== page) continue;
      listIndex.delete(page.id);
      listChanged(page.id, page, undefined);
    }
    for (const now of edits.added) take(pages[now]);
  } else {
    const ids = new Set<string>();
    for (const page of pages) {
      ids.add(page.id);
      take(page);
    }
    for (const [id, page] of listIndex) {
      if (ids.has(id)) continue;
      listIndex.delete(id);
      listChanged(id, page, undefined);
    }
  }
  listed = pages;
}

/**
 * Notes what a page is named by apart from the list, from where (`from`):
 * this screen's own change stands over a name as old or older; a read only
 * over an older one, or as the first name known. `held` is the object it
 * was read from, which a place holding it holds as named then; `by` who
 * notes it, whose own notes it isn't told of. True when the name was taken.
 */
export function notePageName(id: string, given: HeldPageName, from: PageNameSource, held?: object, by?: unknown) {
  const at = editedAt(given.updatedAt);
  const was = newest(id);
  // A name given without its icon keeps the icon known.
  const name = given.iconType === undefined || given.iconType === null
    ? { ...given, icon: was?.name.icon, iconType: was?.name.iconType } : given;
  if (was && sameName(was.name, name)) {
    const own = noted.get(id);
    if (own && at !== undefined && (own.at === undefined || at > own.at)) own.at = at;
    if (held) notedAt.set(held, versions.get(id) ?? 0);
    return false;
  }
  if (was) {
    if (from === "local" && at !== undefined && was.at !== undefined && at < was.at) return false;
    if (from === "read" && (at === undefined || was.at === undefined || at <= was.at)) return false;
  }
  noted.set(id, { name: fieldsOf(name), at: at ?? was?.at });
  changed(id, by);
  if (held) notedAt.set(held, versions.get(id) ?? 0);
  return true;
}

/**
 * The name to show for a page a place holds, where one newer than the
 * place's is known — the name known since that very object was noted, else
 * one of the same last edit or later — and the title being typed for it
 * over that name; undefined where the place's own stands.
 */
export function newerPageName(held: HeldPageName): PageNameFields | undefined {
  const id = held.id;
  if (!id) return undefined;
  const was = newest(id);
  let name: HeldPageName | undefined;
  if (was && was.name !== held && !sameName(was.name, held)) {
    const at = notedAt.get(held as object);
    const heldAt = editedAt(held.updatedAt);
    // Newer: named since that very object was noted; noted apart from the
    // list over the list's own page, which it stands over; else by the last edit.
    const newer = at !== undefined ? (versions.get(id) ?? 0) > at
      : !was.listed && listIndex.get(id) === held ? true
      : heldAt === undefined || was.at === undefined || was.at >= heldAt;
    if (newer) name = was.name;
  }
  const typing = typed.get(id), base = name ?? held;
  if (typing && typing.over === (base.title ?? "") && typing.title !== (base.title ?? "")) {
    // A name typed is plain words.
    return { title: typing.title, icon: base.icon ?? undefined, iconType: base.iconType ?? undefined };
  }
  return name ? fieldsOf(name) : undefined;
}

/** Whether any name of the page is known: the list's, or one noted apart from it. */
export function pageNameKnown(id: string) {
  return noted.has(id) || listIndex.has(id);
}

/** Whether the object was noted as a page's name (a place holding it holds a name the owner knew). */
export function notedPageName(held: object) {
  return notedAt.has(held);
}

/** The page's name as known — the list's, or one noted apart from it — where it is. */
export function knownPageName(id: string): (PageNameFields & { listed: boolean }) | undefined {
  const was = newest(id);
  return was ? { ...fieldsOf(was.name), listed: was.listed } : undefined;
}

/** How many times the page's name or its typed title changed: a place naming it draws again when this does. */
export function pageNameVersion(id: string) {
  return versions.get(id) ?? 0;
}

/** How many names changed in all. */
export function pageNamesVersion() {
  return version;
}

/** The pages whose names changed since `since` (pageNamesVersion), null where too many did to say. */
export function pageNameChangesSince(since: number): readonly string[] | null {
  if (since >= version) return [];
  return version - since > log.length ? null : log.slice(log.length - (version - since));
}

/** Hears the page's name, or the title typed for it, change. */
export function followPageName(id: string, listener: () => void): () => void {
  let set = listeners.get(id);
  if (!set) listeners.set(id, set = new Set());
  set.add(listener);
  return () => {
    set.delete(listener);
    if (!set.size) listeners.delete(id);
  };
}

/** Hears every change of names, the pages' IDs with who noted them where one did all. */
export function followPageNames(listener: (ids: ReadonlySet<string>, by: unknown) => void): () => void {
  anyListeners.add(listener);
  return () => { anyListeners.delete(listener); };
}

/** A title typed for a page whose name is now `over`; shown wherever the page is named. */
export function showTypedPageTitle(pageId: string, title: string, over: string) {
  const was = typed.get(pageId);
  if (title === over) {
    if (!was) return;
    typed.delete(pageId);
  } else {
    if (was && was.title === title && was.over === over) return;
    typed.set(pageId, { title, over });
  }
  changed(pageId, undefined);
  flushPageNames();
}

/** A title the server refused: wherever the page is named it is named as it was again. */
export function dropTypedPageTitle(pageId: string) {
  if (!typed.delete(pageId)) return;
  changed(pageId, undefined);
  flushPageNames();
}

/** The title being typed for the page, whatever name it is typed over. */
export function typingPageTitle(pageId: string): string | undefined {
  return typed.get(pageId)?.title;
}

/** The title typed for the page over the name `over`, if one is being typed. */
export function typedPageTitle(pageId: string, over: string): string | undefined {
  const held = typed.get(pageId);
  return held && held.over === over ? held.title : undefined;
}
