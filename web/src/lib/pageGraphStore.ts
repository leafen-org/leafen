import { createContext, useCallback, useContext, useLayoutEffect, useRef, useState, useSyncExternalStore, type Dispatch, type SetStateAction } from "react";
import { emptyContentGraph, type ContentGraph, type GraphBlock } from "@/contentGraph";
import { keepUnchangedGraph } from "@/lib/keptGraph";

/**
 * The open page's graph, held apart from the app around it: what reads it
 * is drawn again as it changes, the app only when what the app reads of it
 * changes (usePageGraphFrame). A table cell is written to the graph as it is
 * typed, and the graph held in the app's own state drew the whole app again
 * from its root for each character (2026-11-12: about 28 ms a character on
 * a table of 30 rows by 40 properties).
 */
export type PageGraphStore = {
  /** The graph as it is now, what it holds unchanged kept (lib/keptGraph.ts). */
  get(): ContentGraph;
  /**
   * The graph as the page's body last drew it, which a write made now is
   * made against, as the graph the app drew was before it moved here: a
   * change shown ahead of the write that sends it (a database made with its
   * first view) is not yet the server's, and its view must go out as made,
   * not as a change of a view the server holds (2026-11-12: a new inline
   * database was refused as a view changed elsewhere). The store's own while
   * nothing draws it.
   */
  drawn(): ContentGraph;
  set: Dispatch<SetStateAction<ContentGraph>>;
  /** Hears each change; returns how to stop. */
  subscribe(listener: () => void): () => void;
  /** What a drawing of the graph drew (usePageGraph). */
  noteDrawn(graph: ContentGraph): void;
  /** A drawing of the graph mounted; returns how to unmount it (usePageGraph). */
  addDrawing(): () => void;
  /** Told by a drawing once it drew a new graph, before it is painted (usePageGraph). */
  tellDrawn(): void;
  /** Hears each drawing of a new graph; returns how to stop. */
  subscribeDrawn(listener: () => void): () => void;
};

export function createPageGraphStore(): PageGraphStore {
  let graph = emptyContentGraph;
  let drawnGraph: ContentGraph | null = null;
  let drawings = 0;
  const listeners = new Set<() => void>(), drawnListeners = new Set<() => void>();
  return {
    get: () => graph,
    drawn: () => drawings > 0 && drawnGraph ? drawnGraph : graph,
    noteDrawn: (shown) => { drawnGraph = shown; },
    addDrawing: () => {
      drawings += 1;
      return () => { drawings -= 1; };
    },
    set: (next) => {
      const kept = keepUnchangedGraph(graph, typeof next === "function" ? next(graph) : next);
      if (kept === graph) return;
      graph = kept;
      for (const listener of [...listeners]) listener();
    },
    subscribe: (listener) => {
      listeners.add(listener);
      return () => { listeners.delete(listener); };
    },
    tellDrawn: () => { for (const listener of [...drawnListeners]) listener(); },
    subscribeDrawn: (listener) => {
      drawnListeners.add(listener);
      return () => { drawnListeners.delete(listener); };
    },
  };
}

/** A store of the component's own, the same one for as long as it is drawn. */
export function usePageGraphStore(): PageGraphStore {
  const [store] = useState(createPageGraphStore);
  return store;
}

/**
 * The open page's graph store, for what draws a piece of it
 * (usePageGraphSelect) and for a press anywhere in the signed-in app — the
 * sidebar, a favorite, a peek beside the page — that shows its change on
 * the open page at once (lib/rowsShownAtPress.ts): a row moved from the
 * sidebar left the open page's table only once the server had moved it
 * (2026-11-13, NAS condition: 988 ms). It holds whichever page is open, so
 * what changes it from beside the page changes only what the page holds.
 * None outside the signed-in app.
 */
export const PageGraphStoreContext = createContext<PageGraphStore | null>(null);

/** The open page's graph store, or none outside the signed-in app. */
export function useOpenPageGraphStore(): PageGraphStore | null {
  return useContext(PageGraphStoreContext);
}

const noStore = { get: () => emptyContentGraph, subscribe: () => () => {} };

/**
 * A piece of the open page's graph — a row, a cell's value, a view's
 * settings, a block — drawn again only when that piece changes: a press
 * that changes one value draws that value's cells, not the page holding
 * them (user decision 2026-11-12). `select` reads the piece from the graph;
 * `same` tells two readings alike (Object.is by default, which a graph read
 * again keeps for what didn't change: lib/keptGraph.ts). Without a store
 * (outside a page) it reads the empty graph.
 */
