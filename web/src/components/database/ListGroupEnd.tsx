import { useTranslation } from "react-i18next";
import { FilterIcon } from "@/icons/leafen";
import { Plus } from "../icons";
import styles from "./databaseSheet";
import own from "./ListGroupEnd.module.css";

/**
 * Where a list's group ends: its "+ 새 …" row, or, for an empty group of
 * "내 작업" (`empty`), Notion's empty body — "필터 편집하기", which opens the
 * view's filters, and "+ 새 작업", which adds a task in the group, outlined
 * as a view's "no results" buttons (DatabaseNoResults) and centered.
 */
export function ListGroupEnd({ measure, index, groupKey, empty, disabled, label, onAdd, onEditFilters }: {
  measure?: (element: HTMLElement | null) => void;
  index: number;
  groupKey: string;
  empty: boolean;
  disabled: boolean;
  label: string;
  onAdd?: () => void;
  onEditFilters?: (trigger: HTMLElement) => void;
}) {
  const { t } = useTranslation("boardView");
  if (!empty) {
    return (
      <button ref={measure} data-index={index} type="button" disabled={disabled}
        className={`${styles.listNew} ${styles.listGroupAdd}`} data-list-group-add={groupKey} onClick={onAdd}>
        <Plus size="small" aria-hidden="true" /> {label}
      </button>
    );
  }
  return (
    <div ref={measure} data-index={index} className={own.emptyGroup} data-list-group-empty={groupKey}>
      {onEditFilters ? (
        <button type="button" className={styles.boardNothingAction} onClick={(event) => onEditFilters(event.currentTarget)}>
          <FilterIcon size="small" trim aria-hidden="true" />{t("editFilters")}
        </button>
      ) : null}
      {onAdd ? (
        <button type="button" className={styles.boardNothingAction} disabled={disabled} data-list-group-add={groupKey} onClick={onAdd}>
          <Plus size="small" trim aria-hidden="true" />{label}
        </button>
      ) : null}
    </div>
  );
}
