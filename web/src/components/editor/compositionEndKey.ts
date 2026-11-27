import { Extension } from "@tiptap/core";
import { Plugin } from "@tiptap/pm/state";
import type { EditorView } from "@tiptap/pm/view";
import { appleTouchDevice } from "@/lib/iosViewport";
import { imeComposing } from "@/lib/fieldKeyBoundary";

/** When ProseMirror last saw a composition end — its own state, outside its typings. */
type CompositionEndSeen = { input?: { compositionEndedAt: number } };

/** ProseMirror's own "long ago" for a composition's end. */
const LONG_AGO = -2e8;

// Safari and the Mac app's WebKit on a Mac; an iPhone's or iPad's Return keeps its own way (AppleTouchPlainText).
const macWebKit = typeof navigator !== "undefined" && /Apple Computer/u.test(navigator.vendor) && !appleTouchDevice();

/**
 * The key that ends a composition is the field's in WebKit as it is in
 * Chrome (fieldKeyBoundary.ts). WebKit ends the composition before that key
 * comes: compositionend, then its keydown, not composing — the key's own
 * code (Enter 13) where the input method passed it on, as macOS's Korean
 * input method in marked-text mode passes on the Enter, Space or arrow that
 * ends a syllable; 229 where it kept the key. ProseMirror, in Safari, drops
 * every key that comes within half a second of a composition's end, taking
 * it for the input method's (Japanese's Enter that confirms a conversion),
 * and leaves it to the browser: in the desktop app a table cell's Enter
 * missed the cell's own Enter, the browser broke the line itself and the
 * syllable typed next landed in front of the cell's words ("나가", recorded
 * 2026-11-21 with the input method's calls played on the app's WebKit in
 * their recorded order); a new page's title kept its Enter.
 *
 * So a key that is the field's — the editor not composing, the key not the
 * input method's (imeComposing) — clears ProseMirror's mark of the
 * composition's end before ProseMirror reads it, and goes on to the
 * editor's keys as Chrome's does: a title's Enter goes to the body with the
 * syllable it ended, a line's starts the next line, a comment's sends, a
 * database cell's confirms, and a table block cell's goes to the cell below,
 * the last row's staying — as Notion's in Chrome (a table cell's measured
 * 2026-11-21 twice, in two tabs nine minutes apart, with macOS's Korean
 * input method and real keys, the next syllable typed after the Enter: "가"
 * stayed in its cell and "나" went into the cell below; in the last row both
 * stayed). A key the input method kept still comes as 229, the input
 * method's, and ProseMirror's guard keeps it.
 */
export const CompositionEndKey = Extension.create({
  name: "compositionEndKey",
  addProseMirrorPlugins() {
    if (!macWebKit) return [];
    // Where the marked words stood when WebKit set out to put the chosen
    // ones in (commitInPlace), and the place its deletion left the caret.
    let marked: Marked | null = null;
    return [new Plugin({
      props: {
        handleDOMEvents: {
          keydown: (view, event) => {
            const seen = (view as unknown as CompositionEndSeen).input;
            if (seen && !imeComposing(view) && !imeComposing(event)) seen.compositionEndedAt = LONG_AGO;
            return false;
          },
          beforeinput: (view, event) => {
            if (event.inputType === "deleteCompositionText") marked = markedWords(view, event);
            else if (event.inputType === "insertFromComposition" && marked) {
              const words = marked;
              marked = null;
              return commitInPlace(view, event, words);
            }
            return false;
          },
          input: (view, event) => {
            if ((event as InputEvent).inputType === "deleteCompositionText" && marked) marked.left = caretOf(view);
            return false;
          },
          compositionend: () => {
            marked = null;
            return false;
          },
        },
      },
    })];
  },
});

type Caret = { node: globalThis.Node; offset: number } | null;
type Marked = { text: Text; from: number; to: number; words: string; left: Caret };

function caretOf(view: EditorView): Caret {
  const selection = view.dom.ownerDocument.getSelection();
  return selection?.anchorNode ? { node: selection.anchorNode, offset: selection.anchorOffset } : null;
}

// The marked words WebKit is about to take out: one text's run in the editor.
function markedWords(view: EditorView, event: InputEvent): Marked | null {
  const range = event.getTargetRanges()[0];
  const text = range?.startContainer;
  if (!range || !(text instanceof Text) || range.endContainer !== text || !view.dom.contains(text)) return null;
  return { text, from: range.startOffset, to: range.endOffset, words: text.data.slice(range.startOffset, range.endOffset), left: null };
}

/**
 * WebKit puts an input method's chosen words in (macOS's Japanese input
 * method confirming a conversion with Enter) by taking the marked words out
 * and inserting the chosen ones where the caret then is. On a line that held
 * only the marked words, its taking out moved the block they stand in and
 * put an empty line's break and the caret before it, outside the text, and
 * left the marked words in place — so the chosen words went in there as a
 * line of their own on top of the editor's own, and what was typed next went
 * after the editor's (a new page's first line, recorded 2026-11-24 in the
 * desktop app with the input method's real keys: '日本号' twice; the
 * reported '日本語歌詞' over '日本語歌詞中文'). When the chosen words would go
 * anywhere but the line of the marked ones, they go into the marked words'
 * place instead, the caret after them, and the stray break comes out.
 */
function commitInPlace(view: EditorView, event: InputEvent, words: Marked): boolean {
  const { text, from, to, left } = words;
  const line = text.parentElement;
  const caret = caretOf(view);
  if (!event.cancelable || !line || !text.isConnected || !view.dom.contains(text) || (caret && line.contains(caret.node))) return false;
  // The marked words still there are replaced; taken out, the chosen ones go where they were.
  const kept = text.data.slice(from, to) === words.words;
  if (!kept && from > text.length) return false;
  event.preventDefault();
  const chosen = event.data ?? "";
  if (!kept) text.insertData(from, chosen);
  else if (words.words !== chosen) text.replaceData(from, to - from, chosen);
  const stray = left && !line.contains(left.node) ? left.node.childNodes[left.offset] : null;
  // WebKit's own break, not one of the editor's lines (those carry its view).
  if (stray instanceof HTMLBRElement && view.dom.contains(stray) && !("pmViewDesc" in stray && stray.pmViewDesc)) stray.remove();
  view.dom.ownerDocument.getSelection()?.collapse(text, Math.min(text.length, from + chosen.length));
  return true;
}
