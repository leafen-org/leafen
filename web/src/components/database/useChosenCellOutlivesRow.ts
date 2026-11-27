import { useEffect, useLayoutEffect, useRef, type RefObject } from "react";
import { cellLine } from "./tableCellLine";
import { useViewSwitchLetsGo } from "./viewPanel";
import { fieldEnter } from "@/lib/fieldKeyBoundary";

type Place = { line: string; row: number; column: number; cell: HTMLElement };

// How long a choice handed back by a closing picker may still be put back on
// its line: the picker gives the focus back within a frame or two.
const HANDED_BACK_MS = 1000;

// A line chosen once the table has written where its rows now stand
// (TableGrid's writeRowPlaces, later in the same drawing), which it is read by.
function chooseLine(focus: { readonly current: (row: number, column: number) => void }, line: number, column: number) {
  queueMicrotask(() => focus.current(line, column));
}

/**
 * A chosen cell is a place in the table, a line and a column, as Notion's
 * (seen 2026-11-14): a row its new value moves elsewhere (a sort) or takes
 * out of the view (a filter) leaves the choice on the line it stood on, now
 * another row's, and Escape and the arrows go on from there; a row taken
 * away under it — a new row taken back with ⌘Z, one another tab removed —
 * leaves it on the same line, the last one when it was past the end
 * (2026-11-07: ⌘Z taking back a new row chose the title cell of the row
 * above). A value picked in a picker over the table is the cell's too: the
 * picker gives the choice back on the line the cell stood on. A cell left by
 * a press or a key elsewhere hands nothing on. `lines` are the rows' lines
 * shown, in order (viewGroupLines.ts): a row under two groups stands on two.
 *
 * Lines of another view lost no row: a view's tab pressed — a table, a
 * grouped table, a board, a list — leaves what the table chose behind, as
 * Notion's (seen 2026-11-26 in Chrome at 1440, two tabs at different times,
 * after the press only): a chosen cell, a range or chosen rows, the new view
 * chooses nothing and the page stays where it was, the focus on the tab
 * pressed (Tab the next tab, a letter nowhere), and coming back to the table
 * chooses nothing either (a window closing on another view gives it no focus
 * back, useViewSwitchGivesNoFocusBack). The range goes at the same switch
 * (useViewSwitchLetsGo, useTableCellRange).
 */
export function useChosenCellOutlivesRow(
  gridRef: RefObject<HTMLElement | null>,
  lines: readonly string[],
  focusCell: (row: number, column: number) => void,
) {
  // The cell holding the focus now, and the last one that held it.
  const chosen = useRef<Place | null>(null);
  const last = useRef<Place | null>(null);
  // A row moved while the focus was away (a picker open over the table): the
  // line its cell held, until the focus comes back to that cell.
  const pinned = useRef<{ place: Place; until: number } | null>(null);
  const focus = useRef(focusCell);
  focus.current = focusCell;
  useEffect(() => {
    const grid = gridRef.current;
    if (!grid) return;
    const onFocusIn = (event: FocusEvent) => {
      // The cell itself, or a value's button or editor in it.
      const cell = event.target instanceof HTMLElement ? event.target.closest<HTMLElement>("[data-table-cell]") : null;
      const line = cell?.closest("[data-table-line]")?.getAttribute("data-table-line");
      const at = cell && line ? { line, row: cellLine(cell), column: Number(cell.dataset.colIndex), cell } : null;
      if (at) last.current = at;
      const place = at && event.target === cell ? at : null;
      chosen.current = place;
      const pin = pinned.current;
      pinned.current = null;
      if (!place || !pin || pin.until < performance.now() || pin.place.line !== place.line || pin.place.column !== place.column
        || pin.place.row === place.row) return;
      chooseLine(focus, pin.place.row, pin.place.column);
    };
    // A cell still drawn once its focus left was left on purpose; one gone
    // with its row keeps its place for the rows that follow.
    const onFocusOut = (event: FocusEvent) => {
      const left = chosen.current;
      if (!left || event.target !== left.cell) return;
      queueMicrotask(() => { if (chosen.current === left && left.cell.isConnected) chosen.current = null; });
    };
    // A press, or a key that moves on, is the person's own choice.
    const unpin = (event: Event) => {
      if (event instanceof KeyboardEvent && (event.key === "Escape" || fieldEnter(event))) return;
      pinned.current = null;
    };
    grid.addEventListener("focusin", onFocusIn);
    grid.addEventListener("focusout", onFocusOut);
    document.addEventListener("pointerdown", unpin, true);
    document.addEventListener("keydown", unpin, true);
    return () => {
      grid.removeEventListener("focusin", onFocusIn);
      grid.removeEventListener("focusout", onFocusOut);
      document.removeEventListener("pointerdown", unpin, true);
      document.removeEventListener("keydown", unpin, true);
    };
  }, [gridRef]);
  // Another view's lines: the choice stays behind, handing nothing on (ahead of the lines' own check below).
  useViewSwitchLetsGo(gridRef, () => { chosen.current = last.current = pinned.current = null; });
  useLayoutEffect(() => {
    const grid = gridRef.current;
    const left = chosen.current;
    if (left && (!left.cell.isConnected || lines[left.row] !== left.line)) {
      chosen.current = null;
      const focused = document.activeElement;
      // The focus gone with its row, or the cell holding it moved off its line.
      if ((focused === left.cell || !focused || focused === document.body || !focused.isConnected) && lines.length) {
        chooseLine(focus, Math.min(left.row, lines.length - 1), left.column);
      }
      return;
    }
    // Moved while a picker over the table held the focus: its line waits for the focus to come back.
    const away = last.current;
    if (!away || !grid || grid.contains(document.activeElement) || lines[away.row] === away.line || !lines.includes(away.line)) return;
    pinned.current = { place: away, until: performance.now() + HANDED_BACK_MS };
  }, [gridRef, lines]);
}
