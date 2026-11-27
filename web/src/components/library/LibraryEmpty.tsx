import { useTranslation } from "react-i18next";
import { IconTipButton } from "@/components/IconTipButton";
import { ClockIcon, FilterIcon, LockIcon, Plus, SharePeopleIcon, Star } from "@/icons/leafen";
import { QuestionCircleIcon } from "@/icons/leafen/automationIcons";
import styles from "./LibraryView.module.css";

export type LibraryZeroView = "recents" | "favorites" | "shared" | "private";

const MARKS = { recents: ClockIcon, favorites: Star, shared: SharePeopleIcon, private: LockIcon };

/**
 * A page view of the library with nothing in it, as Notion's (its library
 * code, read 2026-10-31; the empty favorites seen the same day): while
 * searching or filtering, the band of "검색 초기화" or "필터 편집하기";
 * otherwise the view's 32px mark, what will appear there and why, and for the
 * private pages "새 페이지 생성하기".
 */
export function LibraryEmpty({ view, searching, filtered, onClearSearch, onEditFilters, onCreatePage }: {
  view: LibraryZeroView;
  /** The table's width: the block is centered on it, from 8px before it to 24px past it, as Notion's. */
  searching: boolean;
  filtered: boolean;
  onClearSearch: () => void;
  onEditFilters: () => void;
  onCreatePage?: () => void;
}) {
  const { t } = useTranslation("library");
  if (searching) return <LibraryNoResults onClearSearch={onClearSearch} />;
  if (filtered) return <LibraryNoResults onEditFilters={onEditFilters} />;
  const Mark = MARKS[view];
  return (
    <div className={styles.emptyView} data-library-empty={view}>
      <Mark size="card" aria-hidden="true" />
      <div className={styles.emptyText}>
        <p className={styles.emptyTitle}>{t(`emptyViews.${view}.title`)}</p>
        <p className={styles.emptyBody}>{t(`emptyViews.${view}.body`)}</p>
      </div>
      {view === "private" && onCreatePage ? (
        <button type="button" className={`${styles.emptyButton} ${styles.emptyCreate}`} data-library-empty-create onClick={onCreatePage}>
          {t("emptyViews.createPage")}
        </button>
      ) : null}
    </div>
  );
}

/**
 * Notion's band for a view that shows nothing (its code, read 2026-10-31; seen
 * the same day): 144px under the head, its buttons in the middle — "검색 초기화"
 * while searching, else "필터 편집하기" while filtering, and the view's own
 * "+ 새 …" where one may make it; with no button, "결과 없음" and a tip.
 */
export function LibraryNoResults({ onClearSearch, onEditFilters, newLabel, onNew }: {
  onClearSearch?: () => void;
  onEditFilters?: () => void;
  newLabel?: string;
  onNew?: () => void;
}) {
  const { t } = useTranslation("library");
  const action = onClearSearch
    ? <button type="button" className={styles.emptyButton} data-library-clear-search onClick={onClearSearch}>{t("clearSearch")}</button>
    : onEditFilters
      ? (
        <button type="button" className={styles.emptyButton} data-library-edit-filters onClick={onEditFilters}>
          <FilterIcon size="small" aria-hidden="true" />{t("editFilters")}
        </button>
      ) : null;
  const create = onNew && newLabel ? (
    <button type="button" className={styles.emptyButton} data-library-empty-new onClick={onNew}>
      <Plus size="small" aria-hidden="true" />{newLabel}
    </button>
  ) : null;
  return (
    <div className={styles.noResults} data-library-no-results>
      {action}
      {create}
      {action || create ? null : (
        <span className={styles.noResultsNote}>
          {t("noResults")}
          <IconTipButton label={t("noResultsTip")} className={styles.noResultsTip}>
            <QuestionCircleIcon size="small" aria-hidden="true" />
          </IconTipButton>
        </span>
      )}
    </div>
  );
}
