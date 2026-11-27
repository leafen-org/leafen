import { useCallback, useEffect, useRef, useState } from "react";
import { api } from "@/api";
import { LeafenApiError } from "@/apiTransport";
import type { FileUsageReportResponse } from "@/fileUsageReportWire.generated";
import { settingsErrorMessage } from "@/lib/settingsErrors";
import {
  loadOrganizationFileUsageReport,
  loadWorkspaceFileUsageReport,
} from "@/lib/workspaceFileUsageReport";
import { createWorkspaceFileManualCleanupDriver } from "@/lib/workspaceFileManualCleanup";
import { loadWorkspaceOrganizationContext } from "@/lib/workspaceOrganizationContext";
import type { Organization } from "@/organizationWire.generated";
import type { Session, Workspace } from "@/types";
import { createOrganizationStoragePolicyMutationDriver } from "./organizationStoragePolicyMutationDriver";

// How long a screen waits for another of the same report, and how often it asks.
const USAGE_REPORT_WAIT_MS = 3_000;
const USAGE_REPORT_WAIT_ATTEMPTS = 50;
const USAGE_REPORT_WAITED_CODES = new Set(["LEAFEN_FILE_USAGE_REPORT_UNAVAILABLE", "LEAFEN_FILE_USAGE_REPORT_CONTINUATION_INVALID"]);

type UsageState = {
  contextKey: string;
  error: string;
  loading: boolean;
  organization: Organization | null;
  organizationReport: FileUsageReportResponse | null;
  report: FileUsageReportResponse | null;
};

// The organization's usage is the owner's and its billing admins' (Notion's
// admin roles, 2026-10-04).
function canReadOrganizationUsage(organization: Organization | null): organization is Organization {
  return organization?.capabilities?.canManageBilling === true;
}

const storagePolicyMutationDriver = createOrganizationStoragePolicyMutationDriver({
  updateOrganizationStorageLimit: api.updateOrganizationStorageLimit,
});
const fileCleanupDriver = createWorkspaceFileManualCleanupDriver({
  start: api.startWorkspaceFileManualCleanup,
  read: api.readWorkspaceFileManualCleanup,
});

type UsageSnapshot = Pick<UsageState, "organization" | "organizationReport" | "report">;

async function loadUsageSnapshot(
  session: Session,
  workspace: Workspace,
  signal: AbortSignal,
): Promise<UsageSnapshot> {
  const workspaceReport = loadWorkspaceFileUsageReport(session, workspace.id, signal);
  const organizationContext = loadWorkspaceOrganizationContext(
    session,
    workspace.organizationId,
    signal,
  );
  const [report, organization] = await Promise.all([workspaceReport, organizationContext]);
  const organizationReport = canReadOrganizationUsage(organization)
    ? await loadOrganizationFileUsageReport(session, organization.id, signal)
    : null;
  return { organization, organizationReport, report };
}

function obsoleteUsageMutation() {
  return new DOMException("Workspace Usage context changed.", "AbortError");
}

