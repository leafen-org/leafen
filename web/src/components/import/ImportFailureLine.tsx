import { useEffect, useState } from "react";
import { api } from "@/api";
import type { Session } from "@/types";
import {
  workspaceImportBatchItemStates,
  workspaceImportFormats,
  type WorkspaceImportJob,
} from "@/workspaceImportWire.generated";
import { workspaceImportBatchItemNote, workspaceImportFailure } from "./directLeafenArchiveImportModel";
import historyStyles from "./ImportHistory.module.css";

/**
 * Why a finished import in "완료됨" failed and what to do, under its status,
 * as its window said it: the import's own refusal, or for a batch whose files
 * all failed on their own, the first of their reasons, as its end window
 * names it (2026-11-21: a broken Word file alone said only its count here).
 * Nothing when no reason is known.
 */
export function ImportFailureLine({ job, session, workspaceId }: { job: WorkspaceImportJob; session: Session; workspaceId: string }) {
  const own = workspaceImportFailure(job)?.message ?? "";
  const [itemNote, setItemNote] = useState("");
  const readsItems = !own && job.format === workspaceImportFormats.fileBatch;
  useEffect(() => {
    if (!readsItems) return;
    const controller = new AbortController();
    api.workspaceImportBatchItems(session, workspaceId, job.id, { signal: controller.signal })
      .then((list) => {
        const failed = list.items.find((item) => item.state === workspaceImportBatchItemStates.failed && workspaceImportBatchItemNote(item));
        setItemNote(failed ? workspaceImportBatchItemNote(failed) : "");
      })
      // Its status says it failed; the reason is what is left out.
      .catch(() => undefined);
    return () => controller.abort();
  }, [job.id, readsItems, session, workspaceId]);
  const message = own || itemNote;
  if (!message) return null;
  return <span className={historyStyles.details}><small data-import-history-failure={job.errorCode || "files"}>{message}</small></span>;
}
