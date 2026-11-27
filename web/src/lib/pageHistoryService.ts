import { LeafenApiError, request } from "@/apiTransport";
import type { Session } from "@/types";
import {
  pageHistoryBounds,
  pageHistoryErrorCodes,
  pageHistoryOperations,
  type PageBlockLastEdit,
  type PageHistoryBookmark,
  type PageHistoryBookmarkListResponse,
  type PageHistoryBookmarkResponse,
  type PageHistoryChangesResponse,
  type PageHistoryListResponse,
  type PageHistoryRestoreRequest,
  type PageHistoryRestoreResponse,
  type PageHistoryRevertRequest,
  type PageHistoryRevertResponse,
  type PageHistorySnapshotResponse,
  type PageLockRecordListResponse,
  type PageHistoryDatabaseAtResponse,
} from "@/pageHistoryWire.generated";
import { newId } from "./ids";
import { subscribeSessionGenerationReset } from "./sessionGeneration";
import { readEveryWindow } from "@/lib/listWindows";

type PageHistoryReadOptions = {
  cursor?: string;
  signal?: AbortSignal;
  /** How many versions, the page's size at most. */
  limit?: number;
};

function pageHistoryPath(template: string, pageId: string, versionId?: string) {
  return template
    .replace("{pageId}", encodeURIComponent(pageId))
    .replace("{versionId}", encodeURIComponent(versionId ?? ""));
}

/** Who last edited some of a page's blocks and when, as the block menu's foot names them. */
export function readPageBlockLastEdit(
  session: Session,
  pageId: string,
  blockIds: readonly string[],
  signal?: AbortSignal,
) {
  const query = new URLSearchParams({ ids: blockIds.slice(0, pageHistoryBounds.blockLastEditIds).join(",") });
  return request<PageBlockLastEdit>(
    `${pageHistoryPath(pageHistoryOperations.blockLastEdit.path, pageId)}?${query}`,
    { method: pageHistoryOperations.blockLastEdit.method, signal },
    session,
  );
}

export function listPageHistoryVersions(
  session: Session,
  pageId: string,
  options: PageHistoryReadOptions = {},
) {
  const query = new URLSearchParams({ limit: String(options.limit ?? pageHistoryBounds.pageSize) });
  if (options.cursor) query.set("cursor", options.cursor);
  return request<PageHistoryListResponse>(
    `${pageHistoryPath(pageHistoryOperations.list.path, pageId)}?${query}`,
    { method: pageHistoryOperations.list.method, signal: options.signal },
    session,
  );
}

/** A page's latest locks and unlocks for everyone, the newest first: its own, or its inline database's when named. */
export function listPageLockRecords(
  session: Session,
  pageId: string,
  options: { blockId?: string; signal?: AbortSignal } = {},
) {
  const query = new URLSearchParams({ limit: String(pageHistoryBounds.lockRecordPageSize) });
  if (options.blockId) query.set("blockId", options.blockId);
  return request<PageLockRecordListResponse>(
    `${pageHistoryPath(pageHistoryOperations.lockRecords.path, pageId)}?${query}`,
    { method: pageHistoryOperations.lockRecords.method, signal: options.signal },
    session,
  );
}

/** An inline database's table as its page held it at a time: its version history's preview. */
export function readPageHistoryDatabaseAt(session: Session, pageId: string, blockId: string, at: number, signal?: AbortSignal) {
  const path = pageHistoryPath(pageHistoryOperations.databaseAt.path, pageId).replace("{blockId}", encodeURIComponent(blockId));
  return request<PageHistoryDatabaseAtResponse>(`${path}?${new URLSearchParams({ at: String(at) })}`,
    { method: pageHistoryOperations.databaseAt.method, signal }, session);
}

export function readPageHistorySnapshot(
  session: Session,
  pageId: string,
  versionId: string,
  options: PageHistoryReadOptions = {},
) {
  const query = new URLSearchParams({ limit: String(pageHistoryBounds.blockPageSize) });
  if (options.cursor) query.set("cursor", options.cursor);
  return request<PageHistorySnapshotResponse>(
    `${pageHistoryPath(pageHistoryOperations.snapshot.path, pageId, versionId)}?${query}`,
    { method: pageHistoryOperations.snapshot.method, signal: options.signal },
    session,
  );
}

/**
 * The changes from one version through a later one, newest first, each run
 * of one person's edits to one thing at its latest (page_history_changes.go):
 * the first page of them, which covers a burst of edits.
 */
