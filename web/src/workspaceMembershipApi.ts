import { request } from "./apiTransport";
import type { Session } from "./types";
import {
  workspaceMembershipOperations,
  workspaceMemberRoles,
  type WorkspaceChangeProbeResponse,
  type WorkspaceCreateInput,
  type WorkspaceCreateResponse,
  type WorkspaceMembership,
  type WorkspaceMembershipInput,
  type WorkspaceMembershipRevocation,
  type WorkspaceOwnerTransferInput,
  type WorkspaceOwnerTransferResponse,
  type WorkspaceMemberAddAcceptedResponse,
  type WorkspaceMemberAddInput,
  type WorkspaceGuestDirectoryResponse,
  type WorkspaceGuestPagesResponse,
  type WorkspaceGuestRemoveResponse,
  type WorkspaceMemberCandidatesResponse,
  type WorkspaceMemberDirectoryResponse,
  type WorkspaceMemberInvitationCancelResponse,
  type WorkspaceSettingsInput,
  type WorkspaceSettingsMutationResponse,
  type WorkspaceDeletionInput,
  type WorkspaceDeletionResponse,
  type WorkspaceDeletionPreview,
  type SidebarLayout,
  type PageVisit,
  type PageVisits,
  type RecentEdits,
  type LibraryPages,
  type PageVisitsRemoved,
  type WorkspaceDocumentCreated,
  type WorkspaceDocumentCreateInput,
  type WorkspaceDocumentKind,
  type WorkspaceDocuments,
  type WorkspaceDocumentSort,
} from "./workspaceMembershipWire.generated";

/**
 * A workspace permission group as 설정 → 사람 → 그룹 lists it: the list also
 * names its owners and counts the teamspaces it is in that the reader sees.
 */
export type WorkspacePermissionGroup = {
  id: string; name: string; memberCount: number; createdAt: number;
  /** Its emoji, "" for none (an older server sends none). */
  icon?: string;
  owners?: string[]; teamspaceCount?: number;
  /** In the list only: whether the reader is in it, and whether it holds a restricted member (an older server sends neither). */
  readerMember?: boolean; hasRestrictedMember?: boolean;
};
export type WorkspacePermissionGroupDetail = WorkspacePermissionGroup & {
  members: { userId: string }[];
  /** The teamspaces the group is in that the reader sees (an older server sends none). */
  teamspaces?: { id: string; name: string; icon: string }[];
};

