import { useTranslation } from "react-i18next";
import type { AdministratorBackupProgress as Progress } from "../../administratorWire.generated";
import { activeNumberLocale } from "../../lib/i18n";
import { formatFileSize } from "../database/files";
import { UploadProgressBar } from "../import/UploadProgressBar";
import styles from "./BackupProgress.module.css";

type Translate = (key: string, options?: Record<string, unknown>) => string;

// A time left is told once the work has gone on this long, from its pace so far.
const LEFT_AFTER_MS = 15_000;

/**
 * How far a backup, a restore or a backup file has got as it runs (user
 * 2026-11-22: a first backup to S3 ran 46 minutes saying only "백업하는 중"):
 * a bar of the bytes gone through, and a line with the step, the bytes and
 * files of all it has, what went over new and about how long is left, as
 * the import's progress tells it.
 */
export function BackupProgressLine({ progress }: { progress: Progress }) {
  const { t } = useTranslation("serverConsole");
  const label = t(`backupProgress.label_${progress.kind}`);
  const counted = progress.bytesTotal > 0;
  return (
    <div className={styles.progress} data-backup-progress={`${progress.kind}:${progress.step}`}>
      <UploadProgressBar className={styles.track} label={label}
        progress={counted ? { phase: "uploading", percent: (progress.bytesDone / progress.bytesTotal) * 100 } : { phase: "preparing", percent: 0 }} />
      <small className={styles.line} role="status" aria-live="polite">{backupProgressText(progress, t)}</small>
    </div>
  );
}

/** The step it is at, with its counts, in one line. */
export function backupProgressText(progress: Progress, t: Translate) {
  const parts = [backupStepLabel(progress, t)];
  if (progress.bytesTotal > 0) {
    parts.push(t("backupProgress.bytes", { done: formatFileSize(progress.bytesDone), total: formatFileSize(progress.bytesTotal) }));
  } else if (progress.bytesDone > 0) {
    parts.push(formatFileSize(progress.bytesDone));
  }
  if (progress.filesTotal > 0) {
    parts.push(t("backupProgress.files", { done: number(progress.filesDone), total: number(progress.filesTotal) }));
  }
  if (progress.bytesSent > 0) parts.push(t("backupProgress.sent", { size: formatFileSize(progress.bytesSent) }));
  if (progress.filesSkipped > 0) parts.push(t("backupProgress.skipped", { count: progress.filesSkipped }));
  const left = timeLeft(progress, t);
  if (left) parts.push(left);
  return parts.join(" · ");
}

/**
 * The last backup as it ended: all it went through and sent, and how long
 * it took; one that stopped says where.
 */
export function backupRunText(run: Progress, t: Translate) {
  const parts: string[] = [];
  if (run.step !== "done") parts.push(t("backupProgress.stopped", { step: backupStepLabel(run, t) }));
  if (run.bytesTotal > 0) {
    parts.push(run.step === "done" ? formatFileSize(run.bytesTotal)
      : t("backupProgress.bytes", { done: formatFileSize(run.bytesDone), total: formatFileSize(run.bytesTotal) }));
  }
  if (run.filesTotal > 0) {
    parts.push(run.step === "done" ? t("backupProgress.fileCount", { count: run.filesTotal, formatted: number(run.filesTotal) })
      : t("backupProgress.files", { done: number(run.filesDone), total: number(run.filesTotal) }));
  }
  if (run.bytesSent > 0) parts.push(t("backupProgress.sent", { size: formatFileSize(run.bytesSent) }));
  // Files deleted while it went through the data folder, left out of it.
  if (run.filesSkipped > 0) parts.push(t("backupProgress.skipped", { count: run.filesSkipped }));
  // How long a finished one took; one that stopped says where instead.
  const took = Date.parse(run.finishedAt) - Date.parse(run.startedAt);
  if (run.step === "done" && Number.isFinite(took) && took >= 0) {
    const minutes = Math.round(took / 60_000);
    parts.push(minutes < 1 ? t("backupProgress.tookUnderMinute") : t("backupProgress.took", { count: minutes }));
  }
  return parts.join(" · ");
}

function backupStepLabel(progress: Pick<Progress, "kind" | "step">, t: Translate) {
  return t(`backupProgress.step_${progress.kind}_${progress.step}`, { defaultValue: t(`backupProgress.label_${progress.kind}`) });
}

// From the pace so far over all of it; a backup file's steps each count
// their own, so it tells none.
function timeLeft(progress: Progress, t: Translate) {
  if (progress.kind === "file" || progress.bytesTotal <= 0 || progress.bytesDone <= 0) return "";
  const elapsed = Date.now() - Date.parse(progress.startedAt);
  if (!Number.isFinite(elapsed) || elapsed < LEFT_AFTER_MS) return "";
  const left = ((progress.bytesTotal - progress.bytesDone) / progress.bytesDone) * elapsed;
  const minutes = Math.round(left / 60_000);
  return minutes < 1 ? t("backupProgress.almostDone") : t("backupProgress.minutesLeft", { count: minutes });
}

function number(value: number) {
  return value.toLocaleString(activeNumberLocale());
}
