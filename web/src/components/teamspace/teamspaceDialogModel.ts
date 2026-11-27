import type {
  Teamspace,
  TeamspaceAccess,
  TeamspaceAccessDecision,
  TeamspacePageRole,
  TeamspacePrincipalOption,
  TeamspaceSettings,
  TeamspaceSnapshotResponse,
  TeamspaceSummary,
} from "@/teamspaceWire.generated";

export type DialogView = "browse" | "create" | "manage" | "archived";

export type DialogTeamspace = Teamspace & TeamspaceAccessDecision & {
  isDefault: boolean;
  summary: TeamspaceSummary;
  role?: TeamspaceAccessDecision["membershipRole"];
  /** Why it may not be archived now (teamspaceArchiveLock), where the list it is in was read whole. */
  archiveLock?: TeamspaceArchiveLock;
};

/** Why the workspace's only default teamspace stays, as Notion's tips say. */
export type TeamspaceArchiveLock = "" | "multipleMembers" | "otherTeamspaces";

/**
 * Why a teamspace may not be archived now, as Notion's (its code, 2026-11-11):
 * a default one goes while another default stays; the only one goes only
 * while the workspace has one member who is in no other teamspace in use,
 * else its "보관" is dim with a tip saying which. The server holds the same
 * rule (checkLastDefaultTeamspaceGoes).
 */
export function teamspaceArchiveLock(teamspace: Pick<DialogTeamspace, "id" | "isDefault">,
  peers: readonly Pick<DialogTeamspace, "id" | "isDefault" | "joined" | "archivedAt">[], memberCount: number | undefined): TeamspaceArchiveLock {
  if (!teamspace.isDefault || peers.some((peer) => peer.id !== teamspace.id && peer.isDefault && !peer.archivedAt)) return "";
  if (memberCount !== 1) return "multipleMembers";
  return peers.some((peer) => peer.id !== teamspace.id && peer.joined && !peer.archivedAt) ? "otherTeamspaces" : "";
}

/**
 * Why one may not leave a teamspace from its settings, as Notion's dim
 * "팀스페이스 나가기" with its tip (its code and seen 2026-11-11): everyone is
 * in a default one, its only owner stays until another owner comes, and
 * someone in it through a group stays while the group does.
 */
export type TeamspaceLeaveLock = "" | "default" | "onlyOwner" | "group";

/**
 * Why its settings' "팀스페이스 나가기" is dim, in Notion's order (its code,
 * 2026-11-11). Any group the reader is in it through dims it there, even
 * beside a membership of their own, as the decision lists them
 * (membershipGroups); the sidebar's row leaves that own membership
 * (teamspaceLeaveGroups).
 */
export function teamspaceLeaveLock(teamspace: Pick<DialogTeamspace, "isDefault" | "membershipRole" | "membershipGroups">,
  ownerCount: number): TeamspaceLeaveLock {
  if (teamspace.isDefault) return "default";
  if (teamspace.membershipRole === "owner" && ownerCount < 2) return "onlyOwner";
  return teamspace.membershipGroups?.length ? "group" : "";
}

/**
 * The groups that keep the reader in a teamspace they have no membership of
 * their own in, which the server will not take them out of
 * (membershipSource "group"); Notion's sidebar "팀스페이스 나가기" says so
 * instead of leaving (its code, 2026-11-11). Empty where they may leave.
 */
export function teamspaceLeaveGroups(teamspace: Pick<DialogTeamspace, "membershipSource" | "membershipGroups">): string[] {
  return teamspace.membershipSource === "group" ? teamspace.membershipGroups ?? [] : [];
}

/** Why an archived teamspace stays archived for now, as Notion's tips on "팀스페이스 복원". */
export type TeamspaceRestoreLock = "" | "noDefault" | "notOwner";

/**
 * Why an archived teamspace may not be restored now, as Notion's (its code,
 * 2026-11-11): it comes back while the workspace has a default teamspace in
 * use, or, open, as the default one; and only by its owners. The server holds
 * the same rule (restoredDefaultTeamspaces).
 */
