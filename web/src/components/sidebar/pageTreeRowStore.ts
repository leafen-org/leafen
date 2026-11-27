import { useLayoutEffect, useReducer, useRef } from "react";
import type { Page } from "@/types";
import type { DatabaseFavoriteProjection, InlineDatabaseProjection } from "@/workspaceMembershipWire.generated";
import { childTreePages } from "./pageTreeModel";

/**
 * What changes under the sidebar's rows: the open page, the rows opened, the
 * page list, the favorites and the databases pages hold. It is kept apart
 * from the tree's context, which every row would draw again for, so each row
 * reads its own part and draws again only when that part changes: opening a
 * page draws the row closed and the row opened, a rename the renamed row.
 */
export type PageTreeState = {
  pages: Page[];
  pagesById: Record<string, Page>;
  selectedPageId: string | null;
  expandedPageIds: ReadonlySet<string>;
  favoritePageIds: ReadonlySet<string>;
  inlineDatabases: readonly InlineDatabaseProjection[];
  databaseFavorites: readonly DatabaseFavoriteProjection[];
};

export type PageTreeStore = ReturnType<typeof createPageTreeStore>;

export function createPageTreeStore(initial: PageTreeState) {
  let state = initial;
  const listeners = new Set<() => void>();
  return {
    get: () => state,
    /** The tree's state as it commits, so a row reads only what is drawn. */
    publish(next: PageTreeState) {
      if (next === state) return;
      state = next;
      for (const listener of [...listeners]) listener();
    },
    subscribe(listener: () => void) {
      listeners.add(listener);
      return () => { listeners.delete(listener); };
    },
  };
}

function sameItems<T>(left: readonly T[], right: readonly T[], same: (a: T, b: T) => boolean = Object.is) {
  if (left === right) return true;
  if (left.length !== right.length) return false;
  for (let i = 0; i < left.length; i++) if (!same(left[i]!, right[i]!)) return false;
  return true;
}

/** The same values field by field, a list's item by item: a page read again from the server is the page it was. */
function sameFields<T extends object>(left: T | undefined, right: T | undefined) {
  if (left === right) return true;
  if (!left || !right) return false;
  const keys = Object.keys(left) as (keyof T)[];
  if (keys.length !== Object.keys(right).length) return false;
  for (const key of keys) {
    const a = left[key], b = right[key];
    if (a === b) continue;
    if (!Array.isArray(a) || !Array.isArray(b) || !sameItems(a, b)) return false;
  }
  return true;
}

/**
 * A part of the tree's state for one row, drawn again when `same` says it
 * changed. The row reads the state the tree last committed, and listens from
 * its layout effect, which runs before the tree's own: a change committed
 * with the row's drawing reaches it before the screen is painted.
 */
function usePageTreeSelection<T>(store: PageTreeStore, select: (state: PageTreeState) => T, same: (a: T, b: T) => boolean): T {
  const [, redraw] = useReducer((count: number) => count + 1, 0);
  const drawn = useRef<{ value: T } | null>(null);
  const next = select(store.get());
  const value = drawn.current && same(drawn.current.value, next) ? drawn.current.value : next;
  const latest = useRef({ select, same, value });
  useLayoutEffect(() => {
    latest.current = { select, same, value };
    drawn.current = { value };
  });
  useLayoutEffect(() => {
    const check = () => {
      const { select: read, same: equal, value: shown } = latest.current;
      if (!equal(shown, read(store.get()))) redraw();
    };
    check();
    return store.subscribe(check);
  }, [store]);
  return value;
}

/** An inline database under its page's row, with whether it is a favorite. */
export type PageTreeRowDatabase = InlineDatabaseProjection & { favorite: boolean; revision: number };

export type PageTreeRowView = {
  page: Page | undefined;
  /** The pages under it, in the tree's order. */
  childIds: readonly string[];
  databases: readonly PageTreeRowDatabase[];
  active: boolean;
  expanded: boolean;
  favorite: boolean;
  /** The page list, while the row's menu is open and moves or copies within it. */
  menuPages: Page[] | null;
};

const NO_IDS: readonly string[] = [];
const NO_DATABASES: readonly PageTreeRowDatabase[] = [];
const databasesOfList = new WeakMap<readonly InlineDatabaseProjection[], ReadonlyMap<string, readonly InlineDatabaseProjection[]>>();

// The databases each page holds in its body, made once per list.
function pageDatabases(databases: readonly InlineDatabaseProjection[], pageId: string) {
  let byPage = databasesOfList.get(databases);
  if (!byPage) {
    const grouped = new Map<string, InlineDatabaseProjection[]>();
    for (const database of databases) {
      const held = grouped.get(database.pageId);
      if (held) held.push(database);
      else grouped.set(database.pageId, [database]);
    }
    byPage = grouped;
    databasesOfList.set(databases, byPage);
  }
  return byPage.get(pageId);
}

function sameRowView(left: PageTreeRowView, right: PageTreeRowView) {
  return left.active === right.active && left.expanded === right.expanded
    && left.favorite === right.favorite && left.menuPages === right.menuPages
    && sameFields(left.page, right.page) && sameItems(left.childIds, right.childIds)
    && sameItems(left.databases, right.databases, sameFields);
}

/** What one sidebar row draws from the tree's state. */
export function usePageTreeRow(store: PageTreeStore, input: {
  pageId: string;
  expansionKey: string;
  menuOpen: boolean;
}): PageTreeRowView {
  const { pageId, expansionKey, menuOpen } = input;
  return usePageTreeSelection(store, (state) => {
    const page = state.pagesById[pageId];
    const children = page ? childTreePages(state.pages, pageId) : [];
    const childIds = children.length === 0 ? NO_IDS : children.map(({ id }) => id);
    const held = pageDatabases(state.inlineDatabases, pageId);
    const databases = held ? held.map((database) => ({
      ...database,
      favorite: state.databaseFavorites.some((item) => item.containerId === database.containerId && item.favorite),
      revision: state.databaseFavorites.find((item) => item.containerId === database.containerId)?.revision ?? 0,
    })) : NO_DATABASES;
    const active = state.selectedPageId === pageId;
    return {
      page,
      childIds,
      databases,
      active,
      expanded: state.expandedPageIds.has(expansionKey),
      favorite: state.favoritePageIds.has(pageId),
      menuPages: menuOpen ? state.pages : null,
    };
  }, sameRowView);
}
