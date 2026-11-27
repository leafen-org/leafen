import { useEffect, useId, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { request } from "@/apiTransport";
import { appPlatformOperations, type AppConnectionApps } from "@/appPlatformWire.generated";
import { appMaker, appText } from "@/components/admin/administratorAppsApi";
import { useOverlayLayer } from "@/lib/overlayLayer";
import { settingsErrorMessage } from "@/lib/settingsErrors";
import type { Session } from "@/types";
import styles from "@/components/admin/AdministratorApps.module.css";
import { ReadFailed } from "../ReadFailed";

/**
 * Which installed apps' tools a connection reaches (docs/app-platform.md 13):
 * every app its person may use, or only the chosen ones — whose tools then
 * also show in the agent's list by their own names.
 */
export function ConnectionAppsDialog({ session, connectionId, name, onClose }: {
  session: Session;
  connectionId: string;
  name: string;
  onClose: () => void;
}) {
  const { t, i18n } = useTranslation(["workspaceSettingsDialog", "serverConsole"]);
  const titleId = useId();
  const overlayRef = useRef<HTMLDivElement>(null);
  const dialogRef = useRef<HTMLElement>(null);
  const closeRef = useRef<HTMLButtonElement>(null);
  const [choice, setChoice] = useState<AppConnectionApps | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  // The first read that failed, said where what it reads shows, with a way to read again.
  const [readFailed, setReadFailed] = useState("");
  const [readTurn, setReadTurn] = useState(0);
  const path = (template: string) => template.replace("{connectionId}", encodeURIComponent(connectionId));
  const { zIndex } = useOverlayLayer(true, {
    kind: "dialog", ref: dialogRef, modalOwned: () => [overlayRef.current], onEscape: () => { if (!busy) onClose(); }, initialFocus: closeRef,
  });

  useEffect(() => {
    let live = true;
    request<AppConnectionApps>(path(appPlatformOperations.readConnectionApps.path), { method: appPlatformOperations.readConnectionApps.method }, session).then(
      (read) => { if (live) setChoice(read); },
      (failure) => { if (live) setReadFailed(settingsErrorMessage(failure, t("connectionApps.readFailed"))); },
    );
    return () => { live = false; };
    // Read once for this connection.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [readTurn, connectionId, session]);

  async function save(mode: "all" | "chosen", installIds: string[]) {
    setBusy(true);
    setError("");
    try {
      setChoice(await request<AppConnectionApps>(path(appPlatformOperations.setConnectionApps.path), {
        method: appPlatformOperations.setConnectionApps.method, body: JSON.stringify({ mode, installIds }),
      }, session));
    } catch (failure) {
      setError(settingsErrorMessage(failure, t("connectionApps.saveFailed")));
    } finally {
      setBusy(false);
    }
  }

  const chosenIds = choice?.apps.filter((app) => app.chosen).map((app) => app.installId) ?? [];
  return createPortal(
    <div ref={overlayRef} className={styles.overlay} style={{ zIndex }} data-connection-apps>
      <button type="button" className={styles.backdrop} tabIndex={-1} aria-label={t("connectionApps.close")} disabled={busy} onClick={onClose} />
      <section ref={dialogRef} className={styles.dialog} role="dialog" aria-modal="true" aria-labelledby={titleId} aria-busy={busy || undefined}>
        <h2 id={titleId} className={styles.dialogTitle}>{t("connectionApps.title", { name })}</h2>
        {choice ? (
          <>
            <p className={styles.note}>{t("connectionApps.body")}</p>
            <label className={styles.jobRow}>
              <input type="radio" name="connection-apps" checked={choice.mode === "all"} disabled={busy} data-connection-apps-all
                onChange={() => void save("all", chosenIds)} />
              <span className={styles.secretName}><strong>{t("connectionApps.all")}</strong><span>{t("connectionApps.allBody")}</span></span>
            </label>
            <label className={styles.jobRow}>
              <input type="radio" name="connection-apps" checked={choice.mode === "chosen"} disabled={busy} data-connection-apps-chosen
                onChange={() => void save("chosen", chosenIds)} />
              <span className={styles.secretName}><strong>{t("connectionApps.chosen")}</strong><span>{t("connectionApps.chosenBody")}</span></span>
            </label>
            {choice.mode === "chosen" ? (
              <>
                <h3 className={styles.permissionsTitle}>{t("connectionApps.apps")}</h3>
                {choice.apps.length ? choice.apps.map((app) => (
                  <label key={`${app.installId}-${app.organization}`} className={styles.jobRow} data-connection-app={app.slug}>
                    <input type="checkbox" checked={app.chosen} disabled={busy}
                      onChange={(event) => void save("chosen", event.target.checked
                        ? [...chosenIds, app.installId] : chosenIds.filter((id) => id !== app.installId))} />
                    <span className={styles.secretName}>
                      <strong>{appText(app.name, i18n.language)}</strong>
                      <span>{[((maker) => maker.builtin ? t("serverConsole:apps.trust.builtin") : t("connectionApps.by", { developer: maker.developer }))(
                        appMaker(app.developer, app.builtin, i18n.language)), app.organization,
                        t("connectionApps.tools", { count: app.tools })].filter(Boolean).join(" · ")}</span>
                    </span>
                  </label>
                )) : <p className={styles.note}>{t("connectionApps.none")}</p>}
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
