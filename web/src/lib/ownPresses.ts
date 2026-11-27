import type { SyntheticEvent } from "react";

// React hands a portal's events up the tree that drew it, not the page's:
// a window drawn at the document's root from inside a sidebar row was the
// row's too, and a pick in the move window opened the page it moved from the
// row it was opened from (2026-11-12; Enter, read on the window, did not).

const keep = (event: SyntheticEvent) => event.stopPropagation();

/**
 * A window's presses are its own: nothing that drew it hears them. Keys go on
 * (the layer's Escape is read on the window). For a window with no links,
 * since the router follows a link by the document's click.
 */
export const ownPresses = {
  onClick: keep, onDoubleClick: keep, onAuxClick: keep, onContextMenu: keep, onMouseDown: keep, onPointerDown: keep,
} as const;

/** Whether a press heard by an element was on it, not on a window it drew at the document's root (a sheet, a dialog). */
export function pressedHere(event: { currentTarget: EventTarget; target: EventTarget }) {
  return event.currentTarget instanceof Node && event.target instanceof Node && event.currentTarget.contains(event.target);
}
