import { createContext, useCallback, useContext, useEffect, useMemo, useState, type RefObject } from "react";
import { parseStored, personValue, storePersonValue } from "@/lib/browserStorage";
import type { DatabaseToolbarCommands } from "./databaseToolbarModel";

/**
 * A view's filter bar as its toolbar's "필터" reaches it, as Notion's (seen
 * 2026-09-30): while the view has filters the button shows or hides the bar,
 * and a property picked from the button's list adds its pill to the bar with
 * the editor open.
 */
export type DatabaseFilterBarSurface = {
  shown: boolean;
  /** Shown with nothing in it but "+ 필터": its filters cleared while it was open, or gone from a bar the filter button opened. */
  keptEmpty: boolean;
  toggle(): void;
  /** `from`: the toolbar button whose list it came from, which the focus goes back to as its editor closes. */
  add(propertyId: string, from?: HTMLElement | null): void;
  /** Opens the sorts' editor at their pill, as a sort just picked from the toolbar does. */
  editSorts(from?: HTMLElement | null): void;
  /** Opens the advanced filter's rules at its pill, as "고급 필터 추가" from the toolbar does. */
  editAdvanced(from?: HTMLElement | null): void;
};

/** A property's pill asked to open, and the toolbar button it came from (DatabaseFilterBarSurface.add). */
export type DatabaseFilterBarRequest = { propertyId: string; at: number; from?: HTMLElement | null };

export const DatabaseFilterBarContext = createContext<DatabaseFilterBarSurface | null>(null);

export function useDatabaseFilterBar() {
  return useContext(DatabaseFilterBarContext);
}

const SHOWN_BARS_KEY = "leafen:v2:shown-filter-bars";
const HIDDEN_BARS_KEY = "leafen:v2:hidden-filter-bars";

/**
 * Whether its views' bars start open, where a page decides it: People's
 * show their sort and filter pills before any change ("↑ Name ·
 * Membership Type: 선택 값…"), as Notion's People does (seen 2026-11-15 in
 * two free workspaces' People, never opened there before), a viewer's
 * filter button closing one for them across visits.
 */
export const DatabaseFilterBarsOpenContext = createContext(false);

function storedBars(key: string): string[] {
  const value = parseStored(personValue(key, { previous: [key] }));
  return Array.isArray(value) ? value.filter((id): id is string => typeof id === "string") : [];
}

/**
 * Whether the viewer opened a view's filter bar with the toolbar's filter
 * button. Every bar starts closed, whether its filters came from the app or
 * the API (Notion's, seen 2026-11-11: closed again after a reload even for a
 * filter the viewer added), and stays open across visits for this viewer
 * alone once the button opens it, as Notion keeps it in the browser per
 * person and view — but where its views' bars start open (People's) one
 * stays closed once the button closes it (Notion's, seen 2026-11-15 after a
 * reload).
 */
function useFilterBarOpened(viewId: string, openAtFirst: boolean) {
  const key = openAtFirst ? HIDDEN_BARS_KEY : SHOWN_BARS_KEY;
  const read = useCallback(() => storedBars(key).includes(viewId) !== openAtFirst, [key, openAtFirst, viewId]);
  const [opened, setOpened] = useState(read);
  useEffect(() => setOpened(read()), [read]);
  const change = useCallback((next: boolean) => {
    setOpened(next);
    // Without storage the bar still opens or closes for this visit.
    const others = storedBars(key).filter((id) => id !== viewId);
    storePersonValue(key, JSON.stringify(next !== openAtFirst ? [...others, viewId].slice(-200) : others));
  }, [key, openAtFirst, viewId]);
  return [opened, change] as const;
}

/**
 * Whether a view's bar, once it held filters or sorts, stays open with "+ 필터"
 * after they are cleared — reset or removed — until the viewer closes it from
 * the toolbar, goes to another view or opens the page again (Notion's, seen
 * 2026-11-06: gone after a reload and after another view). A bar the filter
 * button opened stays so across visits instead (useDatabaseFilterBarState).
 */
function useFilterBarKept(viewId: string) {
  const [keptView, setKeptView] = useState<string | null>(null);
  // Another view lets it go; the new view's own bar may already have said it holds filters.
  useEffect(() => setKeptView((current) => current === viewId ? current : null), [viewId]);
  const change = useCallback((next: boolean) => setKeptView(next ? viewId : null), [viewId]);
  return [keptView === viewId, change] as const;
}

/**
 * A view's filter bar as its toolbar and column menus reach it: hidden or
 * shown, a property's pill asked to open — on the toolbar's sheet where it
 * opens there (a phone) — and the sorts' or the advanced filter's editor asked to open.
 */
export function useDatabaseFilterBarState(view: { id: string },
  commands: RefObject<DatabaseToolbarCommands | null>, unsavedAtOpen = false) {
  const [filterRequest, setFilterRequest] = useState<DatabaseFilterBarRequest | null>(null);
  const [sortRequest, setSortRequest] = useState<{ at: number; from?: HTMLElement | null } | 0>(0);
  const [advancedRequest, setAdvancedRequest] = useState<{ at: number; from?: HTMLElement | null } | 0>(0);
  const openAtFirst = useContext(DatabaseFilterBarsOpenContext);
  const [opened, setOpened] = useFilterBarOpened(view.id, openAtFirst);
  // A filter or sort the viewer adds opens the bar for this visit only: it
  // is closed again after a reload (Notion's, seen 2026-11-11).
  const [openedHere, setOpenedHere] = useState<string | null>(null);
  // A view opened with the viewer's own unsaved filters starts with its bar
  // folded, the filter button's dot telling of them, until the button or a
  // filter opens it (Notion's, seen 2026-11-07 after a reload).
  const [folded, setFolded] = useState<string | null>(() => unsavedAtOpen ? view.id : null);
  const hidden = !(opened || openedHere === view.id) || folded === view.id;
  const [keptHere, setKept] = useFilterBarKept(view.id);
  // A bar the filter button opened stays open with "+ 필터" alone once its
  // filters are gone, after a reload or another view too, until the button
  // closes it: Notion keeps only that open state per view and viewer
  // (collectionViewFilterBarOpenState), whatever the bar holds (seen
  // 2026-11-17 in two tabs each on a table view and on a dashboard). People's
  // bars, open at first, keep the visit-only rule: not seen there.
  const kept = keptHere || (opened && !openAtFirst);
  const surface = useMemo<DatabaseFilterBarSurface>(() => ({
    shown: !hidden,
    keptEmpty: !hidden && kept,
    toggle: () => {
      if (!hidden) setKept(false);
      setFolded(null);
      setOpenedHere(null);
      setOpened(hidden);
    },
    add: (propertyId, from) => {
      if (commands.current?.openFilter?.(propertyId)) return;
      setFolded(null);
      setOpenedHere(view.id);
      setFilterRequest({ propertyId, at: Date.now(), from });
    },
    editSorts: (from) => {
      setFolded(null);
      setOpenedHere(view.id);
      setSortRequest({ at: Date.now(), from });
    },
    editAdvanced: (from) => {
      setFolded(null);
      setOpenedHere(view.id);
      setAdvancedRequest({ at: Date.now(), from });
    },
  }), [commands, hidden, kept, setKept, setOpened, view.id]);
  const keep = useCallback(() => setKept(true), [setKept]);
  return { hidden, kept, keep, surface, filterRequest, sortRequest, advancedRequest, clearFilterRequest: useCallback(() => setFilterRequest(null), []) };
}
