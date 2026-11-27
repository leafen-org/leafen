import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { api } from "../../api";
import type { AdministratorScheduledBackup } from "../../administratorWire.generated";
import { parseStored, personValue, storePersonValue } from "../../lib/browserStorage";
import type { Session } from "../../types";
import { X } from "@/icons/leafen";
import { pickedFoldersOnly, serverOnThisComputer } from "../../lib/deviceFolder";
import { followAcrossTabs, tellTabs } from "../../lib/followAcrossTabs";
import { onWindowReturn } from "../../lib/windowReturn";
import { afterStart } from "../../lib/startSettled";
import { replicationFailureText, scheduledBackupFailureText } from "./scheduledBackupFailure";
import styles from "./BackupFailureNotice.module.css";

// From the second failure in a row, as the mail to administrators.
const NOTICE_FAILURES = 2;
const CHECK_EVERY_MS = 10 * 60_000;
// While a failure band is up it is read again often, so it goes within
// seconds of the backup or replica working again (2026-11-07: replication
// went on by itself once its folder was back, but the band kept asking for a
// restart until the next ten-minute read).
const CHECK_SHOWN_EVERY_MS = 10_000;
// The backup settings say when they saved, so the band follows at once.
const SCHEDULE_SAVED_EVENT = "leafen:scheduled-backup-saved";

/** Tells the band the backup schedule changed. */
export function announceScheduledBackupSaved() {
  window.dispatchEvent(new Event(SCHEDULE_SAVED_EVENT));
}

/** Hears the backup schedule saved, as the band does (BackupOffLine). */
export function onScheduledBackupSaved(listener: () => void): () => void {
  window.addEventListener(SCHEDULE_SAVED_EVENT, listener);
  return () => window.removeEventListener(SCHEDULE_SAVED_EVENT, listener);
}

// Which band the person last had, kept on this browser so a page opened
// again draws it from its first paint instead of a moment later, pushing the
// page down (2026-11-15): what it says, not the backup's settings.
const BAND_KEPT = "leafen:backup-band";
type BackupBand = { kind: "off" } | { kind: "replication"; errorCode: string; target: string }
  | { kind: "failing"; count: number; errorCode: string; target: string };

function bandOf(schedule: AdministratorScheduledBackup): BackupBand | null {
  if (schedule.turnOnSuggested) return { kind: "off" };
  // Changes that stopped reaching the continuous replica, past a minute.
  if (schedule.enabled && schedule.replication?.state === "failing") {
    return { kind: "replication", errorCode: schedule.replication.errorCode, target: schedule.target };
  }
  if (!schedule.enabled || schedule.failureCount < NOTICE_FAILURES) return null;
  return { kind: "failing", count: schedule.failureCount, errorCode: schedule.lastErrorCode, target: schedule.target };
}

function keptBand(userId: string): BackupBand | null {
  const kept = parseStored(personValue(BAND_KEPT, { userId })) as BackupBand | null;
  return kept && ["off", "replication", "failing"].includes(kept.kind) ? kept : null;
}

/**
 * The band at the top of the app for a server's administrators about its
 * scheduled backups; nobody else sees it, and both bands open the backup
 * settings. While backups keep failing it says so until one succeeds. While
 * they are off on a server with data worth keeping it suggests turning them
 * on (2026-10-17), once: dismissed, it doesn't come back for any
 * administrator. Until the server answers, the band the person last had
 * stands in; once it is known they don't administer the server, none does.
 */
