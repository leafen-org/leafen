import { listPageHistoryVersions } from "@/lib/pageHistoryService";
import type { PageHistoryActorProfile, PageHistoryVersion } from "@/pageHistoryWire.generated";
import type { Session } from "@/types";
import { readWorkspaceActivities } from "@/workspaceActivityApi";
import type { WorkspaceActivityResponse } from "@/workspaceActivityWire.generated";
import type { PageVersionsRead } from "./pageUpdates";

// A page's updates tab reads the page's versions, and the workspace's
// updates of it, a page (a turn) at a time, and reads on while what it has
// does not fill its list: a burst of edits is listed only once the version
// before it is read (groupPageUpdates), so a burst longer than a page of
// versions (pageHistoryBounds.pageSize; 70 presses of a button made about
// 100 versions in a minute) left the tab saying it had no updates, and a
// turn of the workspace's updates holds only those the reader may see of
// the rows it went through. It stops after PAGE_UPDATE_READS reads, the
// burst still open then shown since the oldest version read ("cut").

/** The most pages of versions, or turns of the workspace's updates, the tab reads. */
export const PAGE_UPDATE_READS = 20;

/** A page's versions read so far, the newest first, and how much of its history they hold. */
export type PageVersionsReading = {
  versions: PageHistoryVersion[];
  actorProfiles: PageHistoryActorProfile[];
  read: PageVersionsRead;
};

/**
 * A page's versions, a page of them at a time: each reading holds every
 * version read so far, the last one its whole history or cut.
 */
export async function* readPageUpdateVersions(session: Session, pageId: string, signal: AbortSignal): AsyncGenerator<PageVersionsReading> {
  const versions: PageHistoryVersion[] = [];
  const actorProfiles: PageHistoryActorProfile[] = [];
  let cursor: string | undefined;
  for (let reads = 1; ; reads += 1) {
    const list = await listPageHistoryVersions(session, pageId, { ...(cursor ? { cursor } : {}), signal });
    versions.push(...list.versions);
    actorProfiles.push(...list.actorProfiles);
    const read: PageVersionsRead = !list.hasMore || !list.nextCursor ? "whole" : reads >= PAGE_UPDATE_READS ? "cut" : "more";
    yield { versions, actorProfiles, read };
    if (read !== "more" || !list.nextCursor) return;
    cursor = list.nextCursor;
  }
}

/**
 * The workspace's updates of one page or one database, `limit` at most,
 * read on turn by turn until there are that many or none are left.
 */
export async function readWorkspaceActivitiesUpTo(session: Session, workspaceId: string, signal: AbortSignal,
  options: { sourceId?: string; trashOf?: string; limit: number }): Promise<WorkspaceActivityResponse> {
  let result = await readWorkspaceActivities(session, workspaceId, "", signal, options);
  const items = [...result.items];
  const actorProfiles = [...result.actorProfiles];
  for (let reads = 1; items.length < options.limit && result.nextCursor && reads < PAGE_UPDATE_READS; reads += 1) {
    const cursor = result.nextCursor;
    result = await readWorkspaceActivities(session, workspaceId, cursor, signal, options);
    items.push(...result.items);
    actorProfiles.push(...result.actorProfiles);
    if (result.nextCursor === cursor) break;
  }
  return { ...result, items: items.slice(0, options.limit), actorProfiles };
}
