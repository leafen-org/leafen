import { useLayoutEffect, useRef, type RefObject } from "react";
import { focusAfterPaint, placeCaret } from "@/components/editor/focus";
import { afterNextPaint } from "@/lib/afterPress";
import { afterHeldComposition, consumePageTitleFocus, holdingTyping, pageTitleFocusAsked, typeHeldTypingIntoTitle } from "@/lib/pageTitleFocus";
import { fieldAsleep } from "@/lib/sleepingFields";
import { isNewSprintName } from "@/lib/sprintRows";
import { peekFieldFocused, peekFieldHasFocus } from "./peekFocusReturn";

const TITLE = "[data-row-page-title]";

/**
 * A row page's title takes the caret at its end: at once where its editor is
 * made already, else once the frame the page opens in is painted
 * (RichTextCellEditor's afterPaint), what is typed meanwhile waiting for it
 * there and a press meanwhile keeping its own focus (focusAfterPaint). Gives
 * what calls it off.
 */
export function caretAtRowTitleEnd(title: HTMLElement): () => void {
  if (!fieldAsleep(title)) {
    placeCaret(title, "end");
    return () => undefined;
  }
  return focusAfterPaint(() => title.isConnected ? { field: title, position: "end" } : null,
    () => title.closest("[data-row-peek-panel], [data-row-page]"));
}

/**
 * An opening peek's focus (DatabaseRowPage): its title's end where the
 * opening takes the title's caret (`title`), once the frame it opens in is
 * painted, what is typed meanwhile waiting for it there, unless a field of
 * the peek has the focus by then (2026-11-21: a letter typed right after a
 * row's "열기" went nowhere, the caret coming after it); else, a frame on,
 * the panel itself, unless a field of it has the focus or typing is held
 * for its new row (pageTitleFocus). `entered` runs on that frame. Gives what
 * calls it off.
 */
export function takePeekFocus(panel: RefObject<HTMLElement | null>, root: RefObject<HTMLElement | null>, title: boolean, entered: () => void) {
  const caret = title && !holdingTyping() ? focusAfterPaint(() => {
    const field = root.current?.querySelector<HTMLElement>(TITLE);
    return peekFieldHasFocus(panel.current) ? "kept" : field ? { field, position: "end" } : null;
  }, () => panel.current) : null;
  const frame = window.requestAnimationFrame(() => {
    entered();
    if (!caret && !holdingTyping() && !peekFieldFocused(panel.current)) panel.current?.focus();
  });
  return () => { window.cancelAnimationFrame(frame); caret?.(); };
}

/**
 * A row just made opens with its title ready to type, as Notion's, with what
 * was typed while it was being made (held for it, pageTitleFocus.ts): the
 * caret goes there once its editor is made, after the frame the page opens
 * in, the ask kept till then so what is held waits for it. A new sprint's
 * name stays chosen, so what is typed takes its place.
 */
export function useNewRowTitleCaret(rowId: string, titleEditable: boolean, rootRef: RefObject<HTMLElement | null>) {
  const asked = useRef<{ rowId: string; focus: boolean } | null>(null);
  if (asked.current?.rowId !== rowId) asked.current = { rowId, focus: pageTitleFocusAsked(rowId) };
  useLayoutEffect(() => {
    const ask = asked.current;
    const title = ask?.focus && titleEditable ? rootRef.current?.querySelector<HTMLElement>(TITLE) : null;
    if (!ask || !title) return;
    return afterNextPaint(() => {
      ask.focus = false;
      if (!title.isConnected || !consumePageTitleFocus(rowId)) return;
      afterHeldComposition(() => { if (title.isConnected && placeCaret(title, isNewSprintName(title.textContent) ? "select" : "end")) typeHeldTypingIntoTitle(); });
    });
  }, [rowId, titleEditable, rootRef]);
}
