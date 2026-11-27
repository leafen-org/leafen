import * as Y from "yjs";
import type { WebsocketProvider } from "y-websocket";
import { blockChangeTextOf, type BlockChangeText } from "./blockChangeText";
import { blockChangesOnTheirWay, type BlockChangeOnItsWay } from "./blockChangesInFlight";
import { bytesToBase64 } from "./collaborationCrdtCodec";

// The longest this tab's text waits for its block changes; then it goes anyway.
const TEXT_WAIT_LIMIT_MS = 1000;
// The longest text gone with a change waits for its answer; then it goes to the room too.
const RIDING_LIMIT_MS = 10_000;

type UpdateSender = WebsocketProvider["_updateHandler"];

// The text one moment wrote, the block changes made before it that were on
// their way then, and whether it may go; `riding`, the change it went with
// to the server; `stored`, whether that change stored it, so the room takes
// it from the server.
type Batch = { updates: Uint8Array[]; ahead: BlockChangeOnItsWay[]; ready: boolean; riding: string | null; stored: boolean; at: number };

/** Text that goes to the server with a block change: once answered, `stored` or not. */
export type TextRiding = { text: BlockChangeText; answered(stored: boolean): void };

// The pages whose text waits here, by page, for the change that may take it.
const holders = new Map<string, (mutationId: string) => TextRiding | null>();

// The text the batches wrote, as one update in base64; null for none.
function textOf(batches: Batch[]) {
  const updates = batches.flatMap((batch) => batch.updates);
  if (updates.length === 0) return null;
  return bytesToBase64(updates.length === 1 ? updates[0] : Y.mergeUpdates(updates));
}

/**
 * The text this tab wrote in a page that waits only for block changes sent
 * by now — with the change `mutationId`, which is being sent, or before it:
 * it goes with that change, stored with it, and reaches the page's room only
 * once the change is committed (content_graph_mutation_words.go). None when
 * no such text waits.
 */
export function textRidingWith(pageId: string, mutationId: string): TextRiding | null {
  return holders.get(pageId)?.(mutationId) ?? null;
}

/**
 * Sends what this tab writes in a page's collaborative text to the server
 * only with or after the block changes made here before it
 * (lib/blockChangesInFlight.ts), so text never reaches the server, and the
 * tabs opening or showing the page, ahead of its blocks: a line merged into
 * the one above, a line split by Enter, a line put back by ⌘Z reach the
 * others with their blocks. Text that waits for the block changes one change
 * sends goes with it (textRidingWith), stored in the same commit; any other
 * goes to the room once they are saved, or when that change didn't store it.
 * Text written later waits only for the changes made before it, and goes in
 * the order written. The page here shows its text at once; only the sending
 * waits, at most TEXT_WAIT_LIMIT_MS but for text gone with a change. What a
 * moment wrote is looked at once it ends, as an Undo writes its text before
 * its blocks. Each update goes to `kept`, the page's copy on this device,
 * as it goes, in the order written: kept as it was written, a tab opening
 * the page in this browser meanwhile drew it from the copy beside the
 * server's blocks from before it — a merged line beside its emptied one
 * (2026-11-13); the change it waits for keeps it in the outbox meanwhile.
 * Returns a function that sends what waits at once, as the page goes.
 */