export function readPageHistoryChangesSince(
  session: Session,
  pageId: string,
  versionId: string,
  sinceId: string,
  signal?: AbortSignal,
) {
  const query = new URLSearchParams({ since: sinceId, limit: String(pageHistoryBounds.changePageSize) });
  return request<PageHistoryChangesResponse>(
    `${pageHistoryPath(pageHistoryOperations.changes.path, pageId, versionId)}?${query}`,
    { method: pageHistoryOperations.changes.method, signal },
    session,
  );
}

/** What a full-page database's restore leaves as it is now. */
export type PageHistoryDatabaseKeeps = Pick<PageHistoryRestoreRequest, "keepDatabaseProperties" | "keepRowValues" | "keepRowNames">;

export function restorePageHistoryVersion(
  session: Session,
  pageId: string,
  versionId: string,
  input: PageHistoryRestoreRequest,
  signal?: AbortSignal,
) {
  return request<PageHistoryRestoreResponse>(
    pageHistoryPath(pageHistoryOperations.restore.path, pageId, versionId),
    {
      method: pageHistoryOperations.restore.method,
      body: JSON.stringify(input),
      signal,
    },
    session,
  );
}

/**
 * Undoes changes from a version on: with connection, every change the
 * version's connection made to the page from it on, keeping what others
 * changed afterwards. Someone changed the page since its history was read —
 * or the server stored typing its room still held, first (page_history_revert.go):
 * as the history's restore, the revert goes ahead over the page as it is now,
 * once.
 */
export async function revertPageHistoryChanges(
  session: Session,
  pageId: string,
  versionId: string,
  input: Omit<PageHistoryRevertRequest, "requestId">,
) {
  const revertAt = (baseRevision: number) => request<PageHistoryRevertResponse>(
    pageHistoryPath(pageHistoryOperations.revert.path, pageId, versionId),
    { method: pageHistoryOperations.revert.method, body: JSON.stringify({ ...input, baseRevision, requestId: newId() }) },
    session,
  );
  try {
    return await revertAt(input.baseRevision);
  } catch (caught) {
    if (!(caught instanceof LeafenApiError) || caught.code !== pageHistoryErrorCodes.revisionChanged) throw caught;
    const latest = await listPageHistoryVersions(session, pageId, { limit: 1 });
    return revertAt(latest.snapshotFrontier);
  }
}

function bookmarkPath(template: string, pageId: string, bookmarkId = "") {
  return template.replace("{pageId}", encodeURIComponent(pageId)).replace("{bookmarkId}", encodeURIComponent(bookmarkId));
}

/** The page's bookmarked versions, newest version first: all of them, a window at a time. */
export async function listPageHistoryBookmarks(session: Session, pageId: string, signal?: AbortSignal) {
  return readEveryWindow(async (cursor) => {
    const address = bookmarkPath(pageHistoryOperations.bookmarkList.path, pageId);
    const page = await request<PageHistoryBookmarkListResponse>(
      cursor ? `${address}?cursor=${encodeURIComponent(cursor)}` : address,
      { method: pageHistoryOperations.bookmarkList.method, signal },
      session,
    );
    return { items: page.bookmarks, nextCursor: page.nextCursor };
  }, signal);
}

/** Bookmarks a version under a name; a version already bookmarked keeps its bookmark. */
export async function createPageHistoryBookmark(session: Session, pageId: string, versionId: string, name: string) {
  const { bookmark } = await request<PageHistoryBookmarkResponse>(
    bookmarkPath(pageHistoryOperations.bookmarkCreate.path, pageId),
    { method: pageHistoryOperations.bookmarkCreate.method, body: JSON.stringify({ versionId, name }) },
    session,
  );
  return bookmark;
}

export async function renamePageHistoryBookmark(session: Session, bookmark: PageHistoryBookmark, name: string) {
  const { bookmark: renamed } = await request<PageHistoryBookmarkResponse>(
    bookmarkPath(pageHistoryOperations.bookmarkUpdate.path, bookmark.pageId, bookmark.id),
    { method: pageHistoryOperations.bookmarkUpdate.method, body: JSON.stringify({ name }) },
    session,
  );
  return renamed;
}

/** Takes the bookmark away; its version stays in the history. */
export async function deletePageHistoryBookmark(session: Session, bookmark: PageHistoryBookmark) {
  await request<void>(
    bookmarkPath(pageHistoryOperations.bookmarkDelete.path, bookmark.pageId, bookmark.id),
    { method: pageHistoryOperations.bookmarkDelete.method },
    session,
  );
}

function sessionIdentity(session: Session) {
  return session.sessionId ?? `${session.user.id}:${session.token}`;
}

