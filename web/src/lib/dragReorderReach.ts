type Side = "before" | "after";
type Input = { clientX: number; clientY: number };
type Placed = { id: string; box: DOMRect };
export type ReachLanding = { target: string; side: Side };

/**
 * How far past a database view's rows or cards the pointer still lands a
 * dragged one, as Notion's (seen 2026-11-17 in Chrome: a gallery's small and
 * large cards took it up to 200px beyond a row of cards' first or last, not
 * 203; its list and table took it 192px below their last row and 71px above
 * their first, as far as the window went).
 */
export const DROP_REACH = 200;

/**
 * Where a row dragged among a database view's rows (a table's, a list's)
 * lands with the pointer beside them, as Notion's: across their band — from
 * their handles' column to their right edge — the pointer's height alone
 * picks it, the row it is level with, by its nearer half; between two rows
 * the nearer one; above the first row before it and below the last after
 * it, within DROP_REACH. Seen 2026-11-17 in Chrome by the pointer's own
 * events, the line read while dragging, from the handle's top, middle and
 * bottom alike: its list's line stayed with the pointer down its handles'
 * column and in the gaps between rows, and went past its rows' right edge.
 * Notion's band starts a few pixels left of the handles (its list's 10, its
 * table's 7), which isn't followed.
 */
export function landingBesideRows(rows: Placed[], handlesLeft: number, input: Input): ReachLanding | null {
  const shown = rows.filter(({ box }) => box.height > 0).sort((a, b) => a.box.top - b.box.top);
  if (!shown.length) return null;
  const left = Math.min(handlesLeft, ...shown.map(({ box }) => box.left)), right = Math.max(...shown.map(({ box }) => box.right));
  const { clientX: x, clientY: y } = input;
  if (x < left || x > right) return null;
  const first = shown[0], last = shown[shown.length - 1];
  if (y < first.box.top) return y >= first.box.top - DROP_REACH ? { target: first.id, side: "before" } : null;
  if (y >= last.box.bottom) return y <= last.box.bottom + DROP_REACH ? { target: last.id, side: "after" } : null;
  for (let index = 0; index < shown.length; index++) {
    const { id, box } = shown[index];
    if (y < box.top) {
      const above = shown[index - 1];
      return y - above.box.bottom < box.top - y ? { target: above.id, side: "after" } : { target: id, side: "before" };
    }
    if (y < box.bottom) return { target: id, side: y > box.top + box.height / 2 ? "after" : "before" };
  }
  return null;
}

/**
 * Where a card dragged among a gallery's cards lands, as Notion's: on a
 * card, by its nearer half; beside it in its line of cards — between two,
 * or past the line's first or last within DROP_REACH, the next free place
 * too — on the side of the nearest card in that line. Between two lines of
 * cards, above and below them, it lands nowhere. Seen 2026-11-17 in Chrome by
 * the pointer's own events, the line read while dragging, from a card's
 * left edge, middle and right edge alike; a card's line is its own height.
 */
export function landingAmongCards(cards: Placed[], input: Input): ReachLanding | null {
  const { clientX: x, clientY: y } = input;
  let nearest: { placed: Placed; distance: number } | null = null;
  for (const placed of cards) {
    const { box } = placed;
    if (box.width === 0 || y < box.top || y >= box.bottom) continue;
    const distance = x < box.left ? box.left - x : x > box.right ? x - box.right : 0;
    if (!nearest || distance < nearest.distance) nearest = { placed, distance };
  }
  if (!nearest || nearest.distance > DROP_REACH) return null;
  const { id, box } = nearest.placed;
  return { target: id, side: x > box.left + box.width / 2 ? "after" : "before" };
}
