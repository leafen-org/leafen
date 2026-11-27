/**
 * The writes this tab has on their way to the server, so a list that shows
 * its own change at once isn't read back from the server before that change
 * lands there (2026-11-05: "모두 읽음" in the inbox, whose badge change read
 * the list again while the read was still on its way, and the dots came
 * back). Every write the app's requests send is held here while it goes
 * (apiTransport.ts); a change queued to go after others is held from the
 * moment it shows (`holdOwnWrite`). A read that a notice, a badge, a socket
 * or the window coming back asks for goes through `readAfterOwnWrites`: it
 * waits for the writes on their way when it was asked for — not ones begun
 * after, so typing doesn't keep it waiting — and reads once for however many
 * asks came meanwhile. The wait has a bound, as every wait does: a write
 * unanswered that long is given up by its request anyway.
 */

const WAIT_LIMIT_MS = 35_000;

type Hold = { settled: Promise<void>; release: () => void };

const holds = new Set<Hold>();

/** Holds reads asked for from now on until the returned release is called. */
export function holdOwnWrite(): () => void {
  let release = () => {};
  const settled = new Promise<void>((resolve) => { release = resolve; });
  const hold: Hold = { settled, release };
  holds.add(hold);
  let done = false;
  return () => {
    if (done) return;
    done = true;
    holds.delete(hold);
    hold.release();
  };
}

/** Whether any of this tab's writes is still on its way. */
export function ownWritesInFlight(): boolean {
  return holds.size > 0;
}

/** Settles once the writes on their way now have landed, or failed, or after the bound. */
export function afterOwnWrites(): Promise<void> {
  if (holds.size === 0) return Promise.resolve();
  const waiting = Promise.all([...holds].map((hold) => hold.settled)).then(() => undefined);
  return new Promise<void>((resolve) => {
    const timer = setTimeout(resolve, WAIT_LIMIT_MS);
    void waiting.then(() => { clearTimeout(timer); resolve(); });
  });
}

/**
 * A read asked for by something from outside the list — another person's
 * change, a badge, the window coming back — made after this tab's writes on
 * their way have landed. Asks while one waits are the same read; one asked
 * once that read has begun waits for it to start again.
 */
export function readAfterOwnWrites(read: () => unknown): () => void {
  let waiting = false;
  return () => {
    if (!ownWritesInFlight()) {
      void read();
      return;
    }
    if (waiting) return;
    waiting = true;
    void afterOwnWrites().then(() => {
      waiting = false;
      void read();
    });
  };
}

const sendChains = new Map<string, Promise<unknown>>();

/**
 * Sends `send` once the sends asked for before it under the same key have
 * landed or failed, so writes shown at once in the order they were made —
 * comments written in quick succession on one page — reach the server in
 * that order rather than racing on its connections. Queued, it is held as
 * on its way, so a read asked for meanwhile waits for it too.
 */
export function sendInOrder<T>(key: string, send: () => Promise<T>): Promise<T> {
  const release = holdOwnWrite();
  const sent = (sendChains.get(key) ?? Promise.resolve()).then(send);
  const settled = sent.then(release, release);
  sendChains.set(key, settled);
  void settled.then(() => { if (sendChains.get(key) === settled) sendChains.delete(key); });
  return sent;
}
