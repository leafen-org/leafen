import { useSyncExternalStore } from "react";
import type { Editor } from "@tiptap/core";
import type { NodeViewProps } from "@tiptap/react";
import type { Node as PMNode } from "@tiptap/pm/model";

type Holders = { nodes: ReadonlySet<PMNode>; subscribe(listener: () => void): () => void };

const NONE: ReadonlySet<PMNode> = new Set();
const holdersByEditor = new WeakMap<Editor, Holders>();

/**
 * The nodes whose content holds the whole selection while the editor has
 * the focus, read once for the editor as the selection, the focus or the
 * document changes. Each view asking where it stands read its own place at
 * each change, a walk along the blocks before it: an Enter in a page of 120
 * pictures had the 120 captions do it, 3.6 ms at CPU ×2 (2026-11-15).
 */
function holdersOf(editor: Editor): Holders {
  const known = holdersByEditor.get(editor);
  if (known) return known;
  const listeners = new Set<() => void>();
  let nodes = NONE;
  const read = () => {
    let next = NONE;
    if (editor.isFocused && !editor.isDestroyed) {
      const { $from, to } = editor.state.selection;
      const shared = $from.sharedDepth(to);
      if (shared > 0) {
        const held = new Set<PMNode>();
        for (let depth = shared; depth > 0; depth--) held.add($from.node(depth));
        next = held;
      }
    }
    if (next.size === nodes.size && [...next].every((node) => nodes.has(node))) return;
    nodes = next;
    for (const listener of [...listeners]) listener();
  };
  const holders: Holders = {
    get nodes() { return nodes; },
    subscribe(listener) {
      if (!listeners.size) {
        editor.on("selectionUpdate", read); editor.on("update", read); editor.on("focus", read); editor.on("blur", read);
        read();
      }
      listeners.add(listener);
      return () => {
        listeners.delete(listener);
        if (listeners.size) return;
        editor.off("selectionUpdate", read); editor.off("update", read); editor.off("focus", read); editor.off("blur", read);
        nodes = NONE;
      };
    },
  };
  holdersByEditor.set(editor, holders);
  return holders;
}

const nothing = () => () => {};

/**
 * Whether the editor has focus with its caret or selection inside this node
 * view's node. React draws a view again only when its answer changes: every
 * line holds a view with this, and asked on each key, even with the same
 * answer, each drawn key left an update behind it, so a fast typist's 50th
 * key in a row stopped the page with React's "maximum update depth"
 * (2026-11-09).
 */
export function useCaretInsideNode(props: NodeViewProps, enabled: boolean) {
  const holders = enabled ? holdersOf(props.editor) : null;
  return useSyncExternalStore(holders ? holders.subscribe : nothing, () => !!holders && holders.nodes.has(props.node));
}
