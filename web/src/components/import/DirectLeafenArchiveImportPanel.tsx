import { Upload } from "@/icons/leafen";
import { useAttemptNotice } from "@/lib/useNoticeFor";
import {
  clearRememberedDirectWorkspaceImport,
  readRememberedDirectWorkspaceImport,
  rememberDirectWorkspaceImport,
} from "@/lib/directWorkspaceImportMemory";
import { readLeafenImportFile } from "@/lib/leafenImportFile";
import { newId } from "@/lib/ids";
import {
  localWorkspaceArchiveImport,
  type LocalWorkspaceArchiveProgress,
} from "@/lib/localWorkspaceArchiveImport";
import {
  liveLeafenArchiveTransferService,
  type LiveLeafenArchiveTransferProgress,
} from "@/lib/liveLeafenArchiveTransferService";
import { pageHref } from "@/lib/navigation";
import { productErrorMessage } from "@/lib/productErrorPresentation";
import { useReadGeneration } from "@/lib/useReadGeneration";
import { waitForWorkspaceImport } from "@/lib/waitForWorkspaceImport";
import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";

import { api } from "../../api";
import type { Session } from "../../types";
import {
  workspaceImportBounds,
  workspaceImportFormats,
  workspaceImportErrorCodes,
  workspaceImportStatuses,
  type WorkspaceImportJob,
} from "../../workspaceImportWire.generated";
import { LeafenArchiveNeedsEmptyCard } from "./LeafenArchiveNeedsEmptyCard";
import { UploadProgressBar } from "./UploadProgressBar";
import { useProductPages } from "../product/productPagesContext";
import archiveStyles from "./DirectLeafenArchiveImportPanel.module.css";
import {
  LEAFEN_ARCHIVE_ACCEPT,
  LEGACY_JSON_READ_BOUNDS,
  workspaceImportFailure,
  workspaceImportIsCancellable,
  workspaceImportIsTerminal,
  workspaceImportJobHeading,
  workspaceImportJobItems,
} from "./directLeafenArchiveImportModel";
import styles from "./ImportDialog.module.css";
import { useImportOperationGeneration } from "./useImportOperationGeneration";
import { serverOrigin } from "@/lib/serverOrigin";
import { typedLeafenOrigin } from "@/lib/typedServerAddress";

type DirectLeafenArchiveImportPanelProps = {
  createWorkspaceArchiveImport: typeof api.createWorkspaceArchiveImport;
  session: Session;
  workspaceId: string;
  /** A Leafen archive dropped on another way in, which this takes as chosen. */
  initialFile?: File | null;
  /** The workspace was made for that archive: it goes in as soon as it is read. */
  importAtOnce?: boolean;
  /** Starts a workspace from a Leafen file, the one dropped if any: a bundle goes only into an empty workspace. */
  onNewWorkspace?: (file: File | null) => void;
};

