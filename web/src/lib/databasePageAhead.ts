import { api } from "@/api";
import type { ContentGraph } from "@/contentGraph";
import { collectAuthenticatedContentGraph } from "@/lib/authenticatedContentGraph";
import type { Session } from "@/types";

/** What turning a full-page database inline leaves behind: its page's shares and discussions. */
export type DatabasePageLeftovers = { shares: number; comments: number };

type Ahead = { graph: ContentGraph | null; leftovers: Promise<DatabasePageLeftovers | null> };

// A page of more windows than this isn't held: its turn waits for the server, as before.
const AHEAD_MOST_WINDOWS = 20;
const ahead = new Map<string, Ahead>();

function key(principalId: string, pageId: string) {
  return `${principalId}\u0000${pageId}`;
}

/**
 * Reads a full-page database's page while a menu that turns it inline is
 * open on its link: its graph, for the database to stand in the link's place
 * in the press's own frame where the page hasn't shown in this tab
 * (databaseTurnedBackHere.ts — 2026-11-14: from a new tab the turn waited
 * 1.0–1.2 s for the server on a NAS and read "휴지통에 있음" a moment), and
 * what the turn leaves behind, which the press asks about first. Forgotten
 * as the menu closes (forgetDatabasePageAhead).
 */
export function readDatabasePageAhead(session: Session, pageId: string) {
  const at = key(session.user.id, pageId);
  if (ahead.has(at)) return;
  const entry: Ahead = { graph: null, leftovers: api.databasePageLeftovers(session, pageId).catch(() => null) };
  ahead.set(at, entry);
  let windows = 0;
  void collectAuthenticatedContentGraph(session, pageId, null, () => ++windows <= AHEAD_MOST_WINDOWS && ahead.get(at) === entry)
    .then((read) => { if (read && ahead.get(at) === entry) entry.graph = read.graph; })
    .catch(() => undefined);
}

/** The database page's graph read ahead, once it has come. */
export function databasePageAhead(principalId: string, pageId: string): ContentGraph | null {
  return ahead.get(key(principalId, pageId))?.graph ?? null;
}

/** What turning the database page inline leaves behind, as read ahead; null when it wasn't. */
export function databasePageLeftoversAhead(principalId: string, pageId: string) {
  return ahead.get(key(principalId, pageId))?.leftovers ?? null;
}

export function forgetDatabasePageAhead(principalId: string, pageId: string) {
  ahead.delete(key(principalId, pageId));
}
