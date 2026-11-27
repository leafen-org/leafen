import { Extension } from "@tiptap/core";
import { Plugin } from "@tiptap/pm/state";
import type { EditorView } from "@tiptap/pm/view";
import { imeComposing } from "@/lib/fieldKeyBoundary";

const androidChrome = typeof navigator !== "undefined"
  && /Android \d/u.test(navigator.userAgent) && /Chrome\//u.test(navigator.userAgent);

/**
 * Enter in a field on Android's Chrome runs the field's own Enter key. There
 * the keyboard's Enter comes as a key being composed (229) or is left by
 * ProseMirror to the line the browser inserts, so a field that waits out
 * composition before taking keys never saw it (2026-11-01: a page title's
 * Enter became a space). The line the browser is about to insert is refused,
 * and the field takes Enter once the word being composed is in.
 */
export const AndroidEnter = Extension.create({
  name: "androidEnter",
  addProseMirrorPlugins() {
    let waiting = false, frame = 0;
    const press = (view: EditorView) => {
      const event = new KeyboardEvent("keydown", { key: "Enter", code: "Enter", keyCode: 13, bubbles: true, cancelable: true });
      view.someProp("handleKeyDown", handle => handle(view, event));
    };
    return [new Plugin({
      props: {
        handleDOMEvents: {
          beforeinput: (view, event) => {
            const type = (event as InputEvent).inputType;
            if (!androidChrome || (type !== "insertParagraph" && type !== "insertLineBreak")) return false;
            event.preventDefault();
            if (imeComposing(view)) waiting = true;
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
