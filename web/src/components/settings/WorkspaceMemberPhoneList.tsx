import type { ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { PersonAvatar } from "@/components/people/PersonAvatar";
import { personDistinction } from "@/lib/peopleDirectory";
import type { WorkspaceMemberDirectoryItem, WorkspaceMemberInvitation } from "@/workspaceMembershipWire.generated";
import type { WorkspaceMemberRole } from "./WorkspaceMemberRoleMenu";
import styles from "./WorkspaceMemberPhoneList.module.css";
import { SelfMark } from "@/components/people/SelfMark";

/**
 * A phone's members, as Notion's phone "멤버" screen (seen 2026-11-12 at
 * 402pt, iPhone Safari, dark, as the owner and as a restricted member): no
 * table, a line for each person — their 28px face 10px in, their name in
 * 16px with "(나)" on one's own and their role's tag, their address in 14px
 * under it, and at the line's end their role, opening its choices where it
 * can be changed (the role menu's phone screen, WorkspaceMemberRoleMenu).
 * Teamspaces and groups stay with the computer's table. An invitation waits
 * on a line of its own with its link to copy and its cancel.
 */
export function WorkspaceMemberPhoneList({ members, invitations, label, roleOf, roleControl, currentUserId, busyActions, onCopyInvitation, onCancelInvitation }: {
  members: readonly WorkspaceMemberDirectoryItem[];
  invitations: readonly WorkspaceMemberInvitation[];
  label(member: WorkspaceMemberDirectoryItem): string;
  roleOf(member: WorkspaceMemberDirectoryItem): WorkspaceMemberRole;
  roleControl(member: WorkspaceMemberDirectoryItem): ReactNode;
  currentUserId: string;
  busyActions: ReadonlySet<string>;
  onCopyInvitation(token: string): void;
  onCancelInvitation(invitationId: string): void;
}) {
  const { t } = useTranslation("workspaceSettingsDialog");
  return (
    <div className={styles.list} role="list" aria-label={t("peopleTabMembers")} data-member-phone-list>
      {members.map((member) => {
        const name = label(member), role = roleOf(member);
        const own = member.userId === currentUserId;
        return (
          <div key={member.id} role="listitem" className={styles.row} data-workspace-member={member.userId}>
            <PersonAvatar className={styles.avatar} userId={member.userId} name={name} size={28} />
            <span className={styles.text}>
              <span className={styles.nameLine}>
                {/* One's own "(나)" ends the name and is cut with it, the role's tag kept whole after them, as Notion's
                    phone line (seen 2026-11-16, Galaxy Chrome at 411: a long name read "다정한 시험 …" before its tag). */}
                <strong className={styles.name}>{name}{own ? <SelfMark className={styles.me} text={t("memberPhoneMe")} /> : null}</strong>
                {role === "member" ? null : <span className={role === "restricted" ? styles.restrictedTag : styles.tag} data-role={role}>
                  {t(role === "owner" ? "memberRoleOwner" : "memberPhoneRestrictedTag")}
                </span>}
              </span>
              <span className={styles.sub}>{member.app ? t("appAccountMember") : member.email || personDistinction(member.userId)}</span>
            </span>
            <span className={styles.role}>{roleControl(member)}</span>
          </div>
        );
      })}
      {invitations.map((invitation) => (
        <div key={invitation.id} role="listitem" className={styles.row} data-invitation="true">
          <PersonAvatar className={styles.avatar} name={invitation.email} size={28} />
          <span className={styles.text}>
            <span className={styles.nameLine}><strong className={styles.name}>{invitation.email}</strong>
              {/* An invitation as a restricted member carries a member's tag of it. */}
              {invitation.restricted ? <span className={styles.restrictedTag} data-role="restricted">{t("memberPhoneRestrictedTag")}</span> : null}</span>
            {invitation.mailFailed ? <span className={styles.failed} data-invitation-mail-failed>{t("invitationMailFailed")}</span>
              : <span className={styles.sub}>{t("invitationPending")}</span>}
          </span>
          <span className={styles.role}>
            {invitation.signupToken ? (
              <button type="button" className={styles.action} data-invitation-copy onClick={() => onCopyInvitation(invitation.signupToken ?? "")}>
                {t("inviteLinkCopy")}
              </button>
            ) : null}
            <button type="button" className={styles.action} disabled={busyActions.has(`cancel:${invitation.id}`)}
              onClick={() => onCancelInvitation(invitation.id)}>
              {t("cancelInvitation")}
            </button>
          </span>
        </div>
      ))}
    </div>
  );
}
