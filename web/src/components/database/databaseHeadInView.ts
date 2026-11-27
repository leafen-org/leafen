import { watchPageSize } from "../page/pageSizeWatch";
import { flushHeldStyleWrites, holdMeasure, holdStyleWrite } from "./heldStyleWrites";

/**
 * Lays a database's head — its title, views and tools — in the part of its
 * row still in view. A page with a side peek open keeps its column and
 * scrolls sideways (PageView), so a database there may run on under the
 * peek: its head is then cut back to what shows, the title ending short and
 * the tools at that part's end, which the tools also go by (DatabaseToolbar's
 * narrow and cramped rows), as Notion's do (seen 2026-11-11 at 1054 and 1191,
 * an inline database and one on its own page alike: settings alone, then the
 * full page, settings and "새로 만들기", the title cut with "…").
 * Returns what stops following it.
 */
export function keepDatabaseHeadInView(head: HTMLElement, tools: HTMLElement): () => void {
  const database = head.parentElement;
  const pane = head.closest<HTMLElement>("[data-page-scroll]");
  if (!database || !pane || typeof ResizeObserver === "undefined") return () => {};
  // What it last wrote: as the page's width changes with a side peek open,
  // the same width again is not written again, which laid the page out a
  // second time in that frame and drew the toolbar again (seen 2026-11-13).
  let written: number | null = null;
  // Measured with the view's other measures, written after them (heldStyleWrites).
  const place = () => {
    // The tools end 24px in from the pane's edge, scrollbar or none, as
    // Notion's (seen 2026-11-11: 327 in a pane to 351, 447 to 471, 696 to
    // 720, inline or on its own page), less what they keep from the head's end.
    const room = PANE_TOOLS_ROOM - (Number.parseFloat(window.getComputedStyle(tools).marginInlineEnd) || 0);
    const shown = shownInlineSize(database, pane, room);
    if (shown === written) return;
    written = shown;
    holdStyleWrite(() => {
      if (shown === null) {
        head.style.removeProperty("--database-head-shown");
        delete head.dataset.headShown;
      } else {
        head.style.setProperty("--database-head-shown", `${shown}px`);
        head.dataset.headShown = "true";
      }
    });
  };
  const dropFirst = holdMeasure(place, place);
  // As they resize, measured with the page's other measures before any of
  // them writes (pageSizeWatch): with an observer of its own it measured
  // after their writes, laying the whole page out again as a row's peek
  // opened (38–55ms for a page of three tables, seen 2026-11-13).
  const stopDatabase = watchPageSize(database, place), stopPane = watchPageSize(pane, place);
  pane.addEventListener("scroll", place, { passive: true });
  return () => {
    dropFirst();
    flushHeldStyleWrites();
    stopDatabase();
    stopPane();
    pane.removeEventListener("scroll", place);
    head.style.removeProperty("--database-head-shown");
    delete head.dataset.headShown;
  };
}

const PANE_TOOLS_ROOM = 24;

/** How much of the database's row the pane shows, short of `room` at its end, when that is less than all of it. */
function shownInlineSize(database: HTMLElement, pane: HTMLElement, room: number): number | null {
  const row = database.getBoundingClientRect();
  const frame = pane.getBoundingClientRect();
  const rtl = window.getComputedStyle(pane).direction === "rtl";
  const left = frame.left + (rtl ? room : 0);
  const right = frame.right - (rtl ? 0 : room);
  const shown = Math.min(row.right, right) - Math.max(row.left, left);
  return row.width > 0 && shown < row.width - 1 ? Math.max(0, Math.floor(shown)) : null;
}
