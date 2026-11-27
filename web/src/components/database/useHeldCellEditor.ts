import { useContext, useEffect, useLayoutEffect, useReducer, useRef, useState, type RefObject } from "react";
import { HeldRowSourceContext } from "./heldRowSource";
import { dropCaretsLeftInRows } from "./rowsCaret";
import { lineRowId } from "./viewGroupLines";

/** How a row of the table was last drawn, to draw it again out of sight. */
export type HeldRowDrawing<T> = { key: string; item: T; displayIndex: number; index: number };

type Held<T> = { line: string; box: HTMLElement; at?: { top: number; left: number }; drawn?: HeldRowDrawing<T>; seen: boolean; out: boolean; gone?: boolean };

const FIELD = "input, textarea, [contenteditable=\"true\"]";

// The row line whose value's editor over its cell holds `target`, on a screen
// whose editors stand over their cells (not a phone's, written in the cell).
function editorRow(target: EventTarget | null) {
  if (!(target instanceof HTMLElement) || !target.matches(FIELD)) return null;
  const row = target.closest("[data-cell-editor]")?.closest<HTMLElement>("[data-table-line]");
  return row && !row.closest("[data-phone=\"true\"]") ? row : null;
}

// Moves an editor box by `x` and `y` from over its cell: none, its opening,
// or opening next, over its own cell.
function standBack(editor: HTMLElement, x = 0, y = 0) {
  for (const [name, shift] of [["--cell-editor-hold-x", x], ["--cell-editor-hold-y", y]] as const) {
    const written = editor.style.getPropertyValue(name);
    if (shift) { if (written !== `${shift}px`) editor.style.setProperty(name, `${shift}px`); }
    else if (written) editor.style.removeProperty(name);
  }
}

// An editor box no more held: over its own cell when it opens next.
function release(held: { box: HTMLElement } | null) {
  if (!held) return;
  delete held.box.dataset.cellEditorHeld;
  standBack(held.box);
}

/**
 * A value's editor over a table's cell stays where it opened while its row
 * moves away (a sort its new value changes) or leaves the view (a filter it
 * no longer meets), still taking what is typed, as Notion's (seen
 * 2026-11-14 in a table sorted by a number and filtered under 100: the
 * number's box stayed on line 1 as the row went on to line 6, and stayed
 * there taking keys once the row was gone; Escape and Enter then chose the
 * moved row's cell, or none). The table draws the row on while it holds the
 * editor, out of sight where the view has none (`outOfView`), and stands
 * the editor back where it opened (--cell-editor-hold-x, -y); the moved
 * row's cell shows its value meanwhile (`movedColumn`), as Notion's showed
 * the number on line 6 under the box on line 1. A row gone from the rows
 * the table is drawn from — trashed, archived, deleted, by an automation or
 * someone else — takes its editor with it, as Notion's (seen 2026-11-15
 * twice, in two tabs: a row's title being written, the row put in the trash
 * from elsewhere, the box closed and the next key went nowhere, the words
 * typed before kept on the row; the same row filtered out instead kept its
 * box taking keys, twice). A row under two groups holds it on the line it
 * opened on (viewGroupLines.ts).
 */
export function useHeldCellEditor<T>(listRef: RefObject<HTMLElement | null>) {
  const inSource = useContext(HeldRowSourceContext);
  const held = useRef<Held<T> | null>(null);
  const [, redraw] = useReducer((count: number) => count + 1, 0);
  // The held editor's row and column ("row\ncolumn") while it stands back from its cell, else "".
  const [moved, setMoved] = useState("");
  useEffect(() => {
    const list = listRef.current;
    if (!list) return undefined;
    const onFocusIn = (event: FocusEvent) => {
      const row = editorRow(event.target);
      // Given back to the same editor once its row moved in the page: it stays where it opened.
      const box = (event.target as HTMLElement).closest<HTMLElement>("[data-cell-editor]");
      if (!row || !box || held.current?.line === row.dataset.tableLine) return;
      release(held.current);
      standBack(box);
      // Marked while held, its row out of sight though the focus is lost a moment as its row is moved.
      box.dataset.cellEditorHeld = "true";
      const at: Held<T> = { line: row.dataset.tableLine ?? "", box, seen: true, out: false };
      held.current = at;
      // Where it opened, read once the frame is laid out rather than laying
      // it out early at the press (fitCellEditor), before any key moves it.
      if (typeof ResizeObserver === "undefined") return;
      const laidOut = new ResizeObserver(() => {
        laidOut.disconnect();
        if (held.current === at && row.isConnected) at.at = { top: row.offsetTop, left: row.offsetLeft };
      });
      laidOut.observe(row);
    };
    const onFocusOut = (event: FocusEvent) => {
      const was = held.current;
      if (!was || editorRow(event.target)?.dataset.tableLine !== was.line) return;
      // A row moved in the page gives the focus back in the same drawing.
      queueMicrotask(() => {
        if (held.current !== was || editorRow(document.activeElement)?.dataset.tableLine === was.line) return;
        held.current = null;
        release(was);
        setMoved("");
        if (was.out) redraw();
      });
    };
    list.addEventListener("focusin", onFocusIn);
    list.addEventListener("focusout", onFocusOut);
    // An editor left leaves no caret in the rows (rowsCaret.ts).
    const stopCarets = dropCaretsLeftInRows(list);
    return () => {
      list.removeEventListener("focusin", onFocusIn);
      list.removeEventListener("focusout", onFocusOut);
      stopCarets();
    };
  }, [listRef]);
  // Stood back where it opened, in the drawing that moved its row, before it is painted;
  // its row's cell, then left without it, drawn again with its value in the same frame.
  useLayoutEffect(() => {
    // Its row gone from its source, the editor went with it this drawing: held no more.
    if (held.current?.gone) {
      release(held.current);
      held.current = null;
      if (moved) setMoved("");
      return;
    }
    const now = held.current, opened = now?.at;
    const row = now?.box.isConnected ? now.box.closest<HTMLElement>("[data-table-line]") : null;
    const x = opened && row ? opened.left - row.offsetLeft : 0, y = opened && row ? opened.top - row.offsetTop : 0;
    if (now && opened && row) standBack(now.box, x, y);
    const column = now?.box.closest<HTMLElement>("[data-table-cell]")?.dataset.colIndex;
    const away = now && !now.out && (x || y) && column !== undefined ? `${now.line}\n${column}` : "";
    if (away !== moved) setMoved(away);
  });
  return {
    /** A drawing of the table begins: which rows it draws is yet to be seen. */
    begin() { if (held.current) held.current.seen = false; },
    /** A row's line drawn in the view, kept to draw it on out of sight once the view has it no more. */
    drawn(line: string, drawing: HeldRowDrawing<T>) {
      const at = held.current;
      if (at?.line !== line) return;
      at.drawn = drawing;
      at.seen = true;
    },
    /** The column of `line` whose editor stands back where it opened, its cell showing the value under it. */
    movedColumn(line: string): number | undefined {
      const at = moved.lastIndexOf("\n");
      return moved && moved.slice(0, at) === line ? Number(moved.slice(at + 1)) : undefined;
    },
    /** The row holding the editor that this drawing's view has no more. */
    outOfView(): HeldRowDrawing<T> | null {
      const at = held.current;
      if (at) at.out = !at.seen && Boolean(at.drawn);
      if (at?.out && inSource && !inSource(lineRowId(at.line))) at.gone = true;
      return at?.out && !at.gone ? at.drawn! : null;
    },
  };
}