// The service is the dialog's for one page and one signed-in session; it
// reads the session's latest copy (its token may be renewed) for each request,
// so a new copy of the same session doesn't make a new service read again.
export function createPageHistoryService(sessionOf: Session | (() => Session), pageId: string) {
  const currentSession = typeof sessionOf === "function" ? sessionOf : () => sessionOf;
  const ownerSessionIdentity = sessionIdentity(currentSession());
  let sessionGeneration = 0;
  let requestGeneration = 0;
  let disposed = false;
  let versionsController: AbortController | null = null;
  let snapshotController: AbortController | null = null;
  let restoreController: AbortController | null = null;
  let versionsInflight: { key: string; promise: Promise<PageHistoryListResponse | null> } | null = null;
  let snapshotInflight: { key: string; promise: Promise<PageHistorySnapshotResponse | null> } | null = null;
  let restoreInflight: { key: string; promise: Promise<PageHistoryRestoreResponse | null> } | null = null;
  let versionsGeneration = 0;
  let snapshotGeneration = 0;
  let restoreGeneration = 0;
  let snapshotFrontier: number | null = null;
  let snapshotVersionId = "";
  let snapshotVersionFrontier: number | null = null;
  let pendingRestore: { key: string; input: PageHistoryRestoreRequest } | null = null;

  const nextGeneration = () => {
    requestGeneration += 1;
    return requestGeneration;
  };
  const ownsSession = (generation: number) => (
    !disposed
    && sessionGeneration === generation
    && ownerSessionIdentity === sessionIdentity(currentSession())
  );
  const cancelReads = () => {
    versionsController?.abort();
    snapshotController?.abort();
    versionsController = null;
    snapshotController = null;
    versionsInflight = null;
    snapshotInflight = null;
  };
  const unsubscribe = subscribeSessionGenerationReset(() => {
    sessionGeneration += 1;
    nextGeneration();
    cancelReads();
    restoreController?.abort();
    restoreController = null;
    restoreInflight = null;
  });

  function list(cursor?: string) {
    const key = cursor ?? "initial";
    if (versionsInflight?.key === key) return versionsInflight.promise;
    const ownerGeneration = sessionGeneration;
    const generation = nextGeneration();
    versionsGeneration = generation;
    versionsController?.abort();
    const controller = new AbortController();
    versionsController = controller;
    if (!cursor) snapshotFrontier = null;
    const isCurrent = () => (
      ownsSession(ownerGeneration)
      && !controller.signal.aborted
      && versionsGeneration === generation
    );
    const promise = listPageHistoryVersions(currentSession(), pageId, { cursor, signal: controller.signal })
      .then((result) => {
        if (!isCurrent()) return null;
        if (snapshotFrontier !== null && result.snapshotFrontier !== snapshotFrontier) {
          throw new Error("Page history continuation changed snapshotFrontier.");
        }
        snapshotFrontier = result.snapshotFrontier;
        return result;
      })
      .finally(() => {
        if (versionsInflight?.promise === promise) versionsInflight = null;
        if (versionsController === controller) versionsController = null;
      });
    versionsInflight = { key, promise };
    return promise;
  }

  function snapshot(versionId: string, cursor?: string) {
    const key = `${versionId}\u0000${cursor ?? "initial"}`;
    if (snapshotInflight?.key === key) return snapshotInflight.promise;
    const ownerGeneration = sessionGeneration;
    const generation = nextGeneration();
    snapshotGeneration = generation;
    snapshotController?.abort();
    const controller = new AbortController();
    snapshotController = controller;
    if (!cursor || snapshotVersionId !== versionId) {
      snapshotVersionId = versionId;
      snapshotVersionFrontier = null;
    }
    const isCurrent = () => (
      ownsSession(ownerGeneration)
      && !controller.signal.aborted
      && snapshotGeneration === generation
      && snapshotVersionId === versionId
    );
    const promise = readPageHistorySnapshot(
      currentSession(), pageId, versionId, { cursor, signal: controller.signal },
    ).then((result) => {
      if (!isCurrent()) return null;
      if (
        result.versionId !== versionId
        || (snapshotVersionFrontier !== null && result.snapshotFrontier !== snapshotVersionFrontier)
      ) {
        throw new Error("Page history preview changed snapshotFrontier.");
      }
      snapshotVersionFrontier = result.snapshotFrontier;
      return result;
    }).finally(() => {
      if (snapshotInflight?.promise === promise) snapshotInflight = null;
      if (snapshotController === controller) snapshotController = null;
    });
    snapshotInflight = { key, promise };
    return promise;
  }

  // A version's first blocks read apart from the preview, each reader
  // keeping its own one request: the entry a preview is compared with, and
  // the current version whose databases a restore keeps.
  function apartReader() {
    let held: AbortController | null = null;
    const read = (versionId: string) => {
      const ownerGeneration = sessionGeneration;
      held?.abort();
      const controller = new AbortController();
      held = controller;
      const query = new URLSearchParams({ limit: String(pageHistoryBounds.blockPageSize) });
      return request<PageHistorySnapshotResponse>(
        `${pageHistoryPath(pageHistoryOperations.snapshot.path, pageId, versionId)}?${query}`,
        { method: pageHistoryOperations.snapshot.method, signal: controller.signal },
        currentSession(),
      ).then((result) => (ownsSession(ownerGeneration) && !controller.signal.aborted && result.versionId === versionId ? result : null))
        .finally(() => {
          if (held === controller) held = null;
        });
    };
    return { read, abort: () => held?.abort() };
  }
  const compared = apartReader();
  const current = apartReader();

  // An entry's changes read apart from the preview, the latest asked for alone.
  let changesController: AbortController | null = null;
  function changes(versionId: string, sinceId: string, cursor?: string) {
    const ownerGeneration = sessionGeneration;
    changesController?.abort();
    const controller = new AbortController();
    changesController = controller;
    const query = new URLSearchParams({ since: sinceId, limit: String(pageHistoryBounds.changePageSize) });
    if (cursor) query.set("cursor", cursor);
    return request<PageHistoryChangesResponse>(
      `${pageHistoryPath(pageHistoryOperations.changes.path, pageId, versionId)}?${query}`,
      { method: pageHistoryOperations.changes.method, signal: controller.signal },
      currentSession(),
    ).then((result) => (ownsSession(ownerGeneration) && !controller.signal.aborted && result.versionId === versionId ? result : null))
      .finally(() => {
        if (changesController === controller) changesController = null;
      });
  }

  function restore(versionId: string, baseRevision: number, restoreDatabases = false, keeps: PageHistoryDatabaseKeeps = {}) {
    const key = `${versionId}\u0000${baseRevision}\u0000${restoreDatabases}\u0000${JSON.stringify(keeps)}`;
    if (restoreInflight) {
      if (restoreInflight.key === key) return restoreInflight.promise;
      return Promise.reject(new Error("A page history restore is already in progress."));
    }
    const ownerGeneration = sessionGeneration;
    const generation = nextGeneration();
    restoreGeneration = generation;
    const controller = new AbortController();
    restoreController = controller;
    const input = pendingRestore?.key === key
      ? pendingRestore.input
      : { requestId: `page-history-restore:${newId()}`, baseRevision, ...(restoreDatabases ? { restoreDatabases, ...keeps } : {}) };
    pendingRestore = { key, input };
    const promise = restorePageHistoryVersion(
      currentSession(), pageId, versionId, input, controller.signal,
    ).then((result) => {
      if (
        !ownsSession(ownerGeneration)
        || controller.signal.aborted
        || restoreGeneration !== generation
      ) return null;
      pendingRestore = null;
      snapshotFrontier = null;
      snapshotVersionFrontier = null;
      return result;
    }).catch((error) => {
      if (error instanceof LeafenApiError) pendingRestore = null;
      throw error;
    }).finally(() => {
      if (restoreInflight?.promise === promise) restoreInflight = null;
      if (restoreController === controller) restoreController = null;
    });
    restoreInflight = { key, promise };
    return promise;
  }

  return Object.freeze({
    listPageHistoryVersions: list,
    readPageHistorySnapshot: snapshot,
    /** A version's first blocks, to compare a preview with; it leaves the preview's read alone. */
    readComparedSnapshot: compared.read,
    /** The current version's first blocks, whose databases a restore keeps as they are; it leaves the other reads alone. */
    readCurrentSnapshot: current.read,
    /** The changes of an entry's versions, from its oldest through its latest, newest first. */
    readPageHistoryChanges: changes,
    restorePageHistoryVersion: restore,
    currentSnapshotFrontier: () => snapshotFrontier,
    dispose() {
      if (disposed) return;
      disposed = true;
      sessionGeneration += 1;
      nextGeneration();
      unsubscribe();
      cancelReads();
      compared.abort();
      current.abort();
      changesController?.abort();
      restoreController?.abort();
      restoreController = null;
      restoreInflight = null;
    },
  });
}

export type PageHistoryService = ReturnType<typeof createPageHistoryService>;
