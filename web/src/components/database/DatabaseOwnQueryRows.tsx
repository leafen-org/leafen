import { useState, type ReactNode } from "react";
import { Copy, DotsHorizontal, FilterIcon, ResetIcon, SaveForEveryoneIcon } from "@/icons/leafen";
import { databaseViewLabels } from "./databaseViewLabels";
import styles from "./DatabaseOwnQueryRows.module.css";

/**
 * The end of the view settings' filter and sort screens while the viewer's
 * own filters or sorts there differ from the view's, as Notion's (seen
 * 2026-11-03): under a line, an orange "모두에게 저장" (to an editor), "필터
 * 초기화" or "정렬 초기화", and "추가 설정", which opens to a grey note over
 * them and "새 보기로 저장" in its place. The rows look like the screen's own.
 */
export function DatabaseOwnQueryRows({ note, resetLabel, rowClassName, iconClassName, onSaveForEveryone, onReset, onSaveAsNewView, onMergeFilters }: {
  note: string;
  resetLabel: string;
  rowClassName: string;
  iconClassName: string;
  onSaveForEveryone?(): void;
  onReset(): void;
  onSaveAsNewView?(): void;
  /** "고급 통합 필터", under "새 보기로 저장" on the filter screen (seen 2026-11-03). */
  onMergeFilters?(): void;
}) {
  const labels = databaseViewLabels();
  const [more, setMore] = useState(false);
  const row = (icon: ReactNode, label: string, onClick: () => void, data: Record<string, string>, className = rowClassName) => (
    <button type="button" className={className} onClick={onClick} {...data}>
      <span className={iconClassName} aria-hidden="true">{icon}</span>
      <span>{label}</span>
    </button>
  );
  return (
    <div className={styles.own} data-own-view-query>
      {more ? <p className={styles.note}>{note}</p> : null}
      {onSaveForEveryone ? row(<SaveForEveryoneIcon size="regular" />, labels.saveViewForEveryone, onSaveForEveryone,
        { "data-save-view-for-everyone": "" }, `${rowClassName} ${styles.save}`) : null}
      {row(<ResetIcon size="regular" />, resetLabel, onReset, { "data-own-query-reset": "" })}
      {onSaveAsNewView && more ? row(<Copy size="regular" />, labels.saveAsNewView, onSaveAsNewView, { "data-save-as-new-view": "" })
        : onSaveAsNewView ? row(<DotsHorizontal size="regular" />, labels.moreSettings, () => setMore(true), { "data-own-query-more": "" }) : null}
      {onMergeFilters && more ? row(<FilterIcon size="regular" />, labels.mergeIntoAdvancedFilter, onMergeFilters, { "data-merge-into-advanced": "" }) : null}
    </div>
  );
}
