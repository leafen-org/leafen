import type { ContentGraph, GraphBlock, GraphOperation, GraphTextSpan } from "@/contentGraph";
import { editorBlockChildren, insertEditorChildBlock, moveEditorBlock, removeEditorBlocks, replaceEditorBlocks } from "./editorGraphModel";
import { keptSimpleTableColors, simpleTableColumnWidths, simpleTableListedColor, storedSimpleTableWidths } from "./simpleTableColumns";
import { cellSpansAfterColumns, mergesWithin, rowCellSpans, rowsAfterRowChange, simpleTableMergeSpans, withCellSpans, type SimpleTableCellSpan } from "./simpleTableMerges";
import { lineRange, tableRangeSpan, type TableCell, type TableCellRange, type TableLineKey } from "./useSimpleTableCellSelection";
import type { NativeEditorGraphAdapter } from "./editorRuntimeTypes";

/** A row or a column of a simple table, by its place. */
export type SimpleTableLine = { kind: "row" | "column"; index: number };
type Span = ReturnType<typeof tableRangeSpan>;

/**
 * What a simple table's grips, menus, keys and "+" bars change, each as one
 * step to undo: columns put in a new order (a new column empty, an old one
 * with its text, width and colors), rows and columns added, a line's menu
 * action, a chosen line's keys, cells pasted, and a cell menu's color,
 * clearing, merging or unmerging; merged cells follow the lines as they
 * change (simpleTableMerges).
 */
