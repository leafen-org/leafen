import { request } from "@/apiTransport";
import type { Session } from "@/types";

type WorkspaceRestriction = { userId: string; restricted: boolean; changed: boolean };

/** Makes a workspace member a restricted member, as Notion's "제한된 멤버", or a member again. */
export async function restrictWorkspaceMember(session: Session, workspaceId: string, userId: string, restricted: boolean, signal?: AbortSignal) {
  const result = await request<WorkspaceRestriction>(
    `/workspaces/${encodeURIComponent(workspaceId)}/members/${encodeURIComponent(userId)}/restriction`,
    { method: "PUT", body: JSON.stringify({ restricted }), signal }, session,
  );
  if (result.userId !== userId || result.restricted !== restricted) throw new Error("Invalid workspace restriction response.");
}
