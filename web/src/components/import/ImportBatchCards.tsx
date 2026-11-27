import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { api } from "@/api";
import { LeafenApiError } from "@/apiTransport";
import { finishImportBatch, keptImportBatches, type KeptImportBatch } from "@/lib/importBatchMemory";
import type { Session } from "@/types";
import {
  workspaceImportFormats,
  workspaceImportPhases,
  workspaceImportStatuses,
  type WorkspaceImportJob,
} from "@/workspaceImportWire.generated";
import { workspaceImportIsTerminal } from "./directLeafenArchiveImportModel";
import { ImportBatchProgress } from "./ImportBatchProgress";
import styles from "./ImportDialog.module.css";

type Stopped = {
  key: string;
  names: string[];
  fileCount: number;
  total: number;
  done: number;
  /** The files were still being sent, so nothing was made of them yet. */
  sending: boolean;
};

const allIn = (job: WorkspaceImportJob) => job.status === workspaceImportStatuses.completed && job.processedItems >= job.totalItems;

/**
 * What the import window says when it opens about the batches under way or
 * cut short: the ones the server is carrying out, from this browser or another
 * and whether or not anyone stayed, each ending in its window; and for files
 * a reload, a closed tab or a failure stopped before they were all in, how
 * far they got and how to go on. One that completed while away is in
 * "완료됨" alone, as Notion's.
 */
export function ImportBatchCards({ session, workspaceId, onImported, onNavigate }: {
  session: Session;
  workspaceId: string;
  onImported?: () => void;
  /** Leaves the import screen for what an import made. */
  onNavigate?: (path: string) => void;
}) {
  const { t } = useTranslation("importDialog");
  const [jobs, setJobs] = useState<WorkspaceImportJob[]>([]);
  const [stopped, setStopped] = useState<Stopped[]>([]);

  useEffect(() => {
    const controller = new AbortController();
    const signal = controller.signal;
    const fromMemory = (kept: KeptImportBatch, done: number, sending: boolean): Stopped => ({
      key: kept.key, names: kept.names, fileCount: kept.fileCount, total: kept.total, done, sending,
    });
    void (async () => {
      const listed = await api.workspaceImports(session, workspaceId, { signal }).then((list) => list.jobs, () => [] as WorkspaceImportJob[]);
      const batches = new Map(listed.filter((job) => job.format === workspaceImportFormats.fileBatch).map((job) => [job.id, job]));
      const shown: WorkspaceImportJob[] = [];
      const cut: Stopped[] = [];
      const seen = new Set<string>();
      for (const kept of keptImportBatches(workspaceId)) {
        if (!kept.jobId) {
          cut.push(fromMemory(kept, 0, true));
          continue;
        }
        seen.add(kept.jobId);
        let job = batches.get(kept.jobId);
        if (!job) {
          try {
            job = await api.workspaceImportJob(session, workspaceId, kept.jobId, { signal });
          } catch (caught) {
            if (signal.aborted) return;
            // The server no longer has it; files sent or not, nothing waits.
            if (caught instanceof LeafenApiError && caught.status === 404) finishImportBatch(kept.key);
            else cut.push(fromMemory(kept, 0, true));
            continue;
          }
        }
        if (job.phase === workspaceImportPhases.batchUpload) cut.push(fromMemory(kept, job.processedItems, true));
        else if (!workspaceImportIsTerminal(job.status)) shown.push(job);
        // One that completed is in "완료됨" alone, as Notion's: all in, it is
        // forgotten; else the same files chosen again bring in the rest.
        else if (allIn(job)) finishImportBatch(kept.key);
        else if (job.status !== workspaceImportStatuses.completed) cut.push(fromMemory(kept, job.processedItems, false));
      }
      for (const job of batches.values()) {
        if (!seen.has(job.id) && !workspaceImportIsTerminal(job.status) && job.phase !== workspaceImportPhases.batchUpload) shown.push(job);
      }
      if (signal.aborted) return;
      setJobs(shown);
      setStopped(cut);
    })();
    return () => controller.abort();
  }, [session, workspaceId]);

  return (
    <>
      {stopped.map((batch) => (
        <div key={batch.key} className={styles.serverStatus} role="status" data-import-unfinished>
          {t(batch.sending ? "batch.unfinishedSending" : "batch.unfinished", {
            names: batch.fileCount > batch.names.length
              ? t("batch.moreNames", { names: batch.names.join(", "), count: batch.fileCount - batch.names.length })
              : batch.names.join(", "),
            imported: batch.done, total: batch.total, remaining: batch.total - batch.done,
          })}
        </div>
      ))}
      {jobs.map((job) => (
        <ImportBatchProgress key={job.id} initial={job} session={session} workspaceId={workspaceId} onOpen={onNavigate}
          onFinished={(finished) => {
            if (allIn(finished)) {
              for (const kept of keptImportBatches(workspaceId)) if (kept.jobId === finished.id) finishImportBatch(kept.key);
            }
            if (finished.processedItems > 0) onImported?.();
          }} />
      ))}
    </>
  );
}
