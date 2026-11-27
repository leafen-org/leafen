import { useContext, useEffect, useRef, useState } from "react";
import type { GraphBlock, GraphView } from "@/contentGraph";
import { setDocumentViewName } from "@/lib/documentChrome";
import { SuggestingModeContext } from "@/components/editor/suggestingMode";
import { databaseViewBlock } from "@/lib/nativeDataSourceManagement";
import { openedInApp } from "@/lib/router";
import { DashboardWidgetHeightContext } from "./dashboardWidgetHeight";
import { viewOwnName } from "./databaseViewShared";

/**
 * A full-page database names its shown view in the window's title once that
 * view is chosen among two or more: by the address it opened at, by opening
 * it in the app (a link, a mention, an inline one's "전체 페이지로 열기",
 * the history's back and forward — Notion's, seen twice 2026-11-25: "DB |
 * Default view | Notion" from a mention whose link names no view), or by the
 * person since (a tab, the views list). Only the window's first screen loaded
 * at its plain address, or a database with one view, is named by its
 * page alone, a linked view's page alike (Notion's, seen 2026-11-09: "사과 DB
 * 보기 | Notion" over a view named "내 보기" until its tab was pressed or the
 * page read again at that view's address). A view without a name of its own
 * (still its layout's, as DatabaseViewNameField reads it) adds none (seen
 * 2026-11-06: "DB | Notion" on a new board view); a dashboard's widget leaves
 * it to the dashboard (2026-11-07), and a dashboard block's page is named by
 * its block alone, whatever its view's name (seen 2026-11-09). "내 작업"
 * names its screen alone, whatever view it shows (Notion's "내 작업 |
 * Notion", seen 2026-11-09), and one in a peek names none (seen 2026-11-14: a
 * wiki's view in a side peek).
 */
export function useDatabaseDocumentViewName({ block, views, dashboardWidgets, activeView, ownPage, listOnly }: {
  block: GraphBlock;
  /** The database's tabs. */
  views: readonly GraphView[];
  /** Its dashboards' widgets, which keep a database whose tabs are dashboards one (databaseViewBlock). */
  dashboardWidgets: readonly GraphView[];
  activeView: GraphView;
  ownPage: boolean;
  listOnly?: boolean | "tasks";
}) {
  const inDashboardWidget = useContext(DashboardWidgetHeightContext) !== null, ownViewName = viewOwnName(activeView).trim(), inPeek = useContext(SuggestingModeContext).inPeek === true;
  const ownViewBlock = databaseViewBlock(block, [...views, ...dashboardWidgets]) && block.content.linkedDatabaseSource !== true;
  const [openedChosen] = useState(() => typeof window !== "undefined" && (openedInApp() || new URL(window.location.href).searchParams.has("v")));
  const firstShownView = useRef(activeView.id), viewSwitched = useRef(false), acted = useRef(false);
  // Shown by the person's doing — a press or a key here since it opened (a tab, the views list, the shown view
  // deleted: Notion's "DB | Default view", seen 2026-11-17). The view it settles on as it opens, the one last
  // chosen read once the session or the views have come (a plain address on a NAS-class server, 2026-11-16),
  // is still the one it opened at: Notion names none then (seen twice 2026-11-17, last chosen view first or not).
  if (activeView.id !== firstShownView.current) {
    if (acted.current) viewSwitched.current = true;
    else firstShownView.current = activeView.id;
  }
  useEffect(() => {
    if (!ownPage || viewSwitched.current) return undefined;
    const act = () => { acted.current = true; };
    window.addEventListener("pointerdown", act, { capture: true, passive: true });
    window.addEventListener("keydown", act, { capture: true, passive: true });
    return () => {
      window.removeEventListener("pointerdown", act, { capture: true });
      window.removeEventListener("keydown", act, { capture: true });
    };
  }, [ownPage]);
  const viewChosen = views.length > 1 && (openedChosen || viewSwitched.current);
  const documentViewName = ownPage && !inPeek && listOnly !== "tasks" && !inDashboardWidget && !ownViewBlock && viewChosen && ownViewName ? ownViewName : null;
  useEffect(() => {
    if (documentViewName === null) return;
    setDocumentViewName(documentViewName);
    return () => setDocumentViewName(null);
  }, [documentViewName]);
}
