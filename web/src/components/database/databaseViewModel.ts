import { peekModeOf } from "@/lib/pagePeek";
export type DatabaseViewTab = { id: string; name: string; type: string };

export type DatabaseViewRowMode = "side" | "center" | "full";

/** What a tab shows: its icon and name, the name alone or the icon alone. */
export type DatabaseViewTabShows = "both" | "text" | "icon";

// A tab's width as drawn: icon and padding, then its name at 14px, where a
// Hangul or other wide letter takes about twice a Latin one; without the
// icon 24 less, and the icon alone 45 (Notion's, seen 2026-10-11).
function estimatedWidth(view: DatabaseViewTab, shows: DatabaseViewTabShows = "both") {
  if (shows === "icon") return 45;
  const letters = Array.from(view.name || view.type);
  const text = letters.reduce((sum, letter) => sum + (/[\u1100-\u11ff\u2e80-\ua4cf\uac00-\ud7af\uf900-\ufaff\uff00-\uffef]/u.test(letter) ? 14 : 7.5), 0);
  return (shows === "text" ? 24 : 48) + Math.min(140, Math.ceil(text));
}

// Notion keeps 80px past its tabs, where "N개 더 보기" goes, even while every
// tab fits and its "+" shows there instead, and counts each tab with the 1px
// after it (seen 2026-11-02 at 1300px: six tabs of 460 showed from a line of
// 541 to its tools, not 540; five of 397 and "1개 더 보기" from 478, not 477).
const OVERFLOW_ROOM = 80;
const TAB_GAP = 1;

/**
 * An inline database with one view keeps that view's tools in its title row,
 * without a row of tabs, as Notion's (seen 2026-10-10); a second view, a
 * hidden title or the database's own page brings the tabs back.
 */
export function viewToolsInTitleRow({ placement, viewCount, titleShown }: {
  placement: string; viewCount: number; titleShown: boolean;
}) {
  return titleShown && placement === "inline" && viewCount === 1;
}

/**
 * Which tabs a tab line shows, as Notion's (seen 2026-10-06): every tab
 * when they fit with the room kept past them; else the first that fit with
 * the current one, and "N more" in that room; and where not two of them fit,
 * none — the current view alone shows as a picker of them all (seen
 * 2026-11-04: two tabs and "11개 더 보기" down to a 548px database, the
 * picker by 536, never one tab and "N more").
 */
export function visibleDatabaseViewTabs<View extends DatabaseViewTab>(
  views: readonly View[],
  activeId: string,
  availableWidth: number,
  reservedWidth = 0,
  shows: DatabaseViewTabShows = "both",
  /** A tab's width as drawn, where it can be measured. */
  widthOf?: (view: View) => number | undefined,
) {
  if (views.length === 0) return { visible: [], overflow: [], collapsed: false };
  // One view is its tab however narrow, never a picker of one: Notion's "내 작업"
  // beside a side peek shows "☑ 내 작업" with no "⌄" (seen 2026-11-15, a 242px row).
  if (views.length === 1) return { visible: [views[0].id], overflow: [], collapsed: false };
  // The room past 80 is the "N more" label's own, so all tabs fit without it.
  const bounded = Math.max(0, availableWidth - reservedWidth - OVERFLOW_ROOM);
  const estimatedTabWidth = (view: View) => (widthOf?.(view) ?? estimatedWidth(view, shows)) + TAB_GAP;
  const total = views.reduce((sum, view) => sum + estimatedTabWidth(view), 0);
  if (total <= availableWidth - OVERFLOW_ROOM) return { visible: views.map(({ id }) => id), overflow: [], collapsed: false };

  const active = views.find(({ id }) => id === activeId) ?? views[0];
  const collapsed = { visible: [], overflow: views.map(({ id }) => id), collapsed: true };
  if (estimatedTabWidth(active) > bounded) return collapsed;
  const admitted: View[] = [active];
  let consumed = estimatedTabWidth(active);
  // The others in their order up to the first that doesn't fit, a shorter one
  // after it folded too: a widened chosen tab keeps "Default view" beside it,
  // not a later "줄" (Notion's, seen 2026-11-07).
  for (const view of views) {
    if (view.id === active.id) continue;
    const next = estimatedTabWidth(view);
    if (consumed + next > bounded) break;
    admitted.push(view);
    consumed += next;
  }
  if (admitted.length < 2) return collapsed;
  const visible = new Set(admitted.map(({ id }) => id));
  return {
    visible: views.filter(({ id }) => visible.has(id)).map(({ id }) => id),
    overflow: views.filter(({ id }) => !visible.has(id)).map(({ id }) => id),
    collapsed: false,
  };
}

/**
 * A row opened from inside another opened row keeps the rows above it in the
 * address (`pp`, outermost first), as Notion's preview goes back to the row
 * it was opened from: each as its id, how it was open and the view that
 * opened it.
 */
export type DatabaseViewOpenRow = { rowId: string; rowMode: DatabaseViewRowMode; viewId: string };

const ROW_MODE_PARAM: Record<DatabaseViewRowMode, string> = { side: "s", center: "c", full: "f" };

const rowModeOf: (param: string | null) => DatabaseViewRowMode = peekModeOf;

function openRowsOf(param: string | null): DatabaseViewOpenRow[] {
  return (param ?? "").split(",").flatMap((entry) => {
    const [rowId, mode, viewId] = entry.split("~");
    return rowId ? [{ rowId, rowMode: rowModeOf(mode ?? null), viewId: viewId ?? "" }] : [];
  });
}

export function databaseViewUrlState(url: URL) {
  return {
    rowId: url.searchParams.get("p"),
    rowMode: rowModeOf(url.searchParams.get("pm")),
    viewId: url.searchParams.get("v"),
    openAbove: openRowsOf(url.searchParams.get("pp")),
  };
}

export function databaseViewUrlWithSelection(
  current: URL,
  selection: {
    rowId?: string | null;
    rowMode?: DatabaseViewRowMode | null;
    viewId?: string | null;
    /** The opened rows above the selected one; [] or a closed row clears them. */
    openAbove?: readonly DatabaseViewOpenRow[];
  },
) {
  const next = new URL(current.toString());
  if (selection.viewId) next.searchParams.set("v", selection.viewId);
  else if (selection.viewId === null) next.searchParams.delete("v");
  if (selection.rowId) {
    next.searchParams.set("p", selection.rowId);
    next.searchParams.set("pm", ROW_MODE_PARAM[selection.rowMode ?? "side"]);
  } else if (selection.rowId === null) {
    next.searchParams.delete("p");
    next.searchParams.delete("pm");
    next.searchParams.delete("pp");
  }
  if (selection.openAbove && selection.openAbove.length > 0) {
    next.searchParams.set("pp", selection.openAbove
      .map(({ rowId, rowMode, viewId }) => [rowId, ROW_MODE_PARAM[rowMode], viewId].join("~")).join(","));
  } else if (selection.openAbove) {
    next.searchParams.delete("pp");
  }
  return next;
}
