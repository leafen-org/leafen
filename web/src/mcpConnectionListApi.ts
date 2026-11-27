import type { Session } from "./types";
import {
  mcpConnectionListBounds,
  mcpConnectionListOperations,
  type MCPConnectionListResponse,
} from "./mcpConnectionListWire.generated";
import {
  mcpOAuthOperations,
  type MCPConnection,
  type MCPConnectionCode,
  type MCPConnectionCodeInput,
  type MCPConnectionInput,
} from "./mcpOAuthWire.generated";

type JsonRequest = <T>(
  path: string,
  init?: RequestInit,
  session?: Session | null,
) => Promise<T>;

export function createMCPConnectionListApi(request: JsonRequest) {
  return {
    createMCPConnection: (
      session: Session,
      input: MCPConnectionInput,
      signal?: AbortSignal,
    ) => request<MCPConnection>(
      mcpOAuthOperations.createConnection.path,
      {
        method: mcpOAuthOperations.createConnection.method,
        body: JSON.stringify(input),
        signal,
      },
      session,
    ),
    /** A one-time code an agent trades for a key, for agents without OAuth. */
    createMCPConnectionCode: (
      session: Session,
      input: MCPConnectionCodeInput,
      signal?: AbortSignal,
    ) => request<MCPConnectionCode>(
      mcpOAuthOperations.createConnectionCode.path,
      {
        method: mcpOAuthOperations.createConnectionCode.method,
        body: JSON.stringify(input),
        signal,
      },
      session,
    ),
    listMCPConnections: (
      session: Session,
      cursor?: string,
      limit: number = mcpConnectionListBounds.defaultPageSize,
      signal?: AbortSignal,
    ) => {
      const parameters = new URLSearchParams({ limit: String(limit) });
      if (cursor) parameters.set("cursor", cursor);
      return request<MCPConnectionListResponse>(
        `${mcpConnectionListOperations.list.path}?${parameters.toString()}`,
        { method: mcpConnectionListOperations.list.method, signal },
        session,
      );
    },
    revokeMCPConnection: (
      session: Session,
      connectionId: string,
      signal?: AbortSignal,
    ) => request<void>(
      mcpOAuthOperations.revokeConnection.path.replace(
        "{id}",
        encodeURIComponent(connectionId),
      ),
      { method: mcpOAuthOperations.revokeConnection.method, signal },
      session,
    ),
  };
}
