import { useEffect, useId, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { request } from "@/apiTransport";
import { appPlatformOperations, type AppDataRead, type AppDataReads } from "@/appPlatformWire.generated";
import { activeDateLocale } from "@/lib/i18n";
import { useOverlayLayer } from "@/lib/overlayLayer";
import { settingsErrorMessage } from "@/lib/settingsErrors";
import type { Session } from "@/types";
import dialogStyles from "../admin/AdministratorApps.module.css";
import styles from "./AppDataReadsDialog.module.css";
import { displayDate } from "@/lib/displayDate";

/**
 * What this app read of the person's data in the last 30 days
 * (docs/app-platform.md 15): each thing it read, how often and when last,
 * named where the person can still read it.
 */
export function AppDataReadsDialog({ session, installId, name, onClose }: {
  session: Session;
  installId: string;
  name: string;
  onClose: () => void;
}) {
  const { t } = useTranslation("common");
  const titleId = useId();
  const overlayRef = useRef<HTMLDivElement>(null);
  const dialogRef = useRef<HTMLElement>(null);
  const closeRef = useRef<HTMLButtonElement>(null);
  const [reads, setReads] = useState<AppDataReads | null>(null);
  const [error, setError] = useState("");
  const { zIndex } = useOverlayLayer(true, {
    kind: "dialog", ref: dialogRef, modalOwned: () => [overlayRef.current], onEscape: onClose, initialFocus: closeRef,
  });

  useEffect(() => {
    let live = true;
    request<AppDataReads>(appPlatformOperations.dataReads.path.replace("{installId}", encodeURIComponent(installId)),
      { method: appPlatformOperations.dataReads.method }, session).then(
      (read) => { if (live) setReads(read); },
      (failure) => { if (live) setError(settingsErrorMessage(failure, t("appHost.reads.failed"))); },
    );
    return () => { live = false; };
  }, [installId, session, t]);

  // The last read's day in the reader's own time zone; a row kept before
  // reads kept their moment knows only its UTC day, said as that day.
  const day = (item: AppDataRead) => item.lastAt
    ? displayDate(item.lastAt)
    : new Date(item.lastDay).toLocaleDateString(activeDateLocale(), { timeZone: "UTC" });
  return createPortal(
    <div ref={overlayRef} className={dialogStyles.overlay} style={{ zIndex }} data-app-data-reads>
      <button type="button" className={dialogStyles.backdrop} tabIndex={-1} aria-label={t("actions.close")} onClick={onClose} />
      <section ref={dialogRef} className={dialogStyles.dialog} role="dialog" aria-modal="true" aria-labelledby={titleId}>
        <h2 id={titleId} className={dialogStyles.dialogTitle}>{t("appHost.reads.title", { name })}</h2>
        <p className={dialogStyles.note}>{t("appHost.reads.body")}</p>
        {reads ? (
          reads.items.length ? (
            <>
              <p className={dialogStyles.note} data-app-data-reads-total>{t("appHost.reads.total", { count: reads.total })}</p>
              <ul className={styles.list} data-app-data-reads-items>
                {reads.items.map((item) => (
                  <li key={`${item.kind}/${item.targetId}`} data-kind={item.kind}>
                    <span className={styles.what}>
                      <strong>{item.title || t(`appHost.reads.kind.${item.kind}`)}</strong>
                      {item.title ? <span>{t(`appHost.reads.kind.${item.kind}`)}</span> : null}
                    </span>
                    <span className={styles.when}>{t("appHost.reads.count", { count: item.reads, date: day(item) })}</span>
                  </li>
                ))}
              </ul>
            </>
          ) : <p className={dialogStyles.note} data-app-data-reads-none>{t("appHost.reads.none")}</p>
        ) : null}
        {error ? <div className={dialogStyles.error} role="alert">{error}</div> : null}
        <div className={dialogStyles.dialogActions}>
          <button ref={closeRef} type="button" className={dialogStyles.secondary} onClick={onClose}>{t("actions.close")}</button>
        </div>
      </section>
    </div>,
    document.body,
  );
}
