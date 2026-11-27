import type {
  TeamspaceJoinRequest,
  TeamspaceMember,
  TeamspaceMemberRole,
  TeamspaceMutationInput,
  TeamspaceMutationResponse,
  TeamspaceSettings,
} from "@/teamspaceWire.generated";
import { duplicateTeamspaceName, teamspaceKindChanges, teamspaceSecurity, type DialogTeamspace, type TeamspaceSecurity } from "./teamspaceDialogModel";
import { confirmTeamspaceSecurity } from "./teamspaceSecurityConfirm";
import type { MakeOnce } from "@/lib/uncertainWrites";

export type TeamspaceFields = Pick<TeamspaceMutationInput, "name" | "icon" | "description" | "access" | "memberPageRole"
  | "openPageRole" | "membersCanInvite" | "membersCanEditSidebar" | "disablePublishing" | "disableGuests" | "disableExport">;

/** The page security switches, each kept as it is unless changed. */
type PageSecurityField = "disablePublishing" | "disableGuests" | "disableExport";

/**
 * What the teamspace dialog asks the server to do, each through the dialog's
 * one mutation lane: making, changing, joining, leaving, copying, archiving
 * and the members of a teamspace.
 */
type WriteInput = Omit<TeamspaceMutationInput, "requestId">;

