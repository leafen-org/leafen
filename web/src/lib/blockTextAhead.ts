import type * as Y from "yjs";
import { BLOCK_TEXT_ROOT_PREFIX } from "./blockTextEditing";
import { yTextToRichText } from "./collaborationCrdtCodec";
import type { TextSpan } from "./types";

// The longest a line's text is shown ahead of its collaborative text: the
// page's room out of reach meanwhile, the line shows what the room holds.
const AHEAD_LIMIT_MS = 30_000;

// By page document, the text each block's line shows while its own is empty.
const ahead = new WeakMap<Y.Doc, Map<string, TextSpan[]>>();

type TextReads = { spans: WeakMap<object, TextSpan[]>; changing: boolean };
const reads = new WeakMap<Y.Doc, TextReads>();

/**
 * A text's spans, the same ones while it stays as it was: the page's lines
 * are drawn again from them after each change of its blocks, and the
 * engine keeps a line's drawing whose block and spans are the ones it was
 * drawn from (nativeTiptapBlocks). Read anew each time, every line of a
 * long page was made again for each Enter (2026-11-14). A text's spans go
 * as a change to it is told, and while the page's changes are being made
 * and told, a text is read as it is, not kept.
 */
function ownRichText(document: Y.Doc, text: Y.Text): TextSpan[] {
  let kept = reads.get(document);
  if (!kept) {
    const made: TextReads = { spans: new WeakMap(), changing: false };
    document.on("beforeAllTransactions", () => { made.changing = true; });
    document.on("beforeObserverCalls", (transaction: Y.Transaction) => {
      for (const type of transaction.changed.keys()) made.spans.delete(type);
      for (const type of transaction.changedParentTypes.keys()) made.spans.delete(type);
    });
    document.on("afterAllTransactions", () => { made.changing = false; });
    reads.set(document, kept = made);
  }
  if (kept.changing || document._transaction) return yTextToRichText(text) ?? [];
  const found = kept.spans.get(text);
  if (found) return found;
  const spans = yTextToRichText(text) ?? [];
  kept.spans.set(text, spans);
  return spans;
}

/**
 * The text a block's line shows: its collaborative text, or, while that is
 * empty, the text shown ahead of it (showBlockTextAhead), which its own text
 * ends once it has come: emptied later, the line is empty.
 */
export function blockRichText(document: Y.Doc, blockId: string): TextSpan[] {
  const own = ownRichText(document, document.getText(BLOCK_TEXT_ROOT_PREFIX + blockId));
  const held = ahead.get(document);
  if (own.length) {
    held?.delete(blockId);
    return own;
  }
  return held?.get(blockId) ?? own;
}

/** Each block's text as the page holds it now, for showBlockTextAhead later; empty lines left out. */
export function blockTextsNow(document: Y.Doc, blockIds: Iterable<string>): Map<string, TextSpan[]> {
  const texts = new Map<string, TextSpan[]>();
  for (const id of blockIds) {
    const spans = blockRichText(document, id);
    if (spans.length) texts.set(id, spans);
  }
  return texts;
}

/**
 * Lines whose text the server is bringing back into the page — blocks a ⌘Z
 * takes back from the page they were moved to — shown with that text at the
 * press (user decision 2026-11-12: a press answers on the next frame; the
 * server moves a block's text with it, so drawn back before its text came,
 * the line stood empty some 300 ms on a NAS-class server). A line's own text
 * takes its place as it comes; the rest goes at the returned release, or
 * after AHEAD_LIMIT_MS. Only the screen shows it: nothing is written.
 */
export function showBlockTextAhead(document: Y.Doc, texts: ReadonlyMap<string, TextSpan[]>): () => void {
  if (!texts.size) return () => undefined;
  const held = ahead.get(document) ?? new Map<string, TextSpan[]>();
  ahead.set(document, held);
  for (const [id, spans] of texts) held.set(id, spans);
  let released = false;
  const release = () => {
    if (released) return;
    released = true;
    window.clearTimeout(limit);
    for (const [id, spans] of texts) if (held.get(id) === spans) held.delete(id);
  };
  const limit = window.setTimeout(release, AHEAD_LIMIT_MS);
  return release;
}
