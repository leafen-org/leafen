import { request, requestBinaryResponse } from "@/apiTransport";
import type {
  AdministratorWorkspaceActionResult,
  AdministratorWorkspaceContentLog,
  AdministratorWorkspaceFileList,
  AdministratorWorkspacePageContent,
  AdministratorWorkspacePageList,
  AdministratorWorkspacePublicKind,
  AdministratorWorkspacePublicList,
  AdministratorWorkspaceStorageList,
} from "@/administratorWire.generated";
import { newId } from "@/lib/ids";
import type { Session } from "@/types";

/**
 * A server administrator's view of a workspace (Leafen's own): its pages read only, its
 * files, what it made public, and what administrators did in it.
 */
const base = (workspaceId: string) => `/admin/workspaces/${encodeURIComponent(workspaceId)}`;

function withQuery(path: string, query: Record<string, string | undefined>) {
  const params = new URLSearchParams();
  for (const [key, value] of Object.entries(query)) if (value) params.set(key, value);
  return params.size ? `${path}?${params}` : path;
}

export const administratorWorkspaceContent = {
  pages: (session: Session, workspaceId: string, input: { parentId?: string; cursor?: string } = {}, signal?: AbortSignal) =>
    request<AdministratorWorkspacePageList>(withQuery(`${base(workspaceId)}/pages`, input), { signal }, session),
  page: (session: Session, workspaceId: string, pageId: string, signal?: AbortSignal) =>
    request<AdministratorWorkspacePageContent>(`${base(workspaceId)}/pages/${encodeURIComponent(pageId)}`, { signal }, session),
  trashPage: (session: Session, workspaceId: string, pageId: string) =>
    request<AdministratorWorkspaceActionResult>(`${base(workspaceId)}/pages/${encodeURIComponent(pageId)}/trash`,
      { method: "POST", body: "{}" }, session),
  /** Gives the workspace's owner full access to a private page whose person left. */
  takeOverPage: (session: Session, workspaceId: string, pageId: string) =>
    request<AdministratorWorkspaceActionResult>(`${base(workspaceId)}/pages/${encodeURIComponent(pageId)}/take-over`,
      { method: "POST", body: "{}" }, session),
  /** A deletion that takes longer answers "deleting" and goes on with the same request ID. */
  deletePage: (session: Session, workspaceId: string, pageId: string, requestId = newId()) =>
    request<AdministratorWorkspaceActionResult>(`${base(workspaceId)}/pages/${encodeURIComponent(pageId)}/delete-permanently`,
      { method: "POST", body: JSON.stringify({ requestId }) }, session),
  files: (session: Session, workspaceId: string, cursor?: string, signal?: AbortSignal) =>
    request<AdministratorWorkspaceFileList>(withQuery(`${base(workspaceId)}/files`, { cursor }), { signal }, session),
  storage: (session: Session, workspaceId: string, cursor?: string, signal?: AbortSignal) =>
    request<AdministratorWorkspaceStorageList>(withQuery(`${base(workspaceId)}/storage`, { cursor }), { signal }, session),
  downloadFile: (session: Session, workspaceId: string, fileId: string, signal?: AbortSignal) =>
    requestBinaryResponse(`${base(workspaceId)}/files/${encodeURIComponent(fileId)}/download`, session, { signal }),
  deleteFile: (session: Session, workspaceId: string, fileId: string) =>
    request<AdministratorWorkspaceActionResult>(`${base(workspaceId)}/files/${encodeURIComponent(fileId)}/delete`,
      { method: "POST", body: JSON.stringify({ requestId: newId() }) }, session),
  publicItems: (session: Session, workspaceId: string, signal?: AbortSignal) =>
    request<AdministratorWorkspacePublicList>(`${base(workspaceId)}/public`, { signal }, session),
  disablePublic: (session: Session, workspaceId: string, kind: AdministratorWorkspacePublicKind, itemId: string) =>
    request<AdministratorWorkspaceActionResult>(
      `${base(workspaceId)}/public/${encodeURIComponent(kind)}/${encodeURIComponent(itemId)}/disable`,
      { method: "POST", body: "{}" }, session),
  log: (session: Session, workspaceId: string, cursor?: string, signal?: AbortSignal) =>
    request<AdministratorWorkspaceContentLog>(withQuery(`${base(workspaceId)}/log`, { cursor }), { signal }, session),
  /** What server administrators did in a workspace, as its owner sees it. */
  ownerLog: (session: Session, workspaceId: string, cursor?: string, signal?: AbortSignal) =>
    request<AdministratorWorkspaceContentLog>(
      withQuery(`/workspaces/${encodeURIComponent(workspaceId)}/server-admin-log`, { cursor }), { signal }, session),
};
