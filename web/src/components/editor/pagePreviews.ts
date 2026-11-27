import { api } from "@/api";
import { LeafenApiError } from "@/apiTransport";
import { isServerUnreachable, readUntilServerAnswers } from "@/lib/serverReach";
import type { Session } from "@/types";
import type {
  BlockMentionPreviewResponse, DatabaseMentionPreviewResponse, DataSourceMentionPreviewResponse, PagePreviewResponse,
} from "@/workspaceSearchWire.generated";

// A page's or a mentioned database's preview, read once while the app is
// open, shared by a mention's card, its opening and the icons of mentioned
// pages the workspace's page list lacks (database pages); the last one each
// read gave, known at once to a card shown before its own read comes back.
const previews = new Map<string, Promise<unknown>>();
const lastRead = new Map<string, unknown>();

/**
 * Where a read the server didn't answer (isServerUnreachable) ends: as one
 * not there (null), or refused (`"refused"`) for the names a screen shows,
 * which keep what they showed and read again once the server may answer
 * (readUntilServerAnswers) — taken for a page not there, a mention read with
 * the server out of reach kept the words it was written with, and its plain
 * icon, after the server was back (2026-11-25).
 */
export type PreviewUnanswered = "null" | "refused";

function readOnce<T>(key: string, read: () => Promise<T>, unanswered: PreviewUnanswered = "null"): Promise<T | null> {
  let preview = previews.get(key) as Promise<T | null> | undefined;
  if (!preview) {
    preview = read().then((answer) => {
      lastRead.set(key, answer);
      return answer;
    }, (error: unknown) => {
      // What isn't there stays unread; anything else is read again next time.
      if (error instanceof LeafenApiError && error.status === 404) lastRead.set(key, null);
      else previews.delete(key);
      if (isServerUnreachable(error)) throw error;
      return null;
    });
    previews.set(key, preview);
  }
  return unanswered === "refused" ? preview : preview.catch(() => null);
}

const pageKey = (session: Session, pageId: string) => `${session.user.id}\u0000page\u0000${pageId}`;

/**
 * The page's preview with its first blocks, for a card to draw, or null for a
 * page the reader can't open; one in the trash they may open too, marked, as
 * a mention of it shows so (mention_trash_mark.go).
 */
export function pagePreviewOnce(session: Session, pageId: string, unanswered?: PreviewUnanswered): Promise<PagePreviewResponse | null> {
  return readOnce(pageKey(session, pageId), () => api.readPagePreview(session, pageId, { blocks: true, trash: true }), unanswered);
}

/** The page's preview as last read, undefined while none was. */
export function pagePreviewKnown(session: Session, pageId: string): PagePreviewResponse | null | undefined {
  return lastRead.get(pageKey(session, pageId)) as PagePreviewResponse | null | undefined;
}

/**
 * The page's preview read again, for a card shown once more to draw the page
 * as it is now; what was read before stands meanwhile.
 */
export function pagePreviewAgain(session: Session, pageId: string, unanswered?: PreviewUnanswered): Promise<PagePreviewResponse | null> {
  previews.delete(pageKey(session, pageId));
  return pagePreviewOnce(session, pageId, unanswered);
}

/** A mentioned database's name and the page holding it, or null for one the reader can't open. */
export function databasePreviewOnce(session: Session, containerId: string,
  unanswered?: PreviewUnanswered): Promise<DatabaseMentionPreviewResponse | null> {
  return readOnce(`${session.user.id}\u0000database\u0000${containerId}`, () => api.readDatabaseMentionPreview(session, containerId), unanswered);
}

/** A mentioned database's preview read again, for its mentions' name to follow a rename; its card then shows the same. */
export function databasePreviewAgain(session: Session, containerId: string,
  unanswered?: PreviewUnanswered): Promise<DatabaseMentionPreviewResponse | null> {
  previews.delete(`${session.user.id}\u0000database\u0000${containerId}`);
  return databasePreviewOnce(session, containerId, unanswered);
}

const blockKey = (session: Session, pageId: string, blockId: string) => `${session.user.id}\u0000block\u0000${pageId}\u0000${blockId}`;

/** A mentioned block's words and its page's title, as last read when they were; null for a block the reader can't open there. */
export function blockMentionPreviewOnce(session: Session, pageId: string, blockId: string,
  unanswered?: PreviewUnanswered): Promise<BlockMentionPreviewResponse | null> {
  return readOnce(blockKey(session, pageId, blockId), () => api.readBlockMentionPreview(session, pageId, blockId), unanswered);
}

/** A mentioned block's words and its page's title read again, for its mention to follow them; null for a block the reader can't open there. */
export function blockMentionPreviewAgain(session: Session, pageId: string, blockId: string,
  unanswered?: PreviewUnanswered): Promise<BlockMentionPreviewResponse | null> {
  previews.delete(blockKey(session, pageId, blockId));
  return blockMentionPreviewOnce(session, pageId, blockId, unanswered);
}

/**
 * Lets go of the pages' previews as read, each read again the next time one
 * is asked for: those whose mentions a screen didn't read again after a
 * change, being further from its view (useMentionWindow), name the page as
 * it is now once they come nearer.
 */
export function forgetPagePreviews(session: Session, pageIds: Iterable<string>) {
  for (const pageId of pageIds) previews.delete(pageKey(session, pageId));
}

/** A mentioned data source's name, icon and database at its view, or null for one the reader can't open. */
export function dataSourcePreviewOnce(session: Session, dataSourceId: string,
  unanswered?: PreviewUnanswered): Promise<DataSourceMentionPreviewResponse | null> {
  return readOnce(`${session.user.id}\u0000dataSource\u0000${dataSourceId}`, () => api.readDataSourceMentionPreview(session, dataSourceId), unanswered);
}

/** A mentioned data source's preview read again, for its mention's name to follow a rename. */
export function dataSourcePreviewAgain(session: Session, dataSourceId: string,
  unanswered?: PreviewUnanswered): Promise<DataSourceMentionPreviewResponse | null> {
  previews.delete(`${session.user.id}\u0000dataSource\u0000${dataSourceId}`);
  return dataSourcePreviewOnce(session, dataSourceId, unanswered);
}

/**
 * Reads the previews of names a screen shows (`read`, its reads refusing
 * when unanswered) once `wait` ms have passed, and gives them to `take`;
 * while the server doesn't answer, again once it may
 * (readUntilServerAnswers), what the screen showed standing meanwhile.
 * Returns the stop.
 */
export function readShownPreviews<T>(wait: number, read: () => Promise<T>, take: (previews: T) => void): () => void {
  let stop = () => {};
  const timer = window.setTimeout(() => { stop = readUntilServerAnswers(read, take); }, wait);
  return () => {
    window.clearTimeout(timer);
    stop();
  };
}
