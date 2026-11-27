// What the page's chosen words take from either of their tools — the
// desktop's panel under them (NativeTiptapSelectionToolbar) and a touch
// screen's line of tools (PhoneSelectionTools): a comment on them, a
// reaction to them, and their colors.
import type { Editor } from "@tiptap/core";
import { openComments } from "@/components/comments/commentPanelEvents";
import { commentTextPlaces, hasQuotePlaces } from "@/components/comments/commentQuotePlaces";
import { pendingCommentBox } from "@/components/comments/pendingCommentMark";
import { PICKER_WIDTH } from "@/components/page/AnchoredEmojiPicker";
import { POPOVER_MARGIN } from "@/lib/popoverPlacement";
import { chosenCommentText } from "./nativeTiptapChosenComment";
import { rememberUsedEditorColor } from "./colorMemory";
import { selectionColorChange } from "./selectionToolbarModel";
import type { useTextReactionStarter } from "./TextReactions";

type ReactionStarter = NonNullable<ReturnType<typeof useTextReactionStarter>>;

/**
 * Starts a comment on the chosen words: they keep their mark while it is
 * written (pendingCommentMark.ts) and nothing stays chosen. False where the
 * words take none.
 */
export function commentOnChosenWords(editor: Editor, pageId: string) {
  const chosen = chosenCommentText(editor.state);
  if (!chosen) return false;
  openComments({ pageId, blockId: chosen.blockId, compose: true,
    initialQuote: chosen.quote, initialQuoteStart: chosen.start, initialQuoteEnd: chosen.end,
    ...(hasQuotePlaces(chosen.places) ? { initialQuotePlaces: chosen.places } : {}) });
  editor.commands.setTextSelection(editor.state.selection.to);
  return true;
}

/**
 * Opens the reaction picker for the chosen words, its middle under theirs —
 * or over them where the window ends — the words losing their selection but
 * keeping a mark while it is open, the picker's filter taking the keys, as
 * Notion's (measured 2026-11-15 in Chrome, no script in the page). False where
 * the words take none. Closed by Escape there, the next letter went to the
 * page title's start (seen once, not followed); here it goes after the words.
 */
export function reactToChosenWords(editor: Editor, pageId: string, reaction: ReactionStarter, button: HTMLElement) {
  const chosen = chosenCommentText(editor.state);
  if (!chosen) return false;
  const words = pendingCommentBox(commentTextPlaces(chosen.blockId, chosen.start, chosen.end, chosen.places))
    ?? button.getBoundingClientRect();
  const width = Math.min(PICKER_WIDTH, window.innerWidth - 2 * POPOVER_MARGIN);
  reaction({ anchor: new DOMRect(words.left + words.width / 2 - width / 2, words.top, width, words.height),
    pageId, blockId: chosen.blockId, quote: chosen.quote, quoteStart: chosen.start, quoteEnd: chosen.end, places: chosen.places });
  editor.commands.setTextSelection(editor.state.selection.to);
  return true;
}

/**
 * Opens the reaction picker for words of the page's title, as for the page's
 * other words: the reaction is the page's own (TextReactions), and the words
 * lose their selection and the title its focus, nothing taking it after the
 * pick, as Notion's (seen 2026-11-14 in iPhone Safari: no tools, the keyboard
 * down, the page's discussion under the title).
 */
export function reactToTitleWords(field: Editor, pageId: string, reaction: ReactionStarter, button: HTMLElement) {
  const { from, to } = field.state.selection;
  const start = field.view.coordsAtPos(from), end = field.view.coordsAtPos(to);
  const words = start.top === end.top ? new DOMRect(start.left, start.top, end.right - start.left, start.bottom - start.top)
    : button.getBoundingClientRect();
  const width = Math.min(PICKER_WIDTH, window.innerWidth - 2 * POPOVER_MARGIN);
  field.commands.setTextSelection(to);
  field.view.dom.blur();
  reaction({ anchor: new DOMRect(words.left + words.width / 2 - width / 2, words.top, width, words.height), pageId, page: true });
}

/**
 * Colors the chosen words with `token`, a text color or a background, the
 * other kind left as it was; the color is remembered for "최근 사용" and ⌘⇧H.
 * Suggesting, it is suggested.
 */
export function colorChosenWords(editor: Editor, token: string, how: { suggest?: (change: Record<string, string>) => void; writable: boolean }) {
  const change = selectionColorChange(token);
  rememberUsedEditorColor(token);
  if (how.suggest) {
    how.suggest({ [change.kind]: change.value ?? (change.kind === "color" ? "default" : "") });
    return;
  }
  if (!how.writable) return;
  const mark = change.kind === "color" ? "nativeColor" : "nativeBackground";
  if (change.value === null) editor.chain().focus().unsetMark(mark).run();
  else editor.chain().focus().setMark(mark, { stringValue: change.value }).run();
}
