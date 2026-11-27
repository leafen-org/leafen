import type { EditorView } from "@tiptap/pm/view";
import { motionSafeScrollBehavior } from "@/lib/motion";
import { imeComposing } from "@/lib/fieldKeyBoundary";

// How far a key moves a page, as Chrome moves one: a page's step is 0.875 of
// what shows, an arrow's 40px.
const PAGE_STEP = 0.875;
const LINE_STEP = 40;
const WRITES = new Set(["Enter", "Backspace", "Delete", "Tab", "ArrowLeft", "ArrowRight", "Unidentified"]);

/**
 * Someone else took away the block the caret was in, and the caret goes with
 * it, as Notion's (seen 2026-11-12, a guest's caret at a line's end, the line
 * chosen and deleted by another): the page keeps the focus with no caret
 * showing, letters typed land nowhere, and Space, Page Down and Page Up, the
 * arrows up and down, Home and End move the page as a browser moves it.
 * Shortcuts with ⌘ or Ctrl go on as they do. It lasts until the person
 * presses somewhere or the page loses the focus; `ended` is told then.
 * Returns what ends it early.
 */
export function holdTakenCaret(view: EditorView, ended: () => void) {
  const page = view.dom.ownerDocument;
  const scroller = view.dom.closest<HTMLElement>("[data-page-scroll]");
  // The page's own caret, drawn again as it redraws, goes again.
  const hideCaret = () => {
    const shown = page.getSelection();
    if (shown?.rangeCount && shown.anchorNode && view.dom.contains(shown.anchorNode)) shown.removeAllRanges();
  };
  const swallow = (event: Event) => { event.preventDefault(); event.stopImmediatePropagation(); };
  const key = (event: KeyboardEvent) => {
    if (event.metaKey || event.ctrlKey) return;
    const step = (scroller?.clientHeight ?? 0) * PAGE_STEP;
    const moves: Record<string, number> = {
      " ": event.shiftKey ? -step : step, PageDown: step, PageUp: -step, ArrowDown: LINE_STEP, ArrowUp: -LINE_STEP,
    };
    if (!event.altKey && scroller && (event.key in moves || event.key === "Home" || event.key === "End")) {
      swallow(event);
      const behavior = motionSafeScrollBehavior();
      if (event.key === "Home") scroller.scrollTo({ top: 0, behavior });
      else if (event.key === "End") scroller.scrollTo({ top: scroller.scrollHeight, behavior });
      else scroller.scrollBy({ top: moves[event.key], behavior });
      return;
    }
    if (event.key.length === 1 || WRITES.has(event.key) || imeComposing(event)) swallow(event);
  };
  let held = true;
  const stop = () => {
    if (!held) return;
    held = false;
    view.dom.removeEventListener("keydown", key, true);
    for (const type of ["beforeinput", "paste", "drop"]) view.dom.removeEventListener(type, swallow, true);
    view.dom.removeEventListener("focusout", stop);
    page.removeEventListener("pointerdown", stop, true);
    page.removeEventListener("selectionchange", hideCaret);
    ended();
  };
  hideCaret();
  view.dom.addEventListener("keydown", key, true);
  for (const type of ["beforeinput", "paste", "drop"]) view.dom.addEventListener(type, swallow, true);
  view.dom.addEventListener("focusout", stop);
  page.addEventListener("pointerdown", stop, true);
  page.addEventListener("selectionchange", hideCaret);
  return stop;
}
