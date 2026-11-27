import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { api } from "../../api";
import { administratorErrorCodes, type AdministratorScheduledBackup as Schedule } from "../../administratorWire.generated";
import { LeafenApiError } from "../../apiTransport";
import { followAcrossTabs, tellTabs } from "../../lib/followAcrossTabs";
import { activeDateLocale } from "../../lib/i18n";
import { productErrorMessage } from "../../lib/productErrorPresentation";
import type { Session } from "../../types";
import { announceScheduledBackupSaved } from "./BackupFailureNotice";
import { replicationFailureText } from "./scheduledBackupFailure";
import styles from "./AdministratorSettingsPage.module.css";
import { SettingSwitch } from "./AdministratorSettingRows";
import dataStyles from "./AdministratorDataManagement.module.css";

// While the replica goes, its state is read again this often.
const STATUS_MS = 5000;

/**
 * Continuous replication beside the scheduled backups (2026-10-17): with
 * backups on, every change can go out within seconds to the same place, so
 * another server can come back a few seconds behind. The switch, how far
 * behind the replica is, why it fails; and on a server whose data moved to
 * another one, keeping it apart as a server of its own.
 */
export function ReplicationSetting({ session, schedule, onSchedule, onOpenRestore }: {
  session: Session;
  schedule: Schedule;
  onSchedule: (schedule: Schedule) => void;
  /** Opens the page that restores from a replica: where another server's replica sends the person. */
  onOpenRestore?: () => void;
}) {
  const { t } = useTranslation("serverConsole");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const mounted = useRef(true);
  const replication = schedule.replication;
  // Just turned on, the runner is still "off": followed from there, or the
  // screen stays on "starting" (and without a standby's row) until left.
  const going = replication.enabled && replication.state !== "moved";

  useEffect(() => {
    mounted.current = true;
    return () => { mounted.current = false; };
  }, []);

  // The state shown follows the replica while it goes: one of the person's
  // tabs in sight asks for all of them, the backup band's among them, and a
  // tab behind asks nothing (followAcrossTabs).
  useEffect(() => {
    if (!going) return undefined;
    const controller = new AbortController();
    const topic = `scheduled-backup:${session.user.id}`;
    const stop = followAcrossTabs(topic, STATUS_MS,
      () => api.scheduledBackup(session, controller.signal).then((result) => tellTabs(topic, result.schedule)),
      (value) => { if (mounted.current && value && typeof value === "object") onSchedule(value as Schedule); });
    return () => {
      controller.abort();
      stop();
    };
  }, [going, session, onSchedule]);

  async function run(action: () => Promise<{ schedule: Schedule }>) {
    setBusy(true);
    setError("");
    try {
      const result = await action();
      if (!mounted.current) return;
      onSchedule(result.schedule);
      announceScheduledBackupSaved();
    } catch (cause) {
      if (mounted.current) setError(replicationMessage(cause, t));
    } finally {
      if (mounted.current) setBusy(false);
    }
  }

  if (replication.state === "moved") {
    return (
      <div className={styles.emailProviderBlock} data-replication data-replication-moved>
        <p className={styles.emailProviderMeta} role="alert">
          {replication.movedTo ? t("replication.movedTo", { address: replication.movedTo }) : t("replication.moved")}
        </p>
        <div className={styles.toggleRow}>
          <button type="button" className={styles.secondaryButton} disabled={busy}
            onClick={() => void run(() => api.keepServerApart(session))} data-replication-apart>
            {t("replication.keepApart")}
          </button>
        </div>
        <p className={styles.emailProviderMeta}>{t("replication.keepApartHelp")}</p>
        {error ? <div className={dataStyles.live} data-error="true" role="alert">{error}</div> : null}
      </div>
    );
  }

  return (
    <div className={styles.settingRows} data-replication>
      <SettingSwitch title={t("replication.toggle")} on={replication.enabled}
        description={t(replication.setByEnvironment ? "replication.managed" : "replication.help")}
        disabled={busy || replication.setByEnvironment || !schedule.enabled}
        onChange={(enabled) => void run(() => api.saveReplication(session, enabled))} control={{ "data-replication-toggle": "" }} />
      {replication.enabled ? <ReplicationState schedule={schedule} /> : null}
      {error ? <p className={styles.errorText} data-error="true" role="alert">{error}</p> : null}
      {/* Another server's replica is restored from on its own page (2026-11-06: the words said "below", where nothing restores). */}
      {onOpenRestore && (error === t("replication.ownedElsewhere")
        || (replication.state === "failing" && replication.errorCode === administratorErrorCodes.replicaOwnedElsewhere)) ? (
        <div className={styles.toggleRow}>
          <button type="button" className={styles.secondaryButton} onClick={onOpenRestore} data-replication-open-restore>{t("schedule.openRestore")}</button>
        </div>
      ) : null}
    </div>
  );
}

function ReplicationState({ schedule }: { schedule: Schedule }) {
  const { t } = useTranslation("serverConsole");
  const replication = schedule.replication;
  let text: string;
  if (replication.state === "failing") {
    text = replicationFailureText(replication.errorCode, schedule.target, t);
  } else if (replication.state === "starting" || replication.state === "off") {
    text = t("replication.starting");
  } else if (replication.behindSeconds > 0) {
    text = t("replication.behindSeconds", { count: replication.behindSeconds });
  } else if (replication.lastSyncAt) {
    const time = new Date(replication.lastSyncAt).toLocaleTimeString(activeDateLocale());
    text = t("replication.upToDate", { time });
  } else {
    text = t("replication.starting");
  }
  return (
    <div className={dataStyles.summary} data-replication-state={replication.state}>
      <div className={dataStyles.summaryRow}>
        <span>{t("replication.state")}</span>
        <strong role={replication.state === "failing" ? "alert" : undefined}>{text}</strong>
      </div>
    </div>
  );
}

type Translate = (key: string, options?: Record<string, unknown>) => string;

function replicationMessage(error: unknown, t: Translate) {
  if (error instanceof LeafenApiError) {
    if (error.code === administratorErrorCodes.replicaOwnedElsewhere) return t("replication.ownedElsewhere");
    if (error.code === administratorErrorCodes.scheduledBackupFolderUnavailable) return t("replication.unreachable");
    if (error.code === administratorErrorCodes.scheduledBackupSetByEnvironment) return t("replication.managed");
  }
  return productErrorMessage(error, "administratorScheduledBackup.save");
}
