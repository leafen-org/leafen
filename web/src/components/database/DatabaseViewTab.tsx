import { useEffect, useRef } from "react";
import { useTab } from "react-aria";
import type { TabListState } from "react-stately";
import type { GraphView } from "@/contentGraph";
import { useViewTabTip } from "./ViewTabTip";
import { databaseViewLabels } from "./databaseViewLabels";
import { useCurrentSprintViewNames } from "./databaseSprintContext";
import { ViewTabGlyph, viewDisplayName } from "./databaseViewShared";
import type { ViewTabDisplay } from "./databaseViewTabDisplay";
import { useCreatingView, viewTabName } from "./creatingView";
import styles from "./databaseSheet";
import { fieldEnter } from "@/lib/fieldKeyBoundary";
import { viewTabArrowKey } from "./viewTabChoice";

// The Sprint board's current sprint view names its sprint on its tab
// (databaseSprintContext).
export function useSprintTabName() {
  const names = useCurrentSprintViewNames();
  return (view: GraphView, name: string) => {
    const sprint = names.get(view.id);
    return sprint ? databaseViewLabels().currentSprintTab(sprint) : name;
  };
}

export function NativeDatabaseViewTab({ view, state, panelId, active, display, movable, menuOpen, onOpenMenu, register }: {
  view: GraphView; state: TabListState<GraphView>; panelId: string; active: boolean; display: ViewTabDisplay; movable: boolean;
  menuOpen: boolean;
  /** Opens the view's menu: Notion's chosen tab opens it when pressed again (seen 2026-10-11). */
  onOpenMenu?(element: HTMLElement): void;
  register(element: HTMLButtonElement | null): void;
}) {
  const ref = useRef<HTMLButtonElement>(null);
  const creating = useCreatingView(), newViewLabel = databaseViewLabels().newView, sprintTabName = useSprintTabName();
  const tabName = (named: GraphView) => sprintTabName(named, viewTabName(named, creating, newViewLabel));
  const { tabProps } = useTab({ key: view.id }, state, ref);
  useEffect(() => { register(ref.current); return () => register(null); }, [register]);
  // Whether the tab was the chosen one as the press began, not after it chose it.
  const chosenAtPress = useRef(false);
  // A touch screen's tap acts at once, with no tip: it chooses the tab, or
  // opens the chosen one's menu as the finger lifts, as Notion's iPad (seen
  // 2026-11-15 at 834 on fresh pages, the chosen tab twice and another once)
  // — iOS sends no click after a tap that brings the row's "+" out.
  const touchOpened = useRef(false);
  const tabTip = useViewTabTip(viewDisplayName(view), databaseViewLabels().database);
  // As the person shows the tabs: icon and name, the name alone, or the icon named for a screen reader.
  // Each tab is a stop of Tab and ⇧Tab, the chosen one or not, its view kept as it takes the focus (Notion's,
  // seen 2026-11-15 in Chrome at 1440 after the key only, twice: 필터 ⇧Tab → the last tab → the first).
  return <><button {...tabProps} tabIndex={0} ref={ref} type="button" id={`database-view-tab-${view.id}`} aria-controls={panelId}
    onMouseEnter={tabTip.handlers.onMouseEnter} onMouseLeave={tabTip.handlers.onMouseLeave} onPointerDown={(event) => {
      tabTip.handlers.onPointerDown();
      // Shift and a press choose no view and open no menu: the tab only takes the keys, the caret leaving the page's
      // text, as Notion's (seen 2026-11-25 in Chrome, two tabs at different times, with the caret in the page and without).
      if (event.shiftKey && event.button === 0) { event.currentTarget.focus({ preventScroll: true }); return; }
      tabProps.onPointerDown?.(event);
    }}
    onFocus={(event) => { tabProps.onFocus?.(event); tabTip.handlers.onFocus(event); }}
    onBlur={(event) => { tabProps.onBlur?.(event); tabTip.handlers.onBlur(); }}
    className={`${styles.viewTab} ${active ? styles.viewTabActive : ""}`} data-view-tab={view.id} data-tab-display={display}
    aria-label={display === "icon" ? viewDisplayName(view) : undefined}
    aria-keyshortcuts={movable ? "Alt+ArrowLeft Alt+ArrowRight" : undefined}
    aria-haspopup={active && onOpenMenu ? "dialog" : undefined} aria-expanded={active && onOpenMenu ? menuOpen : undefined}
    onPointerDownCapture={() => { chosenAtPress.current = active; touchOpened.current = false; }}
    onPointerUp={(event) => {
      tabProps.onPointerUp?.(event);
      if (event.pointerType !== "touch" || !chosenAtPress.current || !onOpenMenu) return;
      chosenAtPress.current = false; touchOpened.current = true;
      onOpenMenu(event.currentTarget);
    }}
    onClick={(event) => {
      if (touchOpened.current) { touchOpened.current = false; return; }
      if (event.shiftKey) { chosenAtPress.current = false; return; }
      tabProps.onClick?.(event);
      if (chosenAtPress.current && onOpenMenu) onOpenMenu(event.currentTarget);
      chosenAtPress.current = false;
    }}
    // A right press opens any tab's menu where it stands, the chosen view kept (Notion's, seen 2026-11-06).
    onContextMenu={(event) => {
      if (!onOpenMenu) return;
      event.preventDefault();
      onOpenMenu(event.currentTarget);
    }}
    onKeyDown={(event) => {
      // Enter or Space on the chosen tab, or the context-menu keys, open its menu.
      const opens = onOpenMenu && (((fieldEnter(event) || event.key === " ") && active)
        || event.key === "ContextMenu" || (event.shiftKey && event.key === "F10"));
      if (opens) { event.preventDefault(); onOpenMenu(event.currentTarget); return; }
      // ↑ and ↓ choose on the page or in the view, as Notion's tabs do (viewTabChoice.ts).
      viewTabArrowKey(event.nativeEvent, event.currentTarget);
      tabProps.onKeyDown?.(event);
    }}>
    {display !== "text" && <ViewTabGlyph view={view} className={styles.viewGlyph} />}
    {display !== "icon" && <span className={styles.viewTabName} data-view-tab-name={view.id}>{tabName(view)}</span>}
  </button>{tabTip.tip}</>;
}
