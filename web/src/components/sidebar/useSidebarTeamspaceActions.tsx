import { useState } from "react";
import { useTranslation } from "react-i18next";
import { LeafenApiError } from "@/apiTransport";
import { confirmAction } from "@/components/ConfirmActionDialog";
import { TeamspaceArchiveConfirm } from "@/components/teamspace/TeamspaceArchiveConfirm";
import { teamspaceLeaveQuestion } from "@/components/teamspace/teamspaceLeaveQuestion";
import { teamspacePrivilegedQuestion } from "@/components/teamspace/teamspacePrivilegedQuestion";
import { duplicateTeamspaceName, teamspaceLeaveGroups, type DialogTeamspace } from "@/components/teamspace/teamspaceDialogModel";
import { newId } from "@/lib/ids";
import { useProductToast } from "@/lib/productToast";
import { createTeamspaceApi } from "@/teamspaceApi";
import { teamspaceErrorCodes, type TeamspaceMutationInput, type TeamspaceMutationResponse } from "@/teamspaceWire.generated";
import type { Session, Workspace } from "@/types";

export type SidebarTeamspaceAction = "join" | "joinAsMember" | "joinAsOwner" | "becomeOwner" | "duplicate" | "leave" | "archive";

/**
 * What a teamspace's "…" does without opening its settings, as Notion's (seen
 * 2026-10-06): joining an open one someone is not in, said at the window's
 * foot with its name ("〈팀〉 팀에 참여했습니다.", seen 2026-11-12); a copy of
 * its settings, opened at once; leaving it, which the
 * last owner is told needs another owner first, and someone in it only
 * through a group which groups keep them in, asked first
 * (teamspaceLeaveQuestion) and said as "〈팀〉 팀에서 나옴" (Notion's words); archiving it
 * once its name is typed; and, for a workspace owner in 설정 › 팀스페이스,
 * putting themselves in it as its member or its owner — an open one joined
 * as ever, the rest asked first as Notion's privileged step
 * (teamspacePrivilegedQuestion, privilegedTeamspaceSelfMembership).
 */
export function useSidebarTeamspaceActions({ session, workspace, teamspaces, onChanged, onOpenSettings }: {
  session: Session; workspace: Workspace | null; teamspaces: readonly DialogTeamspace[];
  onChanged(): void; onOpenSettings(teamspaceId: string): void;
}) {
  const { t } = useTranslation("teamspaceDialog");
  const { notify } = useProductToast();
  const [archiving, setArchiving] = useState<DialogTeamspace | null>(null);

  async function mutate(input: Omit<TeamspaceMutationInput, "requestId">): Promise<TeamspaceMutationResponse | null> {
    if (!workspace) return null;
    try {
      return await createTeamspaceApi().mutateTeamspace(session, workspace.id, { ...input, requestId: newId() });
    } catch (cause) {
      const code = cause instanceof LeafenApiError ? cause.code : "";
      notify(code === teamspaceErrorCodes.lastOwner ? t("leave.lastOwner")
        : code === teamspaceErrorCodes.defaultMember ? t("error.defaultMember")
          : code === teamspaceErrorCodes.defaultRequired ? t("error.defaultRequired")
            : code === teamspaceErrorCodes.conflict ? t("error.conflict") : t("error.action"), "error");
      return null;
    }
  }

  async function act(teamspace: DialogTeamspace, chosen: SidebarTeamspaceAction) {
    const action = chosen === "joinAsMember" && teamspace.canJoin ? "join" : chosen;
    if (action === "joinAsMember" || action === "joinAsOwner" || action === "becomeOwner") {
      // An open one is joined as ever, an archived one too: only an owner's place or a way into a closed or private one asks.
      const owner = action !== "joinAsMember";
      if ((owner || teamspace.access !== "open") && !await confirmAction(teamspacePrivilegedQuestion(owner ? "owner" : "join"))) return;
      if (!await mutate({ action: "add_member", teamspaceId: teamspace.id, expectedRevision: teamspace.revision,
        principalType: "user", principalId: session.user.id, memberRole: owner ? "owner" : "member" })) return;
      if (action !== "becomeOwner") notify(t("notice.joinedName", { name: teamspace.name }), "success");
      onChanged();
      return;
    }
    if (action === "archive") {
      setArchiving(teamspace);
      return;
    }
    if (action === "join") {
      if (!await mutate({ action: "join", teamspaceId: teamspace.id, expectedRevision: teamspace.revision })) return;
      notify(t("notice.joinedName", { name: teamspace.name }), "success");
      onChanged();
      return;
    }
    if (action === "duplicate") {
      const copy = await mutate({
        action: "create", expectedRevision: 0, name: duplicateTeamspaceName(teamspace.name, teamspaces.map((item) => item.name)),
        icon: teamspace.icon, description: teamspace.description, access: teamspace.access,
        memberPageRole: teamspace.memberPageRole, openPageRole: teamspace.openPageRole,
        membersCanInvite: teamspace.membersCanInvite, membersCanEditSidebar: teamspace.membersCanEditSidebar,
      });
      if (!copy?.teamspace) return;
      notify(t("notice.duplicated"), "success");
      onChanged();
      onOpenSettings(copy.teamspace.id);
      return;
    }
    // Notion's names the groups, each in quotes (its code, 2026-11-11).
    const groups = teamspaceLeaveGroups(teamspace);
    if (groups.length) {
      await confirmAction({ message: t("leave.groupMember", { groupNames: groups.map((name) => `"${name}"`).join(", ") }),
        confirmLabel: t("leave.ok"), cancelLabel: null });
      return;
    }
    if (teamspace.membershipRole === "owner" && teamspace.summary.ownerCount < 2) {
      await confirmAction({ message: t("leave.lastOwner"), confirmLabel: t("leave.ok"), cancelLabel: null });
      return;
    }
    const confirmed = await confirmAction(teamspaceLeaveQuestion(teamspace.name));
    if (!confirmed || !await mutate({ action: "leave", teamspaceId: teamspace.id, expectedRevision: teamspace.revision })) return;
    notify(t("notice.leftName", { name: teamspace.name }), "success");
    onChanged();
  }

  const archiveConfirm = archiving ? <TeamspaceArchiveConfirm name={archiving.name} onClose={() => setArchiving(null)}
    onArchive={async () => {
      if (!await mutate({ action: "archive", teamspaceId: archiving.id, expectedRevision: archiving.revision })) return false;
      notify(t("notice.archived", { name: archiving.name }), "success");
      onChanged();
      return true;
    }} /> : null;

  return { act, archiveConfirm };
}
