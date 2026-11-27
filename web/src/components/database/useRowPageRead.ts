import { useLayoutEffect, useRef, useState } from "react";
import { keepUntakenTyping } from "@/lib/pageTitleFocus";
import { rowNotYetMade } from "@/lib/rowMadeHere";
import { ROW_FILLED_FROM_TEMPLATE_EVENT } from "@/lib/databaseTemplateRequests";
import { answeredUnlessAway, isServerUnreachable, retryWhenServerMayAnswer } from "@/lib/serverReach";
import { followAccess } from "@/lib/workspaceChangeCheck";
import { onRowChanged } from "@/lib/rowChangeWords";
import type { GraphRow } from "@/contentGraph";

// A row page the server says isn't there yet, while the row is on this device:
// read again this often, this many times, as its creation goes out first.
const ROW_PAGE_NOT_YET_MS = 2_000;
const ROW_PAGE_NOT_YET_TRIES = 5;

/**
 * Reads the page of the row a view opened. A row on this device the server
 * doesn't have yet — made while it couldn't be reached, kept in the outbox —
 * stays open and its page is read again when the server may answer, so what
 * was typed for its title goes in once it opens (2026-11-07: on a stalled
 * network New's row page closed as the network came back, read before the
 * row was sent, and the title typed for it was lost). Any other failure
 * closes it with the failure said. Returns the row whose page the server
 * wasn't there to read, which the view shows as a page then says
 * (RowPageUnavailable) rather than as nothing. A row made here that the
 * server doesn't have yet opens at once as made here (showMadeHere), its
 * page read once the server has it (rowMadeHere.ts). The page's body starts
 * its read on the same press (graphReadAhead.ts).
 */
export function useRowPageRead(
  rowId: string | undefined,
  service: { refresh(rowId: string): Promise<unknown>; readBodyAhead?(rowId: string): void } | null,
  rows: readonly GraphRow[],
  clear: () => void,
  fail: () => void,
  keepTitle?: (rowId: string, text: string, rows: readonly GraphRow[]) => void,
  showMadeHere?: (row: GraphRow) => void,
) {
  const rowsHere = useRef(rows);
  rowsHere.current = rows;
  const latest = useRef({ clear, fail, keepTitle, showMadeHere });
  latest.current = { clear, fail, keepTitle, showMadeHere };
  const [unanswered, setUnanswered] = useState<string | null>(null);
  // Before the effects of what the peek draws while it reads — its backlinks,
  // comments, verification, layout — so its own read and its body's go to the
  // server first (2026-11-12: the row's read went eighth, behind them, and
  // waited for one of the browser's six connections).
  useLayoutEffect(() => {
    setUnanswered(null);
    if (!rowId || !service) {
      latest.current.clear();
      return;
    }
    // What was typed for the row and not taken by its page is its title.
    const stopKeeping = keepUntakenTyping(rowId, (text) => latest.current.keepTitle?.(rowId, text, rowsHere.current));
    let stopped = false;
    let stopWaiting = () => {};
    let notYetTries = 0;
    const madeHere = rowNotYetMade(rowId) ? rowsHere.current.find((row) => row.id === rowId) : undefined;
    if (madeHere) latest.current.showMadeHere?.(madeHere);
    // A read fails at once while the connection is known lost, so the page
    // says it couldn't open rather than reading on a stalled network
    // (answeredUnlessAway; 2026-11-16).
    const read = () => {
      void answeredUnlessAway(service.refresh(rowId)).then(() => {
        stopWaiting();
        if (!stopped) setUnanswered(null);
      }, (error) => {
        if (stopped) return;
        const unreachable = isServerUnreachable(error);
        if (unreachable) setUnanswered(rowId);
        if (rowsHere.current.some((row) => row.id === rowId) && (unreachable || notYetTries++ < ROW_PAGE_NOT_YET_TRIES)) {
          stopWaiting();
          if (unreachable) stopWaiting = retryWhenServerMayAnswer(read);
          else {
            const timer = window.setTimeout(read, ROW_PAGE_NOT_YET_MS);
            stopWaiting = () => window.clearTimeout(timer);
          }
          return;
        }
        latest.current.fail();
      });
    };
    read();
    // Its body is read beside its page, not once it came (graphReadAhead.ts).
    if (!madeHere) service.readBodyAhead?.(rowId);
    // Filled from its template, its page is read again: its icon and body came with it.
    const filled = (event: Event) => { if ((event as CustomEvent<string>).detail === rowId) read(); };
    window.addEventListener(ROW_FILLED_FROM_TEMPLATE_EVENT, filled);
    // What the person may do there changed — a share raised or dropped — it is
    // read again at once, so it turns editable or read-only as an open page
    // does (useRowPageReadAgain.ts); so it is when the row changed elsewhere
    // (lib/rowChangeWords.ts), as when another device locks or unlocks it: its
    // lock is its page's, not among the view's rows (2026-11-18: the peek
    // showed it as first read until reopened).
    const stopAccess = followAccess(read);
    const stopRows = onRowChanged(rowId, read);
    return () => {
      stopped = true;
      stopWaiting();
      stopKeeping();
      stopAccess();
      stopRows();
      window.removeEventListener(ROW_FILLED_FROM_TEMPLATE_EVENT, filled);
    };
  }, [rowId, service]);
  return unanswered;
}

/**
 * Saves what was typed for a new row its page didn't take as the row's title,
 * on this device first as any title is: its first line, after what the row's
 * title already holds.
 */
export function saveHeldRowTitle(
  adapter: { commitRows(rows: GraphRow[], isCurrent: () => boolean): Promise<unknown> } | undefined,
  rows: readonly GraphRow[],
  titlePropertyId: string | undefined,
  rowId: string,
  text: string,
  isCurrent: () => boolean,
) {
  const row = rows.find((candidate) => candidate.id === rowId);
  const line = text.split("\n")[0]?.trim() ?? "";
  if (!adapter || !row || !titlePropertyId || !line) return;
  const before = row.values[titlePropertyId]?.value;
  const title = `${typeof before === "string" ? before : ""}${line}`;
  void adapter.commitRows([{ ...row, values: { ...row.values, [titlePropertyId]: { type: "title", value: title } } }], isCurrent);
}
