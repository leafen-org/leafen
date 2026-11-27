import { request } from "./apiTransport";
import type { Session } from "./types";
import {
  teamspacePaths,
  type TeamspaceManagementResponse,
  type TeamspaceMutationInput,
  type TeamspaceMutationResponse,
  type TeamspaceSnapshotResponse,
} from "./teamspaceWire.generated";

function pathForWorkspace(template: string, workspaceId: string) {
  return template.replace("{workspaceId}", encodeURIComponent(workspaceId));
}

function withCursor(path: string, values: Record<string, string | undefined>) {
  const search = new URLSearchParams();
  for (const [key, value] of Object.entries(values)) {
    if (value) search.set(key, value);
  }
  const query = search.toString();
  return query ? `${path}?${query}` : path;
}

export function createTeamspaceApi() {
  return {
    /** With `everyTeamspace`, 설정 › 팀스페이스's read: every active one for the workspace's owner. */
    readTeamspaceSnapshot: (
      session: Session,
      workspaceId: string,
      cursor?: string,
      signal?: AbortSignal,
      everyTeamspace = false,
    ) => request<TeamspaceSnapshotResponse>(
      withCursor(pathForWorkspace(teamspacePaths.readTeamspaceSnapshot, workspaceId), {
        table: everyTeamspace ? "true" : undefined,
        cursor,
      }),
      { method: "GET", signal },
      session,
    ),
    readArchivedTeamspaces: (
      session: Session,
      workspaceId: string,
      cursor?: string,
      signal?: AbortSignal,
    ) => request<TeamspaceSnapshotResponse>(
      withCursor(pathForWorkspace(teamspacePaths.readTeamspaceSnapshot, workspaceId), {
        archived: "true",
        cursor,
      }),
      { method: "GET", signal },
      session,
    ),
    readTeamspaceManagement: (
      session: Session,
      workspaceId: string,
      teamspaceId: string,
      cursors: { memberCursor?: string; requestCursor?: string; principalCursor?: string } = {},
      signal?: AbortSignal,
    ) => request<TeamspaceManagementResponse>(
      withCursor(
        pathForWorkspace(teamspacePaths.readTeamspaceManagement, workspaceId)
          .replace("{teamspaceId}", encodeURIComponent(teamspaceId)),
        cursors,
      ),
      { method: "GET", signal },
      session,
    ),
    mutateTeamspace: (
      session: Session,
      workspaceId: string,
      input: TeamspaceMutationInput,
      signal?: AbortSignal,
    ) => request<TeamspaceMutationResponse>(
      pathForWorkspace(teamspacePaths.mutateTeamspace, workspaceId),
      { method: "POST", body: JSON.stringify(input), signal },
      session,
    ),
  };
}
