import type {
  GraphBlock,
  GraphDatabaseContainer,
  GraphDataSource,
  GraphDataSourceWrite,
  GraphOperation,
  GraphProperty,
  GraphReadItem,
  GraphRow,
  GraphValueType,
  GraphView,
  GraphViewType,
} from "./contentGraphWire.generated";
import { graphManagedFileBlockTypes } from "./contentGraphWire.generated";
import { graphPositionBetween } from "@/lib/graphPosition";
import { newId } from "@/lib/ids";
import { viewWrapsAtFirst } from "@/lib/viewFirstWrap";

export {
  graphBlockTypes,
  graphManagedFileBlockTypes,
  graphTextBlockTypes,
  graphOperationKinds,
  graphReadKinds,
  graphValueTypes,
  graphViewTypes,
  graphWireBounds,
  graphWireErrorCodes,
} from "./contentGraphWire.generated";
export type {
  GraphBlock,
  GraphBlockDelete,
  GraphBlockMove,
  GraphBlockType,
  GraphChange,
  GraphDatabaseContainer,
  GraphDataSource,
  GraphDataSourceWrite,
  GraphMutationCounts,
  GraphMutationInput,
  GraphMutationReceipt,
  GraphPageMutationReceipt,
  GraphTextSpan,
  GraphTextUpdate,
  GraphOperation,
  GraphProperty,
  GraphReadItem,
  GraphReadResponse,
  GraphRow,
  GraphRowTemplateFill,
  GraphRowValue,
  GraphValueType,
  GraphView,
  GraphViewType,
} from "./contentGraphWire.generated";

export type ContentGraph = {
  blocks: GraphBlock[];
  databaseContainers: GraphDatabaseContainer[];
  dataSources: GraphDataSource[];
  views: GraphView[];
  properties: GraphProperty[];
  rows: GraphRow[];
};

export const emptyContentGraph: ContentGraph = {
  blocks: [],
  databaseContainers: [],
  dataSources: [],
  views: [],
  properties: [],
  rows: [],
};

const managedFileBlockTypeSet = new Set<string>(graphManagedFileBlockTypes);

export function isManagedFileBlock(block: GraphBlock) {
  if (!managedFileBlockTypeSet.has(block.type)) return false;
  return Object.hasOwn(block.content, "fileId");
}

function compareCodeUnits(left: string, right: string) {
  return left < right ? -1 : left > right ? 1 : 0;
}

/**
 * Orders content by its position, then its ID, code unit by code unit as the
 * server's store orders them, never by the reader's language, so that every
 * client and the server lay the same items out in the same order.
 */
export function compareGraphOrder<T extends { position: string; id: string }>(left: T, right: T) {
  return compareCodeUnits(left.position, right.position) || compareCodeUnits(left.id, right.id);
}

const byPosition = compareGraphOrder;

export function materializeContentGraph(items: GraphReadItem[]): ContentGraph {
  const graph: ContentGraph = {
    blocks: [],
    databaseContainers: [],
    dataSources: [],
    views: [],
    properties: [],
    rows: [],
  };
  for (const item of items) {
    if (item.kind === "block") graph.blocks.push(item.value as GraphBlock);
    if (item.kind === "databaseContainer") graph.databaseContainers.push(item.value as GraphDatabaseContainer);
    if (item.kind === "dataSource") graph.dataSources.push(item.value as GraphDataSource);
    if (item.kind === "view") graph.views.push(item.value as GraphView);
    if (item.kind === "property") graph.properties.push(item.value as GraphProperty);
    if (item.kind === "row") graph.rows.push(item.value as GraphRow);
  }
  graph.blocks.sort(byPosition);
  graph.databaseContainers.sort(byPosition);
  graph.views.sort(byPosition);
  graph.properties.sort(byPosition);
  graph.rows.sort(byPosition);
  return graph;
}

function positionAt(index: number) {
  return `a${String(index).padStart(8, "0")}`;
}

function rootAppendPlacement(blocks: GraphBlock[]) {
  const roots = blocks.filter((block) => block.parentId === null);
  return { blockIndex: roots.length, afterId: roots.at(-1)?.id ?? null };
}

