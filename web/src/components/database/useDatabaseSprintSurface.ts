import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { readDatabaseSprints, runDatabaseSprintAction } from "@/databaseToolbarConfigApi";
import type { DatabaseSprintChanges, DatabaseSprintState } from "@/databaseToolbarConfigWire.generated";
import { newId } from "@/lib/ids";
import { pagePermanentDeleteErrorCodes } from "@/pagePermanentDeleteWire.generated";
import type { Session } from "@/types";
import { keepRelativeSprints, markSprintBoardPage, nameCurrentSprintView, type DatabaseSprintOutcome, type DatabaseSprintRequest, type DatabaseSprintSurface } from "./databaseSprintContext";
import { useSettingsReadUnanswered, useSettingsWriteUnanswered } from "./useSettingsWriteUnanswered";

function browserTimeZone() {
  try {
    return Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC";
  } catch {
    return "UTC";
  }
}

// useDatabaseSprintSurface reads a task database's sprints and runs sprint
// actions against the revision it last saw; `onChanged` rereads the page the
// server just changed. Sprints are toolbar settings written straight to the
// server: an action without the server's answer is said as not known and
// read again once it answers, a refused one reads what the server keeps, and
// a read without an answer goes again when it may answer
// (useSettingsWriteUnanswered).
export function useDatabaseSprintSurface({
  session,
  pageId,
  dataSourceId,
  taskDatabase,
  onChanged,
}: {
  session: Session | null;
  pageId: string;
  dataSourceId: string;
  taskDatabase: boolean;
  onChanged?(): void;
}): DatabaseSprintSurface | null {
  const [state, setState] = useState<DatabaseSprintState | null>(null);
  const stateRef = useRef<DatabaseSprintState | null>(null);
  stateRef.current = state;
  const changedRef = useRef(onChanged);
  changedRef.current = onChanged;

  // Read again, the sprints show what they showed until the answer.
  const [readTurn, setReadTurn] = useState(0);
  const readAgain = useCallback(() => setReadTurn((turn) => turn + 1), []);
  const readUnanswered = useSettingsReadUnanswered(readAgain);
  const unanswered = useSettingsWriteUnanswered(() => { readAgain(); changedRef.current?.(); });
  useEffect(() => setState(null), [dataSourceId, pageId, session, taskDatabase]);
  useEffect(() => {
    if (!session || !taskDatabase) return;
    const controller = new AbortController();
    readDatabaseSprints(session, pageId, dataSourceId, controller.signal)
      .then((next) => { if (!controller.signal.aborted) setState(next); })
      .catch((error) => { if (!controller.signal.aborted && !readUnanswered(error)) setState(null); });
    return () => controller.abort();
  }, [dataSourceId, pageId, readTurn, readUnanswered, session, taskDatabase]);
  useEffect(() => {
    const current = state?.enabled ? state.sprints.find(({ status }) => status === "current") : undefined;
    return state?.currentViewId && current?.title ? nameCurrentSprintView(state.currentViewId, current.title) : undefined;
  }, [state]);
  const boardPageId = state?.enabled ? state.boardPageId : undefined;
  useEffect(() => boardPageId ? markSprintBoardPage(boardPageId) : undefined, [boardPageId]);
  useEffect(() => state?.enabled && state.sprintsDataSourceId ? keepRelativeSprints(state.sprintsDataSourceId, state.sprints) : undefined, [state]);

  const runWithChanges = useCallback(async (request: DatabaseSprintRequest): Promise<{ outcome: DatabaseSprintOutcome; changes?: DatabaseSprintChanges }> => {
    if (!session) return { outcome: "failed" };
    try {
      const response = await runDatabaseSprintAction(session, pageId, dataSourceId, {
        ...request, requestId: newId(), expectedRevision: stateRef.current?.revision ?? 0,
      });
      setState(response.state);
      changedRef.current?.();
      return { outcome: "done", changes: response.changes };
    } catch (error) {
      if (unanswered(error)) return { outcome: "unanswered" };
      readAgain();
      const code = error && typeof error === "object" ? (error as { code?: unknown }).code : undefined;
      return { outcome: code === pagePermanentDeleteErrorCodes.legalHold ? "legal-hold" : "failed" };
    }
  }, [dataSourceId, pageId, readAgain, session, unanswered]);
  const run = useCallback(async (request: DatabaseSprintRequest) => (await runWithChanges(request)).outcome, [runWithChanges]);

  return useMemo(
    () => session && taskDatabase ? { state, timeZone: browserTimeZone(), run, runWithChanges, pageId } : null,
    [pageId, run, runWithChanges, session, state, taskDatabase],
  );
}
