import { useEffect, useMemo, useState, useSyncExternalStore } from "react";
import { api } from "@/api";
import type { LibraryViewId } from "@/lib/router";
import type { Session } from "@/types";
import {
  workspaceMembershipBounds,
  type LibraryPage,
  type PageVisit,
} from "@/workspaceMembershipWire.generated";
import { isLibraryFilterColumn, type LibraryQuickFilter } from "./libraryFilters";
import { isLibraryGroupColumn, type LibraryGroupColumn } from "./libraryGrouping";
import { parseStored, personValue, storePersonValue } from "@/lib/browserStorage";
import { isServerUnreachable, retryWhenServerMayAnswer } from "@/lib/serverReach";
import { subscribeVisitsHere, visitsHereNow, withVisitsHere } from "@/lib/pageVisitsHere";
import { LIBRARY_VIEW_ORDER_KEY, readLibraryViewOrder } from "./libraryViewOrder";
import type { FilterGroup } from "@/lib/types";
import { DEFAULT_LIBRARY_COLUMNS, LIBRARY_COLUMNS, type LibraryColumn, type LibrarySort } from "./libraryModel";

const PAGE_VISITS_CHANGED = "leafen:page-visits-changed";

/** Has every list of the reader's visits read them again, as after "최근 항목에서 제거". */
export function announcePageVisitsChanged() {
  window.dispatchEvent(new Event(PAGE_VISITS_CHANGED));
}

/**
 * The reader's visits as read: `ready` once the server gave them (an empty
 * list then means there are none), `loading` before its first answer,
 * `offline` while it doesn't answer and none were read this visit, `failed`
 * when it refused and none were read. `readAt` is when the list shown was
 * asked for (0 before any).
 */
export type PageVisitsRead = { state: "loading" | "ready" | "offline" | "failed"; list: PageVisit[]; readAt: number };

// The visits last read by each reader in each workspace this visit, shown
// while the server doesn't answer, as Home's "최근 편집" (HomeRecents.tsx).
const visitsLastRead = new Map<string, { list: PageVisit[]; readAt: number }>();

/**
 * The reader's latest visits in the workspace, newest first, read again
 * when the refresh key (such as the page open) changes or the visits do,
 * with whether they were read: a list not read isn't an empty one
 * (2026-11-14: a guest's Home, reloaded offline, said no page was visited).
 */
export function usePageVisitsRead(session: Session | null, workspaceId: string, refreshKey: string | null = null): PageVisitsRead {
  // Without a reader nothing is read (a menu with no page behind it, mentionLead.ts).
  const key = `${session?.user.id ?? ""}\u0000${workspaceId}`;
  const [visits, setVisits] = useState<PageVisitsRead & { of: string }>(() => {
    const kept = visitsLastRead.get(key);
    return { of: key, state: kept ? "ready" : "loading", list: kept?.list ?? [], readAt: kept?.readAt ?? 0 };
  });
  const [changes, setChanges] = useState(0);
  useEffect(() => {
    const changed = () => setChanges((count) => count + 1);
    window.addEventListener(PAGE_VISITS_CHANGED, changed);
    return () => window.removeEventListener(PAGE_VISITS_CHANGED, changed);
  }, []);
  useEffect(() => {
    if (!session) return undefined;
    const controller = new AbortController();
    let stopRetry: (() => void) | undefined;
    const readAt = Date.now();
    api.readPageVisits(session, workspaceId, controller.signal)
      .then((read) => {
        visitsLastRead.set(key, { list: read.visits, readAt });
        setVisits({ of: key, state: "ready", list: read.visits, readAt });
      })
      .catch((error: unknown) => {
        if (controller.signal.aborted) return;
        // The visits read stay. Without the server they are read again once
        // it may answer (as Home's other lists, HomeMyTasks.tsx).
        const unreachable = isServerUnreachable(error);
        const kept = visitsLastRead.get(key);
        setVisits((current) => current.of === key && current.state === "ready" ? current
          : kept ? { of: key, state: "ready", ...kept }
            : { of: key, state: unreachable ? "offline" : "failed", list: [], readAt: 0 });
        if (unreachable) stopRetry = retryWhenServerMayAnswer(() => setChanges((count) => count + 1));
      });
    return () => {
      controller.abort();
      stopRetry?.();
    };
  }, [changes, key, refreshKey, session, workspaceId]);
  if (visits.of === key) return visits;
  const kept = visitsLastRead.get(key);
  return { state: kept ? "ready" : "loading", list: kept?.list ?? [], readAt: kept?.readAt ?? 0 };
}