export function createParagraph(existingBlocks: GraphBlock[]) {
  const { blockIndex, afterId } = rootAppendPlacement(existingBlocks);
  const block: GraphBlock = {
    id: newId(),
    parentId: null,
    afterId,
    position: positionAt(blockIndex),
    type: "paragraph",
    content: {},
  };
  return { block, operations: [{ kind: "upsertBlock", block }] as GraphOperation[] };
}

/** The names a new inline database's view and property start with. */
export type InlineDatabaseNames = { name: string };

/**
 * A new database's source, view and title property, as Notion's empty
 * database starts (seen 2026-10-04 and 2026-10-06): one view, the table or
 * the one it is made for, a "Name" column and no rows. A view not given a
 * name has none, so each reader sees its layout's name in their own language,
 * as Notion leaves a database's first view made from its menus (seen
 * 2026-11-11: an empty name field, "Untitled" through its API).
 */
export function createInlineDatabase(
  existingBlocks: GraphBlock[],
  title: string,
  pageId: string,
  names: InlineDatabaseNames,
  firstView?: { type: GraphViewType; name: string; config?: Record<string, unknown> },
) {
  const { blockIndex, afterId } = rootAppendPlacement(existingBlocks);
  const dataSource: GraphDataSource = { id: newId(), pageId, title };
  const viewType = firstView?.type ?? "table";
  const view: GraphView = {
    id: newId(),
    dataSourceId: dataSource.id,
    type: viewType,
    name: firstView?.name ?? "",
    position: positionAt(0),
    // Its own row order, as every view made since keeps one (viewRowOrder.ts).
    config: { ...(viewWrapsAtFirst(viewType) ? { wrap: true } : {}), rowOrder: [], ...firstView?.config },
  };
  const titleProperty: GraphProperty = {
    id: newId(),
    dataSourceId: dataSource.id,
    type: "title",
    name: names.name,
    position: positionAt(0),
    config: {},
  };
  const block: GraphBlock = {
    id: newId(),
    parentId: null,
    afterId,
    position: positionAt(blockIndex),
    type: "inline_database",
    content: { dataSourceId: dataSource.id, viewId: view.id },
  };
  const container: GraphDatabaseContainer = {
    id: block.id,
    pageId,
    blockId: block.id,
    parentBlockId: block.parentId,
    position: block.position,
    kind: "source",
    sourceContainerId: null,
    dataSourceIds: [dataSource.id],
    viewIds: [view.id],
  };
  const operations: GraphOperation[] = [
    { kind: "upsertBlock", block },
    { kind: "upsertDataSource", dataSource },
    { kind: "upsertView", view },
    { kind: "upsertProperty", property: titleProperty },
  ];
  return {
    block,
    container,
    dataSource,
    views: [view],
    properties: [titleProperty],
    operations,
  };
}

// A linked database starts with its own copy of the chosen view, so changing
// its views never changes the original database.
export function createLinkedInlineDatabase(
  existingBlocks: GraphBlock[],
  pageId: string,
  source: {
    containerId: string;
    dataSourceId: string;
    viewType: GraphViewType;
    viewName: string;
    viewConfig: Record<string, unknown>;
  },
) {
  const { blockIndex, afterId } = rootAppendPlacement(existingBlocks);
  const blockId = newId();
  const view: GraphView = {
    id: newId(),
    dataSourceId: source.dataSourceId,
    containerId: blockId,
    type: source.viewType,
    name: source.viewName,
    position: positionAt(0),
    config: { ...source.viewConfig },
  };
  const block: GraphBlock = {
    id: blockId,
    parentId: null,
    afterId,
    position: positionAt(blockIndex),
    type: "inline_database",
    content: {
      dataSourceId: source.dataSourceId,
      viewId: view.id,
      linkedDatabaseSource: true,
      sourceContainerId: source.containerId,
    },
  };
  const container: GraphDatabaseContainer = {
    id: block.id,
    pageId,
    blockId: block.id,
    parentBlockId: null,
    position: block.position,
    kind: "linked",
    sourceContainerId: source.containerId,
    dataSourceIds: [source.dataSourceId],
    viewIds: [view.id],
  };
  return {
    block,
    container,
    view,
    operations: [{ kind: "upsertBlock", block }, { kind: "upsertView", view }] as GraphOperation[],
  };
}

