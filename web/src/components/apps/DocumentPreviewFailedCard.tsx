import type { CSSProperties } from "react";
import { useTranslation } from "react-i18next";
import { ExclamationCircleFillIcon } from "@/icons/leafen";
import dialogButtons from "@/components/DialogButtons.module.css";
import styles from "./DocumentPreviewFailedCard.module.css";

/**
 * A page's embed of a sheet drawn as a plain table (not an .xlsx file) whose
 * file can't be read, as Notion's (seen 2026-11-15 at 1440 dark, a .numbers
 * file that is a zip of something else): in the frame, on its element ground
 * 24px in, a grey filled "!" circle (48px), "미리보기 불러올 수 없음" (16/500)
 * and a grey line under it (14px), then an outlined "파일 다운로드하기" (28px
 * high, 8px in) that saves the file — no button where it can't be saved.
 */
export function DocumentPreviewFailedCard({ style, onDownload }: { style?: CSSProperties; onDownload?: () => void }) {
  const { t } = useTranslation("blockItem");
  return <div className={styles.card} style={style} role="status" data-document-preview-failed="card">
    <div className={styles.column}>
      <ExclamationCircleFillIcon size="empty" className={styles.mark} aria-hidden="true" />
      <div className={styles.words}>
        <p className={styles.title}>{t("file.previewUnavailable")}</p>
        <p className={styles.reason}>{t("file.previewUnavailableReason")}</p>
      </div>
      {onDownload ? <button type="button" className={`${dialogButtons.cancel} ${styles.download}`} onClick={onDownload}>
        {t("file.previewDownloadFile")}</button> : null}
    </div>
  </div>;
}
