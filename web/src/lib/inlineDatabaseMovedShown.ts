import { useSyncExternalStore } from "react";
import { emptyContentGraph, type ContentGraph, type GraphBlock } from "@/contentGraph";
import { inlineDatabaseAddressPage, inlineDatabaseAtAddress, rememberInlineDatabaseAddress } from "@/lib/inlineDatabaseAddress";
import { inlineDatabaseDrawable } from "@/lib/inlineDatabaseFullPage";
import { pageHref } from "@/lib/navigation";
import type { PageGraphStore } from "@/lib/pageGraphStore";
import { pushRoute } from "@/lib/router";
import { dropPageTextAhead, readPageTextAhead } from "@/lib/pageTextAhead";

/** The block and every block inside it, its containers, sources, views, properties and rows, as a graph holds them. */
function databaseParts(graph: ContentGraph, blockId: string) {
  const ids = new Set([blockId]);
  for (let grew = true; grew;) {
    grew = false;
    for (const block of graph.blocks) if (block.parentId && ids.has(block.parentId) && !ids.has(block.id)) { ids.add(block.id); grew = true; }
  }
  const blocks = graph.blocks.filter((block) => ids.has(block.id));
  if (!blocks.some((block) => block.id === blockId)) return null;
  const containers = graph.databaseContainers.filter((container) => ids.has(container.blockId));
  const sources = new Set([
    ...blocks.flatMap((block) => [block.content.dataSourceId, ...(Array.isArray(block.content.additionalDataSourceIds) ? block.content.additionalDataSourceIds : [])]
      .filter((id): id is string => typeof id === "string" && id !== "")),
    ...containers.flatMap((container) => container.dataSourceIds),
  ]);
  // Its own views, and its sources' views, which no block holds: those it
  // shows of a source of its own (InlineDatabaseBlock's tabs).
  const views = graph.views.filter((view) => view.containerId ? ids.has(view.containerId) : sources.has(view.dataSourceId));
  for (const view of views) sources.add(view.dataSourceId);
  return {
    blocks, containers, views,
    dataSources: graph.dataSources.filter((source) => sources.has(source.id)),
    properties: graph.properties.filter((property) => sources.has(property.dataSourceId)),
    rows: graph.rows.filter((row) => sources.has(row.dataSourceId)),
  };
}

type Parts = NonNullable<ReturnType<typeof databaseParts>>;

/** A graph with the database's parts it lacks, the database on its own lines where its block's parent isn't there. */
function withParts(graph: ContentGraph, parts: Parts, holderId: string): ContentGraph {
  const add = <T extends { id: string }>(list: readonly T[], more: readonly T[]) => {
    const held = new Set(list.map(({ id }) => id));
    const missing = more.filter(({ id }) => !held.has(id));
    return missing.length ? [...list, ...missing] : list as T[];
  };
  const present = new Set([...graph.blocks, ...parts.blocks].map(({ id }) => id));
  const blocks: GraphBlock[] = parts.blocks.map((block) =>
    block.parentId === null || present.has(block.parentId) ? block : { ...block, parentId: null, afterId: null });
  return {
    ...graph,
    blocks: add(graph.blocks, blocks),
    databaseContainers: add(graph.databaseContainers, parts.containers.map((container) => ({ ...container, pageId: holderId }))),
    dataSources: add(graph.dataSources, parts.dataSources),
    views: add(graph.views, parts.views),
    properties: add(graph.properties, parts.properties),
    rows: add(graph.rows, parts.rows),
  };
}

// How each database shown where it went stops being held there.
const holding = new Map<string, () => void>();

// The page each database opened alone is shown in while the server moves it there, by its block.
const shownIn = new Map<string, string>();
const shownListeners = new Set<() => void>();
let shownVersion = 0;

function setShownIn(blockId: string, pageId: string | null) {
  if (pageId) shownIn.set(blockId, pageId);
  else shownIn.delete(blockId);
  shownVersion += 1;
  for (const listener of [...shownListeners]) listener();
}

function subscribeShownIn(listener: () => void) {
  shownListeners.add(listener);
  return () => { shownListeners.delete(listener); };
}

/** The page an inline database opened alone is shown in while it moves there, whose path is drawn above it. */
export function useInlineDatabaseShownIn(blockId: string | undefined): string | undefined {
  useSyncExternalStore(subscribeShownIn, () => shownVersion, () => shownVersion);
  return blockId ? shownIn.get(blockId) : undefined;
}

/**
 * An inline database opened alone, moved to another page or back by its
 * notice's "실행 취소", shown where it goes at the press (user decision
 * 2026-11-12: a press answers on the next frame — the path above it named
 * the page it left until the server had moved it, 0.3–1.9 s on a NAS-class
 * server, and then the page it went to drew whole until that page was read
 * with it): the path names that page at once while the database stays drawn
 * as it is — the page it is on reads it until the server has moved it — and
 * once moved its address opens it there, drawn as the press found it in
 * whatever graph the open page takes until that page holds it by itself,
 * that page's text read ahead meanwhile so its body draws it in the frame the
 * page changes (lib/pageTextAhead.ts). Refused, the path names the page it
 * stayed on.
 */
export function showInlineDatabaseMovedTo(store: PageGraphStore | null, blockId: string, holderId: string, principalId?: string) {
  const parts = store ? databaseParts(store.get(), blockId) : null;
  // The last graph it was put in, and the one drawn as the server had moved
  // it: the page it left, still drawing it.
  let settled: ContentGraph | null = null, ours: ContentGraph | null = null, stop = () => {};
  // A graph the open page took since that holds it by itself.
  const holdsIt = (now: ContentGraph) => now !== ours && now !== settled && inlineDatabaseDrawable(now, blockId);
  // Whatever of it a graph lacks — all of it, or its sources and views while
  // the server has moved its block alone — it is drawn with as it was.
  const hold = () => {
    const now = store!.get();
    if (inlineDatabaseAtAddress(window.location.pathname) !== blockId) { stop(); return; }
    if (now === ours) return;
    if (settled && holdsIt(now)) { stop(); return; }
    store!.set(withParts(now, parts!, inlineDatabaseAddressPage(blockId) ?? holderId));
    if (store!.get() !== now) ours = store!.get();
  };
  if (store && parts) {
    const unsubscribe = store.subscribe(hold);
    const release = () => {
      stop = () => {};
      unsubscribe();
      if (holding.get(blockId) === release) holding.delete(blockId);
    };
    stop = release;
    // A move of it pressed again takes over from the one before.
    holding.get(blockId)?.();
    holding.set(blockId, release);
  }
  setShownIn(blockId, holderId);
  const textRead = principalId ? readPageTextAhead(principalId, holderId) : Promise.resolve();
  const done = () => { if (shownIn.get(blockId) === holderId) setShownIn(blockId, null); };
  let moving = false;
  return {
    /** The server has moved it: its address opens it there, once its page's text is read. */
    moved: () => {
      if (moving || shownIn.get(blockId) !== holderId) return;
      moving = true;
      void textRead.then(() => {
        if (shownIn.get(blockId) !== holderId) return;
        settled = store?.get() ?? emptyContentGraph;
        rememberInlineDatabaseAddress(blockId, holderId, true);
        pushRoute(`${pageHref(blockId)}${window.location.search}${window.location.hash}`);
        done();
      });
    },
    /** The server didn't: it stays where it was. */
    refused: () => { stop(); done(); dropPageTextAhead(holderId); },
  };
}
