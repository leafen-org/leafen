import { request } from "@/apiTransport";
import { derivedPageId } from "@/lib/ids";
import { followPageDatabaseCopies } from "@/lib/pageDatabaseCopies";
import { pageWritesSent } from "@/lib/pageKindChange";
import type { Session } from "@/types";
import {
  databaseRowPageBounds,
  databaseRowPageOperations,
  type DatabaseRowPageActionInput,
  type DatabaseRowPageActionResponse,
  type DatabaseRowPageArchiveInput,
  type DatabaseRowPagePresentationInput,
  type DatabaseRowPageFavoriteInput,
  type DatabaseRowPageFromPageInput,
  type DatabaseRowPageLockForMeInput,
  type DatabaseRowPageLockInput,
  type DatabaseRowPageLockState,
  type DatabaseRowPagePermissionInput,
  type DatabaseRowPagePermissions,
  type DatabaseRowPageSnapshot,
  type DatabaseRowPageValuesInput,
  type DatabaseRowPageWikiChangeInput,
  type DatabaseRowTrashListResponse,
} from "@/databaseRowPageWire.generated";

function rowPagePath(template: string, rowId: string) {
  return template.replace("{rowId}", encodeURIComponent(rowId));
}

export function readDatabaseRowPage(
  session: Session,
  rowId: string,
  options: { cursor?: string; limit?: number; signal?: AbortSignal; firstBlock?: boolean } = {},
) {
  const parameters = new URLSearchParams({
    limit: String(options.limit ?? databaseRowPageBounds.bodyPageSize),
  });
  if (options.cursor) parameters.set("cursor", options.cursor);
  // Only the page's first block and the first block inside it, what a card's
  // "페이지 콘텐츠" draws (cardPreviewLine).
  if (options.firstBlock) parameters.set("body", "first");
  return request<DatabaseRowPageSnapshot>(
    `${rowPagePath(databaseRowPageOperations.read.path, rowId)}?${parameters}`,
    { method: databaseRowPageOperations.read.method, signal: options.signal },
    session,
  );
}

/** A row's or template's page taken into or out of favorites, for the lists that show them. */
export const DATABASE_ROW_PAGE_FAVORITE_EVENT = "leafen:database-row-page-favorite";
export type DatabaseRowPageFavoriteChange = { rowId: string; favorite: boolean; containerPageId: string };

export async function setDatabaseRowPageFavorite(
  session: Session,
  rowId: string,
  input: DatabaseRowPageFavoriteInput,
  signal?: AbortSignal,
) {
  const response = await request<DatabaseRowPageActionResponse>(
    rowPagePath(databaseRowPageOperations.favorite.path, rowId),
    {
      method: databaseRowPageOperations.favorite.method,
      body: JSON.stringify(input),
      signal,
    },
    session,
  );
  window.dispatchEvent(new CustomEvent<DatabaseRowPageFavoriteChange>(DATABASE_ROW_PAGE_FAVORITE_EVENT,
    { detail: { rowId, favorite: input.favorite, containerPageId: response.containerPageId } }));
  return response;
}

export function setDatabaseRowPageArchive(
  session: Session,
  rowId: string,
  input: DatabaseRowPageArchiveInput,
  signal?: AbortSignal,
) {
  return request<DatabaseRowPageActionResponse>(
    rowPagePath(databaseRowPageOperations.archive.path, rowId),
    {
      method: databaseRowPageOperations.archive.method,
      body: JSON.stringify(input),
      signal,
    },
    session,
  );
}

// One row's changes of look go one after another, each on the revision the one
// before it left: a cover put on right after an icon wasn't to be refused for
// the revision read before the icon saved (Notion keeps both, seen 2026-11-03).
const lookWrites = new Map<string, { last: Promise<unknown>; revision: number }>();

/** Sets a database page's icon or cover, as a page's are set. */
export function setDatabaseRowPagePresentation(
  session: Session,
  rowId: string,
  input: DatabaseRowPagePresentationInput,
  signal?: AbortSignal,
) {
  const writes = lookWrites.get(rowId) ?? { last: Promise.resolve(), revision: 0 };
  const write = writes.last.catch(() => undefined).then(() => request<DatabaseRowPageActionResponse>(
    rowPagePath(databaseRowPageOperations.presentation.path, rowId),
    {
      method: databaseRowPageOperations.presentation.method,
      body: JSON.stringify({ ...input, expectedAuthorityRevision: Math.max(input.expectedAuthorityRevision, writes.revision) }),
      signal,
    },
    session,
  )).then((response) => { writes.revision = response.authorityRevision; return response; });
  writes.last = write;
  lookWrites.set(rowId, writes);
  const done = () => { if (writes.last === write) lookWrites.delete(rowId); };
  write.then(done, done);
  return write;
}

/** Locks a database page for everyone, or unlocks it. */
export function setDatabaseRowPageLock(session: Session, rowId: string, input: DatabaseRowPageLockInput) {
  return request<DatabaseRowPageActionResponse>(
    rowPagePath(databaseRowPageOperations.lock.path, rowId),
    { method: databaseRowPageOperations.lock.method, body: JSON.stringify(input) },
    session,
  );
}

/** Unlocks a locked database page for the person asking, or locks it for them again. */
export function setDatabaseRowPageLockForMe(session: Session, rowId: string, input: DatabaseRowPageLockForMeInput) {
  return request<DatabaseRowPageLockState>(
    rowPagePath(databaseRowPageOperations.lockForMe.path, rowId),
    { method: databaseRowPageOperations.lockForMe.method, body: JSON.stringify(input) },
    session,
  );
}

