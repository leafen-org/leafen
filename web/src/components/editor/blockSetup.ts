import { useEffect, useRef } from "react";

// A block just made from the "/" menu opens what it first needs, as Notion's
// does (seen 2026-10-16): a button its settings, a file, image, video, audio
// or PDF block its upload choice, a web bookmark or embed its link field, a
// math block its TeX editor. The block may show before or after it is asked.

/**
 * How it was asked: `slashKey`, the page of a block chosen from the "/" menu
 * with a key (Enter) rather than a press on its row — what its window does
 * with the caret when it is put away unused is its own (BlockEmptyPopover).
 */
export type BlockSetupAsk = { slashKey?: { pageId: string } };

const requested = new Map<string, BlockSetupAsk>();
const listeners = new Set<() => void>();

export function requestBlockSetup(blockId: string, ask: BlockSetupAsk = {}) {
  requested.set(blockId, ask);
  for (const listener of listeners) listener();
}

export function useBlockSetupRequest(blockId: string, open: (ask: BlockSetupAsk) => void) {
  const latest = useRef(open);
  latest.current = open;
  useEffect(() => {
    const check = () => {
      const ask = requested.get(blockId);
      if (!ask) return;
      requested.delete(blockId);
      latest.current(ask);
    };
    check();
    listeners.add(check);
    return () => {
      listeners.delete(check);
    };
  }, [blockId]);
}
