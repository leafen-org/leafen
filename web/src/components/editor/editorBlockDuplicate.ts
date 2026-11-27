import type { Dispatch, SetStateAction } from "react";
import type { ContentGraph, GraphBlock, GraphOperation } from "@/contentGraph";
import type { EditorSelection, NativeEditorGraphAdapter } from "./editorRuntimeTypes";
import { copiesOnServer } from "./nativeEditorGraphAdapter";
import { blockItemText } from "./blockItemLabels";
import { requestEditorCopyIndicator } from "./nativeTiptapCopyIndicator";

// The latest duplicate on each page: its originals and the copies chosen.
const latest = new Map<string, { originals: string[]; copies: string[] }>();

/** The page's history a duplicate is one step of, as the editor's engine keeps it. */
export type EditorCopyHistory = { run(change: () => void): void; adoptServerBlocks(ids: readonly string[]): void };

/** Notes a duplicate's originals and copies, for the Undo and Redo that take the copies away and bring them back. */
export function noteDuplicate(pageId: string, originals: readonly string[], copies: readonly string[]) {
  latest.set(pageId, { originals: [...originals], copies: [...copies] });
}

/** The copies that are not inside another copy. */
function copyRoots(copies: readonly GraphBlock[]) {
  return copies.filter((copy) => !copies.some((other) => other.id === copy.parentId)).map((copy) => copy.id);
}

/**
 * Duplicates blocks as Notion does (seen 2026-10-31): without a notice, the
 * copies chosen; an Undo that takes the copies away chooses the originals
 * again, a Redo the copies (selectAfterHistoryStep). `history` makes it one
 * step of the page's history. Pages and databases are copied on the server,
 * their copies chosen once the page has them.
 */
export function duplicateChosenBlocks(
  adapter: NativeEditorGraphAdapter,
  ids: readonly string[],
  setSelection: Dispatch<SetStateAction<EditorSelection>>,
  history?: EditorCopyHistory,
  options?: { withoutContent?: boolean; fromCaret?: boolean },
): GraphBlock[] {
  if (copiesOnServer(adapter.graph, ids)) {
    const status = adapter.moveContext?.onStatus;
    // The step the copies make starts here, with the originals still chosen:
    // that is what its ⌘Z gives back once the server has taken the copies
    // away, the history's own step choosing them as it lands. Let go of first,
    // the step held nothing chosen and its ⌘Z left nothing so, the key's own
    // check (selectAfterHistoryStep) seeing the copies still there (2026-11-24:
    // a sub-page's ⌘D, then ⌘Z, left the focus on the page's body).
    const copying = adapter.copyOnServer(ids, { withoutContent: options?.withoutContent, oneStep: true });
    // The copy's place shows at once and goes, saying why, if it fails.
    setSelection(new Set());
    requestEditorCopyIndicator(ids.at(-1) ?? null);
    void copying.then((copies) => {
      if (!copies.length) { status?.(blockItemText("database.duplicateFailed"), "error"); return; }
      history?.adoptServerBlocks(copies);
      noteDuplicate(adapter.pageId, ids, copies);
    }).catch((error: unknown) => {
      if (!(error instanceof DOMException && error.name === "AbortError")) status?.(blockItemText("database.duplicateFailed"), "error");
    }).finally(() => requestEditorCopyIndicator(null));
    return [];
  }
  let copies: GraphBlock[] = [];
  const change = () => { copies = adapter.duplicate(ids, options); };
  if (history) history.run(change); else change();
  const roots = copyRoots(copies);
  if (roots.length > 0) {
    // Duplicated from a caret, an Undo gives the caret back and chooses nothing, as Notion's (seen 2026-11-05).
    noteDuplicate(adapter.pageId, options?.fromCaret ? [] : ids, roots);
    setSelection(new Set(roots));
  }
  return copies;
}

/**
 * Copies blocks where they were dropped (an ⌥-drag) as a duplicate is
 * made, as Notion's (seen 2026-11-11): one step of the page's history, the
 * copies chosen; the Undo that takes them away chooses the originals again,
 * a Redo the copies. None when they can't go there.
 */
export function copyBlocksTo(
  adapter: NativeEditorGraphAdapter,
  ids: readonly string[],
  place: (graph: ContentGraph, copyIds: readonly string[]) => { graph: ContentGraph; operations: GraphOperation[] } | null,
  setSelection: (selection: EditorSelection) => void,
  history?: EditorCopyHistory,
): GraphBlock[] {
  let copies: GraphBlock[] = [];
  const change = () => { copies = adapter.copyBlocks(ids, place); };
  if (history) history.run(change); else change();
  const roots = copyRoots(copies);
  if (roots.length > 0) {
    noteDuplicate(adapter.pageId, ids, roots);
    setSelection(new Set(roots));
  }
  return copies;
}

/**
 * Puts the copies the server made of dropped pages and databases where they
 * were dropped, once the page has them: one step of the page's history with
 * their making, chosen, as the copies of other blocks are (copyBlocksTo), so
 * one Undo takes them away, as Notion's (seen 2026-11-11: the Undo took back
 * the step before and left them). False when they can't go there.
 */
export function placeServerCopies(
  adapter: NativeEditorGraphAdapter,
  originals: readonly string[],
  copyIds: readonly string[],
  place: (graph: ContentGraph, copyIds: readonly string[]) => { graph: ContentGraph; operations: GraphOperation[] } | null,
  setSelection: (selection: EditorSelection) => void,
  history?: EditorCopyHistory,
): boolean {
  const done = { placed: false };
  const change = () => { done.placed = adapter.changeLayout((graph) => place(graph, copyIds)); };
  if (history) history.run(() => { history.adoptServerBlocks(copyIds); change(); });
  else change();
  if (!done.placed) return false;
  noteDuplicate(adapter.pageId, originals, copyIds);
  setSelection(new Set(copyIds));
  return true;
}

/**
 * Runs an Undo or Redo, choosing the originals or copies of the latest
 * duplicate it took away or brought back at the key. A step that waits for
 * the server — copies the server made (duplicateChosenBlocks) — lands later,
 * and chooses what its step held before or after as it does.
 */
export function selectAfterHistoryStep(
  adapter: () => NativeEditorGraphAdapter,
  setSelection: Dispatch<SetStateAction<EditorSelection>>,
  forward: boolean,
  step: () => void,
) {
  const pageId = adapter().pageId;
  const last = latest.get(pageId);
  const present = (id: string) => adapter().graph.blocks.some((block) => block.id === id);
  const before = last ? last.copies.every(present) : false;
  step();
  if (!last || latest.get(pageId) !== last) return;
  const after = last.copies.every(present);
  if (!forward && before && !after && last.originals.every(present)) setSelection(new Set(last.originals));
  if (forward && !before && after) setSelection(new Set(last.copies));
}
