import type { PageVisit } from "@/workspaceMembershipWire.generated";

/**
 * A visit made on this device: the page and when it opened, with when the
 * server kept it once it has (`kept`). `workspaceId` is absent where the page
 * that records it doesn't say (a database row's page): such a visit belongs
 * to whichever workspace lists it, which shows only its own pages.
 */
type VisitHere = { userId: string; workspaceId?: string; pageId: string; at: number; kept?: number };

/** As many visits made here as a list of visits shows (workspaceMembershipBounds.pageVisitsListed). */
const KEPT_HERE = 100;

let visitsHere: readonly VisitHere[] = [];
const listeners = new Set<() => void>();

function changed(next: readonly VisitHere[]) {
  visitsHere = next;
  for (const listener of listeners) listener();
}

/**
 * Notes a page's visit as it opens (usePageVisitRecorder): sent once the page
 * is drawn (pageSideWork.ts), a page made here once the server has it, the
 * visit reaches the server a second or more later, and a list read meanwhile
 * lacked the page open (2026-11-22: Home opened at once, a page move window).
 * Notion counts a page visited as it opens. Answers what the server did with
 * it: kept it, or not.
 */
export function noteVisitHere(userId: string, pageId: string, workspaceId?: string) {
  const visit: VisitHere = { userId, pageId, at: Date.now(), ...(workspaceId ? { workspaceId } : {}) };
  changed([visit, ...visitsHere.filter((other) => other.userId !== userId || other.pageId !== pageId)].slice(0, KEPT_HERE));
  return {
    kept() {
      if (visitsHere.includes(visit)) changed(visitsHere.map((other) => other === visit ? { ...visit, kept: Date.now() } : other));
    },
    refused() {
      if (visitsHere.includes(visit)) changed(visitsHere.filter((other) => other !== visit));
    },
  };
}

export function subscribeVisitsHere(listener: () => void) {
  listeners.add(listener);
  return () => void listeners.delete(listener);
}

export function visitsHereNow() {
  return visitsHere;
}

/**
 * A list of visits the server answered, read from `readAt` on, with the
 * visits made here that it may lack first, newest first: those the server
 * hadn't kept when the read was asked. A visit kept before is the server's
 * to place.
 */
export function withVisitsHere(list: readonly PageVisit[], readAt: number, userId: string, workspaceId: string,
  here: readonly VisitHere[] = visitsHere): PageVisit[] {
  const missing = here.filter((visit) => visit.userId === userId && (!visit.workspaceId || visit.workspaceId === workspaceId)
    && (visit.kept === undefined || visit.kept >= readAt));
  if (missing.length === 0) return list as PageVisit[];
  const first = new Set(missing.map((visit) => visit.pageId));
  return [
    ...missing.map((visit) => ({ pageId: visit.pageId, visitedAt: new Date(visit.at).toISOString() })),
    ...list.filter((visit) => !first.has(visit.pageId)),
  ];
}
