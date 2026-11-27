import { request } from "@/apiTransport";
import type { Session } from "@/types";

export type WorkspaceCustomEmoji = {
  id: string;
  workspaceId: string;
  name: string;
  fileId: string;
  url: string;
  revision: number;
  createdAt: number;
  updatedAt: number;
  createdBy?: string;
  /** In the workspace's list: who added it, and whether the reader may rename or delete it. */
  creatorName?: string;
  canChange?: boolean;
  duplicate?: boolean;
  /** Deleted: read with its last name (?include=deleted), its picture gone. */
  deleted?: boolean;
};

export type WorkspaceCustomEmojiList = {
  items: WorkspaceCustomEmoji[];
  nextCursor: string | null;
  hasMore: boolean;
  /** The reader may add emoji; a workspace may keep that to its owners. */
  canAdd?: boolean;
};

function workspacePath(workspaceId: string) {
  return `/workspaces/${encodeURIComponent(workspaceId)}/custom-emojis`;
}

export function listWorkspaceCustomEmojis(
  session: Session,
  workspaceId: string,
  input: { cursor?: string; limit?: number; signal?: AbortSignal } = {},
) {
  const query = new URLSearchParams();
  if (input.cursor) query.set("cursor", input.cursor);
  query.set("limit", String(input.limit ?? 50));
  return request<WorkspaceCustomEmojiList>(
    `${workspacePath(workspaceId)}?${query}`,
    { signal: input.signal },
    session,
  );
}

/**
 * An emoji named by its id alone, as a comment's or a database cell's text
 * names it; a deleted one comes with its last name and `deleted`, as text
 * that used it shows it (custom_emoji_tombstone.go).
 */
export function getCustomEmoji(session: Session, emojiId: string, options: { signal?: AbortSignal } = {}) {
  return request<WorkspaceCustomEmoji>(`/custom-emojis/${encodeURIComponent(emojiId)}?include=deleted`, { signal: options.signal }, session);
}

export function getWorkspaceCustomEmoji(
  session: Session,
  workspaceId: string,
  emojiId: string,
  options: { signal?: AbortSignal } = {},
) {
  return request<WorkspaceCustomEmoji>(
    `${workspacePath(workspaceId)}/${encodeURIComponent(emojiId)}?include=deleted`,
    { signal: options.signal },
    session,
  );
}

export function createWorkspaceCustomEmoji(
  session: Session,
  workspaceId: string,
  input: { fileId: string; name: string; requestId: string; signal?: AbortSignal },
) {
  const { signal, ...body } = input;
  return request<WorkspaceCustomEmoji>(
    workspacePath(workspaceId),
    { method: "POST", body: JSON.stringify(body), signal },
    session,
  );
}

export function renameWorkspaceCustomEmoji(
  session: Session,
  workspaceId: string,
  emojiId: string,
  /** fileId: a new picture, as "편집" replaces it. */
  input: { baseRevision: number; name: string; fileId?: string; signal?: AbortSignal },
) {
  const { signal, ...body } = input;
  return request<WorkspaceCustomEmoji>(
    `${workspacePath(workspaceId)}/${encodeURIComponent(emojiId)}`,
    { method: "PATCH", body: JSON.stringify(body), signal },
    session,
  );
}

export function deleteWorkspaceCustomEmoji(
  session: Session,
  workspaceId: string,
  emojiId: string,
  input: { baseRevision: number; signal?: AbortSignal },
) {
  const { signal, ...body } = input;
  return request<{ id: string; fileId: string; deleted: boolean }>(
    `${workspacePath(workspaceId)}/${encodeURIComponent(emojiId)}`,
    { method: "DELETE", body: JSON.stringify(body), signal },
    session,
  );
}
