import { type RefObject, useContext, useLayoutEffect } from "react";
import { useDatabaseOwnPage } from "./databaseFullPageLink";
import { DashboardWidgetHeightContext } from "./dashboardWidgetHeight";

/**
 * On a database's own page a view that scrolls sideways — a table, a board,
 * a timeline — reaches the bottom of the window however little it shows, so
 * its sideways bar stands there as Notion's page bar does. Its room under its
 * rows is inside it, over the bar (EditorDatabaseBlocks, the timeline's
 * canvas), and nothing follows it: a short view leaves the page unscrolled
 * and a long one scrolls the page to that room and the bar (Notion's, seen
 * 2026-11-08 at 1303×873: two rows don't scroll, forty end 196px over the
 * bar). What sits above it can change height (the filter bar opening), so
 * that is watched as well as the page. A dashboard widget's view keeps to its
 * card: a row fitted to its widgets' content is as tall as what they show.
 */
export function useOwnPageViewFill(scrollRef: RefObject<HTMLElement | null>) {
  const ownPage = useOwnPageView();
  useLayoutEffect(() => {
    const scroller = scrollRef.current;
    const page = scroller?.closest<HTMLElement>("[data-page-scroll]");
    if (!ownPage || !scroller || !page) return;
    // An empty table's "필터 편집하기" box, which follows the table (a
    // board's stands inside its own scroll).
    const view = scroller.closest<HTMLElement>("[data-view-type]");
    const box = () => {
      const found = view?.querySelector<HTMLElement>("[data-view-no-results]") ?? null;
      return found && !scroller.contains(found) ? found : null;
    };
    const fill = () => {
      const empty = box();
      scroller.style.minHeight = "";
      scroller.style.marginBottom = "";
      if (empty) empty.style.marginBottom = "";
      const top = scroller.getBoundingClientRect().top - page.getBoundingClientRect().top + page.scrollTop;
      // The view's sideways bar kept at the window's foot follows it, by the
      // room it takes, which is its scrollbar alone (OwnPageViewBar).
      const pinned = scroller.nextElementSibling?.matches("[data-pinned-view-bar]") ? scroller.nextElementSibling as HTMLElement : null;
      const bar = pinned ? pinned.offsetHeight + (parseFloat(getComputedStyle(pinned).marginBlockStart) || 0) : 0;
      const room = page.clientHeight - top - bar;
      // A view taller than the window stays as it is, the page scrolling for it.
      if (scroller.offsetHeight > room) return;
      // An empty table's box starts right under its head, over the table's
      // grown foot, its buttons 58px under the head (seen 2026-11-07 at
      // 1440×546 and 999×813), and the page still ends at the window's foot.
      const rows = scroller.clientHeight - parseFloat(getComputedStyle(scroller).paddingBottom);
      scroller.style.minHeight = `${room}px`;
      if (!empty) return;
      const lift = room - rows;
      scroller.style.marginBottom = `${-lift}px`;
      empty.style.marginBottom = `${Math.max(0, lift - empty.offsetHeight)}px`;
    };
    // It goes by heights alone, so it fills again only when one of them
    // changed: a side peek sliding in or out (the sidebar, the window
    // dragged) widens or narrows all of them each frame, and filling then
    // laid the page out again in each, 40–50 ms a frame on a table of 60
    // rows (NAS condition, CPU ×2, 2026-11-13). What the new width wraps
    // anew changes a height, which it then fills for.
    const heights = new WeakMap<Element, string>();
    const observer = new ResizeObserver((entries) => {
      let changed = false;
      for (const { target, contentRect, borderBoxSize } of entries) {
        const height = `${contentRect.height}|${borderBoxSize?.[0]?.blockSize ?? ""}`;
        if (heights.get(target) === height) continue;
        heights.set(target, height);
        changed = true;
      }
      if (changed) fill();
    });
    observer.observe(page);
    observer.observe(scroller);
    if (scroller.firstElementChild) observer.observe(scroller.firstElementChild);
    if (view) observer.observe(view);
    for (let level: HTMLElement | null = scroller; level && level !== page; level = level.parentElement) {
      for (let above = level.previousElementSibling; above; above = above.previousElementSibling) observer.observe(above);
    }
    fill();
    return () => {
      observer.disconnect();
      scroller.style.minHeight = "";
      scroller.style.marginBottom = "";
      const empty = box();
      if (empty) empty.style.marginBottom = "";
    };
  }, [ownPage, scrollRef]);
}

/** Whether a view stands as its database's own page's, not in a dashboard widget's card there. */
export function useOwnPageView() {
  const inWidget = useContext(DashboardWidgetHeightContext) !== null;
  return useDatabaseOwnPage() && !inWidget;
}
