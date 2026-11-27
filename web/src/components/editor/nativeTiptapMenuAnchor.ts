import type { Editor } from "@tiptap/core";
import type { SlashMenuAnchor } from "./SlashMenu";
import { MENU_GAP } from "./slashMenuPlace";

/**
 * Where a menu at the page's text stands, measured at that text each time it
 * is read: placed again as what is around it moves — the page scrolled, or
 * raised over a phone's keyboard opening after the menu did (2026-11-09: the
 * "@" list stayed where its line had been under the keyboard) — it is still
 * at its text. `shift` moves it along the line.
 */
export function nativeTiptapMenuAnchor(editor: Editor, position: number, shift = 0, caret = false, previous?: SlashMenuAnchor): SlashMenuAnchor {
  return anchorAt(editor, position, shift, caret, previous ? heldShifts.get(previous) ?? 0 : 0);
}

// How far along the line a menu chosen from a "/" or "+" menu's row stands
// from where its own menu would (nativeTiptapHeldMenuAnchor), passed on to
// the anchors measured as its query grows.
const heldShifts = new WeakMap<SlashMenuAnchor, number>();

function anchorAt(editor: Editor, position: number, shift: number, caret: boolean, held: number): SlashMenuAnchor {
  const measure = () => caret ? nativeTiptapCaretCoords(editor, position) : editor.view.coordsAtPos(position);
  let last = measure();
  const at = () => {
    if (editor.isDestroyed || position > editor.state.doc.content.size) return last;
    try { last = measure(); } catch { /* A position the page no longer has keeps its last place. */ }
    return last;
  };
  const anchor = { get left() { return at().left + shift + held; }, get top() { return at().top; }, get bottom() { return at().bottom; } };
  if (held) heldShifts.set(anchor, held);
  return anchor;
}

/**
 * Where the menu of an inline row chosen in a "/" or "+" menu stands — a
 * person's, a page's, "날짜 또는 리마인더"'s or "이모지"'s: along the line
 * where the caret stood as the row was chosen, at the end of the words typed
 * to find it, though they gave way to the "@" or ":", however much is typed
 * after; measured from the "@" or ":" as their own menus are, it follows
 * the page as it moves.
 * Notion's (seen 2026-11-15 at 1440, two tabs): "/날짜 또는" 537.6, "+" and
 * "날짜 또는" 533.0, "+" and "날짜" 501.2, "/person" 528.2 — each 22 left of
 * where the words ended, as an "@" list stands 22 left of its caret — and
 * "/emoji" 538.9, at their end, as an emoji menu stands at its anchor.
 */
export function nativeTiptapHeldMenuAnchor(editor: Editor, position: number, shift: number, caret: boolean, heldLeft: number): SlashMenuAnchor {
  return anchorAt(editor, position, shift, caret, heldLeft - anchorAt(editor, position, shift, caret, 0).left);
}

/**
 * Where a "+" line's "/" menu stands: 8px off the line's block, under it or
 * over it as it has room, as Notion's "+" menu (seen 2026-11-15 at 1440×723:
 * 308 under a new line's block ending at 300, 592 over one starting at 600),
 * where a typed "/"'s stands 12px off the caret (slashMenuPlace's MENU_GAP), and
 * 1.6px further out along the line (seen 2026-11-11). The block is read from
 * its top to its foot or where its first nested block starts, as the block
 * menu's (BlockHandle); without one it stands at the line's text, 6px lower.
 */
export function nativeTiptapLineMenuAnchor(editor: Editor, position: number): SlashMenuAnchor {
  const at = nativeTiptapMenuAnchor(editor, position, -1.6);
  const inset = MENU_GAP - 8;
  const block = () => {
    if (editor.isDestroyed) return null;
    try {
      const node = editor.view.domAtPos(position).node;
      const group = (node instanceof Element ? node : node.parentElement)?.closest("[data-block-id]");
      const own = group?.getBoundingClientRect();
      const nested = group?.querySelector("[data-block-id][data-depth]")?.getBoundingClientRect();
      return own ? { top: own.top, bottom: nested?.top ?? own.bottom } : null;
    } catch { return null; }
  };
  return { get left() { return at.left; }, get top() { const box = block(); return box ? box.top + inset : at.top; },
    get bottom() { const box = block(); return box ? box.bottom - inset : at.bottom + 6; } };
}

/**
 * Where the caret stands at a position, as the browser draws it: after an
 * "@" ending a right-to-left line that is the "@"'s left, where Notion's "@"
 * list starts from (seen 2026-11-11, a Hebrew line in a Korean page: 22px left
 * of it), though the text's own measure, reading the "@" as left-to-right,
 * puts it at its right. The text's own measure where the browser draws no caret there.
 */
export function nativeTiptapCaretCoords(editor: Editor, position: number) {
  return caretCoords(editor, position) ?? editor.view.coordsAtPos(position);
}
function caretCoords(editor: Editor, position: number) {
  let { node, offset } = editor.view.domAtPos(position, -1);
  // Between nodes the browser draws no caret; at the end of the text before, it does.
  const before = node.nodeType !== Node.TEXT_NODE && offset > 0 ? node.childNodes[offset - 1] : null;
  if (before?.nodeType === Node.TEXT_NODE) [node, offset] = [before, before.textContent?.length ?? 0];
  const range = document.createRange();
  range.setStart(node, offset);
  range.collapse(true);
  const box = range.getBoundingClientRect();
  return box.height > 0 ? { left: box.left, top: box.top, bottom: box.bottom } : null;
}