export function DirectLeafenArchiveImportPanel({
  createWorkspaceArchiveImport,
  session,
  workspaceId,
  initialFile = null,
  importAtOnce = false,
  onNewWorkspace,
}: DirectLeafenArchiveImportPanelProps) {
  const { t } = useTranslation(["importDialog", "common"]);
  // A bundle goes only into a workspace with no pages, which one that has
  // pages says before anything is chosen (LeafenArchiveNeedsEmptyCard).
  const productPages = useProductPages();
  const occupied = (productPages?.pages ?? []).some((page) => page.workspaceId === workspaceId);
  const inputRef = useRef<HTMLInputElement>(null);
  const requestId = useRef(newId());
  const [initialLiveTransfer] = useState(() => liveLeafenArchiveTransferService.read(session.user.id, workspaceId));
  const [mode, setMode] = useState<"file" | "live">(
    initialLiveTransfer ? "live" : "file",
  );
  const [file, setFile] = useState<File | null>(null);
  const [job, setJob] = useState<WorkspaceImportJob | null>(null);
  const [busy, setBusy] = useState(false);
  const [sourceUrl, setSourceUrl] = useState(
    initialLiveTransfer?.sourceUrl
      ?? (typeof window === "undefined" ? "" : serverOrigin()),
  );
  const [sourceWorkspaceId, setSourceWorkspaceId] = useState(
    initialLiveTransfer?.sourceWorkspaceId ?? "",
  );
  const [sourceToken, setSourceToken] = useState("");
  const [error, setError] = useAttemptNotice<string>(mode, file && [file.name, file.size, file.lastModified], sourceUrl, sourceWorkspaceId, sourceToken);
  const [liveProgress, setLiveProgress] = useState<LiveLeafenArchiveTransferProgress | null>(null);
  const [localProgress, setLocalProgress] = useState<LocalWorkspaceArchiveProgress | null>(null);
  const beginJobRead = useReadGeneration({
    principalId: session.user.id,
    sessionToken: session.token,
    route: `leafen-archive-import:${workspaceId}:${job?.id ?? ""}:${job?.status ?? ""}`,
  });
  const operationGeneration = useImportOperationGeneration({
    principalId: session.user.id,
    sessionToken: session.token,
    workspaceId,
  });
  const jobFailure = job ? workspaceImportFailure(job) : null;

  useEffect(() => {
    const remembered = readRememberedDirectWorkspaceImport(session.user.id, workspaceId);
    const jobId = remembered?.jobId ?? initialLiveTransfer?.targetJobId;
    if (!jobId) return;
    const read = beginJobRead();
    api.workspaceImportJob(session, workspaceId, jobId, { signal: read.signal })
      .then((next) => {
        if (read.isCurrent() && next.format === workspaceImportFormats.nativeArchiveV2) {
          setJob(next);
        }
      })
      .catch((caught: unknown) => {
        if (!read.isCurrent()) return;
        if (remembered) clearRememberedDirectWorkspaceImport(session.user.id, workspaceId);
        setError(productErrorMessage(caught, "workspaceImport.status"));
      });
  }, [beginJobRead, initialLiveTransfer?.targetJobId, session, workspaceId]);

  // A restore shown ending, or one that ended while away and shown now, isn't
  // shown again next time (as the file import window, 2026-11-04).
  useEffect(() => {
    if (job && workspaceImportIsTerminal(job.status) && !initialLiveTransfer) clearRememberedDirectWorkspaceImport(session.user.id, workspaceId);
  }, [job?.id, job?.status]);

  useEffect(() => {
    if (!job || workspaceImportIsTerminal(job.status)) return;
    const activeJob = job;
    const read = beginJobRead();
    void waitForWorkspaceImport({
      initial: activeJob,
      onProgress: (next) => {
        if (read.isCurrent()) setJob(next);
      },
      read: () => api.workspaceImportJob(
        session,
        workspaceId,
        activeJob.id,
        { signal: read.signal },
      ),
      signal: read.signal,
      workspaceId,
    }).catch((caught: unknown) => {
      if (read.isCurrent()) setError(productErrorMessage(caught, "workspaceImport.status"));
    });
  }, [beginJobRead, job?.id, job?.status, session, workspaceId]);

  // A Leafen archive dropped on the general way in comes chosen, at once in the workspace made for it,
  // as its card said; a new import after it is spent (chooseAgain), and the card's new workspace asks.
  const takenInitial = useRef<File | null>(null);
  const [spentInitial, setSpentInitial] = useState<File | null>(null);
  useEffect(() => {
    if (!initialFile || takenInitial.current === initialFile) return;
    takenInitial.current = initialFile;
    void chooseFile(initialFile, importAtOnce && !occupied);
  });

  async function chooseFile(next: File | null, atOnce = false) {
    if (!next || busy || (job && !workspaceImportIsTerminal(job.status))) return;
    const operation = operationGeneration.begin();
    requestId.current = newId();
    setFile(null);
    setJob(null);
    setError("");
    setLocalProgress(null);
    try {
      const selected = await readLeafenImportFile(next, LEGACY_JSON_READ_BOUNDS);
      if (!operation.isCurrent()) return;
      if (selected.kind === "json") {
        setError(t("importDialog:archive.chooseZip"));
        return;
      }
      const chosen = selected.kind === "archive" ? selected.archive.source : selected.file;
      setFile(chosen);
      if (atOnce) void apply(chosen);
    } catch (caught) {
      if (!operation.isCurrent()) return;
      // A file that is not Leafen's says which file to choose.
      const code = caught instanceof Error && "code" in caught ? caught.code : "";
      setError(code === "LEAFEN_IMPORT_FILE_FROM_HANJI" ? t("importDialog:archive.fromHanji")
        : code === "LEAFEN_IMPORT_FILE_INVALID" ? t("importDialog:archive.chooseZip") : t("importDialog:batch.unreadable"));
    }
  }

  // A bundle goes only into a workspace with no pages, and says so.
  function startFailureMessage(caught: unknown) {
    return caught instanceof Error && "code" in caught && caught.code === workspaceImportErrorCodes.targetNotFresh
      ? t("importDialog:archive.targetNotFresh") : productErrorMessage(caught, "workspaceImport.start");
  }

  async function apply(chosen = file) {
    if (!chosen || busy) return;
    const actionRead = beginJobRead();
    const operation = operationGeneration.begin({ isContextCurrent: actionRead.isCurrent });
    setBusy(true);
    setError("");
    try {
      const next = chosen.size <= workspaceImportBounds.archiveDirectBytes
        ? await createWorkspaceArchiveImport(session, workspaceId, requestId.current, chosen)
        : await localWorkspaceArchiveImport.run({
          file: chosen,
          requestId: requestId.current,
          session,
          workspaceId,
          onProgress: (progress) => {
            if (operation.isCurrent()) setLocalProgress(progress);
          },
        });
      if (operation.isCurrent()) {
        setJob(next);
        rememberDirectWorkspaceImport(session.user.id, workspaceId, next.id);
      }
    } catch (caught) {
      // A refusal ends the upload's progress line too, as a batch's rows end failed.
      if (operation.isCurrent()) { setLocalProgress(null); setError(startFailureMessage(caught)); }
    } finally {
      if (operation.isCurrent()) {
        operation.release();
        setBusy(false);
      }
    }
  }

  async function applyLive() {
    if (!sourceWorkspaceId.trim() || busy) return;
    const controller = new AbortController();
    const actionRead = beginJobRead();
    const operation = operationGeneration.begin({
      controller,
      isContextCurrent: actionRead.isCurrent,
    });
    setBusy(true);
    setError("");
    setLiveProgress(null);
    setLocalProgress(null);
    try {
      const next = await liveLeafenArchiveTransferService.run({
        session,
        targetWorkspaceId: workspaceId,
        // Read as every server address field reads one: no scheme needed.
        sourceUrl: sourceUrl.trim() ? await typedLeafenOrigin(sourceUrl) : sourceUrl,
        sourceWorkspaceId: sourceWorkspaceId.trim(),
        sourceToken,
        signal: controller.signal,
        onProgress: (progress) => {
          if (operation.isCurrent()) {
            setLiveProgress((current) => ({
              ...current,
              ...progress,
            }));
          }
        },
      });
      if (operation.isCurrent()) {
        setJob(next);
        setSourceToken("");
        rememberDirectWorkspaceImport(session.user.id, workspaceId, next.id);
      }
    } catch (caught) {
      if (operation.isCurrent()) { setLiveProgress(null); setError(startFailureMessage(caught)); }
    } finally {
      if (operation.isCurrent()) {
        operation.release();
        setBusy(false);
      }
    }
  }

  async function cancelLive() {
    const actionRead = beginJobRead();
    const operation = operationGeneration.supersede({
      isContextCurrent: actionRead.isCurrent,
    });
    setBusy(true);
    setError("");
    try {
      await liveLeafenArchiveTransferService.cancel({ session, targetWorkspaceId: workspaceId });
      if (operation.isCurrent()) setLiveProgress(null);
    } catch (caught) {
      if (operation.isCurrent()) setError(productErrorMessage(caught, "workspaceImport.cancel"));
    } finally {
      if (operation.isCurrent()) {
        operation.release();
        setBusy(false);
      }
    }
  }

  async function cancel() {
    if (!job || busy) return;
    const actionRead = beginJobRead();
    const operation = operationGeneration.begin({ isContextCurrent: actionRead.isCurrent });
    setBusy(true);
    setError("");
    try {
      const next = await api.cancelWorkspaceImport(session, workspaceId, job.id);
      if (operation.isCurrent()) setJob(next);
    } catch (caught) {
      if (operation.isCurrent()) setError(productErrorMessage(caught, "workspaceImport.cancel"));
    } finally {
      if (operation.isCurrent()) {
        operation.release();
        setBusy(false);
      }
    }
  }

  function chooseAgain() {
    setSpentInitial(initialFile);
    operationGeneration.supersede();
    beginJobRead();
    requestId.current = newId();
    setFile(null);
    setJob(null);
    setError("");
    setLiveProgress(null);
    setSourceToken("");
    clearRememberedDirectWorkspaceImport(session.user.id, workspaceId);
    liveLeafenArchiveTransferService.clear(session.user.id, workspaceId);
  }

  const liveProgressLabel = liveProgress
    ? [
        liveProgress.stage === "source-export" ? t("importDialog:archive.stage.sourceExport")
          : liveProgress.stage === "source-download" ? t("importDialog:archive.stage.sourceDownload")
            : liveProgress.stage === "target-upload" ? t("importDialog:archive.stage.targetUpload")
              : t("importDialog:archive.stage.targetApply"),
        liveProgress.receivedBytes !== undefined && liveProgress.archiveBytes
          ? `${liveProgress.receivedBytes} / ${liveProgress.archiveBytes} bytes`
          : "",
      ].filter(Boolean).join(" · ")
    : "";

  if (occupied && !job) return <LeafenArchiveNeedsEmptyCard file={initialFile === spentInitial ? null : initialFile} onNewWorkspace={onNewWorkspace} />;

  return (
    <section className={styles.panel}>
      <div className={archiveStyles.destBanner}>
        <span className={archiveStyles.destRoute}>
          <img src="/mark-128.png" alt="" width="20" height="20" />
          <strong>{t("importDialog:leafen.title")}</strong>
        </span>
        <span className={archiveStyles.destNote}>{t("importDialog:archive.destination")}</span>
      </div>

      <div className={archiveStyles.modeGroup} role="radiogroup" aria-label={t("importDialog:archive.modeLabel")}>
        <button
          type="button"
          className={archiveStyles.modeOption}
          data-active={mode === "file" ? "true" : undefined}
          role="radio"
          aria-checked={mode === "file"}
          disabled={busy || Boolean(job && !workspaceImportIsTerminal(job.status))}
          onClick={() => setMode("file")}
        >
          <strong>{t("importDialog:leafen.fromFile")}</strong>
          <span>{t("importDialog:archive.fromFileHint")}</span>
        </button>
        <button
          type="button"
          className={archiveStyles.modeOption}
          data-active={mode === "live" ? "true" : undefined}
          role="radio"
          aria-checked={mode === "live"}
          disabled={busy || Boolean(job && !workspaceImportIsTerminal(job.status))}
          onClick={() => setMode("live")}
        >
          <strong>{t("importDialog:leafen.fromLive")}</strong>
          <span>{t("importDialog:archive.fromLiveHint")}</span>
        </button>
      </div>

      {!job && mode === "file" ? (
        <div className={archiveStyles.dropzone} data-busy={busy ? "true" : undefined}>
          <span className={archiveStyles.dropIcon} aria-hidden="true"><Upload size="wide" /></span>
          <strong>{file ? file.name : t("importDialog:leafen.choose")}</strong>
          {localProgress ? <span className={archiveStyles.jobHint}>
            {t(`importDialog:archive.localStage.${localProgress.stage === "hashing" ? "hashing" : localProgress.stage === "uploading" ? "uploading" : "accepting"}`)}
            {localProgress.archiveBytes ? ` ${Math.round(localProgress.receivedBytes / localProgress.archiveBytes * 100)}%` : ""}
          </span> : null}
          <span className={archiveStyles.dropActions}>
            {file ? <button type="button" className={styles.primary} disabled={busy} onClick={() => void apply()}>
              {busy ? t("importDialog:leafen.importing") : t("importDialog:leafen.importButton")}
            </button> : null}
            <button
              type="button"
              className={file ? styles.secondary : styles.primary}
              onClick={() => inputRef.current?.click()}
              disabled={busy}
            >
              {t("importDialog:chooseFileButton")}
            </button>
          </span>
        </div>
      ) : null}

      {!job && mode === "live" ? (
        <div className={archiveStyles.liveForm}>
          <label className={styles.field}>
            <span>{t("importDialog:leafen.remoteUrl")}</span>
            <input
              type="url"
              value={sourceUrl}
              disabled={busy}
              placeholder="https://leafen.example.com"
              onChange={(event) => setSourceUrl(event.currentTarget.value)}
            />
          </label>
          <label className={styles.field}>
            <span>{t("importDialog:leafen.remoteWorkspace")}</span>
            <input
              type="text"
              value={sourceWorkspaceId}
              disabled={busy}
              autoCapitalize="none"
              spellCheck={false}
              onChange={(event) => setSourceWorkspaceId(event.currentTarget.value)}
            />
          </label>
          <label className={styles.field}>
            <span>{t("importDialog:leafen.remoteToken")}</span>
            <input
              type="password"
              value={sourceToken}
              disabled={busy}
              autoComplete="off"
              autoCapitalize="none"
              spellCheck={false}
              className={styles.maskedTokenInput}
              onChange={(event) => setSourceToken(event.currentTarget.value)}
            />
          </label>
          <div className={archiveStyles.liveActions}>
            <span>{liveProgressLabel || (initialLiveTransfer
              ? t("importDialog:archive.resumeHint")
              : t("importDialog:archive.liveHint"))}</span>
            {busy ? (
              <button type="button" className={styles.secondary} onClick={() => void cancelLive()}>
                {t("importDialog:cancelImport")}
              </button>
            ) : null}
            <button
              type="button"
              className={styles.primary}
              disabled={busy || !sourceWorkspaceId.trim()}
              onClick={() => void applyLive()}
            >
              {busy ? t("importDialog:leafen.fetching")
                : initialLiveTransfer ? t("importDialog:resumeImport")
                  : t("importDialog:leafen.fetch")}
            </button>
          </div>
        </div>
      ) : null}

      {job ? (
        <div
          className={archiveStyles.jobCard}
          role="status"
          aria-label={workspaceImportJobHeading(job)}
          data-job-id={job.id}
          data-job-status={job.status}
          data-job-phase={job.phase}
        >
          <strong>{workspaceImportJobHeading(job)}</strong>
          <span className={archiveStyles.jobHint}>
            {workspaceImportJobItems(job)}
          </span>
          {workspaceImportIsTerminal(job.status) ? null : <UploadProgressBar
            className={styles.progressTrack}
            label={t("importDialog:archive.progress")}
            progress={{
              phase: job.status === workspaceImportStatuses.queued ? "preparing" : "uploading",
              percent: (job.processedItems / Math.max(job.totalItems, 1)) * 100,
            }}
          />}
          {jobFailure ? (
            <div className={archiveStyles.leafenJobFailure} role="alert">
              <strong>{jobFailure.cause} {jobFailure.nextAction}</strong>
              {/* What the server found, in its words, is for whoever made the
                  file: told as that, under what happened and what to do; the
                  reference under it only when no one foresaw the failure. */}
              {job?.errorDetail ? <code data-import-error-detail>{t("importDialog:archive.errorDetail", { detail: job.errorDetail })}</code> : null}
              {jobFailure.referenceShown ? <code>{t("importDialog:archive.diagnosticId", { id: jobFailure.reference })}</code> : null}
            </div>
          ) : null}
          <span className={archiveStyles.jobActions}>
            {workspaceImportIsCancellable(job) ? (
              <button type="button" className={styles.secondary} disabled={busy} onClick={() => void cancel()}>
                {t("importDialog:cancelImport")}
              </button>
            ) : null}
            {workspaceImportIsTerminal(job.status) ? (
              <button type="button" className={styles.secondary} disabled={busy} onClick={chooseAgain}>
                {t("importDialog:leafen.chooseSourceAgain")}
              </button>
            ) : null}
            {job.status === workspaceImportStatuses.completed && job.rootPageIds[0] ? (
              <a className={styles.primary} href={pageHref(job.rootPageIds[0])}>{t("importDialog:migration.open")}</a>
            ) : null}
          </span>
        </div>
      ) : null}

      <input
        ref={inputRef}
        type="file"
        accept={LEAFEN_ARCHIVE_ACCEPT}
        className={archiveStyles.hiddenInput}
        onChange={(event) => {
          const next = event.currentTarget.files?.[0] ?? null;
          event.currentTarget.value = "";
          void chooseFile(next);
        }}
        disabled={busy || Boolean(job && !workspaceImportIsTerminal(job.status))}
      />
      {/* It goes on without the window only while it is going, in the words the other imports use. */}
      {error || (job && !workspaceImportIsTerminal(job.status)) ? <div className={styles.serverStatus} role="status">
        {error || t("importDialog:migration.closeAnytime")}
      </div> : null}
    </section>
  );
}
