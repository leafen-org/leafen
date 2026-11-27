import { X } from "@/icons/leafen";
import { useOverlayLayer } from "@/lib/overlayLayer";
import { focusOpenedField } from "@/components/editor/openedFieldCaret";
import { useEffect, useId, useRef } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";

import type { Session } from "../../types";
import { NotionImportPanel } from "./NotionImportPanel";
import type { NotionImportArrival, NotionImportController } from "./useNotionImportController";
import importStyles from "./ImportDialog.module.css";
import styles from "./NotionMigrationWindow.module.css";

// "Notion에서 옮기기": a window like Notion's
// Confluence migration window, where the person connects Notion, picks the
// root and imports it. Someone who wants to change the structure on the way
// tells their connected agent, which has the migration tools.

function NotionMigrationBody({ arrival, controller, session, workspaceId }: {
  arrival: NotionImportArrival;
  controller: NotionImportController;
  session: Session;
  workspaceId: string;
}) {
  return (
    <div className={styles.body}>
      <NotionImportPanel controller={controller} arrival={arrival} session={session} workspaceId={workspaceId} />
    </div>
  );
}

export function NotionMigrationWindow({ arrival = "", controller, onClose, session, workspaceId, workspaceName = "" }: {
  /** How it was come to: by an agent's link it opens at the token. */
  arrival?: NotionImportArrival;
  controller: NotionImportController;
  onClose(): void;
  session: Session;
  workspaceId: string;
  /** The workspace it moves into, named over the window (2026-11-22: a move ran in another workspace than the one shown). */
  workspaceName?: string;
}) {
  const { t } = useTranslation("importDialog");
  const titleId = useId();
  const dialogRef = useRef<HTMLElement>(null);
  const closeRef = useRef<HTMLButtonElement>(null);
  const { zIndex } = useOverlayLayer(true, {
    kind: "dialog", ref: dialogRef, modalOwned: () => [dialogRef.current?.parentElement], onEscape: onClose,
  });
  // The window opens on what is first to fill — the connection's token, a
  // field to type in — as a window asking for words does; the close button
  // only when the window has nothing to fill yet (2026-11-09).
  useEffect(() => {
    return focusOpenedField(() => dialogRef.current?.querySelector<HTMLElement>("input:not([type=hidden]):not(:disabled), textarea:not(:disabled)")
      ?? closeRef.current);
  }, []);
  const window_ = (
    <div className={importStyles.overlay} style={{ zIndex }} data-notion-migration-window>
      <button type="button" className={importStyles.backdrop} onClick={onClose} tabIndex={-1} aria-label={t("importDialog:close")} />
      <section ref={dialogRef} className={`${importStyles.dialog} ${styles.window}`} role="dialog" aria-modal="true" aria-labelledby={titleId}>
        <header className={styles.header}>
          <div>
            <h2 id={titleId}>{t("importDialog:window.title")}</h2>
            <p data-notion-migration-into={workspaceName ? "" : undefined}>
              {workspaceName.trim() ? t("importDialog:window.leadInto", { workspace: workspaceName.trim() }) : t("importDialog:window.lead")}
            </p>
          </div>
          <button ref={closeRef} type="button" className={importStyles.close} onClick={onClose} aria-label={t("importDialog:close")}>
            <X size="regular" aria-hidden="true" />
          </button>
        </header>
        <NotionMigrationBody arrival={arrival} controller={controller} session={session} workspaceId={workspaceId} />
      </section>
    </div>
  );
  return typeof document === "undefined" ? window_ : createPortal(window_, document.body);
}
