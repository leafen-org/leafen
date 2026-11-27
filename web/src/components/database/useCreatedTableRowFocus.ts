import { useLayoutEffect, useRef, useState, type RefObject } from "react";
import { isNewSprintName } from "@/lib/sprintRows";
import { afterNextPaint } from "@/lib/afterPress";
import { afterHeldComposition, holdingTyping } from "@/lib/pageTitleFocus";
import { selectNativeTiptapElement } from "../editor/nativeTiptapFocus";
import { liveCellField } from "./RichTextCellShown";

type Intent = { rowId: string | null; scheduled: boolean; cancel(): void };

/** One creation gesture may focus its new native title. A later gesture or
 * retired view cancels that intent; later row renders cannot reclaim focus.
 * Keys held for that title (holdTypingForField) are not a later gesture. */
export function useCreatedTableRowFocus(root: RefObject<HTMLDivElement | null>, signal: AbortSignal, readOnly: boolean) {
  const pending = useRef<Intent | null>(null);
  const [, refresh] = useState(0);
  useLayoutEffect(() => {
    if (readOnly || signal.aborted) { pending.current?.cancel(); return; }
    const intent = pending.current;
    if (!intent?.rowId) return;
    const rowId = intent.rowId;
    const titleOf = () => [...(root.current?.querySelectorAll<HTMLElement>("[data-table-row-id]") ?? [])]
      .find(element => element.dataset.tableRowId === rowId)?.querySelector<HTMLElement>("[data-table-title-input], [data-title=true] input");
    if (!titleOf()?.isConnected || intent.scheduled) return;
    intent.scheduled = true;
    // The row is painted first and its title wakes into its editor and takes
    // the focus in the task after that frame (user decision 2026-11-12: at a
    // press only the visible reaction): woken in the press, the editor held
    // its first paint 30–40 ms (2026-11-14, NAS + CPU ×2). What is typed
    // before then is held for it (holdTypingForField) and goes in as typed.
    // Where the caret goes is as before and as Notion's table "+ 새 페이지"
    // (2026-11-14, nothing run in the page before the press: the screen just
    // after it, one letter typed, then read): the new row shows with its
    // title open for typing, the letter goes into that title, and then
    // document.activeElement is the title's editor holding it, getSelection()
    // in its text at offset 1 — a letter typed at once, before the title
    // took the focus here, lands there too. The page is worked out once for
    // the woken title and its focus together (2026-11-13: asked twice, 22 and
    // 33 ms of a grouped 30×40 table's click).
    afterNextPaint(() => afterHeldComposition(() => {
      intent.scheduled = false;
      const field = titleOf();
      if (pending.current !== intent || signal.aborted || !field?.isConnected) return;
      // Its title's shown words wake into their editor (RichTextCellShown). A
      // new sprint's name stays chosen, so what is typed takes its place.
      const live = liveCellField(field);
      if (!selectNativeTiptapElement(live, isNewSprintName(live.textContent) ? "select" : "end")) live.focus();
      // A title not shown (its group folded, its view hidden) takes no focus:
      // the row drawn again tries again.
      if (live.contains(document.activeElement)) intent.cancel();
    }));
  });
  useLayoutEffect(() => () => pending.current?.cancel(), [signal]);
  return () => {
    pending.current?.cancel();
    if (readOnly || signal.aborted) return () => {};
    const onKey = () => { if (!holdingTyping()) cancel(); };
    const cancel = () => {
      document.removeEventListener("pointerdown", cancel, true);
      document.removeEventListener("keydown", onKey, true);
      signal.removeEventListener("abort", cancel);
      if (pending.current === intent) pending.current = null;
    };
    const intent: Intent = { rowId: null, scheduled: false, cancel };
    pending.current = intent;
    document.addEventListener("pointerdown", cancel, true);
    document.addEventListener("keydown", onKey, true);
    signal.addEventListener("abort", cancel, { once: true });
    return (rowId: string) => {
      if (pending.current !== intent || signal.aborted) return;
      intent.rowId = rowId; refresh(value => value + 1);
    };
  };
}
