import { useCallback, useEffect, useMemo, useRef, useState } from "react";

import type { EnterpriseOperationContext, EnterpriseSettingsSnapshot } from "@/lib/enterpriseSettingsAdapter";
import {
  canMutateOrganizationSharingSecurity,
  canViewEnterpriseSettings,
  enterpriseAreas,
} from "@/lib/organizationSpecialistRolePolicy";
import { settingsErrorMessage } from "@/lib/settingsErrors";
import {
  createNotionAdminService,
  createWorkspaceEnterpriseSettingsService,
  loadWorkspaceEnterpriseSettingsSnapshot,
} from "@/lib/workspaceEnterpriseSettings";
import { loadWorkspaceOrganizationContext } from "@/lib/workspaceOrganizationContext";
import type { Organization } from "@/organizationWire.generated";
import type { Session, Workspace } from "@/types";

type EnterpriseHookState = {
  contextKey: string;
  error: string;
  loading: boolean;
  organization: Organization | null;
  requestGeneration: number;
  signal: AbortSignal | null;
  snapshot: EnterpriseSettingsSnapshot | null;
};

export function useWorkspaceEnterpriseSettings({
  active,
  view,
  errorFallback,
  session,
  workspace,
  workspaces,
}: {
  active: boolean;
  /**
   * The settings item showing: each one reads what is saved as it opens
   * (2026-11-05: back from 보안, 엔터프라이즈 제어 showed the switches as they
   * were on opening the window, and the next switch saved those over the newer).
   */
  view: string;
  errorFallback: string;
  session: Session;
  workspace: Workspace;
  workspaces: readonly Workspace[];
}) {
  const sessionIdentity = JSON.stringify([session.user.id]);
  const contextKey = JSON.stringify([
    sessionIdentity,
    workspace.id,
    workspace.organizationId,
  ]);
  const openView = active ? view : "";
  const workspaceSetIdentity = active
    ? JSON.stringify(workspaces.map(({ id, name, organizationId }) => [id, name, organizationId]))
    : "inactive";
  const contextKeyRef = useRef(contextKey);
  const generationRef = useRef(0);
  const controllerRef = useRef<AbortController | null>(null);
  const currentSnapshotRef = useRef<EnterpriseSettingsSnapshot | null>(null);
  const inactiveControllerRef = useRef<AbortController | null>(null);
  if (!inactiveControllerRef.current) inactiveControllerRef.current = new AbortController();
  if (contextKeyRef.current !== contextKey) {
    contextKeyRef.current = contextKey;
    generationRef.current += 1;
    currentSnapshotRef.current = null;
  }
  const [refreshGeneration, setRefreshGeneration] = useState(0);
  const [state, setState] = useState<EnterpriseHookState>({
    contextKey,
    error: "",
    loading: true,
    organization: null,
    requestGeneration: generationRef.current,
    signal: null,
    snapshot: null,
  });

  useEffect(() => {
    const controller = new AbortController();
    controllerRef.current?.abort();
    controllerRef.current = controller;
    const requestGeneration = ++generationRef.current;
    currentSnapshotRef.current = null;
    // Read again for the same workspace (after a save), the section keeps
    // what it shows until the answer comes: drawn anew it lost its scroll and
    // the switch that was pressed (2026-11-07).
    setState((current) => {
      const same = current.contextKey === contextKey;
      return {
        contextKey,
        error: "",
        loading: !(same && current.snapshot),
        organization: same ? current.organization : null,
        requestGeneration,
        signal: controller.signal,
        snapshot: same ? current.snapshot : null,
      };
    });

    const current = () => ({
      sessionIdentity,
      organizationId: workspace.organizationId,
      workspaceId: workspace.id,
      requestGeneration,
    });
    const isReadCurrent = () => (
      !controller.signal.aborted
      && contextKeyRef.current === contextKey
      && generationRef.current === requestGeneration
    );

    void loadWorkspaceOrganizationContext(session, workspace.organizationId, controller.signal)
      .then(async (organization) => {
        if (!isReadCurrent()) return;
        if (!active || !canViewEnterpriseSettings(organization)) {
          setState({
            contextKey,
            error: "",
            loading: false,
            organization,
            requestGeneration,
            signal: controller.signal,
            snapshot: null,
          });
          return;
        }
        const snapshot = await loadWorkspaceEnterpriseSettingsSnapshot(session, workspaces, {
          sessionIdentity,
          organizationId: workspace.organizationId,
          workspaceId: workspace.id,
          requestGeneration,
          signal: controller.signal,
          areas: enterpriseAreas(organization),
          current,
        });
        if (!isReadCurrent()) return;
        currentSnapshotRef.current = snapshot;
        setState({
          contextKey,
          error: "",
          loading: false,
          organization,
          requestGeneration,
          signal: controller.signal,
          snapshot,
        });
      })
      .catch((error: unknown) => {
        if (!isReadCurrent()) return;
        setState((currentState) => ({
          contextKey,
          error: settingsErrorMessage(error, errorFallback),
          loading: false,
          organization: currentState.contextKey === contextKey
            ? currentState.organization
            : null,
          requestGeneration,
          signal: controller.signal,
          snapshot: null,
        }));
      });
    return () => {
      controller.abort();
      if (controllerRef.current === controller) controllerRef.current = null;
    };
  }, [
    active,
    openView,
    contextKey,
    errorFallback,
    refreshGeneration,
    session,
    sessionIdentity,
    workspace.id,
    workspace.organizationId,
    workspaceSetIdentity,
  ]);

  const visibleState = state.contextKey === contextKey ? state : {
    contextKey,
    error: "",
    loading: true,
    organization: null,
    requestGeneration: generationRef.current,
    signal: null,
    snapshot: null,
  };
  const isCurrent = useCallback((operation: EnterpriseOperationContext) => (
    !operation.signal.aborted
    && contextKeyRef.current === contextKey
    && generationRef.current === operation.requestGeneration
    && operation.sessionIdentity === sessionIdentity
    && operation.organizationId === workspace.organizationId
    && currentSnapshotRef.current?.snapshotIdentity === operation.snapshotIdentity
  ), [contextKey, sessionIdentity, workspace.organizationId]);
  const service = useMemo(
    () => createWorkspaceEnterpriseSettingsService(session, isCurrent),
    [isCurrent, session],
  );
  const signal = visibleState.signal ?? inactiveControllerRef.current.signal;
  const notionAdmin = useMemo(() => createNotionAdminService(
    session,
    workspace.organizationId,
    signal,
    () => (
      !signal.aborted
      && contextKeyRef.current === contextKey
      && generationRef.current === visibleState.requestGeneration
    ),
  ), [contextKey, session, signal, visibleState.requestGeneration, workspace.organizationId]);
  const refresh = useCallback(() => setRefreshGeneration((value) => value + 1), []);

  return {
    ...visibleState,
    canViewEnterprise: canViewEnterpriseSettings(visibleState.organization),
    canManageSecurity: canMutateOrganizationSharingSecurity(visibleState.organization),
    areas: enterpriseAreas(visibleState.organization),
    isCurrent,
    notionAdmin,
    refresh,
    service,
    sessionIdentity,
    signal,
  };
}
