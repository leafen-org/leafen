import { useEffect, useRef, type RefObject } from "react";
import { fieldEscape } from "./fieldKeyBoundary";
import { holdsNothingYet } from "./pageTitleFocus";

// A peek's page or row still being read: its title's caret waits for it.
const BODY_BEING_READ = '[data-row-page-body-reading], [aria-busy="true"][data-loading="true"]';
const PEEKS = "[data-row-peek-panel], [data-page-peek]";

/**
 * Escape in a peek's page title closes the peek, as Notion's does (seen
 * 2026-11-01, a new page's center peek closed with the caret in its title,
 * and a new database template's window with what was typed in its title kept).
 * The title's ProseMirror keeps every Escape to itself (prosemirror-view's
 * captureKeyDown), so the peek's layer never sees one from there. So does an
 * Escape pressed while the peek's page is still being read, before anything
 * is typed for the title its opening gives the caret (caretOnceAnswered),
 * as Notion's closes while it opens: held for that title, it closed the peek
 * only once the page was read (2026-11-22: 5 s away, a row's side peek stayed
 * open after its "열기" and Escape, and the name typed after the table's
 * "+ 새 페이지" went to the row it showed). Typed after letters, it goes to
 * the title in its turn, after them; once a field of the peek has had the
 * caret (a title's Enter waiting for the body), to where that sends it.
 */
export function usePeekTitleEscape(panelRef: RefObject<HTMLElement | null>, enabled: boolean, close: () => void) {
  const latest = useRef(close);
  latest.current = close;
  useEffect(() => {
    const panel = panelRef.current;
    if (!enabled || !panel) return;
    const onKeyDown = (event: KeyboardEvent) => {
      if (!fieldEscape(event)
        || event.metaKey || event.ctrlKey || event.altKey || event.shiftKey) return;
      const title = event.target instanceof Element ? event.target.closest("[data-page-title-editor], [data-row-page-title]") : null;
      if (!title || !panel.contains(title)) return;
      event.preventDefault();
      event.stopPropagation();
      latest.current();
    };
    // The field holding what is typed stands outside the peek, in the top layer. Held from a field of the peek,
    // what it holds is for what that field sent it to (a title's Enter to the body), not for the opening's caret.
    let entered = false;
    const enter = (event: FocusEvent) => {
      const field = event.target instanceof Element ? event.target : null;
      if (!field || field.matches("[data-new-page-typing-hold]")) return;
      entered = field !== panel && panel.contains(field) && Boolean(field.closest("[contenteditable='true'], input, textarea, select"));
    };
    const whileRead = (event: KeyboardEvent) => {
      if (entered || !fieldEscape(event) || event.metaKey || event.ctrlKey || event.altKey || event.shiftKey || !holdsNothingYet(event.target)
        || !panel.querySelector(BODY_BEING_READ) || [...document.querySelectorAll(PEEKS)].at(-1) !== panel) return;
      event.preventDefault();
      event.stopImmediatePropagation();
      latest.current();
    };
    panel.addEventListener("keydown", onKeyDown, true);
    document.addEventListener("focusin", enter, true);
    document.addEventListener("keydown", whileRead, true);
    return () => {
      panel.removeEventListener("keydown", onKeyDown, true);
      document.removeEventListener("focusin", enter, true);
      document.removeEventListener("keydown", whileRead, true);
    };
  }, [panelRef, enabled]);
}

/**
 * Escape with the keyboard's focus ring on a control of a peek — its bar's
 * "»", "⤢", "공유", "☆" and "⋯", or a property's name — keeps the peek, as
 * Notion's does (measured 2026-11-16 in Chrome at 1440, a side peek, with no
 * script before the key: each focused by Shift+Tab or by Escape closing the
 * menu it opened, ringed, and Escape left the peek and the ring as they were;
 * seen once before in a template's center peek). A control pressed with the
 * mouse shows no ring and Escape closes the peek, as there (☆ pressed, then
 * Escape); so does Escape from the title, the text or a value just edited.
 * The ring is read as the key comes down, before the key itself rings the
 * pressed control (Chrome's :focus-visible, lastPress's `data-key-focus`).
 */
export function peekControlKeepsEscape(panel: HTMLElement | null) {
  const active = typeof document === "undefined" ? null : document.activeElement;
  return active instanceof HTMLElement && active === ringedAtKey && active !== panel && Boolean(panel?.contains(active))
    && !active.isContentEditable && active.matches('button, [role="button"], a[href]');
}

// The control holding the keyboard's ring as the last key came down: the
// window hears the key before the document's listeners mark it (lastPress.ts).
let ringedAtKey: Element | null = null;
if (typeof window !== "undefined") {
  window.addEventListener("keydown", () => {
    const active = document.activeElement;
    ringedAtKey = active?.hasAttribute("data-key-focus") ? active : null;
  }, true);
}
