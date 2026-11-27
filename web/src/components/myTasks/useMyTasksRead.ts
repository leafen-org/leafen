import { useCallback, useEffect, useRef, useState } from "react";
import { readMyTaskSources, readMyTasksToTheEnd } from "@/databaseToolbarConfigApi";
import type { DatabaseMyTask, DatabaseMyTaskSource, DatabaseMyTaskSources } from "@/databaseToolbarConfigWire.generated";
import { keptSentFor } from "@/lib/keptSentFor";
import { afterMyTaskFilled, noteMyTaskSourceTemplates } from "@/lib/myTaskMadeHere";
import { afterPageMade } from "@/lib/pagesNotYetMade";
import { isServerUnreachable, retryWhenServerMayAnswer } from "@/lib/serverReach";
import type { Session } from "@/types";

/**
 * The sources "내 작업" reads its tasks from, wherever it shows — its page,
 * Home's widget, the window to pick them: those the person picked, none until
 * they pick, as the server reads their tasks (scope "sources" with nothing
 * stored) and as Notion's My tasks with no source shows none of the person's
 * tasks and its window opens with none picked (seen 2026-11-24, two tabs).
 */
export const pickedMyTaskSources = (sources: DatabaseMyTaskSources | null): readonly DatabaseMyTaskSource[] => sources?.sources ?? [];

type Read = { sources: DatabaseMyTaskSources; tasks: DatabaseMyTask[] };

// The sources and tasks last read in each workspace this visit, for a screen
// that keeps them while the server doesn't answer (`keepOffline`).
const lastRead = new Map<string, Read>();

/**
 * "내 작업"'s sources and every one of its tasks, read a window at a time
 * (readMyTasksToTheEnd): the first read shows its first window with the
 * sources and the whole list when it is in; a read again keeps the list in
 * view until the whole list replaces it, rather than shrinking to a window
 * and growing back. A task made here stays though a read misses it, until a
 * read that began once the server had made it (2026-11-25: a board's
 * "+ 새 작업" card left empty with Escape went from view while the server kept
 * the task — the board reads the list again as it makes a task, and that read
 * came back before the server had it), and keeps the icon its template gave
 * it here though a read finds none, until a read that began once the server
 * had filled it from that template (2026-11-25: the card's icon went as the
 * read after its making caught it unfilled). With `keepOffline` (Home), a server that doesn't answer
 * leaves the tasks last read in view, or says they aren't available offline,
 * and they are read again when it may answer.
 */
export function useMyTasksRead(session: Session | undefined, workspaceId: string | undefined, { keepOffline = false } = {}) {
  const key = session && workspaceId ? `${session.user.id}\u0000${workspaceId}` : "";
  const kept = keepOffline ? lastRead.get(key) : undefined;
  const [sources, setSources] = useState<DatabaseMyTaskSources | null>(kept?.sources ?? null);
  const [tasks, setTasks] = useState<DatabaseMyTask[] | null>(kept?.tasks ?? null);
  const [failed, setFailed] = useState(false);
  const [offline, setOffline] = useState(false);
  const [attempt, setAttempt] = useState(0);
  const tasksInView = useRef(false);
  tasksInView.current = tasks !== null;
  // Each task made here, whether the server has made it yet (afterPageMade) and filled it from its template (afterMyTaskFilled).
  const madeHere = useRef(new Map<string, { made: boolean; filled: boolean }>());
  // A task made in a source shows its default template from the start, as read with the sources (myTaskMadeHere.ts).
  useEffect(() => {
    if (session && sources) noteMyTaskSourceTemplates(session, sources.sources);
  }, [session, sources]);
  useEffect(() => {
    if (!session || !workspaceId) return undefined;
    const controller = new AbortController();
    let stopRetry: (() => void) | undefined;
    // This read tells of the tasks the server had made as it began, and the icons of those it had filled; those made here since
    // stay if it misses them, with their icon if it finds none.
    const before = new Map([...madeHere.current].map(([rowId, task]) => [rowId, { ...task }]));
    setFailed(false);
    const sourcesRead = readMyTaskSources(session, workspaceId, controller.signal);
    const tasksRead = readMyTasksToTheEnd(session, workspaceId, { signal: controller.signal,
      onFirst: tasksInView.current ? undefined : (first) => void sourcesRead.then((nextSources) => {
        if (controller.signal.aborted) return;
        setSources(nextSources);
        setTasks(first);
      }, () => undefined) });
    Promise.all([sourcesRead, tasksRead]).then(([nextSources, all]) => {
      setSources(nextSources);
      setOffline(false);
      // What this device keeps for a source's page went after the read began — a task's values kept across a reload
      // (rowValuesKept.ts) among them: the tasks are read again once it went (keptSentFor).
      for (const pageId of new Set(nextSources.sources.map((source) => source.pageId))) {
        void keptSentFor(session, pageId).then((waited) => { if (waited && !controller.signal.aborted) setAttempt((value) => value + 1); });
      }
      for (const [rowId, task] of before) if (task.made && task.filled) madeHere.current.delete(rowId);
      setTasks((current) => {
        const read = new Set(all.map((task) => task.rowId));
        const here = new Map((current ?? []).filter((task) => madeHere.current.has(task.rowId)).map((task) => [task.rowId, task]));
        const missed = [...here.values()].filter((task) => !before.get(task.rowId)?.made && !read.has(task.rowId));
        const unfilled = (task: DatabaseMyTask) => {
          const shown = before.get(task.rowId)?.filled ? undefined : here.get(task.rowId);
          return shown?.icon && !task.icon ? { ...task, icon: shown.icon, iconType: shown.iconType } : task;
        };
        const next = [...missed, ...(here.size ? all.map(unfilled) : all)];
        if (keepOffline) lastRead.set(`${session.user.id}\u0000${workspaceId}`, { sources: nextSources, tasks: next });
        return next;
      });
    }, (error: unknown) => {
      if (controller.signal.aborted) return;
      if (!keepOffline || !isServerUnreachable(error)) {
        setFailed(true);
        return;
      }
      setOffline(true);
      stopRetry = retryWhenServerMayAnswer(() => setAttempt((value) => value + 1));
    });
    return () => {
      controller.abort();
      stopRetry?.();
    };
  }, [attempt, keepOffline, session, workspaceId]);
  const reload = useCallback(() => setAttempt((value) => value + 1), []);
  /** A task made here joins the list at once: first with `first`, else last. */
  const taskMade = useCallback((task: DatabaseMyTask, first = false) => {
    const made = { made: false, filled: false };
    madeHere.current.set(task.rowId, made);
    void afterPageMade(task.rowId).then(() => { made.made = true; });
    void afterMyTaskFilled(task.rowId).then(() => { made.filled = true; });
    setTasks((current) => first ? [task, ...current ?? []] : [...current ?? [], task]);
  }, []);
  return { sources, setSources, tasks, failed, offline, reload, taskMade };
}

export type MyTasksRead = ReturnType<typeof useMyTasksRead>;