export function usePageGraphSelect<T>(
  store: PageGraphStore | null,
  select: (graph: ContentGraph) => T,
  same: (shown: T, next: T) => boolean = Object.is,
): T {
  const source = store ?? noStore;
  // The last reading, kept while the graph and the reader stay, and kept
  // as it was while a new one is alike.
  const kept = useRef<{ graph: ContentGraph; select: (graph: ContentGraph) => T; value: T } | null>(null);
  const read = useCallback(() => {
    const graph = source.get();
    const last = kept.current;
    if (last && last.graph === graph && last.select === select) return last.value;
    const next = select(graph);
    const value = last && same(last.value, next) ? last.value : next;
    kept.current = { graph, select, value };
    return value;
  }, [source, select, same]);
  return useSyncExternalStore(source.subscribe, read, read);
}

/** The whole graph, drawn again at each change; what it draws is the store's `drawn`. */
export function usePageGraph(store: PageGraphStore): ContentGraph {
  const graph = useGraphShown(store, alwaysChanged);
  store.noteDrawn(graph);
  useLayoutEffect(() => store.addDrawing(), [store]);
  useLayoutEffect(() => store.tellDrawn(), [graph, store]);
  return graph;
}

/**
 * The graph as the app around the page body reads it: a new one once its
 * sources, views (but their rows' order, frameViewsChanged) or properties
 * change, or the blocks it reads change (frameBlocksChanged), not when its
 * rows or the page's other blocks change.
 * Only the body draws those — its databases from the store (WithPageGraph) —
 * and what reads them otherwise reads them from the store as it needs them
 * (`get`, `drawn`): a line made on a long page drew the whole app again
 * inside the press (2026-11-14, about 220 components, CPU ×2), and so did a
 * row made, copied, put in the trash or moved (2026-11-14, 188 components).
 */
export function usePageGraphFrame(store: PageGraphStore): ContentGraph {
  return useGraphShown(store, frameChanged);
}

const alwaysChanged = () => true;

function frameChanged(shown: ContentGraph, next: ContentGraph) {
  if (shown.databaseContainers !== next.databaseContainers || shown.dataSources !== next.dataSources
    || shown.properties !== next.properties) return true;
  if (shown.views !== next.views && frameViewsChanged(shown.views, next.views)) return true;
  return shown.blocks !== next.blocks && frameBlocksChanged(shown.blocks, next.blocks);
}

/**
 * Whether the views changed past their rows' order in a view keeping its own
 * (viewRowOrder.ts), which a row made, copied or moved there writes and only
 * the body's views read.
 */
function frameViewsChanged(shown: ContentGraph["views"], next: ContentGraph["views"]) {
  if (shown.length !== next.length) return true;
  return next.some((view, index) => {
    const was = shown[index];
    if (was === view) return false;
    if (was.id !== view.id || was.dataSourceId !== view.dataSourceId || was.containerId !== view.containerId || was.type !== view.type
      || was.name !== view.name || was.position !== view.position) return true;
    // A setting left undefined is one not set: a view written from what a
    // table shows carries its toolbar's search so when there is none, and
    // counted as a setting, a row made in a table keeping its own order
    // drew the whole app again (2026-11-15, 193 components, CPU ×2).
    const { rowOrder: _was, ...wasConfig } = was.config, { rowOrder: _is, ...isConfig } = view.config;
    for (const key in isConfig) if (wasConfig[key] !== isConfig[key]) return true;
    for (const key in wasConfig) if (!(key in isConfig) && wasConfig[key] !== undefined) return true;
    return false;
  });
}

// Held as the component's state, so a change is drawn together with the
// other state its writer sets alongside it (the page read, its access, the
// page list), as the graph was while the app held it.
function useGraphShown(store: PageGraphStore, changed: (shown: ContentGraph, next: ContentGraph) => boolean): ContentGraph {
  const [shown, setShown] = useState(store.get);
  const latest = useRef(shown);
  latest.current = shown;
  useLayoutEffect(() => {
    const hear = () => {
      const next = store.get();
      if (next === latest.current || !changed(latest.current, next)) return;
      latest.current = next;
      setShown(next);
    };
    hear();
    return store.subscribe(hear);
  }, [changed, store]);
  return shown;
}

/**
 * Whether the blocks the app around the body reads changed: whether there
 * are any, and its databases (a wiki's, one opened alone, a database
 * page's own), in their order, by what they hold, not by their places,
 * which a line made renumbers through the page.
 */
function frameBlocksChanged(shown: readonly GraphBlock[], next: readonly GraphBlock[]) {
  if ((shown.length > 0) !== (next.length > 0)) return true;
  let before = 0, after = 0;
  for (;;) {
    while (before < shown.length && shown[before].type !== "inline_database") before += 1;
    while (after < next.length && next[after].type !== "inline_database") after += 1;
    if (before === shown.length || after === next.length) return before !== shown.length || after !== next.length;
    const was = shown[before], is = next[after];
    if (was !== is && !(was.id === is.id && was.parentId === is.parentId && was.content === is.content)) return true;
    before += 1; after += 1;
  }
}
