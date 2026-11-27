import { useEffect } from "react";
import { useTranslation } from "react-i18next";
import { useCopyNotice } from "@/lib/copyNotice";
import { useProductToast } from "@/lib/productToast";
import { useClosedForLink } from "@/lib/productWindows";
import { serverUrl } from "@/lib/serverOrigin";
import type { Session, Workspace } from "@/types";
import { useWorkspacePeople } from "./useWorkspacePeople";
import { WorkspaceAddMembersDialog, type WorkspaceAddMembersResult } from "./WorkspaceAddMembersDialog";
import { addWorkspaceMembers } from "./workspaceMemberAdds";

const keepWorkspaces = async () => {};

/** "멤버 초대" from the workspace menu: Notion opens its "멤버 추가하기" window
 *  alone over the page, not over the settings window (seen 2026-11-06). What
 *  happened shows as a toast whose "사람" opens who was added or is waiting.
 *  Where this person cannot add or ask, 사람 opens with the reason instead. */
export function WorkspaceInviteDialog({ onClose, onOpenPeople, session, workspace }: {
  onClose(): void;
  onOpenPeople(): void;
  session: Session;
  workspace: Workspace;
}) {
  const { t } = useTranslation("workspaceSettingsDialog");
  const { t: tSidebar } = useTranslation("sidebar");
  const { notify } = useProductToast();
  // A link going to a place here closes it, as the settings window (closeWindowsForLink).
  useClosedForLink(onClose);
  const copyNotice = useCopyNotice();
  const people = useWorkspacePeople({ onWorkspaceRefresh: keepWorkspaces, session, workspace });
  const snapshot = people.snapshot;
  // The window opens at once, as Notion's: the people here and the policy
  // come as the list is read, sending waits for them (a busy server held the
  // window back with no sign for as long as its read was retried).
  const canManage = snapshot ? snapshot.currentMember.role === "owner" : workspace.role !== "member";
  const addsOff = snapshot ? snapshot.memberAddPolicy === "disabled" || (!canManage && snapshot.memberRequests !== true) : false;
  const failed = !snapshot && !people.loading && Boolean(people.readFailed);
  useEffect(() => {
    if (addsOff || failed) onOpenPeople();
  }, [addsOff, failed, onOpenPeople]);
  if (addsOff || failed) return null;

  async function addMembers(emails: string[], message: string, picked: readonly { userId: string }[] = [], restricted = false): Promise<WorkspaceAddMembersResult> {
    const result = await addWorkspaceMembers(people.addMember, t, emails, message, picked, people.addMemberById, restricted);
    // A line to read, with the next step: the invitation's link to hand on
    // where no mail carries it, as its pending row's "링크 복사" copies it;
    // else the way to who was added or is waiting.
    const token = result.signupToken;
    if (result.notice) notify(result.notice, "default", token
      ? { label: t("inviteLinkCopy"), onClick: () => void copyNotice(serverUrl(`/#invitation=${encodeURIComponent(token)}`), t("inviteLinkCopied")) }
      : { label: tSidebar("people"), onClick: onOpenPeople });
    return { added: result.added, error: result.error };
  }

  return (
    <WorkspaceAddMembersDialog session={session} workspace={workspace} request={!canManage}
      members={snapshot ? snapshot.members.filter((member) => !member.app) : []} invitations={snapshot?.invitations ?? []}
      busy={people.busyActions.has("addMember")} loading={!snapshot}
      onAdd={addMembers} onClose={onClose} />
  );
}
