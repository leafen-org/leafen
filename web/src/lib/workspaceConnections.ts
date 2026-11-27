import { request } from "@/apiTransport";
import type { Session } from "@/types";

/** A connection added to a workspace: an app that reached it through the Notion-compatible OAuth. */
export type WorkspaceConnection = {
  id: string;
  name: string;
  installedBy: { id: string; name: string };
  createdAt: number;
  /** The pages chosen when it was added; 0 means every page its person reaches. */
  pages: number;
  canDisconnect: boolean;
};

export type WorkspaceConnections = {
  items: WorkspaceConnection[];
  /** With it, more follow. */
  nextCursor?: string;
  /** Only for the workspace's owner. */
  manage?: { connections: number; activePersonalAccessTokens: number };
};

function connectionsPath(workspaceId: string) {
  return `/workspaces/${encodeURIComponent(workspaceId)}/connections`;
}

function listWorkspaceConnections(session: Session, workspaceId: string, signal?: AbortSignal, cursor?: string) {
  return request<WorkspaceConnections>(
    cursor ? `${connectionsPath(workspaceId)}?cursor=${encodeURIComponent(cursor)}` : connectionsPath(workspaceId),
    { signal },
    session,
  );
}

/** Every connection, a window at a time, with the owner's counts from the first. */
export async function listAllWorkspaceConnections(session: Session, workspaceId: string, signal?: AbortSignal) {
  const first = await listWorkspaceConnections(session, workspaceId, signal);
  const items = [...first.items];
  let cursor = first.nextCursor;
  while (cursor && !signal?.aborted) {
    const page = await listWorkspaceConnections(session, workspaceId, signal, cursor);
    items.push(...page.items);
    cursor = page.nextCursor;
  }
  return { ...first, items, nextCursor: undefined };
}

export function disconnectWorkspaceConnection(session: Session, workspaceId: string, connectionId: string) {
  return request<void>(
    `${connectionsPath(workspaceId)}/${encodeURIComponent(connectionId)}/disconnect`,
    { method: "POST" },
    session,
  );
}
