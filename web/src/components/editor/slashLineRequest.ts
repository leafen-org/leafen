// A block's "+" makes a line and opens the "/" menu on it, its own words the
// menu's filter, as Notion's does (seen 2026-10-18): typing narrows the menu,
// a choice turns the line into the block, Escape leaves the words a line.
// The line may take the caret after the request.

export const SLASH_LINE_REQUEST = "leafen:slash-line-request";

const requested = new Set<string>();

export function requestSlashLine(blockId: string) {
  requested.add(blockId);
  window.dispatchEvent(new Event(SLASH_LINE_REQUEST));
}

/** Whether the line was asked for, taking the request. */
export function takeSlashLine(blockId: string) {
  return requested.delete(blockId);
}

/**
 * Whether a press beside the "+" line's menu went on to another field — the page's title, a property's value — and
 * put the caret there: its closing then leaves it there, the line's blank staying behind, as Notion's (2026-11-26 in
 * Chrome at 1440, two tabs at two times, judged by the screen and one letter after the press with nothing run in the
 * page before it: the title pressed took the letter). Escape, Tab and a press on the editor's own words keep the
 * editor, which takes the focus back.
 */
export function pressLeftForField(editor: Element) {
  const active = document.activeElement;
  return active instanceof HTMLElement && !editor.contains(active) && (active.isContentEditable || active.matches("input, textarea, select"));
}
