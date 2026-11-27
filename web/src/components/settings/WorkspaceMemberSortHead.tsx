import type { Dispatch, SetStateAction } from "react";
import { ArrowDown, ArrowUp } from "@/icons/leafen";
import { nextWorkspaceMemberSort, type WorkspaceMemberSort, type WorkspaceMemberSortKey } from "./workspaceMemberTableModel";
import tableStyles from "./WorkspaceMemberTable.module.css";

/**
 * A people table's sortable column head, as Notion's member table (seen
 * 2026-10-10): the column's name and, on the sorted one, ↑ or ↓; pressing it
 * sorts by that column, or turns the order around when it already does. The
 * workspace's members and the organization's take the one head.
 */
export function WorkspaceMemberSortHead({ sortKey, text, sort, onSort }: {
  sortKey: WorkspaceMemberSortKey;
  text: string;
  sort: WorkspaceMemberSort;
  onSort: Dispatch<SetStateAction<WorkspaceMemberSort>>;
}) {
  const active = sort.key === sortKey;
  return (
    <div role="columnheader" className={tableStyles.headCell}
      aria-sort={active ? (sort.descending ? "descending" : "ascending") : "none"}>
      <button type="button" className={tableStyles.sortButton} onClick={() => onSort((current) => nextWorkspaceMemberSort(current, sortKey))}>
        <span>{text}</span>
        {active ? (sort.descending ? <ArrowDown size="tiny" aria-hidden="true" /> : <ArrowUp size="tiny" aria-hidden="true" />) : null}
      </button>
    </div>
  );
}
