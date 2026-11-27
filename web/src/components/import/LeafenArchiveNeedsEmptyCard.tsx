import { useTranslation } from "react-i18next";

import archiveStyles from "./DirectLeafenArchiveImportPanel.module.css";
import styles from "./ImportDialog.module.css";

/**
 * A Leafen bundle goes only into a workspace with no pages (the server's own
 * check, workspaceImportArchiveTargetFresh): one that has pages says so, with
 * the way to an empty one, where the file dropped here goes in at once
 * (lib/openWorkspace.ts holds it while the new workspace is made). Without
 * one — none dropped, or the one dropped already went in — the new workspace
 * asks for the file, and the card says that instead.
 */
export function LeafenArchiveNeedsEmptyCard({ file, onNewWorkspace }: {
  file: File | null;
  onNewWorkspace?: (file: File | null) => void;
}) {
  const { t } = useTranslation("importDialog");
  return (
    <section className={styles.panel}>
      <div className={archiveStyles.jobCard} data-leafen-archive-needs-empty>
        <strong>{t("importDialog:archive.needsEmptyTitle")}</strong>
        <span className={archiveStyles.jobHint}>{t(file ? "importDialog:archive.needsEmptyBody" : "importDialog:archive.needsEmptyBodyChoose")}</span>
        {onNewWorkspace ? (
          <span className={archiveStyles.jobActions}>
            <button type="button" className={styles.primary} data-leafen-archive-new-workspace onClick={() => onNewWorkspace(file)}>
              {t("importDialog:archive.newWorkspace")}
            </button>
          </span>
        ) : null}
      </div>
    </section>
  );
}
