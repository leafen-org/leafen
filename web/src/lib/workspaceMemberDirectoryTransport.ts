import { api } from "@/api";
import {
  administratorMemberAddPolicies,
  type AdministratorMemberAddPolicy,
} from "@/administratorWire.generated";
import {
  workspaceMembershipBounds,
  type WorkspaceMemberDirectoryItem,
  type WorkspaceMemberDirectoryResponse,
  type WorkspaceMemberInvitation,
} from "@/workspaceMembershipWire.generated";
import type { Session } from "@/types";

export type WorkspaceMemberDirectoryContext = {
  session: Session;
  sessionIdentity: string;
  workspaceId: string;
  requestGeneration: number;
  signal: AbortSignal;
  current: () => {
    sessionIdentity: string;
    userId: string;
    workspaceId: string;
    requestGeneration: number;
  };
};

export type WorkspaceMemberDirectorySnapshot = {
  authorityRevision: number;
  memberAddPolicy: AdministratorMemberAddPolicy;
  memberAddPolicyRevision: number;
  memberRequests?: boolean;
  currentMember: WorkspaceMemberDirectoryItem;
  members: WorkspaceMemberDirectoryItem[];
  /** The invitations waiting for their address to sign up, oldest first. */
  invitations: WorkspaceMemberInvitation[];
};

function abortRead(message: string): never {
  throw new DOMException(message, "AbortError");
}

function isCurrentWorkspaceMemberDirectory(
  context: WorkspaceMemberDirectoryContext,
) {
  const current = context.current();
  return !context.signal.aborted
    && current.sessionIdentity === context.sessionIdentity
    && current.userId === context.session.user.id
    && current.workspaceId === context.workspaceId
    && current.requestGeneration === context.requestGeneration;
}

function assertCurrent(context: WorkspaceMemberDirectoryContext) {
  if (!isCurrentWorkspaceMemberDirectory(context)) {
    throw context.signal.reason ?? abortRead("Workspace member-directory context changed.");
  }
}

function validateMember(
  item: WorkspaceMemberDirectoryItem,
  workspaceId: string,
  seenIds?: Set<string>,
) {
  if (
    !item || typeof item !== "object"
    || typeof item.id !== "string" || !item.id || seenIds?.has(item.id)
    || item.workspaceId !== workspaceId
    || typeof item.userId !== "string" || !item.userId
    || typeof item.displayName !== "string" || !item.displayName
    // An account may have no email (an account starts with a name).
    || typeof item.email !== "string"
    || (item.avatar !== null && typeof item.avatar !== "string")
    || (item.role !== "owner" && item.role !== "member")
    || !Number.isSafeInteger(item.createdAt) || item.createdAt <= 0
    || !Number.isSafeInteger(item.updatedAt) || item.updatedAt <= 0
    || !validMemberships(item.teamspaceCount, item.teamspaces, (teamspace) =>
      typeof teamspace?.id === "string" && typeof teamspace.name === "string" && typeof teamspace.icon === "string"
      && (teamspace.memberCount === undefined || (Number.isSafeInteger(teamspace.memberCount) && teamspace.memberCount >= 0)))
    || !validMemberships(item.groupCount, item.groups, (group) => typeof group?.id === "string" && typeof group.name === "string"
      && (group.memberCount === undefined || (Number.isSafeInteger(group.memberCount) && group.memberCount >= 0)))
  ) throw new Error("Invalid workspace member-directory item.");
  seenIds?.add(item.id);
}

// A person's teamspaces or groups: how many, and at most the first few of them.
function validMemberships<Entry>(count: number | undefined, entries: Entry[] | undefined, valid: (entry: Entry) => boolean) {
  const total = count ?? 0;
  const listed = entries ?? [];
  return Number.isSafeInteger(total) && total >= 0 && Array.isArray(listed)
    && listed.length <= Math.min(total, workspaceMembershipBounds.memberDirectoryMembershipsPerMember) && listed.every(valid);
}

function validateInvitations(response: WorkspaceMemberDirectoryResponse, workspaceId: string, first: boolean) {
  // A page without them (a later page, or an older server) has none.
  const invitations = response.invitations ?? [];
  if (
    !Array.isArray(invitations)
    || invitations.length > (first ? workspaceMembershipBounds.memberInvitations : 0)
    || invitations.some((invitation) => (
      !invitation || typeof invitation.id !== "string" || !invitation.id
      || invitation.workspaceId !== workspaceId
      || typeof invitation.email !== "string" || !invitation.email
      || !Number.isSafeInteger(invitation.createdAt) || invitation.createdAt <= 0
    ))
  ) throw new Error("Invalid workspace member invitations.");
  return invitations;
}

