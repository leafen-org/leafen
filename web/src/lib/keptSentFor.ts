import type { Session } from "@/types";
import { contentGraphOutboxOf } from "./contentGraphSend";

/**
 * Settles once the account's outbox keeps nothing more for a page — a row's
 * holder, which its values are written to — and says whether there was
 * anything to wait for. A row the server copies is copied as the server
 * holds it, so a copy asked for straight from the server (a row page's or
 * the sidebar's "복제", a wiki page's) asks once what this device wrote of
 * the row went (2026-11-19: at a 5 s round trip a name typed just before
 * the copy was missing from it). A table's own copies wait in its page's
 * writes too (nativeTableRowSteps.ts duplicateRows).
 */
export async function keptSentFor(session: Pick<Session, "user">, pageId: string): Promise<boolean> {
  const waiting = contentGraphOutboxOf(session)?.graphPagesWaiting;
  if (!waiting) return false;
  await waiting.ready();
  if (!waiting.has(pageId)) return false;
  await new Promise<void>((resolve) => {
    let stop = () => {};
    let done = false;
    const check = () => {
      if (done || waiting.has(pageId)) return;
      done = true;
      stop();
      resolve();
    };
    stop = waiting.subscribe(check);
    check();
  });
  return true;
}
