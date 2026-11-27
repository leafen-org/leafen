import { useCallback, useRef } from "react";
import { dropNativeEditorCommand, nativeEditorCommandIsLatest, recordCommittedNativeEditorCommand, recordShownNativeEditorCommand } from "@/components/editor/nativeEditorUndoOwner";
import { peekPageOwningCommands } from "@/components/editor/editorScope";
import { authenticatedProductErrorMessage } from "@/lib/authenticatedProductErrors";
import type { ProductStatusSink } from "@/lib/productStatus";

/** How long after a move with dates the date command its dates' write brings may still join its step. */
const DATES_JOIN_MOVE_MS = 5_000;

/**
 * Date commands and a table's changes of values use the same page/principal
 * history as native text and blocks. A date's is read again after each step,
 * for the dates the server moved with it; values are shown as they are saved.
 * A change made in a peek over the page, a row's values or properties, goes
 * in the peek's page history, where ⌘Z there takes it back.
 */
export function useDatabaseDateCommandHistory(input: {
  workspaceId?: string;
  pageId: string | null;
  principalId: string;
  sessionIdentity: string;
  restore(pageId: string, mutationId: string, forward: boolean): Promise<boolean>;
  reread(): Promise<boolean>;
  onStatus: ProductStatusSink;
}) {
  const latest = useRef(input);
  latest.current = input;
  // The history each command went in, by its key.
  const scopes = useRef(new Map<string, string>());
  const recordCommand = useCallback((key: string, step: (forward: boolean) => Promise<boolean>, reread: boolean, shown = false) => {
    const captured = input;
    if (!captured.workspaceId || !captured.pageId) return false;
    const scope = `${captured.workspaceId}/${captured.pageId}/${captured.principalId}`;
    const current = () => latest.current.workspaceId === captured.workspaceId && latest.current.pageId === captured.pageId
      && latest.current.principalId === captured.principalId && latest.current.sessionIdentity === captured.sessionIdentity;
    if (!current()) return false;
    const stepped = async (forward: boolean) => {
      if (!current()) return false;
      try {
        if (!await step(forward) || !current()) return false;
        return reread ? await latest.current.reread() && current() : true;
      } catch (error) {
        if (current()) latest.current.onStatus(authenticatedProductErrorMessage(error, "contentGraphUpdate"), "error");
        return false;
      }
    };
    const peek = peekPageOwningCommands();
    const peekScope = peek && peek !== captured.pageId ? `${captured.workspaceId}/${peek}/${captured.principalId}` : null;
    for (const into of peekScope ? [peekScope, scope] : [scope]) {
      if ((shown ? recordShownNativeEditorCommand : recordCommittedNativeEditorCommand)(into, key, stepped)) {
        // Only a recent command is asked about; a history keeps 200.
        scopes.current.set(key, into);
        if (scopes.current.size > 200) scopes.current.delete(scopes.current.keys().next().value!);
        return true;
      }
    }
    return false;
  }, [input.workspaceId, input.pageId, input.principalId, input.sessionIdentity]);
  const recordDateCommand = useCallback((key: string, step: (forward: boolean) => Promise<boolean>) => recordCommand(key, step, true), [recordCommand]);
  const recordValueCommand = useCallback((key: string, step: (forward: boolean) => Promise<boolean>) => recordCommand(key, step, false), [recordCommand]);
  // A table's values taken back or written again show as the step is taken: it lands at once (recordShownNativeEditorCommand).
  const recordShownValueCommand = useCallback((key: string, step: (forward: boolean) => Promise<boolean>) => recordCommand(key, step, false, true), [recordCommand]);
  // Whether the page's history ends with a step, nothing done or undone since.
  const isLatestCommand = useCallback((key: string) => {
    const current = latest.current;
    return !!current.workspaceId && !!current.pageId
      && nativeEditorCommandIsLatest(scopes.current.get(key) ?? `${current.workspaceId}/${current.pageId}/${current.principalId}`, key);
  }, []);
  // A row's move whose dates another write moved at once (a timeline's bar
  // carried along the days and up or down) is one step with the date command
  // that write brings, still the history's latest when it comes, as Notion's
  // one ⌘Z took the place, the group and the dates back together (seen
  // 2026-11-24 in Chrome, two tabs, three times, across groups too).
  const joining = useRef<{ key: string; until: number; dates: ((forward: boolean) => Promise<boolean>)[] } | null>(null);
  const recordMoveWithDates = useCallback((key: string, step: (forward: boolean) => Promise<boolean>) => {
    const join = { key, until: Date.now() + DATES_JOIN_MOVE_MS, dates: [] as ((forward: boolean) => Promise<boolean>)[] };
    joining.current = join;
    return recordCommand(key, async (forward) => {
      if (!forward) for (const dates of [...join.dates].reverse()) if (!await dates(false)) return false;
      if (!await step(forward)) return false;
      if (forward) for (const dates of join.dates) if (!await dates(true)) return false;
      return true;
    }, false);
  }, [recordCommand]);
  const onDateCommand = useCallback((authorityPageId: string, mutationId: string) => {
    const restore = (forward: boolean) => latest.current.restore(authorityPageId, mutationId, forward);
    const join = joining.current;
    joining.current = null;
    if (join && Date.now() <= join.until && isLatestCommand(join.key)) {
      join.dates.push(async (forward) => await restore(forward) && latest.current.reread());
      return;
    }
    recordDateCommand(`${authorityPageId}/${mutationId}`, restore);
  }, [isLatestCommand, recordDateCommand]);
  // A step recorded as its edit showed leaves the history when the save is refused.
  const dropCommand = useCallback((key: string) => {
    const scope = scopes.current.get(key);
    if (scope) dropNativeEditorCommand(scope, key);
  }, []);
  return { onDateCommand, recordDateCommand, recordValueCommand, recordShownValueCommand, recordMoveWithDates, isLatestCommand, dropCommand };
}
