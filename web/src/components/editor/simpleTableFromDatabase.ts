import { compareGraphOrder } from "@/contentGraph";
import type { ContentGraph, GraphBlock, GraphProperty, GraphRow, GraphTextSpan, GraphView } from "@/contentGraph";
import { nativeBoardQueryBounds } from "@/components/database/boardViewModel";
import { galleryPresentationPage, galleryPresentationProperty } from "@/components/database/galleryViewModel";
import { visibleViewProperties } from "@/components/database/queryProjection";
import { cellSpansOrText } from "@/components/database/richTextCellModel";
import { displayPropertyValue } from "@/components/database/rollup";
import { tableColumnWidth } from "@/components/database/tableViewModel";
import { projectRows } from "@/components/database/viewRowProjection";
import type { DbView, TextSpan, ViewConfig } from "@/lib/types";
import { SIMPLE_TABLE_MAX_WIDTH, SIMPLE_TABLE_TURN_MAX_OPERATIONS, type SimpleTableTurn } from "./databaseSimpleTable";
import { simpleTableColumnWidth } from "./simpleTableColumns";
import { insertEditorBlock, insertEditorChildBlock, removeEditorBlocks } from "./editorGraphModel";

// Loaded when a database is turned into a simple table: it reads the view's
// rows and values as the database's own modules do, which the editor
// downloads only when a page shows a database.

function presentationView(view: GraphView): DbView {
  return { id: view.id, databaseId: view.dataSourceId, name: view.name, type: view.type as DbView["type"], position: 0,
    config: view.config as ViewConfig };
}

/**
 * The columns and rows an inline database turns into, as Notion's "간단한 표로
 * 전환" makes them (seen 2026-10-11): a header row of the shown view's shown
 * properties, in its order, then its rows in its order; a title or text keeps
 * its formatting, any other value its shown text (a tag its name). Hidden
 * properties are left out. A table view's columns keep their widths, one
 * nobody sized its default (seen 2026-11-11: a title left at 280 and a text
 * column narrowed to 126 stayed 280 and 126). Null when the table would not
 * fit one change.
 */
export function simpleTableFromDatabase(graph: ContentGraph, block: GraphBlock, view: GraphView,
  where: { pageId: string; workspaceId: string; currentUserId?: string }) {
  const properties = graph.properties.filter((property) => property.dataSourceId === view.dataSourceId).sort(compareGraphOrder);
  const shown = visibleViewProperties(properties.map(galleryPresentationProperty), presentationView(view), nativeBoardQueryBounds)
    .flatMap((shownProperty) => properties.filter((property) => property.id === shownProperty.id));
  const sourceRows = graph.rows.filter((row) => row.dataSourceId === view.dataSourceId && !row.archivedAt);
  const rows = projectRows({ ...where, properties, rows: sourceRows, view });
  if (!shown.length || shown.length > SIMPLE_TABLE_MAX_WIDTH || rows.length + 4 > SIMPLE_TABLE_TURN_MAX_OPERATIONS) return null;
  const title = properties.find((property) => property.type === "title");
  const pages = Object.fromEntries(graph.rows.map((row, index) =>
    [row.id, galleryPresentationPage({ row, titleProperty: graph.properties.find((property) =>
      property.dataSourceId === row.dataSourceId && property.type === "title"), workspaceId: where.workspaceId, position: index })]));
  const cell = (row: GraphRow, property: GraphProperty): GraphTextSpan[] => {
    const value = row.values[property.id];
    if (property.type === "title" || property.type === "rich_text") return cellSpansOrText(value?.spans as TextSpan[] | undefined, value?.value);
    try {
      const page = pages[row.id] ?? galleryPresentationPage({ row, titleProperty: title, workspaceId: where.workspaceId, position: 0 });
      const text = displayPropertyValue(page, galleryPresentationProperty(property, 0), pages, nativeBoardQueryBounds.formula.rollup);
      return text ? [{ text }] : [];
    } catch {
      return [];
    }
  };
  const columnWidths = view.type === "table"
    ? shown.map((property) => simpleTableColumnWidth(tableColumnWidth(view.config, property.id, property.type))) : undefined;
  const content = { tableWidth: shown.length, hasColumnHeader: true, hasRowHeader: false };
  const table = insertEditorBlock(graph, block.id, "table", columnWidths ? { ...content, columnWidths } : content);
  let next = table.graph;
  const operations = [...table.operations];
  for (const cells of [shown.map((property) => property.name ? [{ text: property.name }] : []),
    ...rows.map((row) => shown.map((property) => cell(row, property)))]) {
    const added = insertEditorChildBlock(next, table.block.id, "table_row", { cells });
    next = added.graph;
    operations.push(...added.operations);
  }
  const removed = removeEditorBlocks(next, [block.id]);
  return { graph: removed.graph, operations: [...operations, ...removed.operations] } satisfies SimpleTableTurn;
}