export function holdTextBehindBlockChanges(provider: WebsocketProvider, document: Y.Doc, pageId: string, kept: (update: Uint8Array) => void) {
  const send: UpdateSender = provider._updateHandler;
  const batches: Batch[] = [];
  let open: Batch | null = null;
  let limit = 0;
  const sendReady = () => {
    const updates: Uint8Array[] = [];
    while (batches[0]?.ready) {
      const batch = batches.shift()!;
      for (const update of batch.updates) kept(update);
      if (!batch.stored) updates.push(...batch.updates);
    }
    if (updates.length) send(updates.length === 1 ? updates[0] : Y.mergeUpdates(updates), null);
    waitNoLonger();
  };
  // Each moment's text waits at most TEXT_WAIT_LIMIT_MS from when it was
  // written, however many came after it (2026-11-13: one wait for them all,
  // in a run of merges and Undos it sent the newest text a moment after it
  // was written, ahead of its blocks, about once a second).
  const waitNoLonger = () => {
    window.clearTimeout(limit);
    limit = 0;
    const waiting = (batch: Batch) => !batch.ready && !batch.riding && batch !== open;
    const oldest = batches.find(waiting);
    if (!oldest) return;
    limit = window.setTimeout(() => {
      limit = 0;
      const now = performance.now();
      for (const batch of batches) if (waiting(batch) && now - batch.at >= TEXT_WAIT_LIMIT_MS) batch.ready = true;
      sendReady();
    }, Math.max(0, oldest.at + TEXT_WAIT_LIMIT_MS - performance.now()));
  };
  const release = () => {
    open = null;
    for (const batch of batches) {
      batch.ready = true;
      batch.riding = null;
    }
    sendReady();
  };
  const close = () => {
    const batch = open;
    open = null;
    if (!batch) return;
    batch.ahead = blockChangesOnTheirWay(pageId);
    if (batch.ahead.length) void Promise.all(batch.ahead.map(({ saved }) => saved)).then(() => {
      if (batch.riding || !batches.includes(batch)) return;
      batch.ready = true;
      sendReady();
    });
    else batch.ready = true;
    sendReady();
  };
  const hold: UpdateSender = (update, origin) => {
    // What the room sent is the room's already.
    if (origin === provider) return;
    if (!open) {
      open = { updates: [], ahead: [], ready: false, riding: null, stored: false, at: performance.now() };
      batches.push(open);
      queueMicrotask(close);
    }
    open.updates.push(update);
  };
  // The text first in line that waits only for changes saved or sent by
  // now goes with this one, committed after them, past text gone with one
  // sent before and not answered yet (2026-11-13: stopped at it, a change
  // sent while the server was slow went without its text); text behind
  // waits for what it waits for. What was written before this change goes
  // apart from what was written with it, as the typing it is, stored first
  // (content_graph_mutation_words.go): merged, a line typed in the second
  // after Enter and removed with the block menu went with its emptying, its
  // words in no version of the page (2026-11-18).
  const ride = (mutationId: string): TextRiding | null => {
    const going: Batch[] = [];
    for (const batch of batches) {
      if (batch.riding) continue;
      if (batch.ready || batch === open) break;
      if (!batch.ahead.every((change) => change.settled || change.mutationId !== null)) break;
      going.push(batch);
    }
    if (going.length === 0) return null;
    for (const batch of going) batch.riding = mutationId;
    const withIt = going.findIndex((batch) => batch.ahead.some((change) => change.mutationId === mutationId));
    const typing = textOf(withIt < 0 ? going : going.slice(0, withIt));
    const words = withIt < 0 ? null : textOf(going.slice(withIt));
    const unanswered = window.setTimeout(() => riding.answered(false), RIDING_LIMIT_MS);
    const riding: TextRiding = {
      text: blockChangeTextOf({ words: words ?? undefined, typing: typing ?? undefined }),
      answered(stored) {
        window.clearTimeout(unanswered);
        for (const batch of going) {
          if (batch.riding !== mutationId) continue;
          batch.riding = null;
          batch.ready = true;
          batch.stored = stored;
        }
        sendReady();
      },
    };
    return riding;
  };
  // The provider sends through its own handler, which it also takes off as it goes.
  document.off("update", send);
  provider._updateHandler = hold;
  document.on("update", hold);
  holders.set(pageId, ride);
  window.addEventListener("pagehide", release);
  return () => {
    window.removeEventListener("pagehide", release);
    if (holders.get(pageId) === ride) holders.delete(pageId);
    release();
  };
}
