import { contentBoxFileKind } from "@/lib/contentBoxFileKind";
import { holdsLeafenArchive, isHanjiExportFile } from "@/lib/leafenImportFile";
import {
  clearRememberedDirectWorkspaceImport,
  readRememberedDirectWorkspaceImport,
} from "@/lib/directWorkspaceImportMemory";
import {
  productErrorMessage,
  productErrorReceiptPresentation,
} from "@/lib/productErrorPresentation";
import { pageHref } from "@/lib/navigation";
import { waitForDurableJob } from "@/lib/durableJobPolling";
import { useReadGeneration } from "@/lib/useReadGeneration";
import { useEffect, useRef, useState, type ReactNode } from "react";
import { useTranslation } from "react-i18next";

import { api } from "../../api";
import type { Session } from "../../types";
import {
  workspaceImportFormats,
  workspaceImportPhases,
  workspaceImportStatuses,
  type WorkspaceImportJob,
} from "../../workspaceImportWire.generated";
import type { CsvImportPlace } from "./CsvImportPlacePicker";
import { DirectFileBatchImport } from "./DirectFileBatchImport";
import { ImportBatchCards } from "./ImportBatchCards";
import { ImportBrowse } from "./ImportBrowse";
import { ImportFailedWindow, ImportFinishedWindow } from "./ImportFinishedWindow";
import type { ImportDestination } from "./ImportDialog";
import { workspaceImportEndedAs, workspaceImportJobHeading, workspaceImportJobItems } from "./directLeafenArchiveImportModel";
import { UploadProgressBar } from "./UploadProgressBar";
import styles from "./ImportDialog.module.css";

type DirectMarkdownImportPanelProps = {
  /** The page the files go into; without it, the workspace's top level. */
  destination?: ImportDestination | null;
  /** An import finished. */
  onImported?: () => void;
  /** Leaves the import screen for what was made. */
  onNavigate?: (path: string) => void;
  /** A CSV chosen alone opens the CSV window, as Notion's import does (seen 2026-10-01). */
  onCSVFile?: (file: File) => void;
  /** A Leafen archive goes to the Leafen archive import, wherever it was dropped. */
  onLeafenArchive?: (file: File) => void;
  /** The settings' "둘러보기": files are dropped or chosen by kind, beside these other places to import from; its CSV card opens the CSV window. The migrations running show over it. */
  browse: { sources: ReactNode; tools?: ReactNode; going?: ReactNode; onCSV(): void };
  session: Session;
  workspaceId: string;
};

function terminal(status: WorkspaceImportJob["status"]) {
  return status === workspaceImportStatuses.completed
    || status === workspaceImportStatuses.cancelled
    || status === workspaceImportStatuses.failed;
}

function cancellable(job: WorkspaceImportJob) {
  return job.phase !== workspaceImportPhases.publishCleanup
    && (job.status === workspaceImportStatuses.queued || job.status === workspaceImportStatuses.running);
}

function workspaceImportFailure(job: WorkspaceImportJob) {
  if (!job.errorCode) return null;
  return productErrorReceiptPresentation({
    code: job.errorCode,
    reference: job.diagnosticReference,
    status: 500,
  }, "workspaceImport.job");
}

