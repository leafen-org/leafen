import styles from "./SearchSkeleton.module.css";

// Each gray row's bars, as parts of the row's width, as Notion's draws them
// (seen 2026-11-14: random twentieths of the row; these are one draw).
const TITLE_WIDTHS = [75, 60, 75, 45, 30, 75, 60, 75];
const META_WIDTHS = [70, 10, 25, 35, 30, 35, 30, 55];
const CARD_WIDTHS = [90, 83, 90, 83, 36];

/**
 * The results while a search reads and nothing found before can stand for
 * them, as Notion's (seen 2026-11-14): eight gray rows, each an icon's
 * square, a title's bar and a shorter line under it.
 */
export function SearchSkeletonRows({ label }: { label: string }) {
  return (
    <div className={styles.rows} role="status" aria-label={label} data-search-skeleton>
      {TITLE_WIDTHS.map((title, index) => (
        <div key={index} className={styles.row} aria-hidden="true">
          <span className={styles.icon} />
          <span className={styles.lines}>
            <span className={styles.title} style={{ inlineSize: `${title}%` }} />
            <span className={styles.meta} style={{ inlineSize: `${META_WIDTHS[index]}%` }} />
          </span>
        </div>
      ))}
    </div>
  );
}

/** The preview's card meanwhile: a round icon's place over the band's foot and five gray lines. */
export function SearchSkeletonCard() {
  return (
    <div className={styles.card} aria-hidden="true" data-search-skeleton-card>
      <span className={styles.cardIcon} />
      {CARD_WIDTHS.map((width, index) => <span key={index} className={styles.cardLine} style={{ inlineSize: `${width}%` }} />)}
    </div>
  );
}
