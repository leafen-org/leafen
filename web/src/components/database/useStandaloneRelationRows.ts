import { useCallback, useMemo, useRef, useState } from "react";
import type { ContentGraph, GraphRow } from "@/contentGraph";
import { collectAuthenticatedContentGraph, collectContentGraphStructure } from "@/lib/authenticatedContentGraph";
import { sendContentGraphChange } from "@/lib/contentGraphSend";
import { newId } from "@/lib/ids";
import { dataSourcePage } from "@/lib/dataSourcePage";
import { graphPositionBetween } from "@/lib/nativeBoardProductAdapter";
import type { DbProperty, Page } from "@/lib/types";
import type { Session } from "@/types";
import { nativeDatabaseFilterSnapshot, nativeDatabaseHydratedRelationTargetIds } from "./databaseFilterSnapshot";

const NOTHING_READ = Object.freeze({ pagesById: {}, propertiesByDataSourceId: {}, dataSourceIds: [] as string[], hydratedIds: [] as string[] });

/**
 * What a relation's picker offers on a row page away from its database: its
 * database page's read, asked for when a picker first opens; a row page holds
 * only its own row. The databases its relations reach are read as the page
 * shows them, without their rows (loadSources): a page section offers "새
 * 페이지 추가하기" into its database however the page was opened, as Notion's
 * (seen 2026-11-14 at a row's own address).
 */
