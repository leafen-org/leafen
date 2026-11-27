import { useId, useRef } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { ArrowRight, CheckIcon, CodeIcon, FileText, PdfFileIcon, TableIcon, WordDocumentIcon, X } from "@/icons/leafen";
import { useOverlayLayer } from "@/lib/overlayLayer";
import doneStyles from "./CsvImportDialog.module.css";
import styles from "./ImportKindWindow.module.css";

/** The kind's mark the windows begin with, as Notion's: its files' own kind, or a page. */
function KindMark({ names }: { names?: readonly string[] }) {
  const kinds = new Set((names ?? []).map((name) => /\.([^.]+)$/.exec(name.toLowerCase())?.[1] ?? ""));
  const kind = kinds.size === 1 ? [...kinds][0] : "";
  if (kind === "pdf") return <PdfFileIcon size="regular" />;
  if (kind === "html" || kind === "htm") return <CodeIcon size="regular" />;
  if (kind === "docx") return <WordDocumentIcon size="regular" />;
  if (kind === "csv") return <TableIcon size="regular" />;
  return <FileText size="regular" />;
}

/**
 * The window a finished file import ends in, as Notion's Text & Markdown import
 * does (seen 2026-11-04 in the audit's side by side): "가져오기를 완료했습니다"
 * and "가져오기로 이동하기", which opens what it made.
 */
export function ImportFinishedWindow({ names, onClose, onOpen }: {
  /** The files' names, whose kind the window's first mark shows. */
  names?: readonly string[];
  onClose(): void;
  /** Opens what it made; without it the button closes the window. */
  onOpen?: () => void;
}) {
  const { t } = useTranslation(["importDialog", "common"]);
  const titleId = useId();
  const dialogRef = useRef<HTMLElement>(null);
  const { zIndex } = useOverlayLayer(true, {
    kind: "dialog", ref: dialogRef, modalOwned: () => [dialogRef.current?.parentElement], onEscape: onClose,
  });
  return createPortal(
    <div className={styles.overlay} style={{ zIndex }} data-import-finished>
      <button type="button" className={styles.backdrop} tabIndex={-1} aria-label={t("importDialog:close")} onClick={onClose} />
      <section ref={dialogRef} className={styles.dialog} data-finished="true" role="dialog" aria-modal="true" aria-labelledby={titleId}>
        <button type="button" className={styles.close} aria-label={t("importDialog:close")} onClick={onClose}>
          <X size="small" aria-hidden="true" />
        </button>
        <div className={doneStyles.done}>
          <div className={doneStyles.doneMarks} aria-hidden="true">
            <span className={doneStyles.doneTile}><KindMark names={names} /></span>
            <span className={doneStyles.doneCheck}><CheckIcon size="small" /></span>
            <img src="/mark-128.png" alt="" width={48} height={48} />
          </div>
          <h2 id={titleId} className={doneStyles.doneTitle}>{t("importDialog:csvWindow.doneTitle")}</h2>
          <p className={doneStyles.doneHint}>{t("importDialog:job.fileImported")}</p>
          <button type="button" className={doneStyles.doneOpen} data-import-finished-open onClick={onOpen ?? onClose}>
            {onOpen ? t("importDialog:csvWindow.openImport") : t("common:actions.close")}
          </button>
        </div>
      </section>
    </div>,
    document.body,
  );
}

/**
 * The window a file import ends in when nothing came in, as Notion's (seen
 * 2026-11-05): its file, a cross and the mark, "가져오기에 실패했습니다", why and
 * a button that closes it.
 */
export function ImportFailedWindow({ message, names, onClose }: { message: string; names?: readonly string[]; onClose(): void }) {
  const { t } = useTranslation(["importDialog", "common"]);
  const titleId = useId();
  const dialogRef = useRef<HTMLElement>(null);
  const { zIndex } = useOverlayLayer(true, {
    kind: "dialog", ref: dialogRef, modalOwned: () => [dialogRef.current?.parentElement], onEscape: onClose,
  });
  return createPortal(
    <div className={styles.overlay} style={{ zIndex }} data-import-failed>
      <button type="button" className={styles.backdrop} tabIndex={-1} aria-label={t("importDialog:close")} onClick={onClose} />
      <section ref={dialogRef} className={styles.dialog} data-finished="true" role="dialog" aria-modal="true" aria-labelledby={titleId}>
        <button type="button" className={styles.close} aria-label={t("importDialog:close")} onClick={onClose}>
          <X size="small" aria-hidden="true" />
        </button>
        <div className={doneStyles.done}>
          <div className={doneStyles.doneMarks} aria-hidden="true">
            <span className={doneStyles.doneTile}><KindMark names={names} /></span>
            <span className={doneStyles.doneCheck} data-failed="true"><X size="small" /></span>
            <img src="/mark-128.png" alt="" width={48} height={48} />
          </div>
          <h2 id={titleId} className={doneStyles.doneTitle}>{t("importDialog:batch.failedTitle")}</h2>
          {message ? <p className={doneStyles.doneHint}>{message}</p> : null}
          <button type="button" className={doneStyles.doneOpen} data-import-failed-close onClick={onClose}>
            {t("common:actions.close")}
          </button>
        </div>
      </section>
    </div>,
    document.body,
  );
}

/**
 * The window a file import started from a kind's window goes on in, as
 * Notion's "가져오는 중…" (seen 2026-11-04; measured 2026-11-07): its file's
 * kind, an arrow and the mark, the title, that the window can close while the
 * import goes on, and a close. Going back leaves it running, its rows in the
 * import screen and Completed, and its end in the finished window. Where
 * Notion promises an email Leafen says where the import ends up, as the
 * migration window does, since a server may send no mail.
 */
export function ImportProgressWindow({ names, onBack }: {
  /** The files' names, whose kind the window's first mark shows. */
  names?: readonly string[];
  onBack(): void;
}) {
  const { t } = useTranslation("importDialog");
  const titleId = useId();
  const dialogRef = useRef<HTMLElement>(null);
  const { zIndex } = useOverlayLayer(true, {
    kind: "dialog", ref: dialogRef, modalOwned: () => [dialogRef.current?.parentElement], onEscape: onBack,
  });
  return createPortal(
    <div className={styles.overlay} style={{ zIndex }} data-import-progress-window>
      <button type="button" className={styles.backdrop} tabIndex={-1} aria-label={t("importDialog:close")} onClick={onBack} />
      <section ref={dialogRef} className={styles.dialog} data-finished="true" role="dialog" aria-modal="true" aria-labelledby={titleId}>
        <button type="button" className={styles.close} aria-label={t("importDialog:close")} onClick={onBack}>
          <X size="small" aria-hidden="true" />
        </button>
        <div className={doneStyles.done}>
          <div className={doneStyles.progressMarks} aria-hidden="true">
            <span className={doneStyles.progressTile}><KindMark names={names} /></span>
            <ArrowRight size="small" className={doneStyles.progressArrow} />
            <img src="/mark-128.png" alt="" width={32} height={32} />
          </div>
          <h2 id={titleId} className={doneStyles.doneTitle}>{t("importDialog:migration.running")}</h2>
          <p className={`${doneStyles.doneHint} ${doneStyles.progressHint}`}>{t("importDialog:migration.closeAnytime")}</p>
          <button type="button" className={doneStyles.doneOpen} data-import-progress-back onClick={onBack}>
            {t("importDialog:batch.backToApp")}
          </button>
        </div>
      </section>
    </div>,
    document.body,
  );
}
