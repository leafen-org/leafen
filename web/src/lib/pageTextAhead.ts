import * as Y from "yjs";
import { replayYgoRecoveryUpdates } from "@/offline";
import { pageTextShown } from "@/lib/pageTextStored";

// A page about to open in place of the one open now, its body drawn the
// same across (an inline database opened alone, moved to another page or
// back): its text document is made and its copy on this device read into it
// ahead, while the server moves it, so the page opens with it at once. Its
// editor was drawn only once that copy had been read after the page changed,
// the database's body blank 52–56 ms meanwhile on a NAS-class server
// (2026-11-13). One at a time; one not taken soon is let go, as it may be
// behind the copy by then.
const KEPT_MS = 5_000;

type Ahead = { principalId: string; pageId: string; document: Y.Doc; fromCopy: boolean; read: Promise<void>; ready: boolean; at: number };

let ahead: Ahead | null = null;

function current(principalId: string, pageId: string) {
  if (ahead && performance.now() - ahead.at > KEPT_MS) dropPageTextAhead();
  return ahead && ahead.principalId === principalId && ahead.pageId === pageId ? ahead : null;
}

/** Reads `pageId`'s copy here into a document of its own; settles once read, or not. */
export function readPageTextAhead(principalId: string, pageId: string): Promise<void> {
  const kept = current(principalId, pageId);
  if (kept) return kept.read;
  dropPageTextAhead();
  const document = new Y.Doc();
  const entry: Ahead = { principalId, pageId, document, fromCopy: false, ready: false, at: performance.now(), read: Promise.resolve() };
  entry.read = replayYgoRecoveryUpdates(principalId, pageId, (update) => {
    entry.fromCopy = true;
    Y.applyUpdate(document, update);
  }).then(() => { entry.ready = true; }, () => { if (ahead === entry) dropPageTextAhead(); });
  ahead = entry;
  return entry.read;
}

/**
 * Hands the text a peek shows of `pageId` to the page opening in its place
 * (rowPageHandoff.ts): a copy of it, ready at once, so that page's body draws
 * with its words in the press's frame. Read anew, they came 200–300 ms after
 * its head on a NAS-class server (2026-11-17), its device copy read and the
 * server asked first. The room then brings what changed since, as it does.
 * A body with no words hands nothing over: its editor, made in the press's
 * frame for nothing to show, held that frame back 20–35 ms (2026-11-17).
 */
export function handPageTextOver(pageId: string) {
  const shown = pageTextShown(pageId);
  // An empty document's state is two bytes: no structs, no deletions.
  const words = shown ? Y.encodeStateAsUpdate(shown.document) : null;
  if (!shown || !words || words.length <= 2) return;
  dropPageTextAhead();
  const document = new Y.Doc();
  Y.applyUpdate(document, words);
  ahead = { principalId: shown.principalId, pageId, document, fromCopy: false, ready: true, at: performance.now(), read: Promise.resolve() };
}

/**
 * Holds the words a page made here starts with — a row's body copied from
 * its template (rowMadeHere.ts keepRowBodyMadeHere) — as its text ahead, ready
 * at once, so its page opening next draws its body in the frame its head
 * draws: read from its copy here, it came 50–90 ms after the head
 * (2026-11-25, a task made from a template in 내 작업), where Notion draws the
 * page's icon, title and body in one frame.
 */
export function holdPageTextAhead(principalId: string, pageId: string, words: Uint8Array) {
  dropPageTextAhead();
  const document = new Y.Doc();
  Y.applyUpdate(document, words);
  ahead = { principalId, pageId, document, fromCopy: true, ready: true, at: performance.now(), read: Promise.resolve() };
}

/** The document read ahead for `pageId`, once read: what its page draws with until it opens. */
export function pageTextAhead(principalId: string, pageId: string): Y.Doc | null {
  const kept = current(principalId, pageId);
  return kept?.ready ? kept.document : null;
}

/** Takes the document read ahead for `pageId` as the page opens, and whether its copy held any of it. */
export function takePageTextAhead(principalId: string, pageId: string): { document: Y.Doc; fromCopy: boolean } | null {
  const kept = current(principalId, pageId);
  if (!kept?.ready) return null;
  ahead = null;
  return { document: kept.document, fromCopy: kept.fromCopy };
}

/** Lets go of the document read ahead (for `pageId`, when named), if no page took it. */
export function dropPageTextAhead(pageId?: string) {
  if (pageId !== undefined && ahead?.pageId !== pageId) return;
  const dropped = ahead;
  ahead = null;
  dropped?.document.destroy();
}
