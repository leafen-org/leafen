import { isLegalHoldPermanentDeleteError } from "@/lib/nativeTrashLifecycle";
import { useCallback, useEffect, useRef, useState } from "react";
import { api } from "@/api";
import { settingsErrorMessage } from "@/lib/settingsErrors";
import { isUncertainWrite } from "@/lib/uncertainWrites";
import { useShownChanges } from "@/lib/shownChanges";
import type { Session, Workspace } from "@/types";
import { workspaceMembershipErrorCodes, type WorkspaceDeletionResponse, type WorkspaceSettingsPatch } from "@/workspaceMembershipWire.generated";
import type { LandingPageAnswer } from "./WorkspaceLandingPage";
import { workspaceDeletionDriver as deletionDriver } from "./resumeWorkspaceDeletions";
import { startLeavingWorkspace, stopLeavingWorkspace } from "@/lib/workspaceLeaving";
import { normalizeWorkspaceSlug, workspaceSettingsRevision } from "./workspaceSettingsOverviewModel";
import { createWorkspaceSettingsMutationDriver } from "./workspaceSettingsMutationDriver";

const settingsMutationDriver = createWorkspaceSettingsMutationDriver({
  updateWorkspaceSettings: api.updateWorkspaceSettings,
});

type DeletionView = {
  workspaceId: string;
  /** The deleted workspace's name, as its deletion is confirmed with it. */
  name: string;
  status: "deleting" | "retryable" | "completed";
  result?: WorkspaceDeletionResponse;
  error?: string;
};

export type WorkspaceSettingsOverviewInput = {
  session: Session;
  workspace: Workspace;
  workspaces: Workspace[];
  canManageWorkspace: boolean;
  onWorkspaceChange: (workspace: Workspace) => void;
  onWorkspaceDeleted: (workspaceId: string) => void | Promise<void>;
  message: (key: string) => string;
};

