import { request } from "@/apiTransport";
import type { Session } from "@/types";

export type ManagedUserWorkspace = {
  id: string;
  name: string;
  /** Another organization's workspace. */
  external: boolean;
};

/** A person on one of the organization's verified domains, or one it suspended. */
export type ManagedUser = {
  id: string;
  name: string;
  email: string;
  workspaces: ManagedUserWorkspace[];
  /** Milliseconds; 0 when never signed in. */
  lastActiveAt: number;
  twoStep: boolean;
  suspended: boolean;
  /** Milliseconds; when the person's scheduled deletion happens, or 0. */
  deletionDueAt: number;
};

/** What deleting a managed user does to their workspaces, or why it can't. */
export type ManagedUserDeletionPreview = {
  transferred: Array<{ id: string; name: string }>;
  deleted: Array<{ id: string; name: string }>;
  blocker: "none" | "last_administrator" | "shared_organization";
};

export type ManagedUserFilter = {
  query: string;
  workspaceId: string;
  /** The 2-step verification method: an authenticator app, none, or any. */
  twoStep: "" | "totp" | "none";
};

/** A window of the people, by name, and where the next starts (none at the last). */
export type ManagedUserWindow = {
  items: ManagedUser[];
  hasMore: boolean;
  nextCursor?: string;
};

/** The first window of the people, with the workspace filter's choices. */
export type ManagedUserList = ManagedUserWindow & {
  /** The workspace filter's first window of choices, and where the next starts. */
  workspaceChoices: ManagedUserWorkspace[];
  workspaceChoicesNextCursor?: string;
};

/** A window of the workspace filter's choices whose name holds the query. */
export type ManagedUserWorkspaceChoices = { items: ManagedUserWorkspace[]; nextCursor?: string };

function usersPath(organizationId: string) {
  return `/organizations/${encodeURIComponent(organizationId)}/managed-users`;
}

function userPath(organizationId: string, userId: string) {
  return `${usersPath(organizationId)}/${encodeURIComponent(userId)}`;
}

function managedUsersSearch(filter: ManagedUserFilter, cursor?: string) {
  const search = new URLSearchParams();
  if (filter.query.trim()) search.set("query", filter.query.trim());
  if (filter.workspaceId) search.set("workspaceId", filter.workspaceId);
  if (filter.twoStep) search.set("twoStep", filter.twoStep);
  if (cursor) search.set("cursor", cursor);
  return search.toString() ? `?${search}` : "";
}

export function listManagedUsers(session: Session, organizationId: string, filter: ManagedUserFilter, signal?: AbortSignal) {
  return request<ManagedUserList>(`${usersPath(organizationId)}${managedUsersSearch(filter)}`, { signal }, session);
}

/** The people after the window that gave the cursor, with the same search and filters. */
export function listMoreManagedUsers(session: Session, organizationId: string, filter: ManagedUserFilter, cursor: string, signal?: AbortSignal) {
  return request<ManagedUserWindow>(`${usersPath(organizationId)}${managedUsersSearch(filter, cursor)}`, { signal }, session);
}

export function listManagedUserWorkspaces(session: Session, organizationId: string, input: { query: string; cursor?: string }, signal?: AbortSignal) {
  const search = new URLSearchParams();
  if (input.query.trim()) search.set("query", input.query.trim());
  if (input.cursor) search.set("cursor", input.cursor);
  const suffix = search.toString() ? `?${search}` : "";
  return request<ManagedUserWorkspaceChoices>(`${usersPath(organizationId)}/workspaces${suffix}`, { signal }, session);
}

export function logOutManagedUser(session: Session, organizationId: string, userId: string) {
  return request<{ revoked: number }>(`${userPath(organizationId, userId)}/logout`, { method: "POST" }, session);
}

export function logOutManagedUsers(session: Session, organizationId: string) {
  return request<{ users: number; revoked: number }>(`${usersPath(organizationId)}/logout`, { method: "POST" }, session);
}

export function setManagedUserSuspended(session: Session, organizationId: string, userId: string, suspended: boolean) {
  return request<ManagedUser>(`${userPath(organizationId, userId)}/suspension`, { method: suspended ? "PUT" : "DELETE" }, session);
}

export function changeManagedUser(session: Session, organizationId: string, userId: string, change: { name?: string; email?: string }) {
  return request<ManagedUser>(userPath(organizationId, userId), { method: "PATCH", body: JSON.stringify(change) }, session);
}

export function removeManagedUserFromExternalWorkspaces(session: Session, organizationId: string, userId: string) {
  return request<{ user: ManagedUser; removed: number; owned: number }>(
    `${userPath(organizationId, userId)}/external-workspaces`, { method: "DELETE" }, session,
  );
}

export function previewManagedUserDeletion(session: Session, organizationId: string, userId: string, signal?: AbortSignal) {
  return request<ManagedUserDeletionPreview>(`${userPath(organizationId, userId)}/deletion`, { signal }, session);
}

/** Suspends the person and deletes them after 30 days, once their email is typed. */
export function scheduleManagedUserDeletion(session: Session, organizationId: string, userId: string, email: string) {
  return request<ManagedUser>(`${userPath(organizationId, userId)}/deletion`, { method: "POST", body: JSON.stringify({ email }) }, session);
}

export function restoreManagedUser(session: Session, organizationId: string, userId: string) {
  return request<ManagedUser>(`${userPath(organizationId, userId)}/deletion`, { method: "DELETE" }, session);
}

/** Asks the person for a new password the next time they open the app, telling them by email. */
export function resetManagedUserPassword(session: Session, organizationId: string, userId: string) {
  return request<ManagedUser>(`${userPath(organizationId, userId)}/password-reset`, { method: "POST" }, session);
}

/** Asks everyone on the verified domains but the admin for a new password. */
export function resetManagedUserPasswords(session: Session, organizationId: string) {
  return request<{ users: number }>(`${usersPath(organizationId)}/password-reset`, { method: "POST" }, session);
}