export function useWorkspaceUsageReport({
  session,
  workspace,
  errorFallback,
}: {
  session: Session;
  workspace: Workspace;
  errorFallback: string;
}) {
  const sessionIdentity = session.user.id;
  const contextKey = `${session.user.id}\u0000${workspace.id}\u0000${workspace.organizationId}`;
  const generation = useRef(0);
  const contextKeyRef = useRef(contextKey);
  if (contextKeyRef.current !== contextKey) {
    contextKeyRef.current = contextKey;
    generation.current += 1;
  }
  const controllerRef = useRef<AbortController | null>(null);
  const [refreshGeneration, setRefreshGeneration] = useState(0);
  const [cleanupBusy, setCleanupBusy] = useState(false);
  const [storageLimitBusy, setStorageLimitBusy] = useState(false);
  const [state, setState] = useState<UsageState>({
    contextKey,
    error: "",
    loading: true,
    organization: null,
    organizationReport: null,
    report: null,
  });

  useEffect(() => {
    const controller = new AbortController();
    controllerRef.current?.abort();
    controllerRef.current = controller;
    const currentGeneration = ++generation.current;
    setCleanupBusy(false);
    setStorageLimitBusy(false);
    setState((current) => current.contextKey === contextKey
      ? { ...current, error: "", loading: true }
      : {
          contextKey,
          error: "",
          loading: true,
          organization: null,
          organizationReport: null,
          report: null,
        });
    // Another of the same report being made — another person's screen, or
    // one abandoned, which the server lets go after two minutes — is waited
    // for, and asked again; a report of this screen's that the server let go
    // so is made anew (2026-10-16: an abandoned report kept the screen at
    // "couldn't load" for good).
    const loadWaiting = async () => {
      for (let attempt = 0; ; attempt += 1) {
        try {
          return await loadUsageSnapshot(session, workspace, controller.signal);
        } catch (error) {
          if (attempt >= USAGE_REPORT_WAIT_ATTEMPTS || controller.signal.aborted
            || !(error instanceof LeafenApiError && USAGE_REPORT_WAITED_CODES.has(error.code))) throw error;
          await new Promise((resolve) => {
            const timer = window.setTimeout(resolve, USAGE_REPORT_WAIT_MS);
            controller.signal.addEventListener("abort", () => { window.clearTimeout(timer); resolve(undefined); }, { once: true });
          });
        }
      }
    };
    void loadWaiting()
      .then((snapshot) => {
        if (
          controller.signal.aborted
          || generation.current !== currentGeneration
          || contextKeyRef.current !== contextKey
        ) return;
        if (snapshot.organization) {
          storagePolicyMutationDriver.seed(sessionIdentity, snapshot.organization);
        }
        setState({
          contextKey,
          error: "",
          loading: false,
          ...snapshot,
        });
      })
      .catch((error) => {
        if (
          controller.signal.aborted
          || generation.current !== currentGeneration
          || contextKeyRef.current !== contextKey
        ) return;
        setState((current) => ({
          contextKey,
          error: settingsErrorMessage(error, errorFallback),
          loading: false,
          organization: current.contextKey === contextKey ? current.organization : null,
          organizationReport: current.contextKey === contextKey ? current.organizationReport : null,
          report: current.contextKey === contextKey ? current.report : null,
        }));
      });
    return () => {
      controller.abort();
      if (controllerRef.current === controller) controllerRef.current = null;
    };
  }, [
    contextKey,
    errorFallback,
    refreshGeneration,
    session,
    sessionIdentity,
    workspace.id,
    workspace.organizationId,
  ]);

  const updateStorageLimit = useCallback(async (storageLimitBytes: number | null) => {
    const organization = state.contextKey === contextKey ? state.organization : null;
    if (!canReadOrganizationUsage(organization)) return null;
    const controller = new AbortController();
    controllerRef.current?.abort();
    controllerRef.current = controller;
    const currentGeneration = ++generation.current;
    const isCurrent = () => (
      contextKeyRef.current === contextKey
      && generation.current === currentGeneration
      && !controller.signal.aborted
    );
    setStorageLimitBusy(true);
    try {
      storagePolicyMutationDriver.seed(sessionIdentity, organization);
      await storagePolicyMutationDriver.mutate({
        session,
        sessionIdentity,
        organizationId: organization.id,
        policyVersion: organization.policyVersion,
        storageLimitBytes,
        signal: controller.signal,
        isCurrent,
      });
      const snapshot = await loadUsageSnapshot(session, workspace, controller.signal);
      if (!isCurrent()) throw obsoleteUsageMutation();
      if (snapshot.organization) {
        storagePolicyMutationDriver.seed(sessionIdentity, snapshot.organization);
      }
      setState({ contextKey, error: "", loading: false, ...snapshot });
      return snapshot.organization;
    } finally {
      if (isCurrent()) setStorageLimitBusy(false);
    }
  }, [contextKey, session, sessionIdentity, state, workspace]);

  const runCleanup = useCallback(async () => {
    const controller = new AbortController();
    controllerRef.current?.abort();
    controllerRef.current = controller;
    const currentGeneration = ++generation.current;
    const isCurrent = () => (
      contextKeyRef.current === contextKey
      && generation.current === currentGeneration
      && !controller.signal.aborted
    );
    setCleanupBusy(true);
    try {
      const response = await fileCleanupDriver.run(session, workspace.id, controller.signal);
      if (!isCurrent()) throw obsoleteUsageMutation();
      const snapshot = await loadUsageSnapshot(session, workspace, controller.signal);
      if (!isCurrent()) throw obsoleteUsageMutation();
      if (snapshot.organization) {
        storagePolicyMutationDriver.seed(sessionIdentity, snapshot.organization);
      }
      setState({ contextKey, error: "", loading: false, ...snapshot });
      return response.job;
    } finally {
      if (isCurrent()) setCleanupBusy(false);
    }
  }, [contextKey, session, sessionIdentity, workspace]);

  const refresh = useCallback(() => setRefreshGeneration((value) => value + 1), []);
  return state.contextKey === contextKey
    ? { ...state, cleanupBusy, refresh, runCleanup, storageLimitBusy, updateStorageLimit }
    : {
        contextKey,
        error: "",
        loading: true,
        organization: null,
        organizationReport: null,
        report: null,
        cleanupBusy: false,
        refresh,
        runCleanup,
        storageLimitBusy: false,
        updateStorageLimit,
      };
}