function validatePage(
  response: WorkspaceMemberDirectoryResponse,
  context: WorkspaceMemberDirectoryContext,
): AdministratorMemberAddPolicy {
  const memberAddPolicy = response?.memberAddPolicy;
  if (
    !response || typeof response !== "object"
    || !Number.isSafeInteger(response.authorityRevision) || response.authorityRevision < 0
    || (memberAddPolicy !== administratorMemberAddPolicies.enabled
      && memberAddPolicy !== administratorMemberAddPolicies.disabled)
    || !Number.isSafeInteger(response.memberAddPolicyRevision)
    || response.memberAddPolicyRevision < 1
    || (response.memberRequests !== undefined && typeof response.memberRequests !== "boolean")
    || !Array.isArray(response.members)
    || response.members.length > workspaceMembershipBounds.memberDirectoryMaximumPageSize
    || (response.nextCursor !== null && (
      typeof response.nextCursor !== "string" || !response.nextCursor
      || response.nextCursor.length > workspaceMembershipBounds.memberDirectoryCursorBytes
    ))
  ) throw new Error("Invalid workspace member-directory page.");
  validateMember(response.currentMember, context.workspaceId);
  if (
    response.currentMember.userId !== context.session.user.id
    || (response.currentMember.role !== "owner" && response.currentMember.role !== "member")
  ) throw new Error("Invalid workspace member-directory principal.");
  return memberAddPolicy;
}

export async function listAllWorkspaceMembers(
  context: WorkspaceMemberDirectoryContext,
): Promise<WorkspaceMemberDirectorySnapshot> {
  assertCurrent(context);
  const members: WorkspaceMemberDirectoryItem[] = [];
  const seenCursors = new Set<string>();
  const seenIds = new Set<string>();
  let cursor: string | undefined;
  let authorityRevision: number | null = null;
  let memberAddPolicy: AdministratorMemberAddPolicy | null = null;
  let memberAddPolicyRevision: number | null = null;
  let memberRequests = false;
  let currentMember: WorkspaceMemberDirectoryItem | null = null;
  let invitations: WorkspaceMemberInvitation[] = [];
  for (;;) {
    const cursorIdentity = cursor ?? "";
    if (seenCursors.has(cursorIdentity)) {
      throw new Error("Workspace member-directory cursor repeated.");
    }
    seenCursors.add(cursorIdentity);
    const response = await api.listWorkspaceMembers(
      context.session,
      context.workspaceId,
      cursor,
      workspaceMembershipBounds.memberDirectoryMaximumPageSize,
      authorityRevision ?? undefined,
      context.signal,
    );
    assertCurrent(context);
    const responseMemberAddPolicy = validatePage(response, context);
    const pageInvitations = validateInvitations(response, context.workspaceId, authorityRevision === null);
    if (authorityRevision === null) {
      memberRequests = response.memberRequests === true;
      invitations = pageInvitations;
      authorityRevision = response.authorityRevision;
      memberAddPolicy = responseMemberAddPolicy;
      memberAddPolicyRevision = response.memberAddPolicyRevision;
      currentMember = response.currentMember;
    } else if (response.authorityRevision !== authorityRevision) {
      throw new Error("Workspace member-directory authority changed during continuation.");
    } else if (
      (response.memberRequests === true) !== memberRequests
      || response.memberAddPolicy !== memberAddPolicy
      || response.memberAddPolicyRevision !== memberAddPolicyRevision
    ) {
      throw new Error("Workspace member-add policy changed during continuation.");
    } else if (
      currentMember?.id !== response.currentMember.id
      || currentMember.userId !== response.currentMember.userId
    ) {
      throw new Error("Workspace member-directory principal changed during continuation.");
    }
    for (const item of response.members) {
      validateMember(item, context.workspaceId, seenIds);
      members.push(item);
    }
    if (!response.nextCursor) {
      assertCurrent(context);
      if (
        authorityRevision === null || memberAddPolicy === null
        || memberAddPolicyRevision === null || currentMember === null
      ) {
        throw new Error("Workspace member-directory snapshot is incomplete.");
      }
      return {
        authorityRevision,
        memberAddPolicy,
        memberAddPolicyRevision,
        memberRequests,
        currentMember,
        members,
        invitations,
      };
    }
    cursor = response.nextCursor;
  }
}
