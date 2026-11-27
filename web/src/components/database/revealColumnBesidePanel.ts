import { inlineSign, readsRightToLeft } from "@/lib/readingDirection";

// A property's settings opened for one of a table's columns (a relation just
// made, a status's "속성 편집") roll the table sideways so that column shows
// beside them, as Notion's (seen 2026-11-14 at 1440: a new relation's column
// ending at 1019, 19px before the panel at 1038, the page left where it was).
// Docked at the page's end the panel lies over the table, which has its
// width more room at its end while it is open (DatabaseViewSettingsPanel's
// sheet), so its last column can roll out from under it. Its column's end goes
// 20px in from the panel's start, as a new column's goes 20px in from the
// table's end (TableAddProperty's revealInTable), its start no further than
// the table's own: a column wider than the room keeps its start in sight. A
// popover's settings, over the toolbar, leave the table as it is.

const TRIES = 12;
const GAP = 20;
// Frames it measures again once both are there: the panel takes its place
// beside the page a frame after it is drawn (useDockedPanelTop, its gap).
const SETTLE = 2;

export function revealColumnBesidePanel(from: HTMLElement, propertyId: string, panel: () => HTMLElement | null) {
  let tries = 0, settled = 0;
  const step = () => {
    const shown = panel();
    const header = Array.from(from.closest("[data-database-view]")?.querySelectorAll<HTMLElement>("[data-table-scroll] [data-property-id]") ?? [])
      .find((element) => element.dataset.propertyId === propertyId);
    // The column and the panel come a frame or more after the press (a new relation's column with its view's order).
    if (!shown?.isConnected || !header) {
      if (++tries < TRIES) window.requestAnimationFrame(step);
      return;
    }
    if (shown.dataset.dockedPanel !== "true") return;
    const scroller = header.closest<HTMLElement>("[data-table-scroll]");
    if (!scroller) return;
    const rtl = readsRightToLeft(scroller);
    const room = scroller.getBoundingClientRect(), column = header.getBoundingClientRect(), side = shown.getBoundingClientRect();
    // Measured along the reading direction: where the panel starts, and how far the column's end runs past that less the gap.
    const past = rtl ? (side.right + GAP) - column.left : column.right - (side.left - GAP);
    const lead = rtl ? room.right - column.right : column.left - room.left;
    const by = Math.min(past, lead);
    if (by > 0) scroller.scrollLeft += by * inlineSign(scroller);
    if (++settled <= SETTLE) window.requestAnimationFrame(step);
  };
  window.requestAnimationFrame(step);
}
