import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { listAllMCPConnections } from "@/lib/mcpConnectionListTransport";
import { settingsErrorMessage } from "@/lib/settingsErrors";
import {
  createWorkspaceMCPConnectionService,
  type WorkspaceMCPConnectionContext,
} from "@/lib/workspaceMCPConnections";
import type { MCPConnectionListItem } from "@/mcpConnectionListWire.generated";
import type { MCPConnectionInput } from "@/mcpOAuthWire.generated";
import type { Session, Workspace } from "@/types";
import { announceMcpConnectionsChanged } from "@/lib/mcpTokens";

type ConnectionState = {
  authorityRevision: number;
  connections: MCPConnectionListItem[];
  /** The person's connections against their limit, in every workspace. */
  capacity: { active: number; limit: number } | null;
  contextKey: string;
  createdConnectionUri: string;
  /** The connection just made, for choosing its apps at once. */
  createdConnectionId: string;
  error: string;
  /** A failed read of the list, said where it shows with a way to read again; an action's failure is `error`. */
  readFailed: string;
  loading: boolean;
  notice: string;
};

function isAbort(error: unknown) {
  return error instanceof DOMException && error.name === "AbortError";
}

export function useWorkspaceAIConnections({
  session,
  workspace,
}: {
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
  const controllers = useRef(new Set<AbortController>());
  const publicationGeneration = useRef(0);
  const connectionService = useMemo(
    () => createWorkspaceMCPConnectionService(),
    [contextKey],
  );
  const [busyActions, setBusyActions] = useState<Set<string>>(() => new Set());
  const [state, setState] = useState<ConnectionState>({
    authorityRevision: -1,
    connections: [],
    capacity: null,
    contextKey,
    createdConnectionUri: "",
    createdConnectionId: "",
    error: "",
    readFailed: "",
    loading: true,
    notice: "",
  });

  const createContext = useCallback((controller: AbortController) => {
    const generation = requestGeneration.current;
    const isCurrent = () => (
      contextKeyRef.current === contextKey
      && requestGeneration.current === generation
      && !controller.signal.aborted
    );
    const result: WorkspaceMCPConnectionContext = {
      current: () => ({
        sessionIdentity: isCurrent() ? sessionIdentity : "",
        userId: isCurrent() ? session.user.id : "",
        requestGeneration: requestGeneration.current,
      }),
      isCurrent,
      requestGeneration: generation,
      session,
      sessionIdentity,
      signal: controller.signal,
      workspaceId: workspace.id,
    };
    return result;
  }, [contextKey, session, sessionIdentity, workspace.id]);

  const startController = useCallback(() => {
    const controller = new AbortController();
    controllers.current.add(controller);
    return controller;
  }, []);

  const finishController = useCallback((controller: AbortController) => {
    controllers.current.delete(controller);
  }, []);

  const setActionBusy = useCallback((action: string, busy: boolean) => {
    setBusyActions((current) => {
      const next = new Set(current);
      if (busy) next.add(action);
      else next.delete(action);
      return next;
    });
  }, []);

  const loadConnections = useCallback(async () => {
    const controller = startController();
    const context = createContext(controller);
    const publication = ++publicationGeneration.current;
    setState((current) => current.contextKey === contextKey
      ? { ...current, error: "", loading: true }
      : {
          authorityRevision: -1,
          connections: [],
          capacity: null,
          contextKey,
          createdConnectionUri: "",
          createdConnectionId: "",
          error: "",
          readFailed: "",
          loading: true,
          notice: "",
        });
    try {
      const snapshot = await listAllMCPConnections(context);
      if (!context.isCurrent()) return;
      setState((current) => (
        current.contextKey === contextKey
        && current.authorityRevision > snapshot.authorityRevision
      ) ? current : {
          ...current,
          authorityRevision: snapshot.authorityRevision,
          connections: snapshot.connections,
          capacity: { active: snapshot.activeConnections, limit: snapshot.connectionLimit },
          contextKey,
          error: "",
          readFailed: "",
          loading: false,
        });
    } catch (error) {
      if (
        isAbort(error)
        || !context.isCurrent()
        || publicationGeneration.current !== publication
      ) return;
      setState((current) => ({
        ...current,
        contextKey,
        readFailed: settingsErrorMessage(error, t("errLoadAiConnections")),
        loading: false,
      }));
    } finally {
      finishController(controller);
    }
  }, [contextKey, createContext, finishController, startController, t]);

  // Read again from a failed read: the loading line comes back at once.
  const readAgain = useCallback(() => {
    setState((current) => ({ ...current, readFailed: "", loading: true }));
    void loadConnections();
  }, [loadConnections]);

  useEffect(() => {
    setBusyActions(new Set());
    void loadConnections();
    return () => {
      for (const controller of controllers.current) controller.abort();
      controllers.current.clear();
    };
  }, [contextKey, loadConnections]);

  const createManualConnection = useCallback(async (options: Pick<MCPConnectionInput, "expiresIn"> & { clientName?: string }) => {
    const action = "manual-token";
    const controller = startController();
    const context = createContext(controller);
    const publication = ++publicationGeneration.current;
    setActionBusy(action, true);
    setState((current) => ({
      ...current,
      createdConnectionUri: "",
      createdConnectionId: "",
      error: "",
      notice: "",
    }));
    try {
      const snapshot = await connectionService.createConnection(context, (connection) => {
        if (!context.isCurrent()) return;
        setState((current) => current.contextKey === contextKey ? {
          ...current,
          createdConnectionUri: connection.connectionUri,
          createdConnectionId: connection.id,
          notice: t("manualTokenCreated"),
        } : current);
      }, { ...options, clientName: options.clientName || t("manualKeyName") });
      if (!context.isCurrent()) return;
      setState((current) => (
        current.contextKey === contextKey
        && current.authorityRevision > snapshot.authorityRevision
      ) ? current : {
          ...current,
          authorityRevision: snapshot.authorityRevision,
          connections: snapshot.connections,
          capacity: { active: snapshot.activeConnections, limit: snapshot.connectionLimit },
          contextKey,
          error: "",
          readFailed: "",
          loading: false,
          notice: t("manualTokenCreated"),
        });
    } catch (error) {
      if (!isAbort(error) && context.isCurrent() && publicationGeneration.current === publication) {
        // The organization's key policy says why, in the reader's words.
        const refused = (error as { code?: unknown }).code === "LEAFEN_USER_KEY_POLICY";
        setState((current) => ({
          ...current,
          error: refused ? t("keyPolicyRefused") : settingsErrorMessage(error, t("errCreateMcpToken")),
        }));
      }
    } finally {
      finishController(controller);
      if (context.isCurrent()) setActionBusy(action, false);
    }
  }, [connectionService, contextKey, createContext, finishController, setActionBusy, startController, t]);

  // A connection's own actions answer with what failed (or ""), which its row
  // says under it (WorkspaceAIConnections); the section's line is the list's.
  const revokeConnection = useCallback(async (connectionId: string): Promise<string> => {
    const action = `revoke:${connectionId}`;
    const controller = startController();
    const context = createContext(controller);
    const publication = ++publicationGeneration.current;
    setActionBusy(action, true);
    setState((current) => ({ ...current, error: "", notice: "" }));
    try {
      const snapshot = await connectionService.revokeConnection(context, connectionId);
      announceMcpConnectionsChanged();
      if (!context.isCurrent()) return "";
      setState((current) => (
        current.contextKey === contextKey
        && current.authorityRevision > snapshot.authorityRevision
      ) ? current : {
          ...current,
          authorityRevision: snapshot.authorityRevision,
          connections: snapshot.connections,
          capacity: { active: snapshot.activeConnections, limit: snapshot.connectionLimit },
          contextKey,
          error: "",
          readFailed: "",
          loading: false,
          notice: t("aiConnectionRevoked"),
        });
      return "";
    } catch (error) {
      return !isAbort(error) && context.isCurrent() && publicationGeneration.current === publication
        ? settingsErrorMessage(error, t("errRevokeMcpConnection")) : "";
    } finally {
      finishController(controller);
      if (context.isCurrent()) setActionBusy(action, false);
    }
  }, [connectionService, contextKey, createContext, finishController, setActionBusy, startController, t]);

  const current = state.contextKey === contextKey ? state : {
    authorityRevision: -1,
    connections: [],
    capacity: null,
    contextKey,
    createdConnectionUri: "",
    createdConnectionId: "",
    error: "",
    readFailed: "",
    loading: true,
    notice: "",
  };
  const workspaceConnections = current.connections.filter((connection) => (
    connection.workspaceIds.includes(workspace.id)
  ));
  return {
    ...current,
    connections: workspaceConnections,
    busyActions,
    createManualConnection,
    readAgain,
    refresh: loadConnections,
    revokeConnection,
  };
}
