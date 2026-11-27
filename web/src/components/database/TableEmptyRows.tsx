import { type PointerEvent, type ReactNode, useRef, useState } from "react";
import { pointerMoved, pointerStill } from "@/lib/pointerHover";
import styles from "./databaseSheet";
import emptyStyles from "./TableEmptyRows.module.css";
import { useDatabaseOwnPage } from "./databaseFullPageLink";
import { tableTrackColumns, withNewColumnCell } from "./tableNewColumn";
import { useEmptyRowMark } from "./databaseBlockRowMarks";

/**
 * A table with no rows, as Notion's new table (seen 2026-10-24 at 1280px):
 * 37px rows under its head across the table — five on the database's own
 * page, three inline — each column's line running through them, "+ 새 페이지"
 * in the first; once a row is in, only "+ 새 페이지" follows it. The line
 * the pointer is on takes "+ 새 페이지" and answers it as one line, a press
 * anywhere on it making the row, and it goes back to the first line once
 * the pointer leaves (Notion's, seen 2026-11-11: on the third line of three
 * the whole line from the first column on is its button, lit). A line comes
 * under the pointer by the pointer's own move or press only: a menu closing
 * over a still pointer leaves "+ 새 페이지" where it was (Notion's, seen
 * 2026-11-16 twice in Chrome at 1440: "텍스트" chosen from the head's
 * "+ 속성 추가" over the second line, the first kept it, the second taking it
 * once the pointer moved).
 */
export function TableEmptyRows({ widths, newColumnAt, newRow }: {
  widths: readonly number[];
  /** Where an insert's empty column stands among the columns, its cell in each line (tableNewColumn.ts). */
  newColumnAt?: number;
  newRow: ReactNode;
}) {
  const columns = tableTrackColumns(widths, widths.length, newColumnAt);
  const count = useDatabaseOwnPage() ? 5 : 3;
  const [at, setAt] = useState(0);
  // The first line under the pointer brings the database block's "+ ⋮⋮" (databaseBlockRowMarks).
  const pointed = useEmptyRowMark();
  // The line the pointer came into by moving or pressing.
  const inside = useRef<number | null>(null);
  const come = (event: PointerEvent<HTMLDivElement>, index: number) => {
    if (inside.current === index) return;
    inside.current = index;
    if (index === 0) pointed(event.currentTarget, true);
    if (event.pointerType !== "touch") setAt(index);
  };
  return Array.from({ length: count }, (_, index) => (
    <div key={index} className={styles.tableRow} role={index === at ? "row" : "presentation"} data-table-empty-row={index}
      data-table-new-row-here={index === at ? "true" : undefined}
      style={{ gridTemplateColumns: `var(--table-gutter-width) ${columns} minmax(0, 1fr)` }}
      onPointerEnter={(event) => { if (!pointerStill(event)) come(event, index); }}
      onPointerMove={(event) => { if (pointerMoved(event)) come(event, index); }}
      onPointerDown={(event) => come(event, index)}
      onPointerLeave={(event) => {
        if (inside.current === index) inside.current = null;
        if (index === 0) pointed(event.currentTarget, false);
        if (event.pointerType !== "touch") setAt(0);
      }}
      onClick={(event) => {
        // The line is the button: a press beside "+ 새 페이지" presses it.
        if (index !== at || (event.target as Element).closest("button, a, input, [role='menu']")) return;
        event.currentTarget.querySelector<HTMLButtonElement>("[data-table-new-row]:not(:disabled)")?.click();
      }}>
      <div aria-hidden="true" />
      {withNewColumnCell(widths.map((_, column) => index === at && column === 0
        ? <div key={column} className={`${styles.cell} ${emptyStyles.newRowCell}`} role="gridcell">{newRow}</div>
        : <div key={column} className={styles.cell} aria-hidden="true" />), newColumnAt,
      () => <div key="new-column" className={styles.cell} aria-hidden="true" data-table-new-column />)}
      {/* The row's line runs on to the table's end. */}
      <div className={styles.cell} data-table-empty-fill aria-hidden="true" />
    </div>
  ));
}
