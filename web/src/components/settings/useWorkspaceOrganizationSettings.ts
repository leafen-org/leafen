import { useCallback, useEffect, useRef, useState } from "react";

import { settingsErrorMessage } from "@/lib/settingsErrors";
import {
  loadOrganizationSettingsSnapshot,
  mutateOrganizationSettings,
  type OrganizationSettingsMutation,
} from "@/lib/workspaceOrganizationSettings";
import { loadWorkspaceOrganizationContext } from "@/lib/workspaceOrganizationContext";
import type { Organization, OrganizationSettingsSnapshot } from "@/organizationWire.generated";
import type { Session, Workspace } from "@/types";

// How often the open 조직 settings ask whether the organization changed elsewhere.
const ORGANIZATION_CHANGE_CHECK_MS = 4_000;

type OrganizationSettingsState = {
  continuing: boolean;
  contextKey: string;
  error: string;
  loading: boolean;
  mutating: boolean;
  organization: Organization | null;
  /** Reading again after a change, with the last snapshot still shown. */
  refreshing: boolean;
  requestGeneration: number;
  signal: AbortSignal | null;
  snapshot: OrganizationSettingsSnapshot | null;
};

export function useWorkspaceOrganizationSettings({
  active,
  view,
  errorFallback,
  session,
  workspace,
}: {
  active: boolean;
  /**
   * The settings item showing: each one reads again as it opens, as the
   * enterprise items do (2026-11-05: people SCIM added while 신원 was open
   * were missing from 조직 until the window opened again).
   */
  view: string;
  errorFallback: string;
  session: Session;
  workspace: Workspace;
}) {
  const sessionIdentity = JSON.stringify([session.user.id]);
  const contextKey = JSON.stringify([sessionIdentity, workspace.id, workspace.organizationId]);
  const openView = active ? view : "";
  const contextKeyRef = useRef(contextKey);
  const requestGeneration = useRef(0);
  const controllerRef = useRef<AbortController | null>(null);
  const snapshotIdentity = useRef("");
  if (contextKeyRef.current !== contextKey) {
    contextKeyRef.current = contextKey;
    requestGeneration.current += 1;
    snapshotIdentity.current = "";
  }
  const [refreshGeneration, setRefreshGeneration] = useState(0);
  const [state, setState] = useState<OrganizationSettingsState>({
    continuing: false,
    contextKey,
    error: "",
    loading: true,
    mutating: false,
    organization: null,
    refreshing: false,
    requestGeneration: requestGeneration.current,
    signal: null,
    snapshot: null,
  });

  useEffect(() => {
    const controller = new AbortController();
    controllerRef.current?.abort();
    controllerRef.current = controller;
    const generation = ++requestGeneration.current;
    snapshotIdentity.current = "";
    // Reading again in the same place keeps what's shown, its controls held,
    // so the settings stay where the person was (a change doesn't throw the
    // window back to its top); a new place starts empty.
    setState((current) => {
      const keep = current.contextKey === contextKey && current.snapshot !== null;
      return {
        continuing: false,
        contextKey,
        error: "",
        loading: !keep,
        mutating: false,
        organization: current.contextKey === contextKey ? current.organization : null,
        refreshing: keep,
        requestGeneration: generation,
        signal: controller.signal,
        snapshot: keep ? current.snapshot : null,
      };
    });
    const current = () => ({
      sessionIdentity,
      organizationId: workspace.organizationId,
      workspaceId: workspace.id,
      requestGeneration: generation,
    });
    const isCurrent = () => (
      !controller.signal.aborted
      && contextKeyRef.current === contextKey
      && requestGeneration.current === generation
    );
    void loadWorkspaceOrganizationContext(session, workspace.organizationId, controller.signal)
      .then(async (organization) => {
        if (!isCurrent()) return;
        if (!active || !organization?.capabilities?.canReadSettings) {
          setState({
            continuing: false,
            contextKey,
            error: "",
            loading: false,
            mutating: false,
            organization,
            refreshing: false,
            requestGeneration: generation,
            signal: controller.signal,
            snapshot: null,
          });
          return;
        }
        const snapshot = await loadOrganizationSettingsSnapshot(session, {
          sessionIdentity,
          organizationId: workspace.organizationId,
          workspaceId: workspace.id,
          requestGeneration: generation,
          signal: controller.signal,
          current,
          publish: (snapshot) => {
            if (!isCurrent()) return;
            snapshotIdentity.current = snapshot.snapshotIdentity;
            setState({
              continuing: snapshot.hasMore,
              contextKey,
              error: "",
              loading: false,
              mutating: false,
              organization,
              refreshing: false,
              requestGeneration: generation,
              signal: controller.signal,
              snapshot,
            });
          },
        });
        if (!isCurrent()) return;
        snapshotIdentity.current = snapshot.snapshotIdentity;
        setState({
          continuing: false,
          contextKey,
          error: "",
          loading: false,
          mutating: false,
          organization,
          refreshing: false,
          requestGeneration: generation,
          signal: controller.signal,
          snapshot,
        });
      })
      .catch((error: unknown) => {
        if (!isCurrent()) return;
        setState((currentState) => ({
          ...currentState,
          continuing: false,
          contextKey,
          error: settingsErrorMessage(error, errorFallback),
          loading: false,
          mutating: false,
          refreshing: false,
          requestGeneration: generation,
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
  ]);

  const visible = state.contextKey === contextKey ? state : {
    ...state,
    contextKey,
    loading: true,
    organization: null,
    refreshing: false,
    snapshot: null,
  };
  const isCurrent = useCallback((identity = snapshotIdentity.current) => (
    !controllerRef.current?.signal.aborted
    && contextKeyRef.current === contextKey
    && snapshotIdentity.current === identity
  ), [contextKey]);
  const refresh = useCallback(() => setRefreshGeneration((value) => value + 1), []);
  // What changes elsewhere shows in the open members list within seconds,
  // as the page list follows others' changes (2026-11-07: a person SCIM
  // turned off or on stayed as they were until 조직 opened again). While the
  // window shows the organization and is in view, it asks every few seconds
  // for the organization's policy version — which SCIM's changes to people
  // and groups move — and its settings revision, which every change to its
  // members, roles, groups, domains and policies moves, another admin's in
  // another window too, and reads again when either moved past what's shown,
  // keeping what's shown.
  const policyVersion = visible.organization?.policyVersion;
  const settingsRevision = visible.snapshot?.organization.revision;
  const busy = visible.loading || visible.mutating || visible.continuing || visible.refreshing;
  useEffect(() => {
    if (openView !== "organization" || policyVersion === undefined || busy) return;
    const controller = new AbortController();
    let asking = false;
    const timer = window.setInterval(() => {
      if (asking || document.visibilityState !== "visible") return;
      asking = true;
      void loadWorkspaceOrganizationContext(session, workspace.organizationId, controller.signal)
        .then((organization) => {
          if (controller.signal.aborted || !organization) return;
          const moved = organization.policyVersion !== policyVersion
            || (settingsRevision !== undefined && organization.settingsRevision !== settingsRevision);
          if (moved) refresh();
        }, () => undefined)
        .finally(() => { asking = false; });
    }, ORGANIZATION_CHANGE_CHECK_MS);
    return () => {
      controller.abort();
      window.clearInterval(timer);
    };
  }, [busy, openView, policyVersion, refresh, session, settingsRevision, workspace.organizationId]);
  const mutate = useCallback(async (mutation: OrganizationSettingsMutation) => {
    const snapshot = visible.snapshot;
    const controller = controllerRef.current;
    if (visible.continuing || !snapshot || !controller || !isCurrent(snapshot.snapshotIdentity)) return;
    setState((current) => ({ ...current, error: "", mutating: true }));
    try {
      await mutateOrganizationSettings(
        session,
        workspace.organizationId,
        snapshot.organization.revision,
        mutation,
        controller.signal,
      );
      if (isCurrent(snapshot.snapshotIdentity)) refresh();
    } catch (error) {
      if (!isCurrent(snapshot.snapshotIdentity)) return;
      setState((current) => ({
        ...current,
        error: settingsErrorMessage(error, errorFallback),
        mutating: false,
      }));
    }
  }, [errorFallback, isCurrent, refresh, session, visible.continuing, visible.snapshot, workspace.organizationId]);

  return {
    ...visible,
    canReadSettings: Boolean(visible.organization?.capabilities?.canReadSettings),
    isCurrent,
    mutate,
    mutating: visible.mutating || visible.continuing || visible.refreshing,
    refresh,
    requestGeneration: visible.requestGeneration,
    sessionIdentity,
  };
}
