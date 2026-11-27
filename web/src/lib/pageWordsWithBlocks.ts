import * as Y from "yjs";

/**
 * A page's words read with its blocks, at one moment (content_graph_words.go),
 * go into its collaborative text as its blocks are drawn. Read apart, as
 * stored, they were the room's of up to ten seconds before — a room stores
 * what it takes a moment later, its writes coalesced — and a page opened
 * while someone typed drew their line some 80 characters short, or a line
 * merged elsewhere beside its emptied one, until the room caught it up
 * (2026-11-13). They come as the room's own text: neither sent back nor kept
 * as an edit made here. A page whose words are too many to come with its
 * blocks reads them apart as before.
 */

type OpenText = { document: Y.Doc; origin: unknown };

// The page's text open here, by page.
const open = new Map<string, OpenText>();
// Words read before the page's text opened, which it takes as it opens.
const waiting = new Map<string, { words: Uint8Array; at: number }>();
// A read of the page's blocks with its words under way, and who waits for
// whether it brought them.
const reading = new Map<string, { at: number; told: Array<(brought: boolean) => void> }>();

// Words kept for a page's text this long; older ones are another opening's.
const WAITING_MS = 10_000;

/** A read of the page's blocks with its words began. */
export function noteWordsRead(pageId: string) {
  const under = reading.get(pageId);
  reading.set(pageId, { at: Date.now(), told: under?.told ?? [] });
}

/**
 * Whether the page's words come with its blocks, as a read of them under
 * way says once it is drawn; false when none is under way.
 */
export function wordsCameWithBlocks(pageId: string): Promise<boolean> {
  const under = reading.get(pageId);
  if (!under || Date.now() - under.at >= WAITING_MS) return Promise.resolve(false);
  return new Promise((settle) => {
    under.told.push(settle);
    window.setTimeout(() => settle(false), WAITING_MS - (Date.now() - under.at));
  });
}

/** The page's words read with the blocks about to be drawn, put into its text first. */
export function showWordsWithBlocks(pageId: string, words: string | undefined) {
  const under = reading.get(pageId);
  reading.delete(pageId);
  for (const tell of under?.told ?? []) tell(Boolean(words));
  if (!words) return;
  const update = Uint8Array.from(atob(words), (character) => character.charCodeAt(0));
  const text = open.get(pageId);
  if (text) Y.applyUpdate(text.document, update, text.origin);
  else waiting.set(pageId, { words: update, at: Date.now() });
}

/**
 * The page's text opened here, taking words read for it meanwhile. Returns
 * how to close it, which also does `alsoClose`.
 */
export function openWordsWithBlocks(pageId: string, document: Y.Doc, origin: unknown, alsoClose: () => void) {
  const kept = waiting.get(pageId);
  waiting.delete(pageId);
  if (kept && Date.now() - kept.at < WAITING_MS) Y.applyUpdate(document, kept.words, origin);
  const text = { document, origin };
  open.set(pageId, text);
  return () => {
    if (open.get(pageId) === text) open.delete(pageId);
    alsoClose();
  };
}
