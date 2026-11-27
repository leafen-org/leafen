import { useEffect, useMemo, useRef, useState, type PointerEvent as ReactPointerEvent, type RefObject } from "react";
import type { GraphBlock, GraphTextSpan } from "@/contentGraph";
import { requestEditorBlockCommand, requestEditorBlockSelection } from "./editorCommands";
import type { NativeEditorGraphAdapter } from "./editorRuntimeTypes";
import { chordLetter, matchesKeyboardShortcut, primaryModifier, shortcutModifierHeld } from "@/lib/keyboardShortcuts";
import { isTextField } from "@/lib/menuKeyboard";
import { placeCaretInCell, readTableCells, writeTableCells } from "./simpleTableClipboard";
import { mergeSpanAt, spanWithMerges, type SimpleTableMergeSpan } from "./simpleTableMerges";
import { shortcutTextMark } from "./editorInputModel";
import { tableCaretQuiet } from "./simpleTableQuietCaret";
import { imeContinuing } from "@/lib/fieldKeyBoundary";

export type TableCell = { row: number; column: number };
/**
 * Cells chosen from one to another; "row" or "column" when the whole line is
 * chosen — Escape climbing from a row's cells, a grip's menu or a dragged
 * line — which Notion shows with its grip blue.
 */
export type TableCellRange = { from: TableCell; to: TableCell; line?: "row" | "column" };
/** What a chosen line's keys do: Backspace or Delete deletes it, ⌘D duplicates it (Notion, seen 2026-11-09). */
export type TableLineKey = "delete" | "duplicate";
export type TableRangeCorner = "tl" | "tr" | "bl" | "br";

const sameCell = (a: TableCell | null | undefined, b: TableCell | null | undefined) => a?.row === b?.row && a?.column === b?.column;

const CHOOSE_TABLE_CELL = "leafen:choose-table-cell";
const ARROW_STEPS: Record<string, TableCell | undefined> = {
  ArrowUp: { row: -1, column: 0 }, ArrowDown: { row: 1, column: 0 }, ArrowLeft: { row: 0, column: -1 }, ArrowRight: { row: 0, column: 1 },
};

/** Chooses one cell of the table holding the row, as Escape at the start of its text does. */
export function requestTableCellChoice(rowId: string, column: number) {
  window.dispatchEvent(new CustomEvent<{ rowId: string; column: number }>(CHOOSE_TABLE_CELL, { detail: { rowId, column } }));
}

/** The cell of this table an element stands in, by row and column. */
function tableCellAt(canvas: HTMLElement | null, rows: readonly GraphBlock[], target: Element | null): TableCell | null {
  const cell = target?.closest?.("[data-native-table-cell]");
  const row = cell?.closest("[data-table-row-id]");
  if (!canvas || !cell || !row || !canvas.contains(cell)) return null;
  const index = rows.findIndex((candidate) => candidate.id === row.getAttribute("data-table-row-id"));
  const column = [...row.querySelectorAll("[data-native-table-cell]")].indexOf(cell);
  return index >= 0 && column >= 0 ? { row: index, column } : null;
}

/** The rows and columns a range spans, first to last. */
export function tableRangeSpan({ from, to }: TableCellRange) {
  return { top: Math.min(from.row, to.row), bottom: Math.max(from.row, to.row),
    left: Math.min(from.column, to.column), right: Math.max(from.column, to.column) };
}

/**
 * A range grown over each merged cell it touches, its first and last cells
 * the corners on their sides, as Notion's (seen 2026-11-10: dragged from a
 * cell into a merged one, the range takes it whole); the same range when it
 * touches none.
 */
export function rangeWithMerges(range: TableCellRange, spans: readonly SimpleTableMergeSpan[]): TableCellRange {
  // A whole row or column is chosen as it is, through a merged cell (Notion, seen 2026-11-10).
  if (!spans.length || range.line) return range;
  const span = tableRangeSpan(range), grown = spanWithMerges(span, spans);
  if (grown.top === span.top && grown.bottom === span.bottom && grown.left === span.left && grown.right === span.right) return range;
  const down = range.from.row <= range.to.row, across = range.from.column <= range.to.column;
  return { ...range, from: { row: down ? grown.top : grown.bottom, column: across ? grown.left : grown.right },
    to: { row: down ? grown.bottom : grown.top, column: across ? grown.right : grown.left } };
}