function rowPageAction(
  session: Session,
  rowId: string,
  operation: { method: string; path: string },
  input: DatabaseRowPageActionInput,
  signal?: AbortSignal,
) {
  return request<DatabaseRowPageActionResponse>(
    rowPagePath(operation.path, rowId),
    { method: operation.method, body: JSON.stringify(input), signal },
    session,
  );
}

export async function duplicateDatabaseRowPage(
  session: Session,
  rowId: string,
  input: DatabaseRowPageActionInput,
  signal?: AbortSignal,
) {
  const copied = await rowPageAction(session, rowId, databaseRowPageOperations.duplicate, input, signal);
  // Its body's own databases come after it (lib/pageDatabaseCopies.ts).
  followPageDatabaseCopies(session, copied.rowId, copied.copyingDatabases === true);
  return copied;
}

/**
 * The ID the server gives the copy a person's duplicate request makes, known
 * before it is sent: as the server names it (databaseRowPageDuplicateRowID,
 * database_row_page_actions.go), so a retry makes the same copy.
 */
export async function duplicateRowCopyId(userId: string, rowId: string, requestId: string) {
  return derivedPageId("duplicate", userId, rowId, requestId.trim());
}

export function moveDatabaseRowPage(
  session: Session,
  rowId: string,
  input: DatabaseRowPageActionInput,
  signal?: AbortSignal,
) {
  return rowPageAction(session, rowId, databaseRowPageOperations.move, input, signal);
}

/**
 * Moves a page into a database as its row; it keeps its ID and content. The
 * page's title typed just before goes first, as a page's (lib/pageKindChange.ts).
 */
export async function movePageIntoDatabase(
  session: Session,
  pageId: string,
  input: DatabaseRowPageFromPageInput,
  signal?: AbortSignal,
) {
  await pageWritesSent();
  signal?.throwIfAborted();
  return request<DatabaseRowPageActionResponse>(
    databaseRowPageOperations.fromPage.path.replace("{pageId}", encodeURIComponent(pageId)),
    { method: databaseRowPageOperations.fromPage.method, body: JSON.stringify(input), signal },
    session,
  );
}

/** Changes a wiki's page from its page menu as the wiki database's page. */
export function changeWikiPage(
  session: Session,
  pageId: string,
  input: DatabaseRowPageWikiChangeInput,
  signal?: AbortSignal,
) {
  return request<DatabaseRowPageActionResponse>(
    databaseRowPageOperations.wikiChange.path.replace("{pageId}", encodeURIComponent(pageId)),
    { method: databaseRowPageOperations.wikiChange.method, body: JSON.stringify(input), signal },
    session,
  );
}

export function trashDatabaseRowPage(
  session: Session,
  rowId: string,
  input: DatabaseRowPageActionInput,
  signal?: AbortSignal,
) {
  return rowPageAction(session, rowId, databaseRowPageOperations.trash, input, signal);
}

export function restoreDatabaseRowPage(
  session: Session,
  rowId: string,
  input: DatabaseRowPageActionInput,
  signal?: AbortSignal,
) {
  return rowPageAction(session, rowId, databaseRowPageOperations.restore, input, signal);
}

export function deleteDatabaseRowPagePermanently(
  session: Session,
  rowId: string,
  input: DatabaseRowPageActionInput,
  signal?: AbortSignal,
) {
  return rowPageAction(session, rowId, databaseRowPageOperations.deletePermanently, input, signal);
}

/** Lists the database rows in the workspace's trash, newest first, a window from the cursor on. */
export function readDatabaseRowTrash(session: Session, workspaceId: string, signal?: AbortSignal, cursor?: string) {
  const address = databaseRowPageOperations.trashList.path.replace("{workspaceId}", encodeURIComponent(workspaceId));
  return request<DatabaseRowTrashListResponse>(
    cursor ? `${address}?cursor=${encodeURIComponent(cursor)}` : address,
    { method: databaseRowPageOperations.trashList.method, signal },
    session,
  );
}

/**
 * Reads who a row page is shared with, for someone who can manage it: a
 * window of shares from the cursor of the window before.
 */
export function readDatabaseRowPagePermissions(session: Session, rowId: string, signal?: AbortSignal, cursor?: string) {
  const address = rowPagePath(databaseRowPageOperations.permissions.path, rowId);
  return request<DatabaseRowPagePermissions>(
    cursor ? `${address}?cursor=${encodeURIComponent(cursor)}` : address,
    { method: databaseRowPageOperations.permissions.method, signal },
    session,
  );
}

/** Shares a row page with someone, or changes or removes a share. */
export function mutateDatabaseRowPagePermission(
  session: Session,
  rowId: string,
  input: DatabaseRowPagePermissionInput,
  signal?: AbortSignal,
) {
  return request<DatabaseRowPagePermissions>(
    rowPagePath(databaseRowPageOperations.mutatePermission.path, rowId),
    { method: databaseRowPageOperations.mutatePermission.method, body: JSON.stringify(input), signal },
    session,
  );
}

/**
 * Writes a row page's values through the row page's own authority. A row's
 * values a person writes go kept in the outbox (rowValuesKept.ts), which
 * sends them again as the page goes with its page's other changes
 * (nativePageMutationLeaving.ts).
 */
export function writeDatabaseRowPageValues(
  session: Session,
  rowId: string,
  input: DatabaseRowPageValuesInput,
  signal?: AbortSignal,
) {
  return request<DatabaseRowPageActionResponse>(
    rowPagePath(databaseRowPageOperations.writeValues.path, rowId),
    { method: databaseRowPageOperations.writeValues.method, body: JSON.stringify(input), signal },
    session,
  );
}
