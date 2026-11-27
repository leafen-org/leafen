import { useEffect, useState } from "react";
import { personValue, storePersonValue } from "@/lib/browserStorage";
import type { Workspace } from "@/types";

const KEPT = "sidebar-home-row";

/**
 * Whether the sidebar's home row (the phone's home tab) shows. A guest has
 * none: Notion's guest sidebar has no tabs, its shared pages right under the
 * search (seen 2026-11-12); nor has a person with no workspace. Until the
 * workspace is known the row is as it was the last time here, so the
 * sidebar's first draw doesn't push its list down when the workspace comes
 * (2026-11-15: the row came 50 ms after the list was drawn, 44px — under the
 * first screen's cover, and in plain sight once a slow start lifts it).
 */
export function useSidebarHomeRow(userId: string, workspace: Workspace | null, workspacesShown: boolean, phone: boolean): boolean {
  const known = workspace ? phone || workspace.guest !== true : workspacesShown ? false : null;
  const [kept] = useState(() => personValue(KEPT, { userId }) !== "none");
  useEffect(() => {
    if (workspace) storePersonValue(KEPT, known ? null : "none", { userId });
  }, [known, userId, workspace]);
  return known ?? (phone || kept);
}
