import { Plugin } from "@tiptap/pm/state";
import type { EditorView } from "@tiptap/pm/view";
import { phoneScreen } from "@/lib/screenKind";
import { takeDomSelection } from "@/lib/tiptapDomSelection";
import { ringPressedLink } from "./pressedLinkRing";

// A link of the text's own: not a mention's, a page card's or a picture's (NativeTiptapLinkPill).
const OWN_LINK = "a[href]:not([data-mention], [data-page-card] a, [data-image-link], [data-image-link-label])";

/**
 * A finger's long press on a page's text that is not being written, as the
 * Notion Android app's (seen 2026-11-25 on a Galaxy A90, the keyboard down,
 * after the action only, a letter typed then):
 * - On a page not yet written in since it opened (three times: a fresh page,
 *   after a table cell's box was closed, and after the app started anew), the
 *   press chooses no words and starts no writing, the keyboard staying down,
 *   and a letter typed then goes nowhere; plain words show nothing (twice).
 *   The app also shows its "붙여넣기 · 모두 선택" menu then, for a field of its
 *   own outside the page (at the window's top, pasting nothing into the
 *   page); Leafen shows none (an approved difference).
 * - Once the page's text has been written in (a tap put the caret in, then
 *   the keyboard was put down; twice), the press chooses the word under it
 *   and writing starts there, the keyboard coming with the words' tools and
 *   the system's menu over the words.
 * Either way, and while the text is being written too (twice), a link
 * pressed is ringed as the keys' focus rings it (pressedLinkRing.ts). Android Chrome gives the text the focus as it chooses
 * the word, ahead of its menu event (focus, then contextmenu); the engine's
 * focus would put its own, older caret back over that word (Tiptap's focus
 * transaction), so the word is taken in first. A tap, a key or a focus given
 * by code takes the text up as being written.
 */
export function textLongPress() {
  return new Plugin({
    view(view: EditorView) {
      const doc = view.dom.ownerDocument;
      // The finger held down on the text, while it is down.
      let held: number | null = null;
      // Whether the text has been written in since it opened, and whether the press now chooses nothing.
      let written = false, resting = false;
      const down = (event: PointerEvent) => {
        if (event.pointerType !== "touch" || !event.isPrimary) return;
        const text = event.target instanceof Element ? event.target.closest("[data-rt-editable]") : null;
        held = text && view.dom.contains(text) ? event.pointerId : null;
        resting = false;
      };
      const up = (event: PointerEvent) => { if (event.pointerId === held) held = null; };
      const focused = (event: FocusEvent) => {
        if (event.target !== view.dom || view.isDestroyed) return;
        if (held === null) { written = true; return; }
        if (!written && phoneScreen()) {
          // Taken from the engine before it hears of it, and let go: no writing, no keyboard.
          event.stopImmediatePropagation();
          resting = true;
          view.dom.blur();
          return;
        }
        takeDomSelection(view);
      };
      const menu = (event: MouseEvent) => {
        if (held === null || !(event.target instanceof Node) || !view.dom.contains(event.target)) return;
        if (resting) {
          resting = false;
          event.preventDefault();
          event.stopImmediatePropagation();
          const chosen = doc.getSelection();
          if (chosen?.anchorNode && view.dom.contains(chosen.anchorNode)) chosen.removeAllRanges();
        }
        const link = event.target instanceof Element ? event.target.closest<HTMLAnchorElement>(OWN_LINK) : event.target.parentElement?.closest<HTMLAnchorElement>(OWN_LINK);
        if (link && view.dom.contains(link) && phoneScreen()) ringPressedLink(link);
      };
      doc.addEventListener("pointerdown", down, true);
      doc.addEventListener("pointerup", up, true);
      doc.addEventListener("pointercancel", up, true);
      doc.addEventListener("focus", focused, true);
      doc.addEventListener("contextmenu", menu, true);
      return {
        destroy() {
          doc.removeEventListener("pointerdown", down, true);
          doc.removeEventListener("pointerup", up, true);
          doc.removeEventListener("pointercancel", up, true);
          doc.removeEventListener("focus", focused, true);
          doc.removeEventListener("contextmenu", menu, true);
        },
      };
    },
  });
}
