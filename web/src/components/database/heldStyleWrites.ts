import { flushSync } from "react-dom";

/**
 * Style writes a view's layout hooks make from what they measured, held
 * until the hooks after them have measured too, so a view opening works out
 * the page's styles once for all of them, not again after each one's writes
 * (a variable set on a table's scroller restyles every cell under it: some
 * 100ms each time for a table of 30 rows by 40 properties, seen 2026-11-12).
 * The held writes are made by the first measure that goes by what they
 * change (`flushHeldStyleWrites`), else as soon as the task that held them
 * is done, before the page is drawn.
 *
 * A view's first measures (`holdMeasure`) wait the same way, till the task
 * that drew it is done: a page's blocks are put in place one after another
 * as it opens, each laying the page out anew for the next view that measured
 * (its tabs, its tools, its table — some 90ms each for the same table, four
 * times as a page holding two databases opened, seen 2026-11-12). Measured
 * together after the last of them, the page is laid out once; what they then
 * change is drawn at once, before the page is.
 */
const held: Array<() => void> = [];
const measures = new Map<object, () => void>();
let queued = false;

export function holdStyleWrite(write: () => void): void {
  held.push(write);
  queue();
}

/**
 * Measures with the other held measures once the task is done, `owner`'s
 * latest measure alone (a view drawn twice in the task measures once); the
 * state it sets is drawn before the page is. Returns what drops it.
 */
export function holdMeasure(owner: object, measure: () => void): () => void {
  measures.set(owner, measure);
  queue();
  return () => {
    if (measures.get(owner) === measure) measures.delete(owner);
  };
}

/**
 * Starts what follows a view's layout with the other held measures (above),
 * what it first reads of the page — its own measures and the styles it looks
 * for — read with theirs. Returns what stops it, or keeps it from starting.
 */
export function startWithMeasures(start: () => (() => void) | undefined): () => void {
  let stop: (() => void) | undefined;
  const drop = holdMeasure(start, () => { stop = start(); });
  return () => {
    drop();
    stop?.();
  };
}

export function flushHeldStyleWrites(): void {
  for (const write of held.splice(0)) write();
}

function queue() {
  if (queued) return;
  queued = true;
  queueMicrotask(flushHeld);
}

function flushHeld() {
  queued = false;
  flushHeldStyleWrites();
  if (measures.size === 0) return;
  const run = [...measures.values()];
  measures.clear();
  flushSync(() => {
    for (const measure of run) measure();
  });
  flushHeldStyleWrites();
}
