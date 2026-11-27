import { useEffect } from "react";
import { api } from "@/api";
import { heldSignIn } from "@/lib/authSessionStore";
import { afterPageShown } from "@/lib/pageSideWork";
import { noteVisitHere } from "@/lib/pageVisitsHere";
import { saveLastOpenedPage } from "@/lib/startPage";
import type { Session } from "@/types";

/** The same page's visit is kept at most this often. */
const PAGE_VISIT_INTERVAL = 60_000;
// When each page's visit was last sent, by whichever recorder: a row's page
// and the view that opened it both record it, which sent it twice.
const recorded = new Map<string, number>();

/**
 * Keeps the reader's visit to the page they open, for their library's
 * "최근" and "마지막 방문 시간"; a visit that can't be sent is tried again
 * the next time the page opens. A page made on this device is visited once
 * the server has it (apiTransport.ts). This device keeps the page too, to
 * start with when the server can't be reached. The visit goes once the page
 * is drawn and has read what it shows (pageSideWork.ts), even if another
 * page opens meanwhile; until the server keeps it, the lists that read
 * visits as they are now (usePageVisitsNow) have it already.
 */
export function usePageVisitRecorder(session: Session | null | undefined, pageId: string | null, workspaceId?: string) {
  useEffect(() => {
    if (!pageId || !session) return;
    if (workspaceId) saveLastOpenedPage(session.user.id, workspaceId, pageId);
    const now = Date.now(), key = `${session.user.id}:${pageId}`;
    if (now - (recorded.get(key) ?? 0) < PAGE_VISIT_INTERVAL) return;
    recorded.set(key, now);
    const visit = noteVisitHere(session.user.id, pageId, workspaceId);
    afterPageShown(() => {
      // With the sign-in held then: one signed out meanwhile keeps it for next time.
      const held = heldSignIn(session);
      if (!held) {
        visit.refused();
        return void recorded.delete(key);
      }
      void api.recordPageVisit(held, pageId).then(visit.kept, () => {
        visit.refused();
        recorded.delete(key);
      });
    });
  }, [pageId, session, workspaceId]);
}
