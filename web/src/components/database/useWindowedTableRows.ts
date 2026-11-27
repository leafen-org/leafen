import { type RefObject, useLayoutEffect } from "react";
import { holdStyleWrite, startWithMeasures } from "./heldStyleWrites";

/**
 * Notion stops drawing a table's columns past its view once they and its
 * "+" run past twice the view's width, and its rows then take the height of
 * what they hold with their foot's line over its last pixel: a wrapped
 * one-line row 36 high, 37 with its page's mark, two lines 57, where a
 * narrower table's are 37, 38 and 58 (seen 2026-11-17 in Chrome, ten 200px
 * columns after a 280px title: 2080 wide by a view of 1155 and 1075 drawn
 * whole, by 1015 and 739 windowed; eleven, 2280 by 1155, windowed). The
 * table's scroller is marked so (data-columns-windowed) and its wrapped rows
 * follow (DatabaseTableWindowedRows.module.css); its text keeps its place.
 */
export function useWindowedTableRows(ref: RefObject<HTMLElement | null>, columnsWidth: number) {
  useLayoutEffect(() => {
    if (!Number.isFinite(columnsWidth)) return undefined;
    return startWithMeasures(() => {
      const scroller = ref.current;
      if (!scroller?.matches("[data-table-scroll]") || typeof ResizeObserver === "undefined") return undefined;
      const mark = (write: (apply: () => void) => void) => {
        const add = scroller.querySelector<HTMLElement>("[data-table-add-property]")?.offsetWidth ?? 0;
        const windowed = scroller.clientWidth > 0 && columnsWidth + add > 2 * scroller.clientWidth;
        if ((scroller.dataset.columnsWindowed === "true") === windowed) return;
        write(() => {
          if (windowed) scroller.dataset.columnsWindowed = "true";
          else delete scroller.dataset.columnsWindowed;
        });
      };
      mark(holdStyleWrite);
      const observer = new ResizeObserver(() => mark((apply) => apply()));
      observer.observe(scroller);
      // The mark stays till the next measure says otherwise: a column
      // dragged wider measures again without the rows changing between.
      return () => observer.disconnect();
    });
  }, [ref, columnsWidth]);
}
