import type { EditorView } from "@tiptap/pm/view";
import type { GraphBlock } from "@/contentGraph";

/**
 * A simple table's merged cells, as Notion's "셀 병합" (seen 2026-11-10 on a
 * synthetic 3×3 table): a range of cells merged shows its first cell's
 * content over them all, the others emptied; a range holding merged cells
 * unmerges them, the cells keeping what they hold. A row keeps the merged
 * cells that start in it (its `cellSpans`): from its column, so many rows
 * down and columns across. A row or column put between a merged cell's own
 * joins it, one taken away leaves it; the public API and page Markdown give
 * the cells one by one, as Notion's do.
 */
export type SimpleTableCellSpan = { column: number; rows: number; columns: number };
/** Where a merged cell stands, by the table's rows and columns, first to last. */
export type SimpleTableMergeSpan = { top: number; bottom: number; left: number; right: number };

const count = (value: unknown) => (typeof value === "number" && Number.isInteger(value) && value > 0 ? value : 0);

/** A row's merged cells as it keeps them. */
export function rowCellSpans(row: Pick<GraphBlock, "content"> | undefined): SimpleTableCellSpan[] {
  const list = Array.isArray(row?.content.cellSpans) ? row.content.cellSpans : [];
  return list.flatMap((entry: unknown) => {
    const { column, rows, columns } = (entry ?? {}) as Record<string, unknown>;
    return typeof column === "number" && Number.isInteger(column) && column >= 0 && count(rows) && count(columns)
      ? [{ column, rows: count(rows), columns: count(columns) }] : [];
  });
}

/** Whether any of the rows keeps a merged cell: such a table lays its cells out on its own grid. */
export function tableHasMergedCells(rows: readonly GraphBlock[]) {
  return rows.some((row) => rowCellSpans(row).length > 0);
}

const overlaps = (a: SimpleTableMergeSpan, b: SimpleTableMergeSpan) =>
  a.top <= b.bottom && b.top <= a.bottom && a.left <= b.right && b.left <= a.right;

/**
 * Where the table's merged cells stand: each clipped to the table, none
 * that comes to one cell or lies over one before it in reading order.
 */
export function simpleTableMergeSpans(rows: readonly GraphBlock[], width: number): SimpleTableMergeSpan[] {
  const spans: SimpleTableMergeSpan[] = [];
  rows.forEach((row, top) => {
    for (const { column, rows: tall, columns } of rowCellSpans(row)) {
      const span = { top, bottom: Math.min(rows.length, top + tall) - 1, left: column, right: Math.min(width, column + columns) - 1 };
      if (span.left >= width || (span.top === span.bottom && span.left === span.right)) continue;
      if (spans.some((other) => overlaps(other, span))) continue;
      spans.push(span);
    }
  });
  return spans;
}

/** The merged cell a cell is part of, if any. */
export function mergeSpanAt(spans: readonly SimpleTableMergeSpan[], row: number, column: number) {
  return spans.find((span) => row >= span.top && row <= span.bottom && column >= span.left && column <= span.right);
}

/** Cells grown until each merged cell they touch is wholly in them, as Notion's ranges do. */
export function spanWithMerges(span: SimpleTableMergeSpan, spans: readonly SimpleTableMergeSpan[]): SimpleTableMergeSpan {
  let next = span;
  for (let grown = true; grown;) {
    grown = false;
    for (const merge of spans) {
      if (!overlaps(merge, next)) continue;
      const wider = { top: Math.min(next.top, merge.top), bottom: Math.max(next.bottom, merge.bottom),
        left: Math.min(next.left, merge.left), right: Math.max(next.right, merge.right) };
      if (wider.top !== next.top || wider.bottom !== next.bottom || wider.left !== next.left || wider.right !== next.right) {
        next = wider;
        grown = true;
      }
    }
  }
  return next;
}

/** The merged cells wholly in a span. */
export function mergesWithin(span: SimpleTableMergeSpan, spans: readonly SimpleTableMergeSpan[]) {
  return spans.filter((merge) => merge.top >= span.top && merge.bottom <= span.bottom && merge.left >= span.left && merge.right <= span.right);
}

/**
 * Where a merged cell's lines are after the lines change: `next` names, for
 * each line now, the line it was (null for a new one, and for a second copy
 * of one). It keeps its lines still there; one put between two of them
 * joins it (Notion, seen 2026-11-10: a row inserted inside a merged cell
 * made it a row taller), one beside it doesn't; a moved line leaves it,
 * unless it lands beside or among its lines again.
 */
export function mergedLinesAfter(from: number, to: number, next: readonly (number | null)[], moved?: number): { from: number; to: number } | null {
  const seen = new Set<number>();
  const kept: number[] = [];
  next.forEach((old, at) => {
    if (old === null || seen.has(old)) return;
    seen.add(old);
    if (old >= from && old <= to && old !== moved) kept.push(at);
  });
  if (!kept.length) return null;
  let first = kept[0], last = kept[kept.length - 1];
  if (moved !== undefined && moved >= from && moved <= to) {
    const at = next.indexOf(moved);
    if (at >= first - 1 && at <= last + 1) { first = Math.min(first, at); last = Math.max(last, at); }
  }
  return { from: first, to: last };
}

