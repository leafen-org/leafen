import type { Bootstrap, Session } from "@/types";
import { workspaceMembershipBounds } from "@/workspaceMembershipWire.generated";

const maxBootstrapSnapshotRestarts = workspaceMembershipBounds.bootstrapSnapshotRestarts;

export type BootstrapPageReader = (
  session: Session,
  cursor: string | null,
  signal?: AbortSignal,
) => Promise<Bootstrap>;

export type BootstrapCollectionProgress = {
  pageIndex: number;
  complete: boolean;
};

export type BootstrapSnapshotObserver = (
  snapshot: Bootstrap,
  progress: BootstrapCollectionProgress,
) => void | Promise<void>;

type BootstrapCollectionOptions = {
  session: Session;
  readPage: BootstrapPageReader;
  onSnapshot: BootstrapSnapshotObserver;
  onInvalidContinuation?: () => never;
  shouldContinue?: () => boolean;
  signal?: AbortSignal;
};

/** What the person may see as a part was read; a part that doesn't say counts its snapshot revision as that. */
function authorityOf(page: Bootstrap): string {
  return page.authorityRevision ?? `snapshot:${page.snapshotRevision}`;
}

/** The list without what it holds of these pages, which a later part read again. */
export function withoutPages(current: Bootstrap, pageIds: ReadonlySet<string>): Bootstrap {
  const kept = (pageId: string) => !pageIds.has(pageId);
  return {
    ...current,
    pages: current.pages.filter(({ id }) => kept(id)),
    pageFavorites: (current.pageFavorites ?? []).filter(({ pageId }) => kept(pageId)),
    databaseFavorites: (current.databaseFavorites ?? []).filter(({ pageId }) => kept(pageId)),
    inlineDatabases: (current.inlineDatabases ?? []).filter(({ pageId }) => kept(pageId)),
    ...(current.sharedPageIds ? { sharedPageIds: current.sharedPageIds.filter(kept) } : {}),
    ...(current.workspaceWidePageIds ? { workspaceWidePageIds: current.workspaceWidePageIds.filter(kept) } : {}),
  };
}

/** Adds a part to the list read so far, whose pages' IDs `listed` holds and takes the part's into. */
function appendBootstrapPage(current: Bootstrap | null, page: Bootstrap, listed: Set<string>): Bootstrap {
  // A page the list changed past the cursor comes again: as it was read last.
  const again = new Set(page.pages.filter(({ id }) => listed.has(id)).map(({ id }) => id));
  for (const { id } of page.pages) listed.add(id);
  if (!current) {
    return {
      ...page,
      workspaces: [...page.workspaces],
      pages: [...page.pages],
      pageFavorites: [...(page.pageFavorites ?? [])],
      databaseFavorites: [...(page.databaseFavorites ?? [])],
      inlineDatabases: [...(page.inlineDatabases ?? [])],
      ...(page.sharedPageIds ? { sharedPageIds: [...page.sharedPageIds] } : {}),
      ...(page.workspaceWidePageIds ? { workspaceWidePageIds: [...page.workspaceWidePageIds] } : {}),
      workspaceRefreshTokens: [...page.workspaceRefreshTokens],
    };
  }
  if (again.size) current = withoutPages(current, again);
  const sharedPageIds = current.sharedPageIds || page.sharedPageIds
    ? { sharedPageIds: [...(current.sharedPageIds ?? []), ...(page.sharedPageIds ?? [])] }
    : {};
  const workspaceWidePageIds = current.workspaceWidePageIds || page.workspaceWidePageIds
    ? { workspaceWidePageIds: [...(current.workspaceWidePageIds ?? []), ...(page.workspaceWidePageIds ?? [])] }
    : {};
  // A workspace read again shows as read last, and keeps the refresh token
  // first read for it: the list holds every change since that one, so its
  // change check reads it again for what changed while it was read.
  const workspaces = new Map(current.workspaces.map((workspace) => [workspace.id, workspace]));
  for (const workspace of page.workspaces) workspaces.set(workspace.id, workspace);
  const tokened = new Set(current.workspaceRefreshTokens.map(({ workspaceId }) => workspaceId));
  return {
    // The list holds every change up to its first part's revision, and the
    // page changes from its first part's number on (bootstrap_page_changes.go).
    snapshotRevision: current.snapshotRevision,
    ...(current.authorityRevision !== undefined ? { authorityRevision: current.authorityRevision } : {}),
    ...(current.pageChangeSeq !== undefined ? { pageChangeSeq: current.pageChangeSeq } : {}),
    workspaces: [...workspaces.values()],
    pages: [...current.pages, ...page.pages],
    pageFavorites: [...(current.pageFavorites ?? []), ...(page.pageFavorites ?? [])],
    // A batch names the favorite databases on its own pages.
    databaseFavorites: [...(current.databaseFavorites ?? []), ...(page.databaseFavorites ?? [])],
    // And the databases its pages hold.
    inlineDatabases: [...(current.inlineDatabases ?? []), ...(page.inlineDatabases ?? [])],
    ...sharedPageIds,
    ...workspaceWidePageIds,
    workspaceRefreshTokens: [
      ...current.workspaceRefreshTokens,
      ...page.workspaceRefreshTokens.filter(({ workspaceId }) => !tokened.has(workspaceId)),
    ],
    cursor: Math.max(current.cursor, page.cursor),
    nextCursor: page.nextCursor,
    hasMore: page.hasMore,
  };
}

