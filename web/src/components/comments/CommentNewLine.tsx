import { useTranslation } from "react-i18next";
import styles from "./CommentNewLine.module.css";

/**
 * The "New" before a thread's first comment new to the reader
 * (lib/commentThreadUnread.ts), in Notion's shapes (seen 2026-11-25 in Chrome
 * at 1440 and at 900, dpr 1, two tabs at different times):
 * - "card": in a discussion's card — beside its block, open or closed, and in
 *   a block's window — before that reply, a thicker rule in the page's pale
 *   blue across the card with its pill in the middle, the thread's line
 *   running on through it;
 * - "more": in a closed card whose first new reply is among the ones it hides,
 *   the pill alone after "답글 N개 더 보기";
 * - "thread": under a page's title, before a thread new as a whole with
 *   replies, the card's rule and pill;
 * - "reply": drawn flat — under a page's title, in "모든 토론", in a feed
 *   post's discussion — before the first new reply of an opened thread, a
 *   hairline laid over the gap above it from the reply's words to the
 *   column's end, its bare "New" at the end.
 */
export function CommentNewLine({ shape }: { shape: "card" | "more" | "thread" | "reply" }) {
  const { t } = useTranslation("commentsPanel");
  if (shape === "more") return <span className={styles.pill} data-comment-new-line data-shape="more">{t("card.newLine")}</span>;
  return (
    <span className={styles.line} role="separator" aria-label={t("card.newLine")} data-comment-new-line data-shape={shape}>
      <span className={styles.pill} aria-hidden="true">{t("card.newLine")}</span>
      {shape === "card" ? <span className={styles.joint} aria-hidden="true" /> : null}
    </span>
  );
}
