import type { Bootstrap } from "@/types";
import { withoutPages } from "./bootstrapCollection";
import { pageMapOf } from "./pageIndex";

// The pages of a workspace made since the newest one a screen's list holds,
// read ahead of the whole list when another screen made one
// (bootstrap_new_pages.go): shown at once, as Notion's sidebar shows another
// screen's new page within a second, while the whole read follows for every
// other change.

// A moment as the server writes pages' created ("2026-11-14 13:47:18.579Z");
// a page made on this device and not yet answered for has its own clock's.
const SERVER_MOMENT = /^\d{4}-\d\d-\d\d \d\d:\d\d:\d\d/;

/** The newest moment a page of the workspace the server listed was made, or "" when it lists none. */
export function newestListedMoment(list: Bootstrap, workspaceId: string): string {
  let newest = "";
  for (const page of list.pages) {
    const made = page.createdAt ?? "";
    if (page.workspaceId === workspaceId && page.cursor > 0 && made > newest && SERVER_MOMENT.test(made)) newest = made;
  }
  return newest;
}

/** The list with pages read ahead of it, as read then, with their favorites, sharing and databases; `onlyNew` adds only those it lacks. */
export function withNewPages(list: Bootstrap, read: Bootstrap, onlyNew = false): Bootstrap {
  const listed = onlyNew ? new Set(list.pages.map(({ id }) => id)) : null;
  const pages = listed ? read.pages.filter(({ id }) => !listed.has(id)) : read.pages;
  if (!pages.length) return list;
  const ids = new Set(pages.map(({ id }) => id));
  const kept = listed ? list : withoutPages(list, ids);
  const of = <Item extends { pageId: string }>(items: Item[] | undefined) => (items ?? []).filter(({ pageId }) => ids.has(pageId));
  const shared = (read.sharedPageIds ?? []).filter((id) => ids.has(id));
  return {
    ...kept,
    pages: [...kept.pages, ...pages],
    pageFavorites: [...(kept.pageFavorites ?? []), ...of(read.pageFavorites)],
    databaseFavorites: [...(kept.databaseFavorites ?? []), ...of(read.databaseFavorites)],
    inlineDatabases: [...(kept.inlineDatabases ?? []), ...of(read.inlineDatabases)],
    ...(kept.sharedPageIds || shared.length ? { sharedPageIds: [...(kept.sharedPageIds ?? []), ...shared] } : {}),
  };
}

/**
 * The list with the pages of a workspace changed since it was read
 * (bootstrap_page_changes.go): each changed page as read then, in the place
 * the list held it (a page it lacked at its end), with its favorites,
 * sharing and databases, those removed gone, and the list's change number
 * and the workspace's token as of that read. Over a whole read that began
 * before them (`over`), a page the whole read holds at a later revision
 * stays as it is there. Kept in place, a page renamed, moved or put in the
 * trash elsewhere leaves the list's other pages where they were, so what is
 * worked out once for a list takes only that page again (listEditsOf):
 * moved to the end, it made every place that does so go over all 47,000
 * pages again (2026-11-15).
 */
export function withChangedPages(list: Bootstrap, changes: Bootstrap, workspaceId: string, token: string, over = false): Bootstrap {
  const listed = over ? pageMapOf(list.pages) : null;
  const pages = listed
    ? changes.pages.filter((page) => (listed.get(page.id)?.revision ?? -1) <= page.revision)
    : changes.pages;
  // A database page the list doesn't carry comes with its favorite alone
  // (bootstrap_page_changes.go): that favorite is as read then too.
  const ids = new Set([...pages.map(({ id }) => id), ...(changes.removedPageIds ?? []), ...rowFavoriteIds(changes)]);
  // Its parts but the pages without the changed pages' (the pages keep their places below).
  const kept = ids.size ? withoutPages({ ...list, pages: [] }, ids) : list;
  const of = <Item extends { pageId: string }>(items: Item[] | undefined) => (items ?? []).filter(({ pageId }) => ids.has(pageId));
  const shared = (changes.sharedPageIds ?? []).filter((id) => ids.has(id));
  const wide = (changes.workspaceWidePageIds ?? []).filter((id) => ids.has(id));
  return {
    ...kept,
    pages: pagesChangedInPlace(list.pages, pages, changes.removedPageIds),
    pageFavorites: [...(kept.pageFavorites ?? []), ...of(changes.pageFavorites)],
    databaseFavorites: [...(kept.databaseFavorites ?? []), ...of(changes.databaseFavorites)],
    inlineDatabases: [...(kept.inlineDatabases ?? []), ...of(changes.inlineDatabases)],
    ...(kept.sharedPageIds || shared.length ? { sharedPageIds: [...(kept.sharedPageIds ?? []), ...shared] } : {}),
    ...(kept.workspaceWidePageIds || wide.length ? { workspaceWidePageIds: [...(kept.workspaceWidePageIds ?? []), ...wide] } : {}),
    // Over an older whole read, the list keeps that read's number and token:
    // its change check reads again what changed since.
    ...(over ? {} : {
      pageChangeSeq: changes.pageChangeSeq,
      workspaceRefreshTokens: [
        ...kept.workspaceRefreshTokens.filter((item) => item.workspaceId !== workspaceId),
        { workspaceId, refreshToken: token },
      ],
    }),
  };
}

/** The list's pages with the changed ones in their places, those it lacks at its end, the removed ones gone. */
function pagesChangedInPlace(listed: Bootstrap["pages"], changed: Bootstrap["pages"], removed: readonly string[] = []): Bootstrap["pages"] {
  if (!changed.length && !removed.length) return listed;
  const byId = new Map(changed.map((page) => [page.id, page]));
  const gone = new Set(removed);
  const pages: Bootstrap["pages"] = [];
  for (const page of listed) {
    if (gone.has(page.id)) continue;
    const now = byId.get(page.id);
    if (now) byId.delete(page.id);
    pages.push(now ?? page);
  }
  for (const page of byId.values()) pages.push(page);
  return pages;
}

/** The pages whose favorite alone the changes bring: database pages the list doesn't carry. */
function rowFavoriteIds(changes: Bootstrap): string[] {
  const pages = new Set(changes.pages.map(({ id }) => id));
  return (changes.pageFavorites ?? []).map(({ pageId }) => pageId).filter((id) => !pages.has(id));
}

/** Whether the list already shows the changed pages as read, with their favorites, sharing and databases. */
export function sameChangedPages(list: Bootstrap, changes: Bootstrap): boolean {
  const listed = pageMapOf(list.pages);
  const ids = new Set([...changes.pages.map(({ id }) => id), ...rowFavoriteIds(changes)]);
  const same = <Item>(a: Item | undefined, b: Item | undefined) => JSON.stringify(a) === JSON.stringify(b);
  const of = <Item extends { pageId: string }>(items: Item[] | undefined) => (items ?? []).filter(({ pageId }) => ids.has(pageId));
  const marked = (items: string[] | undefined) => (items ?? []).filter((id) => ids.has(id)).sort();
  return changes.pages.every((page) => same(listed.get(page.id), page))
    && same(of(list.pageFavorites), of(changes.pageFavorites))
    && same(of(list.databaseFavorites), of(changes.databaseFavorites))
    && same(of(list.inlineDatabases), of(changes.inlineDatabases))
    && same(marked(list.sharedPageIds), marked(changes.sharedPageIds))
    && same(marked(list.workspaceWidePageIds), marked(changes.workspaceWidePageIds));
}
