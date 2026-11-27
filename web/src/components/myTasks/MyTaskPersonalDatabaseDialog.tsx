import { useId, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { Pencil, X } from "@/icons/leafen";
import { useOverlayLayer } from "@/lib/overlayLayer";
import styles from "./MyTaskPersonalDatabaseDialog.module.css";

/**
 * "개인 작업 데이터베이스 이름 지정", as Notion's (seen 2026-11-05 at 1800 wide):
 * a centered 380 wide window with a name, a blue "데이터베이스 생성" that does
 * nothing until it has one, and "취소".
 */
export function MyTaskPersonalDatabaseDialog({ userName, onCreate, onClose }: { userName?: string; onCreate(name: string): Promise<void>; onClose(): void }) {
  const { t } = useTranslation("myTasks");
  const titleId = useId();
  const overlayRef = useRef<HTMLDivElement>(null);
  const dialogRef = useRef<HTMLFormElement>(null);
  const [name, setName] = useState("");
  const [busy, setBusy] = useState(false);
  const [failed, setFailed] = useState(false);
  const { zIndex } = useOverlayLayer(true, { kind: "dialog", ref: dialogRef, modalOwned: () => [overlayRef.current], onEscape: onClose });
  return createPortal(
    <div ref={overlayRef} className={styles.overlay} style={{ zIndex }} data-my-task-personal-database>
      <div className={styles.backdrop} onClick={onClose} />
      <form ref={dialogRef} className={styles.dialog} role="dialog" aria-modal="true" aria-labelledby={titleId} onSubmit={(event) => {
        event.preventDefault();
        if (busy || !name.trim()) return;
        setBusy(true);
        setFailed(false);
        onCreate(name.trim()).catch(() => { setFailed(true); setBusy(false); });
      }}>
        <button type="button" className={styles.close} aria-label={t("close")} onClick={onClose}><X size="small" aria-hidden="true" /></button>
        <Pencil size="large" className={styles.art} aria-hidden="true" />
        <h2 id={titleId} className={styles.title}>{t("namePersonalDatabase")}</h2>
        <input className={styles.field} autoFocus value={name} placeholder={userName ? t("personalDatabasePlaceholder", { name: userName }) : undefined} aria-labelledby={titleId}
          onChange={(event) => setName(event.target.value)} data-my-task-personal-name />
        {failed ? <p className={styles.error} role="alert">{t("couldntCreate")}</p> : null}
        <button type="submit" className={styles.create} data-my-task-personal-create>{t("createDatabase")}</button>
        <button type="button" className={styles.cancel} onClick={onClose}>{t("cancel")}</button>
      </form>
    </div>,
    document.body,
  );
}