export function useStandaloneRelationRows(session: Session, containerPageId: string | undefined, workspaceId: string | undefined) {
  // Each read is the page's it was made for, another page's left unused.
  const [held, setHeld] = useState<{ pageId: string; graph: ContentGraph } | null>(null);
  const [heldSources, setHeldSources] = useState<{ pageId: string; graph: ContentGraph } | null>(null);
  const graph = held && held.pageId === containerPageId ? held.graph : null;
  const sources = heldSources && heldSources.pageId === containerPageId ? heldSources.graph : null;
  const reading = useRef<{ pageId: string; read: Promise<ContentGraph | null> } | null>(null);
  const sourcesStarted = useRef<string | null>(null);
  const graphNow = useRef(graph);
  graphNow.current = graph;
  // The whole read, once for the page, which what makes a row waits on.
  const read = useCallback(() => {
    if (!containerPageId) return Promise.resolve(null);
    if (reading.current?.pageId === containerPageId) return reading.current.read;
    const pageId = containerPageId;
    // Held before it starts: the read asks whether it is still wanted from its first window.
    const entry: { pageId: string; read: Promise<ContentGraph | null> } = { pageId, read: Promise.resolve(null) };
    reading.current = entry;
    const current = () => reading.current === entry;
    entry.read = collectAuthenticatedContentGraph(session, pageId, null, current).then((got) => {
      if (!got || !current()) return null;
      graphNow.current = got.graph;
      setHeld({ pageId, graph: got.graph });
      return got.graph;
    }, () => {
      if (current()) reading.current = null;
      return null;
    });
    return entry.read;
  }, [containerPageId, session]);
  const load = useCallback(() => { void read(); }, [read]);
  const loadSources = useCallback(() => {
    if (!containerPageId || sourcesStarted.current === containerPageId) return;
    const pageId = containerPageId;
    sourcesStarted.current = pageId;
    void collectContentGraphStructure(session, pageId, () => sourcesStarted.current === pageId).then(
      (got) => { if (got && sourcesStarted.current === pageId) setHeldSources({ pageId, graph: got.graph }); },
      () => { if (sourcesStarted.current === pageId) sourcesStarted.current = null; },
    );
  }, [containerPageId, session]);
  // Before the whole read, the databases alone, named as pages; their rows count as unread.
  const sourcePages = useMemo(() => !sources || !workspaceId ? null : Object.fromEntries(sources.dataSources.map((source, position): [string, Page] => [source.id,
    dataSourcePage(source, workspaceId, position)])), [sources, workspaceId]);
  const rows = useMemo(() => {
    if (!graph || !workspaceId) return sourcePages ? { ...NOTHING_READ, pagesById: sourcePages } : NOTHING_READ;
    const filter = nativeDatabaseFilterSnapshot({ currentUserId: session.user.id, graph, snapshotIdentity: `standalone-relations:${containerPageId}`, workspaceId });
    const sources = graph.dataSources.map((source, position) => dataSourcePage(source, workspaceId, position));
    return {
      pagesById: Object.fromEntries([
        ...Object.values(filter.pagesById).filter((page): page is Page => Boolean(page)), ...sources,
      ].map((page) => [page.id, page])) as Record<string, Page>,
      propertiesByDataSourceId: Object.fromEntries(Object.entries(filter.propertiesByDatabase).flatMap(([id, values]) => (
        values ? [[id, [...values]]] : []))) as Record<string, DbProperty[]>,
      dataSourceIds: graph.dataSources.map(({ id }) => id),
      hydratedIds: [...nativeDatabaseHydratedRelationTargetIds(graph)],
    };
  }, [containerPageId, graph, session.user.id, sourcePages, workspaceId]);
  // A relation's "새로 만들기" makes a row in the database it relates to, then
  // names it what was typed (PropertyRelationCell.tsx).
  const write = useCallback(async (row: GraphRow) => {
    const current = graphNow.current;
    const source = current?.dataSources.find(({ id }) => id === row.dataSourceId);
    if (!source) return false;
    // Kept for a server out of reach, it waits to be sent, as a change made in its database does.
    await sendContentGraphChange(session, source.pageId ?? containerPageId ?? "", [{ kind: "upsertRow", row }]);
    // The name typed follows at once, before this draws again.
    const next = graphNow.current && { ...graphNow.current, rows: [...graphNow.current.rows.filter(({ id }) => id !== row.id), row] };
    graphNow.current = next;
    if (next && containerPageId) setHeld({ pageId: containerPageId, graph: next });
    return true;
  }, [containerPageId, session]);
  // Made last among the rows at the top.
  const createRow = useCallback(async (dataSourceId: string): Promise<Page | null> => {
    const current = graphNow.current ?? await read();
    const title = current?.properties.find((property) => property.dataSourceId === dataSourceId && property.type === "title");
    if (!current || !title || !workspaceId) return null;
    const last = current.rows.filter((row) => row.dataSourceId === dataSourceId && !row.subitemParentId).at(-1);
    const given = { [title.id]: { type: "title", value: "" } } as GraphRow["values"];
    const row: GraphRow = {
      id: newId(), dataSourceId, subitemParentId: null, position: graphPositionBetween(last?.position, undefined),
      values: given,
    };
    if (!await write(row)) return null;
    return { id: row.id, workspaceId, parentType: "database", parentId: dataSourceId, kind: "page", title: "", iconType: "none", position: 0 };
  }, [read, workspaceId, write]);
  const renameRow = useCallback(async (rowId: string, title: string) => {
    const current = graphNow.current;
    const row = current?.rows.find(({ id }) => id === rowId);
    const titleId = row && current?.properties.find((property) => property.dataSourceId === row.dataSourceId && property.type === "title")?.id;
    if (row && titleId) await write({ ...row, values: { ...row.values, [titleId]: { type: "title", value: title } } });
  }, [write]);
  // A row just made, taken back: out of the read at once, then out of its database.
  const discardRow = useCallback(async (rowId: string) => {
    const current = graphNow.current;
    const row = current?.rows.find(({ id }) => id === rowId);
    const source = row && current?.dataSources.find(({ id }) => id === row.dataSourceId);
    if (!current || !source) return;
    const next = { ...current, rows: current.rows.filter(({ id }) => id !== rowId) };
    graphNow.current = next;
    if (containerPageId) setHeld({ pageId: containerPageId, graph: next });
    await sendContentGraphChange(session, source.pageId ?? containerPageId ?? "", [{ kind: "deleteRow", rowDeletion: { id: rowId } }]);
  }, [containerPageId, session]);
  return { ...rows, load, loadSources, createRow, renameRow, discardRow };
}
