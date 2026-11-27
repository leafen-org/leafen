import { useEffect, useRef } from "react";
import type { GraphViewType } from "@/contentGraph";
import type { DatabaseViewSettingsScreen } from "./DatabaseViewSettingsPanel";

// The layouts whose settings are the common list (layout, properties, filter, sort, grouping, colours).
const LISTED: ReadonlySet<GraphViewType> = new Set(["table", "board", "list", "gallery", "timeline", "calendar", "feed"]);

/**
 * The screen a view of this layout shows for the one shown before, or null
 * for its first screen. A chart has a grouping screen of its own though its
 * first screen lists none (DatabaseGroupMenu draws it as a board's), and a
 * board's "하위 그룹화 기준" becomes the chosen view's "그룹화 기준" where it
 * has no sub-groups, as Notion's (seen 2026-11-18 in Chrome 1440: a board's
 * "그룹화" turned a chart's, a donut's and a table's "그룹화"; its "하위 그룹화
 * 기준" a chart's and a table's "그룹화 기준" list).
 */
function screenIn(screen: DatabaseViewSettingsScreen, type: GraphViewType): DatabaseViewSettingsScreen | null {
  const grouped = (LISTED.has(type) && type !== "calendar" && type !== "feed") || type === "chart";
  switch (screen) {
    case "layout": return screen;
    case "filter": case "sort": return LISTED.has(type) || type === "chart" ? screen : null;
    case "properties": case "color": case "editProperties": return LISTED.has(type) ? screen : null;
    case "group": return grouped ? screen : null;
    case "subGroup": return type === "board" ? screen : grouped ? "group" : null;
    // Its source's screens stay whatever the layout (Notion's, seen 2026-11-18 in two tabs: "데이터베이스" from a
    // linked table to a chart and a board, "데이터 소스 관리" from a board to a chart and a linked table).
    case "database": return type === "dashboard" || type === "form" ? null : screen;
    case "dataSources": return type === "dashboard" ? null : screen;
    default: return null;
  }
}

/**
 * The view settings stay open as another view is chosen, on the screen they
 * showed, now the chosen view's, as Notion's (seen 2026-11-18 in Chrome 1440
 * in two tabs at different times: the panel open at "필터 추가", "새 정렬" or
 * "그룹화", a press on another view's tab — a board's, a table's, a chart's —
 * left it open at that screen for the view chosen). A screen with no place in
 * the chosen view's layout (a table's "조건부 색상" on a chart) goes back to
 * its first screen, one standing for it there takes its place
 * (screenIn), and one that held a single property goes back to the list.
 */
export function useSettingsScreenAcrossViews(view: { id: string; type: GraphViewType }, screen: DatabaseViewSettingsScreen | null,
  show: (screen: DatabaseViewSettingsScreen | null) => void, forgetProperty: () => void) {
  const shown = useRef(view);
  useEffect(() => {
    const before = shown.current;
    shown.current = view;
    if (before.id === view.id || screen === null) return;
    forgetProperty();
    const next = before.type !== view.type ? screenIn(screen, view.type) : screen === "databaseViews" ? null : screen;
    if (next !== screen) show(next);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [view.id]);
}
