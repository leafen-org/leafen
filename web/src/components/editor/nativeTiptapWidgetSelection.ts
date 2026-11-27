import { Extension } from "@tiptap/core";
import { NodeSelection, Plugin, PluginKey, Selection, TextSelection, type EditorState } from "@tiptap/pm/state";
import type { ResolvedPos } from "@tiptap/pm/model";
import { Decoration, DecorationSet, type EditorView } from "@tiptap/pm/view";
import { lineArrowStep } from "@/lib/readingDirection";
import { requestEditorBlockCommand, requestEditorBlockSelection } from "./editorCommands";
import { mergedCellCaret, tableCellElementAt } from "./simpleTableMerges";
import { caretPlacedByChange, leaveTurnedLine } from "./caretLineTurnedTextless";
import { fieldEnter, imeComposing } from "@/lib/fieldKeyBoundary";

// The caret goes from text to text, as in Notion (seen 2026-10-06): an arrow
// at a line's edge goes on into a table's cells (the first going on, the last
// going back) and a synced block's lines, and past a block with no text. A
// block's own controls (a table's grid, a synced block's header, an image)
// are no place for it: selected, they show nothing, and what is typed there
// never reaches the page. The browser's own moves skip a table's cells, so the
// moves between blocks are made here.

function onWidget(selection: Selection): selection is NodeSelection {
  return selection instanceof NodeSelection && selection.node.type.name === "nativeWidget";
}

// A caret where a block has no text — beside a file's or an image's controls
// — takes no typing either: the browser wrote the "/" there into the screen
// alone, and it was gone after a reload (2026-10-16).
// Only inside a block: on an empty page the caret stands in the page itself,
// and typing there makes the first line.
function offText(selection: Selection) {
  return onWidget(selection) || selection.empty && selection.$from.parent.type.name === "nativeBlock";
}

/** The block a selection off text stands in, by its id in the page. */
function blockOf(selection: Selection) {
  for (let depth = selection.$from.depth; depth > 0; depth--) {
    const node = selection.$from.node(depth);
    if (node.type.name === "nativeBlock") return String(node.attrs.sourceId || node.attrs.id || "");
  }
  return "";
}

