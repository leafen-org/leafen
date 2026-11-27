// Items drawn with `content-visibility: auto` — a table's rows, a board's
// and a gallery's cards — keep their contents unpainted until the browser
// finds them near the view, which it does only in the frame after they are
// put in: an item coming into a list already shown — a row made, copied or
// come from someone else — painted its cells empty for a frame, its contents
// the next (2026-11-14, about 7 ms at CPU ×1, every copy of a ⌘D). Such an
// item is marked `data-arriving` until its first frame has painted, which
// its stylesheet draws whole (content-visibility: visible). The items a list
// draws first keep theirs: a page opening with tables below its fold lays
// out only the rows near the view.

// The items each list has drawn, from its first drawing on.
const drawn = new WeakMap<Element, WeakSet<Element>>();

/**
 * Marks the items new to `list` since it last drew, called after each of its
 * drawings before it is painted (a layout effect); the first drawing marks
 * none.
 */
export function drawArrivalsWhole(list: Element | null, items: Iterable<HTMLElement>) {
  if (!list) return;
  const known = drawn.get(list);
  if (!known) {
    drawn.set(list, new WeakSet(items));
    return;
  }
  for (const item of items) {
    if (known.has(item)) continue;
    known.add(item);
    item.dataset.arriving = "true";
    // Painted whole in the frame after this one's start, and found in view
    // there: from the next it may be skipped again when out of view.
    requestAnimationFrame(() => requestAnimationFrame(() => { delete item.dataset.arriving; }));
  }
}
