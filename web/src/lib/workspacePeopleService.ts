import { api } from "@/api";
import { waitForDurableJob } from "@/lib/durableJobPolling";
import { newId } from "@/lib/ids";
import {
  listAllWorkspaceMembers,
  type WorkspaceMemberDirectoryContext,
  type WorkspaceMemberDirectorySnapshot,
} from "@/lib/workspaceMemberDirectoryTransport";
import {
  workspaceMemberRoles,
  type WorkspaceOwnerTransferResponse,
} from "@/workspaceMembershipWire.generated";

export type WorkspacePeopleContext = WorkspaceMemberDirectoryContext & {
  isCurrent: () => boolean;
  memberRole?: () => "owner" | "member";
};

/** What adding by email did: a waiting invitation, and whether it's mailed. */
export type WorkspaceMemberAddResult = {
  snapshot: WorkspaceMemberDirectorySnapshot;
  invited: boolean;
  inviteEmail: boolean;
  requested: boolean;
};

type Dependencies = {
  addWorkspaceMemberByEmail: typeof api.addWorkspaceMemberByEmail;
  grantWorkspaceMember: typeof api.grantWorkspaceMember;
  requestMemberAdd: typeof api.requestMemberAdd;
  cancelWorkspaceMemberInvitation: typeof api.cancelWorkspaceMemberInvitation;
  createRequestId: () => string;
  listMembers: typeof listAllWorkspaceMembers;
  readWorkspaceOwnerTransfer: typeof api.readWorkspaceOwnerTransfer;
  revokeWorkspaceMember: typeof api.revokeWorkspaceMember;
  startWorkspaceOwnerTransfer: typeof api.startWorkspaceOwnerTransfer;
  waitForDurableJob: typeof waitForDurableJob;
};

const defaults: Dependencies = {
  addWorkspaceMemberByEmail: api.addWorkspaceMemberByEmail,
  grantWorkspaceMember: api.grantWorkspaceMember,
  requestMemberAdd: api.requestMemberAdd,
  cancelWorkspaceMemberInvitation: api.cancelWorkspaceMemberInvitation,
  createRequestId: newId,
  listMembers: listAllWorkspaceMembers,
  readWorkspaceOwnerTransfer: api.readWorkspaceOwnerTransfer,
  revokeWorkspaceMember: api.revokeWorkspaceMember,
  startWorkspaceOwnerTransfer: api.startWorkspaceOwnerTransfer,
  waitForDurableJob,
};

function abortContext(): never {
  throw new DOMException("Workspace People context changed.", "AbortError");
}

function assertCurrent(context: WorkspacePeopleContext) {
  if (!context.isCurrent() || context.signal.aborted) abortContext();
}

function contextKey(context: WorkspacePeopleContext) {
  return `${context.sessionIdentity}\u0000${context.workspaceId}`;
}

function memberActionKey(context: WorkspacePeopleContext, value: string) {
  return `${contextKey(context)}\u0000${value}`;
}

function validateOwnerTransfer(
  response: WorkspaceOwnerTransferResponse,
  context: WorkspacePeopleContext,
  requestId: string,
  targetUserId: string,
) {
  if (
    !response || typeof response !== "object"
    || typeof response.jobId !== "string" || !response.jobId
    || response.requestId !== requestId
    || response.workspaceId !== context.workspaceId
    || response.previousOwnerId !== context.session.user.id
    || response.ownerId !== targetUserId
    || !Number.isSafeInteger(response.permissionCursor) || response.permissionCursor < 0
    || (response.status !== "pending" && response.status !== "completed")
    || typeof response.duplicate !== "boolean"
  ) throw new Error("Invalid workspace owner-transfer response.");
  return response;
}

