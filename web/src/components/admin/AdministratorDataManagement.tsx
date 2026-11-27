"use client";

import { newId } from "../../lib/ids";
import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { api } from "../../api";
import { administratorErrorCodes, type AdministratorBackupCurrentResult, type AdministratorBackupJob } from "../../administratorWire.generated";
import { waitForDurableJob } from "../../lib/durableJobPolling";
import { displayDateTime } from "@/lib/displayDate";
import { saveStreamedResponse } from "../../lib/nativeArchiveDownload";
import { productErrorMessage, type ProductErrorContext } from "../../lib/productErrorPresentation";
import type { Session } from "../../types";
import { AdministratorScheduledBackup } from "./AdministratorScheduledBackup";
import { ServerMoveSetting } from "./ServerMoveSetting";
import styles from "./AdministratorSettingsPage.module.css";
import { SettingRow } from "./AdministratorSettingRows";
import dataStyles from "./AdministratorDataManagement.module.css";
import { formatFileSize } from "../database/files";
import { useSettledPress } from "@/lib/settledPress";
import { ReadFailed } from "../ReadFailed";
import { BackupRestoreCommand, backupFileName } from "./BackupRestoreCommand";
import { BackupProgressLine } from "./BackupProgress";

type BackupAction = "create" | "retry" | "cancel" | "download" | null;
type BackupActionResult = {
  clearRequestId?: boolean;
  job?: AdministratorBackupJob;
  notice: string;
};

const activeBackup = (job: AdministratorBackupJob) =>
  job.status === "pending" || job.status === "running";

const terminalBackup = (job: AdministratorBackupJob) => !activeBackup(job);

/**
 * The server's data pages (2026-11-05: one 2028px page of eight groups): 백업 is
 * the schedule and a backup file to take away, 예비 서버 the replica and the pair,
 * 복원 및 옮기기 going back to a backup or moving to another server.
 */
export function AdministratorDataManagement({ session, part, onSelectSection }: {
  session: Session;
  part: "data" | "standby" | "restore";
  onSelectSection?: (section: "data" | "standby" | "restore") => void;
}) {
  if (part === "standby") return <AdministratorScheduledBackup session={session} part="standby" onSelectSection={onSelectSection} />;
  if (part === "restore") {
    return (
      <>
        <AdministratorScheduledBackup session={session} part="restore" onSelectSection={onSelectSection} />
        <ServerMoveSetting session={session} />
      </>
    );
  }
  return (
    <>
      <AdministratorScheduledBackup session={session} part="backup" onSelectSection={onSelectSection} />
      <BackupFileSection session={session} />
    </>
  );
}

