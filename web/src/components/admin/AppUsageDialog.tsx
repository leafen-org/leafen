import { useEffect, useId, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import type { AppUsage } from "@/appPlatformWire.generated";
import { triggerBrowserFileDownload } from "@/lib/browserFileDownload";
import { formatFileSize } from "@/components/database/files";
import { useOverlayLayer } from "@/lib/overlayLayer";
import { settingsErrorMessage } from "@/lib/settingsErrors";
import type { Session } from "@/types";
import { exportAppLog, getAppUsage } from "./administratorAppsApi";
import { AppEndpointUsage } from "./AppEndpointUsage";
import styles from "./AdministratorApps.module.css";
import usageStyles from "./AppUsageDialog.module.css";
import { ReadFailed } from "../ReadFailed";

/**
 * What an app used over the last day (docs/app-platform.md 15): its calls to
 * Leafen by hour, worker runs, relayed traffic, its temporary store and who
 * is in its rooms, beside its limits; and its log to download without
 * anyone's data.
 */
export function AppUsageDialog({ session, installId, name, slug, onClose }: {
  session: Session;
  installId: string;
  name: string;
  slug: string;
  onClose: () => void;
}) {
  const { t } = useTranslation(["serverConsole", "common"]);
  const titleId = useId();
  const overlayRef = useRef<HTMLDivElement>(null);
  const dialogRef = useRef<HTMLElement>(null);
  const closeRef = useRef<HTMLButtonElement>(null);
  const [usage, setUsage] = useState<AppUsage | null>(null);
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
    getAppUsage(session, installId).then(
      (read) => { if (live) setUsage(read); },
      (failure) => { if (live) setReadFailed(settingsErrorMessage(failure, t("apps.readFailed"))); },
    );
    return () => { live = false; };
  }, [readTurn, installId, session, t]);

  async function download() {
    setBusy(true);
    setError("");
    try {
      const log = await exportAppLog(session, installId);
      const day = new Date().toISOString().slice(0, 10).replaceAll("-", "");
      triggerBrowserFileDownload(new Blob([JSON.stringify(log, null, 2)], { type: "application/json" }), `${slug}-log-${day}.json`,
        { revokeAfterMs: 30_000 });
    } catch (failure) {
      setError(settingsErrorMessage(failure, t("apps.usage.exportFailed")));
    } finally {
      setBusy(false);
    }
  }

  const sum = (pick: (hour: AppUsage["hours"][number]) => number) => usage?.hours.reduce((total, hour) => total + pick(hour), 0) ?? 0;
  const busiest = Math.max(1, ...(usage?.hours.map((hour) => hour.reads + hour.writes) ?? [1]));
  const number = (value: number) => value.toLocaleString();
  return createPortal(
    <div ref={overlayRef} className={styles.overlay} style={{ zIndex }} data-app-usage>
      <button type="button" className={styles.backdrop} tabIndex={-1} aria-label={t("common:actions.close")} disabled={busy} onClick={onClose} />
      <section ref={dialogRef} className={styles.dialog} role="dialog" aria-modal="true" aria-labelledby={titleId} aria-busy={busy || undefined}>
        <h2 id={titleId} className={styles.dialogTitle}>{t("apps.usage.title", { name })}</h2>
        {usage ? (
          <>
            <p className={styles.note}>{t("apps.usage.body")}</p>
            {usage.pausedReason === "budget" ? <p className={styles.note} data-app-usage-paused>{t("apps.pausedBudget")}</p> : null}
            <h3 className={styles.permissionsTitle}>{t("apps.usage.calls")}</h3>
            <div className={usageStyles.bars} aria-hidden="true" data-app-usage-hours>
              {usage.hours.map((hour) => (
                <span key={hour.hour} className={usageStyles.bar} data-refused={hour.refused > 0 || undefined}
                  style={{ height: `${Math.max(2, Math.round(((hour.reads + hour.writes) / busiest) * 40))}px` }} />
              ))}
            </div>
            <dl className={usageStyles.facts} data-app-usage-calls>
              <dt>{t("apps.usage.reads")}</dt><dd>{number(sum((hour) => hour.reads))}</dd>
              <dt>{t("apps.usage.writes")}</dt><dd>{number(sum((hour) => hour.writes))}</dd>
              <dt>{t("apps.usage.delayed")}</dt><dd>{number(sum((hour) => hour.delayed))}</dd>
              <dt>{t("apps.usage.refused")}</dt><dd>{number(sum((hour) => hour.refused))}</dd>
              <dt>{t("apps.usage.limit")}</dt>
              <dd>{t("apps.usage.limitValue", { calls: usage.callsPerMinute, writes: usage.writesPerMinute })}</dd>
            </dl>
            <h3 className={styles.permissionsTitle}>{t("apps.usage.workers")}</h3>
            <dl className={usageStyles.facts} data-app-usage-workers>
              <dt>{t("apps.usage.runs")}</dt><dd>{t("apps.usage.runsValue", { runs: number(usage.runs), failures: number(usage.runFailures) })}</dd>
              <dt>{t("apps.usage.runTime")}</dt><dd>{t("apps.usage.seconds", { seconds: (usage.runMs / 1000).toFixed(1) })}</dd>
              <dt>{t("apps.usage.hostCalls")}</dt><dd>{number(usage.hostCalls)}</dd>
              <dt>{t("apps.usage.limit")}</dt>
              <dd>{t("apps.usage.workerLimit", { memory: usage.workerMemoryMb, seconds: usage.workerSeconds })}</dd>
            </dl>
            <h3 className={styles.permissionsTitle}>{t("apps.usage.outside")}</h3>
            <dl className={usageStyles.facts} data-app-usage-relay>
              <dt>{t("apps.usage.relayCalls")}</dt><dd>{t("apps.usage.runsValue", { runs: number(usage.relayCalls), failures: number(usage.relayFailures) })}</dd>
              <dt>{t("apps.usage.relayBytes")}</dt>
              <dd>{t("apps.usage.bytesValue", { sent: formatFileSize(usage.relaySentBytes), received: formatFileSize(usage.relayReceivedBytes) })}</dd>
            </dl>
            {usage.endpoints ? <AppEndpointUsage session={session} installId={installId} usage={usage.endpoints} /> : null}
            <h3 className={styles.permissionsTitle}>{t("apps.usage.now")}</h3>
            <dl className={usageStyles.facts} data-app-usage-now>
              <dt>{t("apps.usage.rooms")}</dt><dd>{t("apps.usage.roomsValue", { people: usage.roomPeople, guests: usage.roomGuests })}</dd>
              {usage.stores.map((store) => (
                <span key={store.tenancyKey} className={usageStyles.factRow}>
                  {/* The organization in the value, so the name column stays short (2026-11-06). */}
                  <dt>{t("apps.usage.store")}</dt>
                  <dd>{t("apps.usage.storeValue", { used: formatFileSize(store.bytes), limit: formatFileSize(store.maxBytes), name: store.name || store.tenancyKey })}</dd>
                </span>
              ))}
            </dl>
          </>
        ) : null}
        {readFailed ? <ReadFailed message={readFailed} onRetry={() => { setReadFailed(""); setReadTurn((turn) => turn + 1); }} /> : null}
        {error ? <div className={styles.error} role="alert">{error}</div> : null}
        <div className={styles.dialogActions}>
          <button type="button" className={styles.secondary} disabled={busy || !usage} data-app-usage-export onClick={() => void download()}>
            {t("apps.usage.export")}
          </button>
          <button ref={closeRef} type="button" className={styles.secondary} disabled={busy} onClick={onClose}>{t("common:actions.close")}</button>
        </div>
      </section>
    </div>,
    document.body,
  );
}
