import { useTranslation } from "react-i18next";
import { ArrowDown } from "@/icons/leafen";
import { LoadLimitMenuButton } from "./LoadLimitMenuButton";
import styles from "./CardsLoadMore.module.css";

/** A card view's first cards and how to show more. */
export type CardsLoadMoreControl = {
  /** How many cards the view first shows (a board's in each column). */
  limit: number;
  onLoadMore(): void;
  /** Chooses how many it first shows, when the view can be changed. */
  onLimit?(limit: number): void;
};

/**
 * "더 불러오기" under a list's or gallery's first cards, across it, or at
 * the foot of a board's column, as wide as its cards, with the "⋯" that
 * chooses how many it first shows (Notion's, seen 2026-10-01).
 */
export function CardsLoadMore({ limit, onLoadMore, onLimit, placement }: CardsLoadMoreControl & { placement: "view" | "column" }) {
  const { t } = useTranslation("databaseView");
  return (
    <div className={styles.line} data-cards-load-more={placement}
      onClick={(event) => { if (!(event.target as HTMLElement).closest("button, [role='menu']")) onLoadMore(); }}>
      <button type="button" className={styles.loadMore} onClick={onLoadMore}>
        <ArrowDown size="regular" aria-hidden="true" />
        <span>{t("loadMoreCards")}</span>
      </button>
      {onLimit && <LoadLimitMenuButton limit={limit} onLimit={onLimit} />}
    </div>
  );
}
