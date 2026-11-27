import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { api } from "@/api";
import { createImportedPeopleMappingService } from "@/lib/importedPeopleMappingService";
import { organizationModule } from "@/lib/organizationModule";
import { settingsErrorMessage } from "@/lib/settingsErrors";
import { useAccessRevision } from "@/lib/workspaceChangeCheck";
import {
  createWorkspacePeopleService,
  type WorkspacePeopleContext,
} from "@/lib/workspacePeopleService";
import type { WorkspaceMemberDirectorySnapshot } from "@/lib/workspaceMemberDirectoryTransport";
import { nativeNotionImportBounds } from "@/notionImportWire.generated";
import type { Session, Workspace } from "@/types";
import { workspaceMembershipBounds } from "@/workspaceMembershipWire.generated";

type PeopleState = {
  contextKey: string;
  error: string;
  /** A failed read of the people, said where they show with a way to read again; an action's failure is `error`. */
  readFailed: string;
  loading: boolean;
  snapshot: WorkspaceMemberDirectorySnapshot | null;
};

function isAbort(error: unknown) {
  return error instanceof DOMException && error.name === "AbortError";
}

export function useWorkspacePeople({
  onWorkspaceRefresh,
  session,
  workspace,
}: {
  onWorkspaceRefresh: () => Promise<void>;
  session: Session;
  workspace: Workspace;
}) {
  const { t } = useTranslation("workspaceSettingsDialog");
  const sessionIdentity = session.user.id;
  const contextKey = `${sessionIdentity}\u0000${workspace.id}`;
  const contextKeyRef = useRef(contextKey);
  const requestGeneration = useRef(1);
  if (contextKeyRef.current !== contextKey) {
    contextKeyRef.current = contextKey;
    requestGeneration.current += 1;
  }
  const sessionRef = useRef(session);
  sessionRef.current = session;
  const controllers = useRef(new Set<AbortController>());
  const publicationGeneration = useRef(0);
  const peopleService = useMemo(() => createWorkspacePeopleService(), [contextKey]);
  const importedPeopleService = useMemo(() => createImportedPeopleMappingService(
    api,
    () => contextKeyRef.current === contextKey ? sessionRef.current : null,
  ), [contextKey]);
  const importedPeopleBounds = useMemo(() => ({
    maxMembers: nativeNotionImportBounds.personMappingMembers,
    maxPeople: nativeNotionImportBounds.personMappingItems,
    maxScalarCharacters: workspaceMembershipBounds.permissionIdentifierLength,
    maxTotalCharacters: workspaceMembershipBounds.permissionIdentifierLength * (
      nativeNotionImportBounds.personMappingItems * 5
      + nativeNotionImportBounds.personMappingMembers * 4
    ),
  }), []);
  const [busyActions, setBusyActions] = useState<Set<string>>(() => new Set());
  const [state, setState] = useState<PeopleState>({
    contextKey,
    error: "",
    readFailed: "",
    loading: true,
    snapshot: null,
  });

  const peopleStateRef = useRef(state);
  peopleStateRef.current = state;

  const startController = useCallback(() => {
    const controller = new AbortController();
    controllers.current.add(controller);
    return controller;
  }, []);

  const finishController = useCallback((controller: AbortController) => {
    controllers.current.delete(controller);
  }, []);

  const createContext = useCallback((controller: AbortController) => {
    const generation = requestGeneration.current;
    const isCurrent = () => (
      contextKeyRef.current === contextKey
      && requestGeneration.current === generation
      && !controller.signal.aborted
    );
    const result: WorkspacePeopleContext = {
      current: () => ({
        requestGeneration: isCurrent() ? generation : requestGeneration.current,
        sessionIdentity: isCurrent() ? sessionIdentity : "",
        userId: isCurrent() ? session.user.id : "",
        workspaceId: isCurrent() ? workspace.id : "",
      }),
      isCurrent,
      memberRole: () => peopleStateRef.current.contextKey === contextKey && peopleStateRef.current.snapshot
        ? peopleStateRef.current.snapshot.currentMember.role as "owner" | "member"
        : workspace.role === "member" ? "member" : "owner",
      requestGeneration: generation,
      session,
      sessionIdentity,
      signal: controller.signal,
      workspaceId: workspace.id,
    };
    return result;
  }, [contextKey, session, sessionIdentity, workspace.id, workspace.role]);

  const setActionBusy = useCallback((action: string, busy: boolean) => {
    setBusyActions((current) => {
      const next = new Set(current);
      if (busy) next.add(action);
      else next.delete(action);
      return next;
    });
  }, []);

  const publishSnapshot = useCallback((snapshot: WorkspaceMemberDirectorySnapshot) => {
    setState((current) => (
      current.contextKey === contextKey
      && (current.snapshot?.authorityRevision ?? -1) > snapshot.authorityRevision
    ) ? current : {
        contextKey,
        error: "",
        readFailed: "",
        loading: false,
        snapshot,
      });
  }, [contextKey]);

  const loadMembers = useCallback(async () => {
    const controller = startController();
    const current = createContext(controller);
    const publication = ++publicationGeneration.current;
    setState((previous) => previous.contextKey === contextKey
      ? { ...previous, error: "", loading: true }
      : { contextKey, error: "", readFailed: "", loading: true, snapshot: null });
    try {
      const snapshot = await peopleService.refresh(current);
      if (!current.isCurrent() || publicationGeneration.current !== publication) return;
      publishSnapshot(snapshot);
    } catch (error) {
      if (!isAbort(error) && current.isCurrent() && publicationGeneration.current === publication) {
        setState((previous) => ({
          ...previous,
          contextKey,
          readFailed: settingsErrorMessage(error, t("errLoadMembers")),
          loading: false,
        }));
      }
    } finally {
      finishController(controller);
    }
  }, [contextKey, createContext, finishController, peopleService, publishSnapshot, startController, t]);

  // Read again from a failed read: the loading line comes back at once.
  const readAgain = useCallback(() => {
    setState((previous) => ({ ...previous, readFailed: "", loading: true }));
    void loadMembers();
  }, [loadMembers]);

  useEffect(() => {
    setBusyActions(new Set());
    void loadMembers();
    return () => {
      for (const controller of controllers.current) controller.abort();
      controllers.current.clear();
    };
  }, [contextKey, loadMembers]);

  // Who is here changes while the list is open — an invited person joins, one
  // leaves, a role moves — and the workspace's change check says so: the list
  // is read again (2026-10-26: a joined member stayed "초대 대기 중").
  const accessRevision = useAccessRevision();
  const seenAccessRevision = useRef(accessRevision);
  useEffect(() => {
    if (seenAccessRevision.current === accessRevision) return;
    seenAccessRevision.current = accessRevision;
    void loadMembers();
  }, [accessRevision, loadMembers]);

  const runRosterAction = useCallback(async (
    action: string,
    fallback: string,
    operation: (current: WorkspacePeopleContext) => Promise<WorkspaceMemberDirectorySnapshot>,
  ) => {
    const controller = startController();
    const current = createContext(controller);
    publicationGeneration.current += 1;
    setActionBusy(action, true);
    setState((previous) => ({ ...previous, error: "" }));
    try {
      const snapshot = await operation(current);
      if (!current.isCurrent()) return false;
      publishSnapshot(snapshot);
      return true;
    } catch (error) {
      if (!isAbort(error) && current.isCurrent()) {
        setState((previous) => ({
          ...previous,
          error: settingsErrorMessage(error, fallback),
        }));
      }
      return false;
    } finally {
      finishController(controller);
      if (current.isCurrent()) setActionBusy(action, false);
    }
  }, [createContext, finishController, publishSnapshot, setActionBusy, startController]);

  /** Adds someone by email, as a restricted member if asked; says whether they were invited instead, and mailed (with the note, if any). */
  const addMember = useCallback(async (email: string, message?: string, restricted = false) => {
    let outcome: { invited: boolean; inviteEmail: boolean; requested: boolean; signupToken?: string } = { invited: false, inviteEmail: false, requested: false };
    // Why it failed, as the roster's own error says it, for the window that asked.
    let failed = "";
    const added = await runRosterAction("addMember", t("memberAddFailed"), async (current) => {
      try {
        const result = await peopleService.addMember(current, email, message, restricted);
        // The invitation's link, for the toast that hands it on where no mail carries it.
        const signupToken = result.snapshot.invitations.find((invitation) => invitation.email === email.trim().toLowerCase())?.signupToken;
        outcome = { invited: result.invited, inviteEmail: result.inviteEmail, requested: result.requested, ...(signupToken ? { signupToken } : {}) };
        return result.snapshot;
      } catch (error) {
        failed = settingsErrorMessage(error, t("memberAddFailed"));
        throw error;
      }
    });
    if (added) return outcome;
    // The window that asked says it; the page under it doesn't say it again.
    if (failed) setState((previous) => ({ ...previous, error: "" }));
    return { failed };
  }, [peopleService, runRosterAction, t]);

  /** Adds an account of the organization by its id, as addMember adds by email. */
  const addMemberById = useCallback(async (userId: string, restricted = false) => {
    let failed = "";
    const added = await runRosterAction("addMember", t("memberAddFailed"), async (current) => {
      try {
        return (await peopleService.addMemberById(current, userId, restricted)).snapshot;
      } catch (error) {
        failed = settingsErrorMessage(error, t("memberAddFailed"));
        throw error;
      }
    });
    if (added) return { invited: false, inviteEmail: false, requested: false };
    if (failed) setState((previous) => ({ ...previous, error: "" }));
    return { failed };
  }, [peopleService, runRosterAction, t]);

  const cancelInvitation = useCallback((invitationId: string) => runRosterAction(
    `cancel:${invitationId}`,
    t("invitationCancelFailed"),
    (current) => peopleService.cancelInvitation(current, invitationId),
  ), [peopleService, runRosterAction, t]);

  const removeMember = useCallback((userId: string) => runRosterAction(
    `remove:${userId}`,
    t("memberRemoveFailed"),
    (current) => peopleService.removeMember(current, userId),
  ), [peopleService, runRosterAction, t]);

  const restrictMember = useCallback((userId: string, restricted: boolean) => {
    const restrict = organizationModule()?.restrictMember;
    if (!restrict) return Promise.resolve(false);
    return runRosterAction(`role:${userId}`, t("memberRoleFailed"), (current) => peopleService.restrictMember(
      current, userId, restricted, (signal) => restrict(current.session, current.workspaceId, userId, restricted, signal)));
  }, [peopleService, runRosterAction, t]);

  const transferOwner = useCallback(async (targetUserId: string) => {
    const snapshot = state.contextKey === contextKey ? state.snapshot : null;
    if (!snapshot) return false;
    const action = `owner:${targetUserId}`;
    const controller = startController();
    const current = createContext(controller);
    const publication = ++publicationGeneration.current;
    setActionBusy(action, true);
    setState((previous) => ({ ...previous, error: "" }));
    try {
      await peopleService.transferOwner(
        current,
        targetUserId,
        snapshot.authorityRevision,
        onWorkspaceRefresh,
      );
      return current.isCurrent() && publicationGeneration.current === publication;
    } catch (error) {
      if (!isAbort(error) && current.isCurrent() && publicationGeneration.current === publication) {
        setState((previous) => ({
          ...previous,
          error: settingsErrorMessage(error, t("ownerTransferFailed")),
        }));
      }
      return false;
    } finally {
      finishController(controller);
      if (current.isCurrent()) setActionBusy(action, false);
    }
  }, [contextKey, createContext, finishController, onWorkspaceRefresh, peopleService, setActionBusy, startController, state.contextKey, state.snapshot, t]);

  const current = state.contextKey === contextKey ? state : {
    contextKey,
    error: "",
    readFailed: "",
    loading: true,
    snapshot: null,
  };
  return {
    ...current,
    addMember,
    addMemberById,
    busyActions,
    cancelInvitation,
    importedPeopleBounds,
    importedPeopleService,
    readAgain,
    refresh: loadMembers,
    removeMember,
    restrictMember,
    transferOwner,
  };
}