/**
 * The first of name, "name 1", "name 2", … no other property has, as the
 * server names a property made, renamed or moved onto a name its source has
 * (database_property_names.go).
 */
export function distinctPropertyName(name: string, taken: ReadonlySet<string>) {
  if (!taken.has(name)) return name;
  for (let number = 1; ; number += 1) {
    const candidate = `${name} ${number}`;
    if (!taken.has(candidate)) return candidate;
  }
}

export function createGraphProperty(
  existingProperties: GraphProperty[],
  dataSourceId: string,
  type: GraphValueType,
  name: string,
  config: Record<string, unknown> = {},
) {
  const id = newId();
  // A new property comes after the source's last, whichever way that one
  // was made (2026-10-06: a count-based key put a new relation mid-table).
  const last = [...existingProperties].sort(compareGraphOrder).at(-1);
  // Named as the server will name it, so what shows before it answers (a
  // column's head, the settings opened on it) is the name it keeps
  // (2026-11-14: a third "관계된 X" showed "관계된 X" in its settings and
  // "관계된 X 3" in its head).
  const taken = new Set(existingProperties.filter((property) => property.dataSourceId === dataSourceId)
    .map((property) => property.name.trim()));
  const property: GraphProperty = {
    id,
    dataSourceId,
    type,
    name: distinctPropertyName(name.trim(), taken),
    position: graphPositionBetween(last?.position, undefined),
    config,
  };
  return {
    property,
    operations: [{ kind: "upsertProperty", property }] as GraphOperation[],
  };
}

/**
 * A view the client already has is sent as an update from the view it
 * shows, so the server keeps what others changed in the view meanwhile.
 */
export function graphViewUpdates(graph: ContentGraph, operations: readonly GraphOperation[]): GraphOperation[] {
  const views = new Map(graph.views.map((view) => [view.id, view]));
  return operations.map((operation) => {
    const base = operation.kind === "upsertView" ? views.get(operation.view.id) : undefined;
    return base && operation.kind === "upsertView"
      ? { kind: "updateView", viewUpdate: { base, view: operation.view } }
      : operation;
  });
}

/**
 * A block that already stands somewhere, written for what it holds and not
 * for where it is: the server moves an existing block only when the write
 * names a block to follow, and the one this screen last read before it may
 * be gone or no longer before it (2026-10-30: adding a data source failed
 * with "Block order anchor not found." after the block above the database
 * was deleted).
 */
export function blockInPlace(block: GraphBlock): GraphBlock {
  return { ...block, afterId: null };
}

/**
 * A database written with a change of some of its fields: the fields the
 * change names, the rest as they were. The server writes a data source so
 * (writeDatabaseSourceRecord): each field only when written, its name too,
 * and its name's formatting, when none is written, only while the name stays
 * the same words.
 */
export function withDataSourceChange<Source extends GraphDataSourceWrite>(source: Source, change: GraphDataSourceWrite): Source {
  const { title, titleSpans, ...fields } = change;
  const renamed = title !== undefined && title !== source.title;
  const spans = titleSpans ?? (renamed ? undefined : source.titleSpans);
  const { titleSpans: _written, ...merged } = { ...source, ...fields, ...(title !== undefined ? { title } : {}) };
  return (spans ? { ...merged, titleSpans: spans } : merged) as Source;
}

/**
 * Two changes to one view waiting in the same batch make one change: from
 * the first one's starting view to the last one's view. A block made and
 * then changed in the same batch keeps the place it was made at: the later
 * change names no anchor because the block already exists, and without one
 * the new block would be made first on the page. Two writes to one database
 * make one with the fields of both, the later's where both write one.
 */