/**
 * A simple table's cells as Notion selects them (seen 2026-10-31): the cell
 * holding the caret, or a range, is outlined with a grip on its right edge;
 * dragging from one cell into another, or dragging the grip, selects the
 * cells between as one range instead of their text, and a press on the grip
 * alone selects the table. A range's corners drag it larger or smaller from
 * the corner across. Backspace or Delete empties the range's cells as one
 * change, ⌘D and ⌘R fill it down and right, Shift and an arrow grow it,
 * ⌘C and ⌘X copy its cells (⌘X empties them), a typed letter goes to the
 * end of its last cell, and another key or a press elsewhere lets the
 * range go; cells copied from a table and pasted on a cell spread over the
 * cells from there (seen 2026-11-09). A press on the grip opens the cells'
 * menu. Escape climbs as Notion's does (seen
 * 2026-11-07): in a cell's text the first takes the caret to its start, the
 * next chooses the cell, the next its whole row, the next the table, and
 * the last lets the table go.
 */
export function useSimpleTableCellSelection({ adapter, block, rows, spans, canvasRef, cellsFor, colorOf, onGripPress, onLineKey, onPasteCells }: {
  adapter: NativeEditorGraphAdapter;
  block: GraphBlock;
  rows: readonly GraphBlock[];
  /** Where the table's merged cells stand: a range takes each it touches whole. */
  spans: readonly SimpleTableMergeSpan[];
  canvasRef: RefObject<HTMLDivElement | null>;
  cellsFor: (row: GraphBlock) => GraphTextSpan[][];
  /** The color a cell shows, for its copy. */
  colorOf: (row: GraphBlock, column: number) => string;
  /** A press on the grip, where it was, with the cells it holds. */
  onGripPress: (point: { x: number; y: number }, cells: TableCellRange, fromCaret: boolean) => void;
  /** A chosen line's key; the range chosen after it, or null to let it go. */
  onLineKey: (range: TableCellRange, key: TableLineKey) => TableCellRange | null;
  /** Copied cells pasted from a cell on, with the merged cells among them; the cells they took. */
  onPasteCells: (at: TableCell, cells: GraphTextSpan[][][], merges: readonly SimpleTableMergeSpan[]) => TableCellRange | null;
}) {
  const [caret, setCaret] = useState<TableCell | null>(null);
  const [chosenCells, setRange] = useState<TableCellRange | null>(null);
  const spansKey = JSON.stringify(spans);
  // eslint-disable-next-line react-hooks/exhaustive-deps -- the spans by their places, not their list's identity
  const range = useMemo(() => (chosenCells ? rangeWithMerges(chosenCells, spans) : null), [chosenCells, spansKey]);
  const drag = useRef<{ from: TableCell; byGrip: boolean; moved: boolean } | null>(null);
  const current = useRef({ adapter, block, rows, spans, cellsFor, colorOf, onGripPress, onLineKey, onPasteCells });
  current.current = { adapter, block, rows, spans, cellsFor, colorOf, onGripPress, onLineKey, onPasteCells };
  const chosen = useRef({ caret, range });
  chosen.current = { caret, range };

  // A drag that leaves its cell selects cells: the text selection stays where it began.
  function keepCaret() {
    const selection = document.getSelection();
    if (selection?.anchorNode) selection.collapse(selection.anchorNode, selection.anchorOffset);
  }

  function extend(x: number, y: number, buttons: number) {
    const held = drag.current, canvas = canvasRef.current;
    if (!held || !canvas || !(buttons & 1)) return;
    const to = tableCellAt(canvas, current.current.rows, document.elementFromPoint(x, y));
    if (!to || (!held.moved && sameCell(to, held.from))) return;
    held.moved = true;
    canvas.dataset.cellRange = "dragging";
    keepCaret();
    setRange((range) => (range && sameCell(range.from, held.from) && sameCell(range.to, to) ? range : { from: held.from, to }));
  }

  // The browser stretches the text selection after each move; it is put back
  // before the editor reads it on the release. A press on the grip that
  // didn't move opens the menu of what it holds, at the pointer (Notion,
  // seen 2026-11-09: "색 ›" and "콘텐츠 삭제").
  function finish(event: PointerEvent) {
    const held = drag.current;
    drag.current = null;
    if (canvasRef.current) delete canvasRef.current.dataset.cellRange;
    if (held?.moved) keepCaret();
    if (!held?.byGrip || held.moved) return;
    const { caret: at, range: cells } = chosen.current;
    const pressed = cells ?? (at ? { from: at, to: at } : null);
    if (pressed) current.current.onGripPress({ x: event.clientX, y: event.clientY }, pressed, !cells);
  }

  useEffect(() => {
    if (adapter.readOnly) return undefined;
    const element = (node: Node | null | undefined) => (node instanceof Element ? node : node?.parentElement ?? null);
    const update = () => {
      const selection = document.getSelection();
      const focus = element(selection?.focusNode), anchor = element(selection?.anchorNode);
      // The page's body holds no caret: a selection left behind a blur isn't editing.
      const active = document.activeElement;
      const editing = focus !== null && active !== null && active !== document.body && active.contains(focus);
      // A caret a join just put in a cell shows no outline until it moves (simpleTableQuietCaret).
      const at = editing && !tableCaretQuiet(selection) ? tableCellAt(canvasRef.current, current.current.rows, focus) : null;
      const next = at && sameCell(at, tableCellAt(canvasRef.current, current.current.rows, anchor)) ? at : null;
      setCaret((caret) => (sameCell(caret, next) ? caret : next));
    };
    // Focus leaving the editor moves no selection; read it once focus has settled.
    const settle = () => window.setTimeout(update, 0);
    document.addEventListener("selectionchange", update);
    document.addEventListener("focusout", settle);
    return () => {
      document.removeEventListener("selectionchange", update);
      document.removeEventListener("focusout", settle);
    };
  }, [adapter.readOnly, canvasRef]);

  useEffect(() => {
    const canvas = canvasRef.current;
    if (!canvas || adapter.readOnly) return undefined;
    const down = (event: PointerEvent) => {
      if (event.button !== 0) return;
      const target = event.target as Element | null;
      if (target?.closest?.("[data-table-handle], [data-table-add], [data-table-range-grip], [data-table-range-corner]")) return;
      // Shift and a press on another cell chooses the cells from the caret's
      // (or the range's first) to it, as Notion's (seen 2026-11-07), not text across them.
      if (event.shiftKey) {
        const anchor = chosen.current.range?.from ?? chosen.current.caret;
        const to = tableCellAt(canvas, current.current.rows, target);
        if (anchor && to && !sameCell(anchor, to)) {
          event.preventDefault();
          drag.current = null;
          setRange({ from: anchor, to });
          return;
        }
      }
      setRange(null);
      const from = tableCellAt(canvas, current.current.rows, target);
      drag.current = from ? { from, byGrip: false, moved: false } : null;
    };
    const move = (event: PointerEvent) => extend(event.clientX, event.clientY, event.buttons);
    canvas.addEventListener("pointerdown", down);
    canvas.addEventListener("pointermove", move);
    document.addEventListener("pointerup", finish);
    return () => {
      canvas.removeEventListener("pointerdown", down);
      canvas.removeEventListener("pointermove", move);
      document.removeEventListener("pointerup", finish);
    };
    // extend and finish read only refs, so the first ones serve.
  }, [adapter.readOnly, canvasRef]);

  useEffect(() => {
    if (adapter.readOnly) return undefined;
    const choose = (event: Event) => {
      const { rowId, column } = (event as CustomEvent<{ rowId: string; column: number }>).detail;
      const row = current.current.rows.findIndex((candidate) => candidate.id === rowId);
      if (row < 0) return;
      if (document.activeElement instanceof HTMLElement) document.activeElement.blur();
      setRange({ from: { row, column }, to: { row, column } });
    };
    window.addEventListener(CHOOSE_TABLE_CELL, choose);
    return () => window.removeEventListener(CHOOSE_TABLE_CELL, choose);
  }, [adapter.readOnly]);

  useEffect(() => {
    if (!range) return undefined;
    const key = (event: KeyboardEvent) => {
      if (event.key === "Shift" || event.key === "Meta" || event.key === "Control" || event.key === "Alt") return;
      // A field (a menu's search) keeps its own keys.
      if (isTextField(event.target)) return;
      // Escape on cells of one row chooses the whole row; on a whole line, or
      // cells across rows, the table.
      if (event.key === "Escape") {
        event.preventDefault();
        event.stopPropagation();
        const { top, bottom, left, right } = tableRangeSpan(range);
        const last = Math.max(0, (current.current.cellsFor(current.current.rows[top]) ?? []).length - 1);
        if (document.activeElement instanceof HTMLElement) document.activeElement.blur();
        if (!range.line && top === bottom && (left > 0 || right < last)) {
          setRange({ from: { row: top, column: 0 }, to: { row: top, column: last }, line: "row" });
          return;
        }
        setRange(null);
        requestEditorBlockSelection(current.current.block.id);
        return;
      }
      // ⌘B ⌘I ⌘U ⌘⇧S ⌘E change no chosen cell's words and keep them chosen,
      // as Notion's (seen 2026-11-12).
      if (shortcutTextMark(event)) {
        event.preventDefault();
        event.stopPropagation();
        return;
      }
      // ⌘⇧M comments on the table, the range let go, as Notion's (seen
      // 2026-11-22 in Chrome at 1440: two cells dragged over, the field by the
      // table's top, the thread the table's, no cell's words marked).
      if (matchesKeyboardShortcut("addComment", event)) {
        event.preventDefault();
        event.stopPropagation();
        setRange(null);
        requestEditorBlockCommand(current.current.block.id, "comment", undefined, canvasRef.current ?? undefined);
        return;
      }
      const command = primaryModifier(event) && !event.shiftKey && !event.altKey ? chordLetter(event) : "";
      // A chosen row or column: Backspace or Delete deletes it and chooses
      // the next, ⌘D duplicates it (Notion, seen 2026-11-09).
      if (range.line && (event.key === "Backspace" || event.key === "Delete" || command === "d")) {
        event.preventDefault();
        event.stopPropagation();
        setRange(current.current.onLineKey(range, command === "d" ? "duplicate" : "delete"));
        return;
      }
      if (event.key === "Backspace" || event.key === "Delete") {
        event.preventDefault();
        event.stopPropagation();
        const { top, bottom, left, right } = tableRangeSpan(range);
        emptyCells(top, bottom, left, right);
        return;
      }
      // ⌘D fills each column from the range's top row, ⌘R each row from its left
      // column, as one change (Notion's ⌘D, seen 2026-11-05; ⌘R is the browser's to the tool that looked).
      if (command === "d" || command === "r") {
        event.preventDefault();
        event.stopPropagation();
        const { top, bottom, left, right } = tableRangeSpan(range);
        const { adapter: editor, rows: lines, cellsFor: cellsOf } = current.current;
        const first = cellsOf(lines[top]);
        editor.replaceBlocks(lines.slice(top, bottom + 1).map((row) => {
          const cells = cellsOf(row);
          return { ...row, content: { ...row.content, cells: cells.map((cell, at) => {
            if (at < left || at > right) return cell;
            return command === "d" ? first[at] : cells[left];
          }) } };
        }));
        return;
      }
      // ⌘C, ⌘X and ⌘V keep the range for their clipboard events below; ⌘Z
      // and ⌘⇧Z keep it for the page's undo, as Notion's keeps the chosen
      // cell (seen 2026-11-15 and 2026-11-16: a cell emptied, → to the next,
      // ⌘Z brought the words back with the next cell still chosen).
      if (command === "c" || command === "x" || command === "v") return;
      if (primaryModifier(event) && !event.altKey && ["z", "y"].includes(chordLetter(event))) return;
      if (moveRange(event, range)) return;
      // A typed letter goes to the end of the range's last cell, the range or
      // line let go, as Notion's (seen 2026-11-09: "q" on its second row made
      // "c2q", "k" over a1:b2 "b2k").
      if (event.key.length === 1 && !shortcutModifierHeld(event) && !imeContinuing(event)) {
        const { bottom, right } = tableRangeSpan(range);
        // A merged cell there takes it in its first cell.
        const merged = mergeSpanAt(current.current.spans, bottom, right);
        const row = current.current.rows[merged?.top ?? bottom];
        if (row) placeCaretInCell(canvasRef.current, row.id, merged?.left ?? right, "end");
      }
      setRange(null);
    };
    const press = (event: PointerEvent) => {
      if (!canvasRef.current?.contains(event.target as Node)) setRange(null);
    };
    // ⌘C writes the cells; ⌘X writes and empties them, the range kept (Notion, seen 2026-11-09).
    const copy = (event: ClipboardEvent) => {
      if (!event.clipboardData || isTextField(event.target)) return;
      event.preventDefault();
      event.stopPropagation();
      const { top, bottom, left, right } = tableRangeSpan(range);
      const { rows: lines, cellsFor: cellsOf, colorOf } = current.current;
      const chosenRows = lines.slice(top, bottom + 1);
      // The merged cells among them, by their places in the copy.
      const merges = current.current.spans.filter((span) => span.top >= top && span.bottom <= bottom && span.left >= left && span.right <= right)
        .map((span) => ({ top: span.top - top, bottom: span.bottom - top, left: span.left - left, right: span.right - left }));
      writeTableCells(event.clipboardData, chosenRows.map((row) => cellsOf(row).slice(left, right + 1)),
        (row, column) => colorOf(chosenRows[row], left + column), merges);
      if (event.type === "cut") emptyCells(top, bottom, left, right);
    };
    document.addEventListener("keydown", key, true);
    document.addEventListener("pointerdown", press, true);
    document.addEventListener("copy", copy, true);
    document.addEventListener("cut", copy, true);
    return () => {
      document.removeEventListener("keydown", key, true);
      document.removeEventListener("pointerdown", press, true);
      document.removeEventListener("copy", copy, true);
      document.removeEventListener("cut", copy, true);
    };
  }, [range, canvasRef]);

  // Cells a table copied, pasted on a chosen cell or range or on the caret's
  // cell, spread over the cells from its first on, the table growing to
  // hold them, and those cells are chosen (Notion, seen 2026-11-09). Other
  // clipboards paste as text in the cell.
  useEffect(() => {
    if (adapter.readOnly) return undefined;
    const paste = (event: ClipboardEvent) => {
      if (!event.clipboardData || isTextField(event.target)) return;
      const { range: cells, caret: at } = chosen.current;
      const from = cells ? (({ top, left }) => ({ row: top, column: left }))(tableRangeSpan(cells))
        : at && canvasRef.current?.contains(document.getSelection()?.focusNode ?? null) ? at : null;
      if (!from) return;
      const copied = readTableCells(event.clipboardData);
      if (!copied) return;
      event.preventDefault();
      event.stopPropagation();
      const taken = current.current.onPasteCells(from, copied.grid, copied.merges);
      if (!taken) return;
      if (document.activeElement instanceof HTMLElement) document.activeElement.blur();
      setRange(taken);
    };
    document.addEventListener("paste", paste, true);
    return () => document.removeEventListener("paste", paste, true);
  }, [adapter.readOnly, canvasRef]);

  function emptyCells(top: number, bottom: number, left: number, right: number) {
    const { adapter: editor, rows: lines, cellsFor: cellsOf } = current.current;
    editor.replaceBlocks(lines.slice(top, bottom + 1).map((row) => ({ ...row, content: { ...row.content,
      cells: cellsOf(row).map((cell, at) => (at >= left && at <= right ? [] : cell)) } })));
  }

  // Shift and an arrow grow or shrink chosen cells from their first cell, and
  // ↑ or ↓ move a chosen row to the next (Shift: grow the rows), ← or → a
  // chosen column, as Notion's (seen 2026-11-08); an arrow alone chooses one
  // cell (plainArrowCell). Other arrows let the choice go.
  function moveRange(event: KeyboardEvent, range: TableCellRange) {
    const step = ARROW_STEPS[event.key];
    if (!step || event.metaKey || event.ctrlKey || event.altKey) return false;
    const { line } = range;
    if (line === "row" ? step.column !== 0 : line === "column" ? step.row !== 0 : false) return false;
    event.preventDefault();
    event.stopPropagation();
    const { rows: lines, cellsFor: cellsOf } = current.current;
    const lastRow = lines.length - 1;
    const lastColumn = Math.max(0, (cellsOf(lines[range.to.row]) ?? []).length - 1);
    const clamp = (value: number, last: number) => Math.min(last, Math.max(0, value));
    if (!line && !event.shiftKey) {
      const to = plainArrowCell(range, event.key, lastRow, lastColumn, current.current.spans);
      setRange({ from: to, to });
      return true;
    }
    if (line && !event.shiftKey) {
      const { top, bottom, left, right } = tableRangeSpan(range);
      setRange(line === "row" ? lineRange("row", clamp((step.row < 0 ? top : bottom) + step.row, lastRow), lastColumn)
        : lineRange("column", clamp((step.column < 0 ? left : right) + step.column, lastColumn), lastRow));
      return true;
    }
    setRange({ ...range, to: { row: line === "column" ? lastRow : clamp(range.to.row + step.row, lastRow),
      column: line === "row" ? lastColumn : clamp(range.to.column + step.column, lastColumn) } });
    return true;
  }

  // Dragging from a grip or a corner re-selects from a cell that stays put:
  // the caret's, the range's first, or the corner across.
  const dragFrom = (from: TableCell, byGrip: boolean) => ({
    onPointerDown: (event: ReactPointerEvent<HTMLElement>) => {
      if (event.button !== 0) return;
      event.preventDefault();
      event.stopPropagation();
      event.currentTarget.setPointerCapture(event.pointerId);
      drag.current = { from, byGrip, moved: false };
    },
    onPointerMove: (event: ReactPointerEvent<HTMLElement>) => extend(event.clientX, event.clientY, event.buttons),
  });
  // The caret's cell has the corners too (Notion, seen 2026-11-09).
  const span = range ? tableRangeSpan(range) : caret ? tableRangeSpan({ from: caret, to: caret }) : null;
  const gripProps = range ? dragFrom(range.from, true) : caret ? dragFrom(caret, true) : null;
  const cornerProps = (corner: TableRangeCorner) => span ? dragFrom({
    row: corner.startsWith("t") ? span.bottom : span.top, column: corner.endsWith("l") ? span.right : span.left }, false) : null;

  // A press elsewhere, or a menu taking the keys, lets the cells go; a menu's
  // end or a moved line chooses them again (blurring the caret's cell). A
  // closing menu gives its opener the focus back after this, so the caret's
  // cell is blurred again a frame later: chosen cells show no caret, and the
  // focus is the page's, as Notion's (seen 2026-11-09); an opener outside the
  // page (its "⋯") keeps it, as there.
  const choose = (next: TableCellRange | null) => {
    const blurCaret = () => {
      if (document.activeElement instanceof HTMLElement && canvasRef.current?.closest('[contenteditable="true"]')?.contains(document.activeElement)) {
        document.activeElement.blur();
      }
    };
    if (next) { blurCaret(); requestAnimationFrame(blurCaret); }
    setRange(next);
  };

  return { caret, range, gripProps, cornerProps, choose };
}