/** The reader's latest visits in the workspace as read so far (usePageVisitsRead). */
export function usePageVisits(session: Session | null, workspaceId: string, refreshKey: string | null = null) {
  return usePageVisitsRead(session, workspaceId, refreshKey).list;
}

/**
 * The reader's visits as usePageVisitsRead reads them, with the visits made
 * on this device that the server hadn't kept when they were read first
 * (lib/pageVisitsHere.ts): a page opened a moment ago is visited at once, as
 * Notion's Home lists it (seen 2026-11-22 in two Chrome tabs, opened and left
 * within a second) and its move window suggests it. Lists that don't follow
 * Notion so read usePageVisitsRead.
 */
export function usePageVisitsReadNow(session: Session | null, workspaceId: string, refreshKey: string | null = null): PageVisitsRead {
  const read = usePageVisitsRead(session, workspaceId, refreshKey);
  const here = useSyncExternalStore(subscribeVisitsHere, visitsHereNow, visitsHereNow);
  const userId = session?.user.id;
  const list = useMemo(() => userId ? withVisitsHere(read.list, read.readAt, userId, workspaceId, here) : read.list,
    [here, read.list, read.readAt, userId, workspaceId]);
  return list === read.list ? read : { ...read, list };
}

/** The reader's visits as they are now (usePageVisitsReadNow). */
export function usePageVisitsNow(session: Session | null, workspaceId: string, refreshKey: string | null = null) {
  return usePageVisitsReadNow(session, workspaceId, refreshKey).list;
}

/**
 * Who made the pages shown and when the reader last visited them, read for
 * the pages not read yet, as many at once as the server takes.
 */
