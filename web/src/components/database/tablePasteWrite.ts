import type { GraphProperty, GraphRow, GraphRowValue, GraphView } from "@/contentGraph";
import { copyTextWithMime, LEAFEN_TABLE_ROWS_MIME } from "@/lib/clipboard";
import type { Page, TextSpan } from "@/lib/types";
import type { PropertyValueReadContext } from "./PropValue";
import { tableCellFromText, tableCellText, tableColumnTakesPaste } from "./tableCellClipboard";
import { serializeTableClipboard, TABLE_CLIPBOARD_BOUNDS, tableClipboardCellKept, tableClipboardCellSpans, tableClipboardEntry } from "./tableClipboardModel";
import type { TablePaste } from "./tableCellPaste";
import { tableGroupSeedValue } from "./tableGrouping";
import { lineGroup, lineRowId } from "./viewGroupLines";
import { viewKeepsOwnRowOrder, viewRowOrderPlacing } from "./viewRowOrder";

/**
 * Cells copied from a table's or a timeline's table's rows, row by row: as
 * shown for other apps, with Leafen's own entry of their formatting and, for a
 * paste into the same column, their values (tableClipboardModel).
 */
export function tableCellsClipboard(databaseId: string, rows: readonly GraphRow[], shown: readonly GraphProperty[], context?: PropertyValueReadContext) {
  const pages = { pagesById: context?.pagesById ?? {}, propsByDatabaseId: context?.propsByDatabaseId, bounds: context?.bounds };
  return serializeTableClipboard({ databaseId, rowIds: rows.map((row) => row.id),
    values: rows.map((row) => shown.map((property) => tableCellText(property, row, pages))),
    spans: rows.map((row) => shown.map((property) => (row.values[property.id]?.spans as TextSpan[] | undefined) ?? null)),
    propertyIds: shown.map((property) => property.id), kept: rows.map((row) => shown.map((property) => row.values[property.id] ?? null)) });
}

export function copyTableCells(databaseId: string, rows: readonly GraphRow[], shown: readonly GraphProperty[], context?: PropertyValueReadContext) {
  const payload = tableCellsClipboard(databaseId, rows, shown, context);
  void copyTextWithMime(payload.text, LEAFEN_TABLE_ROWS_MIME, tableClipboardEntry(payload), {}, TABLE_CLIPBOARD_BOUNDS);
}

/**
 * The values each line of a paste gives the columns from the first one on:
 * text read as its column's type, a cell copied from the same column keeping
 * its value and a title's or a text's its formatting (tableClipboardModel);
 * a column that only shows what Leafen works out takes nothing.
 */
export function pastedLines(paste: TablePaste, columns: readonly GraphProperty[], pagesById: Readonly<Record<string, Page>>) {
  return paste.lines.map((cells, line) => {
    const values: Record<string, GraphRowValue> = {};
    // A hole, where the copy had no cell, writes nothing (forEach passes it over).
    cells.forEach((value, delta) => {
      const property = columns[delta];
      if (!property || !tableColumnTakesPaste(property)) return;
      const [row, column] = paste.from(line, delta);
      const kept = tableClipboardCellKept(paste.entry, row, column, property.id);
      const spans = property.type === "title" || property.type === "rich_text" ? tableClipboardCellSpans(paste.entry, row, column, value) : undefined;
      values[property.id] = kept && kept.type === property.type ? kept
        : spans ? { type: property.type, value, spans } : tableCellFromText(property, value, { pagesById });
    });
    return values;
  });
}

/**
 * The rows a paste's lines past a part's last row make: after that row
 * (`after`, its line) under its parent, each starting with its line's values
 * over its group's value and the values the view's filters give — as Notion's,
 * which made them in its group with the group's value, one ⌘Z taking them
 * back with the values pasted above them (2026-11-26, Chrome, two tabs at two
 * times: a table, a grouped table, a timeline's table and a grouped one).
 * Notion put them in the reverse of the lines' order; they come in the lines'
 * order here, as copied. A view keeping its own order puts them after that
 * row there too (`order`, given the rows made; null where it keeps none).
 */
export function pastedRowsMade(table: {
  view: GraphView;
  groupProperty?: GraphProperty;
  /** The values the view's filters give a new row, but those named. */
  filterSeeds(omit: readonly string[]): Readonly<Record<string, unknown>>;
  row(id: string): GraphRow | undefined;
  shownIds(): string[];
}, lines: readonly GraphRow["values"][], after: string) {
  const afterId = lineRowId(after), group = lineGroup(after);
  const seed = group !== undefined && table.groupProperty ? tableGroupSeedValue(table.groupProperty, group, table.view.config) : undefined;
  const grouped = seed !== undefined && table.groupProperty ? { [table.groupProperty.id]: seed } : {};
  const parentId = table.row(afterId)?.subitemParentId ?? null;
  const ownOrder = viewKeepsOwnRowOrder(table.view.config) && !parentId;
  return {
    made: { parentId, afterRowId: ownOrder ? null : afterId, seeds: { ...table.filterSeeds(Object.keys(grouped)), ...grouped }, rows: lines },
    order: (ids: readonly string[]) => ownOrder && ids.length ? viewRowOrderPlacing(table.view.config, table.shownIds(), ids, { id: afterId, side: "after" }) : null,
  };
}
