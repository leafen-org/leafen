// A workspace this tab is deleting is left from the moment the person
// confirms. The server closes its pages' connections and its change socket
// as the deletion starts, often before the tab hears that it took it, and
// those closings would read the gone workspace again — its page's storage
// status and the workspace's change check, each a 404 (2026-11-10: five
// such reads as the tab moved home). Nothing in a workspace being left is
// asked about; a deletion the server refuses before the tab leaves gives it
// back. Each page's workspace is noted as the page list shows it, for the
// pages' own connections, which know only their page.

const leaving = new Set<string>();
const pageWorkspaces = new Map<string, string>();

/** Notes which workspace a page is in, as the page list says. */
export function notePageWorkspace(pageId: string, workspaceId: string) {
  pageWorkspaces.set(pageId, workspaceId);
}

/** The workspace a page was last seen in, if the list showed it. */
export function workspaceOfPage(pageId: string): string | undefined {
  return pageWorkspaces.get(pageId);
}

/** The person confirmed this workspace's deletion: the tab leaves it. */
export function startLeavingWorkspace(workspaceId: string) {
  leaving.add(workspaceId);
}

/** The deletion was refused before the tab left: the workspace is its again. */
export function stopLeavingWorkspace(workspaceId: string) {
  leaving.delete(workspaceId);
}

export function isLeavingWorkspace(workspaceId: string | null | undefined): boolean {
  return Boolean(workspaceId) && leaving.has(workspaceId as string);
}

export function isLeavingPage(pageId: string): boolean {
  return isLeavingWorkspace(pageWorkspaces.get(pageId));
}
