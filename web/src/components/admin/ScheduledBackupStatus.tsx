import { useTranslation } from "react-i18next";
import type { AdministratorScheduledBackup as Schedule } from "../../administratorWire.generated";
import { activeNumberLocale } from "../../lib/i18n";
import { displayDateTime } from "@/lib/displayDate";
import { backupFailureDetails, scheduledBackupFailureText } from "./scheduledBackupFailure";
import { BackupProgressLine, backupRunText } from "./BackupProgress";
import { SettingRow } from "./AdministratorSettingRows";
import styles from "./AdministratorSettingsPage.module.css";

/**
 * 백업 상태: the last backup with what it went through and sent, a backup
 * going now as far as it has got (user 2026-11-22: 46 minutes said only
 * "백업하는 중"), the last attempt's failure and where it stopped, the next
 * backup and a backup now, how many are kept, and the way to restoring.
 */
export function ScheduledBackupStatus({ schedule, running, canRun, busy, ranHere, error, message, placeSet, onRunNow, onWriteHere, onOpenRestore }: {
  schedule: Schedule;
  /** A backup asked for here, or one going. */
  running: boolean;
  canRun: boolean;
  busy: boolean;
  /** What "지금 백업" brought says so on its row, in sight of the button. */
  ranHere: boolean;
  error: string | null;
  message: string;
  placeSet: boolean;
  onRunNow: () => void;
  onWriteHere: () => void;
  onOpenRestore?: () => void;
}) {
  const { t } = useTranslation("serverConsole");
  const run = schedule.lastRun;
  const progress = schedule.progress?.kind === "backup" ? schedule.progress : null;
  return (
    <section className={styles.section} aria-labelledby="administrator-schedule-state-title" data-scheduled-backup-status>
      <div id="administrator-schedule-state-title" className={styles.sectionTitle}>{t("schedule.stateTitle")}</div>
      <div className={styles.settingRows}>
        <SettingRow title={t("schedule.lastSuccess")}
          description={run?.step === "done" && schedule.lastSuccessAt ? <span data-scheduled-backup-last-run>{backupRunText(run, t)}</span> : undefined}>
          <span className={styles.settingValue}>{schedule.lastSuccessAt ? <Time value={schedule.lastSuccessAt} /> : t("schedule.never")}</span>
        </SettingRow>
        {schedule.protection.opened ? (
          <SettingRow title={t("schedule.openedTitle")} description={t("schedule.openedMeta")} data-scheduled-backup-opened="">
            <button type="button" className={styles.secondaryButton} onClick={onWriteHere} disabled={busy}>
              {t("schedule.writeHere")}
            </button>
          </SettingRow>
        ) : null}
        {schedule.lastErrorCode ? (
          <div data-scheduled-backup-failed>
            <SettingRow title={t("schedule.lastAttempt")}
              description={<>
                <span className={styles.errorText} role="alert">{scheduledBackupFailureText(schedule.lastErrorCode, schedule.target, t)}</span>
                {/* Where a run that failed stopped, and how far it had got. */}
                {run && run.step !== "done" && !progress ? <><br /><span data-scheduled-backup-last-run>{backupRunText(run, t)}</span></> : null}
                {/* What the place or the system said, as agents read it (backup_failure_reason.go). */}
                {run && (run.errorReason || run.errorCause) && run.step !== "done" ? <><br /><span data-scheduled-backup-failure-reason>
                  {backupFailureDetails(run.errorReason, run.errorCause, schedule.lastErrorCode, t)}</span></> : null}
              </>}><span /></SettingRow>
          </div>
        ) : null}
        {schedule.enabled ? (
          <SettingRow title={t("schedule.next")} description={<>
            {/* Going now, the time it was due is past: the progress below says how it goes. */}
            {schedule.nextRunAt && !running ? <Time value={schedule.nextRunAt} /> : null}
            {ranHere && (error || message) ? <span className={error ? styles.errorText : undefined} role={error ? "alert" : "status"} data-scheduled-backup-ran>
              {running ? "" : " · "}{error || message}</span> : null}
          </>}>
            <button type="button" className={styles.secondaryButton} onClick={onRunNow} disabled={!canRun}
              data-scheduled-backup-run>
              {running ? t("schedule.running") : t("schedule.runNow")}
            </button>
          </SettingRow>
        ) : null}
        {progress ? <div data-scheduled-backup-progress={progress.step}><BackupProgressLine progress={progress} /></div> : null}
        {schedule.snapshots > 0 ? (
          <SettingRow title={t("schedule.kept")}>
            <span className={styles.settingValue}>{schedule.snapshots.toLocaleString(activeNumberLocale())}</span>
          </SettingRow>
        ) : null}
        {placeSet && onOpenRestore ? (
          <SettingRow title={t("restore.title")} description={t("schedule.restoreMeta")}>
            <button type="button" className={styles.secondaryButton} onClick={onOpenRestore}>{t("schedule.openRestore")}</button>
          </SettingRow>
        ) : null}
      </div>
    </section>
  );
}

export function Time({ value }: { value: string }) {
  return <time dateTime={value}>{displayDateTime(value) || value}</time>;
}