export function useWorkspaceSettingsOverview(input: WorkspaceSettingsOverviewInput) {
  const { session, workspace, workspaces, canManageWorkspace } = input;
  const sessionIdentity = session.user.id;
  const contextKey = `${sessionIdentity}\u0000${workspace.id}`;
  const contextKeyRef = useRef(contextKey);
  const generationRef = useRef(1);
  if (contextKeyRef.current !== contextKey) {
    contextKeyRef.current = contextKey;
    generationRef.current += 1;
  }
  const abortRef = useRef<AbortController | null>(null);
  const workspaceRef = useRef(workspace);
  workspaceRef.current = workspace;
  const sessionIdentityRef = useRef(sessionIdentity);
  sessionIdentityRef.current = sessionIdentity;

  const [name, setName] = useState(workspace.name);
  const [domain, setDomain] = useState(workspace.domain ?? "");
  const [workspaceError, setWorkspaceError] = useState("");
  // Which control the failure is told under: its own, as the address's is.
  const [workspaceErrorAt, setWorkspaceErrorAt] = useState<"name" | "icon" | "landing">("name");
  const [domainError, setDomainError] = useState("");
  const [domainBusy, setDomainBusy] = useState(false);
  const [deleteWorkspaceConfirm, setDeleteWorkspaceConfirm] = useState("");
  const [deleteWorkspaceError, setDeleteWorkspaceError] = useState("");
  const [deleteWorkspaceBusy, setDeleteWorkspaceBusy] = useState(false);
  const [workspaceDeletion, setWorkspaceDeletion] = useState<DeletionView>();
  // The icon chosen shows at once and goes to the server behind it; a refused
  // one puts back the server's with why under it (lib/shownChanges.ts).
  const iconChanges = useShownChanges<{ icon: string }>();

  useEffect(() => {
    const controller = new AbortController();
    abortRef.current?.abort();
    abortRef.current = controller;
    settingsMutationDriver.seed(sessionIdentity, workspace);
    setName(workspace.name);
    setDomain(workspace.domain ?? "");
    setWorkspaceError("");
    setDomainError("");
    setDeleteWorkspaceConfirm("");
    setDeleteWorkspaceError("");
    // Another workspace's deletion on its way, or paused, stays: the
    // settings move off a workspace as its deletion starts, and its window
    // still says how it goes (2026-11-11: it showed the next workspace).
    setWorkspaceDeletion((current) => current && current.status !== "completed" && current.workspaceId !== workspace.id ? current : undefined);
    return () => controller.abort();
  }, [contextKey, sessionIdentity, workspace.id]);

  useEffect(() => {
    settingsMutationDriver.seed(sessionIdentity, workspace);
  }, [sessionIdentity, workspace]);

  const commitPatch = useCallback(async (patch: WorkspaceSettingsPatch) => {
    const generation = generationRef.current;
    const signal = abortRef.current?.signal;
    const currentWorkspace = workspaceRef.current;
    const isCurrent = () => (
      contextKeyRef.current === contextKey
      && generationRef.current === generation
      && !signal?.aborted
    );
    const response = await settingsMutationDriver.mutate({
      session,
      sessionIdentity,
      workspaceId: currentWorkspace.id,
      settingsRevision: workspaceSettingsRevision(currentWorkspace.settingsRevision),
      patch,
      signal,
      isCurrent,
    });
    if (isCurrent()) input.onWorkspaceChange(response.workspace);
    return response.workspace;
  }, [contextKey, input, session, sessionIdentity]);

  const saveName = useCallback(async () => {
    if (!canManageWorkspace) return;
    const nextName = name.trim() || input.message("untitledWorkspace");
    if (nextName !== workspaceRef.current.name) {
      setWorkspaceError("");
      try {
        await commitPatch({ name: nextName });
      } catch (error) {
        if (contextKeyRef.current !== contextKey) return;
        setWorkspaceErrorAt("name");
        setWorkspaceError(settingsErrorMessage(error, input.message("errUpdateWorkspace")));
        // Typed words a change the server didn't answer stay, as the account's
        // name keeps them; the settings read again once it answers.
        if (!isUncertainWrite(error)) setName(workspaceRef.current.name);
        return;
      }
    }
    setName(nextName);
  }, [canManageWorkspace, commitPatch, contextKey, input, name]);

  const saveDomain = useCallback(async () => {
    if (!canManageWorkspace) return;
    const nextDomain = normalizeWorkspaceSlug(domain);
    setDomain(nextDomain);
    setDomainError("");
    if (nextDomain === (workspaceRef.current.domain ?? "")) return;
    setDomainBusy(true);
    try {
      const updated = await commitPatch({ domain: nextDomain });
      setDomain(updated.domain ?? "");
    } catch (error) {
      if (contextKeyRef.current !== contextKey) return;
      if (!isUncertainWrite(error)) setDomain(workspaceRef.current.domain ?? "");
      setDomainError(settingsErrorMessage(error, input.message("errUpdateWorkspaceUrl")));
    } finally {
      if (contextKeyRef.current === contextKey) setDomainBusy(false);
    }
  }, [canManageWorkspace, commitPatch, contextKey, domain, input]);

  const updateIcon = useCallback((icon: string) => {
    if (!canManageWorkspace) return;
    setWorkspaceError("");
    void iconChanges.change({ icon }, () => commitPatch({ icon })).catch((error: unknown) => {
      if (contextKeyRef.current !== contextKey) return;
      setWorkspaceErrorAt("icon");
      setWorkspaceError(settingsErrorMessage(error, input.message("errUpdateWorkspaceIcon")));
    });
  }, [canManageWorkspace, commitPatch, contextKey, iconChanges, input]);

  /**
   * Chooses the page people who join open first, or none: "saved", "hidden"
   * for a page some of them couldn't open, which the chooser itself tells
   * (WorkspaceLandingPage), or "failed", told under it.
   */
  const updateLandingPage = useCallback(async (landingPageId: string): Promise<LandingPageAnswer> => {
    if (!canManageWorkspace) return "failed";
    setWorkspaceError("");
    try {
      await commitPatch({ landingPageId });
      return "saved";
    } catch (error) {
      if ((error as { code?: unknown } | null)?.code === workspaceMembershipErrorCodes.landingPageHidden) return "hidden";
      if (contextKeyRef.current === contextKey) {
        setWorkspaceErrorAt("landing");
        setWorkspaceError(settingsErrorMessage(error, input.message("errUpdateLandingPage")));
      }
      return "failed";
    }
  }, [canManageWorkspace, commitPatch, contextKey, input]);

  const canDeleteWorkspace = canManageWorkspace && workspaces.length > 1;
  const deleteWorkspaceConfirmMatches = deleteWorkspaceConfirm.trim() === workspace.name;

  // A deletion belongs to the session that asked for it, not to the
  // workspace the settings show: the workspace leaves the list as soon as
  // its deletion starts, which moves the settings to another one, and the
  // deletion still has to finish and leave its addresses (2026-10-16). What
  // the settings show follows only while they still show it.
  const driveDeletion = useCallback(async (target?: { id: string; name: string }) => {
    const generation = generationRef.current;
    // The workspace asked for, never whichever the settings show by now.
    const currentWorkspace = target ?? workspaceRef.current;
    const isCurrent = () => sessionIdentityRef.current === sessionIdentity;
    const shown = () => isCurrent() && contextKeyRef.current === contextKey && generationRef.current === generation;
    // The person leaves the workspace as soon as the server takes its
    // deletion, as Notion's goes at once; the rest goes on quietly in this
    // tab, and from the next opening should the tab close first
    // (resumeWorkspaceDeletions). 2026-11-11: a workspace of 100,000 rows
    // held the card at "삭제 중..." for its sixteen minutes.
    let left: Promise<void> | undefined;
    const leave = () => {
      left ??= (async () => {
        try {
          await input.onWorkspaceDeleted(currentWorkspace.id);
        } catch (error) {
          if (shown()) {
            setDeleteWorkspaceError(settingsErrorMessage(error, input.message("deleteWorkspaceFailed")));
          }
        } finally {
          if (isCurrent()) setDeleteWorkspaceBusy(false);
        }
      })();
      return left;
    };
    // Left from the confirmation on: the server closes the workspace's
    // connections as it takes the deletion, and nothing asks why.
    startLeavingWorkspace(currentWorkspace.id);
    setDeleteWorkspaceBusy(true);
    setDeleteWorkspaceError("");
    setWorkspaceDeletion((current) => ({
      workspaceId: currentWorkspace.id,
      name: currentWorkspace.name,
      status: "deleting",
      result: current?.workspaceId === currentWorkspace.id ? current.result : undefined,
    }));
    let result: WorkspaceDeletionResponse;
    try {
      result = await deletionDriver.run({
        session,
        sessionIdentity,
        workspaceId: currentWorkspace.id,
        confirmationName: currentWorkspace.name,
        isCurrent,
        onProgress: (progress) => {
          setWorkspaceDeletion({
            workspaceId: currentWorkspace.id,
            name: currentWorkspace.name,
            status: progress.status === "completed" ? "completed" : "deleting",
            result: progress,
          });
          if (isCurrent()) void leave();
        },
      });
    } catch (error) {
      // Once left, a stop waits quietly: the next reading of the list, at the
      // latest the next opening, carries the deletion on.
      if (!isCurrent() || left) return;
      stopLeavingWorkspace(currentWorkspace.id);
      const message = settingsErrorMessage(error, input.message("deleteWorkspaceFailed"));
      // A legal hold refuses before anything goes: nothing is paused, and the
      // confirmation stays open saying why.
      const refused = isLegalHoldPermanentDeleteError(error);
      setWorkspaceDeletion((current) => (refused && !current?.result ? undefined : {
        workspaceId: currentWorkspace.id,
        name: currentWorkspace.name,
        status: "retryable",
        result: current?.result,
        error: message,
      }));
      if (shown()) setDeleteWorkspaceError(message);
      setDeleteWorkspaceBusy(false);
      return;
    }
    if (!isCurrent() || result.status !== "completed") return;
    await leave();
  }, [contextKey, input, session, sessionIdentity]);

  const submitDeleteWorkspace = useCallback(async () => {
    setDeleteWorkspaceError("");
    if (!canDeleteWorkspace) {
      setDeleteWorkspaceError(workspaces.length <= 1
        ? input.message("lastWorkspaceError")
        : input.message("deleteAdminRequired"));
      return;
    }
    if (!deleteWorkspaceConfirmMatches) {
      setDeleteWorkspaceError(input.message("deleteNameMismatch"));
      return;
    }
    await driveDeletion();
  }, [canDeleteWorkspace, deleteWorkspaceConfirmMatches, driveDeletion, input, workspaces.length]);

  // A paused deletion goes on for its own workspace.
  const resumeWorkspaceDeletion = useCallback(async () => {
    if (workspaceDeletion?.status !== "retryable") return;
    await driveDeletion({ id: workspaceDeletion.workspaceId, name: workspaceDeletion.name });
  }, [driveDeletion, workspaceDeletion]);

  return {
    name, setName, domain, setDomain, workspaceError, workspaceErrorAt, domainError, setDomainError,
    domainBusy, saveName, saveDomain, updateIcon, updateLandingPage,
    icon: iconChanges.shown.icon ?? workspace.icon ?? "", iconNote: iconChanges.note("icon"),
    canDeleteWorkspace, deleteWorkspaceConfirm, setDeleteWorkspaceConfirm,
    deleteWorkspaceConfirmMatches, deleteWorkspaceBusy, deleteWorkspaceError,
    setDeleteWorkspaceError,
    workspaceDeletion, submitDeleteWorkspace, resumeWorkspaceDeletion,
  };
}
