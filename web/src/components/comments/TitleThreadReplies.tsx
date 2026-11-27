import type { ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { ChevronDown, ChevronRight } from "@/icons/leafen";
import styles from "./TitleThreadReplies.module.css";

/**
 * A thread's replies under a page's title, as Notion's (seen 2026-11-25 in
 * Chrome at 1440, a page's discussion in two tabs, and on a Galaxy A90's
 * Chrome at 411): shown again, a thread with replies folds them behind
 * "(N) 답글 보기", N its replies; pressed, it opens to "⌄ 답글 숨기기", every
 * reply and the thread's reply row, one step in, faces at the first
 * comment's words — and pressed again it folds them with the row. A thread
 * that had none when shown keeps the ones it is given open, and its row
 * stands only once its "↩" opened it (CommentCard).
 */
export function TitleThreadReplies({ count, open, onToggle, replies, replyRow }: {
  count: number;
  open: boolean;
  onToggle(): void;
  replies: readonly { id: string; node: ReactNode }[];
  replyRow: ReactNode;
}) {
  const { t } = useTranslation("commentsPanel");
  return (
    <>
      {count > 0 ? (
        <div className={styles.step} role="none" data-thread-step="toggle">
          <button type="button" className={styles.toggle} aria-expanded={open} onClick={onToggle} data-thread-replies-toggle>
            <span className={styles.badge}>
              <span className={styles.count}>{count}</span>
              <span className={styles.chevron} aria-hidden="true">{open ? <ChevronDown size="micro" /> : <ChevronRight size="micro" />}</span>
            </span>
            <span className={styles.label}>{t(open ? "thread.hideReplies" : "thread.showReplies")}</span>
          </button>
        </div>
      ) : null}
      {open ? replies.map(({ id, node }) => (
        <div key={id} className={styles.step} role="none" data-thread-step="reply">{node}</div>
      )) : null}
      {open && replyRow ? (
        <div className={styles.step} role="none" data-thread-step="row">{replyRow}</div>
      ) : null}
    </>
  );
}
