import { createContext, useCallback, useContext, useEffect, useLayoutEffect, useReducer, useRef, useSyncExternalStore, type Dispatch, type SetStateAction } from "react";
import type { NodeViewProps } from "@tiptap/react";
import type { Node as PMNode } from "@tiptap/pm/model";
import type { GraphBlock } from "@/contentGraph";
import { besideReads, sameBesidePlace, sameReads, type NativeTiptapGraphIndex } from "./nativeTiptapGraphIndex";
import type { EditorSelection, NativeEditorGraphAdapter } from "./editorRuntimeTypes";
import type { createNativeTiptapController } from "./nativeTiptapController";
import type { NativeTiptapAdapterStore, NativeTiptapUiState, NativeTiptapUiStore } from "./nativeTiptapLiveState";

export type NativeTiptapReactValue = {
  /** The page's adapter as it is when read (nativeTiptapLiveState.ts). */
  adapter: NativeEditorGraphAdapter;
  /** The newest graph, which a view reads its block and neighbours from (useNativeTiptapNode). */
  latestGraph: NativeTiptapLatestGraph;
  controller: ReturnType<typeof createNativeTiptapController>;
  /** The selected blocks as they are when read; a view draws from useNativeTiptapUi. */
  selection: EditorSelection;
  setSelection: Dispatch<SetStateAction<EditorSelection>>;
  setColumnPreview: Dispatch<SetStateAction<ReadonlyMap<string, number>>>;
  staticReadOnlySemantics?: boolean;
  setActiveTabs: Dispatch<SetStateAction<ReadonlyMap<string, string>>>;
  ui: NativeTiptapUiStore;
};
export const NativeTiptapReactContext = createContext<NativeTiptapReactValue | null>(null);

/**
 * The page's adapter itself, new with each redraw of the page: for the views
 * that draw from all of it — databases, media, pages named — and the menus.
 */
export const NativeTiptapAdapterContext = createContext<NativeTiptapAdapterStore | null>(null);

const noAdapter = { subscribe: () => () => {}, get: () => null };

/** The page's adapter itself, drawn again with each new one; outside an editor, the one given. */
export function useNativeTiptapAdapter(fallback: NativeEditorGraphAdapter): NativeEditorGraphAdapter {
  const store = useContext(NativeTiptapAdapterContext) ?? noAdapter;
  return useSyncExternalStore(store.subscribe, store.get) ?? fallback;
}

const noUi = { subscribe: () => () => {}, get: () => undefined };

/** A part of the editor's selection, column drag or open tabs, drawn again only when it changes. */
export function useNativeTiptapUi<T>(select: (state: NativeTiptapUiState) => T, outside?: T): T {
  const ui = useContext(NativeTiptapReactContext)?.ui ?? noUi;
  return useSyncExternalStore(ui.subscribe, () => {
    const state = ui.get();
    return state ? select(state) : outside as T;
  });
}

export { nativeTiptapGraphIndex, type NativeTiptapGraphIndex } from "./nativeTiptapGraphIndex";

/**
 * The newest graph's lookup, kept while the page draws and told once it has
 * (tell): a view draws again at once when its own block changes or first
 * reaches the graph; what it shows of the blocks around it, past the frame
 * that shows the change (tellAround, Editor.tsx), and only the views that
 * show something else then draw again (besideReads). Every view drew again
 * in a long page's paste, its task 300 ms (2026-11-14, 600 blocks, CPU ×2),
 * and again past that frame, 23,600 fibers. A view is told of its own block
 * (subscribeBlock) only when it changed beside its place: telling every view
 * of a 1,031-block page to look took some 3.5 ms of an Enter (2026-11-15).
 */
