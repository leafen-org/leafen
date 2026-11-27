import { request } from "@/apiTransport";
import {
  organizationOperations,
  organizationRoles,
  type Organization,
  type OrganizationList,
} from "@/organizationWire.generated";
import type { Session } from "@/types";

type OrganizationFlight = {
  promise: Promise<Organization | null>;
  signal: AbortSignal;
};

const inFlight = new Map<string, OrganizationFlight>();
const validRoles = new Set<string>(Object.values(organizationRoles));

function invalidOrganization(message: string): never {
  throw new Error(`Invalid workspace organization context: ${message}`);
}

function assertOrganization(value: Organization, organizationId: string): Organization {
  if (
    value.id !== organizationId
    || !value.name.trim()
    || value.status !== "active"
    || !validRoles.has(value.role)
    || !Number.isInteger(value.policyVersion)
    || value.policyVersion < 0
    || !Number.isInteger(value.settingsRevision)
    || value.settingsRevision < 0
    || (
      value.storageLimitBytes !== null
      && (!Number.isSafeInteger(value.storageLimitBytes) || value.storageLimitBytes < 0)
    )
  ) invalidOrganization("organization projection");
  return value;
}

function assertPage(
  value: OrganizationList,
  organizationId: string,
  userId: string,
  cursor: string,
  seenOrganizationIds: Set<string>,
) {
  if (
    !value
    || !Array.isArray(value.organizations)
    || !Array.isArray(value.memberships)
    || typeof value.hasMore !== "boolean"
    || (value.nextCursor !== null && typeof value.nextCursor !== "string")
  ) invalidOrganization("list response");

  let selected: Organization | null = null;
  for (const organization of value.organizations) {
    if (!organization?.id || seenOrganizationIds.has(organization.id)) {
      invalidOrganization("duplicate organization");
    }
    seenOrganizationIds.add(organization.id);
    if (organization.id === organizationId) {
      selected = assertOrganization(organization, organizationId);
    }
  }

  if (selected) {
    const memberships = value.memberships.filter((candidate) => (
      candidate.organizationId === organizationId && candidate.userId === userId
    ));
    const membership = memberships[0];
    if (
      memberships.length !== 1
      || membership.status !== "active"
      || membership.role !== selected.role
    ) {
      invalidOrganization("membership projection");
    }
  }

  if (value.hasMore) {
    if (!value.nextCursor || value.nextCursor === cursor) {
      invalidOrganization("nonadvancing cursor");
    }
  } else if (value.nextCursor !== null) {
    invalidOrganization("terminal cursor");
  }
  return selected;
}

async function resolveWorkspaceOrganization(
  session: Session,
  organizationId: string,
  signal: AbortSignal,
): Promise<Organization | null> {
  if (!organizationId) return null;
  let cursor = "";
  const seenCursors = new Set<string>();
  const seenOrganizationIds = new Set<string>();
  for (;;) {
    if (signal.aborted) throw signal.reason ?? new DOMException("Aborted", "AbortError");
    if (seenCursors.has(cursor)) invalidOrganization("repeated cursor");
    seenCursors.add(cursor);
    const query = cursor ? `?cursor=${encodeURIComponent(cursor)}` : "";
    const page = await request<OrganizationList>(
      `${organizationOperations.list.path}${query}`,
      { method: organizationOperations.list.method, signal },
      session,
    );
    if (signal.aborted) throw signal.reason ?? new DOMException("Aborted", "AbortError");
    const selected = assertPage(
      page,
      organizationId,
      session.user.id,
      cursor,
      seenOrganizationIds,
    );
    if (selected) return selected;
    if (!page.hasMore) return null;
    cursor = page.nextCursor ?? invalidOrganization("missing cursor");
  }
}

export function loadWorkspaceOrganizationContext(
  session: Session,
  organizationId: string,
  signal: AbortSignal,
): Promise<Organization | null> {
  if (!organizationId) return Promise.resolve(null);
  const key = `${session.user.id}\u0000${session.token}\u0000${organizationId}`;
  const existing = inFlight.get(key);
  if (existing && !existing.signal.aborted) return existing.promise;
  if (existing) inFlight.delete(key);
  const task = resolveWorkspaceOrganization(session, organizationId, signal).finally(() => {
    if (inFlight.get(key)?.promise === task) inFlight.delete(key);
  });
  inFlight.set(key, { promise: task, signal });
  return task;
}
