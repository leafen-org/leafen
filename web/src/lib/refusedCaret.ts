import { focusNextFrame } from "@/components/editor/focus";

/** Where the caret was in a page whose text the DLP policy refused. */
export type RefusedCaret = { blockId: string; offset: number; before: string | null };

/** The block the caret is in, how far into its text, and the block before it. */
export function caretHere(): RefusedCaret | null {
  const selection = typeof document === "undefined" ? null : document.getSelection();
  const node = selection?.focusNode;
  const element = node instanceof Element ? node : node?.parentElement;
  const block = element?.closest<HTMLElement>("[data-block-id]");
  const blockId = block?.dataset.blockId;
  if (!selection || !node || !block || !blockId) return null;
  const range = document.createRange();
  range.selectNodeContents(block);
  range.setEnd(node, selection.focusOffset);
  const blocks = [...document.querySelectorAll<HTMLElement>("[data-block-id]")];
  const before = blocks[blocks.indexOf(block) - 1]?.dataset.blockId ?? null;
  return { blockId, offset: range.toString().length, before };
}

/**
 * Puts the caret back where it was when the page was opened again from the
 * server's copy, so typing goes on there: in its block within the text kept,
 * or at the end of the block before it when its own wasn't kept — a frame on,
 * once that copy is drawn, what is typed meanwhile waiting for it (focusNextFrame).
 */
export function putCaretBack(caret: RefusedCaret) {
  focusNextFrame(() => {
    const kept = document.querySelector(`[data-block-id="${CSS.escape(caret.blockId)}"]`);
    if (kept) return { key: caret.blockId, position: Math.min(caret.offset, kept.textContent?.length ?? 0) };
    return caret.before ? { key: caret.before, position: "end" } : null;
  });
}