export function coalesceGraphOperation(previous: GraphOperation | undefined, next: GraphOperation): GraphOperation {
  if (previous?.kind === "upsertDataSource" && next.kind === "upsertDataSource") {
    return { kind: "upsertDataSource", dataSource: withDataSourceChange(previous.dataSource, next.dataSource) };
  }
  if (previous?.kind === "updateView" && next.kind === "updateView") {
    return { kind: "updateView", viewUpdate: { base: previous.viewUpdate.base, view: next.viewUpdate.view } };
  }
  if (previous?.kind === "upsertView" && next.kind === "updateView") {
    return { kind: "upsertView", view: next.viewUpdate.view };
  }
  // A row's change holds the values it changes, which the server merges into
  // the row's others: one made while another to the row waits keeps both.
  if (previous?.kind === "upsertRow" && next.kind === "upsertRow") {
    return { kind: "upsertRow", row: { ...previous.row, ...next.row, values: { ...previous.row.values, ...next.row.values } } };
  }
  if (previous?.kind === "upsertBlock" && next.kind === "upsertBlock") {
    const { baseContent: _base, ...block } = next.block;
    return { kind: "upsertBlock", block: { ...block,
      ...(previous.block.type === next.block.type && previous.block.baseContent
        ? { baseContent: previous.block.baseContent } : {}),
      afterId: previous.block.afterId && !next.block.afterId ? previous.block.afterId : next.block.afterId,
    } };
  }
  return next;
}

export function graphOperationKey(operation: GraphOperation) {
  if (operation.kind === "restoreDateCommand") return `${operation.kind}:${operation.dateRestore.mutationId}:${operation.dateRestore.forward}`;
  if (operation.kind === "upsertBlock") return `${operation.kind}:${operation.block.id}`;
  if (operation.kind === "moveBlock") return `${operation.kind}:${operation.move.blockId}`;
  if (operation.kind === "deleteBlock") return `${operation.kind}:${operation.deletion.blockId}`;
  if (operation.kind === "upsertDataSource") return `${operation.kind}:${operation.dataSource.id}`;
  if (operation.kind === "deleteDataSource") return `${operation.kind}:${operation.dataSourceDeletion.id}`;
  if (operation.kind === "upsertView") return `${operation.kind}:${operation.view.id}`;
  if (operation.kind === "updateView") return `upsertView:${operation.viewUpdate.view.id}`;
  if (operation.kind === "deleteView") return `${operation.kind}:${operation.viewDeletion.id}`;
  if (operation.kind === "upsertProperty") return `${operation.kind}:${operation.property.id}`;
  if (operation.kind === "deleteProperty") return `${operation.kind}:${operation.propertyDeletion.id}`;
  if (operation.kind === "upsertRow") return `${operation.kind}:${operation.row.id}`;
  if (operation.kind === "deleteRow") return `${operation.kind}:${operation.rowDeletion.id}`;
  if (operation.kind === "configureDependency") return `${operation.kind}:${operation.dependencyBinding.dataSourceId}`;
  if (operation.kind === "upsertDependencyEdge") return `${operation.kind}:${operation.dependencyEdge.id}`;
  return `${operation.kind}:${operation.dependencyEdgeDeletion.id}`;
}

/**
 * The record an operation changes: a block, data source, view, property,
 * row, dependency setting or dependency link. One change to the server
 * touches a record once, so different waiting changes to one record (a block
 * made, then moved) go out one after another.
 */
export function graphOperationSubject(operation: GraphOperation) {
  if (operation.kind === "restoreDateCommand") return `dateCommand:${operation.dateRestore.mutationId}`;
  if (operation.kind === "upsertBlock") return `block:${operation.block.id}`;
  if (operation.kind === "moveBlock") return `block:${operation.move.blockId}`;
  if (operation.kind === "deleteBlock") return `block:${operation.deletion.blockId}`;
  if (operation.kind === "upsertDataSource") return `dataSource:${operation.dataSource.id}`;
  if (operation.kind === "deleteDataSource") return `dataSource:${operation.dataSourceDeletion.id}`;
  if (operation.kind === "upsertView") return `view:${operation.view.id}`;
  if (operation.kind === "updateView") return `view:${operation.viewUpdate.view.id}`;
  if (operation.kind === "deleteView") return `view:${operation.viewDeletion.id}`;
  if (operation.kind === "upsertProperty") return `property:${operation.property.id}`;
  if (operation.kind === "deleteProperty") return `property:${operation.propertyDeletion.id}`;
  if (operation.kind === "upsertRow") return `row:${operation.row.id}`;
  if (operation.kind === "deleteRow") return `row:${operation.rowDeletion.id}`;
  if (operation.kind === "configureDependency") return `dependency:${operation.dependencyBinding.dataSourceId}`;
  if (operation.kind === "upsertDependencyEdge") return `dependencyEdge:${operation.dependencyEdge.id}`;
  return `dependencyEdge:${operation.dependencyEdgeDeletion.id}`;
}