export function createWorkspacePeopleService(
  dependencies: Partial<Dependencies> = {},
) {
  const resolved = { ...defaults, ...dependencies };
  const refreshInflight = new Map<string, {
    epoch: number;
    promise: Promise<WorkspaceMemberDirectorySnapshot>;
  }>();
  const mutationEpoch = new Map<string, number>();
  const addInflight = new Map<string, Promise<WorkspaceMemberAddResult>>();
  const removeInflight = new Map<string, Promise<WorkspaceMemberDirectorySnapshot>>();
  const transferInflight = new Map<string, Promise<WorkspaceOwnerTransferResponse>>();
  const pendingTransferRequestIds = new Map<string, string>();

  function startRefresh(context: WorkspacePeopleContext, epoch: number) {
    const key = contextKey(context);
    const promise = (async () => {
      assertCurrent(context);
      const result = await resolved.listMembers(context);
      assertCurrent(context);
      return result;
    })();
    const entry = { epoch, promise };
    refreshInflight.set(key, entry);
    const clear = () => {
      if (refreshInflight.get(key) === entry) refreshInflight.delete(key);
    };
    promise.then(clear, clear);
    return promise;
  }

  function refreshAtLeast(
    context: WorkspacePeopleContext,
    epoch: number,
  ): Promise<WorkspaceMemberDirectorySnapshot> {
    const key = contextKey(context);
    const current = refreshInflight.get(key);
    if (!current) return startRefresh(context, epoch);
    if (current.epoch >= epoch) return current.promise;
    return current.promise.then(() => {
      assertCurrent(context);
      return refreshAtLeast(context, epoch);
    });
  }

  // The list read after a change that failed: if the list can't be read
  // either, the change's own failure is the one said (a change the server
  // didn't answer, 2026-11-07, read as "can't reach the server", the list's).
  async function refreshAfterFailed(context: WorkspacePeopleContext, mutationError: unknown) {
    try {
      return await refreshAtLeast(context, advanceMutation(context));
    } catch (error) {
      throw mutationError ?? error;
    }
  }

  function advanceMutation(context: WorkspacePeopleContext) {
    const key = contextKey(context);
    const next = (mutationEpoch.get(key) ?? 0) + 1;
    mutationEpoch.set(key, next);
    return next;
  }

  return {
    refresh(context: WorkspacePeopleContext) {
      return refreshAtLeast(context, mutationEpoch.get(contextKey(context)) ?? 0);
    },

    /** Adds someone by email; restricted brings them as a restricted member. */
    addMember(context: WorkspacePeopleContext, rawEmail: string, message?: string, restricted = false) {
      const email = rawEmail.trim().toLowerCase();
      const key = memberActionKey(context, `add:${email}`);
      const current = addInflight.get(key);
      if (current) return current;
      const request = (async () => {
        assertCurrent(context);
        if (context.memberRole?.() === "member") {
          await resolved.requestMemberAdd(context.session, context.workspaceId, email, context.signal, message);
          assertCurrent(context);
          const snapshot = await refreshAtLeast(context, advanceMutation(context));
          assertCurrent(context);
          return { snapshot, invited: false, inviteEmail: false, requested: true };
        }
        let mutationError: unknown;
        let invited = false;
        let inviteEmail = false;
        try {
          const response = await resolved.addWorkspaceMemberByEmail(
            context.session,
            context.workspaceId,
            { email, role: workspaceMemberRoles.member, ...(message ? { message } : {}), ...(restricted ? { restricted } : {}) },
            context.signal,
          );
          if (
            !response || response.workspaceId !== context.workspaceId
            || typeof response.accepted !== "boolean"
          ) throw new Error("Invalid workspace member-add response.");
          invited = response.invited === true;
          inviteEmail = invited && response.inviteEmail === true;
        } catch (error) {
          mutationError = error;
        }
        assertCurrent(context);
        const snapshot = await refreshAfterFailed(context, mutationError);
        assertCurrent(context);
        if (
          mutationError
          && !snapshot.members.some((member) => member.email.toLowerCase() === email)
          && !snapshot.invitations.some((invitation) => invitation.email === email)
        ) throw mutationError;
        return { snapshot, invited, inviteEmail, requested: false };
      })();
      addInflight.set(key, request);
      const clear = () => {
        if (addInflight.get(key) === request) addInflight.delete(key);
      };
      request.then(clear, clear);
      return request;
    },

    /** Adds an account of the organization by its id: someone with no address to add by (a name invitation's). */
    addMemberById(context: WorkspacePeopleContext, userId: string, restricted = false) {
      const key = memberActionKey(context, `add-id:${userId}`);
      const current = addInflight.get(key);
      if (current) return current;
      const request = (async () => {
        assertCurrent(context);
        let mutationError: unknown;
        try {
          await resolved.grantWorkspaceMember(context.session, context.workspaceId, userId, restricted);
        } catch (error) {
          mutationError = error;
        }
        assertCurrent(context);
        const snapshot = await refreshAfterFailed(context, mutationError);
        assertCurrent(context);
        if (mutationError && !snapshot.members.some((member) => member.userId === userId)) throw mutationError;
        return { snapshot, invited: false, inviteEmail: false, requested: false };
      })();
      addInflight.set(key, request);
      const clear = () => {
        if (addInflight.get(key) === request) addInflight.delete(key);
      };
      request.then(clear, clear);
      return request;
    },

    cancelInvitation(context: WorkspacePeopleContext, invitationId: string) {
      const key = memberActionKey(context, `cancel:${invitationId}`);
      const current = removeInflight.get(key);
      if (current) return current;
      const request = (async () => {
        assertCurrent(context);
        let mutationError: unknown;
        try {
          const response = await resolved.cancelWorkspaceMemberInvitation(
            context.session,
            context.workspaceId,
            invitationId,
            context.signal,
          );
          if (
            !response || response.workspaceId !== context.workspaceId
            || response.invitationId !== invitationId || response.cancelled !== true
          ) throw new Error("Invalid workspace invitation-cancel response.");
        } catch (error) {
          mutationError = error;
        }
        assertCurrent(context);
        const snapshot = await refreshAfterFailed(context, mutationError);
        assertCurrent(context);
        if (mutationError && snapshot.invitations.some((invitation) => invitation.id === invitationId)) {
          throw mutationError;
        }
        return snapshot;
      })();
      removeInflight.set(key, request);
      const clear = () => {
        if (removeInflight.get(key) === request) removeInflight.delete(key);
      };
      request.then(clear, clear);
      return request;
    },

    removeMember(context: WorkspacePeopleContext, userId: string) {
      const key = memberActionKey(context, `remove:${userId}`);
      const current = removeInflight.get(key);
      if (current) return current;
      const request = (async () => {
        assertCurrent(context);
        let mutationError: unknown;
        try {
          const response = await resolved.revokeWorkspaceMember(
            context.session,
            context.workspaceId,
            userId,
            context.signal,
          );
          if (
            !response || response.workspaceId !== context.workspaceId
            || response.userId !== userId || response.revoked !== true
          ) throw new Error("Invalid workspace member-removal response.");
        } catch (error) {
          mutationError = error;
        }
        assertCurrent(context);
        const snapshot = await refreshAfterFailed(context, mutationError);
        assertCurrent(context);
        if (mutationError && snapshot.members.some((member) => member.userId === userId)) {
          throw mutationError;
        }
        return snapshot;
      })();
      removeInflight.set(key, request);
      const clear = () => {
        if (removeInflight.get(key) === request) removeInflight.delete(key);
      };
      request.then(clear, clear);
      return request;
    },

    /** Makes a member restricted, or a member again, through the organization module's call. */
    restrictMember(
      context: WorkspacePeopleContext,
      userId: string,
      restricted: boolean,
      run: (signal: AbortSignal) => Promise<void>,
    ) {
      const key = memberActionKey(context, `role:${userId}`);
      const current = removeInflight.get(key);
      if (current) return current;
      const request = (async () => {
        assertCurrent(context);
        let mutationError: unknown;
        try {
          await run(context.signal);
        } catch (error) {
          mutationError = error;
        }
        assertCurrent(context);
        const snapshot = await refreshAfterFailed(context, mutationError);
        assertCurrent(context);
        if (mutationError && snapshot.members.some((member) => member.userId === userId && (member.restricted ?? false) !== restricted)) {
          throw mutationError;
        }
        return snapshot;
      })();
      removeInflight.set(key, request);
      const clear = () => {
        if (removeInflight.get(key) === request) removeInflight.delete(key);
      };
      request.then(clear, clear);
      return request;
    },

    transferOwner(
      context: WorkspacePeopleContext,
      targetUserId: string,
      basePermissionCursor: number,
      refreshWorkspace: () => Promise<void>,
    ) {
      const key = memberActionKey(context, `owner:${targetUserId}`);
      const current = transferInflight.get(key);
      if (current) return current;
      const requestId = pendingTransferRequestIds.get(key) ?? resolved.createRequestId();
      pendingTransferRequestIds.set(key, requestId);
      const request = (async () => {
        assertCurrent(context);
        const initial = validateOwnerTransfer(
          await resolved.startWorkspaceOwnerTransfer(
            context.session,
            context.workspaceId,
            { basePermissionCursor, requestId, targetUserId },
            context.signal,
          ),
          context,
          requestId,
          targetUserId,
        );
        assertCurrent(context);
        const terminal = await resolved.waitForDurableJob({
          initial,
          isSameJob: (next) => (
            next.jobId === initial.jobId
            && next.requestId === requestId
            && next.workspaceId === context.workspaceId
            && next.previousOwnerId === context.session.user.id
            && next.ownerId === targetUserId
          ),
          isTerminal: (next) => next.status === "completed",
          onProgress: () => {},
          read: async () => validateOwnerTransfer(
            await resolved.readWorkspaceOwnerTransfer(
              context.session,
              context.workspaceId,
              initial.jobId,
              context.signal,
            ),
            context,
            requestId,
            targetUserId,
          ),
          signal: context.signal,
        });
        assertCurrent(context);
        await refreshWorkspace();
        pendingTransferRequestIds.delete(key);
        return terminal;
      })();
      transferInflight.set(key, request);
      const clear = () => {
        if (transferInflight.get(key) === request) transferInflight.delete(key);
      };
      request.then(clear, clear);
      return request;
    },
  };
}
