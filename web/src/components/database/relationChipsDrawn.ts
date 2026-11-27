import { startTransition, useEffect, useState } from "react";
import { afterNextPaint } from "@/lib/afterPress";

/** The pages a relation's value draws at once: more than a cell or a row page's first screen shows. */
export const FIRST_RELATION_CHIPS = 24;

// How long a slice waits for the page to be idle before it is drawn anyway.
const IDLE_WAIT_MS = 1_000;

function whenIdle(run: () => void): () => void {
  if (typeof window.requestIdleCallback === "function") {
    const id = window.requestIdleCallback(run, { timeout: IDLE_WAIT_MS });
    return () => window.cancelIdleCallback(id);
  }
  return afterNextPaint(run);
}

/**
 * How many of a relation's `count` pages its value draws: the first ones as
 * it shows, the rest as many again each time the page is idle, behind any
 * press. A row's side peek holding a relation of 100 pages drew the 76 after
 * the first in one go, some 1,140 parts, which a frame of the peek opening
 * laid out with the page beside it (seen 2026-11-13); drawn when the page is
 * idle, they come once the peek has opened, a few at a time.
 */
export function useRelationChipsDrawn(count: number): number {
  const [drawn, setDrawn] = useState(FIRST_RELATION_CHIPS);
  useEffect(() => {
    if (drawn >= count) return;
    return whenIdle(() => startTransition(() => setDrawn((now) => now + FIRST_RELATION_CHIPS)));
  }, [count, drawn]);
  return drawn;
}
