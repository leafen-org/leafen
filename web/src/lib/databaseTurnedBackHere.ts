import type { ContentGraph, GraphBlock } from "@/contentGraph";
import type { PageGraphStore } from "@/lib/pageGraphStore";
import { holdRowsShown } from "@/lib/rowsShownAtPress";
import { shownGraph } from "@/lib/shownGraphs";
import { databasePageAhead } from "@/lib/databasePageAhead";

/** An inline database as a graph draws it: its block, and what its views read. */
export type DrawnInlineDatabase = Pick<ContentGraph, "databaseContainers" | "dataSources" | "views" | "properties" | "rows"> & {
  block: GraphBlock;
};

/** The database `block` as `graph` draws it. */
export function inlineDatabaseDrawn(graph: ContentGraph, block: GraphBlock): DrawnInlineDatabase {
  const container = graph.databaseContainers.find(({ blockId }) => blockId === block.id);
  const sources = new Set(container?.dataSourceIds ?? [String(block.content.dataSourceId ?? "")]);
  return {
    block,
    databaseContainers: container ? [container] : [],
    dataSources: graph.dataSources.filter(({ id }) => sources.has(id)),
    views: graph.views.filter(({ id, dataSourceId }) => container ? container.viewIds.includes(id) : sources.has(dataSourceId)),
    properties: graph.properties.filter(({ dataSourceId }) => sources.has(dataSourceId)),
    rows: graph.rows.filter(({ dataSourceId }) => sources.has(dataSourceId)),
  };
}

/** `items` with those of `more` it lacks, by ID; `items` itself when it lacks none. */
function withMissing<T extends { id: string }>(items: T[], more: readonly T[]): T[] {
  const held = new Set(items.map(({ id }) => id));
  const missing = more.filter(({ id }) => !held.has(id));
  return missing.length ? [...items, ...missing] : items;
}

/**
 * A full-page database turned inline where its page's link stands — taken
 * back by the page's ⌘Z after "페이지로 전환", or by its link's own
 * "인라인 데이터베이스로 전환" — shown so at once (user decision
 * 2026-11-12: a press answers on the next frame — its page's link stayed
 * some 1.1 s while the server took the page's four steps back, read
 * "휴지통에 있음" a moment once its page was in the trash, and then the
 * database came, 1.3–1.5 s on a NAS; Notion's comes back in 0.1 s with
 * nothing between, seen 2026-11-14): the database stands in its page's
 * link's place, drawn as its own page shows it last in this tab (its rows as
 * changed there) or as read while its link's menu was open
 * (databasePageAhead.ts), else as it was drawn here before it turned
 * (`drawn`); with none, it waits for the server. Held over every graph the open page
 * takes, while it is this page's (`here`), until released: once the page is
 * read back with it, or refused, the link back where it stood.
 */
export function showDatabaseTurnedBackHere(input: {
  principalId: string; store: PageGraphStore; pageId: string; pageBlockId: string; drawn: DrawnInlineDatabase | null; here: () => boolean;
}) {
  const { store, pageId, pageBlockId, here } = input;
  const pageBlock = store.get().blocks.find(({ id }) => id === pageBlockId);
  if (!pageBlock) return null;
  const own = shownGraph(input.principalId, pageBlockId)?.graph ?? databasePageAhead(input.principalId, pageBlockId);
  const ownBlock = own?.blocks.find(({ parentId, type }) => !parentId && type === "inline_database");
  const drawn = own && ownBlock ? inlineDatabaseDrawn(own, ownBlock) : input.drawn;
  if (!drawn) return null;
  const database: GraphBlock = { ...drawn.block, parentId: pageBlock.parentId, afterId: pageBlock.afterId, position: pageBlock.position };
  const owns = drawn.databaseContainers[0]?.kind !== "linked";
  const containers = drawn.databaseContainers.map((container) => ({ ...container, pageId, parentBlockId: pageBlock.parentId }));
  const sources = drawn.dataSources.map((source) => owns ? { ...source, pageId } : source);
  const back = (now: ContentGraph): ContentGraph => {
    if (!here()) return now;
    const linked = now.blocks.some(({ id }) => id === pageBlockId);
    const standing = now.blocks.find(({ id }) => id === database.id);
    const container = now.databaseContainers.find(({ blockId }) => blockId === database.id);
    // The page read back with it: the server's own.
    if (!linked && standing && container?.pageId === pageId) return now;
    const blocks = [...now.blocks.filter(({ id }) => id !== pageBlockId && id !== database.id), standing ?? database];
    return {
      ...now,
      blocks,
      databaseContainers: container?.pageId === pageId ? now.databaseContainers
        : [...now.databaseContainers.filter(({ blockId }) => blockId !== database.id), ...containers],
      dataSources: withMissing(now.dataSources, sources),
      views: withMissing(now.views, drawn.views),
      properties: withMissing(now.properties, drawn.properties),
      rows: withMissing(now.rows, drawn.rows),
    };
  };
  const link = (now: ContentGraph): ContentGraph => !here() || now.blocks.some(({ id }) => id === pageBlockId) ? now
    : { ...now, blocks: [...now.blocks.filter(({ id }) => id !== database.id), pageBlock] };
  // In the key's own frame, as a page's blocks back at a ⌘Z (showBlocksBack).
  const release = holdRowsShown(store, back, link, true);
  return { databaseId: database.id, release };
}