export function DirectMarkdownImportPanel({
  destination = null,
  onImported,
  onNavigate,
  onCSVFile,
  onLeafenArchive,
  browse,
  session,
  workspaceId,
}: DirectMarkdownImportPanelProps) {
  const { t } = useTranslation(["importDialog", "common"]);
  const [batchFiles, setBatchFiles] = useState<File[] | null>(null);
  // Files from a kind's window go where it says and start at once, as Notion's "계속하기".
  const [batchPlace, setBatchPlace] = useState<CsvImportPlace | null>(null);
  // The batch ended and its window closed: it leaves "둘러보기" (ImportBatchProgress).
  const [batchEnded, setBatchEnded] = useState(false);
  const [job, setJob] = useState<WorkspaceImportJob | null>(null);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const beginJobRead = useReadGeneration({
    principalId: session.user.id,
    sessionToken: session.token,
    route: `direct-import:${workspaceId}:${job?.id ?? ""}:${job?.status ?? ""}`,
  });
  const jobFailure = job ? workspaceImportFailure(job) : null;
  const importedRef = useRef(onImported);
  importedRef.current = onImported;
  const parentPageId = destination?.pageId;

  useEffect(() => {
    const remembered = readRememberedDirectWorkspaceImport(session.user.id, workspaceId);
    if (!remembered) return;
    const read = beginJobRead();
    api.workspaceImportJob(session, workspaceId, remembered.jobId, { signal: read.signal })
      .then((next) => {
        if (!read.isCurrent()) return;
        // One that ended while away is in "완료됨" alone, as Notion's; one
        // shown ending isn't shown again (2026-11-04: a finished import came
        // back as if it were the batch a reload had stopped).
        if (terminal(next.status)) clearRememberedDirectWorkspaceImport(session.user.id, workspaceId);
        else if (next.format === workspaceImportFormats.markdown
          || next.format === workspaceImportFormats.csv) {
          setJob(next);
        }
      })
      .catch((caught: unknown) => {
        if (!read.isCurrent()) return;
        clearRememberedDirectWorkspaceImport(session.user.id, workspaceId);
        setError(productErrorMessage(caught, "workspaceImport.status"));
      });
  }, [beginJobRead, session, workspaceId]);

  useEffect(() => {
    if (job?.status === workspaceImportStatuses.completed) importedRef.current?.();
    if (job && terminal(job.status)) clearRememberedDirectWorkspaceImport(session.user.id, workspaceId);
  }, [job?.id, job?.status]);

  useEffect(() => {
    if (!job || terminal(job.status)) return;
    const activeJob = job;
    const read = beginJobRead();
    void waitForDurableJob({
      initial: activeJob,
      isSameJob: (next) => next.id === activeJob.id && next.workspaceId === workspaceId,
      isTerminal: (next) => terminal(next.status),
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
    }).catch((caught: unknown) => {
      if (read.isCurrent()) setError(productErrorMessage(caught, "workspaceImport.status"));
    });
  }, [beginJobRead, job?.id, job?.status, session, workspaceId]);

  function chooseFiles(list: FileList | File[] | null) {
    setBatchPlace(null);
    const chosen = Array.from(list ?? []);
    // A Leafen archive is a Leafen archive import, not a batch of files
    // (2026-11-09: a ".leafen.zip" dropped here listed "가져올 파일 0개").
    if (onLeafenArchive && chosen.length === 1) {
      const [only] = chosen;
      void holdsLeafenArchive(only).then((archive) => archive ? onLeafenArchive(only) : chooseOthers(chosen), () => chooseOthers(chosen));
      return;
    }
    chooseOthers(chosen);
  }

  // What "둘러보기" hands here (ImportBrowse): an export of the old name, a
  // CSV or TSV alone or a Leafen archive; the files of a kind come through
  // its window (onKindFiles), and a file of no kind it takes is refused there.
  function chooseOthers(chosen: File[]) {
    if (chosen.some(isHanjiExportFile)) {
      setJob(null);
      setBatchFiles(null);
      setError(t("importDialog:archive.fromHanji"));
      return;
    }
    if (chosen.length === 1 && onCSVFile && contentBoxFileKind(chosen[0]) === "csv") {
      setBatchFiles(null);
      onCSVFile(chosen[0]);
      return;
    }
    // Anything else goes to the import list, which says what it becomes.
    setJob(null);
    setError("");
    setBatchEnded(false);
    setBatchFiles(chosen.length ? chosen : null);
  }

  async function cancel() {
    if (!job || busy) return;
    setBusy(true);
    setError("");
    try {
      setJob(await api.cancelWorkspaceImport(session, workspaceId, job.id));
    } catch (caught) {
      setError(productErrorMessage(caught, "workspaceImport.cancel"));
    } finally {
      setBusy(false);
    }
  }

  // In "둘러보기" what the files become shows under where they were chosen.
  const batch = batchFiles ? (
    <DirectFileBatchImport
      files={batchFiles}
      parentPageId={batchPlace ? (batchPlace.kind === "page" ? batchPlace.pageId : undefined) : parentPageId}
      teamspaceId={batchPlace?.kind === "teamspace" ? batchPlace.teamspaceId : undefined}
      dataSourceId={batchPlace?.kind === "database" ? batchPlace.dataSourceId : undefined}
      autoStart={batchPlace !== null}
      onImported={onImported}
      onNavigate={onNavigate}
      onEnded={() => setBatchEnded(true)}
      session={session}
      workspaceId={workspaceId}
    />
  ) : null;
  const ended = job ? terminal(job.status) : false;
  // How it ended, by the one rule every import screen says it by.
  const endedAs = job ? workspaceImportEndedAs(job) : null;

  return (
    <section className={styles.panel} data-import-file-panel="browse">
      {batchFiles ? null : <ImportBatchCards session={session} workspaceId={workspaceId} onImported={onImported} onNavigate={onNavigate} />}
      {browse.going}
      <ImportBrowse disabled={busy} onFiles={chooseFiles} sources={browse.sources} tools={browse.tools} onCSV={browse.onCSV}
        afterDrop={batchEnded ? null : batch}
        session={session} workspaceId={workspaceId} initialPlace={destination ? { pageId: destination.pageId, title: destination.title } : null} initialFormat={destination?.format}
        onKindFiles={(files, place) => {
          setJob(null); setError(""); setBatchEnded(false);
          setBatchPlace(place); setBatchFiles(files);
        }} />
      {/* A CSV import the CSV window started shows here until it ends, a
          reload between (readRememberedDirectWorkspaceImport), then says how
          it ended in a window and goes, as a batch does (ImportBatchProgress). */}
      {batchFiles || !job || ended ? null : (
        <div className={styles.jobPanel}>
          <strong>{workspaceImportJobHeading(job)}</strong>
          <UploadProgressBar
            className={styles.progressTrack}
            label={t("importDialog:job.fileProgress")}
            progress={{
              phase: job.status === workspaceImportStatuses.queued ? "preparing" : "uploading",
              percent: (job.processedItems / Math.max(job.totalItems, 1)) * 100,
            }}
          />
          <small>{workspaceImportJobItems(job)}</small>
          {cancellable(job) ? (
            <button type="button" className={styles.secondary} disabled={busy} onClick={() => void cancel()}>{t("common:actions.cancel")}</button>
          ) : null}
        </div>
      )}
      {/* Its kind's mark is all the window takes of a name. */}
      {!batchFiles && job && endedAs === "completed" ? (
        <ImportFinishedWindow names={[job.format === workspaceImportFormats.csv ? "import.csv" : "import.md"]} onClose={() => setJob(null)}
          onOpen={job.rootPageId && onNavigate ? () => onNavigate(pageHref(job.rootPageId!)) : undefined} />
      ) : null}
      {!batchFiles && job && endedAs === "failed" ? (
        <ImportFailedWindow names={[job.format === workspaceImportFormats.csv ? "import.csv" : "import.md"]} onClose={() => setJob(null)}
          message={jobFailure?.message ?? ""} />
      ) : null}
      {/* The background note belongs to an import under way, not to choosing one. */}
      {error || (job && !terminal(job.status)) ? (
        <div className={styles.serverStatus} role="status">
          {error || t("importDialog:wizard.serverRunningHint")}
        </div>
      ) : null}
    </section>
  );
}
