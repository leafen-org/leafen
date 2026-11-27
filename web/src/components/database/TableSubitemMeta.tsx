import { useTranslation } from "react-i18next";
import type { GraphRow } from "@/contentGraph";
import { SubitemCountIcon, SubitemParentIcon } from "@/icons/leafen";
import type { SubitemFilterScope } from "./subitemFilterScope";
import styles from "./TableSubitemMeta.module.css";

/** A table showing sub-items as a flattened list: what its "포함" keeps, each parent's count and each row's parent's title. */
export type TableFlatSubitems = {
  scope: SubitemFilterScope;
  counts: ReadonlyMap<string, number>;
  titles: ReadonlyMap<string, string>;
};

/** Whether a row's title has a mark after it: a parent with sub-items, or a sub-item. */
export function tableSubitemMetaShown(row: GraphRow, flat: TableFlatSubitems) {
  return flat.scope === "parents" ? (flat.counts.get(row.id) ?? 0) > 0 : Boolean(row.subitemParentId);
}

/**
 * After a title in a flattened list, as Notion's table (seen 2026-10-11):
 * with parents only, how many sub-items the row holds after ↳; otherwise a
 * sub-item's parent after ↰, which opens the parent beside the table.
 */
export function TableSubitemMeta({ row, flat, onOpenParent }: {
  row: GraphRow;
  flat: TableFlatSubitems;
  onOpenParent(parentId: string): void;
}) {
  const { t } = useTranslation("tableView");
  if (flat.scope === "parents") {
    const count = flat.counts.get(row.id) ?? 0;
    if (count <= 0) return null;
    return (
      <span className={styles.count} data-subitem-count={count} aria-label={t("subitemCount", { count })}>
        <SubitemCountIcon size="small" aria-hidden="true" /><span>{count}</span>
      </span>
    );
  }
  const parentId = row.subitemParentId;
  if (!parentId) return null;
  return (
    <button type="button" className={styles.parent} data-subitem-parent-id={parentId}
      aria-label={t("openParentInSidePeek")} onClick={() => onOpenParent(parentId)}>
      <SubitemParentIcon size="small" aria-hidden="true" /><span>{flat.titles.get(parentId) ?? ""}</span>
    </button>
  );
}
