import { request } from "@/apiTransport";
import type { Session } from "@/types";

/** An agent key that reaches a workspace, for its owner: never its value. */
export type WorkspaceAgentKey = {
  id: string;
  name: string;
  person: { id: string; name: string };
  /** An agent connection — its person's in all their workspaces — rather than an MCP token. */
  agent: boolean;
  /** An OAuth connection renews itself while its agent uses it. */
  oauth: boolean;
  createdAt: number;
  lastUsedAt?: number;
  expiresAt?: number;
  status: "active" | "expired";
};

function connectionsPath(workspaceId: string) {
  return `/workspaces/${encodeURIComponent(workspaceId)}/connections`;
}

/** Every agent key that reaches the workspace (internal/app/workspace_agent_keys.go). */
export function listWorkspaceAgentKeys(session: Session, workspaceId: string, signal?: AbortSignal) {
  return request<{ items: WorkspaceAgentKey[] }>(`${connectionsPath(workspaceId)}?kind=agent-keys`, { signal }, session);
}

/** Ends a key that reaches the workspace, as its owner. */
export function disconnectWorkspaceAgentKey(session: Session, workspaceId: string, keyId: string) {
  return request<void>(`${connectionsPath(workspaceId)}/${encodeURIComponent(keyId)}/disconnect?kind=agent-key`, { method: "POST" }, session);
}