export function createWorkspaceMembershipApi() {
  const sidebarLayoutPath = (workspaceId: string) =>
    workspaceMembershipOperations.readSidebarLayout.path.replace("{workspaceId}", encodeURIComponent(workspaceId));
  return {
    /**
     * The reader's sidebar sections in a workspace: their order, sort and
     * count. The sidebar and a database's view tabs, which open a moment
     * apart, share the one read out (lib/sameTaskReads.ts).
     */
    readSidebarLayout: (session: Session, workspaceId: string, signal?: AbortSignal) => request<SidebarLayout>(
      sidebarLayoutPath(workspaceId), { method: workspaceMembershipOperations.readSidebarLayout.method, signal }, session,
      { shareWhileOut: true },
    ),
    saveSidebarLayout: (session: Session, workspaceId: string, layout: SidebarLayout) => request<SidebarLayout>(
      sidebarLayoutPath(workspaceId),
      { method: workspaceMembershipOperations.saveSidebarLayout.method, body: JSON.stringify(layout) },
      session,
    ),
    /** Keeps the reader's visit to a page, for their library. */
    recordPageVisit: (session: Session, pageId: string) => request<PageVisit>(
      workspaceMembershipOperations.recordPageVisit.path.replace("{pageId}", encodeURIComponent(pageId)),
      { method: workspaceMembershipOperations.recordPageVisit.method },
      session,
    ),
    /** The reader's latest page visits in a workspace, newest first. */
    readPageVisits: (session: Session, workspaceId: string, signal?: AbortSignal) => request<PageVisits>(
      workspaceMembershipOperations.readPageVisits.path.replace("{workspaceId}", encodeURIComponent(workspaceId)),
      { method: workspaceMembershipOperations.readPageVisits.method, signal },
      session,
    ),
    /** The pages the reader edited last, for the home's "최근 편집". */
    readRecentEdits: (session: Session, workspaceId: string, signal?: AbortSignal) => request<RecentEdits>(
      workspaceMembershipOperations.readRecentEdits.path.replace("{workspaceId}", encodeURIComponent(workspaceId)),
      { method: workspaceMembershipOperations.readRecentEdits.method, signal },
      session,
    ),
    /** Who made each page and when the reader last visited it, for the library's rows. */
    readLibraryPages: (session: Session, workspaceId: string, pageIds: readonly string[], signal?: AbortSignal) => request<LibraryPages>(
      workspaceMembershipOperations.readLibraryPages.path.replace("{workspaceId}", encodeURIComponent(workspaceId)),
      { method: workspaceMembershipOperations.readLibraryPages.method, body: JSON.stringify({ pageIds }), signal },
      session,
    ),
    /**
     * The document files on the pages the reader can read, for the library's
     * "문서": latest change or name first, by name or words, one kind at a time.
     */
    listWorkspaceDocuments: (session: Session, workspaceId: string, options: {
      query?: string; kinds?: readonly WorkspaceDocumentKind[]; sort?: WorkspaceDocumentSort; cursor?: string; limit?: number;
    }, signal?: AbortSignal) => {
      const search = new URLSearchParams();
      if (options.query) search.set("q", options.query);
      for (const kind of options.kinds ?? []) search.append("kind", kind);
      if (options.sort) search.set("sort", options.sort);
      if (options.cursor) search.set("cursor", options.cursor);
      if (options.limit) search.set("limit", String(options.limit));
      const path = workspaceMembershipOperations.listWorkspaceDocuments.path.replace("{workspaceId}", encodeURIComponent(workspaceId));
      return request<WorkspaceDocuments>(`${path}${search.size ? `?${search}` : ""}`,
        { method: workspaceMembershipOperations.listWorkspaceDocuments.method, signal }, session);
    },
    /** Starts a document from an app's blank file in a page, or in the reader's own "문서" page. */
    createWorkspaceDocument: (session: Session, workspaceId: string, input: WorkspaceDocumentCreateInput, signal?: AbortSignal) => request<WorkspaceDocumentCreated>(
      workspaceMembershipOperations.createWorkspaceDocument.path.replace("{workspaceId}", encodeURIComponent(workspaceId)),
      { method: workspaceMembershipOperations.createWorkspaceDocument.method, body: JSON.stringify(input), signal },
      session,
    ),
    /** Takes pages off the reader's own "최근" in a workspace. */
    removePageVisits: (session: Session, workspaceId: string, pageIds: readonly string[], signal?: AbortSignal) => request<PageVisitsRemoved>(
      workspaceMembershipOperations.removePageVisits.path.replace("{workspaceId}", encodeURIComponent(workspaceId)),
      { method: workspaceMembershipOperations.removePageVisits.method, body: JSON.stringify({ pageIds }), signal },
      session,
    ),
    /**
     * With `wait`, a member's check is held until another person changes the page list, up to 25 seconds;
     * with `pagesChangedSince`, it answers with the pages changed since when they say what changed.
     */
    probeWorkspaceChange: (
      session: Session,
      workspaceId: string,
      workspaceRefreshToken: string,
      { wait, pagesChangedSince, signal }: { wait?: boolean; pagesChangedSince?: number; signal?: AbortSignal } = {},
    ) => request<WorkspaceChangeProbeResponse>(
      workspaceMembershipOperations.changeProbe.path.replace(
        "{workspaceId}",
        encodeURIComponent(workspaceId),
      ),
      {
        method: workspaceMembershipOperations.changeProbe.method,
        body: JSON.stringify({ workspaceRefreshToken, ...(wait ? { wait } : {}), ...(pagesChangedSince ? { pagesChangedSince } : {}) }),
        signal,
      },
      session,
    ),
    createWorkspace: (session: Session, input: WorkspaceCreateInput, signal?: AbortSignal) =>
      request<WorkspaceCreateResponse>(
        "/workspaces",
        { method: "POST", body: JSON.stringify(input), signal },
        session,
      ),
    grantWorkspaceMember: (
      session: Session,
      workspaceId: string,
      userId: string,
      restricted = false,
    ) => {
      // Someone new joins as a member, or as a restricted member if asked.
      const input: WorkspaceMembershipInput = { role: workspaceMemberRoles.member, ...(restricted ? { restricted } : {}) };
      return request<WorkspaceMembership>(
        `/workspaces/${encodeURIComponent(workspaceId)}/members/${encodeURIComponent(userId)}`,
        { method: "PUT", body: JSON.stringify(input) },
        session,
      );
    },
    revokeWorkspaceMember: (
      session: Session,
      workspaceId: string,
      userId: string,
      signal?: AbortSignal,
    ) => request<WorkspaceMembershipRevocation>(
      `/workspaces/${encodeURIComponent(workspaceId)}/members/${encodeURIComponent(userId)}`,
      { method: "DELETE", signal },
      session,
    ),
    addWorkspaceMemberByEmail: (
      session: Session,
      workspaceId: string,
      input: WorkspaceMemberAddInput,
      signal?: AbortSignal,
    ) => request<WorkspaceMemberAddAcceptedResponse>(
      workspaceMembershipOperations.addMemberByEmail.path.replace(
        "{workspaceId}",
        encodeURIComponent(workspaceId),
      ),
      {
        method: workspaceMembershipOperations.addMemberByEmail.method,
        body: JSON.stringify(input),
        signal,
      },
      session,
    ),
    /** Cancels an invitation still waiting for its address to sign up. */
    cancelWorkspaceMemberInvitation: (
      session: Session,
      workspaceId: string,
      invitationId: string,
      signal?: AbortSignal,
    ) => request<WorkspaceMemberInvitationCancelResponse>(
      workspaceMembershipOperations.cancelMemberInvitation.path
        .replace("{workspaceId}", encodeURIComponent(workspaceId))
        .replace("{invitationId}", encodeURIComponent(invitationId)),
      { method: workspaceMembershipOperations.cancelMemberInvitation.method, signal },
      session,
    ),
    listWorkspaceMembers: (
      session: Session,
      workspaceId: string,
      cursor?: string,
      limit?: number,
      authorityRevision?: number,
      signal?: AbortSignal,
    ) => {
      const query = new URLSearchParams();
      if (cursor) query.set("cursor", cursor);
      if (limit !== undefined) query.set("limit", String(limit));
      if (authorityRevision !== undefined) {
        query.set("authorityRevision", String(authorityRevision));
      }
      const suffix = query.size > 0 ? `?${query}` : "";
      return request<WorkspaceMemberDirectoryResponse>(
        `${workspaceMembershipOperations.listMembers.path.replace(
          "{workspaceId}",
          encodeURIComponent(workspaceId),
        )}${suffix}`,
        {
          method: workspaceMembershipOperations.listMembers.method,
          signal,
        },
        session,
      );
    },
    /** A window of the workspace's guests from the cursor on. */
    listWorkspaceGuests: (session: Session, workspaceId: string, cursor = "", signal?: AbortSignal) =>
      request<WorkspaceGuestDirectoryResponse>(
        workspaceMembershipOperations.listGuests.path.replace("{workspaceId}", encodeURIComponent(workspaceId))
          + (cursor ? `?cursor=${encodeURIComponent(cursor)}` : ""),
        { method: workspaceMembershipOperations.listGuests.method, signal },
        session,
      ),
    /** The organization's people with no address to add by, found by name, whom the owner can add back. */
    listWorkspaceMemberCandidates: (session: Session, workspaceId: string, name: string, signal?: AbortSignal) =>
      request<WorkspaceMemberCandidatesResponse>(
        `${workspaceMembershipOperations.listMemberCandidates.path.replace("{workspaceId}", encodeURIComponent(workspaceId))}?${
          new URLSearchParams({ q: name })}`,
        { method: workspaceMembershipOperations.listMemberCandidates.method, signal },
        session,
      ),
    /** A window of the pages one guest can open, from the cursor on. */
    listWorkspaceGuestPages: (session: Session, workspaceId: string, guest: string, cursor = "", signal?: AbortSignal) =>
      request<WorkspaceGuestPagesResponse>(
        `${workspaceMembershipOperations.listGuestPages.path.replace("{workspaceId}", encodeURIComponent(workspaceId))}?${
          new URLSearchParams({ guest, ...(cursor ? { cursor } : {}) })}`,
        { method: workspaceMembershipOperations.listGuestPages.method, signal },
        session,
      ),
    /** Takes guests out of the workspace: every page of it shared with them stops being shared. */
    removeWorkspaceGuests: (session: Session, workspaceId: string, guests: readonly string[]) =>
      request<WorkspaceGuestRemoveResponse>(
        workspaceMembershipOperations.removeGuests.path.replace("{workspaceId}", encodeURIComponent(workspaceId)),
        { method: workspaceMembershipOperations.removeGuests.method, body: JSON.stringify({ guests }) },
        session,
      ),
    /** 설정 → 사람 → 그룹: the workspace's groups, by name. */
    listWorkspaceGroups: (session: Session, workspaceId: string, signal?: AbortSignal) =>
      request<{ groups: WorkspacePermissionGroup[] }>(
        `/workspaces/${encodeURIComponent(workspaceId)}/groups`, { signal }, session,
      ),
    /** "새 그룹 생성": its owner gives a group its icon, name and first members. */
    createWorkspaceGroup: (session: Session, workspaceId: string, input: { name: string; icon?: string; memberIds: string[] }) =>
      request<WorkspacePermissionGroup>(
        `/workspaces/${encodeURIComponent(workspaceId)}/groups`,
        { method: "POST", body: JSON.stringify(input) },
        session,
      ),
    /** A group opened in 설정 → 사람 → 그룹: its name and members. */
    readWorkspaceGroup: (session: Session, workspaceId: string, groupId: string, signal?: AbortSignal) =>
      request<WorkspacePermissionGroupDetail>(
        `/workspaces/${encodeURIComponent(workspaceId)}/groups/${encodeURIComponent(groupId)}`, { signal }, session,
      ),
    /** Its owner renames a group, changes its icon, or adds and removes its members. */
    changeWorkspaceGroup: (session: Session, workspaceId: string, groupId: string,
      input: { name?: string; icon?: string; add?: string[]; remove?: string[] }) =>
      request<WorkspacePermissionGroupDetail>(
        `/workspaces/${encodeURIComponent(workspaceId)}/groups/${encodeURIComponent(groupId)}`,
        { method: "PATCH", body: JSON.stringify(input) },
        session,
      ),
    deleteWorkspaceGroup: (session: Session, workspaceId: string, groupId: string) =>
      request<void>(
        `/workspaces/${encodeURIComponent(workspaceId)}/groups/${encodeURIComponent(groupId)}`,
        { method: "DELETE" },
        session,
      ),
    startWorkspaceOwnerTransfer: (
      session: Session,
      workspaceId: string,
      input: WorkspaceOwnerTransferInput,
      signal?: AbortSignal,
    ) => request<WorkspaceOwnerTransferResponse>(
      workspaceMembershipOperations.startOwnerTransfer.path.replace(
        "{workspaceId}",
        encodeURIComponent(workspaceId),
      ),
      {
        method: workspaceMembershipOperations.startOwnerTransfer.method,
        body: JSON.stringify(input),
        signal,
      },
      session,
    ),
    readWorkspaceOwnerTransfer: (
      session: Session,
      workspaceId: string,
      jobId: string,
      signal?: AbortSignal,
    ) => request<WorkspaceOwnerTransferResponse>(
      workspaceMembershipOperations.readOwnerTransfer.path
        .replace("{workspaceId}", encodeURIComponent(workspaceId))
        .replace("{jobId}", encodeURIComponent(jobId)),
      { method: workspaceMembershipOperations.readOwnerTransfer.method, signal },
      session,
    ),
    updateWorkspaceSettings: (
      session: Session,
      workspaceId: string,
      input: WorkspaceSettingsInput,
      signal?: AbortSignal,
    ) => request<WorkspaceSettingsMutationResponse>(
      workspaceMembershipOperations.updateSettings.path.replace(
        "{workspaceId}",
        encodeURIComponent(workspaceId),
      ),
      {
        method: workspaceMembershipOperations.updateSettings.method,
        body: JSON.stringify(input),
        signal,
      },
      session,
    ),
    /** What would refuse the workspace's deletion, read as its window opens. */
    readWorkspaceDeletionPreview: (session: Session, workspaceId: string, signal?: AbortSignal) => request<WorkspaceDeletionPreview>(
      workspaceMembershipOperations.readDeletionPreview.path.replace("{workspaceId}", encodeURIComponent(workspaceId)),
      { method: workspaceMembershipOperations.readDeletionPreview.method, signal },
      session,
    ),
    deleteWorkspace: (
      session: Session,
      workspaceId: string,
      input: WorkspaceDeletionInput,
      signal?: AbortSignal,
    ) => request<WorkspaceDeletionResponse>(
      workspaceMembershipOperations.deleteWorkspace.path.replace(
        "{workspaceId}",
        encodeURIComponent(workspaceId),
      ),
      {
        method: workspaceMembershipOperations.deleteWorkspace.method,
        body: JSON.stringify(input),
        signal,
      },
      session,
    ),
  };
}
