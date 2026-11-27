import { blockInPlace, type ContentGraph, type GraphDataSource, type GraphDataSourceWrite, type GraphOperation, type GraphProperty } from "@/contentGraph";
import { notePageHistoryStep } from "@/components/editor/pageUndo";
import type { DatabaseContainerSource } from "@/lib/databaseContainerPicker";
import { graphPositionBetween } from "@/lib/graphPosition";
import { overlayPendingGraphOperations } from "./pendingGraphOverlay";

/**
 * A source linked in place (the start panel's "기존 데이터 소스 연결하기") is
 * one step of the page's ⌘Z once the server has it, as Notion's (seen
 * 2026-11-18 in two tabs at two times: "/데이터 소스 보기 연결", a source
 * chosen, its "새 보기" left by a press outside, then ⌘Z took the source
 * away, the database waiting for one again, and the next ⌘Z the database
 * with it; ⌘⇧Z made each again in turn). ⌘Z writes the database as it
 * waited (unlinkSourceOperations), ⌘⇧Z links it again with its views as
 * shown when taken back. Its "새 보기" left is no step; a view chosen there
 * is a step after it (graphViewStepOf). Both draw at the press, the page
 * read again once saved; their writes make no step (commitStepping).
 */
export function noteSourceLinkStep(pageId: string, source: DatabaseContainerSource, before: ContentGraph, link: readonly GraphOperation[], page: {
  graph: () => ContentGraph;
  setGraph: (next: (current: ContentGraph) => ContentGraph) => void;
  commit: (operations: GraphOperation[]) => Promise<boolean>;
  reread: () => Promise<unknown>;
}) {
  const back = unlinkSourceOperations(before, link);
  let again = [...link];
  notePageHistoryStep(pageId, `source-link:${++linkSteps}`, async (forward) => {
    if (!forward) again = linkAsShown(page.graph(), link);
    const operations = forward ? again : back;
    page.setGraph((current) => {
      const shown = overlayPendingGraphOperations(current, operations);
      return forward ? withLinkedSourceFrame(shown, source) : shown;
    });
    const saved = await page.commit([...operations]);
    if (saved) await page.reread();
    return saved;
  });
}

let linkSteps = 0;

/**
 * The writes that take a link in place back: the block as it stood, the
 * sources it let go with their properties, views and rows, the views it
 * changed as they were, and the views it made gone. Sources go before what
 * names them, the views made last.
 */
export function unlinkSourceOperations(before: ContentGraph, link: readonly GraphOperation[]): GraphOperation[] {
  const blocks: GraphOperation[] = [], sources: GraphOperation[] = [], properties: GraphOperation[] = [];
  const views = new Map<string, GraphOperation>(), rows: GraphOperation[] = [], made: GraphOperation[] = [];
  const restoreView = (id: string) => {
    const prior = before.views.find((view) => view.id === id);
    if (prior) views.set(id, { kind: "upsertView", view: prior });
    return Boolean(prior);
  };
  for (const operation of link) {
    if (operation.kind === "upsertBlock") {
      const prior = before.blocks.find(({ id }) => id === operation.block.id);
      if (prior) blocks.push({ kind: "upsertBlock", block: blockInPlace(prior) });
    } else if (operation.kind === "upsertView") {
      if (!restoreView(operation.view.id)) made.push({ kind: "deleteView", viewDeletion: { id: operation.view.id } });
    } else if (operation.kind === "deleteView") {
      restoreView(operation.viewDeletion.id);
    } else if (operation.kind === "deleteDataSource") {
      const id = operation.dataSourceDeletion.id;
      const prior = before.dataSources.find((source) => source.id === id);
      if (!prior) continue;
      sources.push({ kind: "upsertDataSource", dataSource: dataSourceWrite(prior) });
      for (const property of before.properties) if (property.dataSourceId === id) properties.push({ kind: "upsertProperty", property });
      for (const view of before.views) if (view.dataSourceId === id) restoreView(view.id);
      for (const row of before.rows) if (row.dataSourceId === id) rows.push({ kind: "upsertRow", row });
    }
  }
  return [...blocks, ...sources, ...properties, ...views.values(), ...rows, ...made];
}

/** The link written again with its block and views as the page shows them now. */
function linkAsShown(graph: ContentGraph, link: readonly GraphOperation[]): GraphOperation[] {
  return link.map((operation) => {
    if (operation.kind === "upsertView") {
      const shown = graph.views.find(({ id }) => id === operation.view.id);
      return shown ? { kind: "upsertView", view: shown } : operation;
    }
    if (operation.kind === "upsertBlock") {
      const shown = graph.blocks.find(({ id }) => id === operation.block.id);
      return shown ? { kind: "upsertBlock", block: blockInPlace(shown) } : operation;
    }
    return operation;
  });
}

// What a source's write names: what it holds, not what the server reads about it.
function dataSourceWrite(source: GraphDataSource): GraphDataSourceWrite {
  const { id, pageId, title, titleSpans, icon, iconType, description, descriptionHidden, descriptionSpans, cover, coverPosition } = source;
  const written: GraphDataSourceWrite = { id, pageId, title, titleSpans, icon, iconType, description, descriptionHidden, descriptionSpans, cover, coverPosition };
  for (const key of Object.keys(written) as (keyof GraphDataSourceWrite)[]) if (written[key] === undefined) delete written[key];
  return written;
}

/**
 * A source from another page, shown linked before the page is read again:
 * its name and the first properties its chooser read (previewProperties),
 * a title among them, and no rows yet. A source the page holds is drawn as
 * it is.
 */
export function withLinkedSourceFrame(graph: ContentGraph, source: DatabaseContainerSource): ContentGraph {
  if (graph.dataSources.some(({ id }) => id === source.dataSourceId)) return graph;
  const read = (source.previewProperties ?? []).filter(({ dataSourceId }) => dataSourceId === source.dataSourceId);
  const title: GraphProperty[] = read.some(({ type }) => type === "title") ? [] : [{
    id: `${source.dataSourceId}:title`, dataSourceId: source.dataSourceId, type: "title", name: "",
    position: graphPositionBetween(undefined, read[0]?.position), config: {},
  }];
  return {
    ...graph,
    dataSources: [...graph.dataSources, { id: source.dataSourceId, pageId: source.pageId, title: source.title }],
    properties: [...graph.properties, ...title, ...read],
  };
}
