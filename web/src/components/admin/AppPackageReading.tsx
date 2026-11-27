import type { RefObject } from "react";
import { useTranslation } from "react-i18next";
import { UploadProgressBar } from "@/components/import/UploadProgressBar";
import type { AppPackageReading } from "./useAppPackageRead";
import styles from "./AdministratorApps.module.css";
import reviewStyles from "./AppPackageReviewDialog.module.css";

/**
 * The review window while its package is read: what is read, that it is
 * being uploaded or fetched and checked, and the review that follows; or why
 * it stopped, with the way to choose another file or try again.
 */
export function AppPackageReadingBody({ reading, titleId, focusRef, onClose, onAgain }: {
  reading: AppPackageReading;
  titleId: string;
  /** Where the focus enters: the way on once it failed, else 취소. */
  focusRef: RefObject<HTMLButtonElement | null>;
  onClose: () => void;
  /** Choose another file, or read the catalog's app again. */
  onAgain?: () => void;
}) {
  const { t } = useTranslation(["serverConsole", "common"]);
  const file = reading.source === "file";
  const failed = reading.failure !== undefined;
  const title = failed
    ? t(file ? "apps.reading.fileFailed" : "apps.reading.catalogFailed")
    : t(file ? "apps.reading.file" : "apps.reading.catalog");
  return (
    <>
      <h2 id={titleId} className={styles.dialogTitle}>{title}</h2>
      {reading.name || reading.detail ? (
        <div className={styles.reviewMeta}>
          {reading.name ? <span className={reviewStyles.readingName}>{reading.name}</span> : null}
          {reading.detail ? <span>{reading.detail}</span> : null}
        </div>
      ) : null}
      {failed ? (
        <div className={styles.error} role="alert" data-app-package-reading-failure>{reading.failure}</div>
      ) : (
        <>
          <UploadProgressBar className={reviewStyles.readingBar} label={title} progress={{ phase: "preparing", percent: 0 }} />
          <p className={styles.note} role="status">{t(file ? "apps.reading.fileLead" : "apps.reading.catalogLead")}</p>
        </>
      )}
      <div className={styles.dialogActions}>
        <button ref={failed && onAgain ? undefined : focusRef} type="button" className={styles.cancel} onClick={onClose}>
          {t(failed ? "common:actions.close" : "common:actions.cancel")}
        </button>
        {failed && onAgain ? (
          <button ref={focusRef} type="button" className={styles.primary} data-app-package-reading-again onClick={onAgain}>
            {t(file ? "apps.reading.chooseAnother" : "apps.reading.tryAgain")}
          </button>
        ) : null}
      </div>
    </>
  );
}
