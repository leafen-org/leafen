import type { ContentGraph, GraphBlock, GraphDataSource, GraphOperation, GraphProperty, GraphView } from "@/contentGraph";
import { newId } from "@/lib/ids";

/**
 * The views a block owns — a linked database's tabs, which name the block as
 * their container — go with its copy as copies of their own, the copy showing
 * the copy of the tab its block names: a linked database duplicated, ⌥-dragged
 * or copied and pasted is a linked view of the same source with the same tabs
 * and settings, as Notion's (seen 2026-11-25 in two tabs: ⌘D, ⌘⇧D, "복제 ›"
 * and a paste on its page and on another all made one, its rows shown, its
 * first tab open though the original showed another). Without them the copy
 * named its original's view, which the server refuses as another block's.
 */
function copyViews(views: readonly GraphView[], copyId: string) {
  const ids = new Map<string, string>();
  const copies = views.map((view) => {
    const id = newId();
    ids.set(view.id, id);
    return { ...structuredClone(view), id, containerId: copyId } as GraphView;
  });
  return { copies, ids };
}

/** A copy's content naming its own copy of the view its original showed. */
function contentShowingCopiedView(content: Record<string, unknown>, viewIds: ReadonlyMap<string, string>) {
  const viewId = typeof content.viewId === "string" ? viewIds.get(content.viewId) : undefined;
  return viewId ? { ...content, viewId } : content;
}

const upsertViews = (views: readonly GraphView[]) => views.map((view) => ({ kind: "upsertView", view }) as GraphOperation);

/**
 * A block's views live and go with it, as the server keeps them
 * (deleteOrphanedLinkedDatabaseViews): a linked database removed takes its
 * tabs with it, and whatever brings the block back — ⌘Z of its removal, ⌘⇧Z
 * of its making (editorHistoryModel.ts) — makes them again with it, the same
 * tabs with their filters and sorts, as Notion's ⌘Z brings a removed linked
 * view back (seen 2026-11-25). Without them its return named a view the
 * server no longer had, and the server refused it.
 */
export function ownViewsOf(graph: ContentGraph, blockIds: ReadonlySet<string>) {
  const owned = new Map<string, GraphView[]>();
  if (!blockIds.size) return owned;
  for (const view of graph.views) {
    if (!view.containerId || !blockIds.has(view.containerId)) continue;
    owned.set(view.containerId, [...owned.get(view.containerId) ?? [], view]);
  }
  return owned;
}

/** A graph without the views of the blocks that left it (ownViewsOf). */
export function withoutViewsOfGone(graph: ContentGraph, goneIds: ReadonlySet<string>): ContentGraph {
  if (!goneIds.size || !graph.views.some((view) => view.containerId && goneIds.has(view.containerId))) return graph;
  return { ...graph, views: graph.views.filter((view) => !view.containerId || !goneIds.has(view.containerId)) };
}

/** The views a block brought back makes again with it: those it owned that the graph no longer has. */
export function viewsToMakeAgain(graph: ContentGraph, kept: readonly GraphView[]) {
  const held = new Set(graph.views.map((view) => view.id));
  return kept.filter((view) => !held.has(view.id));
}

/**
 * Copied blocks, each with its original's id (`copyIds`), and the graph after
 * them, with copies of the views their originals own, each copy naming its own,
 * and the database containers of the copies, as the server will project them.
 * Returns the copies, the graph and the operations that make them, the views'
 * after the blocks'.
 */
export function withOwnViewCopies(graph: ContentGraph, copies: readonly GraphBlock[], copyIds: ReadonlyMap<string, string>) {
  const views: GraphView[] = [];
  const viewIds = new Map<string, string>();
  for (const [sourceId, copyId] of copyIds) {
    const made = copyViews(graph.views.filter((view) => view.containerId === sourceId), copyId);
    views.push(...made.copies);
    for (const [from, to] of made.ids) viewIds.set(from, to);
  }
  const blocks = viewIds.size ? copies.map((block) => block.type === "inline_database"
    ? { ...block, content: contentShowingCopiedView(block.content, viewIds) } : block) : [...copies];
  const copyOf = new Map(blocks.map((block) => [block.id, block]));
  const containers = viewIds.size ? graph.databaseContainers.flatMap((container) => {
    const copy = copyOf.get(copyIds.get(container.blockId) ?? "");
    return copy ? [{ ...container, id: copy.id, blockId: copy.id, parentBlockId: copy.parentId ?? null, position: copy.position,
      viewIds: container.viewIds.map((id) => viewIds.get(id) ?? id) }] : [];
  }) : [];
  return {
    copies: blocks,
    graph: { ...graph, blocks: [...graph.blocks, ...blocks], views: [...graph.views, ...views],
      databaseContainers: [...graph.databaseContainers, ...containers] },
    operations: [...blocks.map((block) => ({ kind: "upsertBlock", block }) as GraphOperation), ...upsertViews(views)],
  };
}

