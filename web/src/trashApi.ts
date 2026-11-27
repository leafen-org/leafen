import type { Session } from "./types";
import {
  pagePermanentDeleteOperations,
  type DeletedPageRecoverResponse,
  type DeletedPagesResponse,
  type PagePermanentDeleteInput,
  type PagePermanentDeleteResponse,
  type TrashEmptyInput,
  type TrashEmptyResponse,
  type TrashLegalHolds,
} from "./pagePermanentDeleteWire.generated";

type JsonRequest = <T>(
  path: string,
  init?: RequestInit,
  session?: Session | null,
) => Promise<T>;

export function createTrashApi(request: JsonRequest) {
  const deletedPagePath = (operation: { path: string }, workspaceId: string, pageId = "") => operation.path
    .replace("{workspaceId}", encodeURIComponent(workspaceId))
    .replace("{pageId}", encodeURIComponent(pageId));
  return {
    listDeletedPages: (session: Session, workspaceId: string, cursor: string, signal?: AbortSignal) => request<DeletedPagesResponse>(
      deletedPagePath(pagePermanentDeleteOperations.listDeletedPages, workspaceId)
        + (cursor ? `?cursor=${encodeURIComponent(cursor)}` : ""),
      { signal },
      session,
    ),
    recoverDeletedPage: (session: Session, workspaceId: string, pageId: string) => request<DeletedPageRecoverResponse>(
      deletedPagePath(pagePermanentDeleteOperations.recoverDeletedPage, workspaceId, pageId),
      { method: pagePermanentDeleteOperations.recoverDeletedPage.method, body: "{}" },
      session,
    ),
    purgeDeletedPage: (session: Session, workspaceId: string, pageId: string, input: PagePermanentDeleteInput) => request<void>(
      deletedPagePath(pagePermanentDeleteOperations.purgeDeletedPage, workspaceId, pageId),
      { method: pagePermanentDeleteOperations.purgeDeletedPage.method, body: JSON.stringify(input) },
      session,
    ),
    deletePagePermanently: (
      session: Session,
      pageId: string,
      input: PagePermanentDeleteInput,
      signal?: AbortSignal,
    ) => request<PagePermanentDeleteResponse>(
      pagePermanentDeleteOperations.deletePagePermanently.path.replace(
        "{pageId}",
        encodeURIComponent(pageId),
      ),
      {
        method: pagePermanentDeleteOperations.deletePagePermanently.method,
        body: JSON.stringify(input),
        signal,
      },
      session,
    ),
    readTrashLegalHolds: (session: Session, workspaceId: string, signal?: AbortSignal) => request<TrashLegalHolds>(
      deletedPagePath(pagePermanentDeleteOperations.readTrashLegalHolds, workspaceId), { signal }, session,
    ),
    emptyTrash: (
      session: Session,
      workspaceId: string,
      input: TrashEmptyInput,
      signal?: AbortSignal,
    ) => request<TrashEmptyResponse>(
      pagePermanentDeleteOperations.emptyTrash.path.replace(
        "{workspaceId}",
        encodeURIComponent(workspaceId),
      ),
      {
        method: pagePermanentDeleteOperations.emptyTrash.method,
        body: JSON.stringify(input),
        signal,
      },
      session,
    ),
  };
}
