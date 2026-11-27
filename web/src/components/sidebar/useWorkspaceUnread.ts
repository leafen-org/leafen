import { useEffect, useMemo, useState } from "react";
import { request } from "@/apiTransport";
import { followAcrossTabs, tellTabs } from "@/lib/followAcrossTabs";
import type { Session } from "@/types";

const ASK_EVERY_MS = 60_000;
// The server takes 64 workspaces an ask; more are asked in turn.
const UNREAD_WORKSPACES_PER_ASK = 64;

/**
 * Which of the person's other workspaces hold unread personal notifications,
 * for Notion's red dot on them in the workspace switcher and on the
 * sidebar's workspace mark (seen 2026-10-06). Asked when the list changes,
 * every minute and when the window comes back; the current workspace's
 * unread shows on the sidebar's inbox instead. One of the person's tabs in
 * sight asks about all their workspaces for every tab, whichever workspace
 * each shows, and a tab behind asks nothing (followAcrossTabs; 2026-11-13:
 * every tab, hidden ones too, asked each minute).
 */
export function useWorkspaceUnread(session: Session, workspaceIds: readonly string[], currentId: string | null): ReadonlySet<string> {
  const others = workspaceIds.some((id) => id !== currentId);
  const key = others ? [...workspaceIds].sort().join(",") : "";
  const [unread, setUnread] = useState<ReadonlySet<string>>(() => new Set());
  useEffect(() => {
    if (!key) {
      setUnread(new Set());
      return undefined;
    }
    const controller = new AbortController();
    const topic = `workspace-unread:${session.user.id}:${key}`;
    const read = () => {
      const ids = key.split(",");
      const asks: Array<Promise<{ unread: string[] }>> = [];
      for (let start = 0; start < ids.length; start += UNREAD_WORKSPACES_PER_ASK) {
        const query = new URLSearchParams(ids.slice(start, start + UNREAD_WORKSPACES_PER_ASK).map((id) => ["workspaceId", id]));
        asks.push(request<{ unread: string[] }>(`/account/workspace-unread?${query}`, { signal: controller.signal }, session));
      }
      // A failed ask keeps the dots it had.
      return Promise.all(asks).then((answers) => tellTabs(topic, answers.flatMap((answer) => answer.unread)));
    };
    const stop = followAcrossTabs(topic, ASK_EVERY_MS, read, (value) => {
      if (Array.isArray(value)) setUnread(new Set(value.filter((id): id is string => typeof id === "string")));
    }, { now: true });
    return () => {
      controller.abort();
      stop();
    };
  }, [key, session]);
  return useMemo(() => {
    if (!currentId || !unread.has(currentId)) return unread;
    const shown = new Set(unread);
    shown.delete(currentId);
    return shown;
  }, [unread, currentId]);
}
