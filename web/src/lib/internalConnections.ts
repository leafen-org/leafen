import { request } from "@/apiTransport";
import type { Session } from "@/types";
import type { DatabaseSourceTitle } from "@/components/editor/databaseTitles";

/** What an internal connection may do with its token: Notion's "기능". */
export type InternalConnectionCapabilities = {
  readContent: boolean;
  updateContent: boolean;
  insertContent: boolean;
  readComments: boolean;
  insertComments: boolean;
  userInfo: "none" | "no_email" | "email";
};

/** A workspace's internal connection, without its token (notion_internal_connections.go). */
export type InternalConnection = {
  id: string;
  name: string;
  /** An emoji or a picture's link, or "" for its first letter (connection_icon.go). */
  icon: string;
  /** The grant a page's "연결" adds it to. */
  grantId: string;
  capabilities: InternalConnectionCapabilities;
  /** The token's start and last four. */
  tokenHint: string;
  /** When the token was made, or last made again. */
  tokenMadeAt: number;
  /** Until when the token it had before still works, after a refresh that revokes it in seven days. */
  previousUntil?: number;
  creator: { id: string; name: string };
  pages: number;
  createdAt: number;
  /** The token, in the answer that makes it only. */
  secret?: string;
};

export type InternalConnectionPage = { id: string; title: string; icon: string; iconType: string; pageKind?: string; viewBlock?: boolean; sourceTitles?: DatabaseSourceTitle[] };

// A token made in the window that made the connection is shown on its page
// at once; later its owners ask the server for it again.
const madeSecrets = new Map<string, string>();

export function rememberInternalConnectionSecret(connectionId: string, secret: string) {
  madeSecrets.set(connectionId, secret);
}

export function takeInternalConnectionSecret(connectionId: string) {
  const secret = madeSecrets.get(connectionId) ?? "";
  madeSecrets.delete(connectionId);
  return secret;
}

function connectionsPath(workspaceId: string, connectionId?: string) {
  const base = `/workspaces/${encodeURIComponent(workspaceId)}/internal-connections`;
  return connectionId ? `${base}/${encodeURIComponent(connectionId)}` : base;
}

export async function listInternalConnections(session: Session, workspaceId: string, signal?: AbortSignal) {
  return (await request<{ items: InternalConnection[] }>(connectionsPath(workspaceId), { signal }, session)).items;
}

/** Makes a connection; its token is in this answer only. */
export function createInternalConnection(session: Session, workspaceId: string, name: string) {
  return request<InternalConnection>(connectionsPath(workspaceId), { method: "POST", body: JSON.stringify({ name }) }, session);
}

export function updateInternalConnection(
  session: Session, workspaceId: string, connectionId: string,
  change: { name?: string; icon?: string; capabilities?: InternalConnectionCapabilities },
) {
  return request<InternalConnection>(connectionsPath(workspaceId, connectionId), { method: "PATCH", body: JSON.stringify(change) }, session);
}

/** Makes the token again; the old one stops at once ("now") or in seven days ("later"). */
export function renewInternalConnectionToken(session: Session, workspaceId: string, connectionId: string, revokeOld: "now" | "later") {
  return request<InternalConnection>(`${connectionsPath(workspaceId, connectionId)}/token`, { method: "POST", body: JSON.stringify({ revokeOld }) }, session);
}

/** The token, for one of its owners, as Notion's "보기". */
export async function revealInternalConnectionToken(session: Session, workspaceId: string, connectionId: string) {
  return (await request<{ secret: string }>(`${connectionsPath(workspaceId, connectionId)}/token/reveal`, { method: "POST", body: "{}" }, session)).secret;
}

export function deleteInternalConnection(session: Session, workspaceId: string, connectionId: string) {
  return request<void>(connectionsPath(workspaceId, connectionId), { method: "DELETE" }, session);
}

/** The pages the connection reaches, as its "콘텐츠 사용 권한" names them. */
export async function listInternalConnectionPages(session: Session, workspaceId: string, connectionId: string, signal?: AbortSignal) {
  return (await request<{ items: InternalConnectionPage[] }>(`${connectionsPath(workspaceId, connectionId)}/pages`, { signal }, session)).items;
}

