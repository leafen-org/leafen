import { useEffect, useId, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { api } from "../../api";
import {
  type AdministratorBackupProgress as Progress,
  type AdministratorReplica as Replica,
  type AdministratorScheduledBackup as Schedule,
  type AdministratorScheduledBackupSnapshot as Snapshot,
} from "../../administratorWire.generated";
import { LeafenApiError } from "../../apiTransport";
import dialogStyles from "../WorkspaceCreateDialog.module.css";
import { styles as settingsStyles } from "../account/accountSettingsStyles";
import { displayDateTime } from "@/lib/displayDate";
import { formatFileSize } from "../database/files";
import { useOverlayLayer } from "../../lib/overlayLayer";
import { canRestartPackagedServer, restartPackagedServer } from "../../lib/packagedServerRestart";
import { openRestoredAfresh } from "@/lib/restoredPlace";
import { serverPath } from "@/lib/serverOrigin";
import { focusWhenFree } from "@/lib/focusThroughWaits";
import { dataEpochFor, noteRestoreChosenHere, readServerDataEpoch } from "@/lib/dataEpoch";
import type { Session } from "../../types";
import styles from "./AdministratorSettingsPage.module.css";
import dataStyles from "./AdministratorDataManagement.module.css";
import { BackupOpener } from "./BackupProtection";
import { ReadFailed } from "../ReadFailed";
import { BackupFolderRestoreCommand, type RestoreWay } from "./BackupRestoreCommand";
import { BackupProgressLine } from "./BackupProgress";
import { listMessage, restoreMessage } from "./backupRestoreMessages";
import { backupFailureDetails } from "./scheduledBackupFailure";

// After the server starts itself again to restore, the screen waits for it
// to answer, then opens the restored server afresh.
const RESTART_POLL_MS = 1000;
const RESTART_LIMIT_MS = 5 * 60 * 1000;
// While a backup is put together, its window looks at how far it has got this often.
const PROGRESS_POLL_MS = 1500;

type Ask = { kind: "restore"; snapshot: Snapshot } | { kind: "undo" } | { kind: "replica"; label: string };

/**
 * Restoring the server from one of its folder backups (user decision
 * 2026-10-03): the backup replaces what the server holds now, and what it
 * held stays aside so the restore can be undone for a week or until the next
 * backup. The server starts itself again where it can; an app's own server,
 * or one on Windows, restores when it is next opened.
 */
export function AdministratorBackupRestore({
  session,
  schedule,
  onSchedule,
}: {
  session: Session;
  schedule: Schedule;
  onSchedule: (schedule: Schedule) => void;
}) {
  const { t } = useTranslation("serverConsole");
  const [snapshots, setSnapshots] = useState<Snapshot[] | null>(null);
  const [replica, setReplica] = useState<Replica | null>(null);
  const [ask, setAsk] = useState<Ask | null>(null);
  const [busy, setBusy] = useState(false);
  const [restarting, setRestarting] = useState(false);
  const [error, setError] = useState("");
  // A stopped restore's reason in the place's own words, under why it stopped.
  const [errorReason, setErrorReason] = useState("");
  // A failed read of the list, said where it shows with a way to read again.
  const [readFailed, setReadFailed] = useState("");
  // Opening the backups with their code or password reads the list again.
  const [opened, setOpened] = useState(0);
  const mounted = useRef(true);
  // Where the server runs, for the command that restores without this screen.
  const [way, setWay] = useState<RestoreWay | null>(null);
  // A confirmation from here that leaves a restore waiting for the app to
  // start its server again gives the focus to that next step, "지금 다시 시작":
  // the backup's button it came from stays locked by it (2026-11-21: the
  // focus went to another section's code field, and a phone's keyboard came
  // up). Elsewhere it stays near the locked button (focusThroughWaits.ts).
  const nextStepRef = useRef<HTMLDivElement>(null);
  const [stepOwed, setStepOwed] = useState(false);
  useEffect(() => {
    if (!stepOwed) return;
    // Once drawn: the schedule the answer gave may come in a later turn.
    const step = nextStepRef.current?.querySelector<HTMLElement>("[data-backup-restore-restart-now]:not(:disabled)");
    if (!step) return;
    setStepOwed(false);
    const active = document.activeElement;
    if (!active || active === document.body || active.matches(":disabled")) focusWhenFree(step);
  }, [stepOwed, schedule.restorePending, busy]);
  // A restore is put together apart from its request (backup_restore_background.go):
  // one that answered with its progress, or one going as this opens, is followed
  // until it is together — then the restart or the next step, as an answer
  // brings — or stops, with why (2026-11-22: a large one was cut off when the
  // screen gave up on its answer after 30 seconds).
  const [following, setFollowing] = useState(false);
  const followingRef = useRef(following);
  followingRef.current = following;
  const followed = following || schedule.progress?.kind === "restore";
  const working = busy || followed;
  // Asked here, it shows from the start, while the request still waits.
  const watching = followed || (busy && ask !== null && ask.kind !== "undo");
  const onScheduleRef = useRef(onSchedule);
  onScheduleRef.current = onSchedule;
  useEffect(() => {
    if (!watching) return undefined;
    const controller = new AbortController();
    let timer = 0;
    // A restore seen going, or one its answer handed over, is over once it isn't.
    let seen = false;
    const look = () => {
      api.scheduledBackup(session, controller.signal).then((result) => {
        if (!mounted.current) return;
        onScheduleRef.current(result.schedule);
        if (result.schedule.progress?.kind === "restore" || !(seen || followingRef.current)) {
          seen = seen || result.schedule.progress?.kind === "restore";
          timer = window.setTimeout(look, PROGRESS_POLL_MS);
          return;
        }
        setFollowing(false);
        if (result.schedule.restorePending) {
          settle(result.schedule);
        } else if (result.schedule.lastRestore?.errorCode) {
          setError(restoreMessage(new LeafenApiError(result.schedule.lastRestore.errorCode, 422, "", ""), result.schedule.target, t));
          const last = result.schedule.lastRestore;
          setErrorReason(backupFailureDetails(last.errorReason, last.errorCause, last.errorCode, t));
        }
      }, () => {
        if (!controller.signal.aborted) timer = window.setTimeout(look, PROGRESS_POLL_MS);
      });
    };
    timer = window.setTimeout(look, PROGRESS_POLL_MS);
    return () => {
      controller.abort();
      window.clearTimeout(timer);
    };
    // The run is followed from when it is seen going.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [watching, session]);
  const readWay = () => {
    api.currentAdministratorBackup(session)
      .then((result) => { if (mounted.current) setWay({ restoreRuns: result.restoreRuns, restoreProgram: result.restoreProgram }); }, () => {});
  };

  // The list is read again when backups go elsewhere, or one more is made.
  const place = schedule.target === "webdav" ? schedule.webdavUrl
    : schedule.target === "s3" ? `${schedule.s3Endpoint}/${schedule.s3Bucket}/${schedule.s3Prefix}` : schedule.folder;
  useEffect(() => {
    mounted.current = true;
    if (!place) {
      setSnapshots([]);
      return () => { mounted.current = false; };
    }
    const controller = new AbortController();
    // A list read again replaces what the last read said, failure included.
    setReadFailed("");
    api.scheduledBackupSnapshots(session, controller.signal)
      .then((list) => setSnapshots(list.snapshots))
      .catch((cause) => {
        if (!controller.signal.aborted) setReadFailed(listMessage(cause, schedule.target, t));
      });
    return () => {
      mounted.current = false;
      controller.abort();
    };
  }, [session, place, schedule.lastSuccessAt, opened]);

  // The place's continuous replica, this server's or another's, as it starts,
  // stops and goes on: it is one of the things to restore.
  useEffect(() => {
    if (!place) {
      setReplica(null);
      return undefined;
    }
    const controller = new AbortController();
    api.backupReplica(session, controller.signal)
      .then((result) => setReplica(result.replica.present ? result.replica : null), () => setReplica(null));
    return () => controller.abort();
  }, [session, place, schedule.replication.enabled, schedule.replication.state, schedule.replication.lastSyncAt, opened]);

  // The server starts again: wait until it answers, then open it afresh.
  useEffect(() => {
    if (!restarting) return undefined;
    const deadline = Date.now() + RESTART_LIMIT_MS;
    let timer = 0;
    let wentAway = false;
    // A server that starts again in place may never be seen away (2026-11-05:
    // the screen stayed on "다시 시작하는 중"): its data's new epoch tells it too.
    const before = dataEpochFor(session.user.id);
    const look = () => {
      // At the server's address: an app window's own page answers every path (2026-11-23).
      fetch(serverPath("/api/leafen/v1/health"), { cache: "no-store" }).then(async (response) => {
        const epoch = response.ok ? (await response.json().catch(() => null) as { dataEpoch?: unknown } | null)?.dataEpoch : null;
        // Past the wait, an answering server is opened afresh whatever it is: the page then says how it stands.
        if (response.ok && (wentAway || Date.now() >= deadline || (before && typeof epoch === "string" && epoch && epoch !== before))) {
          // This window had it restored: it opens on the new data (lib/dataEpoch.ts),
          // where it was if that is still there (lib/restoredPlace.ts).
          noteRestoreChosenHere(session.user.id, typeof epoch === "string" ? epoch : null);
          void openRestoredAfresh(session);
          return;
        }
        if (!response.ok) wentAway = true;
        if (Date.now() < deadline) timer = window.setTimeout(look, RESTART_POLL_MS);
      }).catch(() => {
        wentAway = true;
        if (Date.now() < deadline) timer = window.setTimeout(look, RESTART_POLL_MS);
      });
    };
    timer = window.setTimeout(look, RESTART_POLL_MS);
    return () => window.clearTimeout(timer);
  }, [restarting, session.user.id]);

  async function run(action: () => Promise<{ schedule: Schedule }>) {
    setBusy(true);
    setError("");
    setErrorReason("");
    try {
      const result = await action();
      if (!mounted.current) return;
      onSchedule(result.schedule);
      // Still being put together: its window stays with its progress.
      if (result.schedule.progress?.kind === "restore") {
        setFollowing(true);
        return;
      }
      settle(result.schedule);
    } catch (cause) {
      if (mounted.current) {
        setError(restoreMessage(cause, schedule.target, t));
        // What the place or the system said stays with the restore that stopped.
        if (ask && ask.kind !== "undo") {
          api.scheduledBackup(session).then((result) => {
            const last = result.schedule.lastRestore;
            if (mounted.current) setErrorReason(last ? backupFailureDetails(last.errorReason, last.errorCause, last.errorCode, t) : "");
          }, () => {});
        }
      }
    } finally {
      if (mounted.current) setBusy(false);
    }
  }

  // A restore together waits for the next start: the server starts itself
  // again, or the next step is in sight.
  function settle(next: Schedule) {
    if (ask) setStepOwed(Boolean(next.restorePending) && next.restoreRestart !== "automatic" && canRestartPackagedServer());
    setAsk(null);
    if (next.restorePending && next.restoreRestart === "automatic") setRestarting(true);
  }

  async function restartNow() {
    setBusy(true);
    setError("");
    try {
      await restartPackagedServer();
      // This window had it restored: it opens on the new data (lib/dataEpoch.ts),
      // where it was if that is still there (lib/restoredPlace.ts).
      noteRestoreChosenHere(session.user.id, await readServerDataEpoch().catch(() => null));
      await openRestoredAfresh(session);
    } catch {
      if (mounted.current) {
        setError(t("restore.restartFailed"));
        setBusy(false);
      }
    }
  }

  const pending = schedule.restorePending;
  const manual = schedule.restoreRestart !== "automatic";
  return (
    <section className={styles.section} aria-labelledby="administrator-restore-title" data-backup-restore>
      <div id="administrator-restore-title" className={styles.sectionTitle}>{t("restore.title")}</div>
      <p className={`${styles.settingNote} ${dataStyles.lead}`}>{t("restore.meta")}</p>
      {/* One going while its window is closed, as this opens. */}
      {followed && !ask && schedule.progress ? <BackupProgressLine progress={schedule.progress} /> : null}
      {restarting || (pending && !manual) ? (
        <p className={dataStyles.live} role="status" data-backup-restore-restarting>{t("restore.restarting")}</p>
      ) : pending ? (
        <div className={styles.emailProviderBlock} data-backup-restore-pending>
          <p className={styles.emailProviderMeta} role="status">
            {pending === "undo" ? t("restore.undoWaiting") : t("restore.waiting", { name: snapshotTime(schedule.restorePendingSnapshot, t) })}
          </p>
          <div ref={nextStepRef} className={dataStyles.actions}>
            {/* The app starts its own server again on the spot, which finishes the restore. */}
            {manual && canRestartPackagedServer() ? (
              <button type="button" className={styles.primaryButton} disabled={busy} data-backup-restore-restart-now
                onClick={() => void restartNow()}>
                {t("restore.restartNow")}
              </button>
            ) : null}
            <button type="button" className={styles.secondaryButton} disabled={busy}
              onClick={() => void run(() => api.cancelScheduledBackupRestore(session))}>
              {t("restore.takeBack")}
            </button>
          </div>
        </div>
      ) : schedule.restoredAt ? (
        <div className={styles.emailProviderBlock} data-backup-restore-done>
          <p className={styles.emailProviderMeta}>
            {t("restore.restored", { time: formatTime(schedule.restoredAt), name: snapshotTime(schedule.restoredSnapshot, t) })}
          </p>
          {schedule.restoreUndoUntil ? (
            <div className={dataStyles.actions}>
              <button type="button" className={styles.secondaryButton} disabled={busy} onClick={() => setAsk({ kind: "undo" })}
                data-backup-restore-undo>
                {t("restore.undo", { time: formatTime(schedule.restoreUndoUntil) })}
              </button>
            </div>
          ) : null}
        </div>
      ) : null}
      {/* A restore the last start couldn't finish: it moved nothing, or put it back. */}
      {!pending && !restarting && schedule.restoreFailed ? (
        <p className={styles.emailProviderMeta} role="status" data-backup-restore-failed>
          {schedule.restoreFailed === "undo"
            ? t("restore.undoFailed", { time: formatTime(schedule.restoreFailedAt) })
            : t("restore.failed", { time: formatTime(schedule.restoreFailedAt), name: snapshotTime(schedule.restoreFailedSnapshot, t) })}
        </p>
      ) : null}
      {replica ? (
        <ul className={dataStyles.summary} data-backup-restore-replica={replica.owner}>
          <li className={dataStyles.summaryRow}>
            <span>
              {replicaLabel(replica, t)}
              {replica.readable ? "" : ` · ${t("restore.unreadable")}`}
            </span>
            <button type="button" className={styles.secondaryButton}
              disabled={working || Boolean(pending) || restarting || !replica.readable}
              onClick={() => setAsk({ kind: "replica", label: replicaLabel(replica, t) })} data-backup-restore-replica-choose>
              {t("restore.choose")}
            </button>
          </li>
        </ul>
      ) : null}
      {snapshots === null ? (readFailed ? <ReadFailed message={readFailed} onRetry={() => { setReadFailed(""); setOpened((count) => count + 1); }} /> : (
        <p className={dataStyles.live} role="status">{t("restore.loading")}</p>)
      ) : snapshots.length === 0 ? (
        <p className={styles.emailProviderMeta}>{t("restore.none")}</p>
      ) : (
        <ul className={dataStyles.summary} data-backup-restore-list>
          {snapshots.map((snapshot) => (
            <li key={snapshot.name} className={dataStyles.summaryRow}>
              <span>
                {backupSnapshotLabel(snapshot, snapshots)}
                {snapshot.readable
                  ? ` · ${formatFileSize(snapshot.bytes)}`
                  : ` · ${t("restore.unreadable")}`}
              </span>
              <button type="button" className={styles.secondaryButton}
                disabled={working || Boolean(pending) || restarting || !snapshot.readable}
                onClick={() => setAsk({ kind: "restore", snapshot })} data-backup-restore-choose={snapshot.name}>
                {t("restore.choose")}
              </button>
            </li>
          ))}
        </ul>
      )}
      {!schedule.setByEnvironment && !pending && ((replica && !replica.readable) || snapshots?.some((snapshot) => !snapshot.readable)) ? (
        <BackupOpener session={session} onSchedule={(next) => { onSchedule(next); setOpened((count) => count + 1); }} />
      ) : null}
      {readFailed && snapshots !== null ? <ReadFailed message={readFailed} onRetry={() => { setReadFailed(""); setOpened((count) => count + 1); }} /> : null}
      {error && snapshots !== null && !ask ? <p className={dataStyles.live} data-error="true" role="alert">{error}</p> : null}
      {error && errorReason && snapshots !== null && !ask ? <p className={dataStyles.live} data-backup-restore-reason>{errorReason}</p> : null}
      {ask ? (
        <RestoreDialog
          ask={ask}
          label={ask.kind === "restore" ? backupSnapshotLabel(ask.snapshot, snapshots ?? []) : ask.kind === "replica" ? ask.label : ""}
          manual={manual}
          busy={working}
          progress={watching && schedule.progress?.kind === "restore" ? schedule.progress : null}
          error={error}
          reason={errorReason}
          onClose={() => { if (!working) { setAsk(null); setError(""); setErrorReason(""); } }}
          onConfirm={() => void run(() => (ask.kind === "undo"
            ? api.undoScheduledBackupRestore(session)
            : ask.kind === "replica" ? api.restoreReplica(session)
              : api.restoreScheduledBackup(session, ask.snapshot.name)))}
        />
      ) : null}
      {/* The command line is folded away: most never need it. Where the
          server runs is read as it is first opened. */}
      <details className={`${styles.settingNote} ${dataStyles.fold}`} data-backup-restore-without-screen
        onToggle={(event) => { if (event.currentTarget.open && !way) readWay(); }}>
        <summary>{t("restore.withoutScreen")}</summary>
        {way?.restoreRuns ? <BackupFolderRestoreCommand way={way} schedule={schedule}
          newest={snapshots?.find((snapshot) => snapshot.readable || snapshot.sealed) ?? null} /> : null}
      </details>
    </section>
  );
}

function RestoreDialog({ ask, label, manual, busy, progress, error, reason, onClose, onConfirm }: {
  ask: Ask;
  /** The chosen backup as the list names it. */
  label: string;
  manual: boolean;
  busy: boolean;
  /** How far the backup being put together has got. */
  progress: Progress | null;
  /** Why it was refused, here where it shows: the list behind the window is out of sight. */
  error: string;
  /** What the place or the system said of it. */
  reason: string;
  onClose: () => void;
  onConfirm: () => void;
}) {
  const { t } = useTranslation(["serverConsole", "common"]);
  const titleId = useId();
  const overlayRef = useRef<HTMLDivElement>(null);
  const dialogRef = useRef<HTMLElement>(null);
  const cancelRef = useRef<HTMLButtonElement>(null);
  // A step that replaces the server's data starts on "cancel".
  const { zIndex } = useOverlayLayer(true, { kind: "alertdialog", ref: dialogRef, modalOwned: () => [overlayRef.current], onEscape: onClose,
    initialFocus: cancelRef });

  const undo = ask.kind === "undo";
  const dialog = (
    <div ref={overlayRef} className={dialogStyles.overlay} style={{ zIndex }}>
      <button type="button" className={dialogStyles.backdrop} onClick={onClose} disabled={busy} tabIndex={-1}
        aria-label={t("common:actions.close")} />
      <section ref={dialogRef} className={dialogStyles.dialog} role="alertdialog" aria-modal="true"
        aria-labelledby={titleId} aria-busy={busy || undefined} tabIndex={-1} data-backup-restore-dialog>
        <header className={dialogStyles.header}>
          <h2 id={titleId}>{undo ? t("restore.undoTitle") : t("restore.confirmTitle")}</h2>
        </header>
        <div className={dialogStyles.body}>
          <p className={dialogStyles.message}>{undo
            ? t("restore.undoBody")
            : ask.kind === "replica" ? t("restore.replicaConfirmBody", { name: label })
              : t("restore.confirmBody", { time: label })}</p>
          <p className={dialogStyles.message}>{!manual ? t("restore.restartAutomatic")
            : canRestartPackagedServer() ? t("restore.restartInApp") : t("restore.restartManual")}</p>
          {progress ? <BackupProgressLine progress={progress} /> : null}
          {error ? <p className={dataStyles.live} data-error="true" role="alert">{error}</p> : null}
          {error && reason ? <p className={dataStyles.live} data-backup-restore-reason>{reason}</p> : null}
          <div className={dialogStyles.actions}>
            <button ref={cancelRef} type="button" className={dialogStyles.cancel} onClick={onClose} disabled={busy}>
              {t("common:actions.cancel")}
            </button>
            <button type="button" className={settingsStyles.dangerButton} onClick={onConfirm} disabled={busy}
              data-backup-restore-confirm>
              {busy ? t("restore.preparing") : undo ? t("restore.undoConfirm") : t("restore.confirm")}
            </button>
          </div>
        </div>
      </section>
    </div>
  );
  return typeof document === "undefined" ? dialog : createPortal(dialog, document.body);
}

function formatTime(value: string) {
  // The screen's own way of writing a moment, as everywhere else (lib/displayDate).
  return displayDateTime(value) || value;
}

// A snapshot reads as the time it was taken, with its name when another
// was taken in the same second (as when a backup starts and "지금 백업" follows).
function backupSnapshotLabel(snapshot: { name: string; createdAt?: string }, all: readonly { name: string; createdAt?: string }[]) {
  if (!snapshot.createdAt) return snapshot.name;
  const time = formatTime(snapshot.createdAt);
  const twin = all.some((other) => other.name !== snapshot.name && other.createdAt && formatTime(other.createdAt) === time);
  return twin ? `${time} (${snapshot.name})` : time;
}

// The continuous replica as the list names it: whose, and its last change.
function replicaLabel(replica: Replica, t: Translate) {
  const time = replica.lastChangeAt ? formatTime(replica.lastChangeAt) : "";
  if (replica.owner === "this") return t("restore.replicaThis", { time });
  return replica.ownerOrigin ? t("restore.replicaOther", { time, address: replica.ownerOrigin }) : t("restore.replicaOtherUnknown", { time });
}

// A snapshot is named by the time it was taken, as 20261004T063601Z; a
// restore from the replica by "replica-" or, as a standby took over,
// "standby-", and the time it was asked for.
function snapshotTime(name: string, t: Translate): string {
  const replica = /^(?:replica|standby)-/u.exec(name);
  if (replica) return t("restore.replicaName", { time: snapshotTime(name.slice(replica[0].length), t) });
  const parts = /^(\d{4})(\d{2})(\d{2})T(\d{2})(\d{2})(\d{2})Z$/.exec(name);
  if (!parts) return name;
  const [, year, month, day, hour, minute, second] = parts.map(Number);
  // Named as the list names it, to the second (2026-11-06: "2026. 11. 6. 오후 2:02:36에
  // 2026년 11월 6일 오후 1:52 백업으로", the row picked read "2026. 11. 6. 오후 1:52:45").
  return formatTime(new Date(Date.UTC(year, month - 1, day, hour, minute, second)).toISOString());
}

type Translate = (key: string, options?: Record<string, unknown>) => string;
