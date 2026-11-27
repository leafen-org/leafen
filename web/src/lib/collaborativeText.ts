import { useCallback, useSyncExternalStore } from "react";
import * as Y from "yjs";
import { writeRichTextToYText } from "./collaborationCrdtCodec";
import { BLOCK_TEXT_ROOT_PREFIX, localEditorOrigin } from "./blockTextEditing";
import type { TextSpan } from "./types";

// Writes to a page's shared text and the hook that reads a block's back; the
// page's room that carries them is collaboration.ts.

/** Sets a block's text to plain text, changing only the part that differs. */
export function replaceText(document: Y.Doc, blockId: string, nextText: string): void {
  const sharedText = document.getText(BLOCK_TEXT_ROOT_PREFIX + blockId);
  const currentText = sharedText.toString();
  if (currentText === nextText) return;

  let prefix = 0;
  while (prefix < currentText.length && prefix < nextText.length && currentText[prefix] === nextText[prefix]) prefix += 1;
  let suffix = 0;
  while (
    suffix < currentText.length - prefix
    && suffix < nextText.length - prefix
    && currentText[currentText.length - suffix - 1] === nextText[nextText.length - suffix - 1]
  ) suffix += 1;

  document.transact(() => {
    const deleteLength = currentText.length - prefix - suffix;
    if (deleteLength > 0) sharedText.delete(prefix, deleteLength);
    const inserted = nextText.slice(prefix, nextText.length - suffix);
    if (inserted) sharedText.insert(prefix, inserted);
  }, localEditorOrigin);
}

/** Sets a block's text to formatted spans, changing only what differs. */
export function setBlockRichText(document: Y.Doc, blockId: string, spans: TextSpan[]) {
  document.transact(() => writeRichTextToYText(document.getText(BLOCK_TEXT_ROOT_PREFIX + blockId), spans), localEditorOrigin);
}

/** Gives each copied block the text of the block it copies, marks and mentions included, as one change. */
export function copyBlockTexts(document: Y.Doc, copies: readonly { sourceId: string; copyId: string }[]) {
  if (copies.length === 0) return;
  document.transact(() => {
    for (const { sourceId, copyId } of copies) {
      const copy = document.getText(BLOCK_TEXT_ROOT_PREFIX + copyId);
      if (copy.length === 0) copy.applyDelta(document.getText(BLOCK_TEXT_ROOT_PREFIX + sourceId).toDelta());
    }
  }, localEditorOrigin);
}

/** A block's plain text, drawn again as it changes. */
export function useCollaborativeText(document: Y.Doc | null, blockId: string): string {
  const sharedText = document?.getText(BLOCK_TEXT_ROOT_PREFIX + blockId) ?? null;
  const subscribe = useCallback((notify: () => void) => {
    if (!sharedText) return () => undefined;
    sharedText.observe(notify);
    return () => sharedText.unobserve(notify);
  }, [sharedText]);
  const snapshot = useCallback(() => sharedText?.toString() ?? "", [sharedText]);
  return useSyncExternalStore(subscribe, snapshot, snapshot);
}
