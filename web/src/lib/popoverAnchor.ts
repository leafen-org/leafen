import { getOverflowAncestors } from "@floating-ui/dom";
import type { PopoverRect } from "./popoverPlacement";

/** An opener as popoverAnchorOf reads it. */
export type PopoverAnchor = DOMRect & { contextElement: Element };

/**
 * A popover's opener as its placing reads it (usePopoverPlacement): the
 * element where it stands now, read again at each placing, so the popover
 * follows it on scroll and resize rather than freezing where it opened.
 * One gone from the page meanwhile keeps the place it last had, and reads
 * the way of what held it: a detached element reads all zeros, which put the
 * popover in the window's corner, and no direction, which turned a right-to-
 * left screen's popover to the left-to-right side (2026-11-19: an inline
 * equation's window jumped from under its line to 12,12 once its TeX was
 * emptied, its piece of the line drawn anew under it). No element gives no
 * anchor: an opener read after its press (an event's currentTarget once the
 * event has gone by) is null, and threw here in a render (2026-11-21: a
 * library column's menu, opened from a state updater run again).
 */
export function popoverAnchorOf(element: Element): PopoverAnchor;
export function popoverAnchorOf(element: Element | null | undefined): PopoverAnchor | null;
export function popoverAnchorOf(element: Element | null | undefined): PopoverAnchor | null {
  if (!element) return null;
  let last = element.isConnected ? element.getBoundingClientRect() : undefined;
  const rect = () => (element.isConnected ? (last = element.getBoundingClientRect()) : last ?? element.getBoundingClientRect());
  const standing = standingElement(element);
  return {
    get contextElement() { return standing(); },
    get x() { return rect().x; },
    get y() { return rect().y; },
    get top() { return rect().top; },
    get bottom() { return rect().bottom; },
    get left() { return rect().left; },
    get right() { return rect().right; },
    get width() { return rect().width; },
    get height() { return rect().height; },
    toJSON: () => rect().toJSON(),
  };
}
export function popoverRectOf(element: Element | null | undefined): PopoverRect | null {
  return popoverAnchorOf(element);
}

/**
 * Where an opener stood as its popover opened, moved only as what holds it
 * scrolls: the opener redrawn, grown or shrunk meanwhile leaves the popover
 * where it is. Notion's inline equation window stays so while its TeX is
 * typed, emptied and typed again (measured 2026-11-19 in Chrome 1440, ko:
 * at 611,260.5 throughout while its piece went from 134 wide at 611 to 19
 * wide at 607, and at 554,257.5 for an equation pressed in the line).
 */
export function popoverAnchorHeld(opener: PopoverRect): PopoverRect {
  const at = { top: opener.top, bottom: opener.bottom, left: opener.left, right: opener.right };
  const element = opener.contextElement, standing = element && standingElement(element);
  const holders = element ? getOverflowAncestors(element) : [];
  const start = holders.map(scrolled);
  const moved = (axis: "x" | "y") => holders.reduce((sum, holder, index) => sum + start[index][axis] - scrolled(holder)[axis], 0);
  return {
    get contextElement() { return standing?.(); },
    get top() { return at.top + moved("y"); },
    get bottom() { return at.bottom + moved("y"); },
    get left() { return at.left + moved("x"); },
    get right() { return at.right + moved("x"); },
  };
}
/** The element itself while it is in the page, else what held it as the
 * popover opened, if that still is: the way the screen reads there
 * (lib/readingDirection) is read from a place in the page. */
function standingElement(element: Element) {
  const holder = element.parentElement;
  return () => (element.isConnected || !holder?.isConnected ? element : holder);
}
function scrolled(holder: Element | Window | VisualViewport) {
  return holder instanceof Element ? { x: holder.scrollLeft, y: holder.scrollTop }
    : "scrollX" in holder ? { x: holder.scrollX, y: holder.scrollY } : { x: 0, y: 0 };
}
