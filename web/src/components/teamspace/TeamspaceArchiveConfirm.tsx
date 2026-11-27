import { useId, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { useOverlayLayer } from "@/lib/overlayLayer";
import { WarningCircleIcon } from "@/icons/leafen";
import styles from "./TeamspaceSettings.module.css";
import { fieldEnter } from "@/lib/fieldKeyBoundary";

/**
 * "이 팀스페이스를 보관하시겠습니까?", as Notion asks from the sidebar and the
 * settings alike (seen 2026-10-06): the teamspace's name typed in its own
 * box archives it; another name says what to type and stays.
 */
export function TeamspaceArchiveConfirm({ name, onArchive, onClose }: {
  name: string; onArchive(): Promise<boolean>; onClose(): void;
}) {
  const { t } = useTranslation(["teamspaceDialog", "common"]);
  const dialogRef = useRef<HTMLDivElement>(null);
  const inputRef = useRef<HTMLInputElement>(null);
  const titleId = useId();
  const [typed, setTyped] = useState("");
  const [mismatch, setMismatch] = useState(false);
  const [busy, setBusy] = useState(false);
  const { zIndex } = useOverlayLayer(true, {
    kind: "alertdialog", ref: dialogRef, modalOwned: () => [dialogRef.current?.parentElement], onEscape: () => { if (!busy) onClose(); },
    initialFocus: inputRef,
  });

  async function archive() {
    if (busy) return;
    if (typed.trim() !== name.trim()) {
      setMismatch(true);
      inputRef.current?.focus();
      return;
    }
    setBusy(true);
    const archived = await onArchive();
    setBusy(false);
    if (archived) onClose();
  }

  return createPortal(
    <div className={styles.confirmOverlay} style={{ zIndex }}>
      <button type="button" className={styles.confirmBackdrop} tabIndex={-1} aria-label={t("common:actions.close")} onClick={() => { if (!busy) onClose(); }} />
      <div ref={dialogRef} className={styles.confirm} role="alertdialog" aria-modal="true" aria-labelledby={titleId} data-teamspace-archive-confirm>
        <span className={styles.confirmMark} aria-hidden="true"><WarningCircleIcon size="card" /></span>
        <h2 id={titleId}>{t("teamspaceDialog:archiveConfirm.title")}</h2>
        <p>{t("teamspaceDialog:archiveConfirm.body")}</p>
        <input ref={inputRef} value={typed} placeholder={name} aria-label={t("teamspaceDialog:archiveConfirm.nameLabel")} disabled={busy}
          aria-invalid={mismatch || undefined}
          onChange={(event) => { setTyped(event.target.value); setMismatch(false); }}
          onKeyDown={(event) => {
            if (!fieldEnter(event)) return;
            event.preventDefault();
            void archive();
          }} />
        {mismatch ? <p className={styles.confirmError} role="alert">{t("teamspaceDialog:archiveConfirm.mismatch", { name })}</p> : null}
        <button type="button" className={styles.confirmDanger} disabled={busy} onClick={() => void archive()}>
          {t("teamspaceDialog:archiveConfirm.confirm")}
        </button>
        <button type="button" className={styles.confirmCancel} disabled={busy} onClick={onClose}>{t("common:actions.cancel")}</button>
      </div>
    </div>,
    document.body,
  );
}
