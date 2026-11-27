import type { Node as ProseMirrorNode } from "@tiptap/pm/model";
import { Plugin, PluginKey, type EditorState } from "@tiptap/pm/state";
import type { EditorView } from "@tiptap/pm/view";
import { focusTitleStartInPlace } from "./focus";
import { nativeProjectionMeta } from "./nativeTiptapBinding";

const placedKey = new PluginKey<boolean>("caretPlacedByChange");

/**
 * Whether the caret was last put where it stands by a change made in this
 * editor that placed it itself — "---", typed or pasted, its empty line after
 * the divider — rather than pushed there by the page's blocks changing under
 * it (a projection, which at most keeps a caret it had). A change of the
 * page or a move that sets no caret lets it go.
 */
export const caretPlacedByChange = new Plugin<boolean>({
  key: placedKey,
  state: {
    init: () => false,
    apply: (tr, placed) => tr.selectionSet ? !tr.getMeta(nativeProjectionMeta) : tr.docChanged ? false : placed,
  },
});

/** The page's id of the block whose own line holds the selection's start, or "". */
function lineBlockOf(state: EditorState) {
  const { $from } = state.selection;
  if ($from.parent.type.name !== "nativeText" && $from.parent.type.name !== "nativeCode") return "";
  const block = $from.depth > 1 ? $from.node($from.depth - 1) : null;
  return block?.type.name === "nativeBlock" ? String(block.attrs.sourceId || block.attrs.id || "") : "";
}

/** Where the block of that id now stands, when it has no text of its own. */
function textlessBlockAt(doc: ProseMirrorNode, blockId: string) {
  let at: number | null = null, found = false;
  doc.descendants((node, pos) => {
    if (found) return false;
    if (node.type.name !== "nativeBlock") return true;
    if (String(node.attrs.sourceId || node.attrs.id || "") !== blockId) return true;
    found = true;
    if (node.firstChild?.type.name === "nativeWidget") at = pos;
    return false;
  });
  return at;
}

/**
 * Whether the line the caret was in has just become a block with no text of
 * its own — a page, by the "/" menu's "페이지 · 전환", ⌘⌥9 or the selection
 * panel's list — leaving the selection where the change pushed it: in the
 * line after, or on a block with no text after it, which the editor would
 * then choose (nativeTiptapWidgetSelection). `first`: the turned line was the
 * page's first block. A change that placed the caret itself is no such turn:
 * "---" leaves its caret in the empty line it makes after the divider, as
 * Notion's (measured 2026-11-15 in Chrome, no script in the page: the next
 * letter went there); left by this rule as well, the caret went nowhere and
 * the next letters with it (2026-11-19, under a table at the page's foot).
 *
 * Notion's, measured 2026-11-18 in Chrome at 1440 in two tabs at different
 * times, pages made by hand, no script in the page, the pointer off the
 * page, one letter typed after: the line stood as a page block, unopened,
 * nothing chosen — the page block under it not washed — and the letter went
 * nowhere; turned from the page's first line it went in before the title's
 * first letter ("/" twice, ⌘⌥9 once).
 */
function caretLineTurnedTextless(before: EditorState, after: EditorState): { first: boolean } | null {
  // Typing in a line, or the caret moved with the page unchanged: no turn.
  const was = lineBlockOf(before);
  if (!was || lineBlockOf(after) === was || before.doc === after.doc || placedKey.getState(after)) return null;
  const at = textlessBlockAt(after.doc, was);
  return at === null ? null : { first: at === 0 };
}

/**
 * After the editor's selection moved: where its line has just turned a page
 * (caretLineTurnedTextless), the caret goes to the title's start from the
 * page's first line, else out of the editor, once the change is through.
 * Says whether it was such a turn.
 */
export function leaveTurnedLine(view: EditorView, before: EditorState) {
  const turned = caretLineTurnedTextless(before, view.state);
  if (!turned) return false;
  const { selection } = view.state;
  const pageId = turned.first ? view.dom.closest("[data-editor-page]")?.getAttribute("data-editor-page") : null;
  queueMicrotask(() => {
    if (view.state.selection.eq(selection) && view.hasFocus() && !(pageId && focusTitleStartInPlace(pageId))) view.dom.blur();
  });
  return true;
}
