import { useId, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import type { AppRemovalPreview } from "@/appPlatformWire.generated";
import type { Session } from "@/types";
import { WorkspaceExportDialog } from "@/components/WorkspaceExportDialog";
import { useOverlayLayer } from "@/lib/overlayLayer";
import pageStyles from "./AdministratorSettingsPage.module.css";
import styles from "./AdministratorApps.module.css";

/**
 * What goes with an app that has data (docs/app-platform.md 6): each
 * organization's app workspace with its pages, databases and people, then
 * the choice — the app and its data to the trash for thirty days, or the
 * data kept as ordinary workspaces. A legal hold keeps the data, so it can
 * only be kept.
 */
export function AppRemovalDialog({ session, name, preview, busy, error, onRemove, onClose }: {
  session: Session;
  name: string;
  preview: AppRemovalPreview;
  busy: boolean;
  error: string;
  onRemove: (mode: "trash" | "keep") => void;
  onClose: () => void;
}) {
  const { t } = useTranslation(["serverConsole", "common"]);
  const titleId = useId();
  const overlayRef = useRef<HTMLDivElement>(null);
  const dialogRef = useRef<HTMLElement>(null);
  const cancelRef = useRef<HTMLButtonElement>(null);
  const [mode, setMode] = useState<"trash" | "keep">(preview.legalHold ? "keep" : "trash");
  const [exporting, setExporting] = useState<AppRemovalPreview["workspaces"][number] | null>(null);
  const { zIndex } = useOverlayLayer(true, {
    kind: "alertdialog", ref: dialogRef, modalOwned: () => [overlayRef.current], onEscape: () => { if (!busy) onClose(); }, initialFocus: cancelRef,
  });
  return createPortal(
    <div ref={overlayRef} className={styles.overlay} style={{ zIndex }} data-app-removal>
      <button type="button" className={styles.backdrop} tabIndex={-1} aria-label={t("common:actions.close")} disabled={busy} onClick={onClose} />
      <section ref={dialogRef} className={styles.dialog} role="alertdialog" aria-modal="true" aria-labelledby={titleId} aria-busy={busy || undefined}>
        <h2 id={titleId} className={styles.dialogTitle}>{t("apps.removeConfirmTitle", { name })}</h2>
        <p className={styles.note}>{t("apps.removal.intro")}</p>
        <ul className={styles.permissions} data-app-removal-workspaces>
          {preview.workspaces.map((workspace) => (
            <li key={workspace.workspaceId}>
              {t("apps.removal.workspace", {
                tenancy: workspace.tenancyName || t("apps.serverTenancy"), pages: workspace.pages,
                databases: workspace.databases, people: workspace.people,
              })}
              {workspace.legalHold ? ` · ${t("apps.removal.held")}` : ""}
              {" "}
              <button type="button" className={pageStyles.secondaryButton} disabled={busy} data-app-removal-export
                onClick={() => setExporting(workspace)}>{t("apps.removal.exportButton")}</button>
            </li>
          ))}
        </ul>
        {preview.legalHold ? <p className={styles.note} data-app-removal-hold>{t("apps.removal.legalHold")}</p> : null}
        <label className={styles.acknowledge}>
          <input type="radio" name="app-removal" checked={mode === "trash"} disabled={busy || preview.legalHold} data-app-removal-mode="trash" onChange={() => setMode("trash")} />
          {t("apps.removal.trash", { days: preview.keepDays })}
        </label>
        <label className={styles.acknowledge}>
          <input type="radio" name="app-removal" checked={mode === "keep"} disabled={busy} data-app-removal-mode="keep" onChange={() => setMode("keep")} />
          {t("apps.removal.keep")}
        </label>
        {error ? <div className={styles.error} role="alert">{error}</div> : null}
        <div className={styles.dialogActions}>
          <button ref={cancelRef} type="button" className={styles.cancel} disabled={busy} onClick={onClose}>{t("common:actions.cancel")}</button>
          <button type="button" className={styles.danger} disabled={busy} data-app-removal-confirm onClick={() => onRemove(mode)}>
            {t("apps.remove")}
          </button>
        </div>
      </section>
      {exporting ? (
        <WorkspaceExportDialog session={session} workspaceId={exporting.workspaceId} privatePagesAtFirst
          workspaceName={exporting.tenancyName || t("apps.serverTenancy")} onClose={() => setExporting(null)} />
      ) : null}
    </div>,
    document.body,
  );
}
