import { i18next } from "@/i18n";
import { productErrorReceiptPresentation } from "@/lib/productErrorPresentation";
import {
  workspaceImportFormats,
  workspaceImportPhases,
  workspaceImportStatuses,
  type WorkspaceImportBatchItem,
  type WorkspaceImportJob,
} from "@/workspaceImportWire.generated";

export const LEAFEN_ARCHIVE_ACCEPT = ".leafen.zip,application/vnd.leafen.archive+zip,application/zip";
export const LEGACY_JSON_READ_BOUNDS = {
  maxJsonBytes: 4 * 1024 * 1024,
  parser: {
    maxTextCharacters: 4 * 1024 * 1024,
  },
} as const;

export function workspaceImportIsTerminal(status: WorkspaceImportJob["status"]) {
  return status === workspaceImportStatuses.completed
    || status === workspaceImportStatuses.cancelled
    || status === workspaceImportStatuses.failed;
}

export function workspaceImportIsCancellable(job: WorkspaceImportJob) {
  return job.phase !== workspaceImportPhases.publishCleanup
    && (job.status === workspaceImportStatuses.queued
      || job.status === workspaceImportStatuses.running);
}

/** A job's count of items, or that it is getting ready while it has not
 * counted them yet, or nothing for one that ended before it counted any:
 * "0 / 0 items" said nothing true of a job just begun or refused. A
 * Leafen bundle says its pages, as the export that made it said them, and
 * its bar alone the steps of putting them in (2026-11-13: "112 / 112 항목"
 * for a bundle exported as "항목 81개"). */
export function workspaceImportJobItems(job: WorkspaceImportJob) {
  if (job.pageCount) return i18next.t("importDialog:job.pages", { count: job.pageCount });
  if (job.totalItems <= 0) return workspaceImportIsTerminal(job.status) ? "" : i18next.t("importDialog:preparingImport");
  return i18next.t("importDialog:job.items", { processed: job.processedItems, total: job.totalItems });
}

export function workspaceImportJobHeading(job: WorkspaceImportJob) {
  const heading = (key: string) => i18next.t(`importDialog:job.${key}`);
  if (job.status === workspaceImportStatuses.completed) return heading("completed");
  if (job.status === workspaceImportStatuses.cancelled) return heading("cancelled");
  if (job.status === workspaceImportStatuses.failed) return heading("failed");
  if (job.status === workspaceImportStatuses.cancelling) return heading("cancelling");
  if (
    job.phase === workspaceImportPhases.cleanup
    || job.phase === workspaceImportPhases.publishCleanup
  ) return heading("cleaningUp");
  return heading("running");
}

/**
 * How an import that ended ended, as every screen says it: a batch of files
 * the server ran to its end with none of them in failed, as its window says
 * ("가져오기에 실패했습니다") and as Notion's "완료됨" shows such an import
 * (2026-11-21: a broken Word file alone read "완료 · 0 / 1 항목" there).
 * Null while it goes on.
 */
export function workspaceImportEndedAs(job: WorkspaceImportJob): "completed" | "failed" | "cancelled" | null {
  if (!workspaceImportIsTerminal(job.status)) return null;
  if (job.status === workspaceImportStatuses.completed && job.format === workspaceImportFormats.fileBatch
    && job.totalItems > 0 && job.processedItems === 0) return "failed";
  return job.status === workspaceImportStatuses.completed ? "completed"
    : job.status === workspaceImportStatuses.cancelled ? "cancelled" : "failed";
}

/**
 * Why one file of a batch ended unfinished, as the batch's rows say it: the
 * words it was sent with (a file that could not be read), or its refusal's
 * reason; "" when there is none.
 */
export function workspaceImportBatchItemNote(item: Pick<WorkspaceImportBatchItem, "errorCode" | "message">) {
  return item.message || (item.errorCode
    ? productErrorReceiptPresentation({ code: item.errorCode, reference: null, status: 500 }, "workspaceImport.job").cause : "");
}

export function workspaceImportFailure(job: WorkspaceImportJob) {
  if (!job.errorCode) return null;
  return productErrorReceiptPresentation({
    code: job.errorCode,
    reference: job.diagnosticReference,
    status: 500,
  }, "workspaceImport.job");
}
