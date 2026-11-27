import type { EditorView } from "@tiptap/pm/view";
type SelectionRoot = Node & { getSelection?: () => Selection | null };
type Snapshot = { selection: Selection; anchorNode: Node; anchorOffset: number; focusNode: Node; focusOffset: number };
const snapshots = new WeakMap<Node, { value: Snapshot | null }>();

/** One DOM selection read for a synchronous React node-view commit. Tiptap
 * still moves its contentDOM and restores the saved endpoints itself. Each
 * engine transaction/focus change invalidates this short-lived read cache. */
export function nativeDomSelectionSnapshot(element: HTMLElement): Snapshot | null {
  // Content not yet in the page holds no selection, as Tiptap would find;
  // each such content is its own root, so it would read the selection anew
  // (2026-10-16: a long page's node views read it thousands of times as they
  // came in).
  if (!element.isConnected) return null;
  const root = element.getRootNode() as SelectionRoot & Partial<DocumentOrShadowRoot>;
  // Nor does content outside the editor in focus: reading the selection lays
  // the page out, so a page opening laid out what it had drawn so far again
  // at each node view it drew (2026-11-13: a page of three databases, 120 ms
  // at the first). An editor out of focus keeps no caret here to put back;
  // ProseMirror puts its selection back itself as it takes the focus.
  const active = root.activeElement;
  if (!active || active === element.ownerDocument.body || !active.contains(element)) return null;
  const cached = snapshots.get(root);
  if (cached) return cached.value;
  const selection = typeof root.getSelection === "function" ? root.getSelection() : element.ownerDocument.defaultView?.getSelection();
  const { anchorNode, anchorOffset, focusNode, focusOffset } = selection ?? {};
  const entry = { value: selection && anchorNode && focusNode
    ? { selection, anchorNode, anchorOffset: anchorOffset!, focusNode, focusOffset: focusOffset! } : null };
  snapshots.set(root, entry);
  queueMicrotask(() => { if (snapshots.get(root) === entry) snapshots.delete(root); });
  return entry.value;
}

const restoring = new Set<Snapshot>();

/**
 * Puts a selection a node view's contentDOM took with it back once, after
 * the React commit that moved it: a new line's block and its text each move
 * their contentDOM in, and each put the caret back at once, laying out the
 * page twice in an Enter's commit (2026-11-14: a 600-block page, 18 ms of
 * it at CPU ×2). The engine reads the browser's selection only after that
 * (its DOM observer puts its own back first: nativeTiptapDomRead's
 * keepsSelection), so then it is not set again.
 */
export function restoreNativeDomSelection(saved: Snapshot) {
  if (restoring.has(saved)) return;
  restoring.add(saved);
  queueMicrotask(() => {
    restoring.delete(saved);
    const { selection } = saved;
    if (selection.anchorNode === saved.anchorNode && selection.anchorOffset === saved.anchorOffset
      && selection.focusNode === saved.focusNode && selection.focusOffset === saved.focusOffset) return;
    try { selection.setBaseAndExtent(saved.anchorNode, saved.anchorOffset, saved.focusNode, saved.focusOffset); } catch { /* gone from the page */ }
  });
}

export function invalidateNativeDomSelection(root: Node) { snapshots.delete(root); }

/**
 * Takes the selection the browser shows into its editor now. A key moves the
 * caret or the chosen words at once but says so later, in a selectionchange:
 * a key on its heels acts where the editor last heard it was (2026-11-05:
 * Enter just after a click split the page's first line; 2026-11-09: ⌘K just
 * after three ⇧← linked a database cell's last letter alone).
 */
export function takeDomSelection(view: EditorView) {
  (view as unknown as { domObserver: { flush(): void } }).domObserver.flush();
}
