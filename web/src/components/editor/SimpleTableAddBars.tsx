import { type CSSProperties, useRef, useState } from "react";
import type { GraphBlock, GraphTextSpan } from "@/contentGraph";
import { Plus } from "@/icons/leafen";
import { blockItemText } from "./blockItemLabels";
import { insertEditorChildBlock, removeEditorBlocks, replaceEditorBlocks } from "./editorGraphModel";
import type { simpleTableActions } from "./simpleTableActions";
import type { NativeEditorGraphAdapter } from "./editorRuntimeTypes";
import styles from "./editorStyles.generated.module.css";
import { readsRightToLeft } from "@/lib/readingDirection";

export type AddEdge = "right" | "bottom" | "corner";
type Grow = { edge: AddEdge; columns: number; rows: number };
type Box = { left: number; top: number; width: number; height: number };
/** Where a simple table's rows and its first row's cells stand in its canvas. */
export type SimpleTableShape = { rows: Box[]; columns: Box[]; top: number; bottom: number; left: number; right: number };
// A press that moves this far is a drag.
export const DRAG_START = 4;

/**
 * The "+" bar a cell shows, as Notion's (seen 2026-10-31): a cell of the last
 * column the right one, of the last row the bottom one, and the corner cell
 * the one whose edge the pointer is nearer for the cell's size.
 */
export function addEdge(cell: Element, event: PointerEvent, lastRow: boolean, lastColumn: boolean): AddEdge | undefined {
  if (!lastRow || !lastColumn) return lastColumn ? "right" : lastRow ? "bottom" : undefined;
  const rect = cell.getBoundingClientRect();
  // The column bar ("right") stands at the table's end: its left read from the right.
  const across = readsRightToLeft(cell) ? event.clientX - rect.left : rect.right - event.clientX;
  return across / rect.width < (rect.bottom - event.clientY) / rect.height ? "right" : "bottom";
}

/**
 * A simple table's "+" bars and corner, as Notion's (seen 2026-10-31): the
 * right one adds a column, the bottom one a row, the corner both as one
 * change; dragged, they add or take back lines (SimpleTableHandles shows
 * which bar under the pointer).
 */
