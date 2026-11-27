import type { NativePageMutation } from "./nativePageMutationOutbox";

// The most cursors of one page's texts written here that are remembered.
const MAX_CHAIN_CURSORS = 64;

type Chain = {
  tail: Promise<unknown>;
  /** When the newest text written here for the page was written. */
  newest: number;
  /** When the text the server last took for the page was written. */
  takenAt: number;
  /** The cursors this device's texts of the page were made on or answered with, and the last answered. */
  cursors: Set<number>;
  cursor: number | null;
};

export interface NativePageTextChains<TResult> {
  /**
   * Sends a page's text in its turn among the page's texts, `live` for one
   * just written here; null when it goes no more, a newer text being whole.
   */
  send(
    text: NativePageMutation,
    transport: (text: NativePageMutation) => Promise<TResult>,
    live?: boolean,
  ): Promise<{ result: TResult } | null>;
}

/**
 * A page's plain text (its '페이지 내용' field) goes whole each time, checked
 * against the cursor the page had when the text was written. Written fast,
 * each text was made on the cursor the screen had, the same one, so the
 * server took the first and refused the rest as stale and the words typed
 * after the first letter were gone (2026-11-13: on a NAS-class server 100 ms
 * away); kept while away, every one waited on that cursor and only the first
 * went. A page's texts now go one at a time: one made on a cursor this
 * device's own texts of the page went through goes on the cursor the last
 * answer gave, as the next of them, so a text written elsewhere meanwhile is
 * still refused; one written before the text the server last took is let go;
 * and one just written here that a newer one written meanwhile replaces is
 * let go before it goes. Kept on the device, the newest goes in its turn.
 */
export function createNativePageTextChains<TResult>(
  answeredCursor: (result: TResult) => number | null | undefined = () => null,
): NativePageTextChains<TResult> {
  const chains = new Map<string, Chain>();
  const chainOf = (pageId: string) => {
    let chain = chains.get(pageId);
    if (!chain) {
      chain = { tail: Promise.resolve(), newest: -1, takenAt: -1, cursors: new Set(), cursor: null };
      chains.set(pageId, chain);
    }
    return chain;
  };
  const remember = (chain: Chain, cursor: number) => {
    chain.cursors.delete(cursor);
    chain.cursors.add(cursor);
    if (chain.cursors.size > MAX_CHAIN_CURSORS) chain.cursors.delete(chain.cursors.values().next().value!);
  };
  return {
    send(text, transport, live = false) {
      const chain = chainOf(text.pageId);
      if (live) chain.newest = Math.max(chain.newest, text.createdAt);
      const run = chain.tail.then(async () => {
        if (text.createdAt < chain.takenAt || (live && text.createdAt < chain.newest)) return null;
        const onChain = chain.cursor !== null && chain.cursors.has(text.baseCursor);
        const result = await transport(onChain ? { ...text, baseCursor: chain.cursor! } : text);
        chain.takenAt = Math.max(chain.takenAt, text.createdAt);
        const cursor = answeredCursor(result);
        if (typeof cursor === "number" && Number.isSafeInteger(cursor)) {
          remember(chain, text.baseCursor);
          remember(chain, cursor);
          chain.cursor = cursor;
        }
        return { result };
      });
      chain.tail = run.catch(() => undefined);
      return run;
    },
  };
}
