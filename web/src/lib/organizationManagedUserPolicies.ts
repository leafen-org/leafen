import { request } from "@/apiTransport";
import type { Session } from "@/types";

/**
 * An organization's managed user policies, as Notion's Identity rows (seen
 * 2026-10-02 behind its upgrade): whether managed users change their name
 * and photo, join workspaces it doesn't own, and how many hours they stay
 * signed in (1 to 180 days' worth, 180 days by default).
 */
export type ManagedUserPolicies = {
  secondaryEmails: "disabled" | "verified_domains" | "any";
  profileChanges: boolean;
  externalWorkspaces: boolean;
  sessionDurationHours: number;
};

/** Notion's range, from an hour to 180 days. */
export const SESSION_DURATION_HOURS = [1, 12, 24, 7 * 24, 14 * 24, 30 * 24, 90 * 24, 180 * 24] as const;

function policiesPath(organizationId: string) {
  return `/organizations/${encodeURIComponent(organizationId)}/managed-user-policies`;
}

export function readManagedUserPolicies(session: Session, organizationId: string, signal?: AbortSignal) {
  return request<ManagedUserPolicies>(policiesPath(organizationId), { signal }, session);
}

export function changeManagedUserPolicies(session: Session, organizationId: string, change: Partial<ManagedUserPolicies>) {
  return request<ManagedUserPolicies>(policiesPath(organizationId), { method: "PATCH", body: JSON.stringify(change) }, session);
}

/** What the person's organizations ask of them. */
export function readAccountOrganizationPolicies(session: Session, signal?: AbortSignal) {
  return request<{ managed: boolean; profileChanges: boolean }>("/account/organization-policies", { signal }, session);
}
