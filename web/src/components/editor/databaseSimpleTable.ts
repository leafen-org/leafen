import type {
  ContentGraph, GraphBlock, GraphDatabaseContainer, GraphDataSource, GraphOperation, GraphProperty, GraphRow, GraphRowValue,
  GraphTextSpan, GraphView,
} from "@/contentGraph";
import { cellValueFromSpans } from "@/components/database/richTextCellModel";
import { graphWireBounds } from "@/contentGraphWire.generated";
import { graphPositionBetween } from "@/lib/graphPosition";
import { newId } from "@/lib/ids";
import type { TextSpan } from "@/lib/types";
import { editorBlockChildren, insertEditorBlock, removeEditorBlocks } from "./editorGraphModel";

/** One change carries at most this many operations; a turn that needs more is not offered. */
export const SIMPLE_TABLE_TURN_MAX_OPERATIONS = 1000;
/** As wide as a table row can hold (Notion keeps 2,000 columns and more). */
export const SIMPLE_TABLE_MAX_WIDTH = graphWireBounds.tableWidth;

export type SimpleTableTurn = { graph: ContentGraph; operations: GraphOperation[] };

function tableRowCells(row: GraphBlock, width: number) {
  const raw = Array.isArray(row.content.cells) ? row.content.cells : [];
  return Array.from({ length: width }, (_, index) => {
    const cell = raw[index];
    return Array.isArray(cell)
      ? cell.filter((span): span is GraphTextSpan => !!span && typeof span === "object" && typeof span.text === "string")
      : [];
  });
}

export type SimpleTableDatabaseNames = {
  /** The first column's name when the table has no header row: "제목". */
  title: string;
  /** Another column's name then, by its place after the first: "1열". */
  column(index: number): string;
  /** The widths the table's columns show at, which its properties keep. */
  widths?: readonly number[] | null;
};

/**
 * The inline database a simple table turns into, as Notion's "데이터베이스로
 * 전환" makes it (seen 2026-10-11): an untitled database whose first column
 * is its title and every other a text property, named by the header row, or
 * "제목", "1열", "2열" … without one; every other row, empty ones too,
 * becomes a row with its cells' text and formatting. Its columns keep the
 * table's widths and its view doesn't wrap, a title cell's line break
 * becoming a space (seen 2026-11-07: 240px columns stayed 240, a
 * long title one line, "E⏎S" the title "E S"). Its one view has no name,
 * so each reader sees its layout's name in their own language, as Notion
 * leaves it (seen 2026-11-11: an empty name field, its tab "표"). Null when
 * the database would not fit one change.
 */
