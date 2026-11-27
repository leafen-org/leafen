import { useCallback, useEffect, useRef, useState } from "react";
import { replaceRoute } from "@/lib/router";
import { notePageWorkspace, workspaceOfPage } from "@/lib/workspaceLeaving";
import type { Page, Workspace } from "@/types";

/**
 * Leaving a workspace this tab deleted, as Notion does: the remaining
 * workspace's home takes the address, and an address of the deleted one (the
 * back button's) says the workspace was deleted rather than offering to ask
 * for a page nobody has (2026-10-16: the deleted page's address stayed, with
 * "page not found" and a request for access).
 */
export function useDeletedWorkspaces({ pages, workspaces, leave }: {
  pages: readonly Page[];
  workspaces: readonly Workspace[];
  /** Reads the list again and closes what showed the deleted workspace. */
  leave: () => Promise<void>;
}) {
  // The list drops a workspace as soon as its deletion starts, so what was
  // in it is kept from before: each page's workspace (lib/workspaceLeaving.ts)
  // and each workspace's name.
  const workspaceNames = useRef(new Map<string, string>());
  useEffect(() => {
    for (const page of pages) notePageWorkspace(page.id, page.workspaceId);
  }, [pages]);
  useEffect(() => {
    for (const workspace of workspaces) workspaceNames.current.set(workspace.id, workspace.name);
  }, [workspaces]);
  const [deleted, setDeleted] = useState<ReadonlyMap<string, string>>(() => new Map());

  const leaveDeleted = useCallback(async (workspaceId: string) => {
    setDeleted((current) => new Map(current).set(workspaceId, workspaceNames.current.get(workspaceId) ?? ""));
    await leave();
    replaceRoute("/");
  }, [leave]);

  /** The deleted workspace's name for a page that was in it, or undefined. */
  const deletedWorkspaceName = useCallback((pageId: string | null) => {
    const workspaceId = pageId ? workspaceOfPage(pageId) : undefined;
    return workspaceId === undefined ? undefined : deleted.get(workspaceId);
  }, [deleted]);

  return { leaveDeleted, deletedWorkspaceName };
}
