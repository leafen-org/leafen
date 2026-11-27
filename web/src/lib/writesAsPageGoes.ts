import { pageLeaving } from "./pageLeaving";

// A write sent straight to the server, kept nowhere on the device — a
// database row's values: a row page's title, a cell's text, a task's values —
// went with the page when the page went before its answer, the request going
// with its document: the value typed last was lost and an earlier save of it
// stood (2026-11-22: "zy" typed in an opened row's title and deleted, the row
// closed and another page opened at once, and the row's title read "zy"
// again). As the outbox does with the changes on their way
// (nativePageMutationLeaving.ts), what is on its way as the page goes is sent
// again then, in a request that outlives the page (pageLeaving.ts,
// apiTransport.ts): the server takes a write once by its ID, so one that
// arrived already is not made twice.

const goingWithPage = new Map<number, () => unknown>();
let written = 0;

if (typeof window !== "undefined") {
  // After pageLeaving's own listener (a capturing one), so what goes outlives the page.
  window.addEventListener("pagehide", () => {
    const sends = [...goingWithPage.values()];
    goingWithPage.clear();
    for (const send of sends) {
      try {
        void Promise.resolve(send()).catch(() => undefined);
      } catch {
        // One that can't go now doesn't keep the others.
      }
    }
  });
}

/** Holds `send` to go as the page goes, oldest first, until the answer given is called. */
export function sendAsPageGoes(send: () => unknown): () => void {
  const key = written += 1;
  goingWithPage.set(key, send);
  // A request failing as the page goes fails after its going was heard: it stays.
  return () => { if (!pageLeaving()) goingWithPage.delete(key); };
}

/** Sends a write that is sent again as the page goes before its answer. */
export async function sentOutlivingPage<T>(send: () => Promise<T>): Promise<T> {
  if (pageLeaving()) return send();
  const answered = sendAsPageGoes(send);
  try {
    return await send();
  } finally {
    answered();
  }
}