export function databaseFromSimpleTable(graph: ContentGraph, table: GraphBlock, pageId: string, names: SimpleTableDatabaseNames) {
  const width = Math.max(1, Math.min(SIMPLE_TABLE_MAX_WIDTH, Number(table.content.tableWidth) || 1));
  const tableRows = editorBlockChildren(graph, table.id).filter((row) => row.type === "table_row");
  const header = table.content.hasColumnHeader === true ? tableRows[0] : undefined;
  const bodyRows = header ? tableRows.slice(1) : tableRows;
  if (4 + width + bodyRows.length + tableRows.length + 1 > SIMPLE_TABLE_TURN_MAX_OPERATIONS) return null;
  const headerText = header ? tableRowCells(header, width).map((spans) => spans.map((span) => span.text).join("").trim()) : [];
  const dataSource: GraphDataSource = { id: newId(), pageId, title: "" };
  let position: string | undefined;
  const properties = Array.from({ length: width }, (_, index): GraphProperty => {
    position = graphPositionBetween(position, undefined);
    return { id: newId(), dataSourceId: dataSource.id, type: index === 0 ? "title" : "rich_text",
      name: headerText[index] || (index === 0 ? names.title : names.column(index)), position, config: {} };
  });
  const widths = names.widths?.length === width
    ? Object.fromEntries(properties.map((property, index) => [property.id, names.widths![index]])) : null;
  const view: GraphView = { id: newId(), dataSourceId: dataSource.id, type: "table", name: "",
    position: graphPositionBetween(undefined, undefined), config: { wrap: false, ...(widths ? { propertyWidths: widths } : {}) } };
  let rowPosition: string | undefined;
  const rows = bodyRows.map((tableRow): GraphRow => {
    rowPosition = graphPositionBetween(rowPosition, undefined);
    const values: Record<string, GraphRowValue> = {};
    tableRowCells(tableRow, width).forEach((spans, index) => {
      const value = cellValueFromSpans((index === 0 ? spans.map((span) => ({ ...span, text: span.text.replace(/\n/g, " ") })) : spans) as TextSpan[]);
      if (index === 0 || value.value) values[properties[index].id] = { type: properties[index].type, ...value };
    });
    return { id: newId(), dataSourceId: dataSource.id, subitemParentId: null, position: rowPosition, values };
  });
  const placed = insertEditorBlock(graph, table.id, "inline_database", { dataSourceId: dataSource.id, viewId: view.id });
  const container: GraphDatabaseContainer = { id: placed.block.id, pageId, blockId: placed.block.id, parentBlockId: placed.block.parentId,
    position: placed.block.position, kind: "source", sourceContainerId: null, dataSourceIds: [dataSource.id], viewIds: [view.id] };
  const removed = removeEditorBlocks(placed.graph, [table.id]);
  return {
    graph: { ...removed.graph, databaseContainers: [...removed.graph.databaseContainers, container], dataSources: [...removed.graph.dataSources, dataSource],
      views: [...removed.graph.views, view], properties: [...removed.graph.properties, ...properties], rows: [...removed.graph.rows, ...rows] },
    operations: [...placed.operations, { kind: "upsertDataSource", dataSource }, { kind: "upsertView", view },
      ...properties.map((property) => ({ kind: "upsertProperty", property }) as GraphOperation),
      ...rows.map((row) => ({ kind: "upsertRow", row }) as GraphOperation), ...removed.operations],
  } satisfies SimpleTableTurn;
}

/**
 * A page's graph with the database a change made along with its block, or the
 * views a copied linked database's copy took (blockViewCopies.ts), which block
 * changes alone leave out.
 */
export function withMadeDatabase(graph: ContentGraph, next: ContentGraph, operations: readonly GraphOperation[]): ContentGraph {
  if (!operations.some((operation) => operation.kind === "upsertDataSource" || operation.kind === "upsertView")) return graph;
  const add = <T extends { id: string }>(items: T[], made: (T | undefined)[]) => {
    const known = new Set(items.map((item) => item.id));
    const fresh = made.filter((item): item is T => !!item && !known.has(item.id));
    return fresh.length ? [...items, ...fresh] : items;
  };
  const blocks = new Set(operations.flatMap((operation) => operation.kind === "upsertBlock" ? [operation.block.id] : []));
  // A pasted linked view's source this page doesn't hold shows as drawn where it was copied (blockViewCopies.ts).
  const shown = new Set(operations.flatMap((operation) => operation.kind === "upsertView" ? [operation.view.dataSourceId] : []));
  const framed = new Set([...shown].filter((id) => !graph.dataSources.some((source) => source.id === id)));
  return {
    ...graph,
    databaseContainers: add(graph.databaseContainers, next.databaseContainers.filter((container) => blocks.has(container.blockId))),
    // A database made without a name has none.
    dataSources: add(graph.dataSources, [...operations.map((operation) => operation.kind === "upsertDataSource"
      ? { title: "", ...operation.dataSource } : undefined), ...next.dataSources.filter((source) => framed.has(source.id))]),
    views: add(graph.views, operations.map((operation) => operation.kind === "upsertView" ? operation.view : undefined)),
    properties: add(graph.properties, [...operations.map((operation) => operation.kind === "upsertProperty" ? operation.property : undefined),
      ...next.properties.filter((property) => framed.has(property.dataSourceId))]),
    rows: add(graph.rows, operations.map((operation) => operation.kind === "upsertRow" ? operation.row : undefined)),
  };
}
