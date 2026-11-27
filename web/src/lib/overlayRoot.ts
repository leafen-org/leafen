// The app's own "react-dom": what the app's code imports from "react-dom" is
// this module (scripts/portals-overlay-root.mjs), so a floating layer drawn
// into the page's body is drawn into one box at its end instead.
//
// Taking a box out of the body anywhere but its end restyles the whole page
// (the page's styles read its children's order), and a layer drawn straight
// into the body is taken out from before the one drawn after it: a cell's
// list, its backdrop first (2026-11-12: about 80–120 ms each time a cell's
// list closed, on a page with a table of 30 rows by 40 properties, on this
// Mac; 0.2 ms inside one box at the end).
import { createPortal as reactCreatePortal, flushSync } from "react-dom";

let root: HTMLElement | null = null;

/** The box at the end of the page's body every layer drawn into the body is drawn in. */
export function overlayRoot(): HTMLElement {
  if (root?.isConnected) return root;
  root = document.createElement("div");
  root.setAttribute("data-overlay-root", "");
  document.body.appendChild(root);
  return root;
}

/** React's createPortal, a layer for the body drawn into the box at its end (overlayRoot). */
export function createPortal(...[children, container, key]: Parameters<typeof reactCreatePortal>): ReturnType<typeof reactCreatePortal> {
  return reactCreatePortal(children, typeof document !== "undefined" && container === document.body ? overlayRoot() : container, key);
}

export { flushSync };
