import { i18next } from "@/i18n";
import { type CSSProperties, type ReactNode, type RefObject, useEffect, useRef, useState } from "react";
import type { GraphBlock, GraphTextSpan } from "@/contentGraph";
import { DragHandleIcon } from "@/icons/leafen";
import type { PopoverRect } from "@/lib/popoverPlacement";
import { blockItemText } from "./blockItemLabels";
import { BlockMenuFooter } from "./BlockMenuFooter";
import { SimpleTableAddBars, addEdge, DRAG_START, type AddEdge } from "./SimpleTableAddBars";
import { SimpleTableMenu } from "./SimpleTableMenu";
import { simpleTableActions, type SimpleTableLine } from "./simpleTableActions";
import { lineRange, tableRangeSpan, useSimpleTableCellSelection, type TableCellRange } from "./useSimpleTableCellSelection";
import { simpleTableCellColor, simpleTableListedColor } from "./simpleTableColumns";
import { mergeSpanAt, mergesWithin, simpleTableGridBoxes, simpleTableMergeSpans, spanWithMerges } from "./simpleTableMerges";
import type { NativeEditorGraphAdapter } from "./editorRuntimeTypes";
import styles from "./editorStyles.generated.module.css";
import { readsRightToLeft } from "@/lib/readingDirection";
import { fieldEnter } from "@/lib/fieldKeyBoundary";

type Line = SimpleTableLine;
type Span = ReturnType<typeof tableRangeSpan>;
// A grip's menu: a row's or a column's, or the cells a cell's grip held — a
// range, or the caret's cell, which has the caret back when it closes.
type Menu = (Line & { anchor: PopoverRect }) | { kind: "cells"; span: Span; fromCaret: boolean; anchor: PopoverRect };
type Box = { left: number; top: number; width: number; height: number };
type DragCell = { id: string; box: Box; text: string; background: string; color: string };

/** Moves one item of a list to another place in it. */
function moved<T>(list: readonly T[], from: number, to: number): T[] {
  const next = [...list];
  const [item] = next.splice(from, 1);
  next.splice(to, 0, item);
  return next;
}

/**
 * A simple table's row and column handles, as Notion's (seen 2026-10-29 on
 * a synthetic 3×3 table): over a cell, a 6×18 grip on its row's left edge
 * and an 18×6 grip on its column's top edge, each across the edge's middle.
 * A press selects the row or column and opens its menu (SimpleTableMenu);
 * dragging one moves the row or column, a blue line showing where. Each
 * change is one step to undo, and the row or column it leaves stays chosen,
 * as Notion's (seen 2026-11-09: after a menu's action or Escape, after a
 * drag, the new row after "위에 삽입").
 */
