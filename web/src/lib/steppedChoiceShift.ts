import { imeComposing } from "./fieldKeyBoundary";

/**
 * A choice a step of the page's ⌘Z or ⌘⇧Z leaves — chosen blocks, a view's
 * rows or cards, the two joined — takes the first Shift+↑ or Shift+↓ after
 * it as nothing, the next going on from it as ever, as Notion's (seen
 * 2026-11-27 in Chrome at 1440, judged after each key, in two tabs at two
 * times: a line deleted, a table's row trashed, a joined choice cut or
 * deleted, then ⌘Z — the first Shift+↑ or Shift+↓ left the choice as it
 * was, the second stretched it). Any other key or a press first ends it; a
 * field with the focus, or a view's chosen cells, keep their own.
 */
const CHOSEN = '[data-block-id][data-selected="true"], [data-row-selected="true"], [data-card-chosen]:not([hidden])';
const MODIFIERS = new Set(["Shift", "Meta", "Control", "Alt"]);
let stepped = false, heard = false;

function shiftStep(event: KeyboardEvent) {
  return event.shiftKey && !event.metaKey && !event.ctrlKey && !event.altKey && (event.key === "ArrowUp" || event.key === "ArrowDown");
}

function typing() {
  const focus = document.activeElement;
  return focus instanceof HTMLElement && (focus.isContentEditable || focus.matches("input, textarea, select"));
}

/** A step of the page's history was taken: the choice it leaves takes its first Shift+↑ or ↓ as nothing. */
export function noteChoiceStepped() {
  stepped = true;
  if (heard || typeof window === "undefined") return;
  heard = true;
  // Before any view's or the page's own keys (the window's capture comes first).
  window.addEventListener("keydown", (event) => {
    if (!stepped || MODIFIERS.has(event.key)) return;
    stepped = false;
    if (!shiftStep(event) || imeComposing(event) || typing() || !document.querySelector(CHOSEN)) return;
    event.preventDefault();
    event.stopImmediatePropagation();
  }, true);
  window.addEventListener("pointerdown", () => { stepped = false; }, true);
}
