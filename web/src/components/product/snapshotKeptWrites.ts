import type { ContentGraph, GraphOperation } from "@/contentGraph";
import { overlayPendingGraphOperations } from "./pendingGraphOverlay";

/** The writes of a page's structure changes the outbox keeps for the server, oldest first. */
export type KeptGraphWrites = readonly { mutationId: string; operations: readonly GraphOperation[] }[];

/** Those writes as read, or their read while it is still under way. */
export type KeptWritesRead = KeptGraphWrites | Promise<KeptGraphWrites>;

// How long the server's answer waits for the kept writes still being read
// once it came: read from this device, they take some 60 ms when there are
// any (and none when there are none), while an answer taken ahead on the
// press can come first, drawn without them for a frame (2026-11-25).
const KEPT_READ_WAIT_MS = 300;

/**
 * A page's read from the server, with what the outbox keeps for the page read
 * beside it, so its answer is drawn with them on top at once (showKeptWrites).
 * The answer waits a moment for them, never for the server to take them: a
 * read still under way past that comes as its promise. Each read again (the
 * page's writes made meanwhile, readSettled) reads them again.
 */
export function readingKeptWrites<Value>(read: () => Promise<Value>, keptWrites: (() => Promise<KeptGraphWrites>) | undefined) {
  let kept: KeptWritesRead | undefined;
  return {
    read() {
      const value = read();
      const reading = keptWrites?.().catch((): KeptGraphWrites => []);
      kept = reading;
      void reading?.then((writes) => { if (kept === reading) kept = writes; });
      return value;
    },
    /** What the last read found of them, once read or past the wait. */
    async kept(): Promise<KeptWritesRead | undefined> {
      const reading = kept;
      if (!(reading instanceof Promise)) return reading;
      let timer = 0;
      const found = await Promise.race([reading, new Promise<undefined>((resolve) => { timer = window.setTimeout(resolve, KEPT_READ_WAIT_MS); })]);
      window.clearTimeout(timer);
      return found ?? reading;
    },
  };
}

/**
 * A page's snapshot — its copy here, or the server's answer — with the kept
 * writes it lacks on top, each where the server will put it once it is sent,
 * and the IDs of all the kept writes, which what it shows then holds. A copy
 * names the writes it shows already (its `shown`); the server's answer holds
 * none of those still kept, the outbox sending them in turn.
 */
function withKeptWrites(snapshot: { graph: ContentGraph; shown?: readonly string[] }, writes: KeptGraphWrites) {
  const shown = new Set(snapshot.shown);
  const operations = writes.flatMap(({ mutationId, operations }) => shown.has(mutationId) ? [] : [operations]);
  return {
    graph: operations.length ? overlayPendingGraphOperations(snapshot.graph, operations) : snapshot.graph,
    shown: writes.map(({ mutationId }) => mutationId),
  };
}

/**
 * Shows a page's snapshot with the changes the outbox keeps for it on top
 * where it lacks them: a copy here as the server's answer alike. A page opened
 * while changes made before were still kept (sent one by one once the server
 * answered again, or refused for a while) showed the server's answer without
 * them until they were sent, a value typed away from the server back to the
 * one before for a minute (2026-11-25). `show` is given the graph to draw,
 * or null when it has nothing to add, and the kept writes it holds; writes
 * still being read are laid on once read, while `current`.
 */
export function showKeptWrites(
  snapshot: { graph: ContentGraph; shown?: readonly string[] },
  kept: KeptWritesRead | undefined,
  show: (graph: ContentGraph | null, shown: readonly string[]) => void,
  current: () => boolean,
): void {
  if (!(kept instanceof Promise)) {
    const laid = kept ? withKeptWrites(snapshot, kept) : { graph: snapshot.graph, shown: snapshot.shown ?? [] };
    show(laid.graph, laid.shown);
    return;
  }
  show(snapshot.graph, snapshot.shown ?? []);
  void kept.then((writes) => {
    if (!current()) return;
    const laid = withKeptWrites(snapshot, writes);
    show(laid.graph === snapshot.graph ? null : laid.graph, laid.shown);
  }, () => undefined);
}
