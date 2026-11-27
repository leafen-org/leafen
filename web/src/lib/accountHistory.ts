import { request } from "@/apiTransport";
import { loadSession } from "@/lib/authSessionStore";
import {
  organizationAuditOperations,
  type AccountHistory,
  type AccountHistoryPolicy,
  type ViewDisplayPolicy,
  type OrganizationAuditEventList,
} from "@/organizationAuditWire.generated";
import type { Session } from "@/types";

// An account's history (the server's account_history.go): one person's
// sign-ins, reads, edits, permission changes, tokens and connections' work,
// as much of it as the reader may see; and the organization's audit log
// read by person, period, kind and target, page by page.

export const OPEN_ACCOUNT_HISTORY_EVENT = "leafen:open-account-history";

export type OpenAccountHistoryDetail = { organizationId: string; userId: string; name?: string };

export function requestAccountHistory(detail: OpenAccountHistoryDetail) {
  window.dispatchEvent(new CustomEvent<OpenAccountHistoryDetail>(OPEN_ACCOUNT_HISTORY_EVENT, { detail }));
}

function sessionOrFail(): Session {
  const session = loadSession();
  if (!session) throw new Error("Login required.");
  return session;
}

function path(template: string, values: Record<string, string>) {
  return Object.entries(values).reduce((built, [key, value]) => built.replace(`{${key}}`, encodeURIComponent(value)), template);
}

export function readAccountHistory(organizationId: string, userId: string, cursor = "", signal?: AbortSignal): Promise<AccountHistory> {
  const query = cursor ? `?${new URLSearchParams({ cursor })}` : "";
  return request<AccountHistory>(`${path(organizationAuditOperations.readAccountHistory.path, { organizationId, userId })}${query}`,
    { signal }, sessionOrFail());
}

export function readAccountHistoryPolicy(organizationId: string, signal?: AbortSignal): Promise<AccountHistoryPolicy> {
  return request<AccountHistoryPolicy>(path(organizationAuditOperations.readAccountHistoryPolicy.path, { organizationId }), { signal }, sessionOrFail());
}

export function updateAccountHistoryPolicy(organizationId: string, scopedView: boolean, revision: number): Promise<AccountHistoryPolicy> {
  return request<AccountHistoryPolicy>(path(organizationAuditOperations.updateAccountHistoryPolicy.path, { organizationId }),
    { method: organizationAuditOperations.updateAccountHistoryPolicy.method, body: JSON.stringify({ scopedView, revision }) }, sessionOrFail());
}

/** What the audit log is read by: all empty for everything. */
export type AuditLogFilter = { actor: string; action: string; target: string; since: number; until: number };

export function listAuditEvents(organizationId: string, filter: AuditLogFilter, cursor = "", signal?: AbortSignal): Promise<OrganizationAuditEventList> {
  const query = new URLSearchParams();
  for (const [key, value] of Object.entries(filter)) if (value) query.set(key, String(value));
  if (cursor) query.set("cursor", cursor);
  const search = query.toString();
  return request<OrganizationAuditEventList>(`${path(organizationAuditOperations.listEvents.path, { organizationId })}${search ? `?${search}` : ""}`,
    { signal }, sessionOrFail());
}

/** Whom page analytics names as having viewed, the organization's (the server's view_display_policy.go). */
export function readViewDisplayPolicy(organizationId: string, signal?: AbortSignal): Promise<ViewDisplayPolicy> {
  return request<ViewDisplayPolicy>(path(organizationAuditOperations.readViewDisplayPolicy.path, { organizationId }), { signal }, sessionOrFail());
}

export function updateViewDisplayPolicy(organizationId: string, display: ViewDisplayPolicy["display"], revision: number): Promise<ViewDisplayPolicy> {
  return request<ViewDisplayPolicy>(path(organizationAuditOperations.updateViewDisplayPolicy.path, { organizationId }),
    { method: organizationAuditOperations.updateViewDisplayPolicy.method, body: JSON.stringify({ display, revision }) }, sessionOrFail());
}