export function SimpleTableHandles({ adapter, block, rows, width, canvasRef, cellsFor }: {
  adapter: NativeEditorGraphAdapter;
  block: GraphBlock;
  rows: readonly GraphBlock[];
  width: number;
  canvasRef: RefObject<HTMLDivElement | null>;
  cellsFor: (row: GraphBlock) => GraphTextSpan[][];
}) {
  const [hover, setHover] = useState<{ row: number; column: number; edge?: AddEdge } | null>(null);
  const [menu, setMenu] = useState<Menu | null>(null);
  // A line dragged: where it would go, how far it has come along its way,
  // and its cells as they were, drawn following the pointer.
  const [drop, setDrop] = useState<(Line & { to: number; shift: number; cells: DragCell[] }) | null>(null);
  const press = useRef<(Line & { x: number; y: number; dragging: boolean }) | null>(null);
  // Merged cells stand over others: a chosen cell or range takes each it touches whole.
  const merges = simpleTableMergeSpans(rows, width);
  const withMerges = (span: Span) => spanWithMerges(span, merges);
  // A table with merged cells is measured by its grid, which the editor
  // lays out again after this draws when its rows change: measure again
  // a frame later.
  const [, remeasure] = useState(0);
  const shapeKey = merges.length ? `${rows.map((row) => row.id).join(" ")}|${width}|${JSON.stringify(merges)}` : "";
  useEffect(() => {
    if (!shapeKey) return undefined;
    const frame = requestAnimationFrame(() => remeasure((count) => count + 1));
    return () => cancelAnimationFrame(frame);
  }, [shapeKey]);
  // The rows come into the canvas after this first draws, and the canvas
  // grows and shrinks with them: measured again whenever its box changes,
  // the "+" bars stand from the start, so a press under a table no pointer
  // has crossed yet adds its row, as Notion's (seen 2026-11-20 in Chrome
  // 1440 on a page just opened: 8–24px under the table a row came; here the
  // bars came only once a cell was pointed at, and the press chose the
  // block).
  useEffect(() => {
    const canvas = canvasRef.current;
    if (!canvas || typeof ResizeObserver === "undefined") return undefined;
    const observer = new ResizeObserver(() => remeasure((count) => count + 1));
    observer.observe(canvas);
    return () => observer.disconnect();
  }, [canvasRef]);
  const cells = useSimpleTableCellSelection({ adapter, block, rows, spans: merges, canvasRef, cellsFor,
    colorOf: (row, column) => simpleTableCellColor(block, row, column),
    // A menu takes the keys: the cells are let go while it is open.
    onGripPress: (point, held, fromCaret) => {
      cells.choose(null);
      setMenu({ kind: "cells", span: withMerges(tableRangeSpan(held)), fromCaret, anchor: { left: point.x, right: point.x, top: point.y, bottom: point.y } });
    },
    onLineKey: (range, key) => lineKey(range, key),
    onPasteCells: (at, grid, copiedMerges) => pasteCells(at, grid, copiedMerges) });

  // The table's rows and its first row's cells, where they stand in the
  // canvas; a table with merged cells, whose rows have no boxes, by its grid.
  function layout() {
    const canvas = canvasRef.current;
    if (!canvas) return null;
    const origin = canvas.getBoundingClientRect();
    const box = (element: Element): Box => {
      const rect = element.getBoundingClientRect();
      return { left: rect.left - origin.left, top: rect.top - origin.top, width: rect.width, height: rect.height };
    };
    const grid = canvas.querySelector<HTMLElement>("[role=table][data-table-merged]");
    const rowElements = [...canvas.querySelectorAll("[data-table-row-id]")];
    const first = rowElements[0];
    if (!first) return null;
    const tracks = grid ? simpleTableGridBoxes(grid, origin) : null;
    const rowBoxes = tracks?.rows ?? rowElements.map(box);
    const columnBoxes = tracks?.columns ?? [...first.querySelectorAll("[data-native-table-cell]")].map(box);
    if (!rowBoxes.length) return null;
    const last = rowBoxes[rowBoxes.length - 1];
    // Its columns in their order, from the right where the table reads from the right.
    return { origin, rows: rowBoxes, columns: columnBoxes, top: rowBoxes[0].top, bottom: last.top + last.height,
      left: columnBoxes.length ? Math.min(...columnBoxes.map((column) => column.left)) : rowBoxes[0].left,
      right: columnBoxes.length ? Math.max(...columnBoxes.map((column) => column.left + column.width)) : 0 };
  }

  useEffect(() => {
    const canvas = canvasRef.current;
    if (!canvas || adapter.readOnly) return undefined;
    const move = (event: PointerEvent) => {
      if (press.current || menu) return;
      const target = event.target as Element | null;
      // Over a "+" bar the cell stays chosen, and the bar under the pointer is the one shown.
      const bar = target?.closest?.("[data-table-add]")?.getAttribute("data-table-add") as AddEdge | undefined;
      if (bar) {
        setHover((current) => (current && current.edge !== bar ? { ...current, edge: bar } : current));
        return;
      }
      const cell = target?.closest?.("[data-native-table-cell]");
      const row = cell?.closest("[data-table-row-id]");
      if (!cell || !row) return;
      const rowIndex = rows.findIndex((candidate) => candidate.id === row.getAttribute("data-table-row-id"));
      const column = [...row.querySelectorAll("[data-native-table-cell]")].indexOf(cell);
      if (rowIndex < 0 || column < 0) return;
      // A merged cell reaches the last row or column by its own end.
      const merged = mergeSpanAt(merges, rowIndex, column);
      const edge = addEdge(cell, event, (merged?.bottom ?? rowIndex) === rows.length - 1, (merged?.right ?? column) === width - 1);
      setHover((current) => (current?.row === rowIndex && current.column === column && current.edge === edge ? current : { row: rowIndex, column, edge }));
    };
    const leave = () => { if (!press.current && !menu) setHover(null); };
    canvas.addEventListener("pointermove", move);
    canvas.addEventListener("pointerleave", leave);
    return () => {
      canvas.removeEventListener("pointermove", move);
      canvas.removeEventListener("pointerleave", leave);
    };
    // The merged cells by their places, not their list's identity.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [adapter.readOnly, canvasRef, menu, rows, width, JSON.stringify(merges)]);

  const actions = simpleTableActions({ adapter, block, rows, width, cellsFor });
  const { columns, reorderColumns, moveRow, run, lineKey, pasteCells, runCells, lastRowIndex, lastColumnIndex } = actions;

  if (adapter.readOnly) return null;
  const shape = layout();
  if (!shape) return null;
  const rtl = readsRightToLeft(canvasRef.current);

  // Where a dragged row or column would go: before the one under the pointer's middle.
  function target(kind: Line["kind"], x: number, y: number) {
    if (!shape) return 0;
    const boxes = kind === "row" ? shape.rows : shape.columns;
    const at = kind === "row" ? y - shape.origin.top : x - shape.origin.left;
    const found = boxes.findIndex((box) => kind === "row" ? at < box.top + box.height / 2
      : rtl ? at > box.left + box.width / 2 : at < box.left + box.width / 2);
    return found < 0 ? boxes.length : found;
  }

  // The moved row or column is chosen where it lands (Notion, seen 2026-11-09).
  function finishDrag(line: Line, to: number) {
    // `to` counts slots before the items as they stand; the item leaves its own.
    const place = to > line.index ? to - 1 : to;
    cells.choose(line.kind === "row" ? lineRange("row", place, lastColumnIndex) : lineRange("column", place, lastRowIndex));
    if (place === line.index) return;
    if (line.kind === "row") {
      moveRow(line.index, place);
      return;
    }
    reorderColumns(moved(columns, line.index, place), line.index);
  }
  // The cells of a line as they stand, for the copy a drag carries.
  function dragCells(line: Line): DragCell[] {
    const canvas = canvasRef.current;
    if (!canvas) return [];
    const origin = canvas.getBoundingClientRect();
    const lineRows = line.kind === "row" ? [rows[line.index]] : rows;
    return lineRows.flatMap((row) => {
      const rowCells = row ? [...canvas.querySelectorAll(`[data-table-row-id="${CSS.escape(row.id)}"] [data-native-table-cell]`)] : [];
      return line.kind === "row" ? rowCells : rowCells.slice(line.index, line.index + 1);
    }).map((cell) => {
      const rect = cell.getBoundingClientRect(), look = getComputedStyle(cell);
      return { id: cell.getAttribute("data-table-cell") ?? "", text: cell.textContent ?? "", background: look.backgroundColor, color: look.color,
        box: { left: rect.left - origin.left, top: rect.top - origin.top, width: rect.width, height: rect.height } };
    });
  }
  // A menu takes the keys: what was chosen is let go while it is open.
  const openMenu = (line: Line, rect: DOMRect) => {
    cells.choose(null);
    setMenu({ ...line, anchor: { top: rect.top, bottom: rect.bottom, left: rect.left, right: rect.right } });
  };

  // A grip is a 14×22 button (22×14 on a column) across its edge's middle,
  // drawn at rest as a 6×18 bar; over it, its six dots on the raised ground;
  // its whole row or column chosen, blue with white dots — as Notion's (seen
  // 2026-11-07: a row chosen by Esc shows its grip so with the pointer away).
  function handle(line: Line, box: Box, chosen = false): ReactNode {
    const label = blockItemText(line.kind === "row" ? "simpleTable.rowHandle" : "simpleTable.columnHandle", { number: line.index + 1 });
    return (
      <button key={`${line.kind}-${line.index}`} type="button" className={styles.simpleTableHandle} contentEditable={false} data-table-handle={line.kind}
        data-chosen={chosen ? "true" : undefined} aria-label={label} title={label} aria-haspopup="menu" style={box as CSSProperties}
        onPointerDown={(event) => {
          if (event.button !== 0) return;
          event.preventDefault();
          event.currentTarget.setPointerCapture(event.pointerId);
          press.current = { ...line, x: event.clientX, y: event.clientY, dragging: false };
        }}
        onPointerMove={(event) => {
          const held = press.current;
          if (!held) return;
          if (!held.dragging && Math.hypot(event.clientX - held.x, event.clientY - held.y) < DRAG_START) return;
          held.dragging = true;
          const shift = line.kind === "row" ? event.clientY - held.y : event.clientX - held.x;
          setDrop((dragged) => ({ ...line, to: target(line.kind, event.clientX, event.clientY), shift, cells: dragged?.cells ?? dragCells(line) }));
        }}
        onPointerUp={(event) => {
          const held = press.current;
          press.current = null;
          setDrop(null);
          if (held?.dragging) {
            finishDrag(line, target(line.kind, event.clientX, event.clientY));
            return;
          }
          openMenu(line, event.currentTarget.getBoundingClientRect());
        }}
        onPointerCancel={() => { press.current = null; setDrop(null); }}
        onKeyDown={(event) => {
          if (!fieldEnter(event) && event.key !== " ") return;
          event.preventDefault();
          openMenu(line, event.currentTarget.getBoundingClientRect());
        }}>
        <DragHandleIcon size="small" aria-hidden="true" />
      </button>
    );
  }

  const lineMenu = menu && menu.kind !== "cells" ? menu : null;
  // The caret's cell keeps its handles with the pointer away, as Notion's (seen 2026-10-31).
  const rowLine = lineMenu?.kind === "row" ? lineMenu.index : hover?.row ?? cells.caret?.row;
  const columnLine = lineMenu?.kind === "column" ? lineMenu.index : hover?.column ?? cells.caret?.column;
  const rowBox = rowLine === undefined ? undefined : shape.rows[rowLine];
  const columnBox = columnLine === undefined ? undefined : shape.columns[columnLine];
  // The grips of the selection's first cell — the caret's, or a range's top
  // left — and of the cell under the pointer, both, as Notion's (seen
  // 2026-11-01: a range of two columns shows two column grips only where its
  // top left and the pointer are in each).
  const rangeSpan = cells.range ? tableRangeSpan(cells.range) : null;
  const firstCell = rangeSpan ? { row: rangeSpan.top, column: rangeSpan.left } : cells.caret;
  const gripLines = (chosen: number | undefined, pointed: number | undefined) =>
    [...new Set([chosen, pointed])].filter((index): index is number => index !== undefined);
  // A chosen row shows its grip chosen and the column grips only under the
  // pointer, a chosen column the other way round; a range that merely spans
  // a row or column keeps the plain grips (Notion, 2026-11-07).
  const chosenRow = cells.range?.line === "row" && rangeSpan ? rangeSpan.top : undefined;
  const chosenColumn = cells.range?.line === "column" && rangeSpan ? rangeSpan.left : undefined;
  const rowLines = lineMenu?.kind === "row" ? [lineMenu.index]
    : gripLines(chosenColumn === undefined ? firstCell?.row : undefined, hover?.row);
  const columnLines = lineMenu?.kind === "column" ? [lineMenu.index]
    : gripLines(chosenRow === undefined ? firstCell?.column : undefined, hover?.column);
  const selected: Box | null = !lineMenu ? null : lineMenu.kind === "row" && rowBox
    ? { left: shape.left, top: rowBox.top, width: shape.right - shape.left, height: rowBox.height }
    : lineMenu.kind === "column" && columnBox ? { left: columnBox.left, top: shape.top, width: columnBox.width, height: shape.bottom - shape.top } : null;
  // Where a dragged line would land, a 3px blue line along the border there
  // (Notion, seen 2026-11-09), and the line itself following the pointer
  // along its way, its own place left with its colors and no text.
  const dropLine: Box | null = !drop ? null : drop.kind === "row"
    ? { left: shape.left, top: (drop.to < shape.rows.length ? shape.rows[drop.to].top : shape.bottom) - 1, width: shape.right - shape.left, height: 3 }
    : { left: (drop.to < shape.columns.length ? shape.columns[drop.to].left + (rtl ? shape.columns[drop.to].width : 0) : rtl ? shape.left : shape.right) - 1,
      top: shape.top, width: 3, height: shape.bottom - shape.top };
  const dragBox = drop?.cells.length ? drop.cells.reduce<Box>((all, { box }) => ({ left: Math.min(all.left, box.left), top: Math.min(all.top, box.top),
    width: Math.max(all.left + all.width, box.left + box.width) - Math.min(all.left, box.left),
    height: Math.max(all.top + all.height, box.top + box.height) - Math.min(all.top, box.top) }), drop.cells[0].box) : null;
  const along = (box: Box): Box => (!drop ? box : drop.kind === "row" ? { ...box, top: box.top + drop.shift } : { ...box, left: box.left + drop.shift });


  // Cells as one box, over their borders: from the line before the first to the last's own (Notion, 2026-10-31).
  const span = (top: number, bottom: number, left: number, right: number): Box | null => {
    const first = shape.rows[top], last = shape.rows[bottom], start = shape.columns[left], end = shape.columns[right];
    if (!first || !last || !start || !end) return null;
    // From the line before the first, on its right where the table reads from the right.
    const near = Math.min(start.left, end.left), far = Math.max(start.left + start.width, end.left + end.width);
    return { left: rtl ? near : near - 1, top: first.top - 1, width: far - near + 1, height: last.top + last.height - first.top + 1 };
  };
  // While the cells' menu is open its cells keep their outline.
  const heldSpan = menu?.kind === "cells" ? menu.span : null;
  // The caret's cell, a merged one whole.
  const caretSpan = cells.caret && !cells.range && !menu
    ? withMerges({ top: cells.caret.row, bottom: cells.caret.row, left: cells.caret.column, right: cells.caret.column }) : null;
  const caretBox = caretSpan ? span(caretSpan.top, caretSpan.bottom, caretSpan.left, caretSpan.right) : null;
  const rangeBox = rangeSpan ? span(rangeSpan.top, rangeSpan.bottom, rangeSpan.left, rangeSpan.right)
    : heldSpan ? span(heldSpan.top, heldSpan.bottom, heldSpan.left, heldSpan.right) : null;
  const cellBox = rangeBox ?? caretBox;
  const closeMenu = (chosen: TableCellRange | null) => {
    setMenu(null);
    setHover(null);
    cells.choose(chosen);
  };
  // The first row's and first column's switches, as the table's own menu has them.
  const headerKey = lineMenu?.index === 0 ? (lineMenu.kind === "row" ? "hasColumnHeader" : "hasRowHeader") : null;
  const lineHeader = headerKey ? {
    on: block.content[headerKey] === true,
    label: i18next.t(lineMenu?.kind === "row" ? "blockHandle:menu.headerRow" : "blockHandle:menu.headerColumn"),
    onToggle: () => adapter.updateBlock(block, { [headerKey]: block.content[headerKey] !== true }),
  } : undefined;
  const lastEdit = lineMenu?.kind === "row" && rows[lineMenu.index]
    ? <BlockMenuFooter session={adapter.session ?? null} pageId={adapter.pageId} blockIds={[rows[lineMenu.index].id]} texts={[]} /> : undefined;

  return <>
    {cellBox ? <div className={styles.simpleTableSelection} contentEditable={false} aria-hidden="true"
      data-table-cell-selection={rangeBox ? "range" : "caret"} style={cellBox as CSSProperties} /> : null}
    {cellBox && cells.gripProps && !menu ? <span className={styles.simpleTableRangeGrip} contentEditable={false} aria-hidden="true" data-table-range-grip=""
      style={{ left: cellBox.left + (rtl ? 0 : cellBox.width) - 3.5, top: cellBox.top + cellBox.height / 2 - 6 }} {...cells.gripProps} /> : null}
    {/* A corner's "l" and "r" are its start and end: drawn on the right and left where the table reads from the right. */}
    {cellBox && !menu ? (["tl", "tr", "bl", "br"] as const).map((corner) => <span key={corner} className={styles.simpleTableRangeCorner} contentEditable={false}
      aria-hidden="true" data-table-range-corner={corner} style={{ left: cellBox.left + (corner.endsWith("r") !== rtl ? cellBox.width : 0) - 7.5,
        top: cellBox.top + (corner.startsWith("b") ? cellBox.height : 0) - 7.5 }} {...cells.cornerProps(corner)} />) : null}
    <SimpleTableAddBars adapter={adapter} block={block} rows={rows} width={width} shape={shape} rtl={rtl} cellsFor={cellsFor} actions={actions}
      hoverEdge={hover?.edge} menuOpen={menu !== null} />
    {!lineMenu || lineMenu.kind === "row" ? rowLines.map((index) => shape.rows[index] ? handle({ kind: "row", index },
      { left: (rtl ? shape.right : shape.left) - 7, top: shape.rows[index].top + shape.rows[index].height / 2 - 11, width: 14, height: 22 }, index === chosenRow) : null) : null}
    {!lineMenu || lineMenu.kind === "column" ? columnLines.map((index) => shape.columns[index] ? handle({ kind: "column", index },
      { left: shape.columns[index].left + shape.columns[index].width / 2 - 11, top: shape.top - 7, width: 22, height: 14 }, index === chosenColumn) : null) : null}
    {selected ? <div className={styles.simpleTableSelection} contentEditable={false} aria-hidden="true" style={selected as CSSProperties} /> : null}
    {dropLine ? <div className={styles.simpleTableDropLine} contentEditable={false} aria-hidden="true" style={dropLine as CSSProperties} /> : null}
    {drop?.cells.length ? <style>{drop.cells.map((cell) => `[data-table-cell="${CSS.escape(cell.id)}"] [data-rt-editable]`).join(",")}{"{visibility:hidden}"}</style> : null}
    {drop?.cells.map((cell) => <div key={cell.id} className={styles.simpleTableDragCell} contentEditable={false} aria-hidden="true"
      style={{ ...along(cell.box), backgroundImage: `linear-gradient(${cell.background}, ${cell.background})`, color: cell.color } as CSSProperties}>{cell.text}</div>)}
    {dragBox ? <div className={styles.simpleTableSelection} contentEditable={false} aria-hidden="true" style={along(dragBox) as CSSProperties} /> : null}
    {lineMenu ? <SimpleTableMenu kind={lineMenu.kind} anchor={lineMenu.anchor} canDelete={lineMenu.kind === "row" ? rows.length > 1 : width > 1}
      color={lineMenu.kind === "row" ? String(rows[lineMenu.index]?.content.color ?? "") : simpleTableListedColor(block.content.columnColors, lineMenu.index)}
      canAdd={lineMenu.kind === "row" || width < 100} footer={lastEdit} header={lineHeader}
      onAction={(action) => closeMenu(run(lineMenu, action))}
      onClose={(byEscape) => closeMenu(!byEscape ? null
        : lineMenu.kind === "row" ? lineRange("row", lineMenu.index, lastColumnIndex) : lineRange("column", lineMenu.index, lastRowIndex))} /> : null}
    {menu?.kind === "cells" ? <SimpleTableMenu kind="cells" anchor={menu.anchor} canDelete={false} canAdd={false}
      color={simpleTableListedColor(rows[menu.span.top]?.content.cellColors, menu.span.left)}
      merge={{ merge: !merges.some((merged) => merged.top === menu.span.top && merged.bottom === menu.span.bottom && merged.left === menu.span.left
        && merged.right === menu.span.right) && (menu.span.top !== menu.span.bottom || menu.span.left !== menu.span.right),
        unmerge: mergesWithin(menu.span, merges).length > 0 }}
      // Merged or unmerged, its cells stay chosen (Notion, seen 2026-11-10).
      onAction={(action) => { runCells(menu.span, action); closeMenu(menu.fromCaret && action !== "merge" && action !== "unmerge" ? null
        : { from: { row: menu.span.top, column: menu.span.left }, to: { row: menu.span.bottom, column: menu.span.right } }); }}
      onClose={(byEscape) => closeMenu(menu.fromCaret || !byEscape ? null
        : { from: { row: menu.span.top, column: menu.span.left }, to: { row: menu.span.bottom, column: menu.span.right } })} /> : null}
  </>;
}
