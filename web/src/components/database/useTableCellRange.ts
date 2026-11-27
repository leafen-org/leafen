import { useEffect, useRef, useState, type KeyboardEvent, type PointerEvent, type RefObject } from "react";
import { nextTableCell, type TablePart } from "./tableInteractionModel";
import { cellLine } from "./tableCellLine";
import { tablePasteArea } from "./tableCellPaste";
import { chordLetter, primaryModifier } from "@/lib/keyboardShortcuts";
import { useViewSwitchLetsGo } from "./viewPanel";

export type CellPoint = { row: number; column: number };
/** The cells a range covers, both ends in. */
export type CellBox = { top: number; bottom: number; left: number; right: number };

type Range = { anchor: CellPoint; focus: CellPoint };

function boxOf({ anchor, focus }: Range): CellBox {
  return {
    top: Math.min(anchor.row, focus.row), bottom: Math.max(anchor.row, focus.row),
    left: Math.min(anchor.column, focus.column), right: Math.max(anchor.column, focus.column),
  };
}

function withinPart(box: CellBox, { first, end }: TablePart): CellBox | null {
  const within = { ...box, top: Math.max(box.top, first), bottom: Math.min(box.bottom, end - 1) };
  return within.top < within.bottom || (within.top === within.bottom && within.left < within.right) ? within : null;
}

function cellAt(grid: HTMLElement | null, x: number, y: number): CellPoint | null {
  const cell = document.elementFromPoint(x, y)?.closest<HTMLElement>("[data-table-cell]");
  if (!cell || !grid?.contains(cell)) return null;
  const row = cellLine(cell), column = Number(cell.dataset.colIndex);
  return Number.isFinite(row) && Number.isFinite(column) ? { row, column } : null;
}

const ARROWS = new Set(["ArrowDown", "ArrowLeft", "ArrowRight", "ArrowUp"]);

/**
 * A table's range of cells, as Notion's (seen 2026-11-02): Shift and the
 * arrow keys grow it from the chosen cell, which keeps its ring, and pressing
 * a cell and moving onto others makes one; its other cells take the chosen
 * cell's faint blue. Escape turns it into its rows chosen, Delete and
 * Backspace clear its cells (one ⌘Z), ⌘C copies them, ⌘X copies and clears
 * them, a letter writes the chosen cell alone. The chosen cell's corner dot
 * pulled up or down gives its value to the cells it passes over, which then
 * are the range; a paste's cells are too (paste). A table's and a timeline's
 * table's ranges are this one piece. A range keeps to the
 * part of the table its chosen cell stands in, a grouped table's group
 * (`part`, tablePart): the keys stop at its edge and the pointer over
 * another group's rows is not followed, as Notion's. Another view shown in
 * its place lets it go, the new view and the one come back to drawing none
 * (useViewSwitchLetsGo), as Notion's (seen 2026-11-26 in Chrome at 1440, two
 * tabs at different times): its lines are that view's, another's rows standing
 * on them.
 */
