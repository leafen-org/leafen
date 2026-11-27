import { useCallback, useMemo, useSyncExternalStore, type ReactNode } from "react";

/**
 * Which of a table's cells — a timeline's table's too — draw their value's
 * tools (TableCellValueTools), which show only under the pointer or while
 * they hold the focus: the cell under the pointer and the one whose tools
 * have the focus. Drawn in every cell, the hidden trays were over a third
 * of a table's nodes (6,540 of 17,418 for 30 rows of 40 properties,
 * 2026-11-12).
 */
export type CellToolsShown = {
  pointerOver(target: EventTarget | null): void;
  pointerLeave(): void;
  focusIn(target: EventTarget | null): void;
  listen(cell: string, listener: () => void): () => void;
  shows(cell: string): boolean;
};

/** A cell's key: its row's line — a row shows under each of its groups (viewGroupLines.ts) — and its column. */
export const toolsCellKey = (line: string, column: number) => `${line}:${column}`;

function cellOf(target: EventTarget | null) {
  const cell = target instanceof Element ? target.closest<HTMLElement>("[data-table-cell]") : null;
  const row = cell?.closest<HTMLElement>("[data-table-line], [data-timeline-table-line]");
  const line = row?.dataset.tableLine ?? row?.dataset.timelineTableLine;
  return cell && line ? toolsCellKey(line, Number(cell.dataset.colIndex)) : "";
}

export function useCellToolsShown(): CellToolsShown {
  return useMemo(() => {
    let pointed = "", focused = "";
    const listeners = new Map<string, Set<() => void>>();
    const tell = (cell: string) => listeners.get(cell)?.forEach((listener) => listener());
    const point = (cell: string) => {
      if (cell === pointed) return;
      const before = pointed;
      pointed = cell;
      tell(before);
      tell(cell);
    };
    return {
      pointerOver: (target) => point(cellOf(target)),
      pointerLeave: () => point(""),
      focusIn: (target) => {
        const cell = target instanceof Element && target.closest("[data-cell-value-tools]") ? cellOf(target) : "";
        if (cell === focused) return;
        const before = focused;
        focused = cell;
        tell(before);
        tell(cell);
      },
      listen: (cell, listener) => {
        const set = listeners.get(cell) ?? new Set();
        set.add(listener);
        listeners.set(cell, set);
        return () => {
          set.delete(listener);
          if (!set.size) listeners.delete(cell);
        };
      },
      shows: (cell) => cell !== "" && (cell === pointed || cell === focused),
    };
  }, []);
}

/** A cell's tools, drawn while the cell shows them. */
export function CellTools({ shown, cell, children }: { shown: CellToolsShown; cell: string; children: () => ReactNode }) {
  const listen = useCallback((listener: () => void) => shown.listen(cell, listener), [shown, cell]);
  const shows = useSyncExternalStore(listen, () => shown.shows(cell));
  return shows ? children() : null;
}