export function teamspaceRestoreLock(teamspace: Pick<DialogTeamspace, "access" | "membershipRole">,
  settings: Pick<TeamspaceSettings, "defaultTeamspaceIds"> | null | undefined): TeamspaceRestoreLock {
  if (!settings?.defaultTeamspaceIds?.length && teamspace.access !== "open") return "noDefault";
  return teamspace.membershipRole === "owner" ? "" : "notOwner";
}

/** Teamspaces with why each may not be archived, the list read whole. */
export function withArchiveLocks<T extends DialogTeamspace>(teamspaces: readonly T[], memberCount: number | undefined): T[] {
  return teamspaces.map((teamspace) => ({ ...teamspace, archiveLock: teamspaceArchiveLock(teamspace, teamspaces, memberCount) }));
}

/** Notion's four kinds of teamspace: the default one everybody is in, then
 * its access. */
export type TeamspaceSecurity = "default" | TeamspaceAccess;

export const SECURITY_KINDS: TeamspaceSecurity[] = ["default", "open", "closed", "private"];

/** Whether a teamspace is one of its workspace's default ones: several, at least one once any is chosen, as Notion's. */
export function isDefaultTeamspace(settings: Pick<TeamspaceSettings, "defaultTeamspaceIds"> | null | undefined, teamspaceId: string) {
  return settings?.defaultTeamspaceIds?.includes(teamspaceId) === true;
}

export function teamspaceSecurity(teamspace: DialogTeamspace): TeamspaceSecurity {
  return teamspace.isDefault ? "default" : teamspace.access;
}

/** The kinds a teamspace's menu offers, as Notion's (its code, 2026-11-11):
 * "기본" only to the workspace's owners, or on a default one. */
export function securityKinds(workspaceAdmin: boolean, isDefault: boolean): TeamspaceSecurity[] {
  return workspaceAdmin || isDefault ? SECURITY_KINDS : SECURITY_KINDS.filter((kind) => kind !== "default");
}

/**
 * What a teamspace keeps as it turns open, closed or private, as Notion's
 * (seen 2026-11-18 at 1440 in its Business workspace, each way twice):
 * closing an open one takes the rest of the workspace's role away; opening
 * one the rest has no role in gives them the members' role, a role they had
 * staying; an open one lets every member invite, which it keeps when it
 * closes again. The server keeps an open teamspace to these
 * (teamspace_page_roles.go).
 */
export function teamspaceKindChanges(teamspace: Pick<DialogTeamspace, "access" | "isDefault" | "openPageRole" | "memberPageRole">,
  access: TeamspaceAccess): { access: TeamspaceAccess; openPageRole: TeamspacePageRole; membersCanInvite?: true } {
  if (access === "open") {
    return { access, openPageRole: teamspace.openPageRole === "none" ? teamspace.memberPageRole : teamspace.openPageRole, membersCanInvite: true };
  }
  const wasOpen = teamspace.isDefault || teamspace.access === "open";
  return { access, openPageRole: access === "closed" && wasOpen ? "none" : teamspace.openPageRole };
}

/**
 * Why a default teamspace's kind stays as it is, as Notion's (its code,
 * 2026-11-11): only the workspace's owners change a default teamspace, and a
 * workspace keeps at least one, so the only one stays until another is
 * made default; "" when it may change.
 */
export function defaultSecurityLock(teamspace: Pick<DialogTeamspace, "id" | "isDefault">,
  settings: Pick<TeamspaceSettings, "defaultTeamspaceIds"> | null | undefined, workspaceAdmin: boolean): "" | "onlyDefault" | "notAdmin" {
  if (!teamspace.isDefault) return "";
  if (!workspaceAdmin) return "notAdmin";
  const ids = settings?.defaultTeamspaceIds ?? [];
  return ids.length === 1 && ids[0] === teamspace.id ? "onlyDefault" : "";
}