function rejectInvalidContinuation(options: BootstrapCollectionOptions): never {
  if (options.onInvalidContinuation) return options.onInvalidContinuation();
  throw new Error("bootstrap continuation repeated");
}

/**
 * Reads the page list a part at a time. A change to its pages while it is
 * read doesn't start the read over: the parts after it are read as the list
 * is then, a page met twice (moved past the cursor) counts as last read, and
 * the list keeps its first part's revision and refresh tokens, so its change
 * check finds it behind and reads it again (2026-11-14: in a workspace of
 * 29,000 pages a page copy and a second writer moved the snapshot revision
 * between every two parts, every read began again and gave up after the
 * second time, and the sidebar showed no new page till the copy ended). A
 * change to what the person may see (bootstrap_reader_authority.go) does
 * start it over, a bounded number of times, so a list never mixes
 * workspaces read before they lost or gained them with ones read after.
 */
export async function collectBootstrapPages(
  options: BootstrapCollectionOptions,
): Promise<Bootstrap | null> {
  const seenCursors = new Set<string>();
  const listed = new Set<string>();
  const shouldContinue = options.shouldContinue ?? (() => true);
  let snapshot: Bootstrap | null = null;
  let cursor: string | null = null;
  let pageIndex = 0;
  let authority: string | null = null;
  let snapshotRestarts = 0;

  for (;;) {
    if (!shouldContinue()) return null;
    let page: Bootstrap;
    try {
      page = await options.readPage(options.session, cursor, options.signal);
    } catch (error) {
      if (!shouldContinue()) return null;
      throw error;
    }
    if (!shouldContinue()) return null;
    if (authority !== null && authorityOf(page) !== authority) {
      if (snapshotRestarts >= maxBootstrapSnapshotRestarts) {
        rejectInvalidContinuation(options);
      }
      snapshotRestarts += 1;
      snapshot = null;
      seenCursors.clear();
      listed.clear();
      cursor = null;
      pageIndex = 0;
      authority = null;
      continue;
    }
    authority = authorityOf(page);
    if (page.hasMore && (!page.nextCursor || seenCursors.has(page.nextCursor))) {
      rejectInvalidContinuation(options);
    }
    snapshot = appendBootstrapPage(snapshot, page, listed);
    await options.onSnapshot(snapshot, { pageIndex, complete: !page.hasMore });
    if (!shouldContinue()) return null;
    if (!page.hasMore) return snapshot;
    seenCursors.add(page.nextCursor!);
    cursor = page.nextCursor;
    pageIndex += 1;
  }
}
