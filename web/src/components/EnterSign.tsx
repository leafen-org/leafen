import styles from "./EnterSign.module.css";

/**
 * The Enter sign "↵" written after a Done button's word ("완료 ↵"): Notion
 * draws it as its directional Enter icon, turned over in a right-to-left
 * screen (seen 2026-11-19 in Arabic and Hebrew, its equation panels), which
 * the direction rule in styles.css (`data-reading-turn`) does to this one.
 * A computer's browser draws it as the 12px letter. A phone draws the Notion
 * Android app's mark (measured 2026-11-25 on a Galaxy A90 at 411, the
 * equation's "완료 ↵", twice): an arrow 13.3 wide and 11 high in a 15 wide
 * box — its head on the left, 9 high, its line rising 6 at the right, 1.4
 * thick — where Android Chrome's letter is 6 wide in every font it has.
 */
export function EnterSign() {
  return <span className={styles.sign} aria-hidden="true" data-reading-turn>
    <span className={styles.letter}>↵</span>
    <svg className={styles.mark} width="15" height="11.1" viewBox="0 0 15 11.1" fill="none" stroke="currentColor"
      strokeWidth="1.4" strokeLinecap="round" strokeLinejoin="round">
      <path d="M13.55 0.7V6.5H1.65M5.55 2.6L1.65 6.5L5.55 10.4" />
    </svg>
  </span>;
}
