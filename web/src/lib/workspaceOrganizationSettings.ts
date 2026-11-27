import { LeafenTransportError } from "@/apiTransport";
import { newId } from "@/lib/ids";
import {
  readOrganizationSettingsSnapshot,
  writeOrganizationSettingsMutation,
} from "@/organizationSettingsApi";
import type {
  OrganizationSettingsAction,
  OrganizationSettingsDomain,
  OrganizationSettingsGroup,
  OrganizationSettingsMember,
  OrganizationSettingsMutationResponse,
  OrganizationSettingsSnapshot,
} from "@/organizationWire.generated";
import type { Session } from "@/types";

export type OrganizationSettingsReadContext = {
  sessionIdentity: string;
  organizationId: string;
  workspaceId: string;
  requestGeneration: number;
  signal: AbortSignal;
  current: () => {
    sessionIdentity: string;
    organizationId: string;
    workspaceId: string;
    requestGeneration: number;
  };
  publish?: (snapshot: OrganizationSettingsSnapshot) => void;
};

export type OrganizationSettingsMutation = {
  action: OrganizationSettingsAction;
  targetId?: string;
  secondaryTargetId?: string;
  value?: string;
};

const mutationFlights = new Map<string, Promise<OrganizationSettingsMutationResponse>>();
const replayRequestIds = new Map<string, string>();

function abort(message: string): never {
  throw new DOMException(message, "AbortError");
}

function assertCurrent(context: OrganizationSettingsReadContext) {
  if (context.signal.aborted) throw context.signal.reason ?? abort("Organization settings read aborted.");
  const current = context.current();
  if (
    current.sessionIdentity !== context.sessionIdentity
    || current.organizationId !== context.organizationId
    || current.workspaceId !== context.workspaceId
    || current.requestGeneration !== context.requestGeneration
  ) abort("Organization settings read context changed.");
}

function appendUnique<T extends { id: string }>(target: T[], incoming: T[], seen: Set<string>) {
  for (const item of incoming) {
    if (!item.id || seen.has(item.id)) throw new Error("Organization settings identity repeated.");
    seen.add(item.id);
    target.push(item);
  }
}

function mergeGroups(target: OrganizationSettingsGroup[], incoming: OrganizationSettingsGroup[]) {
  for (const group of incoming) {
    const existing = target.find((candidate) => (
      candidate.id === group.id && candidate.managedBy === group.managedBy
    ));
    if (!existing) {
      target.push({ ...group, members: [...group.members] });
      continue;
    }
    const memberIds = new Set(existing.members.map((member) => member.id));
    for (const member of group.members) {
      if (memberIds.has(member.id)) throw new Error("Organization group member identity repeated.");
      memberIds.add(member.id);
      existing.members.push(member);
    }
    existing.memberCount = group.memberCount;
    existing.membersHasMore = group.membersHasMore;
  }
}

function snapshotPublication(
  snapshot: OrganizationSettingsSnapshot,
  hasMore: boolean,
  nextCursor: string | null,
): OrganizationSettingsSnapshot {
  return {
    ...snapshot,
    organization: { ...snapshot.organization },
    members: snapshot.members.map((member) => ({ ...member })),
    groups: snapshot.groups.map((group) => ({
      ...group,
      members: group.members.map((member) => ({ ...member })),
    })),
    domains: snapshot.domains.map((domain) => ({ ...domain })),
    hasMore,
    nextCursor,
  };
}

export async function loadOrganizationSettingsSnapshot(
  session: Session,
  context: OrganizationSettingsReadContext,
): Promise<OrganizationSettingsSnapshot> {
  assertCurrent(context);
  let cursor = "";
  let result: OrganizationSettingsSnapshot | null = null;
  const cursors = new Set<string>();
  const memberIds = new Set<string>();
  const domainIds = new Set<string>();
  for (;;) {
    if (cursors.has(cursor)) throw new Error("Organization settings cursor repeated.");
    cursors.add(cursor);
    const page = await readOrganizationSettingsSnapshot(
      session,
      context.organizationId,
      cursor,
      context.signal,
    );
    assertCurrent(context);
    if (!result) {
      result = { ...page, members: [], groups: [], domains: [] };
    } else if (
      result.snapshotIdentity !== page.snapshotIdentity
      || result.organization.revision !== page.organization.revision
    ) {
      throw new Error("Organization settings snapshot changed during continuation.");
    }
    appendUnique<OrganizationSettingsMember>(result.members, page.members, memberIds);
    mergeGroups(result.groups, page.groups);
    appendUnique<OrganizationSettingsDomain>(result.domains, page.domains, domainIds);
    const publication = snapshotPublication(result, page.hasMore, page.nextCursor);
    context.publish?.(publication);
    if (!page.hasMore) {
      if (page.nextCursor !== null) throw new Error("Organization settings terminal cursor is invalid.");
      return publication;
    }
    if (!page.nextCursor || page.nextCursor === cursor) {
      throw new Error("Organization settings continuation is invalid.");
    }
    cursor = page.nextCursor;
  }
}

export function mutateOrganizationSettings(
  session: Session,
  organizationId: string,
  expectedRevision: number,
  mutation: OrganizationSettingsMutation,
  signal: AbortSignal,
) {
  // same-key calls share one request identity and one in-flight transport.
  const key = JSON.stringify([
    session.user.id,
    session.sessionId ?? "",
    session.token,
    organizationId,
    expectedRevision,
    mutation.action,
    mutation.targetId ?? "",
    mutation.secondaryTargetId ?? "",
    mutation.value ?? "",
  ]);
  const existing = mutationFlights.get(key);
  if (existing) return existing;
  const requestId = replayRequestIds.get(key) ?? newId();
  replayRequestIds.set(key, requestId);
  const task = writeOrganizationSettingsMutation(session, organizationId, {
    requestId,
    expectedRevision,
    action: mutation.action,
    targetId: mutation.targetId ?? "",
    secondaryTargetId: mutation.secondaryTargetId ?? "",
    value: mutation.value ?? "",
  }, signal).then((response) => {
    replayRequestIds.delete(key);
    return response;
  }).catch((error: unknown) => {
    if (!(error instanceof LeafenTransportError)) replayRequestIds.delete(key);
    throw error;
  }).finally(() => {
    if (mutationFlights.get(key) === task) mutationFlights.delete(key);
  });
  mutationFlights.set(key, task);
  return task;
}
