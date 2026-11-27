import { useEffect, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { api } from "@/api";
import { waitForDurableJob } from "@/lib/durableJobPolling";
import { pageHref } from "@/lib/navigation";
import { productErrorMessage } from "@/lib/productErrorPresentation";
import type { Session } from "@/types";
import {
  workspaceImportBatchItemStates,
  workspaceImportStatuses,
  type WorkspaceImportBatchItem,
  type WorkspaceImportJob,
} from "@/workspaceImportWire.generated";
import {
  workspaceImportBatchItemNote, workspaceImportEndedAs, workspaceImportIsCancellable, workspaceImportIsTerminal, workspaceImportJobHeading,
  workspaceImportJobItems,
} from "./directLeafenArchiveImportModel";
import { UploadProgressBar } from "./UploadProgressBar";
import { ImportFailedWindow, ImportFinishedWindow } from "./ImportFinishedWindow";
import styles from "./ImportDialog.module.css";

const ROWS_READ_EVERY_MS = 1_000;
const STILL_OPEN: ReadonlySet<string> = new Set<string>([
  workspaceImportBatchItemStates.pending, workspaceImportBatchItemStates.received, workspaceImportBatchItemStates.importing,
]);

const ROW_LABEL: Record<WorkspaceImportBatchItem["state"], string> = {
  pending: "pending", received: "pending", importing: "importing", imported: "completed",
  failed: "failed", cancelled: "cancelled", skipped: "skipped",
};

/**
 * A batch the server is carrying out: how far it is and each document as it
 * goes in, then how it ended, in a window. It reads the same however it was
 * opened, from the files just sent or in a window opened after a reload or
 * from another tab.
 */
export function ImportBatchProgress({ initial, session, workspaceId, onFinished, onOpen, onLeave }: {
  initial: WorkspaceImportJob;
  session: Session;
  workspaceId: string;
  /** It ended, here or while away. */
  onFinished?: (job: WorkspaceImportJob) => void;
  /** Ends in the finished window, whose button leaves for what it made. */
  onOpen?: (path: string) => void;
  /** It ended and its window closed, or it had none: it shows nothing more. */
  onLeave?: () => void;
}) {
  const { t } = useTranslation(["importDialog", "common"]);
  const [job, setJob] = useState(initial);
  const [rows, setRows] = useState<WorkspaceImportBatchItem[]>([]);
  const [error, setError] = useState("");
  const [cancelling, setCancelling] = useState(false);
  const [finishedShown, setFinishedShown] = useState(true);
  const known = useRef(new Map<number, WorkspaceImportBatchItem>());
  const finishedRef = useRef(onFinished);
  finishedRef.current = onFinished;
  const leaveRef = useRef(onLeave);
  leaveRef.current = onLeave;

  useEffect(() => {
    const controller = new AbortController();
    const signal = controller.signal;
    let readAt = 0;
    let reading = false;
    const show = () => setRows([...known.current.values()].sort((left, right) => left.position - right.position));
    const firstOpen = () => {
      for (const item of [...known.current.values()].sort((left, right) => left.position - right.position)) {
        if (STILL_OPEN.has(item.state)) return item.position;
      }
      return Number.MAX_SAFE_INTEGER;
    };
    // Reads on from the first document not yet through: what is through stays as it was.
    const readRows = async (all: boolean) => {
      let after = known.current.size === 0 ? -1 : firstOpen() - 1;
      if (after >= Number.MAX_SAFE_INTEGER - 1) return;
      for (;;) {
        const list = await api.workspaceImportBatchItems(session, workspaceId, initial.id, { after, signal });
        for (const item of list.items) known.current.set(item.position, item);
        show();
        if (!all || !list.hasMore || list.items.length === 0) return;
        after = list.items[list.items.length - 1].position;
      }
    };
    const refresh = () => {
      if (reading || Date.now() - readAt < ROWS_READ_EVERY_MS) return;
      reading = true;
      readAt = Date.now();
      void readRows(false).catch(() => undefined).finally(() => { reading = false; });
    };
    void (async () => {
      await readRows(true);
      const finished = await waitForDurableJob<WorkspaceImportJob>({
        initial,
        isSameJob: (next) => next.id === initial.id,
        isTerminal: (next) => workspaceImportIsTerminal(next.status),
        onProgress: (next) => {
          setJob(next);
          refresh();
        },
        read: () => api.workspaceImportJob(session, workspaceId, initial.id, { signal }),
        signal,
      });
      await readRows(true);
      if (!signal.aborted) finishedRef.current?.(finished);
    })().catch((caught: unknown) => {
      if (!signal.aborted) setError(productErrorMessage(caught, "workspaceImport.status"));
    });
    return () => controller.abort();
  }, [initial, session, workspaceId]);

  async function cancel() {
    setCancelling(true);
    try {
      setJob(await api.cancelWorkspaceImport(session, workspaceId, job.id));
    } catch {
      // It ended first; the next read says how.
    } finally {
      setCancelling(false);
    }
  }

  const noteOf = workspaceImportBatchItemNote;
  const list = useMemo(() => rows.map((row) => {
    const note = noteOf(row);
    return (
      <li key={row.position} data-batch-status={ROW_LABEL[row.state]} title={row.details}>
        <span>{row.name}</span>
        <span>
          {t(`importDialog:batch.${ROW_LABEL[row.state]}`)}
          {note ? ` · ${note}` : ""}
          {row.state === workspaceImportBatchItemStates.imported && row.pageId ? (
            <> · <a href={pageHref(row.pageId)}>{t("importDialog:batch.open")}</a></>
          ) : null}
        </span>
      </li>
    );
  }), [rows, t]);

  const ended = workspaceImportIsTerminal(job.status);
  // An import that ended with nothing in has not completed anything: it
  // failed, here as in "완료됨" (workspaceImportEndedAs).
  const nothingIn = workspaceImportEndedAs(job) === "failed" && job.processedItems === 0 && job.totalItems > 0;
  // An import that ended with something in says so in its window, whether
  // every file came in or some did not, as Notion's does, its log page
  // naming what failed (2026-11-05: a broken file chosen with a good one ended
  // in the list alone). The window opens what it made.
  const someIn = ended && job.status !== workspaceImportStatuses.cancelled && job.processedItems > 0;
  const opens = job.rootPageId || rows.find((row) => row.state === workspaceImportBatchItemStates.imported && row.pageId)?.pageId || "";
  // An import that ended leaves "둘러보기", as Notion's: its end is said in
  // its window, and once that closes it is in "완료됨" alone (2026-11-21: a
  // finished import's card stayed under the place files are dropped until
  // the settings opened again). One that was cancelled just goes.
  const failedSays = nothingIn && (rows.length > 0 || Boolean(error));
  const gone = ended && (!finishedShown || !(someIn || failedSays));
  useEffect(() => { if (gone) leaveRef.current?.(); }, [gone]);
  if (ended) {
    if (gone) return null;
    return someIn ? (
      <ImportFinishedWindow names={rows.map((row) => row.name)} onClose={() => setFinishedShown(false)}
        onOpen={opens && onOpen ? () => onOpen(pageHref(opens)) : undefined} />
    ) : (
      <ImportFailedWindow names={rows.map((row) => row.name)} onClose={() => setFinishedShown(false)}
        message={rows.length ? noteOf(rows.find((row) => noteOf(row)) ?? rows[0]!) : error} />
    );
  }
  return (
    <div className={styles.jobPanel} data-import-batch={job.status}>
      <strong>{job.title ? `${job.title} · ` : ""}{workspaceImportJobHeading(job)}</strong>
      <UploadProgressBar
        className={styles.progressTrack}
        label={t("importDialog:job.fileProgress")}
        progress={{
          phase: job.status === workspaceImportStatuses.queued ? "preparing" : "uploading",
          percent: (job.processedItems / Math.max(job.totalItems, 1)) * 100,
        }}
      />
      <small data-import-batch-count>{workspaceImportJobItems(job)}</small>
      <ul className={styles.batchList}>{list}</ul>
      {error ? <p role="alert">{error}</p> : null}
      <div className={styles.serverStatus} role="status">{t("importDialog:wizard.serverRunningHint")}</div>
      {workspaceImportIsCancellable(job) ? (
        <button type="button" className={styles.secondary} disabled={cancelling} onClick={() => void cancel()}>{t("common:actions.cancel")}</button>
      ) : null}
    </div>
  );
}
