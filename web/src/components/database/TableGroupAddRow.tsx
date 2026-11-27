import { forwardRef, type MouseEvent, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { FilterIcon } from "@/icons/leafen";
import { Plus } from "../icons";
import styles from "./databaseSheet";

/**
 * A grouped table's "New page" under one group, which makes a row in it, or
 * its "+ 신규 그룹" under the last group (`groupAdd`, ViewGroupAdd), as
 * Notion's (seen 2026-10-06).
 */
export const TableGroupAddRow = forwardRef<HTMLDivElement, {
  index: number;
  /** "subitemAdd" is an open row's "+ 새 하위 항목", indented under it. */
  kind: "add" | "newGroup" | "subitemAdd";
  label: string;
  indent?: number;
  /** "+ 신규 그룹" (ViewGroupAdd), given the class that places it. */
  groupAdd?(className: string): ReactNode;
  onAdd(): void;
  /** An empty group of a filtered table: "필터 편집하기" before it, as Notion's (seen 2026-11-04). */
  onEditFilters?(trigger: HTMLElement): void;
  /** A group of a database gone to the trash: nowhere to add, its filters still (Notion's sprint board, seen 2026-11-05). */
  filtersOnly?: boolean;
}>(function TableGroupAddRow({ index, kind, label, indent, groupAdd, onAdd, onEditFilters, filtersOnly }, ref) {
  const { t } = useTranslation("boardView");
  return (
    <div ref={ref} data-index={index} className={styles.newRowRail} role="row"
      data-table-group-new-row={kind === "add" ? "true" : undefined} data-table-new-group={kind === "newGroup" ? "true" : undefined}
      data-table-new-subitem={kind === "subitemAdd" ? "true" : undefined}
      style={{ gridTemplateColumns: "var(--table-gutter-width) minmax(0, 1fr)" }}>
      <div className={styles.newRowGutterCell} role="gridcell" aria-hidden="true" />
      <div className={styles.newRowCell} role="gridcell">
        {onEditFilters ? <button type="button" className={styles.newRow} data-table-group-edit-filters onClick={(event) => onEditFilters(event.currentTarget)}>
          <FilterIcon size="small" aria-hidden="true" /> {t("editFilters")}</button> : null}
        {filtersOnly ? null : kind === "newGroup" ? groupAdd?.(styles.tableNewGroup) : <button type="button" className={styles.newRow}
          style={indent ? { marginInlineStart: indent } : undefined} onMouseDown={keepFocusAtPress} onClick={() => onAdd()}>
          <Plus size="small" aria-hidden="true" /> {label}</button>}
      </div>
    </div>
  );
});

/**
 * A mouse press on "+ 새 페이지" leaves the focus where it was: the press
 * then gives it to the field that holds what is typed for the new title
 * (TableView), with no move to the button first, which made two focus moves
 * of the one press.
 */
export function keepFocusAtPress(event: MouseEvent<HTMLElement>) {
  if (event.button === 0) event.preventDefault();
}

/** The names of a grouping property's options, which a new group may not take. */
export function groupOptionNames(property: { config?: unknown } | null | undefined): string[] {
  const options = (property?.config as { options?: unknown } | undefined)?.options;
  return Array.isArray(options)
    ? options.map((option) => (option as { name?: unknown })?.name).filter((name): name is string => typeof name === "string")
    : [];
}