export function simpleTableActions({ adapter, block, rows, width, cellsFor }: {
  adapter: NativeEditorGraphAdapter;
  block: GraphBlock;
  rows: readonly GraphBlock[];
  width: number;
  cellsFor: (row: GraphBlock) => GraphTextSpan[][];
}) {
  const emptyCells = () => Array.from({ length: width }, () => [] as GraphTextSpan[]);
  const widths = simpleTableColumnWidths(block.content, width);
  // The table again with its columns in a new order: each new column is an
  // old one (its text, width and colors) or, where null, a new empty one;
  // `moved` is a column dragged to its place.
  const withColumns = (order: readonly (number | null)[], moved?: number): GraphBlock[] => {
    const pick = <T,>(list: readonly T[], empty: T) => order.map((at) => (at === null ? empty : list[at] ?? empty));
    const colorsOf = (value: unknown) => Array.from({ length: width }, (_, at) => simpleTableListedColor(value, at));
    const { columnColors: _columns, ...table } = block.content;
    const columnColors = keptSimpleTableColors(pick(colorsOf(block.content.columnColors), ""));
    return [
      { ...block, content: { ...table, tableWidth: order.length, columnWidths: storedSimpleTableWidths(pick(widths, 0)),
        ...(columnColors ? { columnColors } : {}) } },
      ...rows.map((row) => {
        const { cellColors: _cells, ...content } = row.content;
        const cellColors = keptSimpleTableColors(pick(colorsOf(row.content.cellColors), ""));
        const kept = { ...content, cells: pick(cellsFor(row), [] as GraphTextSpan[]), ...(cellColors ? { cellColors } : {}) };
        return { ...row, content: rowCellSpans(row).length ? withCellSpans(kept, cellSpansAfterColumns(row, order, moved)) : kept };
      }),
    ];
  };
  const reorderColumns = (order: readonly (number | null)[], moved?: number) => adapter.replaceBlocks(withColumns(order, moved));
  // A change to the rows, the merged cells following it in the same step to
  // undo; `moved` is a row dragged to its place.
  type RowChange = { graph: ContentGraph; operations: GraphOperation[] } | null;
  const changeRows = (change: (graph: ContentGraph) => RowChange, moved?: number) => adapter.changeLayout((graph) => {
    const changed = change(graph);
    if (!changed || !simpleTableMergeSpans(rows, width).length) return changed;
    const after = editorBlockChildren(changed.graph, block.id).filter((row) => row.type === "table_row");
    const places = new Map(rows.map((row, at) => [row.id, at]));
    const followed = rowsAfterRowChange(rows, width, after, after.map((row) => places.get(row.id) ?? null), moved);
    if (!followed.length) return changed;
    const merged = replaceEditorBlocks(changed.graph, followed);
    return { graph: merged.graph, operations: [...changed.operations, ...merged.operations] };
  });
  const insertRow = (content: Record<string, unknown>, after: string | null) =>
    changeRows((graph) => insertEditorChildBlock(graph, block.id, "table_row", content, after));
  const removeRows = (ids: readonly string[]) => changeRows((graph) => removeEditorBlocks(graph, ids));
  // A row dragged from one place to another (`to` counted without it).
  const moveRow = (from: number, to: number) => {
    const others = rows.filter((_, at) => at !== from);
    changeRows((graph) => moveEditorBlock(graph, rows[from].id, block.id, to === 0 ? null : others[to - 1].id), from);
  };
  const columns = Array.from({ length: width }, (_, at) => at as number | null);
  const addRow = () => adapter.insertChild(block.id, "table_row", { cells: emptyCells() });
  // The corner adds a row and a column as one change, which one undo takes back (Notion, seen 2026-10-31).
  const addRowAndColumn = () => {
    const wider = withColumns([...columns, null]);
    adapter.changeLayout((graph) => {
      const widened = replaceEditorBlocks(graph, wider);
      const added = insertEditorChildBlock(widened.graph, block.id, "table_row", { cells: Array.from({ length: width + 1 }, () => []) });
      return { graph: added.graph, operations: [...widened.operations, ...added.operations] };
    });
  };

  const lastRowIndex = rows.length - 1;
  const lastColumnIndex = width - 1;
  // A line's action, then the line it leaves chosen: the new one after an
  // insert, the next after a delete, else the same (Notion, seen 2026-11-09).
  function run(line: SimpleTableLine, action: string): TableCellRange | null {
    const { index } = line;
    const color = action.startsWith("color:") ? action.slice("color:".length) : null;
    const chosen = (at: number) => line.kind === "row" ? lineRange("row", at, lastColumnIndex) : lineRange("column", at, lastRowIndex);
    if (line.kind === "row") {
      const row = rows[index];
      if (!row) return null;
      if (color !== null) {
        const { color: _old, ...rest } = row.content;
        adapter.replaceBlocks([{ ...row, content: color ? { ...rest, color } : rest }]);
        return chosen(index);
      }
      if (action === "before") insertRow({ cells: emptyCells() }, rows[index - 1]?.id ?? null);
      else if (action === "after") insertRow({ cells: emptyCells() }, row.id);
      else if (action === "duplicate") insertRow({ ...row.content, cells: cellsFor(row) }, row.id);
      else if (action === "clear") adapter.replaceBlocks([{ ...row, content: { ...row.content, cells: emptyCells() } }]);
      else if (action === "delete" && rows.length > 1) {
        removeRows([row.id]);
        return lineRange("row", Math.min(index, rows.length - 2), lastColumnIndex);
      }
      return chosen(action === "after" ? index + 1 : index);
    }
    if (color !== null) {
      const colors = Array.from({ length: width }, (_, at) => (at === index ? color : simpleTableListedColor(block.content.columnColors, at)));
      const { columnColors: _old, ...rest } = block.content;
      const kept = keptSimpleTableColors(colors);
      adapter.replaceBlocks([{ ...block, content: kept ? { ...rest, columnColors: kept } : rest }]);
    } else if (action === "before" || action === "after" || action === "duplicate") {
      if (width >= 100) return chosen(index);
      const at = action === "before" ? index : index + 1;
      reorderColumns([...columns.slice(0, at), action === "duplicate" ? index : null, ...columns.slice(at)]);
    } else if (action === "clear") {
      adapter.replaceBlocks(rows.map((row) => ({ ...row, content: { ...row.content,
        cells: cellsFor(row).map((cell, at) => (at === index ? [] : cell)) } })));
    } else if (action === "delete" && width > 1) {
      reorderColumns(columns.filter((at) => at !== index));
      return lineRange("column", Math.min(index, width - 2), lastRowIndex);
    }
    return chosen(action === "after" ? index + 1 : index);
  }

  // A chosen line's Backspace or Delete deletes it, choosing the line now in
  // its place; ⌘D puts a copy after it, keeping it chosen (Notion, seen 2026-11-09).
  function lineKey(range: TableCellRange, key: TableLineKey): TableCellRange | null {
    const { top, bottom, left, right } = tableRangeSpan(range);
    if (range.line === "row") {
      const chosenRows = rows.slice(top, bottom + 1);
      if (key === "delete") {
        if (chosenRows.length >= rows.length) return range;
        removeRows(chosenRows.map((row) => row.id));
        return lineRange("row", Math.min(top, rows.length - chosenRows.length - 1), lastColumnIndex);
      }
      changeRows((graph) => {
        let next = { graph, operations: [] as GraphOperation[] };
        let after = chosenRows.at(-1)!.id;
        for (const row of chosenRows) {
          const added = insertEditorChildBlock(next.graph, block.id, "table_row", { ...row.content, cells: cellsFor(row) }, after);
          next = { graph: added.graph, operations: [...next.operations, ...added.operations] };
          after = added.block?.id ?? after;
        }
        return next;
      });
      return range;
    }
    const count = right - left + 1;
    if (key === "delete") {
      if (count >= width) return range;
      reorderColumns(columns.filter((at) => at === null || at < left || at > right));
      return lineRange("column", Math.min(left, width - count - 1), lastRowIndex);
    }
    if (width + count > 100) return range;
    reorderColumns([...columns.slice(0, right + 1), ...columns.slice(left, right + 1), ...columns.slice(right + 1)]);
    return range;
  }

  // Cells a table copied, pasted from a cell on: past the table's edge it
  // grows (up to 100 columns), all as one change; the cells they took. The
  // merged cells among them are merged where they land, over any there
  // (Notion, seen 2026-11-10).
  function pasteCells(at: TableCell, grid: GraphTextSpan[][][], merges: readonly Span[] = []): TableCellRange | null {
    const tall = grid.length, wide = Math.max(0, ...grid.map((line) => line.length));
    if (!tall || !wide || at.column >= 100) return null;
    const columnsTo = Math.min(100, Math.max(width, at.column + wide));
    const rowsTo = Math.max(rows.length, at.row + tall);
    const pasted = (row: number, column: number, old: GraphTextSpan[]) => {
      const line = grid[row - at.row];
      return line && column >= at.column && column - at.column < line.length ? line[column - at.column] : old;
    };
    const bottom = at.row + tall - 1, right = at.column + wide - 1;
    const spansAt = (row: number, kept: readonly SimpleTableCellSpan[]) => [
      ...kept.filter((span) => row + span.rows - 1 < at.row || row > bottom || span.column + span.columns - 1 < at.column || span.column > right),
      ...merges.filter((span) => at.row + span.top === row && at.column + span.left < columnsTo)
        .map((span) => ({ column: at.column + span.left, rows: span.bottom - span.top + 1, columns: Math.min(columnsTo - at.column, span.right + 1) - span.left })),
    ];
    const merged = (content: Record<string, unknown>, row: number) => (merges.length ? withCellSpans(content, spansAt(row, rowCellSpans({ content }))) : content);
    adapter.changeLayout((graph) => {
      const [table, ...widened] = withColumns(columnsTo > width ? [...columns, ...Array.from({ length: columnsTo - width }, () => null)] : columns);
      let next = replaceEditorBlocks(graph, [table, ...widened.map((row, index) => ({ ...row, content: merged({ ...row.content,
        cells: (row.content.cells as GraphTextSpan[][]).map((cell, column) => pasted(index, column, cell)) }, index) }))]);
      for (let index = rows.length; index < rowsTo; index += 1) {
        const added = insertEditorChildBlock(next.graph, block.id, "table_row",
          merged({ cells: Array.from({ length: columnsTo }, (_, column) => pasted(index, column, [])) }, index));
        next = { graph: added.graph, operations: [...next.operations, ...added.operations] };
      }
      return next;
    });
    return { from: at, to: { row: bottom, column: Math.min(columnsTo, at.column + wide) - 1 } };
  }

  // "셀 병합" merges the cells, its first cell's content over them all and
  // the others emptied, the merged cells within taken in; "셀 병합 해제"
  // unmerges those within, each cell keeping what it holds (Notion, seen
  // 2026-11-10). One step to undo.
  function mergeCells(span: Span, merge: boolean) {
    const changed = new Map<number, GraphBlock>();
    for (const within of mergesWithin(span, simpleTableMergeSpans(rows, width))) {
      const row = changed.get(within.top) ?? rows[within.top];
      changed.set(within.top, { ...row, content: withCellSpans(row.content, rowCellSpans(row).filter((kept) => kept.column !== within.left)) });
    }
    if (merge) {
      rows.slice(span.top, span.bottom + 1).forEach((source, at) => {
        const row = changed.get(span.top + at) ?? source;
        const cells = cellsFor(source).map((cell, column) => (column < span.left || column > span.right || (at === 0 && column === span.left) ? cell : []));
        const content = { ...row.content, cells };
        changed.set(span.top + at, { ...row, content: at > 0 ? content : withCellSpans(content, [...rowCellSpans(row),
          { column: span.left, rows: span.bottom - span.top + 1, columns: span.right - span.left + 1 }]) });
      });
    }
    if (changed.size) adapter.replaceBlocks([...changed.values()]);
  }

  // The cells' menu: "색" colors each of its cells, "콘텐츠 삭제" empties them.
  function runCells(span: Span, action: string) {
    if (action === "merge" || action === "unmerge") { mergeCells(span, action === "merge"); return; }
    const color = action.startsWith("color:") ? action.slice("color:".length) : null;
    adapter.replaceBlocks(rows.slice(span.top, span.bottom + 1).map((row) => {
      if (color === null) return { ...row, content: { ...row.content, cells: cellsFor(row).map((cell, at) => (at >= span.left && at <= span.right ? [] : cell)) } };
      const { cellColors: _old, ...content } = row.content;
      const kept = keptSimpleTableColors(Array.from({ length: width }, (_, at) => (at >= span.left && at <= span.right ? color
        : simpleTableListedColor(row.content.cellColors, at))));
      return { ...row, content: kept ? { ...content, cellColors: kept } : content };
    }));
  }

  return { columns, withColumns, reorderColumns, moveRow, addRow, addRowAndColumn, run, lineKey, pasteCells, runCells, lastRowIndex, lastColumnIndex };
}
