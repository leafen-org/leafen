import { useTranslation } from "react-i18next";
import styles from "./ReadFailed.module.css";

/**
 * A read that failed, said where what it reads would show, with a way to
 * read it again — instead of an empty list, a loading line that stays, or a
 * control that never turns on.
 */
export function ReadFailed({ message, onRetry }: { message: string; onRetry: () => void }) {
  return (
    <div className={styles.readFailed} role="alert" data-read-failed>
      <span>{message}</span>
      <ReadAgain onRetry={onRetry} />
    </div>
  );
}

/** The way to read it again, after what a failed read says (ReadFailed, a settings ListNotice). */
export function ReadAgain({ onRetry }: { onRetry: () => void }) {
  const { t } = useTranslation("common");
  return <button type="button" className={styles.again} onClick={onRetry} data-read-again>{t("retry")}</button>;
}
