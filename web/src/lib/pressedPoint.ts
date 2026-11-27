import type { PopoverRect } from "./popoverPlacement";

/**
 * Where a pointer pressed, which a menu the press opens hangs from
 * (placeAtPoint): the pixel under the pointer, a 1px box, the menu whole —
 * its top 1px under the point where it fits under it, else its bottom at the
 * point where it fits over it, else under it moved up into the window.
 * Notion's menus at a press (seen 2026-11-18 in Chrome at 1440, dpr 2, a 723
 * window: a database row's right click at 313 opened at 314 and, too tall
 * either way, moved up to 252..711, at 498 and at 683 over it ending there;
 * a sidebar row's right click at 254 at 255, at 628 over it ending at 628; a
 * sidebar row's "⋯" pressed at 255 at 256; a synced block's head "⋯" at 233
 * at 234; the selection toolbar's "⋯" at 585 over it ending at 585). A point
 * worked out from a box (a key's, a field's) is no press and stays where given.
 */
export type PressedPoint = { x: number; y: number; pressed: true };

export function pressedPoint(event: { clientX: number; clientY: number }): PressedPoint {
  return { x: event.clientX, y: event.clientY, pressed: true };
}

/** The box a press's menu hangs from: the pixel under the pointer. */
export function pressedBox(point: { x: number; y: number; contextElement?: Element }): PopoverRect {
  return { left: point.x, right: point.x, top: point.y, bottom: point.y + 1, contextElement: point.contextElement };
}