export function useTableCellRange(options: {
  gridRef: RefObject<HTMLElement | null>;
  /** The lines of the part of the table a line stands in. */
  part(line: number): TablePart;
  columns: number;
  focusCell(row: number, column: number): void;
  onRows(rows: readonly number[]): void;
  onClear(box: CellBox): void;
  onCopy(box: CellBox): void;
  onFill(from: CellPoint, rows: readonly number[]): void;
}) {
  const [range, setRange] = useState<Range | null>(null);
  const latest = useRef(options);
  latest.current = options;
  const stopDrag = useRef<(() => void) | null>(null);
  useEffect(() => () => stopDrag.current?.(), []);
  useViewSwitchLetsGo(options.gridRef, () => { stopDrag.current?.(); setRange(null); });
  // Within the lines of its part as they are now: a paste's rows still to come, or taken back by ⌘Z, are no lines of it.
  const box = range && withinPart(boxOf(range), latest.current.part(range.anchor.row));

  function inRange(row: number, column: number) {
    return Boolean(box && row >= box.top && row <= box.bottom && column >= box.left && column <= box.right);
  }

  /** Handles a key on a chosen cell; true when the range took it. */
  function keyDown(event: KeyboardEvent<HTMLElement>, row: number, column: number) {
    const command = primaryModifier(event);
    if (event.shiftKey && !command && !event.altKey && ARROWS.has(event.key)) {
      const from = range ?? { anchor: { row, column }, focus: { row, column } };
      const { columns } = latest.current, { first, end } = latest.current.part(from.anchor.row);
      const focus = nextTableCell({ ...from.focus, first, rows: end, columns, key: event.key as "ArrowDown" | "ArrowLeft" | "ArrowRight" | "ArrowUp" });
      event.preventDefault();
      setRange({ anchor: from.anchor, focus });
      return true;
    }
    if (!box) {
      // A chosen cell copies its value as shown, as Notion's (seen 2026-11-06), when ⌘C is pressed
      // on the cell itself rather than on words being edited in it; ⌘X copies it and clears it
      // (Notion's, seen 2026-11-26 in two tabs at two times, a table's cell and a timeline's table's).
      const letter = chordLetter(event);
      if (command && !event.shiftKey && !event.altKey && (letter === "c" || letter === "x") && event.target === event.currentTarget) {
        const cell = { top: row, bottom: row, left: column, right: column };
        latest.current.onCopy(cell);
        if (letter === "x") latest.current.onClear(cell);
        return stop(event);
      }
      return false;
    }
    if (event.key === "Escape" && !command && !event.shiftKey) {
      latest.current.onRows(Array.from({ length: box.bottom - box.top + 1 }, (_, index) => box.top + index));
    } else if ((event.key === "Delete" || event.key === "Backspace") && !command && !event.shiftKey && !event.altKey) {
      latest.current.onClear(box);
      return stop(event);
    } else if (command && !event.shiftKey && !event.altKey && (chordLetter(event) === "c" || chordLetter(event) === "x")) {
      // ⌘X copies them and clears them as Delete does, the range staying (Notion's, seen 2026-11-26 in two tabs, a table's and a timeline's table's).
      latest.current.onCopy(box);
      if (chordLetter(event) === "x") latest.current.onClear(box);
      return stop(event);
    } else if (command || event.key === "Shift" || event.key === "Meta" || event.key === "Control" || event.key === "Alt") {
      // ⌘Z and the other commands leave it, as Notion's (seen 2026-11-02).
      return false;
    }
    setRange(null);
    return event.key === "Escape" ? stop(event) : false;
  }

  function stop(event: KeyboardEvent<HTMLElement>) {
    event.preventDefault();
    event.stopPropagation();
    return true;
  }

  // Pressing a cell lets any range go; moving on to another cell while
  // pressed makes one from it, the pressed cell chosen.
  function pointerDown(event: PointerEvent<HTMLElement>, row: number, column: number) {
    if (event.button !== 0 || event.pointerType !== "mouse" || event.shiftKey || event.metaKey || event.ctrlKey) return;
    setRange(null);
    follow(event, row, (at) => {
      if (at.row === row && at.column === column) return;
      window.getSelection()?.removeAllRanges();
      const active = document.activeElement as HTMLElement | null;
      if (!active?.matches("[data-table-cell]")) latest.current.focusCell(row, column);
      setRange({ anchor: { row, column }, focus: at });
    });
  }

  function fillPointerDown(event: PointerEvent<HTMLElement>, row: number, column: number) {
    if (event.button !== 0) return;
    event.preventDefault();
    event.stopPropagation();
    let to = row;
    follow(event, row, (at) => {
      to = at.row;
      setRange({ anchor: { row, column }, focus: { row: to, column } });
    }, () => {
      if (to === row) return;
      const step = to > row ? 1 : -1;
      const rows: number[] = [];
      for (let at = row + step; at !== to + step; at += step) rows.push(at);
      latest.current.onFill({ row, column }, rows);
    });
  }

  // The pointer pressed on a line's cell, followed over the cells of that line's part of the table.
  function follow(event: PointerEvent<HTMLElement>, line: number, move: (at: CellPoint) => void, done?: () => void) {
    stopDrag.current?.();
    const pointerId = event.pointerId, part = latest.current.part(line);
    const onMove = (next: globalThis.PointerEvent) => {
      if (next.pointerId !== pointerId || (next.buttons & 1) === 0) return;
      const at = cellAt(latest.current.gridRef.current, next.clientX, next.clientY);
      if (at && at.row >= part.first && at.row < part.end) move(at);
    };
    const onUp = (next: globalThis.PointerEvent) => {
      if (next.pointerId !== pointerId) return;
      stopDrag.current?.();
      done?.();
    };
    window.addEventListener("pointermove", onMove);
    window.addEventListener("pointerup", onUp);
    window.addEventListener("pointercancel", onUp);
    stopDrag.current = () => {
      window.removeEventListener("pointermove", onMove);
      window.removeEventListener("pointerup", onUp);
      window.removeEventListener("pointercancel", onUp);
      stopDrag.current = null;
    };
  }

  /** A range starts at the chosen cell; another cell chosen lets it go. */
  function chosen(row: number, column: number) {
    if (range && (range.anchor.row !== row || range.anchor.column !== column)) setRange(null);
  }

  /**
   * Where a paste on the chosen cell goes (tablePasteArea): over the range, or
   * from the cell; the cells it fills become the range, rows it makes past the
   * part's last line too as they come (withinPart).
   */
  function paste(row: number, column: number, cells: readonly (readonly string[])[]) {
    const area = tablePasteArea(cells, { row, column }, box);
    const bottom = area.row + area.lines.length - 1;
    const right = Math.min(latest.current.columns - 1, area.column + Math.max(1, ...area.lines.map((line) => line.length)) - 1);
    // The chosen cell stays the range's own end where the paste covers it; else the paste's first cell is chosen.
    const inside = row >= area.row && row <= bottom && column >= area.column && column <= right;
    if (!inside) latest.current.focusCell(area.row, area.column);
    const anchor = inside ? { row, column } : { row: area.row, column: area.column };
    const focus = { row: anchor.row === area.row ? bottom : area.row, column: anchor.column === area.column ? right : area.column };
    setRange(focus.row === anchor.row && focus.column === anchor.column ? null : { anchor, focus });
    return area;
  }

  return { box, inRange, keyDown, pointerDown, fillPointerDown, paste, chosen, clear: () => setRange(null) };
}
