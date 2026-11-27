import { useEffect, useRef, type RefObject } from "react";
import { requestPageUndo } from "@/components/editor/pageUndo";
import { registerRowTitleUndo } from "@/lib/inlineDatabaseRequests";
import { chordLetter, primaryModifier } from "@/lib/keyboardShortcuts";
import { caretDownFromTitle } from "@/components/editor/titleBodyCaret";
import { titleBodyUnseen } from "@/components/editor/pageBodyFrame";
import type { RichTextCellHandle } from "./RichTextCellEditor";

/** The handle a row page's title gives, so the body's used-up Undo goes on there. */
export function useRowTitleUndo(rowId: string) {
  const handle = useRef<RichTextCellHandle>(null);
  useEffect(() => registerRowTitleUndo(rowId, () => handle.current?.undoTyped() ?? false, () => handle.current?.focusEnd()), [rowId]);
  return handle;
}

/**
 * ⌘Z with the caret in a row page's title undoes what was typed there, then
 * goes on to the page's own steps, as a page's title passes it on
 * (PageHeader; Notion has one ⌘Z for a page, seen 2026-11-05). ↓ on the
 * title's last line goes to the "댓글 추가" field under it, or without one
 * to the body's first line under the caret, as a page's title does
 * (titleBodyCaret); a body kept unseen takes no caret, and the caret stays
 * (Notion's, seen 2026-11-24 in two tabs: a relation tab open, ↓ then a
 * letter went to the title's end, no line made). Tab moves nothing from it,
 * as a page's title (pageTitleEngine): Notion's row title in a side peek, a
 * centre peek and a row page open whole kept the caret, a letter typed after
 * Tab going in at its end (Chrome 1440, no script before the key, each in two
 * tabs 2026-11-26).
 */
export function rowTitleKeys(rowId: string, handle: RefObject<RichTextCellHandle | null>, focusBody: (at: "start") => void) {
  return (event: KeyboardEvent): boolean | undefined => {
    if (event.key === "Tab" && !(event.shiftKey || event.altKey || event.ctrlKey || event.metaKey)) return true;
    if (event.key === "ArrowDown" && !(event.metaKey || event.ctrlKey || event.altKey || event.shiftKey)) {
      const title = event.target instanceof Element ? event.target.closest<HTMLElement>("[data-row-page-title]") : null;
      const moved = title ? caretDownFromTitle(title, rowId) : false;
      if (moved === null && !titleBodyUnseen(title)) focusBody("start");
      return moved === false ? undefined : true;
    }
    if (!primaryModifier(event) || event.shiftKey || event.altKey || chordLetter(event) !== "z") return undefined;
    if (handle.current?.undoTyped()) return true;
    return requestPageUndo(rowId, false) || undefined;
  };
}