export function createLatestGraph(initial: NativeTiptapGraphIndex) {
  let current = initial, told = initial;
  const listeners = new Set<() => void>(), around = new Set<() => void>();
  const byBlock = new Map<string, Set<() => void>>();
  const changedBlocks = (before: NativeTiptapGraphIndex, after: NativeTiptapGraphIndex) => {
    const changed: Array<Set<() => void>> = [];
    const note = (id: string) => { const views = byBlock.get(id); if (views) changed.push(views); };
    for (const block of after.blocks) {
      const last = before.byId.get(block.id);
      if (!last || last !== block && !sameBesidePlace(block, last)) note(block.id);
    }
    for (const block of before.blocks) if (!after.byId.has(block.id)) note(block.id);
    return changed;
  };
  return {
    get current() { return current; },
    keep(index: NativeTiptapGraphIndex) { current = index; },
    tell() {
      const before = told;
      told = current;
      for (const listener of [...listeners]) listener();
      if (before === current || !byBlock.size) return;
      for (const views of changedBlocks(before, current)) for (const listener of [...views]) listener();
    },
    subscribe(listener: () => void) {
      listeners.add(listener);
      return () => { listeners.delete(listener); };
    },
    subscribeBlock(id: string, listener: () => void) {
      const views = byBlock.get(id) ?? new Set<() => void>();
      views.add(listener); byBlock.set(id, views);
      return () => { views.delete(listener); if (!views.size && byBlock.get(id) === views) byBlock.delete(id); };
    },
    tellAround() { for (const listener of [...around]) listener(); },
    subscribeAround(listener: () => void) {
      around.add(listener);
      return () => { around.delete(listener); };
    },
  };
}

export type NativeTiptapLatestGraph = ReturnType<typeof createLatestGraph>;

// Where each node of a document sits: the block that owns it (itself, for a
// block) and how many blocks stand above it. Made in one walk once many node
// views of the same document ask, as a page opening draws them all; until
// then, and for a node that appears twice, a view asks the editor where it
// is. Each of a long page's node views asked, a walk along its siblings each
// time (2026-10-16); the few views an edit or a paste draws again walked the
// whole document for the second of them (2026-11-15: 0.7 ms of a paste's
// press in a 1,031-block page at CPU ×2).
type NodePlace = { owner: PMNode | null; depth: number } | "twice";
const documentPlaces = new WeakMap<PMNode, Map<PMNode, NodePlace> | number>();
// How many views of a document ask the editor before it is walked.
const ASKS_BEFORE_WALK = 32;

function placeOf(doc: PMNode, node: PMNode): NodePlace | undefined {
  const known = documentPlaces.get(doc) ?? 0;
  if (typeof known !== "number") return known.get(node);
  if (known < ASKS_BEFORE_WALK) { documentPlaces.set(doc, known + 1); return undefined; }
  const places = new Map<PMNode, NodePlace>();
  const walk = (parent: PMNode, ownerAbove: PMNode | null, blocksAbove: number) => parent.forEach((child) => {
    const block = child.type.name === "nativeBlock";
    places.set(child, places.has(child) ? "twice" : { owner: block ? child : ownerAbove, depth: blocksAbove });
    if (child.childCount) walk(child, block ? child : ownerAbove, blocksAbove + (block ? 1 : 0));
  });
  walk(doc, null, 0);
  documentPlaces.set(doc, places);
  return places.get(node);
}

/**
 * Where a node view's node sits in its editor's document now, or null while
 * the node is leaving it: a removed node's view can draw once more with its
 * old position, past the end of a shorter document (2026-10-25: undoing a
 * slash-made divider at a page's end showed the error screen). Node views
 * read their place only here.
 */
export function nodeViewPlace(props: Pick<NodeViewProps, "editor" | "getPos">) {
  const position = props.getPos();
  const doc = props.editor.state.doc;
  if (position === undefined || position < 0 || position > doc.content.size) return null;
  return doc.resolve(position);
}

