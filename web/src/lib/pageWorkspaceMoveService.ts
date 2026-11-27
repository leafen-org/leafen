import { request } from "@/apiTransport";
import { waitForDurableJob } from "@/lib/durableJobPolling";
import { newId } from "@/lib/ids";
import type { Session } from "@/types";

/**
 * A page moved to another workspace on this server moves there: it keeps its
 * address, its pages' and databases' too, and the server cuts what can't come
 * along (internal/app/page_workspace_move.go). Its plan, asked first, says how
 * much that is; the move then goes on the server and is read until it ends.
 */

export type PageWorkspaceMoveDestination = {
  workspaceId: string;
  /** The page it goes under, "" for the top of a place. */
  parentId?: string;
  /** The teamspace whose top it goes to, "" for the mover's private pages. */
  teamspaceId?: string;
};

export type PageWorkspaceMoveCuts = {
  relationProperties: number; relationValues: number; rollups: number;
  peopleLosingAccess: number; shares: number; publicLinks: number; guestLinks: number; favorites: number;
  /** The people count stopped at those the server asks about: at least that many. */
  peopleLosingAccessAtLeast?: boolean;
};

export type PageWorkspaceMovePlan = {
  pages: number; databases: number; rows: number; comments: number; files: number;
  cut: PageWorkspaceMoveCuts;
  across: { mentionsOut: number; mentionsIn: number; linkedViewsOut: number; linkedViewsIn: number; sharedFiles: number };
};

export type PageWorkspaceMoveResponse = {
  jobId?: string;
  status: "planned" | "pending" | "completed" | "failed";
  phase?: string;
  rootPageId: string;
  sourceWorkspaceId: string;
  targetWorkspaceId: string;
  plan: PageWorkspaceMovePlan;
  cut?: PageWorkspaceMoveCuts;
  errorCode?: string;
};

const startPath = (pageId: string) => `/pages/${encodeURIComponent(pageId)}/workspace-move`;

function body(destination: PageWorkspaceMoveDestination, extra: Record<string, unknown>) {
  return JSON.stringify({
    destinationWorkspaceId: destination.workspaceId,
    ...(destination.parentId ? { destinationParentId: destination.parentId } : {}),
    ...(destination.teamspaceId ? { destinationTeamspaceId: destination.teamspaceId } : {}),
    ...extra,
  });
}

/** What moving the page there takes and cuts, counted by the server. */
export function planPageWorkspaceMove(session: Session, pageId: string, destination: PageWorkspaceMoveDestination, signal?: AbortSignal) {
  return request<PageWorkspaceMoveResponse>(startPath(pageId), { method: "POST", body: body(destination, { dryRun: true }), signal }, session);
}

/** Moves the page there and waits until the move has ended, refused if it went back. */
export async function movePageToWorkspace(session: Session, pageId: string, destination: PageWorkspaceMoveDestination) {
  const initial = await request<PageWorkspaceMoveResponse>(startPath(pageId),
    { method: "POST", body: body(destination, { requestId: newId() }) }, session);
  const followed = new AbortController().signal;
  const result = await waitForDurableJob({
    initial,
    isSameJob: (next) => next.jobId === initial.jobId,
    isTerminal: (next) => next.status === "completed" || next.status === "failed",
    onProgress: () => undefined,
    read: () => request<PageWorkspaceMoveResponse>(`/page-workspace-moves/${encodeURIComponent(initial.jobId ?? "")}`,
      { method: "GET", signal: followed }, session),
    signal: followed,
  });
  if (result.status !== "completed") {
    throw Object.assign(new Error("The page's move to another workspace did not complete."), {
      code: result.errorCode || "LEAFEN_PAGE_WORKSPACE_MOVE_UNAVAILABLE",
    });
  }
  return result;
}
