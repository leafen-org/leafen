/**
 * A page's moves to or out of the trash this tab sent that the server hasn't
 * answered yet. A page opened meanwhile is read once they have landed: the
 * trash window's restore opens the page as the window closes, the move going
 * on past it (a press answers on the next frame, user decision 2026-11-12),
 * and the read that opened it beside the move was answered for the page in
 * the trash, read only — its owner saw "편집 권한 요청" and what was typed
 * went nowhere until the page was opened again (2026-11-15, NAS-class
 * server). Every move goes through the page's metadata writes
 * (commitNativePageMetadata, the product's changePageMetadata).
 */

// A move unanswered this long is given up by its request anyway, as any write is (ownWritesInFlight.ts).
const WAIT_LIMIT_MS = 35_000;

const moves = new Map<string, Set<Promise<void>>>();

/** Notes the page's move to or out of the trash on its way until it is answered, and hands it back. */
export function noteTrashMove<T>(pageId: string, move: Promise<T>): Promise<T> {
  const landed = move.then(() => undefined, () => undefined);
  const pending = moves.get(pageId) ?? new Set<Promise<void>>();
  moves.set(pageId, pending.add(landed));
  void landed.then(() => {
    pending.delete(landed);
    if (pending.size === 0 && moves.get(pageId) === pending) moves.delete(pageId);
  });
  return move;
}

/** Whether a move of the page to or out of the trash made here is on its way. */
export function trashMoveOnItsWay(pageId: string): boolean {
  return moves.has(pageId);
}

/** Settles once the page's moves on their way now have landed or failed, or after the bound. */
export function afterTrashMoves(pageId: string): Promise<void> {
  const pending = moves.get(pageId);
  if (!pending) return Promise.resolve();
  return new Promise<void>((resolve) => {
    const timer = window.setTimeout(resolve, WAIT_LIMIT_MS);
    void Promise.all(pending).then(() => {
      window.clearTimeout(timer);
      resolve();
    });
  });
}
