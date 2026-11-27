import * as Y from "yjs";

/**
 * Where a caret goes when someone else's change took away the character it
 * kept to. A relative position on a removed character falls back to where
 * that character stood; when the same change wrote new text there (a page
 * version restored, a block's text set anew, a replaced selection), that is
 * before the new text, so the next letters land at the block's start.
 * Notion keeps the caret's character offset inside the replaced text, cut to
 * the new text's length (seen 2026-11-07: a restore leaves the caret at the
 * end of the restored line). The replaced run is the change's removed and
 * written characters next to each other; a removal with no writing keeps the
 * relative position's place.
 *
 * `index` is the caret's text index before the change. Returns the text
 * index after it, or null when the caret's character is still there.
 */
export function replacedRunIndex(text: Y.Text, transaction: Y.Transaction, kept: Y.ID, index: number): number | null {
  if (!Y.isDeleted(transaction.deleteSet, kept)) return null;
  let before = 0, after = 0;
  let run: { before: number; after: number; removed: number; written: number; holds: boolean } | null = null;
  const settled = () => run?.holds ? run.after + Math.min(Math.max(0, index - run.before), run.removed, run.written) : null;
  for (let item = text._start; item; item = item.right) {
    if (!item.countable) continue;
    const { client, clock } = item.id;
    // Characters at clocks from the client's state before the change were written by it.
    const existed = Math.max(0, Math.min(item.length, (transaction.beforeState.get(client) ?? 0) - clock));
    const removedNow = item.deleted && Y.isDeleted(transaction.deleteSet, item.id);
    for (const [from, to, wasThere] of [[0, existed, true], [existed, item.length, false]] as const) {
      if (to <= from) continue;
      const length = to - from;
      const seenBefore = wasThere && (!item.deleted || removedNow), seenAfter = !item.deleted;
      if (seenBefore && seenAfter) {
        if (run?.holds) return settled();
        run = null;
      } else if (seenBefore || seenAfter) {
        run ??= { before, after, removed: 0, written: 0, holds: false };
        if (seenBefore) run.removed += length; else run.written += length;
        if (client === kept.client && clock + from <= kept.clock && kept.clock < clock + to) run.holds = true;
      }
      if (seenBefore) before += length;
      if (seenAfter) after += length;
    }
  }
  return settled();
}
