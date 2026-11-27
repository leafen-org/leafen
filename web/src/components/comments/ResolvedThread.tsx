import { useTranslation } from "react-i18next";
import { CheckIcon } from "@/icons/leafen";
import styles from "./ResolvedThread.module.css";

/**
 * "✓ 해결됨" after a resolved thread's time, in the tertiary words of the
 * time, as Notion's in a thread's own view and in "모든 토론" (seen
 * 2026-11-25 in Chrome at 1440, dpr 2: 12px words, a 14px tick 2px before
 * them, 6px after the time).
 */
export function ResolvedThreadMark() {
  const { t } = useTranslation("commentsPanel");
  return (
    <span className={styles.mark} data-thread-resolved-mark>
      <CheckIcon size="tiny" aria-hidden="true" />{t("card.resolved")}
    </span>
  );
}

/**
 * Where a resolved thread's reply row was in its own view, as Notion's (seen
 * 2026-11-25 in Chrome at 1440, dpr 2, two tabs): a card saying it is
 * resolved and that it is reopened to be answered, with "다시 열기" for one
 * who may reopen it.
 */
export function ResolvedThreadCard({ onReopen }: { onReopen?: () => void }) {
  const { t } = useTranslation("commentsPanel");
  return (
    <div className={styles.card} role="status" data-thread-resolved-card>
      <div className={styles.words}>
        <div className={styles.title}>{t("card.resolvedTitle")}</div>
        <div className={styles.note}>{t("card.resolvedNote")}</div>
      </div>
      {onReopen ? (
        <button type="button" className={styles.reopen} onClick={onReopen} data-thread-reopen>{t("actions.reopen")}</button>
      ) : null}
    </div>
  );
}
