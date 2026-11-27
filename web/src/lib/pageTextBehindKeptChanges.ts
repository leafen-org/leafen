import type { WebsocketProvider } from "y-websocket";
import type { NativePageMutationBrowser } from "./nativePageMutationBrowser";
import type { PagesWithKeptChanges } from "./nativeOutboxKeptPages";

/**
 * A page's text this device has — its copy here, written while the server was
 * away, in this tab or in one closed before it could send — reaches the server
 * only behind the page's changes this device keeps for it (its new blocks, its
 * settings), of any app's format: sent ahead, the server holds words of blocks
 * it doesn't have, in a version of the page apart from the change (2026-11-18:
 * a tab that didn't open the page sent the copy's line as it started; one
 * opening the page sent it through its room as it joined, some 30 ms ahead of
 * the block, always ahead with the block kept in a newer app's format it left
 * for that app; a tab back from an outage sent the line typed meanwhile 0.3 s
 * ahead of its Enter). The one rule for every way that text goes: the room of
 * a page no tab shows (keptPageTextSend.ts), each connection of the page's own
 * room (holdRoomTextBehindKeptChanges), and the copy taken back on a return
 * (collaboration.ts).
 */

type KeptChangesOutbox = Pick<NativePageMutationBrowser<unknown>, "pagesWithKeptChanges" | "subscribeChanges">;

const told = new Set<KeptChangesOutbox>();
// How often a wait reads the outbox again though it heard of no change.
const READ_AGAIN_MS = 3_000;

/** Tells which outbox keeps an account's changes; the returned function stops it. */
export function tellKeptChanges(outbox: KeptChangesOutbox): () => void {
  told.add(outbox);
  return () => { told.delete(outbox); };
}

/** The pages whose changes the outboxes in use keep, or "every" when one can't be read now. */
export async function pagesWithKeptChangesNow(): Promise<PagesWithKeptChanges> {
  const pages = new Set<string>();
  for (const outbox of [...told]) {
    const kept = await Promise.resolve().then(() => outbox.pagesWithKeptChanges()).catch(() => "every" as const);
    if (kept === "every") return "every";
    for (const pageId of kept) pages.add(pageId);
  }
  return pages;
}

/**
 * Settles once no outbox in use keeps a change of the page, or once signal is
 * aborted: true when it waited for some. An outbox that can't be read now
 * (another tab keeps it past the wait, deviceStoreLock.ts) holds no page here:
 * the page's own room doesn't wait on a tab that may never let go; nor does
 * one that fails to be read or heard, so it never rejects.
 */
export async function afterKeptChangesWent(pageId: string, signal?: AbortSignal | null): Promise<boolean> {
  for (let held = false; ; held = true) {
    if (signal?.aborted) return held;
    const kept = await pagesWithKeptChangesNow().catch(() => "every" as const);
    if (kept === "every" || !kept.has(pageId) || signal?.aborted) return held;
    const heard = await new Promise<void>((resolve) => {
      let stops: (() => void)[] = [];
      let timer = 0;
      const done = () => {
        for (const stop of stops) stop();
        window.clearTimeout(timer);
        signal?.removeEventListener("abort", done);
        resolve();
      };
      stops = [...told].map((outbox) => outbox.subscribeChanges(done));
      timer = window.setTimeout(done, READ_AGAIN_MS);
      signal?.addEventListener("abort", done);
    }).then(() => true, () => false);
    if (!heard) return held;
  }
}

// y-websocket's sync message, and in it what carries text: the answer to the room's ask, and an update (y-protocols/sync).
const MESSAGE_SYNC = 0;
const SYNC_STEP_2 = 1;
const SYNC_UPDATE = 2;

function carriesText(data: unknown) {
  const bytes = data instanceof Uint8Array ? data : data instanceof ArrayBuffer ? new Uint8Array(data) : null;
  return bytes !== null && bytes.length > 1 && bytes[0] === MESSAGE_SYNC && (bytes[1] === SYNC_STEP_2 || bytes[1] === SYNC_UPDATE);
}

/**
 * Each connection of a page's room, its first and every one after a return,
 * sends the page's text only once no change of the page is kept here: what it
 * would send meanwhile — the answer to what the room lacks, and the text
 * written since — waits, in its order, while the rest goes (its presence, its
 * ask for the room's text), so the page shows the room's text and people at
 * once. One that waited tells `waited`: the changes it waited for went to a
 * server that may have told no room of them, the room not open there yet, and
 * the page reads what changed since it read (collaboration.ts catchUps). A
 * connection that closes first drops what waited; the next one's answer holds
 * it all again. Stops with `signal`.
 */
export function holdRoomTextBehindKeptChanges(provider: WebsocketProvider, pageId: string, signal: AbortSignal, waited: () => void) {
  const Socket = provider._WS as typeof WebSocket;
  const send = Socket.prototype.send;
  provider._WS = class extends Socket {
    private waiting: Parameters<WebSocket["send"]>[0][] | null = [];
    constructor(url: string | URL, protocols?: string | string[]) {
      super(url, protocols);
      const closed = new AbortController();
      this.addEventListener("close", () => closed.abort(), { once: true });
      void afterKeptChangesWent(pageId, AbortSignal.any([signal, closed.signal])).then((held) => {
        const waiting = this.waiting ?? [];
        this.waiting = null;
        if (closed.signal.aborted || signal.aborted) return;
        for (const data of waiting) send.call(this, data);
        if (held) waited();
      });
    }

    send(data: Parameters<WebSocket["send"]>[0]) {
      if (this.waiting && carriesText(data)) this.waiting.push(data);
      else send.call(this, data);
    }
  };
}
