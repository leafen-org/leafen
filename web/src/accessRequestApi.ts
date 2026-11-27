import type { Session } from "./types";
import {
  accessRequestOperations,
  type AccessRequest,
  type AccessRequestDecisionInput,
  type AccessRequestGuestInput,
  type AccessRequestList,
  type AccessRequestMemberInput,
  type AccessRequestMembershipInput,
  type AccessRequestPageInput,
  type PageGuestLink,
  type PageGuestLinkCreated,
  type PageGuestLinkInput,
  type PageGuestLinkList,
  type WorkspaceInvite,
  type WorkspaceInviteLink,
  type WorkspaceMembershipRequestStatus,
} from "./accessRequestWire.generated";

type Request = <T>(
  path: string,
  init?: RequestInit,
  session?: Session | null,
) => Promise<T>;

function at(path: string, name: string, value: string) {
  return path.replace(`{${name}}`, encodeURIComponent(value));
}

/** Requests, as Notion's: asking for a page, a guest or a member, and deciding. */
export function createAccessRequestApi(request: Request) {
  const operations = accessRequestOperations;
  return {
    // A guest invite link (page_guest_links.go): its code shows only in the answer that makes it.
    createPageGuestLink: (session: Session, workspaceId: string, input: PageGuestLinkInput) =>
      request<PageGuestLinkCreated>(at(operations.createPageGuestLink.path, "workspaceId", workspaceId), {
        method: operations.createPageGuestLink.method, body: JSON.stringify(input),
      }, session),
    // A window of a page's links, newest first; the next goes on from its nextCursor.
    listPageGuestLinks: (session: Session, pageId: string, signal?: AbortSignal, cursor = "") =>
      request<PageGuestLinkList>(`${at(operations.listPageGuestLinks.path, "pageId", pageId)}${cursor ? `?${new URLSearchParams({ cursor })}` : ""}`, {
        method: operations.listPageGuestLinks.method, signal,
      }, session),
    revokePageGuestLink: (session: Session, linkId: string) =>
      request<PageGuestLink>(at(operations.revokePageGuestLink.path, "linkId", linkId), {
        method: operations.revokePageGuestLink.method,
      }, session),
    requestPageAccess: (session: Session, pageId: string, role: AccessRequestPageInput["role"]) =>
      request<AccessRequest>(at(operations.requestPage.path, "pageId", pageId), {
        method: operations.requestPage.method, body: JSON.stringify({ role }),
      }, session),
    requestGuestInvite: (session: Session, pageId: string, input: AccessRequestGuestInput) =>
      request<AccessRequest>(at(operations.requestGuest.path, "pageId", pageId), {
        method: operations.requestGuest.method, body: JSON.stringify(input),
      }, session),
    // With why the member asks, when they say ("요청 이유").
    requestMemberAdd: (session: Session, workspaceId: string, email: string, signal?: AbortSignal, message = "") =>
      request<AccessRequest>(at(operations.requestMember.path, "workspaceId", workspaceId), {
        method: operations.requestMember.method, signal,
        body: JSON.stringify({ email, ...(message.trim() ? { message: message.trim() } : {}) } satisfies AccessRequestMemberInput),
      }, session),
    // With why the person asks, when they say ("메시지 추가").
    requestWorkspaceMembership: (session: Session, workspaceId: string, signal?: AbortSignal, message = "") =>
      request<AccessRequest>(at(operations.requestMembership.path, "workspaceId", workspaceId), {
        method: operations.requestMembership.method, signal,
        body: JSON.stringify(message.trim() ? { message: message.trim() } satisfies AccessRequestMembershipInput : {}),
      }, session),
    workspaceMembershipRequestStatus: (session: Session, workspaceId: string, signal?: AbortSignal) =>
      request<WorkspaceMembershipRequestStatus>(at(operations.membershipStatus.path, "workspaceId", workspaceId), {
        method: operations.membershipStatus.method, signal,
      }, session),
    // A window of waiting requests; with nextCursor, more follow.
    listAccessRequests: (session: Session, workspaceId: string, signal?: AbortSignal, cursor?: string) =>
      request<AccessRequestList>(`${at(operations.list.path, "workspaceId", workspaceId)}${cursor ? `?cursor=${encodeURIComponent(cursor)}` : ""}`, {
        method: operations.list.method, signal,
      }, session),
    // The named requests as they stand, decided ones too, which the inbox's rows are about.
    readAccessRequests: (session: Session, workspaceId: string, ids: string[], signal?: AbortSignal) =>
      request<AccessRequestList>(`${at(operations.list.path, "workspaceId", workspaceId)}?ids=${ids.map(encodeURIComponent).join(",")}`, {
        method: operations.list.method, signal,
      }, session),
    // The workspace's invite link, its owners' alone, and what it invites to.
    readWorkspaceInviteLink: (session: Session, workspaceId: string, signal?: AbortSignal) =>
      request<WorkspaceInviteLink>(at(operations.readInviteLink.path, "workspaceId", workspaceId), {
        method: operations.readInviteLink.method, signal,
      }, session),
    /** Turns the link on or off. */
    setWorkspaceInviteLink: (session: Session, workspaceId: string, enabled: boolean) =>
      request<WorkspaceInviteLink>(at(operations.updateInviteLink.path, "workspaceId", workspaceId), {
        method: operations.updateInviteLink.method, body: JSON.stringify({ enabled }),
      }, session),
    rotateWorkspaceInviteLink: (session: Session, workspaceId: string) =>
      request<WorkspaceInviteLink>(at(operations.rotateInviteLink.path, "workspaceId", workspaceId), {
        method: operations.rotateInviteLink.method, body: "{}",
      }, session),
    readWorkspaceInvite: (session: Session, token: string, signal?: AbortSignal) =>
      request<WorkspaceInvite>(operations.readInvite.path, {
        method: operations.readInvite.method, signal, body: JSON.stringify({ token }),
      }, session),
    joinWorkspaceInvite: (session: Session, token: string) =>
      request<WorkspaceInvite>(operations.joinInvite.path, {
        method: operations.joinInvite.method, body: JSON.stringify({ token }),
      }, session),
    // A teamspace's invite link, for those who may invite its members.
    readTeamspaceInviteLink: (session: Session, workspaceId: string, teamspaceId: string, signal?: AbortSignal) =>
      request<WorkspaceInviteLink>(at(at(operations.readTeamspaceInviteLink.path, "workspaceId", workspaceId), "teamspaceId", teamspaceId), {
        method: operations.readTeamspaceInviteLink.method, signal,
      }, session),
    setTeamspaceInviteLink: (session: Session, workspaceId: string, teamspaceId: string, enabled: boolean) =>
      request<WorkspaceInviteLink>(at(at(operations.updateTeamspaceInviteLink.path, "workspaceId", workspaceId), "teamspaceId", teamspaceId), {
        method: operations.updateTeamspaceInviteLink.method, body: JSON.stringify({ enabled }),
      }, session),
    /** Approving a page's or a guest's request may give another role than the one asked for. */
    decideAccessRequest: (session: Session, requestId: string, approve: boolean, role?: AccessRequestDecisionInput["role"]) => {
      const operation = approve ? operations.approve : operations.decline;
      return request<AccessRequest>(at(operation.path, "requestId", requestId), {
        method: operation.method, body: JSON.stringify(approve && role ? { role } : {}),
      }, session);
    },
  };
}
