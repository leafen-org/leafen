import { useEffect, useRef, useState } from "react";
import { followAcrossTabs, tellTabs } from "@/lib/followAcrossTabs";
import { listAllMCPConnections } from "@/lib/mcpConnectionListTransport";
import type { Session } from "@/types";

const CONNECTED_AGENTS_EVERY = 3000;

export type ConnectedAgent = { id: string; name: string };

// The person's active connections, as one tab tells them to the others.
type Told = { id: string; name: string; workspaceIds: string[] }[];

/**
 * The agents connected to a workspace, read as a screen opens, as the person
 * comes back to the window from their agent and every few seconds while the
 * window shows — one of the person's tabs in sight for all of them, which
 * hear its answer (followAcrossTabs): it may stand beside the agent, and say
 * at once when one connects. Null until the first read. `onFirstRead` hears
 * what the first read found, which says whether one connected before the
 * screen came.
 */
export function useConnectedAgents(session: Session, workspaceId: string,
  onFirstRead?: (agents: ConnectedAgent[]) => void): ConnectedAgent[] | null {
  const [agents, setAgents] = useState<ConnectedAgent[] | null>(null);
  const first = useRef(onFirstRead);
  first.current = onFirstRead;
  useEffect(() => {
    const controller = new AbortController();
    let firstRead = true;
    const identity = `${session.user.id}:${session.token}`;
    const topic = `mcp-connections:${session.user.id}`;
    const read = () => listAllMCPConnections({
      session, sessionIdentity: identity, requestGeneration: 0, signal: controller.signal,
      current: () => ({ sessionIdentity: identity, userId: session.user.id, requestGeneration: 0 }),
    }).then((snapshot) => {
      if (controller.signal.aborted) return;
      tellTabs(topic, snapshot.connections.filter((connection) => connection.status === "active")
        .map((connection) => ({ id: connection.id, name: connection.clientName || connection.clientId,
          workspaceIds: connection.workspaceIds })) satisfies Told);
    });
    const heard = (value: unknown) => {
      if (controller.signal.aborted || !Array.isArray(value)) return;
      const connected = (value as Told).filter((connection) => connection.workspaceIds.includes(workspaceId))
        .map((connection) => ({ id: connection.id, name: connection.name }));
      if (firstRead) first.current?.(connected);
      firstRead = false;
      setAgents((current) => current && JSON.stringify(current) === JSON.stringify(connected) ? current : connected);
    };
    const stopFollowing = followAcrossTabs(topic, CONNECTED_AGENTS_EVERY, read, heard, { now: true });
    // Coming back to the window from the agent, it reads at once.
    const comeBack = () => {
      if (document.visibilityState === "visible") void read().catch(() => undefined);
    };
    window.addEventListener("focus", comeBack);
    return () => {
      controller.abort();
      stopFollowing();
      window.removeEventListener("focus", comeBack);
    };
  }, [session, workspaceId]);
  return agents;
}
