import { useEffect, useState } from "react";
import { publicMutationService } from "@/lib/publicMutationService";
import type { Session } from "@/types";
import type { PublicMutationState } from "@/publicMutationWire.generated";

// What this tab last read, for pages named again as a preset is changed.
const known = new Map<string, boolean>();

/** Whether every member opens a page, from its access summary: the workspace's own share, one above it, an open teamspace's, or the web's. */
function everyMemberOpens(state: PublicMutationState) {
  return Boolean(state.workspacePermission || state.workspaceInheritedRole || state.teamspace?.openRole
    || state.webSharingEnabled || state.inheritedWebLink || state.site?.published);
}

/**
 * The pages, of those a preset's items name, that not every member of the
 * workspace may open, as Notion's preset editor marks them with ⚠ (seen
 * 2026-11-09): read from each page's access summary, as its share button
 * reads it. A page whose summary can't be read is marked too.
 */
export function useSidebarPresetReach(session: Session, pageIds: readonly string[]): ReadonlySet<string> {
  const [, setRead] = useState(0);
  const wanted = [...new Set(pageIds)].sort().join(" ");
  useEffect(() => {
    const controller = new AbortController();
    for (const pageId of wanted ? wanted.split(" ") : []) {
      if (known.has(pageId)) continue;
      publicMutationService.summary(session, pageId, controller.signal).then(
        ({ state }) => { known.set(pageId, everyMemberOpens(state)); setRead((count) => count + 1); },
        () => { if (!controller.signal.aborted) { known.set(pageId, false); setRead((count) => count + 1); } },
      );
    }
    return () => controller.abort();
  }, [session, wanted]);
  return new Set(pageIds.filter((pageId) => known.get(pageId) === false));
}
