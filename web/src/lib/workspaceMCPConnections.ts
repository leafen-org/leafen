import { api } from "@/api";
import { newId } from "@/lib/ids";
import {
  listAllMCPConnections,
  type MCPConnectionListContext,
  type MCPConnectionListSnapshot,
} from "@/lib/mcpConnectionListTransport";
import type { MCPConnection, MCPConnectionInput } from "@/mcpOAuthWire.generated";

export type WorkspaceMCPConnectionContext = MCPConnectionListContext & {
  workspaceId: string;
  isCurrent: () => boolean;
};

type Dependencies = {
  createMCPConnection: typeof api.createMCPConnection;
  createRequestId: () => string;
  listConnections: typeof listAllMCPConnections;
  revokeMCPConnection: typeof api.revokeMCPConnection;
};

const defaults: Dependencies = {
  createMCPConnection: api.createMCPConnection,
  createRequestId: newId,
  listConnections: listAllMCPConnections,
  revokeMCPConnection: api.revokeMCPConnection,
};

function abortContext(): never {
  throw new DOMException("Workspace MCP connection context changed.", "AbortError");
}

function assertCurrent(context: WorkspaceMCPConnectionContext) {
  if (!context.isCurrent() || context.signal.aborted) abortContext();
}

function validateCreatedConnection(
  connection: MCPConnection,
  workspaceId: string,
): MCPConnection {
  if (
    !connection || typeof connection !== "object"
    || typeof connection.id !== "string" || !connection.id
    || typeof connection.clientName !== "string" || !connection.clientName
    || !Array.isArray(connection.workspaceIds)
    || connection.workspaceIds.length !== 1
    || connection.workspaceIds[0] !== workspaceId
    || typeof connection.connectionUri !== "string" || !connection.connectionUri
  ) throw new Error("Invalid MCP connection response.");
  return connection;
}

function createKey(context: WorkspaceMCPConnectionContext) {
  return `${context.sessionIdentity}\u0000${context.workspaceId}`;
}

function revokeKey(context: WorkspaceMCPConnectionContext, connectionId: string) {
  return `${createKey(context)}\u0000${connectionId}`;
}

export function createWorkspaceMCPConnectionService(
  dependencies: Partial<Dependencies> = {},
) {
  const resolved = { ...defaults, ...dependencies };
  const createInflight = new Map<string, Promise<MCPConnectionListSnapshot>>();
  const pendingRequestIds = new Map<string, string>();
  const revokeInflight = new Map<string, Promise<MCPConnectionListSnapshot>>();

  return {
    createConnection(
      context: WorkspaceMCPConnectionContext,
      publishAccepted: (connection: MCPConnection) => void,
      // How long the key lives (a year unless chosen); it does what its
      // person may with the tools its page leaves on.
      options: Pick<MCPConnectionInput, "expiresIn"> & { clientName?: string } = {},
    ) {
      const key = `${createKey(context)}\u0000${options.expiresIn ?? ""}`;
      const current = createInflight.get(key);
      if (current) return current;
      const requestId = pendingRequestIds.get(key) ?? resolved.createRequestId();
      pendingRequestIds.set(key, requestId);
      const request = (async () => {
        assertCurrent(context);
        const response = validateCreatedConnection(await resolved.createMCPConnection(
          context.session,
          {
            requestId,
            // The screen names it in the person's language (2026-10-16: "Manual MCP token" on a Korean screen).
            clientName: options.clientName ?? "Manual MCP token",
            workspaceIds: [context.workspaceId],
            ...(options.expiresIn ? { expiresIn: options.expiresIn } : {}),
          },
          context.signal,
        ), context.workspaceId);
        pendingRequestIds.delete(key);
        assertCurrent(context);
        publishAccepted(response);
        const result = await resolved.listConnections(context);
        assertCurrent(context);
        return result;
      })();
      createInflight.set(key, request);
      const clear = () => {
        if (createInflight.get(key) === request) createInflight.delete(key);
      };
      request.then(clear, clear);
      return request;
    },

    revokeConnection(context: WorkspaceMCPConnectionContext, connectionId: string) {
      const key = revokeKey(context, connectionId);
      const current = revokeInflight.get(key);
      if (current) return current;
      const request = (async () => {
        assertCurrent(context);
        let revokeError: unknown;
        try {
          await resolved.revokeMCPConnection(
            context.session,
            connectionId,
            context.signal,
          );
        } catch (error) {
          revokeError = error;
        }
        assertCurrent(context);
        const result = await resolved.listConnections(context);
        assertCurrent(context);
        if (
          revokeError
          && !result.connections.some((connection) => (
            connection.id === connectionId && connection.status === "revoked"
          ))
        ) throw revokeError;
        return result;
      })();
      revokeInflight.set(key, request);
      const clear = () => {
        if (revokeInflight.get(key) === request) revokeInflight.delete(key);
      };
      request.then(clear, clear);
      return request;
    },
  };
}
