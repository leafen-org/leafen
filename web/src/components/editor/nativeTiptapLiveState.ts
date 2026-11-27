import type { SetStateAction } from "react";
import type { EditorSelection, NativeEditorGraphAdapter } from "./editorRuntimeTypes";

// What a page's node views draw from beyond their own node, kept so that
// each view draws again for what it shows, not for every change around it
// (2026-10-18: selecting one block of a 3,000-block page drew all 6,000
// node views again, 0.2 s, and so did every redraw of the page holding the
// editor, which made a new adapter each time).

/** Told once a long page's opening node views are all drawn (NativeEditorContent.tsx). */
export const NODE_VIEWS_DRAWN_EVENT = "leafen:node-views-drawn";

export type NativeTiptapUiState = {
  selection: EditorSelection;
  columnPreview: ReadonlyMap<string, number>;
  activeTabs: ReadonlyMap<string, string>;
};

/** The editor's selected blocks, column widths being dragged and open tabs. */
export function createNativeTiptapUiStore() {
  let state: NativeTiptapUiState = { selection: new Set(), columnPreview: new Map(), activeTabs: new Map() };
  const listeners = new Set<() => void>();
  return {
    get: () => state,
    subscribe(listener: () => void) {
      listeners.add(listener);
      return () => { listeners.delete(listener); };
    },
    set<K extends keyof NativeTiptapUiState>(key: K, next: SetStateAction<NativeTiptapUiState[K]>) {
      const current = state[key];
      const value = typeof next === "function" ? (next as (value: NativeTiptapUiState[K]) => NativeTiptapUiState[K])(current) : next;
      if (Object.is(value, current)) return;
      state = { ...state, [key]: value };
      for (const listener of [...listeners]) listener();
    },
  };
}

export type NativeTiptapUiStore = ReturnType<typeof createNativeTiptapUiStore>;

/**
 * The selection as it is when asked: what a block's press or menu reads,
 * while the block draws only whether it is selected.
 */
export function liveSelection(store: NativeTiptapUiStore): EditorSelection {
  const now = () => store.get().selection;
  return {
    get size() { return now().size; },
    has: (id) => now().has(id),
    forEach: (each, thisArg) => now().forEach((value, key) => each.call(thisArg, value, key, now())),
    entries: () => now().entries(),
    keys: () => now().keys(),
    values: () => now().values(),
    [Symbol.iterator]: () => now()[Symbol.iterator](),
  };
}

/**
 * The page's adapter as it is when read: node views keep one, so the page
 * making a new adapter on each of its redraws doesn't draw them again, and
 * whatever they call or read reaches the current one. `own` stands in for
 * some of its members (the editor's lease on whether it is current).
 */
export function liveAdapter(latest: { readonly current: NativeEditorGraphAdapter },
  own: Partial<NativeEditorGraphAdapter> = {}): NativeEditorGraphAdapter {
  return new Proxy({} as NativeEditorGraphAdapter, {
    get: (_target, key) => Object.hasOwn(own, key) ? Reflect.get(own, key) : Reflect.get(latest.current, key),
    has: (_target, key) => Object.hasOwn(own, key) || Reflect.has(latest.current, key),
    // Spread or listed as the current one is, too (2026-11-06: full screen's
    // "{ ...adapter, readOnly: true }" took nothing else, so a linked PDF there
    // had no session to be read with).
    ownKeys: () => Reflect.ownKeys(latest.current),
    getOwnPropertyDescriptor: (_target, key) => {
      const found = Reflect.getOwnPropertyDescriptor(Object.hasOwn(own, key) ? own : latest.current, key);
      return found && { ...found, configurable: true };
    },
  });
}

/**
 * The page's adapter for the views that draw from all of it, told to them
 * by subscription: a context changing with each redraw of the page made
 * React look through all of a long page's views for the few reading it.
 */
export function createAdapterStore(initial: NativeEditorGraphAdapter) {
  let current = initial;
  const listeners = new Set<() => void>();
  return {
    get: () => current,
    /** Kept while the page draws; told once it has (tell). */
    keep(adapter: NativeEditorGraphAdapter) { current = adapter; },
    tell() { for (const listener of [...listeners]) listener(); },
    subscribe(listener: () => void) {
      listeners.add(listener);
      return () => { listeners.delete(listener); };
    },
  };
}

export type NativeTiptapAdapterStore = ReturnType<typeof createAdapterStore>;

/**
 * What node views draw from the adapter: when one of these changes, they
 * draw again. The graph counts by its blocks, which a reread of the page
 * keeps when they are the same. The mention list is not among them (the
 * blocks naming pages read it themselves), nor the graph's syncing, made
 * anew with each redraw of the page and only called.
 */
export function adapterDrawnFields(adapter: NativeEditorGraphAdapter): readonly unknown[] {
  return [...adapterDrawnFieldsBesideGraph(adapter), adapter.graph.blocks];
}

/**
 * The same but the graph, of which an editor's views read their own blocks
 * and those around them (useNativeTiptapNode). Of the mention list, whether
 * there is one and whose it is: a to-do's "@" hint and its people's buttons.
 */
export function adapterDrawnFieldsBesideGraph(adapter: NativeEditorGraphAdapter): readonly unknown[] {
  return [adapter.pageId, adapter.workspaceId, adapter.session, adapter.generation, adapter.signal,
    adapter.readOnly, adapter.fixedBody, adapter.collaborationDocument, adapter.pagePlaceholderBlockId, Boolean(adapter.moveContext),
    adapter.mentionSnapshot?.currentUserId ?? null];
}