/** A whole backup made now, to download and keep elsewhere. */
function BackupFileSection({ session }: { session: Session }) {
  const { t } = useTranslation("serverConsole");
  const [job, setJob] = useState<AdministratorBackupJob | null>(null);
  // The newest finished backup whose file is kept, while a later one isn't finished.
  const [finished, setFinished] = useState<AdministratorBackupJob | null>(null);
  const [restoreWay, setRestoreWay] = useState<Pick<AdministratorBackupCurrentResult, "restoreRuns" | "restoreProgram"> | null>(null);
  // One finished here that a new backup takes the place of is that one.
  const shownJob = useRef<AdministratorBackupJob | null>(null);
  useEffect(() => {
    const before = shownJob.current;
    shownJob.current = job;
    if (before?.status === "completed" && job && job.id !== before.id) setFinished(before);
  }, [job]);
  const [loading, setLoading] = useState(true);
  const [action, setAction] = useState<BackupAction>(null);
  const [error, setError] = useState("");
  // The backup's state that could not be read, said with a way to read it again.
  const [statusFailed, setStatusFailed] = useState("");
  const [readTurn, setReadTurn] = useState(0);
  const [notice, setNotice] = useState("");
  // "다운로드" comes where "취소" stood as the file is done.
  const downloadSettled = useSettledPress(job ? `${job.id}:${job.status}` : null);
  const requestIdRef = useRef<string | null>(null);
  const generationRef = useRef(0);
  const readAbortRef = useRef<AbortController | null>(null);
  const actionAbortRef = useRef<AbortController | null>(null);

  useEffect(() => {
    const generation = ++generationRef.current;
    const controller = new AbortController();
    readAbortRef.current?.abort();
    actionAbortRef.current?.abort();
    readAbortRef.current = controller;
    requestIdRef.current = null;
    setAction(null);
    setLoading(true);
    setError("");
    setStatusFailed("");
    api.currentAdministratorBackup(session, controller.signal)
      .then((result) => {
        if (generationRef.current === generation) {
          setJob(result.job);
          setFinished(result.finished ?? null);
          setRestoreWay({ restoreRuns: result.restoreRuns, restoreProgram: result.restoreProgram });
        }
      })
      .catch((cause) => {
        if (generationRef.current === generation && !controller.signal.aborted) {
          setStatusFailed(backupMessage(cause, "administratorBackup.status"));
        }
      })
      .finally(() => {
        if (generationRef.current === generation) setLoading(false);
      });
    return () => controller.abort();
  }, [readTurn, session]);

  useEffect(() => {
    if (!job || !activeBackup(job)) return;
    const observed = job;
    const generation = ++generationRef.current;
    const controller = new AbortController();
    readAbortRef.current?.abort();
    readAbortRef.current = controller;
    void waitForDurableJob({
      initial: observed,
      signal: controller.signal,
      isSameJob: (next) => next.id === observed.id && next.requestId === observed.requestId,
      isTerminal: terminalBackup,
      read: () => api.administratorBackup(session, observed.id, controller.signal).then((result) => result.job),
      onProgress: (next) => {
        if (generationRef.current !== generation) return;
        setJob(next);
        // "접수했습니다" is over once the file is done or stopped: its row says how it went.
        if (terminalBackup(next)) setNotice("");
      },
    }).catch((cause) => {
      if (generationRef.current === generation && !controller.signal.aborted) {
        setError(backupMessage(cause, "administratorBackup.status"));
      }
    });
    return () => controller.abort();
  }, [job, session]);

  useEffect(() => () => {
    generationRef.current += 1;
    readAbortRef.current?.abort();
    actionAbortRef.current?.abort();
  }, []);

  async function createBackup() {
    if (action || (job && activeBackup(job))) return;
    const requestId = requestIdRef.current ?? newId();
    requestIdRef.current = requestId;
    await runAction("create", "administratorBackup.create", async (signal) => {
      try {
        const result = await api.createAdministratorBackup(session, { requestId }, signal);
        return {
          clearRequestId: true,
          job: result.job,
          notice: result.replayed ? t("backup.replayed") : t("backup.requested"),
        };
      } catch (cause) {
        const recovered = await api.currentAdministratorBackup(session, signal).catch(() => null);
        if (recovered?.job?.requestId === requestId) {
          return {
            clearRequestId: true,
            job: recovered.job,
            notice: t("backup.checked"),
          };
        }
        throw cause;
      }
    });
  }

  async function retryBackup() {
    if (!job?.canRetry || action) return;
    await runAction("retry", "administratorBackup.retry", async (signal) => {
      const result = await api.retryAdministratorBackup(session, job.id, signal);
      return { job: result.job, notice: t("backup.restarted") };
    });
  }

  async function cancelBackup() {
    if (!job?.canCancel || action) return;
    await runAction("cancel", "administratorBackup.cancel", async (signal) => {
      const result = await api.cancelAdministratorBackup(session, job.id, signal);
      return { job: result.job, notice: t("backup.canceled") };
    });
  }

  async function downloadBackup(made: AdministratorBackupJob | null = job) {
    if (!made || made.status !== "completed" || action) return;
    await runAction("download", "administratorBackup.download", async (signal) => {
      const response = await api.downloadAdministratorBackup(session, made.id, signal);
      const saved = await saveStreamedResponse(response, backupFileName(made), {
        description: t("common:fileTypes.serverBackup"),
        mimeType: "application/zip",
        extensions: [".zip"],
      });
      return { notice: saved === "saved" ? t("backup.downloaded") : "" };
    });
  }

  async function runAction(
    nextAction: Exclude<BackupAction, null>,
    context: ProductErrorContext,
    operation: (signal: AbortSignal) => Promise<BackupActionResult>,
  ) {
    readAbortRef.current?.abort();
    const generation = ++generationRef.current;
    const controller = new AbortController();
    actionAbortRef.current?.abort();
    actionAbortRef.current = controller;
    setAction(nextAction);
    setError("");
    setNotice("");
    try {
      const result = await operation(controller.signal);
      if (generationRef.current === generation) {
        if (result.job) setJob(result.job);
        if (result.clearRequestId) requestIdRef.current = null;
        setNotice(result.notice);
      }
    } catch (cause) {
      if (generationRef.current === generation && !controller.signal.aborted) {
        setError(backupMessage(cause, context));
      }
    } finally {
      if (generationRef.current === generation) setAction(null);
    }
  }

  return (
    <section className={styles.section} aria-labelledby="administrator-backup-title" data-backup-file>
      <div id="administrator-backup-title" className={styles.sectionTitle}>{t("backup.title")}</div>
      <div className={styles.settingRows}>
        <SettingRow title={t("backup.create")} description={t("backup.meta")}>
          <button type="button" className={styles.secondaryButton} onClick={() => void createBackup()} disabled={Boolean(action) || Boolean(job && activeBackup(job))}>
            {action === "create" ? t("backup.requesting") : t("backup.createAction")}
          </button>
        </SettingRow>
        {loading ? <p className={styles.settingNote} role="status">{t("backup.loading")}</p> : null}
        {job ? (
          <SettingRow title={t("backup.lastFile")}
            description={<>
              {activeBackup(job) ? t(`backup.phase_${job.phase}`, { defaultValue: t(statusKey(job)) }) : t(statusKey(job))}
              {" · "}<time dateTime={job.updatedAt}>{formatDate(job.updatedAt)}</time>
              {job.artifactBytes > 0 ? <> · {formatBytes(job.artifactBytes)}</> : null}
              {job.artifactSha256 ? <><br /><code title="SHA-256">SHA-256 {job.artifactSha256}</code></> : null}
              {/* The reference is for asking about a failure; a finished file carried an earlier attempt's. */}
              {job.diagnosticReference && job.status === "failed" ? <><br />{t("backup.reference")} <code>{job.diagnosticReference}</code></> : null}
            </>}>
            <span className={styles.settingEnd}>
              {job.canRetry ? <button type="button" className={styles.secondaryButton} onClick={() => void retryBackup()} disabled={Boolean(action)}>{t("backup.retry")}</button> : null}
              {job.canCancel ? <button type="button" className={styles.secondaryButton} onClick={() => void cancelBackup()} disabled={Boolean(action)}>{t("backup.cancel")}</button> : null}
              {job.status === "completed" ? <button type="button" className={styles.secondaryButton} onClick={() => { if (downloadSettled()) void downloadBackup(); }} disabled={Boolean(action)}>{action === "download" ? t("backup.downloading") : t("backup.download")}</button> : null}
            </span>
          </SettingRow>
        ) : null}
        {/* Being made, how far its steps have got (2026-11-22: "백업 파일 준비 중" alone however long). */}
        {job && activeBackup(job) && job.progress ? (
          <div data-backup-file-progress={job.progress.step}><BackupProgressLine progress={job.progress} /></div>
        ) : null}
        {/* A file made before stays to download under a later backup cancelled,
            failed or still being made, while the server keeps it. */}
        {job && job.status !== "completed" && finished ? (
          <SettingRow title={t("backup.keptFile")}
            description={<>
              <time dateTime={finished.completedAt ?? finished.updatedAt}>{formatDate(finished.completedAt ?? finished.updatedAt)}</time>
              {finished.artifactBytes > 0 ? <> · {formatBytes(finished.artifactBytes)}</> : null}
              {finished.artifactSha256 ? <><br /><code title="SHA-256">SHA-256 {finished.artifactSha256}</code></> : null}
            </>}>
            <span className={styles.settingEnd}>
              <button type="button" className={styles.secondaryButton} onClick={() => { if (downloadSettled()) void downloadBackup(finished); }}
                disabled={Boolean(action)}>{action === "download" ? t("backup.downloading") : t("backup.download")}</button>
            </span>
          </SettingRow>
        ) : null}
        {job?.status === "failed" ? <p className={styles.errorText} role="alert" data-backup-failed-help>{t(backupFailedKey(job.errorCode))}</p> : null}
        {/* What the system said, as agents read it (backup_failure_reason.go). */}
        {job?.status === "failed" && job.errorReason ? (
          <p className={styles.settingNote} data-backup-failure-reason>{t("schedule.failureReason", { reason: job.errorReason })}</p>
        ) : null}
        {statusFailed ? <ReadFailed message={statusFailed} onRetry={() => { setStatusFailed(""); setReadTurn((turn) => turn + 1); }} /> : null}
        <div className={dataStyles.live} data-error={error ? "true" : undefined} role={error ? "alert" : "status"} aria-live="polite">{error || (job?.status === "failed" ? "" : notice)}</div>
        {/* The command line is folded away: most never need it. */}
        <details className={`${styles.settingNote} ${dataStyles.fold}`}>
          <summary>{t("backup.withoutScreen")}</summary>
          <p>{t("backup.help")}</p>
          {restoreWay?.restoreRuns ? <BackupRestoreCommand way={restoreWay}
            made={job?.status === "completed" ? job : finished} /> : null}
        </details>
      </div>
    </section>
  );
}

function backupMessage(error: unknown, context: ProductErrorContext) {
  return productErrorMessage(error, context);
}

// Why a backup file wasn't made: no room, another backup or restore going, or the rest.
function backupFailedKey(code: string) {
  if (code === administratorErrorCodes.backupNoSpace) return "backup.failedNoSpace";
  if (code === administratorErrorCodes.backupBusy) return "backup.failedBusy";
  return "backup.failedHelp";
}

function statusKey(job: AdministratorBackupJob) {
  return {
    pending: "backup.statusPending",
    running: "backup.statusRunning",
    completed: "backup.statusCompleted",
    failed: "backup.statusFailed",
    canceled: "backup.statusCanceled",
  }[job.status];
}

function formatDate(value: string) {
  // The screen's own way of writing a moment, as everywhere else (lib/displayDate).
  return displayDateTime(value) || value;
}

const formatBytes = formatFileSize;