export function useLibraryPages(session: Session | null, workspaceId: string, pageIds: readonly string[]) {
  const [read, setRead] = useState<{ workspaceId: string; pages: Map<string, LibraryPage> }>(
    () => ({ workspaceId, pages: new Map() }),
  );
  const [failed, setFailed] = useState(false);
  const known = read.workspaceId === workspaceId ? read.pages : null;
  const missing = pageIds.filter((id) => !known?.has(id)).slice(0, workspaceMembershipBounds.libraryPagesLimit);
  const missingKey = missing.join("\u0000");
  useEffect(() => {
    if (missing.length === 0 || !session) return undefined;
    const controller = new AbortController();
    api.readLibraryPages(session, workspaceId, missing, controller.signal)
      .then((result) => {
        setFailed(false);
        setRead((current) => {
          const pages = new Map(current.workspaceId === workspaceId ? current.pages : []);
          // A page the server doesn't give back is remembered as read, with nothing to show.
          for (const id of missing) pages.set(id, { pageId: id, creatorName: "", lastEditorName: "", visitedAt: "" });
          for (const page of result.pages) pages.set(page.pageId, page);
          return { workspaceId, pages };
        });
      })
      .catch(() => {
        if (!controller.signal.aborted) setFailed(true);
      });
    return () => controller.abort();
    // The pages to read are the ones missingKey names.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [missingKey, session, workspaceId]);
  return { pages: known ?? new Map<string, LibraryPage>(), failed };
}

/**
 * A library view's sort, wrapped columns, shown columns in their order,
 * filters and grouping, as its column menus, "속성 표시 여부" and filter bar
 * set them.
 */
export type LibraryViewSettings = {
  sort: LibrarySort | null;
  /** The sorts after the first, which settle what it leaves even. */
  moreSorts?: LibrarySort[];
  wrap: LibraryColumn[];
  columns: LibraryColumn[];
  /** The columns hidden since the view opened first, the last one hidden first (libraryHiddenColumns). */
  hidden?: LibraryColumn[];
  filters: LibraryQuickFilter[];
  /** The advanced filter's group of rules and groups, applied with the quick filters. */
  filterGroup: FilterGroup | null;
  groupBy: LibraryGroupColumn | null;
  /** "보관된 페이지 포함": the archived pages listed too, where the view offers it (librarySettingsItems). */
  includeArchived?: boolean;
  /** Kept since the view's sort became one with its sidebar section's (useLibrarySectionSort). */
  section?: boolean;
};

const defaultSettings = (columns: readonly LibraryColumn[]): LibraryViewSettings => ({
  sort: null, wrap: [], columns: [...columns], filters: [], filterGroup: null, groupBy: null,
});

/** A stored filter on a column a filter reads; a group from elsewhere is left out. */
function storedFilters(list: unknown): LibraryQuickFilter[] {
  if (!Array.isArray(list)) return [];
  return list.filter((filter): filter is LibraryQuickFilter => typeof filter === "object" && filter !== null
    && typeof (filter as { propertyId?: unknown }).propertyId === "string"
    && isLibraryFilterColumn((filter as { propertyId: string }).propertyId)
    && typeof (filter as { operator?: unknown }).operator === "string");
}

const LIBRARY_VIEW_KEY = "leafen:library-view:v1";

function readLibraryViewSettings(userId: string, place: string, columnsAtFirst: readonly LibraryColumn[],
  sortAtFirst: LibrarySort | null): LibraryViewSettings {
  try {
    const stored = parseStored(personValue(LIBRARY_VIEW_KEY, {
      userId, part: place, previous: [`${LIBRARY_VIEW_KEY}:${userId}:${place}`],
    })) as Partial<LibraryViewSettings> | null;
    const sortable = LIBRARY_COLUMNS.filter((column) => column !== "source") as readonly string[];
    const valid = (one: LibrarySort | null | undefined) => Boolean(one && sortable.includes(one.column)
      && (one.direction === "ascending" || one.direction === "descending"));
    // A view never sorted here has its own sort; one whose sort was taken off keeps none.
    const sort = stored && "sort" in stored ? valid(stored.sort) ? stored.sort! : null : sortAtFirst;
    const moreSorts = Array.isArray(stored?.moreSorts) ? stored.moreSorts.filter(valid) : [];
    const known = (list: unknown) => Array.isArray(list)
      ? [...new Set(list.filter((column): column is LibraryColumn => LIBRARY_COLUMNS.includes(column)))] : null;
    const wrap = known(stored?.wrap) ?? [];
    // The page name always shows, first.
    const shown = known(stored?.columns);
    const columns: LibraryColumn[] = shown ? ["name", ...shown.filter((column) => column !== "name")] : [...columnsAtFirst];
    const hidden = known(stored?.hidden)?.filter((column) => !columns.includes(column)) ?? [];
    const groupBy = typeof stored?.groupBy === "string" && isLibraryGroupColumn(stored.groupBy) ? stored.groupBy : null;
    const group = stored?.filterGroup;
    const filterGroup = group && typeof group === "object" && Array.isArray(group.filters) ? group : null;
    return { sort, moreSorts, wrap, columns, hidden, filters: storedFilters(stored?.filters), filterGroup, groupBy,
      includeArchived: stored?.includeArchived === true, ...(stored?.section === true ? { section: true } : {}) };
  } catch {
    return { ...defaultSettings(columnsAtFirst), sort: sortAtFirst };
  }
}

/**
 * A library view's settings for this person in this browser, kept for the
 * next visit (Notion keeps a view's sort with the view).
 */
export function useLibraryViewSettings(userId: string, place: string, columnsAtFirst: readonly LibraryColumn[] = DEFAULT_LIBRARY_COLUMNS,
  sortAtFirst: LibrarySort | null = null) {
  const key = `${userId}:${place}`;
  const [state, setState] = useState(() => ({ key, settings: readLibraryViewSettings(userId, place, columnsAtFirst, sortAtFirst) }));
  const settings = state.key === key ? state.settings : readLibraryViewSettings(userId, place, columnsAtFirst, sortAtFirst);
  function change(next: LibraryViewSettings) {
    setState({ key, settings: next });
    // Without storage the setting lasts while the library is open.
    storePersonValue(LIBRARY_VIEW_KEY, JSON.stringify(next), { userId, part: place });
  }
  return [settings, change] as const;
}

/**
 * The reader's order of the library's views in a workspace, as the list of
 * views' handles set it (Notion keeps it with the library), kept for this
 * person in this browser as the views' settings are.
 */
export function useLibraryViewOrder(userId: string, workspaceId: string, views: readonly LibraryViewId[]) {
  const key = `${userId}:${workspaceId}:${views.join(",")}`;
  const [state, setState] = useState(() => ({ key, order: readLibraryViewOrder(userId, workspaceId, views) }));
  const order = state.key === key ? state.order : readLibraryViewOrder(userId, workspaceId, views);
  function move(view: LibraryViewId, target: LibraryViewId, after: boolean) {
    if (view === target) return;
    const without = order.filter((entry) => entry !== view);
    const at = without.indexOf(target);
    if (at < 0) return;
    const next = [...without.slice(0, at + (after ? 1 : 0)), view, ...without.slice(at + (after ? 1 : 0))];
    setState({ key, order: next });
    storePersonValue(LIBRARY_VIEW_ORDER_KEY, JSON.stringify(next), { userId, part: workspaceId });
  }
  return [order, move] as const;
}
