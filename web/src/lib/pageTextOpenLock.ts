import { serverScopedName } from "./serverOrigin";

// Which tab here sends a page's text: a tab with the page open sends it
// through its own room (collaboration.ts); the text of pages open in no tab
// here goes in the background (keptPageTextSend.ts).

function pageTextLockName(userId: string, pageId: string) {
  return serverScopedName(`leafen:page-text:${JSON.stringify([userId, pageId])}`);
}

/**
 * Notes, for every tab here, that this tab has the page's text open and
 * sends it through its own room; returns what lets it go.
 */
export function holdPageTextOpen(userId: string, pageId: string): () => void {
  const locks = typeof navigator === "undefined" ? undefined : navigator.locks;
  if (!locks) return () => undefined;
  let release = () => {};
  const held = new Promise<void>((resolve) => { release = resolve; });
  void locks.request(pageTextLockName(userId, pageId), { mode: "shared" }, () => held).catch(() => undefined);
  return release;
}

/**
 * Runs send with the page open in no tab here, or not at all; without locks
 * in the browser it runs, the room taking twice what it has once.
 */
export async function withPageClosedEverywhere(userId: string, pageId: string, send: () => Promise<void>) {
  const locks = typeof navigator === "undefined" ? undefined : navigator.locks;
  if (!locks) return send();
  await locks.request(pageTextLockName(userId, pageId), { ifAvailable: true }, (lock) => lock ? send() : undefined);
}
