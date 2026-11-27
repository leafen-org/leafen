import type { PointerEvent } from "react";

/**
 * A table scrolled so far sideways that its columns pass under the rows' box
 * strip (the strip pinned at the scroller's edge) says so on its scroller
 * (`data-scrolled-under`), and its table says whether a head's box is pointed
 * at (`data-head-box-pointed`): then every row's box shows
 * (DatabaseTableBoxBand.module.css), as Notion's.
 */
export function noteColumnsUnderBoxes(scroller: HTMLElement) {
  const strip = scroller.querySelector<HTMLElement>("[data-table-head]")?.firstElementChild;
  const first = strip?.nextElementSibling;
  let under = false;
  if (strip && first && scroller.scrollLeft !== 0) {
    const stripBox = strip.getBoundingClientRect(), firstBox = first.getBoundingClientRect();
    under = getComputedStyle(scroller).direction === "rtl" ? firstBox.right > stripBox.left + 0.5 : firstBox.left < stripBox.right - 0.5;
  }
  const value = under ? "true" : "false";
  if (scroller.dataset.scrolledUnder !== value) scroller.dataset.scrolledUnder = value;
}

function pointHeadBox(event: PointerEvent<HTMLElement>, pointed: boolean) {
  const table = event.currentTarget.closest<HTMLElement>('[role="grid"]');
  if (!table) return;
  if (pointed) table.dataset.headBoxPointed = "true";
  else delete table.dataset.headBoxPointed;
}

/** A head's box strip (its row-actions cell) under the pointer. */
export const headBoxPointer = {
  onPointerEnter: (event: PointerEvent<HTMLElement>) => pointHeadBox(event, event.pointerType !== "touch"),
  onPointerLeave: (event: PointerEvent<HTMLElement>) => pointHeadBox(event, false),
};
