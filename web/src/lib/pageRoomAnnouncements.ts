import type { WebsocketProvider } from "y-websocket";
import { announceAutomationRevision, announcedAutomationRevision } from "./automationRevisionEvents";
import { announceCommentRevision } from "./commentRevisionEvents";
import { announcedGraphCursor, type GraphCursorWord } from "./graphCursorWord";
import { announcedGraphReaderRevision, announceGraphReaderWord } from "./graphReaderEvents";

/** The comment revision the server's own awareness state tells the room, if it tells one. */
function announcedCommentRevision(state: unknown) {
  const revision = (state as { leafenComments?: { revision?: unknown } } | null)?.leafenComments?.revision;
  return typeof revision === "number" && Number.isSafeInteger(revision) && revision > 0 ? revision : 0;
}

/**
 * What the server tells a page's room in its awareness: another session's
 * block changes reach the page as the graph's new position
 * (ygo_room_publication.go), comments others post, resolve or remove as the
 * comments' revision, rows the page reads beyond its own graph as the
 * readers' revisions (lib/graphReaderEvents.ts), and its databases'
 * automations as theirs (lib/automationRevisionEvents.ts) — each told on
 * when it moves on.
 */
export function hearRoomAnnouncements(awareness: WebsocketProvider["awareness"], pageId: string,
  graphCursor: GraphCursorWord & { tell(cursor: number): void }, stopped: () => boolean) {
  let commentRevision = 0;
  let automationRevision = 0;
  const readerRevisions = { rows: 0, external: 0 };
  awareness.on("change", () => {
    if (stopped()) return;
    let furthest = 0;
    let comments = 0;
    let automations = 0;
    const readers = { rows: 0, external: 0 };
    for (const state of awareness.getStates().values()) {
      furthest = Math.max(furthest, announcedGraphCursor(state));
      comments = Math.max(comments, announcedCommentRevision(state));
      automations = Math.max(automations, announcedAutomationRevision(state));
      readers.rows = Math.max(readers.rows, announcedGraphReaderRevision(state, "leafenRows"));
      readers.external = Math.max(readers.external, announcedGraphReaderRevision(state, "leafenExternal"));
    }
    if (furthest > graphCursor.current()) graphCursor.tell(furthest);
    if (comments > commentRevision) {
      commentRevision = comments;
      announceCommentRevision(pageId, comments);
    }
    if (automations > automationRevision) {
      automationRevision = automations;
      announceAutomationRevision(pageId, automations);
    }
    for (const kind of ["rows", "external"] as const) {
      if (readers[kind] <= readerRevisions[kind]) continue;
      readerRevisions[kind] = readers[kind];
      announceGraphReaderWord({ kind, pageId, revision: readers[kind] });
    }
  });
}