export function teamspaceDialogCommands({ execute, selected, settings, teamspaces, fields }: {
  /** Sends a write after those before it; one read as it goes out sees the teamspace as their answers left it. */
  execute(input: WriteInput | ((latest: (teamspace: DialogTeamspace) => DialogTeamspace) => WriteInput), success: string,
    make?: MakeOnce): Promise<TeamspaceMutationResponse | false>;
  selected: DialogTeamspace | undefined;
  settings: TeamspaceSettings | null;
  teamspaces: readonly DialogTeamspace[];
  /** What the dialog's fields hold, for a teamspace being made. */
  fields: Required<Omit<TeamspaceFields, PageSecurityField>>;
}) {
  const { name, icon, description, access, memberPageRole, openPageRole, membersCanInvite, membersCanEditSidebar } = fields;

  // A teamspace's settings with some changed, saved after the writes before
  // it, the rest as their answers left them, so a change pressed while
  // another is out keeps that one's too.
  function saveWith(teamspace: DialogTeamspace | undefined, changes: Partial<TeamspaceFields>, success: string, expectedRevision = teamspace?.revision) {
    if (!teamspace || expectedRevision === undefined) return Promise.resolve(false as const);
    return execute((latest) => {
      const current = latest(teamspace);
      return {
        action: "update", teamspaceId: current.id, expectedRevision, name: current.name, icon: current.icon,
        description: current.description, access: current.access, memberPageRole: current.memberPageRole,
        openPageRole: current.openPageRole, membersCanInvite: current.membersCanInvite,
        membersCanEditSidebar: current.membersCanEditSidebar, ...changes,
      };
    }, success);
  }
  function saveSelectedWith(changes: Partial<TeamspaceFields>, success: string, expectedRevision = selected?.revision) {
    return saveWith(selected, changes, success, expectedRevision);
  }
  // A teamspace not open to everybody is opened before it becomes a default
  // one, as Notion's "기본" makes it open (its code, 2026-11-11), since
  // everybody is in each default teamspace; its revision then.
  async function opened(teamspace: DialogTeamspace, success: string) {
    if (teamspace.access === "open") return teamspace.revision;
    const result = await saveWith(teamspace, teamspaceKindChanges(teamspace, "open"), success);
    return result ? result.revision : undefined;
  }
  // "기본" makes it one more default teamspace, which everybody is in.
  async function makeDefault(teamspace: DialogTeamspace | undefined, success: string) {
    if (!teamspace) return false;
    const revision = await opened(teamspace, success);
    if (revision === undefined) return false;
    return execute({ action: "set_default", teamspaceId: teamspace.id, defaultTeamspaceId: teamspace.id,
      expectedRevision: revision }, success);
  }
  // A kind chosen in a teamspace's menu — the settings' table or its own
  // settings — once asked (teamspaceSecurityConfirm.tsx): "기본" makes it one
  // more default teamspace; another kind takes a default one off the default
  // teamspaces, then sets its access unless it is that already.
  async function changeSecurity(teamspace: DialogTeamspace | undefined, kind: TeamspaceSecurity, success: string, defaultSuccess: string) {
    if (!teamspace) return false;
    const from = teamspaceSecurity(teamspace);
    if (kind === from || !await confirmTeamspaceSecurity(teamspace.name, from, kind)) return false;
    if (kind === "default") return makeDefault(teamspace, defaultSuccess);
    if (teamspace.isDefault) {
      if (!settings) return false;
      const result = await execute({ action: "set_defaults", expectedRevision: settings.revision,
        defaultTeamspaceIds: settings.defaultTeamspaceIds.filter((id) => id !== teamspace.id) }, success);
      if (!result || teamspace.access === kind) return result;
    }
    return saveWith(teamspace, teamspaceKindChanges(teamspace, kind), success);
  }
  // The "기본 팀스페이스" field's "업데이트": these teamspaces, in this order,
  // are the default ones, and no other; at least one, as the server keeps.
  async function setDefaults(ids: readonly string[], success: string) {
    if (!settings) return false;
    for (const id of ids) {
      const teamspace = teamspaces.find((item) => item.id === id);
      if (teamspace && await opened(teamspace, success) === undefined) return false;
    }
    return execute({ action: "set_defaults", expectedRevision: settings.revision, defaultTeamspaceIds: [...ids] }, success);
  }

  return {
    createTeamspace(success: string, make?: MakeOnce) { return execute({
      action: "create", expectedRevision: 0, name: name.trim(), icon: icon.trim(),
      description: description.trim(), access, memberPageRole, openPageRole,
      membersCanInvite, membersCanEditSidebar,
    }, success, make); },
    saveWith,
    saveSelectedWith,
    changeSecurity,
    setDefaults,
    leaveSelected(success: string) { return selected ? execute({
      action: "leave", teamspaceId: selected.id, expectedRevision: selected.revision,
    }, success) : Promise.resolve(false as const); },
    // A copy keeps the settings, not the pages or the members, as Notion's.
    duplicate(teamspace: DialogTeamspace, success: string) { return execute({
      action: "create", expectedRevision: 0, name: duplicateTeamspaceName(teamspace.name, teamspaces.map((item) => item.name)),
      icon: teamspace.icon, description: teamspace.description, access: teamspace.access,
      memberPageRole: teamspace.memberPageRole, openPageRole: teamspace.openPageRole,
      membersCanInvite: teamspace.membersCanInvite, membersCanEditSidebar: teamspace.membersCanEditSidebar,
    }, success); },
    join(teamspace: DialogTeamspace, success: string) { return execute({
      action: "join", teamspaceId: teamspace.id, expectedRevision: teamspace.revision,
    }, success); },
    // A closed teamspace's request, with why when the request window says.
    requestAccess(teamspace: DialogTeamspace, success: string, message = "") { return execute({
      action: "request", teamspaceId: teamspace.id, expectedRevision: teamspace.revision,
      ...(message.trim() ? { joinMessage: message.trim() } : {}),
    }, success); },
    updateMember(member: TeamspaceMember, role: TeamspaceMemberRole, success: string) {
      return selected ? execute({ action: "update_member", teamspaceId: selected.id,
        expectedRevision: selected.revision, principalType: member.principalType,
        principalId: member.principalId, memberRole: role }, success) : Promise.resolve(false);
    },
    // A workspace owner making themselves its owner from their own row, after
    // Notion's privileged question (teamspacePrivilegedQuestion): the one
    // add the server lets them make where they don't manage it
    // (privilegedTeamspaceSelfMembership).
    becomeOwner(currentUserId: string, success: string) {
      return selected ? execute({ action: "add_member", teamspaceId: selected.id, expectedRevision: selected.revision,
        principalType: "user", principalId: currentUserId, memberRole: "owner" }, success) : Promise.resolve(false);
    },
    // Everyone chosen in the invite dialog is added in turn, each with the
    // revision the one before left; the first refusal stops the rest.
    async addMembers(principals: Array<{ principalType: "user" | "group"; principalId: string }>, role: TeamspaceMemberRole, success: string) {
      if (!selected) return 0;
      let revision = selected.revision, added = 0;
      for (const item of principals) {
        const result = await execute({ action: "add_member", teamspaceId: selected.id, expectedRevision: revision,
          principalType: item.principalType, principalId: item.principalId, memberRole: role }, success);
        if (!result) break;
        revision = result.revision;
        added += 1;
      }
      return added;
    },
    // A group made a member of the teamspace just made from it, as Notion's
    // "그룹 정보로 팀스페이스 생성" (its code, 2026-11-11).
    addGroupToCreated(created: { id: string; revision: number }, groupId: string, role: TeamspaceMemberRole, success: string) {
      return execute({ action: "add_member", teamspaceId: created.id, expectedRevision: created.revision,
        principalType: "group", principalId: groupId, memberRole: role }, success);
    },
    removeMember(member: TeamspaceMember, success: string) {
      return selected ? execute({ action: "remove_member", teamspaceId: selected.id,
        expectedRevision: selected.revision, principalType: member.principalType,
        principalId: member.principalId }, success) : Promise.resolve(false);
    },
    respond(request: TeamspaceJoinRequest, response: "approved" | "denied", success: string) {
      return selected ? execute({ action: "respond", teamspaceId: selected.id,
        expectedRevision: selected.revision, joinRequestId: request.id, joinResponse: response }, success)
        : Promise.resolve(false);
    },
    updateSettings(value: boolean, success: string) { return settings ? execute({
      action: "update_settings", expectedRevision: settings.revision, ownersOnlyCreate: value,
    }, success) : Promise.resolve(false); },
    setDefaultTeamspace(teamspace: Pick<DialogTeamspace, "id" | "revision">, success: string) { return execute({
      action: "set_default", teamspaceId: teamspace.id, defaultTeamspaceId: teamspace.id,
      expectedRevision: teamspace.revision,
    }, success); },
    // Archiving and restoring say so in a toast naming the teamspace, as
    // Notion's (seen 2026-11-07); the caller tells, no line stays here.
    archiveSelected() { return selected ? execute({
      action: "archive", teamspaceId: selected.id, expectedRevision: selected.revision,
    }, "") : Promise.resolve(false as const); },
    archiveTeamspace(teamspace: DialogTeamspace) { return execute({
      action: "archive", teamspaceId: teamspace.id, expectedRevision: teamspace.revision,
    }, ""); },
    restore(teamspace: DialogTeamspace) { return execute({
      action: "restore", teamspaceId: teamspace.id, expectedRevision: teamspace.revision,
    }, ""); },
  };
}

