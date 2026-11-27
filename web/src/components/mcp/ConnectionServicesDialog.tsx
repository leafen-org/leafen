import { useEffect, useId, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { useOverlayLayer } from "@/lib/overlayLayer";
import { settingsErrorMessage } from "@/lib/settingsErrors";
import { readConnectionServices, setConnectionServices, type MCPConnectionServices } from "@/mcpServicesApi";
import type { Session } from "@/types";
import styles from "@/components/admin/AdministratorApps.module.css";
import { ReadFailed } from "../ReadFailed";

/**
 * Which of the person's connected outside services a connection's agent
 * uses through Leafen: every one, also those connected later, or only the
 * chosen ones. Only the person changes it, not the agent.
 */
export function ConnectionServicesDialog({ session, connectionId, name, onClose }: {
  session: Session;
  connectionId: string;
  name: string;
  onClose: () => void;
}) {
  const { t } = useTranslation("workspaceSettingsDialog");
  const titleId = useId();
  const overlayRef = useRef<HTMLDivElement>(null);
  const dialogRef = useRef<HTMLElement>(null);
  const closeRef = useRef<HTMLButtonElement>(null);
  const [choice, setChoice] = useState<MCPConnectionServices | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  // The first read that failed, said where what it reads shows, with a way to read again.
  const [readFailed, setReadFailed] = useState("");
  const [readTurn, setReadTurn] = useState(0);
  const { zIndex } = useOverlayLayer(true, {
    kind: "dialog", ref: dialogRef, modalOwned: () => [overlayRef.current], onEscape: () => { if (!busy) onClose(); }, initialFocus: closeRef,
  });

  useEffect(() => {
    let live = true;
    readConnectionServices(session, connectionId).then(
      (read) => { if (live) setChoice(read); },
      (failure) => { if (live) setReadFailed(settingsErrorMessage(failure, t("connectionServices.readFailed"))); },
    );
    return () => { live = false; };
  }, [readTurn, connectionId, session, t]);

  // The services come a window at a time; each choice changes one of them.
  async function loadMore() {
    if (!choice?.nextCursor) return;
    setBusy(true);
    setError("");
    try {
      const read = await readConnectionServices(session, connectionId, choice.nextCursor);
      setChoice((shown) => shown ? { ...shown, services: [...shown.services, ...read.services], nextCursor: read.nextCursor } : read);
    } catch (failure) {
      setError(settingsErrorMessage(failure, t("connectionServices.readFailed")));
    } finally {
      setBusy(false);
    }
  }

  async function save(mode: "all" | "chosen", change?: { serviceId: string; chosen: boolean }) {
    setBusy(true);
    setError("");
    try {
      const saved = await setConnectionServices(session, connectionId, mode, change);
      setChoice((shown) => shown ? {
        ...shown,
        mode: saved.mode,
        services: shown.services.map((service) => service.id === change?.serviceId ? { ...service, chosen: change.chosen } : service),
      } : shown);
    } catch (failure) {
      setError(settingsErrorMessage(failure, t("connectionServices.saveFailed")));
    } finally {
      setBusy(false);
    }
  }

  return createPortal(
    <div ref={overlayRef} className={styles.overlay} style={{ zIndex }} data-connection-services>
      <button type="button" className={styles.backdrop} tabIndex={-1} aria-label={t("connectionApps.close")} disabled={busy} onClick={onClose} />
      <section ref={dialogRef} className={styles.dialog} role="dialog" aria-modal="true" aria-labelledby={titleId} aria-busy={busy || undefined}>
        <h2 id={titleId} className={styles.dialogTitle}>{t("connectionServices.title", { name })}</h2>
        {choice ? (
          <>
            <p className={styles.note}>{t("connectionServices.body")}</p>
            <label className={styles.jobRow}>
              <input type="radio" name="connection-services" checked={choice.mode === "all"} disabled={busy} data-connection-services-all
                onChange={() => void save("all")} />
              <span className={styles.secretName}><strong>{t("connectionServices.all")}</strong><span>{t("connectionServices.allBody")}</span></span>
            </label>
            <label className={styles.jobRow}>
              <input type="radio" name="connection-services" checked={choice.mode === "chosen"} disabled={busy} data-connection-services-chosen
                onChange={() => void save("chosen")} />
              <span className={styles.secretName}><strong>{t("connectionServices.chosen")}</strong></span>
            </label>
            {choice.mode === "chosen" ? (
              <>
                <h3 className={styles.permissionsTitle}>{t("connectionServices.services")}</h3>
                {choice.services.length ? choice.services.map((service) => (
                  <label key={service.id} className={styles.jobRow} data-connection-service={service.id}>
                    <input type="checkbox" checked={service.chosen} disabled={busy}
                      onChange={(event) => void save("chosen", { serviceId: service.id, chosen: event.target.checked })} />
                    <span className={styles.secretName}>
                      <strong>{service.name || service.address}</strong>
                      <span>{[service.address, t("connectionApps.tools", { count: service.tools })].join(" · ")}</span>
                    </span>
                  </label>
                )) : <p className={styles.note}>{t("connectionServices.none")}</p>}
                {choice.nextCursor ? (
                  <button type="button" className={styles.secondary} disabled={busy} data-connection-services-more
                    onClick={() => void loadMore()}>{t("loadMore")}</button>
                ) : null}
              </>
            ) : null}
          </>
        ) : null}
        {readFailed ? <ReadFailed message={readFailed} onRetry={() => { setReadFailed(""); setReadTurn((turn) => turn + 1); }} /> : null}
        {error ? <div className={styles.error} role="alert">{error}</div> : null}
        <div className={styles.dialogActions}>
          <button ref={closeRef} type="button" className={styles.secondary} disabled={busy} onClick={onClose}>{t("connectionApps.close")}</button>
        </div>
      </section>
    </div>,
    document.body,
  );
}
