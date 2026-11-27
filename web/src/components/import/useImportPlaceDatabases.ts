import { useEffect, useState } from "react";
import { api } from "@/api";
import type { Session } from "@/types";
import type { WorkspaceCSVMergeTargetSummary } from "@/workspaceImportWire.generated";

/** Where a place list reads the databases a file can become a row of. */
export type ImportPlaceDatabaseSource = { session: Session; workspaceId: string };

type Databases = readonly WorkspaceCSVMergeTargetSummary[];

/**
 * The databases a file can go into as a row, as Notion's file windows list
 * them under "데이터베이스" (seen 2026-11-15 in the Text & Markdown, HTML,
 * PDF, Word and Zip windows, not the CSV one): those the reader may add rows
 * to, inline ones too, most recently changed first, by the words searched.
 * A search none of them answers keeps the list as it was before the search,
 * as Notion's ("abc def" and "HQ", which named no database, 2026-11-19);
 * `unsearched` says the list shown is that one. Without a source there are
 * none; while a search is read the last list stays, so the group doesn't
 * blink.
 */
export function useImportPlaceDatabases(source: ImportPlaceDatabaseSource | undefined, query: string):
  { databases: Databases; unsearched: boolean } {
  const [first, setFirst] = useState<Databases>([]);
  const [answer, setAnswer] = useState<Databases>([]);
  const session = source?.session, workspaceId = source?.workspaceId;
  const trimmed = query.trim();
  useEffect(() => {
    if (!session || !workspaceId) return;
    const read = new AbortController();
    api.csvMergeTargets(session, workspaceId, { query: "", signal: read.signal })
      .then((response) => { if (!read.signal.aborted) setFirst(response.items); })
      .catch(() => undefined);
    return () => read.abort();
  }, [session, workspaceId]);
  useEffect(() => {
    if (!session || !workspaceId || !trimmed) return;
    const read = new AbortController();
    // A search once the typing pauses.
    const timer = window.setTimeout(() => {
      api.csvMergeTargets(session, workspaceId, { query: trimmed, signal: read.signal })
        .then((response) => { if (!read.signal.aborted) setAnswer(response.items); })
        .catch(() => undefined);
    }, 150);
    return () => {
      window.clearTimeout(timer);
      read.abort();
    };
  }, [trimmed, session, workspaceId]);
  if (!source) return { databases: [], unsearched: true };
  return trimmed && answer.length ? { databases: answer, unsearched: false } : { databases: first, unsearched: true };
}
