"use client";

import { useCallback, useEffect, useId, useMemo, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import type { Session } from "@/types";
import {
  type WorkspaceExportFormat,
  type WorkspaceExportJob,
} from "@/workspaceExportWire.generated";
import { durableJobUnreachable } from "@/lib/durableJobPolling";
import { readRememberedWorkspaceExportJob } from "@/lib/workspaceExportJobMemory";
import { downloadWorkspaceExportToBrowser } from "@/lib/workspaceExportDownload";
import { workspaceExportService } from "@/lib/workspaceExportService";
import { useReadGeneration } from "@/lib/useReadGeneration";
import { useOverlayLayer } from "@/lib/overlayLayer";
import { useProductToast } from "@/lib/productToast";
import {
  formatWorkspaceExportBytes,
  workspaceExportFailureMessage,
  workspaceExportJobCanRetry,
  workspaceExportJobFailure,
  workspaceExportJobIsActive,
  workspaceExportIsReadable,
  workspaceExportOptions,
} from "./workspaceExportDialogModel";
import { ExportChoiceRows, initialExportChoice, type PageExportChoice, type PageExportFormat } from "./PageExportDialog";
import dialogStyles from "./PageExportDialog.module.css";
import styles from "./WorkspaceExportDialog.module.css";
import { useSettledPress } from "@/lib/settledPress";

function sameWorkspaceExportRead(
  expected: WorkspaceExportJob,
  received: WorkspaceExportJob,
) {
  return received.id === expected.id
    && received.actorId === expected.actorId
    && received.workspaceId === expected.workspaceId
    && received.sourceRevision === expected.sourceRevision;
}

const FORMATS: readonly PageExportFormat[] = ["html", "markdown_csv", "leafen_archive"];
const jobFormatOf = (format: PageExportFormat): WorkspaceExportFormat => format === "html" || format === "markdown_csv" ? format : "native_archive";
const choiceFormatOf = (format: WorkspaceExportFormat): PageExportFormat => format === "html" || format === "markdown_csv" ? format : "leafen_archive";

// While the server doesn't answer, it is asked again this often; a start
// waits for it at most five minutes.
const EXPORT_RECONNECT_MS = 2_000;
const EXPORT_START_RECONNECT_TRIES = 150;

/**
 * Notion's export window for a whole workspace (seen 2026-11-04): the same
 * 320px card and rows as a page's, without subpages, since it is every page,
 * and with its private pages left out unless asked for (seen 2026-11-13).
 * The Leafen archive is ours. The export is a job on the server that goes on
 * if the window closes; while it is open a finished export downloads at once.
 */
export function WorkspaceExportDialog({
  session,
  workspaceId,
  workspaceName,
  onClose,
  onRestoreFocus,
  privatePagesAtFirst = false,
}: {
  session: Session;
  workspaceId: string;
  workspaceName?: string;
  onClose: () => void;
  onRestoreFocus?: () => void;
  /** Opens with "개인 페이지 포함" on: a copy kept before a workspace goes, whose private pages are its own data. */
  privatePagesAtFirst?: boolean;
}) {
  const { t } = useTranslation(["sidebar", "common", "pageExport"]);
  const { notify } = useProductToast();
  const titleId = useId();
  const overlayRef = useRef<HTMLDivElement>(null);
  const dialogRef = useRef<HTMLElement>(null);
  const restoreFocusOverrideRef = useRef(onRestoreFocus);
  restoreFocusOverrideRef.current = onRestoreFocus;
  const [job, setJob] = useState<WorkspaceExportJob | null>(null);
  // The newest finished export of each other format whose file is kept.
  const [kept, setKept] = useState<WorkspaceExportJob[]>([]);
  // One finished here that a new export takes the place of joins them.
  const shownBefore = useRef<WorkspaceExportJob | null>(null);
  useEffect(() => {
    const before = shownBefore.current;
    shownBefore.current = job;
    if (before?.status === "completed" && job && job.id !== before.id) {
      setKept((current) => [before, ...current.filter((candidate) => candidate.format !== before.format)]);
    }
  }, [job]);
  const [choice, setChoice] = useState<PageExportChoice>(() => ({ ...initialExportChoice(FORMATS), includePrivatePages: privatePagesAtFirst }));
  const update = (change: Partial<PageExportChoice>) => setChoice((current) => ({ ...current, ...change }));
  const adoptJobChoices = useCallback((format: WorkspaceExportFormat, options?: Partial<WorkspaceExportJob["options"]>) => {
    setChoice((current) => ({ ...current, format: choiceFormatOf(format),
      ...(workspaceExportIsReadable(format) ? {
        includeFiles: options?.includeFiles !== false, createFolders: options?.createFolders !== false,
        includeComments: options?.includeComments === true, defaultDatabaseViews: options?.defaultDatabaseViews === true,
        includePrivatePages: options?.leavePrivatePagesOut === false,
      } : {}) }));
  }, []);
  // An export this window is waiting on downloads as soon as it is done.
  const downloadWhenDone = useRef(false);
  const cancelledJob = useRef("");
  const [loading, setLoading] = useState(true);
  const [action, setAction] = useState<"start" | "retry" | "cancel" | "download" | null>(null);
  const [error, setError] = useState("");
  // The server doesn't answer for now: what it holds shows again once it does.
  const [reconnecting, setReconnecting] = useState(false);
  const [restoreAttempt, setRestoreAttempt] = useState(0);
  const mountedRef = useRef(true);

  const actorId = session.user.id;
  const beginRestoreRead = useReadGeneration({
    principalId: actorId,
    sessionToken: session.token,
    route: `workspace-export-restore:${workspaceId}`,
  });
  const beginPollRead = useReadGeneration({
    principalId: actorId,
    sessionToken: session.token,
    route: `workspace-export-poll:${workspaceId}:${job?.id ?? ""}:${job?.sourceRevision ?? ""}`,
  });

  // A modal layer: the page stays still and Escape closes it while nothing
  // opened over it takes the key.
  const { zIndex } = useOverlayLayer(true, {
    kind: "dialog", ref: dialogRef, modalOwned: () => [overlayRef.current], onEscape: () => { if (!busy) onClose(); },
    // Focus returns to the opener unless the owner names where it goes.
    initialFocus: "first", shouldRestoreFocus: () => !restoreFocusOverrideRef.current,
  });

  useEffect(() => {
    mountedRef.current = true;
    return () => {
      mountedRef.current = false;
      if (restoreFocusOverrideRef.current) window.requestAnimationFrame(() => restoreFocusOverrideRef.current?.());
    };
  }, []);

  useEffect(() => {
    const restoreRead = beginRestoreRead();
    setJob(null);
    setError("");
    // A page export shares this memory; only a workspace export restores here.
    const rememberedJob = readRememberedWorkspaceExportJob(actorId, workspaceId);
    const remembered = rememberedJob?.scope.kind === "workspace" ? rememberedJob : null;
    if (remembered) adoptJobChoices(remembered.format, remembered.options);
    setLoading(true);
    // The server keeps the person's latest export of the workspace, whatever
    // this browser remembers (2026-10-16: an export the server took while
    // its answer was lost, and finished after a restart, never showed).
    let retry = 0;
    workspaceExportService.current(session, workspaceId)
      .then((result) => {
        const currentJob = result.job;
        if (!restoreRead.isCurrent()) return;
        setReconnecting(false);
        shownBefore.current = currentJob;
        setJob(currentJob);
        setKept(result.finished ?? []);
        if (currentJob?.format) adoptJobChoices(currentJob.format, currentJob.options);
        if (workspaceExportJobIsActive(currentJob)) downloadWhenDone.current = true;
      })
      .catch((cause) => {
        if (!restoreRead.isCurrent()) return;
        if (durableJobUnreachable(cause)) {
          setReconnecting(true);
          retry = window.setTimeout(() => setRestoreAttempt((attempt) => attempt + 1), EXPORT_RECONNECT_MS);
          return;
        }
        setError(workspaceExportFailureMessage(cause, "status"));
      })
      .finally(() => {
        if (restoreRead.isCurrent()) setLoading(false);
      });
    return () => window.clearTimeout(retry);
  }, [actorId, adoptJobChoices, beginRestoreRead, restoreAttempt, session, t, workspaceId]);

  useEffect(() => {
    const activeJob = job;
    if (!activeJob || !workspaceExportJobIsActive(activeJob)) return;
    const pollRead = beginPollRead();
    void workspaceExportService.wait(session, workspaceId, activeJob, {
      signal: pollRead.signal,
      onProgress(next) {
        if (pollRead.isCurrent() && sameWorkspaceExportRead(activeJob, next)) setJob(next);
      },
      onReach(reached) {
        if (pollRead.isCurrent()) setReconnecting(!reached);
      },
    }).then((completed) => {
      if (pollRead.isCurrent() && sameWorkspaceExportRead(activeJob, completed)) setJob(completed);
    }).catch((cause) => {
      // Cancelling ends the wait, also after the cancel has answered; that is
      // not a failure to report.
      if (pollRead.isCurrent() && cancelledJob.current !== activeJob.id) setError(workspaceExportFailureMessage(cause, "poll"));
    });
  }, [beginPollRead, job, session, t, workspaceId]);

  async function startExport() {
    if (action) return;
    setAction("start");
    setError("");
    const format = jobFormatOf(choice.format);
    const input = {
      workspaceId,
      format,
      scope: { kind: "workspace" as const, targetId: "" },
      options: workspaceExportOptions(format, choice),
    };
    downloadWhenDone.current = true;
    try {
      // A start the server may have taken while its answer was lost is
      // asked for again, as the same request, once the server answers.
      for (let attempt = 0; ; attempt += 1) {
        try {
          const started = await workspaceExportService.start(session, input);
          if (!mountedRef.current) return;
          setReconnecting(false);
          setJob(started.job);
          return;
        } catch (cause) {
          if (!durableJobUnreachable(cause) || attempt >= EXPORT_START_RECONNECT_TRIES || !mountedRef.current) throw cause;
          setReconnecting(true);
          await new Promise((resolve) => window.setTimeout(resolve, EXPORT_RECONNECT_MS));
        }
      }
    } catch (cause) {
      if (!mountedRef.current) return;
      setReconnecting(false);
      setError(workspaceExportFailureMessage(cause, "start"));
    } finally {
      if (mountedRef.current) setAction(null);
    }
  }

  async function retryExport() {
    if (!job || action) return;
    setAction("retry");
    setError("");
    downloadWhenDone.current = true;
    try {
      setJob((await workspaceExportService.retry(session, workspaceId, job.id)).job);
    } catch (cause) {
      setError(workspaceExportFailureMessage(cause, "retry"));
    } finally {
      setAction(null);
    }
  }

  async function cancelExport() {
    if (!job || action) return;
    setAction("cancel");
    setError("");
    downloadWhenDone.current = false;
    cancelledJob.current = job.id;
    try {
      setJob((await workspaceExportService.cancel(session, workspaceId, job.id)).job);
    } catch (cause) {
      cancelledJob.current = "";
      setError(workspaceExportFailureMessage(cause, "cancel"));
    } finally {
      setAction(null);
    }
  }

  async function downloadExport(made: WorkspaceExportJob | null = job): Promise<boolean> {
    if (!made || made.status !== "completed" || action) return false;
    setAction("download");
    setError("");
    try {
      // Said downloaded once it is: a save window closed without saving says nothing.
      const saved = await downloadWorkspaceExportToBrowser({
        session,
        workspaceId,
        workspaceName,
        jobId: made.id,
        format: made.format,
      });
      return saved === "saved";
    } catch (cause) {
      setError(workspaceExportFailureMessage(cause, "download"));
      return false;
    } finally {
      setAction(null);
    }
  }

  useEffect(() => {
    if (job?.status !== "completed" || !downloadWhenDone.current) return;
    downloadWhenDone.current = false;
    // What the export leaves out stays said, so the window stays.
    const leftOut = Boolean(job.omittedPageCount || job.omittedFileCount);
    // The window closes saying the file came down, as a page's export does.
    void downloadExport().then((downloaded) => {
      if (!downloaded || !mountedRef.current || leftOut) return;
      notify(t("pageExport:exported"), "success");
      onClose();
    });
    // Runs as the job reaches its end; the download reads that job.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [job?.id, job?.status]);

  const busy = action !== null;
  const active = workspaceExportJobIsActive(job);
  // What comes where "취소" stood as the export ends waits a moment for a press.
  const finishSettled = useSettledPress(active);
  // A finished job belongs to its format: picking another format hides its
  // card and download, so "다운로드" never hands out a different format. That
  // format's file made before, while it is kept, shows instead — under a later
  // export of another format, or one of its own cancelled or failed
  // (2026-11-07: an HTML export's download went once Markdown was exported).
  const latestForChoice = !active && job && jobFormatOf(choice.format) === job.format ? job : null;
  const keptForChoice = kept.find((candidate) => candidate.format === jobFormatOf(choice.format)) ?? null;
  const finishedJob = latestForChoice?.status === "completed" ? latestForChoice : keptForChoice ?? latestForChoice;
  const shown = active ? job : finishedJob;
  const showJob = !loading && shown && (active || shown.status === "failed" || shown.status === "completed");
  const statusLabel = shown
    ? t(`sidebar:workspaceExportStatus.${shown.status}`)
    : "";
  const failure = useMemo(() => workspaceExportJobFailure(shown), [shown]);
  // Its size is said in pages — its pages and database pages, as the card of
  // the import that takes it in says them — not in the parts it is written
  // in (2026-11-13: "항목 77개", then 81, and "112 / 112 항목" on import).
  const progressDetails = shown
    ? [
        shown.pageCount
          ? t("sidebar:workspaceExportPages", { count: shown.pageCount })
          : "",
        formatWorkspaceExportBytes(shown.artifactBytes),
      ].filter(Boolean).join(" · ")
    : "";

  const dialog = (
    <div ref={overlayRef} className={dialogStyles.overlay} style={{ zIndex }}>
      <button
        type="button"
        className={dialogStyles.backdrop}
        onClick={() => {
          if (!busy) onClose();
        }}
        disabled={busy}
        tabIndex={-1}
        aria-label={t("common:actions.close")}
      />
      <section
        ref={dialogRef}
        className={dialogStyles.dialog}
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleId}
        aria-busy={loading || busy || undefined}
        data-workspace-export-dialog
      >
        <h2 id={titleId} className={dialogStyles.title}>{t("sidebar:workspaceExportDialogTitle")}</h2>
        <fieldset className={dialogStyles.body} disabled={busy || loading || active}>
          {active ? null : <ExportChoiceRows choice={choice} update={update} formats={FORMATS} everyPage
            archiveNote={t("sidebar:workspaceExportNativeDescription")} />}
          {showJob ? (
            <section className={styles.job} aria-label={t("sidebar:workspaceExportCurrentJob")} data-workspace-export-job={shown.status}>
              <div className={styles.jobHeading}>
                <strong>{t(`pageExport:formats.${choiceFormatOf(shown.format)}`)}</strong>
                <span data-status={shown.status}>{statusLabel}</span>
              </div>
              {active ? <p>{t(`sidebar:workspaceExportPhase_${shown.phase}`, { defaultValue: statusLabel })}</p> : null}
              {progressDetails ? <p>{progressDetails}</p> : null}
              {shown.omittedPageCount ? (
                <p>{t("sidebar:workspaceExportOmittedPages", { count: shown.omittedPageCount })}</p>
              ) : null}
              {shown.omittedFileCount ? (
                <p data-workspace-export-omitted-files>{t("sidebar:workspaceExportOmittedFiles", { count: shown.omittedFileCount })}</p>
              ) : null}
              {failure ? (
                <div role="alert">
                  <p>{failure.cause} {failure.nextAction}</p>
                  {failure.referenceShown ? <small>{failure.referenceLabel}: {failure.reference}</small> : null}
                </div>
              ) : null}
            </section>
          ) : null}
          {reconnecting ? (
            <p className={dialogStyles.note} role="status" data-workspace-export-reconnecting>
              {t("sidebar:workspaceExportReconnecting")}
            </p>
          ) : null}
          {error ? <p className={dialogStyles.error} role="alert">{error}</p> : null}
        </fieldset>
        <footer className={dialogStyles.footer}>
          <button type="button" className={dialogStyles.secondary} onClick={onClose} disabled={busy}>
            {t(active ? "common:actions.close" : "pageExport:cancel")}
          </button>
          {active ? (
            <button type="button" className={styles.danger} onClick={() => void cancelExport()} disabled={busy}>
              {action === "cancel" ? t("sidebar:workspaceExportCancelling") : t("sidebar:workspaceExportCancel")}
            </button>
          ) : (
            <>
              {finishedJob?.status === "completed" && !loading ? (
                <button type="button" className={dialogStyles.secondary} onClick={() => { if (finishSettled()) void downloadExport(finishedJob); }} disabled={busy}>
                  {action === "download" ? t("sidebar:workspaceExportDownloading") : t("sidebar:workspaceExportDownload")}
                </button>
              ) : null}
              {finishedJob && workspaceExportJobCanRetry(finishedJob) ? (
                <button type="button" className={dialogStyles.secondary} onClick={() => { if (finishSettled()) void retryExport(); }} disabled={busy}>
                  {action === "retry" ? t("sidebar:workspaceExportRetrying") : t("sidebar:workspaceExportRetry")}
                </button>
              ) : null}
              <button type="button" className={dialogStyles.primary} onClick={() => { if (finishSettled()) void startExport(); }} disabled={busy || loading || !actorId}>
                {action === "start" ? t("sidebar:workspaceExportStarting") : t("pageExport:export")}
              </button>
            </>
          )}
        </footer>
      </section>
    </div>
  );

  return typeof document === "undefined" ? dialog : createPortal(dialog, document.body);
}
