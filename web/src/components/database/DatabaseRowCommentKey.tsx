import type { ReactNode } from "react";
import { matchesKeyboardShortcut } from "@/lib/keyboardShortcuts";

const ROW = "[data-row-id], [data-table-row-id], [data-list-row-id], [data-timeline-row-id]";

/**
 * ⌘⇧M on a row or card the keyboard is on opens its new comment's field, as
 * its menu's "댓글" does — Notion's row menu shows the key (seen
 * 2026-10-11). Text being typed keeps the key.
 */
export function DatabaseRowCommentKey({ onComment, children }: {
  onComment?: (rowId: string, element: HTMLElement) => void;
  children: ReactNode;
}) {
  return (
    <div style={{ display: "contents" }} onKeyDown={(event) => {
      if (!onComment || !matchesKeyboardShortcut("addComment", event.nativeEvent)) return;
      const target = event.target instanceof HTMLElement ? event.target : null;
      // An inline database sits in its page's editable text, so only an editable target keeps the key.
      if (!target || target.isContentEditable || target.closest("input, textarea")) return;
      const row = target.closest<HTMLElement>(ROW);
      const rowId = row?.dataset.rowId ?? row?.dataset.tableRowId ?? row?.dataset.listRowId ?? row?.dataset.timelineRowId;
      if (!row || !rowId) return;
      event.preventDefault();
      event.stopPropagation();
      onComment(rowId, target);
    }}>{children}</div>
  );
}
