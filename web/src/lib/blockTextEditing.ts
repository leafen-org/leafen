import * as Y from "yjs";

// Block text lives in the page's collaborative document, one Y.Text per
// block. Splitting, joining and line breaks move the text with its marks in
// one transaction of this editor's own origin, which its undo tracks.

export const BLOCK_TEXT_ROOT_PREFIX = "block:";
/** The origin of this editor's own text changes. */
export const localEditorOrigin = Symbol("leafen-local-editor");

const undoManagers = new WeakMap<Y.Doc, Y.UndoManager>();

function blockText(document: Y.Doc, blockId: string) {
  return document.getText(BLOCK_TEXT_ROOT_PREFIX + blockId);
}

/** Runs a structural text change as its own undo step, apart from typing. */
function asUndoStep(document: Y.Doc, change: () => void) {
  const undo = undoManagers.get(document);
  undo?.stopCapturing();
  document.transact(change, localEditorOrigin);
  undo?.stopCapturing();
}

/** Replaces a range of a block's text, taking the marks before it. */
export function replaceBlockTextRange(document: Y.Doc, blockId: string, start: number, end: number, value: string) {
  const text = blockText(document, blockId);
  asUndoStep(document, () => {
    if (end > start) text.delete(start, end - start);
    if (value) text.insert(start, value);
  });
}

/** Removes a group's text as one local undo step, alongside its structure. */
export function removeBlockTexts(document: Y.Doc, blockIds: readonly string[]) {
  const texts = blockIds.map((id) => blockText(document, id)).filter((text) => text.length > 0);
  if (texts.length === 0) return;
  asUndoStep(document, () => {
    for (const text of texts) text.delete(0, text.length);
  });
}

/** How close one typed change follows another to go back with it, one Undo. */
export const TEXT_UNDO_CAPTURE_MS = 500;

/** The undo of this editor's own text changes in a page document. */
export function blockTextUndoManager(document: Y.Doc) {
  let manager = undoManagers.get(document);
  if (!manager) {
    manager = new Y.UndoManager(document, {
      trackedOrigins: new Set([localEditorOrigin]), captureTimeout: TEXT_UNDO_CAPTURE_MS,
      captureTransaction: transaction => transaction.changedParentTypes.size > 0,
    });
    undoManagers.set(document, manager);
  }
  return manager;
}
