/**
 * Whether this engine keeps a box's copy of a value it takes from its parent
 * with `inherit` when the parent's value changes alone: WebKit (Safari, the
 * Mac app, iOS) does for a registered value that does not inherit, a
 * ::before's and ::after's copy too (seen 2026-11-16 in the desktop app: a
 * table's first column where the window's earlier width put it; in WebKit
 * 2336 every such copy, in a row of boxes or a pseudo-element, a value
 * written inline or chosen by an attribute, stayed at the value first
 * drawn). Blink follows the parent (Chrome 157, the same page); its user
 * agent names Chrome, iOS's Chrome (CriOS) does not.
 */
const keepsInheritedCopies = typeof navigator !== "undefined" && /AppleWebKit\//u.test(navigator.userAgent)
  && !/\b(?:Chrome|Chromium|Edg)\//u.test(navigator.userAgent);

let turn = 0;

/**
 * A box whose own value — registered not to inherit, so a change to it
 * restyles only the few boxes under it that take it with `inherit` (styles.css
 * --page-wide-*, DatabaseTableChrome --table-frozen-zone, DatabaseTimelineTable
 * --timeline-*, dockedPanelColumn --dock-gap) — or the state its stylesheet
 * works that value out from has just changed is handed down again: where the
 * engine keeps those copies, the box takes a new turn of a value that does
 * inherit, so the box and everything under it are worked out again with it.
 * Whoever writes such a value, or such a state, calls this once it has.
 */
export function handDownAgain(holder: HTMLElement | null | undefined) {
  if (!keepsInheritedCopies || !holder) return;
  turn = (turn + 1) % 1_000_000;
  holder.style.setProperty("--handed-down-turn", String(turn));
}
