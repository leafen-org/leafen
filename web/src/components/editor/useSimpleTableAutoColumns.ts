import { type RefObject, useLayoutEffect } from "react";
import { autoSimpleTableWidths, overflowingSimpleTableWidths } from "./simpleTableColumns";

/** The strip a table keeps past its last column for its "+", and its first line (Notion's 24px and 1px, seen 2026-11-12). */
const SIMPLE_TABLE_ROOM_KEPT = 25;
/** A top table's inset while the text has its whole column (EditorSimpleTable --simple-table-inset). */
const SIMPLE_TABLE_WHOLE_INSET = 8;

/**
 * Lays a simple table's auto columns out in the room its block has, as
 * Notion's table does (autoSimpleTableWidths): each first takes its words'
 * width up to 240px (the table's own tracks), and when together they don't
 * fit, all give way toward 120px alike. Again as the block's width, the
 * table's words or its sized columns change. The room is the block's less
 * the table's inset and the strip of its "+", wherever the table scrolls.
 * Columns all at widths in pixels, sized or laid out so, mark the block
 * data-table-tracks="fixed" with their whole width, where each row lays its
 * cells out by itself (NativeTiptap.module.css).
 */
export function useSimpleTableAutoColumns(
  wrapRef: RefObject<HTMLElement | null>,
  scrollerRef: RefObject<HTMLElement | null>,
  widths: readonly number[],
) {
  const key = widths.join(",");
  useLayoutEffect(() => {
    const wrap = wrapRef.current;
    const scroller = scrollerRef.current;
    if (!wrap || !scroller) return undefined;
    const sized = key.split(",").map(Number);
    const clear = () => {
      wrap.style.removeProperty("--simple-table-auto-columns");
      wrap.style.removeProperty("--simple-table-tracks-width");
      delete wrap.dataset.tableTracks;
    };
    const fixed = (laid: readonly number[]) => {
      wrap.style.setProperty("--simple-table-tracks-width", `${laid.reduce((sum, width) => sum + width, 0)}px`);
      wrap.dataset.tableTracks = "fixed";
    };
    if (sized.every((width) => width > 0)) {
      clear();
      fixed(sized);
      return clear;
    }
    const set = (laid: readonly number[] | null) => {
      const value = laid ? laid.map((width) => `${width}px`).join(" ") : "";
      if (wrap.style.getPropertyValue("--simple-table-auto-columns") === value && Boolean(wrap.dataset.tableTracks) === Boolean(laid)) return;
      if (!laid) { clear(); return; }
      wrap.style.setProperty("--simple-table-auto-columns", value);
      fixed(laid);
    };
    const apply = () => {
      const grid = scroller.querySelector<HTMLElement>("[role=table]");
      if (!grid) return;
      const reach = parseFloat(scroller.style.getPropertyValue("--page-wide-reach")) || 0;
      const inset = parseFloat(getComputedStyle(scroller).paddingInlineStart) - reach;
      // A top table on a page whose edges hold its text keeps its inset at its
      // end too (Notion's at 1024: 374–905 by the text's 372–907).
      const top = wrap.closest<HTMLElement>('[data-block-type="table"]')?.dataset.depth === "0";
      const end = top && inset < SIMPLE_TABLE_WHOLE_INSET ? inset : 0;
      const room = wrap.clientWidth - inset - end - SIMPLE_TABLE_ROOM_KEPT;
      if (room <= 0) { set(null); return; }
      // Columns that overflow the room even at their narrowest all stand
      // there whatever their words: the grid isn't laid out again at its
      // words' widths for each letter typed. In a 100-column table of 300
      // rows that took two layouts of its 30,000 cells, some 0.5 s each, with
      // every letter typed anywhere on its page (2026-11-19).
      const narrowest = overflowingSimpleTableWidths(sized, room);
      if (narrowest) { set(narrowest); return; }
      // Its columns at their words' widths, as its own tracks lay them out
      // through its rows.
      clear();
      const natural = getComputedStyle(grid).gridTemplateColumns.split(" ").map((track) => parseFloat(track));
      set(natural.length === sized.length ? autoSimpleTableWidths(sized, natural, room) : null);
    };
    let frame = 0;
    const schedule = () => {
      if (!frame) frame = window.requestAnimationFrame(() => { frame = 0; apply(); });
    };
    apply();
    const resized = new ResizeObserver(schedule);
    resized.observe(wrap);
    const grid = scroller.querySelector("[role=table]");
    if (grid) resized.observe(grid);
    // Words typed in a cell may widen its column without the table's size changing.
    const typed = new MutationObserver(schedule);
    typed.observe(scroller, { subtree: true, childList: true, characterData: true });
    return () => {
      resized.disconnect();
      typed.disconnect();
      if (frame) window.cancelAnimationFrame(frame);
      clear();
    };
  }, [key, scrollerRef, wrapRef]);
}
