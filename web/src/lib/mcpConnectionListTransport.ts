import { api } from "@/api";
import {
  mcpConnectionListBounds,
  type MCPConnectionListItem,
  type MCPConnectionListResponse,
} from "@/mcpConnectionListWire.generated";
import type { Session } from "@/types";

export type MCPConnectionListContext = {
  session: Session;
  sessionIdentity: string;
  requestGeneration: number;
  signal: AbortSignal;
  current: () => {
    sessionIdentity: string;
    userId: string;
    requestGeneration: number;
  };
};

export type MCPConnectionListSnapshot = {
  authorityRevision: number;
  connections: MCPConnectionListItem[];
  // The person's connections that count toward the limit, in every workspace.
  activeConnections: number;
  connectionLimit: number;
};

const statuses = new Set(["active", "revoked"]);
// A connection carries one scope: "mcp" for an agent, or the content-access
// scope an app's OAuth grant was given. The server sends exactly one of these.
const connectionScopes = new Set(["mcp", "read", "content", "structure", "admin"]);

function abortRead(message: string): never {
  throw new DOMException(message, "AbortError");
}

function isCurrentMCPConnectionList(context: MCPConnectionListContext) {
  const current = context.current();
  return !context.signal.aborted
    && current.sessionIdentity === context.sessionIdentity
    && current.userId === context.session.user.id
    && current.requestGeneration === context.requestGeneration;
}

function assertCurrent(context: MCPConnectionListContext) {
  if (!isCurrentMCPConnectionList(context)) {
    throw context.signal.reason ?? abortRead("MCP connection-list context changed.");
  }
}

function validateConnection(item: MCPConnectionListItem, seenIds: Set<string>) {
  if (
    !item || typeof item !== "object"
    || typeof item.id !== "string" || !item.id || seenIds.has(item.id)
    || typeof item.clientId !== "string" || !item.clientId
    || typeof item.clientName !== "string" || !item.clientName
    || typeof item.resource !== "string"
    || !Array.isArray(item.scopes) || item.scopes.length !== 1 || !connectionScopes.has(item.scopes[0])
    || !Array.isArray(item.workspaceIds) || item.workspaceIds.length < 1
    || new Set(item.workspaceIds).size !== item.workspaceIds.length
    || item.workspaceIds.some((workspaceId) => typeof workspaceId !== "string" || !workspaceId)
    || !statuses.has(item.status)
    || !Number.isSafeInteger(item.createdAt) || item.createdAt <= 0
    || (item.expiresAt !== null && (!Number.isSafeInteger(item.expiresAt) || item.expiresAt <= 0))
    || (item.revokedAt !== null && (!Number.isSafeInteger(item.revokedAt) || item.revokedAt <= 0))
  ) throw new Error("Invalid MCP connection-list response.");
  seenIds.add(item.id);
}

function validatePage(response: MCPConnectionListResponse) {
  if (
    !response || typeof response !== "object"
    || !Number.isSafeInteger(response.authorityRevision) || response.authorityRevision < 0
    || !Array.isArray(response.connections)
    || response.connections.length > mcpConnectionListBounds.maximumPageSize
    || !Number.isSafeInteger(response.activeConnections) || response.activeConnections < 0
    || !Number.isSafeInteger(response.connectionLimit) || response.connectionLimit < 0
    || (response.nextCursor !== null && (
      typeof response.nextCursor !== "string" || !response.nextCursor
      || response.nextCursor.length > mcpConnectionListBounds.cursorBytes
    ))
  ) throw new Error("Invalid MCP connection-list page.");
}

export async function listAllMCPConnections(
  context: MCPConnectionListContext,
): Promise<MCPConnectionListSnapshot> {
  assertCurrent(context);
  const connections: MCPConnectionListItem[] = [];
  const seenCursors = new Set<string>();
  const seenIds = new Set<string>();
  let cursor: string | undefined;
  let authorityRevision: number | null = null;
  for (;;) {
    const cursorIdentity = cursor ?? "";
    if (seenCursors.has(cursorIdentity)) throw new Error("MCP connection-list cursor repeated.");
    seenCursors.add(cursorIdentity);
    const response = await api.listMCPConnections(
      context.session,
      cursor,
      mcpConnectionListBounds.maximumPageSize,
      context.signal,
    );
    assertCurrent(context);
    validatePage(response);
    if (authorityRevision === null) authorityRevision = response.authorityRevision;
    else if (response.authorityRevision !== authorityRevision) {
      throw new Error("MCP connection-list authority changed during continuation.");
    }
    for (const connection of response.connections) {
      validateConnection(connection, seenIds);
      connections.push(connection);
    }
    if (!response.nextCursor) {
      assertCurrent(context);
      return {
        authorityRevision, connections,
        activeConnections: response.activeConnections, connectionLimit: response.connectionLimit,
      };
    }
    cursor = response.nextCursor;
  }
}