/** A row's content with these merged cells, in their order; none kept when there are none. */
export function withCellSpans(content: Record<string, unknown>, spans: readonly SimpleTableCellSpan[]): Record<string, unknown> {
  const { cellSpans: _old, ...rest } = content;
  const kept = spans.filter((span) => span.rows > 1 || span.columns > 1).sort((a, b) => a.column - b.column);
  return kept.length ? { ...rest, cellSpans: kept } : rest;
}

/**
 * The rows' content after the rows change: `order` names, for each row now,
 * the row it was (null for a new one); each merged cell goes on the first
 * of its rows now. Only the rows whose content changes come back.
 */
export function rowsAfterRowChange(rows: readonly GraphBlock[], width: number, after: readonly GraphBlock[], order: readonly (number | null)[],
  moved?: number): GraphBlock[] {
  const spans = simpleTableMergeSpans(rows, width);
  const placed = new Map<number, SimpleTableCellSpan[]>();
  for (const span of spans) {
    const lines = mergedLinesAfter(span.top, span.bottom, order, moved);
    if (!lines) continue;
    placed.set(lines.from, [...placed.get(lines.from) ?? [],
      { column: span.left, rows: lines.to - lines.from + 1, columns: span.right - span.left + 1 }]);
  }
  return after.flatMap((row, at) => {
    const now = withCellSpans(row.content, placed.get(at) ?? []);
    return JSON.stringify(now.cellSpans ?? []) === JSON.stringify(row.content.cellSpans ?? []) ? [] : [{ ...row, content: now }];
  });
}

/** A row's merged cells after the columns are put in a new order, as `withColumns` does. */
export function cellSpansAfterColumns(row: GraphBlock, order: readonly (number | null)[], moved?: number): SimpleTableCellSpan[] {
  return rowCellSpans(row).flatMap((span) => {
    const lines = mergedLinesAfter(span.column, span.column + span.columns - 1, order, moved);
    return lines ? [{ column: lines.from, rows: span.rows, columns: lines.to - lines.from + 1 }] : [];
  });
}

type Box = { left: number; top: number; width: number; height: number };

/**
 * A table's rows and columns as its grid lays them out, each a box from
 * `origin`: a table with merged cells puts its cells straight on its grid,
 * its rows having no boxes of their own. Its columns run from its start
 * edge, the right one in a right-to-left page.
 */
export function simpleTableGridBoxes(table: HTMLElement, origin: { left: number; top: number }): { rows: Box[]; columns: Box[] } {
  const style = getComputedStyle(table), rect = table.getBoundingClientRect();
  const sizes = (tracks: string) => tracks.split(/\s+/u).map((size) => Number.parseFloat(size)).filter((size) => Number.isFinite(size));
  const rows: Box[] = [], columns: Box[] = [];
  let top = rect.top - origin.top + (Number.parseFloat(style.borderTopWidth) || 0);
  const left = rect.left - origin.left + (Number.parseFloat(style.borderLeftWidth) || 0);
  const inner = rect.width - (Number.parseFloat(style.borderLeftWidth) || 0) - (Number.parseFloat(style.borderRightWidth) || 0);
  for (const height of sizes(style.gridTemplateRows)) {
    rows.push({ left, top, width: inner, height });
    top += height;
  }
  const rtl = style.direction === "rtl";
  let at = rtl ? left + inner : left;
  for (const width of sizes(style.gridTemplateColumns)) {
    columns.push({ left: rtl ? at - width : at, top: rows[0]?.top ?? top, width, height: top - (rows[0]?.top ?? top) });
    at += rtl ? -width : width;
  }
  return { rows, columns };
}

/** The table cell holding a place in the editor, as drawn. */
export function tableCellElementAt(view: EditorView, pos: number): HTMLElement | null {
  const node = view.domAtPos(pos).node;
  return (node instanceof Element ? node : node.parentElement)?.closest<HTMLElement>("[data-native-table-cell]") ?? null;
}

/**
 * Where the caret goes in the merged cell standing over the cell at a place,
 * at its text's start or end; null when no merged cell stands over it. The
 * cells under a merged cell take no caret: the caret meets the merged cell.
 */
export function mergedCellCaret(view: EditorView, pos: number, atEnd: boolean): number | null {
  const over = tableCellElementAt(view, pos)?.getAttribute("data-table-covered");
  // The cell's text is the editor's own box inside its frame.
  const text = over ? view.dom.querySelector(`[data-table-cell="${CSS.escape(over)}"] [data-rt-editable] [data-node-view-content-react]`) : null;
  return text ? view.posAtDOM(text, atEnd ? text.childNodes.length : 0) : null;
}
