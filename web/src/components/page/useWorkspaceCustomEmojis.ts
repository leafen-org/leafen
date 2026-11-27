import { useEffect, useState } from "react";
import { loadSession } from "@/lib/authSessionStore";
import { listWorkspaceCustomEmojis, type WorkspaceCustomEmoji } from "@/lib/customEmoji";

/**
 * The workspace's custom emoji for the emoji tab, read again as one is
 * added, and whether the reader may add one.
 */
export function useWorkspaceCustomEmojis(workspaceId: string | undefined, refreshKey: number) {
  const [items, setItems] = useState<WorkspaceCustomEmoji[]>([]);
  const [canAdd, setCanAdd] = useState(true);
  useEffect(() => {
    const session = loadSession();
    if (!workspaceId || !session) {
      setItems([]);
      return;
    }
    const controller = new AbortController();
    listWorkspaceCustomEmojis(session, workspaceId, { signal: controller.signal }).then(
      (response) => {
        if (controller.signal.aborted) return;
        setItems(response.items);
        setCanAdd(response.canAdd !== false);
      },
      () => {
        // The emoji tab keeps its other groups without the workspace's.
      },
    );
    return () => controller.abort();
  }, [refreshKey, workspaceId]);
  return { items, canAdd };
}