function shown(view: EditorView, pos: number) {
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
function visibleText(view: EditorView, state: EditorState, pos: number, dir: 1 | -1, leaving?: ResolvedPos) {
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
function atAcross(view: EditorView, entry: Selection, left: number) {
  const text = view.nodeDOM(entry.$head.before());
  const box = text instanceof Element ? text.getBoundingClientRect() : null;
  const line = view.coordsAtPos(entry.head);
  const x = box ? Math.min(Math.max(left, box.left + 1), box.right - 1) : left;
  const hit = view.posAtCoords({ left: x, top: (line.top + line.bottom) / 2 });
  return hit && view.state.doc.resolve(hit.pos).parent === entry.$head.parent ? TextSelection.create(view.state.doc, hit.pos) : entry;
}

/** The text straight above or below the caret's line, where the screen shows one near. */
function textAcross(view: EditorView, $head: ResolvedPos, dir: 1 | -1) {
  const here = view.coordsAtPos($head.pos);
  for (let step = 1, y = dir > 0 ? here.bottom : here.top; step <= 60; step++) {
    y += dir * 6;
    const hit = view.posAtCoords({ left: here.left, top: y });
    if (!hit) continue;
    const $hit = view.state.doc.resolve(hit.pos);
    // Over a callout's icon or edge its lines are simply next — going in,
    // its first line down or its last up, coming out the line after or
    // before it — at the caret's place across, as Notion's (seen 2026-11-14:
    // ↓ from a quote's "인용 하|나" went to "콜아|웃 하나", not its start).
    if (!$hit.parent.inlineContent && Array.from({ length: $hit.depth + 1 }, (_, depth) => $hit.node(depth)).some(node => node.attrs.kind === "callout")) {
      const entry = visibleText(view, view.state, dir > 0 ? $head.after() : $head.before(), dir, $head);
      return entry ? atAcross(view, entry, here.left) : null;
    }
    if (!$hit.parent.inlineContent || $hit.parent === $head.parent || !shown(view, hit.pos)) continue;
    // The hit may be in the text's padding; its line's middle gives the place across it.
    const line = view.coordsAtPos(hit.pos);
    const exact = view.posAtCoords({ left: here.left, top: (line.top + line.bottom) / 2 });
    const pos = exact && view.state.doc.resolve(exact.pos).parent === $hit.parent ? exact.pos : hit.pos;
    return TextSelection.create(view.state.doc, pos);
  }
  return null;
}

/** Where the simple table holding a place starts, or null outside one. */
function tableAt($pos: ResolvedPos) {
  for (let depth = $pos.depth; depth > 0; depth--) {
    const node = $pos.node(depth);
    if (node.type.name === "nativeBlock" && node.attrs.kind === "table") return $pos.before(depth);
  }
  return null;
}

/**
 * Down into a simple table from a line outside it goes to its first cell and
 * up to its last, as → and ← do, at the caret's place across when that stands
 * over the cell, else at the cell's start going down and its end going up
 * (Notion, seen 2026-11-11: down from over the second column went to the
 * first cell's start, up from over the first column or the table's edge to
 * the last cell's end). `entry` is the next text that way; the table it is in
 * stands below or above the caret's line, not beside it in a column.
 */
function intoTable(view: EditorView, $head: ResolvedPos, dir: 1 | -1, across: Selection | null, entry: Selection | null) {
  const table = entry ? tableAt(entry.$head) : null;
  if (!entry || table === null || table === tableAt($head)) return across;
  const box = (view.nodeDOM(table) as HTMLElement | null)?.getBoundingClientRect();
  const here = view.coordsAtPos($head.pos);
  if (!box || (dir > 0 ? box.top < here.bottom - 2 : box.bottom > here.top + 2)) return across;
  const cell = tableCellElementAt(view, entry.from)?.getBoundingClientRect();
  const { left } = here;
  if (!cell || left < cell.left || left > cell.right) return entry;
  const line = view.coordsAtPos(entry.from);
  const hit = view.posAtCoords({ left, top: (line.top + line.bottom) / 2 });
  return hit && view.state.doc.resolve(hit.pos).parent === entry.$head.parent ? TextSelection.create(view.state.doc, hit.pos) : entry;
}

function arrow(view: EditorView, key: "left" | "right" | "up" | "down") {
  const { state } = view;
  const { selection } = state;
  if (imeComposing(view) || !(selection instanceof TextSelection) || !selection.empty || !selection.$head.depth) return false;
  const $head = selection.$head;
  const vertical = key === "up" || key === "down";
  // ← and → step the way the line reads, one with no letter left to right (lineArrowStep).
  const dir: 1 | -1 = vertical ? (key === "down" ? 1 : -1) : lineArrowStep(key === "left" ? "ArrowLeft" : "ArrowRight",
    view.domAtPos($head.pos).node.parentElement ?? view.dom, $head.parent.textContent);
  // ← or → beside a mention, a date or an inline equation goes past it at
  // once, as Notion's (seen 2026-11-14: Home then → put the caret after a
  // date mention; the engine's own step first chose the token unseen, and a
  // second press went past it).
  const token = vertical || $head.textOffset ? null : dir > 0 ? $head.nodeAfter : $head.nodeBefore;
  if (token?.type.name === "nativeAtom") {
    view.dispatch(state.tr.setSelection(TextSelection.create(state.doc, $head.pos + dir * token.nodeSize)).scrollIntoView());
    return true;
  }
  if (!view.endOfTextblock(vertical ? key : dir > 0 ? "forward" : "backward")) return false;
  const from = dir > 0 ? $head.after() : $head.before();
  const entry = visibleText(view, state, from, dir, $head);
  const target = (vertical ? intoTable(view, $head, dir, textAcross(view, $head, dir), entry) : null) ?? entry;
  if (!target) return false;
  view.dispatch(state.tr.setSelection(target).scrollIntoView());
  return true;
}

// A selection dragged across blocks stops before a code block, a picture, a
// bookmark or a file on its way, as Notion's (seen 2026-11-05/06: its text
// ends at the last line before one; a simple table, an equation, a divider, a
// video, an audio, a PDF and an embed let it through, the table's cells
// chosen too; seen 2026-11-09: so do an inline and a full-page database, the
// line after one chosen).
const SELECTION_STOPS: ReadonlySet<string> = new Set(["code", "image", "file", "bookmark", "link_preview"]);
// Shift+↑↓ stops before an inline database too, any view, the next press
// choosing it as a block, as Notion's (seen 2026-11-14: Shift+↓ at a line's
// end over a table, board or other view chose the line, then the view).
const KEY_STOPS: ReadonlySet<string> = new Set([...SELECTION_STOPS, "inline_database"]);

function stoppedSelection(doc: EditorState["doc"], $anchor: ResolvedPos, $head: ResolvedPos, stops = SELECTION_STOPS) {
  if ($anchor.sameParent($head)) return null;
  const forward = $head.pos > $anchor.pos;
  const from = Math.min($anchor.pos, $head.pos), to = Math.max($anchor.pos, $head.pos);
  let stop: { from: number; to: number } | null = null;
  doc.nodesBetween(from, to, (node, pos) => {
    if (stop && forward) return false;
    if (node.type.name !== "nativeBlock" || !stops.has(String(node.attrs.kind))) return true;
    // The block the drag started in isn't in its way.
    if (pos < $anchor.pos && $anchor.pos < pos + node.nodeSize) return true;
    stop = { from: pos, to: pos + node.nodeSize };
    return false;
  });
  const found = stop as { from: number; to: number } | null;
  if (!found) return null;
  const end = forward ? Selection.findFrom(doc.resolve(found.from), -1, true) : Selection.findFrom(doc.resolve(found.to), 1, true);
  const head = end ? (forward ? end.$to.pos : end.$from.pos) : $anchor.pos;
  return TextSelection.create(doc, $anchor.pos, forward ? Math.max(head, $anchor.pos) : Math.min(head, $anchor.pos));
}

/** Whether words Shift+↑↓ chooses from `$anchor` on to `$head` run into a block that stops them (KEY_STOPS). */
export function selectionStopsBefore(doc: EditorState["doc"], $anchor: ResolvedPos, $head: ResolvedPos) {
  return stoppedSelection(doc, $anchor, $head, KEY_STOPS) !== null;
}

// A drag begun in a code block's code or in a caption chooses that field's
// text only, from the press to the field's end or start however far it goes
// on, as Notion's (seen 2026-11-07 and 11-09: a Mermaid block's code dragged
// on into the line under it, an image's caption dragged up or down out of
// it). A drag begun in a line may still run across blocks.
const KEPT_FIELDS: ReadonlySet<string> = new Set(["nativeCode", "nativeCaption"]);
const dragging = new WeakSet<EditorView>();
// Where the pointer is while a drag goes on.
const pointers = new WeakMap<EditorView, { left: number; top: number }>();

function keptInField(view: EditorView, $anchor: ResolvedPos, $head: ResolvedPos) {
  if (!dragging.has(view) || !KEPT_FIELDS.has($anchor.parent.type.name) || $anchor.sameParent($head)) return null;
  return TextSelection.create(view.state.doc, $anchor.pos, $head.pos > $anchor.pos ? $anchor.end() : $anchor.start());
}

// A drag let go past its line as below, run on forward, runs out of the line
// into the block after it, as Notion's selection does (seen 2026-11-09: its
// text then ends "\n"): the text toolbar counts that line to its right edge.
const ranOut = new WeakMap<EditorView, Selection>();
export function selectionRanOutOfLine(view: EditorView, selection: Selection) {
  return ranOut.get(view)?.eq(selection) ?? false;
}

// Let go on a block with no text of its own (a database, a page, a divider)
// or in a simple table's cell, a drag begun outside it chooses the text up to
// it only, as Notion's (seen 2026-11-09: from "R|x one two three" onto a
// database, "x one two three"; from a line into the table under it, that
// line's words to its end, the cells unpainted; from the line under the table
// up into its last cell, that line's words from its start). Run on past
// either, it chooses on to the line where it is let go.
function landedOffText(view: EditorView, $anchor: ResolvedPos, $head: ResolvedPos) {
  if (!dragging.has(view) || $anchor.sameParent($head)) return null;
  const forward = $head.pos > $anchor.pos;
  const { doc } = view.state;
  // The browser puts the drag's end in the text after such a block: the
  // pointer says it is over the block's own part (a callout's icon is its
  // lines' edge, and the block the drag began in is its own).
  const pointer = pointers.get(view), hit = pointer ? view.posAtCoords(pointer) : null;
  const $over = hit && hit.inside >= 0 && doc.nodeAt(hit.inside)?.type.name === "nativeWidget" ? doc.resolve(hit.inside) : null;
  const overBlock = $over && $over.depth > 0 && $over.parent.attrs.kind !== "callout"
    && !($anchor.pos > $over.before() && $anchor.pos < $over.after());
  let edge: number | null = overBlock ? (forward ? $over.before() : $over.after())
    : $head.parent.inlineContent ? null : $head.pos;
  for (let depth = $head.depth; edge === null && depth > 0; depth--) {
    const node = $head.node(depth);
    if (node.type.name !== "nativeBlock" || node.attrs.kind !== "table") continue;
    const start = $head.before(depth), end = $head.after(depth);
    // A drag among a table's own cells is the table's (useSimpleTableCellSelection).
    if ($anchor.pos > start && $anchor.pos < end) return null;
    edge = forward ? start : end;
  }
  if (edge === null) return null;
  const back = Selection.findFrom(doc.resolve(edge), forward ? -1 : 1, true);
  const head = back ? (forward ? back.$to.pos : back.$from.pos) : $anchor.pos;
  const chosen = TextSelection.create(doc, $anchor.pos, forward ? Math.max(head, $anchor.pos) : Math.min(head, $anchor.pos));
  // Onto an inline database it stays by the words, each time (Notion's, seen 2026-11-13).
  if (forward && !(overBlock && $over?.parent.attrs.kind === "inline_database")) ranOut.set(view, chosen);
  return chosen;
}

function fieldBox(view: EditorView, start: number) {
  const dom = view.domAtPos(start).node;
  return (dom instanceof Element ? dom : dom.parentElement)?.closest("[data-rt-editable]")?.getBoundingClientRect() ?? null;
}

// Let go over a part with no text (a caption's picture, a diagram under its
// code), the browser leaves the caret where it was pressed: the drag still
// reaches the field's start above it or its end below it, as Notion's.
function releasedOutside(view: EditorView, doc: EditorState["doc"], pressedAt: number, up: MouseEvent) {
  const { state } = view;
  if (state.doc !== doc) return;
  const $press = doc.resolve(pressedAt);
  const anchor = state.selection.anchor >= $press.start() && state.selection.anchor <= $press.end() ? state.selection.anchor : pressedAt;
  const box = fieldBox(view, $press.start());
  if (!box || (up.clientY >= box.top && up.clientY <= box.bottom)) return;
  const kept = TextSelection.create(doc, anchor, up.clientY < box.top ? $press.start() : $press.end());
  if (!state.selection.eq(kept)) view.dispatch(state.tr.setSelection(kept));
}

/**
 * A drag begun on a code block's colored copy, which holds no text the
 * browser could choose: from the caret the press has just put in the code,
 * its words as far as the pointer goes, kept in the code as any drag begun
 * there.
 */
export function followKeptDrag(view: EditorView) {
  const window = view.dom.ownerDocument.defaultView ?? globalThis.window;
  const anchor = view.state.selection.anchor;
  if (!KEPT_FIELDS.has(view.state.selection.$anchor.parent.type.name)) return;
  const move = (event: MouseEvent) => {
    if (!(event.buttons & 1) || view.isDestroyed) { stop(); return; }
    const { state } = view;
    const $anchor = state.doc.resolve(Math.min(anchor, state.doc.content.size));
    const box = KEPT_FIELDS.has($anchor.parent.type.name) ? fieldBox(view, $anchor.start()) : null;
    if (!box) return;
    let head = event.clientY < box.top ? $anchor.start() : $anchor.end();
    if (event.clientY >= box.top && event.clientY <= box.bottom) {
      const hit = view.posAtCoords({ left: Math.min(Math.max(event.clientX, box.left + 1), box.right - 1), top: event.clientY });
      if (hit && hit.pos >= $anchor.start() && hit.pos <= $anchor.end()) head = hit.pos;
    }
    const next = TextSelection.create(state.doc, $anchor.pos, head);
    if (!state.selection.eq(next)) view.dispatch(state.tr.setSelection(next));
  };
  const stop = () => {
    window.removeEventListener("mousemove", move, true);
    window.removeEventListener("mouseup", stop, true);
  };
  window.addEventListener("mousemove", move, true);
  window.addEventListener("mouseup", stop, true);
}

/** Where the selection a drag makes stands from: the browser's, else the press. */
function dragAnchor(view: EditorView, pressedAt: number | null) {
  const dom = view.dom.ownerDocument.getSelection();
  if (dom?.anchorNode && view.dom.contains(dom.anchorNode)) {
    try { return view.posAtDOM(dom.anchorNode, dom.anchorOffset); } catch { /* not the page's own text */ }
  }
  return pressedAt ?? view.state.selection.anchor;
}

// Over a block with no text of its own the browser may leave its selection
// where the drag began — an inline database's empty cells and its "+ 새
// 페이지" line take none — so the drag chooses the text up to that block
// itself, as it would let go there (landedOffText), and goes on showing it
// as Notion's does (seen 2026-11-13: from "끝| 줄" onto the table under it,
// " 줄" chosen, its text toolbar under the words).
function followOverBlock(view: EditorView, pressedAt: number | null) {
  const pointer = pointers.get(view), hit = pointer ? view.posAtCoords(pointer) : null;
  const { doc } = view.state;
  if (!hit || hit.inside < 0 || doc.nodeAt(hit.inside)?.type.name !== "nativeWidget") return false;
  // A callout's icon is its lines' edge, where the browser's own selection goes on.
  const $over = doc.resolve(hit.inside);
  if ($over.depth === 0 || $over.parent.attrs.kind === "callout") return false;
  const chosen = landedOffText(view, doc.resolve(Math.min(dragAnchor(view, pressedAt), doc.content.size)), doc.resolve(hit.pos));
  if (chosen && !view.state.selection.eq(chosen)) view.dispatch(view.state.tr.setSelection(chosen));
  return Boolean(chosen);
}

// A drag from a line's end to the next line's start chooses only the break
// between them, which shows nothing: let go, it is the caret at the first
// line's end, whichever way it went, as Notion's (seen 2026-11-13: ⌫ takes
// its last letter, Enter makes a line under it; measured 2026-11-15 in Chrome
// at 1440 with no script in the page: a typed "z" went after the first line's
// last letter, the lines kept apart). Any character or other block between
// them keeps the choice.
function breakOnly(state: EditorState) {
  const { selection, doc } = state, { $from, $to } = selection;
  if (!(selection instanceof TextSelection) || selection.empty || $from.sameParent($to)
    || !$from.parent.inlineContent || !$to.parent.inlineContent
    || $from.parentOffset !== $from.parent.content.size || $to.parentOffset !== 0) return null;
  let lines = 0, other = false;
  doc.nodesBetween($from.pos, $to.pos, node => {
    if (node.inlineContent) lines++;
    else if (node.isLeaf) other = true;
    return !other;
  });
  return lines === 2 && !other ? TextSelection.create(doc, $from.pos) : null;
}

// From a press until the editor has put the drag's selection back on the
// screen after its release (it does that a turn after the mouseup).
function pressed(view: EditorView, event: MouseEvent) {
  if (event.button !== 0) return false;
  dragging.add(view);
  const at = event.shiftKey ? null : view.posAtCoords({ left: event.clientX, top: event.clientY });
  const field = at && KEPT_FIELDS.has(view.state.doc.resolve(at.pos).parent.type.name) ? { doc: view.state.doc, at: at.pos } : null;
  const window = view.dom.ownerDocument.defaultView ?? globalThis.window;
  const start = { x: event.clientX, y: event.clientY };
  let away = false;
  const moved = (move: MouseEvent) => {
    pointers.set(view, { left: move.clientX, top: move.clientY });
    away ||= Math.abs(move.clientX - start.x) >= 4 || Math.abs(move.clientY - start.y) >= 4;
    if (away && move.buttons & 1 && !field && !view.isDestroyed) followOverBlock(view, at?.pos ?? null);
  };
  pointers.set(view, { left: event.clientX, top: event.clientY });
  const released = (up: MouseEvent) => {
    window.removeEventListener("mouseup", released, true);
    window.removeEventListener("mousemove", moved, true);
    pointers.set(view, { left: up.clientX, top: up.clientY });
    if (field && !view.isDestroyed) releasedOutside(view, field.doc, field.at, up);
    // A drag let go over such a block is no press on it: its "+ 새 페이지"
    // makes no row (Notion's, seen 2026-11-13).
    if (away && !field && !view.isDestroyed && followOverBlock(view, at?.pos ?? null)) {
      const swallow = (click: MouseEvent) => { click.preventDefault(); click.stopPropagation(); };
      window.addEventListener("click", swallow, { capture: true, once: true });
      window.setTimeout(() => window.removeEventListener("click", swallow, true), 0);
    }
    window.setTimeout(() => window.setTimeout(() => {
      dragging.delete(view); pointers.delete(view);
      const caret = away && !field && !view.isDestroyed ? breakOnly(view.state) : null;
      if (caret) view.dispatch(view.state.tr.setSelection(caret));
    }));
  };
  window.addEventListener("mousemove", moved, true);
  window.addEventListener("mouseup", released, true);
  return false;
}

/**
 * A block with no text the editor's selection is left on — an app's block,
 * an HTML block or an embed made by "/" — is chosen as its handle chooses it:
 * washed blue, the keys then the chosen block's (Backspace takes away what
 * shows chosen, ↑↓ move the choice). Left as the editor's own selection on
 * its controls it showed nothing, Escape and the arrows did nothing, and a
 * Backspace took the block away unseen (2026-11-15). One with text of its
 * own has the selection moved into that text first (appendTransaction below).
 * Only a selection that has just come there with the editor holding the focus
 * — by a key, a block taken away, a "/" — is such a choice: the editor's own
 * first selection, on a page that starts with such a block, or one left while
 * the editor had no focus, is none. Chosen as a press on a line brought the
 * focus in, it took the press's caret away and left the page's first block
 * chosen off screen (2026-11-15); Notion's page opens with nothing chosen and
 * a press on any line puts the caret there.
 */
function chosenWithoutText(selection: Selection) {
  return onWidget(selection) ? blockOf(selection) : "";
}
const pressedSelectionKey = new PluginKey<boolean>("nativeWidgetPressedSelection");

export const NativeTiptapWidgetSelection = Extension.create({
  name: "nativeWidgetSelection", priority: 1050,
  addKeyboardShortcuts() {
    const view = () => this.editor.view;
    return {
      ArrowRight: () => arrow(view(), "right"), ArrowLeft: () => arrow(view(), "left"),
      ArrowDown: () => arrow(view(), "down"), ArrowUp: () => arrow(view(), "up"),
    };
  },
  addProseMirrorPlugins() {
    const nowhere = (state: EditorState) => offText(state.selection);
    return [new Plugin({
      key: pressedSelectionKey,
      // Whether the selection was last set by a press: a press on a block's
      // controls already chooses the block its own way (BlockFrame's select,
      // ⌘ and Shift adding to the choice).
      state: {
        init: () => false,
        apply: (tr, pressed: boolean) => tr.selectionSet ? Boolean(tr.getMeta("pointer")) : pressed,
      },
      view: () => ({
        update: (view, before) => {
          const { selection } = view.state;
          const moved = !selection.eq(before.selection) && view.editable && view.hasFocus();
          // Its own line turned a page, nothing is chosen there: the caret goes as leaveTurnedLine says.
          if (moved && leaveTurnedLine(view, before)) return;
          const blockId = moved && !pressedSelectionKey.getState(view.state) ? chosenWithoutText(selection) : "";
          // Past the transaction that left it there: choosing lets go of the editor's focus.
          if (blockId) queueMicrotask(() => {
            if (view.state.selection.eq(selection) && view.hasFocus()) requestEditorBlockSelection(blockId, false, view.dom);
          });
        },
      }),
      // A click on a block's controls puts the caret in the block's own text.
      appendTransaction: (transactions, _before, state) => {
        if (!transactions.some(transaction => transaction.selectionSet) || !onWidget(state.selection)) return null;
        const inside = Selection.findFrom(state.doc.resolve(state.selection.to), 1, true);
        return inside && inside.from <= state.selection.$from.end() ? state.tr.setSelection(inside) : null;
      },
      props: {
        // An embed a selection of text runs through is washed blue as a chosen
        // block is (Notion's, seen 2026-11-06: its frame under the selection's
        // blue; a video's, an audio's or a PDF's frame isn't).
        decorations: (state) => {
          const { selection, doc } = state;
          if (!(selection instanceof TextSelection) || selection.empty || selection.$from.sameParent(selection.$to)) return null;
          const washed: Decoration[] = [];
          doc.nodesBetween(selection.from, selection.to, (node, pos) => {
            if (node.type.name !== "nativeBlock") return true;
            if (node.attrs.kind === "embed" && pos >= selection.from && pos + node.nodeSize <= selection.to) {
              washed.push(Decoration.node(pos, pos + node.nodeSize, { "data-text-selected-through": "true" }));
            }
            return true;
          });
          return washed.length ? DecorationSet.create(doc, washed) : null;
        },
        createSelectionBetween: (view, $anchor, $head) => keptInField(view, $anchor, $head)
          ?? stoppedSelection(view.state.doc, $anchor, $head) ?? landedOffText(view, $anchor, $head),
        handleTextInput: (view) => nowhere(view.state),
        handleKeyDown: (view, event) => {
          if (!nowhere(view.state) || event.metaKey || event.ctrlKey || event.altKey) return false;
          // Enter makes a line after the block, as on a selected image in Notion.
          if (fieldEnter(event) && !event.shiftKey) {
            const blockId = blockOf(view.state.selection);
            return Boolean(blockId) && requestEditorBlockCommand(blockId, "lineBelow", undefined, view.dom);
          }
          return event.key.length === 1;
        },
        handleDOMEvents: {
          mousedown: pressed,
          beforeinput: (view, event) => {
            if (!nowhere(view.state) || !(event as InputEvent).inputType.startsWith("insert")) return false;
            event.preventDefault();
            return true;
          },
        },
      },
    }), caretPlacedByChange];
  },
});
