import { request } from "./apiTransport";
import type { Session } from "./types";

// A Notion-compatible integration's connection, which the person answers in
// the app (components/mcp/NotionAuthorizationPage.tsx).

export type NotionOAuthRequest = { clientName: string; redirectUri: string; scopes: string[] };

export type NotionOAuthApproval = {
  requestId: string;
  clientId: string;
  redirectUri: string;
  workspaceId: string;
  state: string;
  resourceIds: string[];
};

export function readNotionOAuthRequest(session: Session, clientId: string, redirectUri: string, signal?: AbortSignal) {
  return request<NotionOAuthRequest>("/notion/oauth/requests", {
    method: "POST", body: JSON.stringify({ clientId, redirectUri }), signal,
  }, session);
}

export function approveNotionOAuthRequest(session: Session, approval: NotionOAuthApproval, signal?: AbortSignal) {
  return request<{ redirectUrl: string }>("/notion/oauth/authorizations", {
    method: "POST", body: JSON.stringify(approval), signal,
  }, session);
}