/** Node views read the same native adapter as page menus and database widgets. */
export function useNativeTiptapNode(props: NodeViewProps) {
  const context = useContext(NativeTiptapReactContext);
  if (!context) throw new Error("Native node views require their page context.");
  const found = nodeOwner(props);
  const attrs = found?.owner?.attrs ?? {};
  const widgetId = props.node.type.name === "nativeWidget" ? props.node.attrs.blockId : null;
  const id: string | undefined = found ? widgetId || attrs.sourceId || attrs.id : undefined;
  // Its block as the newest graph has it, drawn again when what it draws of
  // it changes, not its place among its siblings (a line made or pasted
  // renumbers the places through the page, 2026-11-14): a write of its
  // content keeps the place the graph has (nativeEditorGraphAdapter.ts).
  const { latestGraph } = context;
  const drawn = useRef<GraphBlock | undefined>(undefined);
  const subscribe = useCallback((listener: () => void) => id ? latestGraph.subscribeBlock(id, listener) : () => {}, [id, latestGraph]);
  useSyncExternalStore(subscribe, () => {
    const now = id ? latestGraph.current.byId.get(id) : undefined, last = drawn.current;
    if (now && last && now !== last && sameBesidePlace(now, last)) return last;
    drawn.current = now;
    return now;
  });
  const graphIndex = latestGraph.current;
  useDrawnAround(latestGraph, graphIndex, id, Boolean(attrs.sourceId));
  if (!found) return { ...context, graphIndex, block: null, depth: 0, occurrenceId: "", attrs: {} };
  const { depth } = found;
  const known = id ? graphIndex.byId.get(id) : undefined;
  if (widgetId && !known) return { ...context, graphIndex, block: null, depth, occurrenceId: "", attrs };
  const block = known ?? (id ? {
    id, parentId: null, afterId: null, position: "", type: attrs.kind, content: attrs.props,
  } as GraphBlock : null);
  return { ...context, graphIndex, block, depth, occurrenceId: String(attrs.id ?? ""), attrs };
}

/**
 * Draws the caller again with each change of the page's graph past the
 * frame that shows it, while `active`: what reads the whole graph as it
 * draws, as a block's open menu (its counts, its blocks).
 */
export function useNativeTiptapGraphRedraw(active: boolean) {
  const latestGraph = useContext(NativeTiptapReactContext)?.latestGraph;
  const [, redraw] = useReducer((count: number) => count + 1, 0);
  useEffect(() => active && latestGraph ? latestGraph.subscribeAround(redraw) : undefined, [active, latestGraph]);
}

/**
 * Draws a view again past the frame of a change (tellAround, in a
 * transition a press cuts) when what it shows of the blocks around its own
 * changed (besideReads), from the graph it was drawn with to the newest.
 */
function useDrawnAround(latestGraph: NativeTiptapLatestGraph, shown: NativeTiptapGraphIndex, id: string | undefined, copied: boolean) {
  const [, redraw] = useReducer((count: number) => count + 1, 0);
  const drawnWith = useRef<{ index: NativeTiptapGraphIndex; reads: readonly unknown[] | null } | null>(null);
  useLayoutEffect(() => { drawnWith.current = { index: shown, reads: null }; });
  useEffect(() => latestGraph.subscribeAround(() => {
    const drawnNow = drawnWith.current, now = latestGraph.current;
    if (!drawnNow || drawnNow.index === now) return;
    const before = drawnNow.reads ?? besideReads(drawnNow.index, id, copied), after = besideReads(now, id, copied);
    if (sameReads(before, after)) drawnWith.current = { index: now, reads: after };
    else redraw();
  }), [copied, id, latestGraph]);
}

/** The block a node view's node belongs to and how many blocks stand above it; null while the node is leaving. */
function nodeOwner(props: NodeViewProps): { owner: PMNode | null; depth: number } | null {
  const place = placeOf(props.editor.state.doc, props.node);
  if (place && place !== "twice") return place;
  const resolved = nodeViewPlace(props);
  if (!resolved) return null;
  let owner = props.node.type.name === "nativeBlock" ? props.node : null;
  let depth = 0;
  for (let index = resolved.depth; index > 0; index--) {
    const node = resolved.node(index);
    if (node.type.name === "nativeBlock") { owner ??= node; depth++; }
  }
  return { owner, depth };
}
