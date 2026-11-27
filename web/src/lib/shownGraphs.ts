import type { CachedContentGraph } from "./localFirstProductCache";

/**
 * The graphs of the last few pages shown in this tab, as they were shown:
 * one opened again — the page before, a row's page opened whole from its
 * peek — draws from it in the press's own frame (the held step of
 * localFirstSnapshotLifecycle.ts), before this device's copy is read and the
 * server asked, which still follow. Large pages aren't held: their copy
 * draws them, as before.
 */
const SHOWN_KEPT = 8;
const SHOWN_MOST_ITEMS = 2_000;
const shown = new Map<string, CachedContentGraph>();

function key(principalId: string, pageId: string) {
  return `${principalId}\u0000${pageId}`;
}

/** Holds `value` as what shows of `pageId` now. */
export function holdShownGraph(principalId: string, pageId: string, value: CachedContentGraph) {
  const at = key(principalId, pageId);
  shown.delete(at);
  if (value.graph.blocks.length + value.graph.rows.length > SHOWN_MOST_ITEMS) return;
  shown.set(at, value);
  if (shown.size > SHOWN_KEPT) shown.delete(shown.keys().next().value!);
}

/** What showed of `pageId` last in this tab, if it is held. */
export function shownGraph(principalId: string, pageId: string): CachedContentGraph | null {
  return shown.get(key(principalId, pageId)) ?? null;
}

/** Lets go of what showed of `pageId`: it is no longer the reader's. */
export function forgetShownGraph(principalId: string, pageId: string) {
  shown.delete(key(principalId, pageId));
}
