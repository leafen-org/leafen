import styles from "./NotionMigrationTimeLeft.module.css";

/**
 * A run's time left — "약 54분 남음", "곧 끝남" — on its own near its head,
 * where its window, its card on "둘러보기" and its 완료됨 row show it
 * (2026-11-20: at the end of the count's long grey line it went unseen, and
 * the person thought the move was done). Nothing without a reckoning.
 */
export function NotionMigrationTimeLeft({ text }: { text: string | null | undefined }) {
  if (!text) return null;
  return <span className={styles.timeLeft} data-notion-migration-time-left>{text}</span>;
}