/**
 * Where a copied block carries the views it owns, and the sources they show
 * as the page drew them, for its paste to make copies of them and draw them
 * at once on a page that doesn't hold those sources (withPastedViewCopies).
 */
const COPIED_VIEWS = "copiedViews", COPIED_VIEW_SOURCES = "copiedViewSources";

/** A block's content as copied to the clipboard, with the views it owns and the sources they show. */
export function contentWithOwnViews(content: Record<string, unknown>, graph: ContentGraph, blockId: string) {
  const own = graph.views.filter((view) => view.containerId === blockId);
  if (!own.length) return content;
  const shown = new Set(own.map((view) => view.dataSourceId));
  return { ...content, [COPIED_VIEWS]: own, [COPIED_VIEW_SOURCES]: {
    dataSources: graph.dataSources.filter((source) => shown.has(source.id)),
    properties: graph.properties.filter((property) => shown.has(property.dataSourceId)),
  } };
}

/**
 * The sources carried with pasted views that a graph doesn't hold, as the page
 * they were copied on drew them — their names and properties, which a linked
 * view's own page holds once it reads them, its rows with them.
 */
function carriedSources(graph: ContentGraph, carried: unknown) {
  const frame = carried as { dataSources?: unknown; properties?: unknown } | null;
  const held = new Set(graph.dataSources.map((source) => source.id));
  const dataSources = (Array.isArray(frame?.dataSources) ? frame.dataSources as GraphDataSource[] : [])
    .filter((source) => typeof source?.id === "string" && !held.has(source.id));
  const added = new Set(dataSources.map((source) => source.id));
  const properties = (Array.isArray(frame?.properties) ? frame.properties as GraphProperty[] : [])
    .filter((property) => typeof property?.id === "string" && added.has(property.dataSourceId));
  return { dataSources, properties };
}

/** Whether operations make views of sources a graph doesn't hold, which reading the page again brings. */
export function makesViewsOfSourcesElsewhere(graph: ContentGraph, operations: readonly GraphOperation[]) {
  return operations.some((operation) => operation.kind === "upsertView" && !!operation.view.containerId
    && !graph.dataSources.some((source) => source.id === operation.view.dataSourceId));
}

function isCopiedView(value: unknown): value is GraphView {
  const view = value as Partial<GraphView> | null;
  return !!view && typeof view.id === "string" && typeof view.dataSourceId === "string" && typeof view.type === "string"
    && typeof view.name === "string" && typeof view.position === "string" && !!view.config && typeof view.config === "object";
}

/**
 * A change making blocks pasted from a copy that carried the views its block
 * owned (contentWithOwnViews): each such block takes copies of them, made
 * after it, and shows its copy of the one its original showed, the views
 * themselves left out of its content.
 */
export function withPastedViewCopies<Change extends { graph: ContentGraph; operations: GraphOperation[] }>(change: Change): Change {
  const views: GraphView[] = [];
  const contents = new Map<string, Record<string, unknown>>();
  let graph = change.graph;
  const operations = change.operations.map((operation) => {
    if (operation.kind !== "upsertBlock" || !(COPIED_VIEWS in operation.block.content)) return operation;
    const { [COPIED_VIEWS]: carried, [COPIED_VIEW_SOURCES]: sources, ...content } = operation.block.content;
    const made = copyViews(operation.block.type === "inline_database" && Array.isArray(carried) ? carried.filter(isCopiedView) : [],
      operation.block.id);
    views.push(...made.copies);
    // Their sources show as they were drawn where copied, till the page reads them (makesViewsOfSourcesElsewhere).
    const frame = carriedSources(graph, made.copies.length ? sources : null);
    graph = { ...graph, dataSources: [...graph.dataSources, ...frame.dataSources], properties: [...graph.properties, ...frame.properties] };
    contents.set(operation.block.id, contentShowingCopiedView(content, made.ids));
    return { ...operation, block: { ...operation.block, content: contents.get(operation.block.id)! } };
  });
  if (!contents.size) return change;
  const blocks = graph.blocks.map((block) => contents.has(block.id) ? { ...block, content: contents.get(block.id)! } : block);
  return { ...change, graph: { ...graph, blocks, views: [...graph.views, ...views] }, operations: [...operations, ...upsertViews(views)] };
}