/** Whether the reader may add a page to a teamspace, as its sidebar "+" and the add window offer. */
export function canAddTeamspacePage(teamspace: Pick<DialogTeamspace, "membershipRole" | "membersCanEditSidebar" | "pageRole">) {
  if (teamspace.membershipRole === "owner") return true;
  return teamspace.membershipRole === "member"
    && teamspace.membersCanEditSidebar !== false
    && (teamspace.pageRole === "edit" || teamspace.pageRole === "full_access");
}

/** A teamspace the reader has just made or brought back, its only owner. */
export function ownedTeamspace(teamspace: Teamspace, isDefault: boolean, summary: TeamspaceSummary): DialogTeamspace {
  return {
    ...teamspace, teamspaceId: teamspace.id, joined: true, visible: true, canJoin: false, requestPending: false,
    canRequest: false, membershipRole: "owner", membershipSource: "explicit", pageRole: "full_access", role: "owner",
    isDefault, summary,
  };
}

/** A copy's name as Notion gives it: "〈name〉 (1)", or the next number no
 * teamspace has yet. */
export function duplicateTeamspaceName(name: string, taken: readonly string[], maxRunes = 80) {
  const used = new Set(taken);
  for (let number = 1; ; number += 1) {
    const suffix = ` (${number})`;
    const base = [...name].slice(0, maxRunes - [...suffix].length).join("");
    if (!used.has(base + suffix)) return base + suffix;
  }
}

export function principalKey(type: "user" | "group", id: string) {
  return `${type}:${id}`;
}

export function projectTeamspaceSnapshot(result: TeamspaceSnapshotResponse): DialogTeamspace[] {
  const decisions = new Map(result.decisions.map((decision) => [decision.teamspaceId, decision]));
  const summaries = new Map(result.summaries.map((summary) => [summary.teamspaceId, summary]));
  return result.teamspaces.map((teamspace) => {
    const decision = decisions.get(teamspace.id) ?? {
      teamspaceId: teamspace.id,
      joined: false,
      visible: false,
      canJoin: false,
      requestPending: false,
      canRequest: false,
    };
    return {
      ...teamspace,
      ...decision,
      isDefault: isDefaultTeamspace(result.settings, teamspace.id),
      summary: summaries.get(teamspace.id) ?? {
        teamspaceId: teamspace.id,
        ownerLabels: [],
        ownerUserIds: [],
        ownerCount: 0,
        memberCount: 0,
      },
      role: decision.membershipRole,
    };
  });
}

export function mergeById<T extends { id: string }>(current: T[], incoming: T[]) {
  const merged = new Map(current.map((item) => [item.id, item]));
  for (const item of incoming) merged.set(item.id, item);
  return [...merged.values()];
}

export function optionLabel(options: TeamspacePrincipalOption[], type: "user" | "group", id: string) {
  return options.find((option) => option.principalType === type && option.principalId === id)?.label ?? id;
}

/** Teamspaces in the reader's own order, as Notion's sidebar keeps them: those
 * not in it yet — just made or joined — on top, newest first, then the rest
 * in that order. A rename or a change never moves one. */
export function orderTeamspaces<T extends Pick<Teamspace, "id" | "createdAt">>(
  teamspaces: readonly T[], order: readonly string[] = [],
): T[] {
  const place = new Map(order.map((id, index) => [id, index]));
  return teamspaces.slice().sort((left, right) => {
    const a = place.get(left.id), b = place.get(right.id);
    if (a === undefined && b === undefined) return right.createdAt - left.createdAt;
    if (a === undefined) return -1;
    if (b === undefined) return 1;
    return a - b;
  });
}

/** The reader's order once a teamspace moved to another's place in the shown list. */
export function movedTeamspaceOrder(shown: readonly { id: string }[], movedId: string, targetIndex: number): string[] {
  const ids = shown.map((teamspace) => teamspace.id).filter((id) => id !== movedId);
  ids.splice(Math.max(0, Math.min(targetIndex, ids.length)), 0, movedId);
  return ids;
}