export function useBackupNotice(session: Session, administrator: boolean, openSettings: (section?: "data" | "standby") => void,
  administratorKnown: boolean) {
  const { t } = useTranslation("serverConsole");
  const [schedule, setSchedule] = useState<AdministratorScheduledBackup | null>(null);
  const [kept, setKept] = useState(() => keptBand(session.user.id));
  // When what is found comes into view is the band's (useSteadyBand): with a
  // page, not under a pointer about to press.
  const failing = Boolean(schedule?.enabled
    && (schedule.replication?.state === "failing" || schedule.failureCount >= NOTICE_FAILURES));
  // One of the person's tabs in sight asks for all of them, which hear its
  // answer, and a tab behind asks nothing (followAcrossTabs; 2026-11-13:
  // every tab, hidden ones too, asked). The first read waits for the page's
  // start to settle (startSettled.ts): asked at once, it went out beside the
  // open page's own reads and shared the server with them (2026-11-13).
  useEffect(() => {
    if (!administrator) {
      setSchedule(null);
      return undefined;
    }
    const controller = new AbortController();
    const topic = `scheduled-backup:${session.user.id}`;
    const read = () => api.scheduledBackup(session, controller.signal).then((result) => tellTabs(topic, result.schedule));
    const check = () => { void read().catch(() => undefined); };
    let stop = () => {};
    afterStart(() => {
      if (controller.signal.aborted) return;
      stop = followAcrossTabs(topic, failing ? CHECK_SHOWN_EVERY_MS : CHECK_EVERY_MS, read, (value) => {
        if (value && typeof value === "object") setSchedule(value as AdministratorScheduledBackup);
      }, { now: true });
    });
    const stopReturn = onWindowReturn(check);
    window.addEventListener(SCHEDULE_SAVED_EVENT, check);
    return () => {
      controller.abort();
      stop();
      stopReturn();
      window.removeEventListener(SCHEDULE_SAVED_EVENT, check);
    };
  }, [administrator, failing, session]);

  // What the server said is kept for the next opening; not administering it, the person keeps none.
  const read = administrator && schedule ? bandOf(schedule) : null;
  const settled = Boolean(administrator && schedule) || (administratorKnown && !administrator);
  const readText = read ? JSON.stringify(read) : null;
  useEffect(() => {
    if (!settled) return;
    storePersonValue(BAND_KEPT, readText, { userId: session.user.id });
    setKept(null);
  }, [readText, settled, session.user.id]);
  const band = settled ? read : kept;
  if (!band) return null;
  if (band.kind === "off") {
    // Where the data is now: this computer in the app on its own server, this
    // phone in the phone app, or else the server.
    const text = t(pickedFoldersOnly() ? "schedule.offNoticeDevice"
      : serverOnThisComputer() ? "schedule.offNoticeComputer" : "schedule.offNoticeServer");
    const dismiss = () => {
      // The person's other tabs let it go too.
      if (schedule) tellTabs(`scheduled-backup:${session.user.id}`, { ...schedule, turnOnSuggested: false });
      setKept(null);
      storePersonValue(BAND_KEPT, null, { userId: session.user.id });
      void api.dismissScheduledBackupSuggestion(session).catch(() => undefined);
    };
    return (
      <div className={styles.suggestion} role="status" data-backup-off-notice>
        <span className={styles.text} title={text}>{text}</span>
        <button type="button" className={styles.suggestionAction} onClick={() => openSettings("data")}>{t("schedule.offNoticeTurnOn")}</button>
        <button type="button" className={styles.dismiss} onClick={dismiss}
          aria-label={t("schedule.offNoticeDismiss")} title={t("schedule.offNoticeDismiss")}>
          <X size="regular" aria-hidden="true" />
        </button>
      </div>
    );
  }
  if (band.kind === "replication") {
    const text = `${t("replication.notice")} ${replicationFailureText(band.errorCode, band.target, t)}`;
    return (
      <div className={styles.notice} role="status" data-replication-failure-notice>
        <span className={styles.text} title={text}>{text}</span>
        <button type="button" className={styles.action} onClick={() => openSettings("standby")}>{t("schedule.noticeOpen")}</button>
      </div>
    );
  }
  const text = `${t("schedule.notice", { count: band.count })} ${scheduledBackupFailureText(band.errorCode, band.target, t)}`;
  return (
    <div className={styles.notice} role="status" data-backup-failure-notice>
      <span className={styles.text} title={text}>{text}</span>
      <button type="button" className={styles.action} onClick={() => openSettings("data")}>{t("schedule.noticeOpen")}</button>
    </div>
  );
}