export function SimpleTableAddBars({ adapter, block, rows, width, shape, rtl, cellsFor, actions, hoverEdge, menuOpen }: {
  adapter: NativeEditorGraphAdapter;
  block: GraphBlock;
  rows: readonly GraphBlock[];
  width: number;
  shape: SimpleTableShape;
  /** The table reads from the right: its columns run leftward and grow past its left. */
  rtl: boolean;
  cellsFor: (row: GraphBlock) => GraphTextSpan[][];
  actions: ReturnType<typeof simpleTableActions>;
  hoverEdge: AddEdge | undefined;
  menuOpen: boolean;
}) {
  // A "+" bar dragged: the lines it would leave, shown as it goes and made at release.
  const [grow, setGrow] = useState<Grow | null>(null);
  const growPress = useRef<{ x: number; y: number; moved: boolean } | null>(null);
  const grew = useRef(false);
  const { columns, withColumns, reorderColumns, addRow, addRowAndColumn } = actions;
  // A "+" bar dragged adds a line for each line's length it goes, and going
  // back takes away only empty lines at the end, as one change, as Notion's
  // (seen 2026-10-31: 180px past 120px columns added two, back took the two
  // empty ones and no further).
  const columnStep = shape.columns.at(-1)?.width || 120;
  const rowStep = shape.rows.at(-1)?.height || 35;
  const emptyCell = (spans: GraphTextSpan[] | undefined) => !spans || spans.every((span) => !span.text);
  const emptyAtEnd = (lines: number, empty: (at: number) => boolean) => {
    let count = 0;
    while (count < lines - 1 && empty(lines - 1 - count)) count += 1;
    return count;
  };
  // Half a line rounds away from where it started, either way.
  const lines = (distance: number, step: number) => Math.sign(distance) * Math.round(Math.abs(distance) / step);
  const growTo = (edge: AddEdge, dx: number, dy: number): Grow => ({
    edge,
    columns: edge === "bottom" ? width : Math.min(100, Math.max(width - emptyAtEnd(width, (at) => rows.every((row) => emptyCell(cellsFor(row)[at]))),
      width + lines(rtl ? -dx : dx, columnStep))),
    rows: edge === "right" ? rows.length : Math.max(rows.length - emptyAtEnd(rows.length, (at) => cellsFor(rows[at]).every(emptyCell)),
      rows.length + lines(dy, rowStep)),
  });
  const commitGrow = ({ columns: columnsTo, rows: rowsTo }: Grow) => {
    if (columnsTo === width && rowsTo === rows.length) return;
    adapter.changeLayout((graph) => {
      let next = { graph, operations: [] as ReturnType<typeof replaceEditorBlocks>["operations"] };
      const step = (change: typeof next) => { next = { graph: change.graph, operations: [...next.operations, ...change.operations] }; };
      if (columnsTo !== width) {
        step(replaceEditorBlocks(next.graph, withColumns(columnsTo > width
          ? [...columns, ...Array.from({ length: columnsTo - width }, () => null)] : columns.slice(0, columnsTo))));
      }
      if (rowsTo < rows.length) step(removeEditorBlocks(next.graph, rows.slice(rowsTo).map((row) => row.id)));
      for (let added = rows.length; added < rowsTo; added += 1) {
        step(insertEditorChildBlock(next.graph, block.id, "table_row", { cells: Array.from({ length: columnsTo }, () => []) }));
      }
      return next;
    });
  };
  // What the drag would make, drawn over the table: new cells past its edge,
  // the page's ground over lines it would take away.
  const ghosts: { box: Box; cover?: boolean; first?: boolean }[] = [];
  if (grow) {
    const columnsAdded = Array.from({ length: Math.max(0, grow.columns - width) }, (_, at): Box =>
      ({ left: rtl ? shape.left - (at + 1) * columnStep : shape.right + at * columnStep, top: 0, width: columnStep, height: 0 }));
    for (const row of shape.rows) for (const column of columnsAdded) ghosts.push({ box: { ...column, top: row.top, height: row.height } });
    const allColumns = [...shape.columns, ...columnsAdded];
    for (let at = 0; at < grow.rows - rows.length; at += 1) {
      allColumns.slice(0, grow.columns).forEach((column, index) =>
        ghosts.push({ box: { left: column.left, top: shape.bottom + at * rowStep, width: column.width, height: rowStep }, first: index === 0 }));
    }
    const cutColumn = shape.columns[grow.columns];
    if (cutColumn) ghosts.push({ cover: true, box: rtl
      ? { left: shape.left - 1, top: shape.top - 1, width: cutColumn.left + cutColumn.width - shape.left + 2, height: shape.bottom - shape.top + 2 }
      : { left: cutColumn.left, top: shape.top - 1, width: shape.right - cutColumn.left + 2, height: shape.bottom - shape.top + 2 } });
    const cutRow = shape.rows[grow.rows];
    if (cutRow) ghosts.push({ cover: true, box: { left: shape.left - 1, top: cutRow.top, width: shape.right - shape.left + 2, height: shape.bottom - cutRow.top + 2 } });
  }
  const barShift = (edge: AddEdge): CSSProperties | undefined => {
    if (!grow || grow.edge !== edge) return undefined;
    const cut = shape.columns[grow.columns];
    const x = edge === "bottom" ? 0 : grow.columns >= width ? (grow.columns - width) * columnStep * (rtl ? -1 : 1)
      : rtl ? (cut ? cut.left + cut.width : shape.left) - shape.left : (cut?.left ?? shape.right) - shape.right;
    const y = edge === "right" ? 0 : grow.rows >= rows.length ? (grow.rows - rows.length) * rowStep
      : (shape.rows[grow.rows]?.top ?? shape.bottom) - shape.bottom;
    return { transform: `translate(${x}px, ${y}px)` };
  };
  const add = (edge: AddEdge, label: string, tip: string, onClick: () => void, disabled = false) => (
    <button key={edge} type="button" className={styles.simpleTableAdd} contentEditable={false} data-table-add={edge}
      data-shown={!menuOpen && edge !== "corner" && (hoverEdge === edge || grow?.edge === edge) ? "true" : undefined}
      aria-label={label} data-icon-tip={tip} disabled={disabled} style={barShift(edge)}
      // A press takes no focus: the caret stays where it was and what is typed
      // next goes there, in a cell or nowhere, as Notion's bars (seen
      // 2026-11-20 in Chrome 1440, a letter typed after each press: the
      // bottom one from a cell, from the page and from the workspace button,
      // the right one from a cell). Here the bar took the focus, and Space or
      // Enter added another line.
      onMouseDown={(event) => event.preventDefault()}
      onPointerDown={(event) => {
        if (event.button !== 0) return;
        event.currentTarget.setPointerCapture(event.pointerId);
        growPress.current = { x: event.clientX, y: event.clientY, moved: false };
      }}
      onPointerMove={(event) => {
        const held = growPress.current;
        if (!held) return;
        const dx = event.clientX - held.x, dy = event.clientY - held.y;
        if (!held.moved && Math.hypot(dx, dy) < DRAG_START) return;
        held.moved = true;
        setGrow(growTo(edge, dx, dy));
      }}
      onPointerUp={(event) => {
        const held = growPress.current;
        growPress.current = null;
        setGrow(null);
        if (!held?.moved) return;
        // The click that follows the release is the drag's, not a press of the bar.
        grew.current = true;
        commitGrow(growTo(edge, event.clientX - held.x, event.clientY - held.y));
      }}
      onPointerCancel={() => { growPress.current = null; setGrow(null); }}
      onClick={() => { if (grew.current) { grew.current = false; return; } onClick(); }}>
      <Plus size="micro" aria-hidden="true" />
    </button>
  );

  return <>
    {ghosts.map(({ box, cover, first }, at) => <div key={at} className={cover ? styles.simpleTableGrowCover : styles.simpleTableGrowCell}
      contentEditable={false} aria-hidden="true" data-first={first ? "true" : undefined} style={box as CSSProperties} />)}
    {add("right", blockItemText("columns.add"), blockItemText("simpleTable.addColumnTip"), () => reorderColumns([...columns, null]), width >= 100)}
    {add("bottom", blockItemText("simpleTable.addRowShort"), blockItemText("simpleTable.addRowTip"), addRow)}
    {add("corner", blockItemText("simpleTable.addRowAndColumn"), blockItemText("simpleTable.resizeTip"), addRowAndColumn, width >= 100)}
  </>;
}
