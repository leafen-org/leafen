import { useTranslation } from "react-i18next";
import { ArrowDown } from "@/icons/leafen";
import { LoadLimitMenuButton } from "./LoadLimitMenuButton";
import styles from "./TableLoadMoreRow.module.css";

/**
 * The line under an inline table's first rows, as Notion's (seen
 * 2026-10-01): "더 불러오기" anywhere on it shows as many more as the table first
 * shows (seen 2026-11-03), and its "⋯"
 * chooses how many the table first shows ("처음 불러오기에서 보기": 10, 25,
 * 50 or 100 pages).
 */
export function TableLoadMoreRow({ loading, limit, onLoadMore, onLimit }: {
  loading: boolean;
  limit?: number;
  onLoadMore(): void;
  onLimit?(limit: number): void;
}) {
  const { t } = useTranslation("tableView");
  return (
    <div className={styles.rail} data-table-load-more-rail
      onClick={(event) => { if (!(event.target as HTMLElement).closest("button, [role='menu']") && !loading) onLoadMore(); }}>
      <div className={styles.sticky}>
        <button type="button" className={styles.loadMore} data-table-load-more disabled={loading} onClick={onLoadMore}>
          <ArrowDown size="regular" aria-hidden="true" />
          <span>{t("loadMoreAria")}</span>
        </button>
        {onLimit && limit !== undefined && <LoadLimitMenuButton limit={limit} onLimit={onLimit} />}
      </div>
    </div>
  );
}
