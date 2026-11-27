import { useMemo } from "react";
import type { Session } from "@/types";
import { parseStored, personValue, storePersonValue } from "@/lib/browserStorage";
import { usePageVisitsNow } from "./library/useLibraryData";

const MOVED_TO_KEY = "leafen:move-to:recent-pages";
/** The most places a reader's moves are remembered, as Notion keeps them (seen 2026-11-19: ten). */
const MOVED_TO_LIMIT = 10;

/**
 * The pages the reader last moved pages or blocks into in a workspace,
 * newest first, kept on this browser as the reader's own, as Notion keeps
 * its "옮기기" places (seen 2026-11-19 in two Chrome tabs: a page moved into
 * another came first in the next move's suggestions, that place now its
 * parent among them).
 */
export function movedToPageIds(userId: string, workspaceId: string): string[] {
  const kept = parseStored(personValue(MOVED_TO_KEY, { userId, part: workspaceId }));
  return Array.isArray(kept) ? kept.filter((id): id is string => typeof id === "string" && id !== "").slice(0, MOVED_TO_LIMIT) : [];
}

/** Remembers a page as the place the reader last moved something into. */
export function rememberMoveTarget(session: Session, workspaceId: string, pageId: string) {
  if (!workspaceId || !pageId) return;
  const ids = [pageId, ...movedToPageIds(session.user.id, workspaceId).filter((id) => id !== pageId)].slice(0, MOVED_TO_LIMIT);
  storePersonValue(MOVED_TO_KEY, JSON.stringify(ids), { userId: session.user.id, part: workspaceId });
}

/**
 * What a move window suggests before a search (pageMoveDialogModel): the
 * pages the reader last moved into and the pages they visited last, each
 * newest first, read as the window opens — a page opened a moment ago among
 * them before its visit reaches the server (usePageVisitsNow), as Notion's
 * page window suggests the page open, first of those visited (seen
 * 2026-11-22, another page moved from the sidebar).
 */
export function useMoveSuggestions(session: Session, workspaceId: string) {
  const visits = usePageVisitsNow(session, workspaceId);
  const recentPageIds = useMemo(() => visits.map((visit) => visit.pageId), [visits]);
  const movedTo = useMemo(() => movedToPageIds(session.user.id, workspaceId), [session.user.id, workspaceId]);
  return { recentPageIds, movedToPageIds: movedTo };
}