/**
 * The cell an arrow alone chooses from chosen cells, as Notion's (seen
 * 2026-11-14, 2026-11-15 and 2026-11-16 in another tab, a second between
 * the keys, 3×3 and 4×4 tables). From one cell, the next that way: ← and →
 * run on through the rows — from a row's first cell to the last of the row
 * before, from its last to the first of the next — and stay at the table's
 * first and last cells; ↑ and ↓ stay at its top and foot. From cells across,
 * one cell by the cell they were stretched to: ← the cell before the
 * range's first column in that cell's row (from the table's first cell, the
 * range's last row's first cell), → the range's last column in that row, ↑
 * the row over the range in that cell's column, ↓ the range's last row
 * there ("a1:b2" stretched from a1: ← c1, → b2, ↑ b1, ↓ b2; "b2:c3"
 * stretched from c3 to b2: ← a2, → c2, ↑ b1, ↓ b3). A merged cell moves as
 * one cell, from its first row and column back and its last ones on.
 */
export function plainArrowCell(range: TableCellRange, key: string, lastRow: number, lastColumn: number,
  spans: readonly SimpleTableMergeSpan[] = []): TableCell {
  const { top, bottom, left, right } = tableRangeSpan(range), focus = range.to;
  const before = ({ row, column }: TableCell) => column > 0 ? { row, column: column - 1 } : row > 0 ? { row: row - 1, column: lastColumn } : null;
  const after = ({ row, column }: TableCell) => column < lastColumn ? { row, column: column + 1 } : row < lastRow ? { row: row + 1, column: 0 } : null;
  const merged = mergeSpanAt(spans, top, left);
  if ((top === bottom && left === right) || merged && merged.top === top && merged.bottom === bottom && merged.left === left && merged.right === right) {
    if (key === "ArrowLeft") return before({ row: top, column: left }) ?? { row: top, column: left };
    if (key === "ArrowRight") return after({ row: bottom, column: right }) ?? { row: bottom, column: right };
    return key === "ArrowUp" ? { row: Math.max(0, top - 1), column: left } : { row: Math.min(lastRow, bottom + 1), column: left };
  }
  if (key === "ArrowLeft") return before({ row: focus.row, column: left }) ?? { row: bottom, column: left };
  if (key === "ArrowRight") return { row: focus.row, column: right };
  return key === "ArrowUp" ? { row: Math.max(0, top - 1), column: focus.column } : { row: bottom, column: focus.column };
}

/** A whole row or column chosen, as its grip's menu or Escape leaves it. */
export function lineRange(line: "row" | "column", index: number, last: number): TableCellRange {
  return line === "row" ? { from: { row: index, column: 0 }, to: { row: index, column: last }, line }
    : { from: { row: 0, column: index }, to: { row: last, column: index }, line };
}
