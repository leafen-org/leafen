import type { HTMLAttributes, ReactNode } from "react";
import styles from "./MiddleTitle.module.css";

// The end of a long title kept whole: its last 24 letters, half of a shorter
// one (Notion's, seen 2026-11-12: 52 + 24, 69 + 24, 18 + 19 letters).
const KEPT_END = 24;

/**
 * A page's title in what a search found, as Notion's lists that find pages by
 * their words draw it — the move list's and the @ menu's (seen 2026-11-12):
 * on one line, too long, its start is cut with "…" and its end stays, at most
 * half the line wide and cut with "…" itself past that. A title that fits
 * reads whole. A list not searched cuts its titles at the end. It takes the
 * width of its line, which its list gives it.
 *
 * The title is its words once, whole, laid unseen over the line: a screen
 * reader and a row named by it (`aria-labelledby`) read "Meeting Notes
 * Archive", not the two drawn pieces as two words ("Meeting No tes Archive").
 * The pieces are drawn from their attributes, so they add no words of their
 * own to the row.
 */
export function MiddleTitle({ title, after, className, ...rest }: {
  title: string;
  /** What follows the words on their line (a verified page's mark), right after them. */
  after?: ReactNode;
} & HTMLAttributes<HTMLSpanElement>) {
  const letters = Array.from(title);
  const cut = letters.length - Math.min(KEPT_END, Math.ceil(letters.length / 2));
  return (
    <span {...rest} className={className ? `${styles.title} ${className}` : styles.title}>
      <span className={styles.whole}>{title}</span>
      <span className={styles.start} data-words={letters.slice(0, cut).join("")} aria-hidden="true" />
      <span className={styles.end} data-words={letters.slice(cut).join("")} aria-hidden="true" />
      {after ? <span className={styles.after}>{after}</span> : null}
    </span>
  );
}
