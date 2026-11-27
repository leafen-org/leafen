import { readsRightToLeft } from "@/lib/readingDirection";

/**
 * How far into a timeline's days, from their start edge along the reading
 * direction, the table laid over them reaches; 0 with it shut. The days
 * shown start past it: Notion's arrows and pulled-out titles stand beside
 * the table's edge (seen 2026-11-11).
 */
export function timelineTableCover(scroller: HTMLElement) {
  const table = scroller.parentElement?.querySelector("[data-timeline-table-pane]");
  if (!table) return 0;
  const view = scroller.getBoundingClientRect(), pane = table.getBoundingClientRect();
  return Math.max(0, readsRightToLeft(scroller) ? view.right - pane.left : pane.right - view.left);
}

/**
 * How far the days run under the page's margin before the box the timeline
 * stands in: on a database's own page its days start at the window's edge,
 * 96px before the text (DatabaseTimelineCore .timelineScroll); in a page's
 * text, 0.
 */
export function timelineMarginBleed(scroller: HTMLElement) {
  const box = scroller.parentElement;
  if (!box) return 0;
  const view = scroller.getBoundingClientRect(), own = box.getBoundingClientRect();
  return Math.max(0, readsRightToLeft(scroller) ? view.right - own.right : own.left - view.left);
}
