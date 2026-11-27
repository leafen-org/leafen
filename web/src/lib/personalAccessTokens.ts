import { request } from "@/apiTransport";
import type { Session } from "@/types";

/** How long a new token lasts: the lifetimes Notion offers. */
export const PERSONAL_ACCESS_TOKEN_LIFETIMES = ["7d", "30d", "90d", "180d", "1y"] as const;
export type PersonalAccessTokenLifetime = typeof PERSONAL_ACCESS_TOKEN_LIFETIMES[number];

/** One of the signed-in person's tokens in a workspace, without its secret. */
export type PersonalAccessToken = {
  id: string;
  name: string;
  /** Only in the owner's list of the workspace's tokens. */
  creator?: { id: string; name: string };
  status: "active" | "expired" | "revoked";
  createdAt: number;
  expiresAt: number;
  lastUsedAt?: number;
  revokedAt?: number;
};

function tokensPath(workspaceId: string) {
  return `/workspaces/${encodeURIComponent(workspaceId)}/personal-access-tokens`;
}

/** A list read whole, a window at a time, with how many working tokens a person may keep. */
export type PersonalAccessTokenList = { items: PersonalAccessToken[]; activeLimit: number };

async function listAllTokens(session: Session, path: string, signal?: AbortSignal): Promise<PersonalAccessTokenList> {
  const items: PersonalAccessToken[] = [];
  let activeLimit = 0;
  let cursor = "";
  do {
    const separator = path.includes("?") ? "&" : "?";
    const page = await request<{ items: PersonalAccessToken[]; nextCursor?: string; activeLimit: number }>(
      cursor ? `${path}${separator}cursor=${encodeURIComponent(cursor)}` : path, { signal }, session,
    );
    items.push(...page.items);
    activeLimit = page.activeLimit;
    cursor = page.nextCursor ?? "";
  } while (cursor);
  return { items, activeLimit };
}

export function listPersonalAccessTokens(session: Session, workspaceId: string, signal?: AbortSignal) {
  return listAllTokens(session, tokensPath(workspaceId), signal);
}

/** Every token in the workspace, for its owner. */
export function listWorkspacePersonalAccessTokens(session: Session, workspaceId: string, signal?: AbortSignal) {
  return listAllTokens(session, `${tokensPath(workspaceId)}?scope=workspace`, signal);
}

/** Makes a token; its secret is in this answer only. */
export function createPersonalAccessToken(
  session: Session,
  workspaceId: string,
  input: { name: string; expiresIn: PersonalAccessTokenLifetime },
) {
  return request<{ token: PersonalAccessToken; secret: string }>(
    tokensPath(workspaceId),
    { method: "POST", body: JSON.stringify(input) },
    session,
  );
}

export function revokePersonalAccessToken(session: Session, workspaceId: string, tokenId: string) {
  return request<PersonalAccessToken>(
    `${tokensPath(workspaceId)}/${encodeURIComponent(tokenId)}/revoke`,
    { method: "POST" },
    session,
  );
}

/**
 * When a token made now would expire, for the form's note: a year ends on
 * the same date a year later. The server sets the real time.
 */
export function personalAccessTokenExpiry(lifetime: PersonalAccessTokenLifetime, now = new Date()): Date {
  const date = new Date(now);
  if (lifetime === "1y") date.setFullYear(date.getFullYear() + 1);
  else date.setDate(date.getDate() + Number.parseInt(lifetime, 10));
  return date;
}
