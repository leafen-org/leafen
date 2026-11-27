import { useEffect, useRef, useState } from "react";
import type { LibraryColumn } from "./libraryModel";
import { fieldEnter, imeComposing } from "@/lib/fieldKeyBoundary";

export type LibraryCell = { pageId: string; column: LibraryColumn };

/**
 * A chosen cell of the library's table, as Notion's library chooses one as a
 * database table does (seen twice on 2026-11-16 at 1440, in two tabs, its
 * private pages and its favorites): a name closed with Enter chooses the next
 * row's name cell — a 2px blue ring inside the cell on a faint blue ground,
 * a 9px dot at its bottom right — ↑↓ move it from row to row and ←→ from
 * column to column, and Escape chooses its row instead; Enter on a chosen
 * name renames it. A press anywhere lets it go. The keys are read where the
 * page has them, a field or a window keeping its own.
 */
export function useLibraryCellChoice({ rowIds, columns, onRename, onChooseRow }: {
  /** The rows shown, in their order. */
  rowIds: readonly string[];
  columns: readonly LibraryColumn[];
  onRename(pageId: string, cell: DOMRect): void;
  onChooseRow(pageId: string): void;
}) {
  const [cell, setCell] = useState<LibraryCell | null>(null);
  const latest = useRef({ rowIds, columns, onRename, onChooseRow });
  latest.current = { rowIds, columns, onRename, onChooseRow };

  useEffect(() => {
    if (!cell) return undefined;
    const keydown = (event: KeyboardEvent) => {
      const target = event.target as HTMLElement | null;
      if (event.defaultPrevented || imeComposing(event) || event.metaKey || event.ctrlKey || event.altKey
        || target?.closest("input, textarea, [contenteditable='true'], [role='dialog'], [role='menu']")) return;
      const { rowIds: rows, columns: shown } = latest.current;
      const row = rows.indexOf(cell.pageId), column = shown.indexOf(cell.column);
      if (event.key === "ArrowUp" || event.key === "ArrowDown") {
        const next = rows[row + (event.key === "ArrowUp" ? -1 : 1)];
        if (next) setCell({ pageId: next, column: cell.column });
      } else if (event.key === "ArrowLeft" || event.key === "ArrowRight") {
        const forward = (event.key === "ArrowRight") === (getComputedStyle(document.documentElement).direction !== "rtl");
        const next = shown[column + (forward ? 1 : -1)];
        if (next) setCell({ pageId: cell.pageId, column: next });
      } else if (event.key === "Escape") {
        setCell(null);
        latest.current.onChooseRow(cell.pageId);
      } else if (fieldEnter(event) && cell.column === "name") {
        const box = document.querySelector(`[data-library-row="${CSS.escape(cell.pageId)}"] [data-library-name-cell]`);
        if (!box) return;
        setCell(null);
        latest.current.onRename(cell.pageId, box.getBoundingClientRect());
      } else {
        return;
      }
      event.preventDefault();
    };
    const press = () => setCell(null);
    document.addEventListener("keydown", keydown);
    document.addEventListener("pointerdown", press, true);
    return () => {
      document.removeEventListener("keydown", keydown);
      document.removeEventListener("pointerdown", press, true);
    };
  }, [cell]);

  return {
    cell,
    /** Chooses the name cell of the row after this one, where there is one. */
    chooseBelow(pageId: string) {
      const { rowIds: rows } = latest.current;
      const next = rows[rows.indexOf(pageId) + 1];
      setCell(next ? { pageId: next, column: "name" } : null);
    },
  };
}
