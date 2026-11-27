import { useEffect, useState } from "react";
import { throwInvalidContentGraphContinuation } from "@/api";
import { request } from "@/apiTransport";
import { graphWireBounds, materializeContentGraph, type ContentGraph, type GraphReadResponse } from "@/contentGraph";
import { databasePreviewAgain } from "@/components/editor/pagePreviews";
import { collectContentGraphPages } from "@/lib/contentGraphCollection";
import type { Session } from "@/types";

/**
 * An inline database whose block alone is in the trash, its page out of it,
 * opened alone — from the trash's list or a mention, at its own address — as
 * Notion opens it (seen 2026-11-24 in Chrome at 1440, two tabs: the database
 * alone under the trash's banner, "… 님이 이 페이지를 휴지통으로
 * 이동했습니다", "페이지 복원", "영구 삭제하기"; restored, it stays open). Its
 * page's graph leaves it out; read asked for it (trashedDatabase=, the
 * server's content_graph_trashed_database.go), it comes beside the page's own,
 * its block marked trashedAlone and its sources in the trash, which the page
 * draws read only. Who sent it there and when is its preview's
 * (mention_trash_mark.go), for its banner.
 */
export type TrashedDatabaseAlone = {
  blockId: string;
  pageId: string;
  /** Its block, container, sources, views, properties and rows. */
  graph: ContentGraph;
  trashedAt: string;
  trashedBy: string;
};

/** Whether a block is an inline database the trash holds alone, read beside its page. */
export function trashedAloneBlock(graph: Pick<ContentGraph, "blocks">, blockId: string | null | undefined) {
  return Boolean(blockId && graph.blocks.some((block) => block.id === blockId && block.content.trashedAlone === true));
}

/**
 * The database the address opens alone, where it is one of the page's whose
 * block alone is in the trash (`shown`: the page's graph has it, as it has
 * one out of the trash or in a page in the trash); null otherwise, and again
 * once it is back (`shown`).
 */
export function useTrashedDatabaseAlone(session: Session | null | undefined, pageId: string | undefined, blockId: string | null, shown: boolean) {
  const [read, setRead] = useState<TrashedDatabaseAlone | null>(null);
  const wanted = session && pageId && blockId && !shown ? `${pageId}\u0000${blockId}` : "";
  useEffect(() => {
    if (!wanted || !session || !pageId || !blockId) return;
    let current = true;
    void (async () => {
      const preview = await databasePreviewAgain(session, blockId);
      if (!current || !preview?.inTrash || !preview.trashedAlone || preview.pageId !== pageId) return;
      const query = new URLSearchParams({ limit: String(graphWireBounds.readItems), trashedDatabase: blockId });
      const result = await collectContentGraphPages({
        readPage: (cursor) => {
          if (cursor) query.set("cursor", cursor);
          return request<GraphReadResponse>(`/pages/${encodeURIComponent(pageId)}/content-graph?${query}`, {}, session);
        },
        onInvalidContinuation: throwInvalidContentGraphContinuation,
        shouldContinue: () => current,
      }).catch(() => null);
      if (!current || !result) return;
      const graph = trashedDatabaseOf(materializeContentGraph(result.items), blockId);
      if (graph) setRead({ blockId, pageId, graph, trashedAt: preview.trashedAt ?? "", trashedBy: preview.trashedBy ?? "" });
    })();
    return () => { current = false; };
  }, [blockId, pageId, session, wanted]);
  return wanted && read && `${read.pageId}\u0000${read.blockId}` === wanted ? read : null;
}

/** Of a page's graph read with it, the trashed database's own pieces. */
function trashedDatabaseOf(graph: ContentGraph, blockId: string): ContentGraph | null {
  const block = graph.blocks.find((candidate) => candidate.id === blockId && candidate.content.trashedAlone === true);
  if (!block) return null;
  const containers = graph.databaseContainers.filter((container) => container.id === blockId || container.blockId === blockId);
  const sources = new Set(graph.dataSources.filter((source) => source.sourceInTrash === true).map((source) => source.id));
  const own = new Set([String(block.content.dataSourceId ?? ""), ...containers.flatMap((container) => container.dataSourceIds)]
    .filter((id) => sources.has(id)));
  return {
    blocks: [block],
    databaseContainers: containers,
    dataSources: graph.dataSources.filter((source) => own.has(source.id)),
    views: graph.views.filter((view) => view.containerId === blockId || own.has(view.dataSourceId)),
    properties: graph.properties.filter((property) => own.has(property.dataSourceId)),
    rows: graph.rows.filter((row) => own.has(row.dataSourceId)),
  };
}

/** A page's graph with the trashed database beside it, for the page to draw it alone. */
export function withTrashedDatabase(graph: ContentGraph, trashed: TrashedDatabaseAlone | null): ContentGraph {
  if (!trashed || graph.blocks.some((block) => block.id === trashed.blockId)) return graph;
  const add = <T extends { id: string }>(kept: readonly T[], more: readonly T[]) => {
    const known = new Set(kept.map(({ id }) => id));
    return [...kept, ...more.filter(({ id }) => !known.has(id))];
  };
  return {
    blocks: add(graph.blocks, trashed.graph.blocks),
    databaseContainers: add(graph.databaseContainers, trashed.graph.databaseContainers),
    dataSources: add(graph.dataSources, trashed.graph.dataSources),
    views: add(graph.views, trashed.graph.views),
    properties: add(graph.properties, trashed.graph.properties),
    rows: add(graph.rows, trashed.graph.rows),
  };
}
