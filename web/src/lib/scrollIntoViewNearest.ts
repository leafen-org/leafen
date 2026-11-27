import { placeInScrollBox } from "./scrollBoxPlace";

/**
 * Scrolls the nearest scrolling box so an element shows, as the browser's
 * `scrollIntoView({ block: "nearest" })` does, without moving where the next
 * Tab starts — the browser's own call moves it there (2026-10-19: the
 * sidebar keeping the open page's row in view sent a screen's first Tab past
 * "본문으로 건너뛰기"). With a margin it keeps that much room between the
 * element and the box's edges, and an element too tall for the room shows
 * from its top, or with `foot` its foot. `below` is the room kept under it
 * instead, for a box whose foot a sticky footer covers. Measured in the
 * box's own pixels (placeInScrollBox), however a transform draws it.
 */
export function scrollIntoViewNearest(element: HTMLElement | null, margin = 0, below = margin, foot = false) {
  if (!element) return;
  let box = element.parentElement;
  while (box && !(box.scrollHeight > box.clientHeight && /(auto|scroll)/u.test(getComputedStyle(box).overflowY))) {
    box = box.parentElement;
  }
  if (!box) return;
  const target = placeInScrollBox(element, box);
  const top = margin, bottom = target.boxHeight - below;
  if (target.top < top) box.scrollTop -= top - target.top;
  else if (target.bottom > bottom) box.scrollTop += foot ? target.bottom - bottom : Math.min(target.bottom - bottom, target.top - top);
}
