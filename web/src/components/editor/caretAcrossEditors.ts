import { Selection, TextSelection, type EditorState } from "@tiptap/pm/state";
import type { ResolvedPos } from "@tiptap/pm/model";
import type { EditorView } from "@tiptap/pm/view";
import { nativeTiptapEditorAround, nativeTiptapEditorsWithin } from "./nativeTiptapFocus";
import { mergedCellCaret } from "./simpleTableMerges";

// A synced copy of another page's original — a block or the page's whole
// body — draws its original with an editor of its own inside the page's. The
// caret goes into and out of it as it goes between any two lines, as
// Notion's does (seen 2026-11-27 in Chrome, two tabs at two times, copies of
// a block and of a page's body: ↓ from the line above, and → at its end, go
// to the copy's first line, ↑ and ← from the line below to its last; ↓ and →
// from its last line go to the line below, ↑ from its first to the line
// above; ↑↓ keep the caret's place across). Before, an arrow went past the
// copy to the line after it, and ↓ from its last line went to the page's
// line before it.

/** Whether the text at a place is drawn: one in a collapsed toggle is not. */
export function shown(view: EditorView, pos: number) {
  const dom = view.domAtPos(pos).node;
  const element = dom instanceof Element ? dom : dom.parentElement;
  return !!element && element.getClientRects().length > 0;
}

/**
 * The next text the reader can see from `pos` that way: a collapsed
 * toggle's lines are skipped; a table cell a merged cell stands over leads
 * into the merged cell, at its near end (Notion, seen 2026-11-10: ← at the
 * start of the cell right of one goes to its end), unless the caret is
 * leaving that merged cell.
 */
export function visibleText(view: EditorView, state: EditorState, pos: number, dir: 1 | -1, leaving?: ResolvedPos) {
  let found = Selection.findFrom(state.doc.resolve(pos), dir, true);
  while (found && !shown(view, found.from)) {
    const merged = mergedCellCaret(view, found.from, dir < 0);
    if (merged !== null && state.doc.resolve(merged).parent !== leaving?.parent) return TextSelection.create(state.doc, merged);
    found = Selection.findFrom(state.doc.resolve(dir > 0 ? found.$from.after() : found.$from.before()), dir, true);
  }
  return found;
}

/**
 * A place in `entry`'s text on its line nearest the way the caret came — its
 * first line going down, its last going up — at `left` across, held inside
 * the text's own width.
 */
export function atAcross(view: EditorView, entry: Selection, left: number) {
  const text = view.nodeDOM(entry.$head.before());
  const box = text instanceof Element ? text.getBoundingClientRect() : null;
  const line = view.coordsAtPos(entry.head);
  const x = box ? Math.min(Math.max(left, box.left + 1), box.right - 1) : left;
  const hit = view.posAtCoords({ left: x, top: (line.top + line.bottom) / 2 });
  return hit && view.state.doc.resolve(hit.pos).parent === entry.$head.parent ? TextSelection.create(view.state.doc, hit.pos) : entry;
}

type Way = { dir: 1 | -1; vertical: boolean; left: number; line?: { top: number; bottom: number } };

/**
 * The editor of a copy's original standing between `from` and the view's
 * next text that way (to its end with none), nearest first; a vertical move
 * takes only one below or above the caret's line, not one beside it in a
 * column. Null with none.
 */
function editorBetween(view: EditorView, from: number, entry: Selection | null, way: Way) {
  const { doc } = view.state;
  const [low, high] = way.dir > 0 ? [from, entry ? entry.from : doc.content.size] : [entry ? entry.to : 0, from];
  const found: EditorView[] = [];
  if (low < high) {
    doc.nodesBetween(low, high, (node, pos) => {
      if (node.type.name !== "nativeBlock") return true;
      const dom = view.nodeDOM(pos);
      if (!(dom instanceof Element)) return true;
      const inner = nativeTiptapEditorsWithin(dom, view);
      found.push(...inner);
      return inner.length === 0;
    });
  }
  const placed = found.filter((inner) => {
    if (!way.line) return true;
    const box = inner.dom.getBoundingClientRect();
    return way.dir > 0 ? box.top >= way.line.bottom - 2 : box.bottom <= way.line.top + 2;
  });
  return (way.dir > 0 ? placed : placed.reverse())[0] ?? null;
}

/** The caret into an editor at its edge the way it comes, deeper in where its edge is another's; false when it shows no text. */
function enter(view: EditorView, way: Way): boolean {
  if (!view.editable) return false;
  const edge = way.dir > 0 ? 0 : view.state.doc.content.size;
  const entry = visibleText(view, view.state, edge, way.dir);
  const deeper = editorBetween(view, edge, entry, { ...way, line: undefined });
  if (deeper && enter(deeper, way)) return true;
  if (!entry) return false;
  place(view, way.vertical ? atAcross(view, entry, way.left) : entry);
  return true;
}

function place(view: EditorView, selection: Selection) {
  view.dispatch(view.state.tr.setSelection(selection).scrollIntoView());
  view.focus();
}

/**
 * Past the edge of a copy's editor, into the editor around it: its next
 * text after the copy that way, or another copy's first; on out from there
 * at its edge too. False with no editor around.
 */
function leave(view: EditorView, way: Way): boolean {
  const outer = nativeTiptapEditorAround(view);
  if (!outer) return false;
  let holder: { pos: number; size: number } | null = null;
  outer.state.doc.descendants((node, pos) => {
    const dom = outer.nodeDOM(pos);
    if (!(dom instanceof Element) || !dom.contains(view.dom)) return false;
    if (node.type.name === "nativeBlock") holder = { pos, size: node.nodeSize };
    return true;
  });
  const held = holder as { pos: number; size: number } | null;
  if (!held) return false;
  const from = way.dir > 0 ? held.pos + held.size : held.pos;
  const entry = visibleText(outer, outer.state, from, way.dir);
  const inner = editorBetween(outer, from, entry, { ...way, line: undefined });
  if (inner && enter(inner, way)) return true;
  if (entry) {
    place(outer, way.vertical ? atAcross(outer, entry, way.left) : entry);
    return true;
  }
  return leave(outer, way);
}

/**
 * Moves the caret at a line's edge across into or out of a copy's editor
 * when that is where the arrow goes next (`entry`: the view's own next text
 * that way); false when the view moves it itself. At a copy's last line with
 * nothing after it anywhere, the caret stays in it.
 */
export function caretAcrossEditors(view: EditorView, $head: ResolvedPos, dir: 1 | -1, vertical: boolean, entry: Selection | null) {
  const here = view.coordsAtPos($head.pos);
  const way: Way = { dir, vertical, left: here.left, line: vertical ? { top: here.top, bottom: here.bottom } : undefined };
  const inner = editorBetween(view, dir > 0 ? $head.after() : $head.before(), entry, way);
  if (inner && enter(inner, way)) return true;
  if (entry) return false;
  return leave(view, way) || nativeTiptapEditorAround(view) !== null;
}
