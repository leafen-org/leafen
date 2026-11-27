import { Extension } from "@tiptap/core";
import { Plugin } from "@tiptap/pm/state";
import type { EditorView } from "@tiptap/pm/view";
import { appleTouchDevice } from "@/lib/iosViewport";
import { takeDomSelection } from "@/lib/tiptapDomSelection";
import { fieldEnter, imeComposing } from "@/lib/fieldKeyBoundary";

/** What ProseMirror keeps of an iOS Return it waits on — its own state, outside its typings. */
type ReturnWaitedOn = { input?: { lastIOSEnter: number; lastIOSEnterFallbackTimeout: ReturnType<typeof setTimeout> } };

/** How long after a Return run at its key's release a line break the browser still sends belongs to it. */
const SAME_RETURN_MS = 250;

/**
 * On an iPhone or iPad a field being written in — a page's title, a
 * database's name, a cell, a comment, a formula, a page's lines — is plain
 * text to the system, as Notion's (seen 2026-11-16 in iPad Safari: its
 * title, a database's name and its page's lines are
 * read-write-plaintext-only, and the keyboard's bar shows no B I U for any
 * of them, where it showed them for Leafen's). Formatting stays the field's
 * own: its tools, ⌘B, ⌘I, ⌘U. The field marks its element (data-apple-plain)
 * and the stylesheet makes it plain while it is editable
 * (EditorCanvas.module.css).
 *
 * Plain, the browser's Return puts a line break in the text, which
 * ProseMirror reads as typed (2026-11-16: a database's name took the break
 * and kept it). The break is refused and the editor's own Return runs at
 * once — a page's lines split by their own keys — in place of the one
 * ProseMirror runs 200 ms later when the browser's new paragraph never
 * comes. A Return the browser sends no break for (after a Korean letter,
 * seen 2026-11-17 in iPad Safari) runs as its key comes up, not 200 ms
 * later. A word being composed is taken in first, as AndroidEnter waits for
 * it. Where the browser shows the caret is taken in before it runs.
 *
 * ⇧Return is ⇧Return only while the Shift key is held: the keyboard's own
 * capital at a line's or sentence's start marks its Return shifted too, with
 * no Shift key pressed, and a page's line took a line break there where
 * Notion's makes a new line (seen 2026-11-17 in iPad Safari, the English
 * keyboard at an empty line's start and after "B. ": a new block, the next
 * letter in it).
 */
export const AppleTouchPlainText = Extension.create({
  name: "appleTouchPlainText",
  addProseMirrorPlugins() {
    if (!appleTouchDevice()) return [];
    let shiftHeld = false, shift = false, waiting = false, frame = 0, ranAtKeyUp = -Infinity;
    const press = (view: EditorView) => {
      const own = view as unknown as ReturnWaitedOn;
      if (own.input) {
        clearTimeout(own.input.lastIOSEnterFallbackTimeout);
        own.input.lastIOSEnter = 0;
      }
      takeDomSelection(view);
      const event = new KeyboardEvent("keydown", { key: "Enter", code: "Enter", keyCode: 13, shiftKey: shift, bubbles: true, cancelable: true });
      shift = false;
      view.someProp("handleKeyDown", handle => handle(view, event));
    };
    return [new Plugin({
      props: {
        attributes: { "data-apple-plain": "true" },
        handleDOMEvents: {
          keydown: (_view, event) => {
            if (event.key === "Shift") shiftHeld = true;
            else if (fieldEnter(event)) shift = event.shiftKey && shiftHeld;
            return false;
          },
          keyup: (view, event) => {
            if (event.key === "Shift") shiftHeld = false;
            else if (fieldEnter(event) && !imeComposing(view) && (view as unknown as ReturnWaitedOn).input?.lastIOSEnter) {
              ranAtKeyUp = performance.now();
              press(view);
            }
            return false;
          },
          blur: () => {
            shiftHeld = false;
            return false;
          },
          beforeinput: (view, event) => {
            const type = (event as InputEvent).inputType;
            if ((type !== "insertParagraph" && type !== "insertLineBreak") || !event.cancelable) return false;
            event.preventDefault();
            if (performance.now() - ranAtKeyUp < SAME_RETURN_MS) ranAtKeyUp = -Infinity;
            else if (imeComposing(view)) waiting = true;
            else press(view);
            return true;
          },
          compositionend: view => {
            if (!waiting) return false;
            waiting = false;
            cancelAnimationFrame(frame);
            frame = requestAnimationFrame(() => { frame = 0; if (!view.isDestroyed) press(view); });
            return false;
          },
        },
      },
      view: () => ({ destroy: () => cancelAnimationFrame(frame) }),
    })];
  },
});
