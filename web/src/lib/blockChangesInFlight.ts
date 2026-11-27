/**
 * The block changes this tab made to a page — blocks made, removed or moved —
 * while they are on their way to the server, by page. The page's
 * collaborative text this tab writes meanwhile waits for them before it goes
 * to the room (lib/textBehindBlockChanges.ts), or goes with the change that
 * saves them: text that reached the server first was read there without its
 * blocks, and a page opened elsewhere in that moment drew a merged line
 * beside its emptied one, or a line put back by an Undo without its place
 * (2026-11-13).
 */

/** A block change on its way: settled once it is saved or given up; the change that takes it once it is sent. */
export type BlockChangeOnItsWay = {
  readonly saved: Promise<void>;
  settled: boolean;
  mutationId: string | null;
};

const changes = new Map<string, Set<BlockChangeOnItsWay>>();

/**
 * Notes a page's block change as on its way until `saved` settles, however
 * it settles; the change that sends it names itself with `sentWith`.
 */
export function noteBlockChangeSending(pageId: string, saved: Promise<unknown>) {
  let sending = changes.get(pageId);
  if (!sending) changes.set(pageId, sending = new Set());
  const change: BlockChangeOnItsWay = { saved: saved.then(() => undefined, () => undefined), settled: false, mutationId: null };
  sending.add(change);
  void change.saved.then(() => {
    change.settled = true;
    sending.delete(change);
    if (sending.size === 0 && changes.get(pageId) === sending) changes.delete(pageId);
  });
  return {
    sentWith(mutationId: string | null) {
      change.mutationId = mutationId;
    },
  };
}

/** The page's block changes on their way now; none when none is. */
export function blockChangesOnTheirWay(pageId: string): BlockChangeOnItsWay[] {
  return [...changes.get(pageId) ?? []];
}
