// A tall panel opened low on the page brings its opener up first, as
// Notion's view settings do (seen 2026-11-04 in a 960-high window): a panel
// that fits under its button opens there; one that doesn't, from a button in
// the window's upper part (its top above 565), moves up over the button to
// end 12 above the window's bottom (`rise`); from a button lower than that,
// the page scrolls the button up to 50 from the window's top and the panel
// opens under it (button 566 or 570 → 50, panel from 81).

const OPENER_TOP = 50;
const LOW_OPENER = 0.59;

function scrollParent(element: HTMLElement): HTMLElement | null {
  for (let parent = element.parentElement; parent; parent = parent.parentElement) {
    const overflow = getComputedStyle(parent).overflowY;
    if ((overflow === "auto" || overflow === "scroll") && parent.scrollHeight > parent.clientHeight) return parent;
  }
  return null;
}

/**
 * Scrolls the page to bring a low opener up when its panel won't fit under it; true when it did.
 * Given where the opener was as it was pressed (`pressedAt`), one high on the page reads nothing:
 * read as the panel is drawn, the panel's height and the opener's place laid the page out in the
 * press that opened it.
 */
export function scrollOpenerUpForPanel(opener: HTMLElement | null, panelHeight: number | (() => number), margin = 12,
  pressedAt?: { top: number }): boolean {
  const height = window.innerHeight;
  if (!opener || (pressedAt && pressedAt.top <= height * LOW_OPENER)) return false;
  const panel = typeof panelHeight === "number" ? panelHeight : panelHeight();
  if (panel <= 0) return false;
  const rect = opener.getBoundingClientRect();
  if (rect.bottom + panel <= height - margin || rect.top <= height * LOW_OPENER) return false;
  const scroller = scrollParent(opener);
  if (!scroller) return false;
  const before = scroller.scrollTop;
  scroller.scrollTop = before + rect.top - OPENER_TOP;
  return scroller.scrollTop !== before;
}

/**
 * Brings a card up whose foot is under the window's as its menu opens, as
 * Notion's form question options do (seen 2026-11-08 in a 723-high window: a
 * card at 549..789 went to 405..645, the "+" 12 under it showing with 34 to
 * spare); a card that shows whole with that room stays, and the card's top
 * stays at least 50 under its scroller's.
 */
export function scrollCardFootIntoView(card: HTMLElement | null, below = 78): boolean {
  if (!card) return false;
  const rect = card.getBoundingClientRect();
  const over = rect.bottom + below - window.innerHeight;
  const scroller = over > 0 ? scrollParent(card) : null;
  if (!scroller) return false;
  const before = scroller.scrollTop;
  scroller.scrollTop = before + Math.min(over, Math.max(0, rect.top - scroller.getBoundingClientRect().top - OPENER_TOP));
  return scroller.scrollTop !== before;
}

/** Whether an element shows whole in its scroller and the window. */
export function showsWhole(element: Element): boolean {
  const rect = element.getBoundingClientRect();
  const scroller = element instanceof HTMLElement ? scrollParent(element) : null;
  const box = scroller?.getBoundingClientRect() ?? { top: 0, bottom: window.innerHeight };
  return rect.top >= Math.max(0, box.top) && rect.bottom <= Math.min(window.innerHeight, box.bottom);
}
