import { request } from "@/apiTransport";
import { workspaceActivityOperations, type WorkspaceActivityChangesResponse, type WorkspaceActivityResponse } from "@/workspaceActivityWire.generated";
import type { Session } from "@/types";

/** A workspace's updates, or one database's own with its source (a database opened alone lists them). */
export function readWorkspaceActivities(session: Session, workspaceId: string, cursor: string, signal: AbortSignal,
  options: { sourceId?: string; trashOf?: string; limit?: number } = {}) {
  const query = new URLSearchParams();
  if (cursor) query.set("cursor", cursor);
  if (options.sourceId) query.set("sourceId", options.sourceId);
  // One page's moves to and from the trash, as its updates list them.
  if (options.trashOf) query.set("trashOf", options.trashOf);
  if (options.limit) query.set("limit", String(options.limit));
  const path = workspaceActivityOperations.list.path.replace("{workspaceId}", encodeURIComponent(workspaceId));
  return request<WorkspaceActivityResponse>(path + (query.size ? "?" + query : ""), { signal }, session);
}

/** What one "edited the page" update changed, its lines as they were then. */
export function readWorkspaceActivityChanges(session: Session, workspaceId: string, activityId: string) {
  const path = workspaceActivityOperations.changes.path.replace("{workspaceId}", encodeURIComponent(workspaceId))
    .replace("{activityId}", encodeURIComponent(activityId));
  return request<WorkspaceActivityChangesResponse>(path, {}, session);
}