/** An owner of an internal connection: its maker ("생성자") first. */
export type InternalConnectionOwner = { id: string; name: string; email: string; creator: boolean };

export async function listInternalConnectionOwners(session: Session, workspaceId: string, connectionId: string, signal?: AbortSignal) {
  return (await request<{ items: InternalConnectionOwner[] }>(`${connectionsPath(workspaceId, connectionId)}/owners`, { signal }, session)).items;
}

export async function addInternalConnectionOwner(session: Session, workspaceId: string, connectionId: string, userId: string) {
  return (await request<{ items: InternalConnectionOwner[] }>(`${connectionsPath(workspaceId, connectionId)}/owners`,
    { method: "POST", body: JSON.stringify({ userId }) }, session)).items;
}

export async function removeInternalConnectionOwner(session: Session, workspaceId: string, connectionId: string, userId: string) {
  return (await request<{ items: InternalConnectionOwner[] }>(`${connectionsPath(workspaceId, connectionId)}/owners/${encodeURIComponent(userId)}`,
    { method: "DELETE" }, session)).items;
}


/** A connection's webhook subscription (notion_internal_connection_webhooks.go). */
export type InternalConnectionWebhook = {
  id: string;
  url: string;
  apiVersion: string;
  eventTypes: string[];
  /** Whether its verification token came back; till then it hears nothing. */
  verified: boolean;
  createdAt: number;
  /** How many of its events failed to reach its address, and when the latest did (0 with none). */
  failures: number;
  lastFailedAt: number;
};

/** The subscriptions, and what Notion's "구독 생성하기" offers: its events in order, those checked first, its API versions and
 *  the events it checks once a version is picked. */
export type InternalConnectionWebhooks = {
  items: InternalConnectionWebhook[];
  eventTypes: string[];
  defaultEventTypes: string[];
  apiVersions: string[];
  versionEventTypes: Record<string, string[]>;
};

function webhooksPath(workspaceId: string, connectionId: string, webhookId?: string) {
  const base = `${connectionsPath(workspaceId, connectionId)}/webhooks`;
  return webhookId ? `${base}/${encodeURIComponent(webhookId)}` : base;
}

export function listInternalConnectionWebhooks(session: Session, workspaceId: string, connectionId: string, signal?: AbortSignal) {
  return request<InternalConnectionWebhooks>(webhooksPath(workspaceId, connectionId), { signal }, session);
}

/** Makes a subscription; its address is sent a verification token first. */
export function createInternalConnectionWebhook(
  session: Session, workspaceId: string, connectionId: string, input: { url: string; apiVersion: string; eventTypes: string[] },
) {
  return request<InternalConnectionWebhook>(webhooksPath(workspaceId, connectionId), { method: "POST", body: JSON.stringify(input) }, session);
}

export function updateInternalConnectionWebhook(
  session: Session, workspaceId: string, connectionId: string, webhookId: string,
  change: { url?: string; apiVersion?: string; eventTypes?: string[] },
) {
  return request<InternalConnectionWebhook>(webhooksPath(workspaceId, connectionId, webhookId), { method: "PATCH", body: JSON.stringify(change) }, session);
}

export function deleteInternalConnectionWebhook(session: Session, workspaceId: string, connectionId: string, webhookId: string) {
  return request<void>(webhooksPath(workspaceId, connectionId, webhookId), { method: "DELETE" }, session);
}

/** Gives back the token the subscription was sent, as Notion's "구독 확인". */
export function verifyInternalConnectionWebhook(session: Session, workspaceId: string, connectionId: string, webhookId: string, token: string) {
  return request<InternalConnectionWebhook>(`${webhooksPath(workspaceId, connectionId, webhookId)}/verify`,
    { method: "POST", body: JSON.stringify({ token }) }, session);
}

/** Sends a waiting subscription its token again now: whether its address took it, and the status it answered
 *  (502 when nothing answered). A verified one is sent nothing. */
export function resendInternalConnectionWebhookToken(session: Session, workspaceId: string, connectionId: string, webhookId: string) {
  return request<InternalConnectionWebhook & { tokenDelivered: boolean; tokenStatus: number }>(
    `${webhooksPath(workspaceId, connectionId, webhookId)}/resend`, { method: "POST", body: "{}" }, session);
}
