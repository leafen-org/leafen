import type { Session } from "./types";

// Moving one workspace to another Leafen server (workspace_move_in.go and
// workspace_move_out.go): the new workspace's code, the old workspace's check
// and pairs, and each side's state.

type JsonRequest = <T>(path: string, init?: RequestInit, session?: Session | null) => Promise<T>;

export interface WorkspaceMoveIn {
  state: "none" | "waiting" | "receiving" | "importing";
  code?: string;
  expiresAt?: string;
  jobId?: string;
  jobStatus?: string;
  errorCode?: string;
  pages: number;
  files: number;
  comments: number;
  /** Once a moved workspace is in: who wasn't paired, to invite, and what was public there, to turn on again. */
  moved?: WorkspaceMovedSummary;
}

export interface WorkspaceMovedSummary {
  people: { name: string; email: string }[];
  links: { pageId: string; title: string }[];
  sites: { pageId: string; title: string }[];
}

export interface WorkspaceMoveCounts {
  pages: number;
  files: number;
  comments: number;
}

export interface WorkspaceMoveOut {
  state: "none" | "sending" | "moved" | "failed";
  step?: string;
  movedTo?: string;
  errorCode?: string;
  report?: { here: WorkspaceMoveCounts; there: WorkspaceMoveCounts };
}

export interface WorkspaceMovePerson {
  id: string;
  name: string;
  email: string;
  role: string;
  suggestion?: string;
}

export interface WorkspaceMoveAccount {
  id: string;
  name: string;
  email: string;
}

export interface WorkspaceMoveCheck {
  to: string;
  release: string;
  people: WorkspaceMovePerson[];
  accounts: WorkspaceMoveAccount[];
  pagesHere: number;
}

export const workspaceMoveErrorCodes = {
  codeInvalid: "LEAFEN_WORKSPACE_MOVE_CODE_INVALID",
  notEmpty: "LEAFEN_WORKSPACE_MOVE_NOT_EMPTY",
  failed: "LEAFEN_WORKSPACE_MOVE_FAILED",
  /** The new server already has the workspace's pages: it was moved or imported there before. */
  alreadyHere: "LEAFEN_WORKSPACE_MOVE_ALREADY_HERE",
  /** The code was made on this server, where the workspace already is. */
  sameServer: "LEAFEN_WORKSPACE_MOVE_SAME_SERVER",
} as const;

export function createWorkspaceMoveApi(request: JsonRequest) {
  const path = (workspaceId: string, side: "move-in" | "move-out") => `/workspaces/${encodeURIComponent(workspaceId)}/${side}`;
  return {
    workspaceMoveIn: (session: Session, workspaceId: string, signal?: AbortSignal) =>
      request<WorkspaceMoveIn>(path(workspaceId, "move-in"), { method: "GET", signal }, session),
    openWorkspaceMoveIn: (session: Session, workspaceId: string) =>
      request<WorkspaceMoveIn>(path(workspaceId, "move-in"), { method: "POST", body: "{}" }, session),
    closeWorkspaceMoveIn: (session: Session, workspaceId: string) =>
      request<WorkspaceMoveIn>(path(workspaceId, "move-in"), { method: "DELETE" }, session),
    workspaceMoveOut: (session: Session, workspaceId: string, signal?: AbortSignal) =>
      request<WorkspaceMoveOut>(path(workspaceId, "move-out"), { method: "GET", signal }, session),
    checkWorkspaceMoveOut: (session: Session, workspaceId: string, code: string) =>
      request<WorkspaceMoveCheck>(`${path(workspaceId, "move-out")}/check`, { method: "POST", body: JSON.stringify({ code }) }, session),
    startWorkspaceMoveOut: (session: Session, workspaceId: string, code: string, pairs: Record<string, string>) =>
      request<WorkspaceMoveOut>(path(workspaceId, "move-out"), { method: "POST", body: JSON.stringify({ code, pairs }) }, session),
  };
}
