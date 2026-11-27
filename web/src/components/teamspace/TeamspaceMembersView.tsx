import { PersonAvatar } from "@/components/people/PersonAvatar";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { ArrowDown, ArrowUp, Search } from "@/icons/leafen";
import { confirmAction } from "@/components/ConfirmActionDialog";
import type { TeamspaceMember, TeamspaceMemberRole } from "@/teamspaceWire.generated";
import { TeamspaceInviteDialog } from "./TeamspaceInviteDialog";
import { TeamspaceRoleMenu } from "./TeamspaceRoleMenu";
import { teamspaceOwnerDropQuestion, teamspaceRemoveMemberQuestion } from "./teamspaceLeaveQuestion";
import { teamspacePrivilegedQuestion } from "./teamspacePrivilegedQuestion";
import type { TeamspaceDialogController } from "./useTeamspaceDialog";
import styles from "./TeamspaceMembers.module.css";
import { ReadFailed } from "@/components/ReadFailed";

/**
 * A teamspace's members, as Notion's "멤버" tab (seen 2026-10-06): a search,
 * "멤버 추가" opening the invite dialog, and a table of names and roles whose
 * role menu also removes, after asking. What would take your own place in it
 * asks Notion's own questions (seen 2026-11-17): your ownership given up, the
 * owner's (teamspaceOwnerDropQuestion); yourself removed, leaving's (onLeave);
 * someone else removed, Notion's (teamspaceRemoveMemberQuestion). A
 * workspace owner who doesn't manage it may still choose "팀스페이스 소유자"
 * on their own row, after Notion's privileged question, as its Enterprise
 * workspace owner (its code, 2026-11-18: the owner role open to oneself;
 * privilegedTeamspaceSelfMembership).
 */
export function TeamspaceMembersView({ controller, initialInvite = false, onLeave }: {
  controller: TeamspaceDialogController; initialInvite?: boolean;
  /** Leaving it, as its settings' "✓ 참여 중" leaves: asked, said and closed there. */
  onLeave?: () => void;
}) {
  const { t } = useTranslation("teamspaceDialog");
  const [query, setQuery] = useState("");
  const [inviting, setInviting] = useState(initialInvite);
  // Owners first, as Notion's "↓ 역할"; the header turns the order around.
  const [ownersFirst, setOwnersFirst] = useState(true);
  const { busy, canInviteSelected, canManageSelected, managementRead, memberCursor, members, readFailed, selected, workspaceAdmin } = controller;
  if (!selected) return null;
  // The members not read: what failed, with a way to read them again — no
  // empty table, and no "멤버 추가" whose people come from that read.
  if (!managementRead) {
    return (
      <section className={styles.members} data-teamspace-members>
        {readFailed ? <ReadFailed message={readFailed} onRetry={controller.readManagementAgain} /> : null}
        {inviting ? <TeamspaceInviteDialog controller={controller} onClose={() => setInviting(false)} /> : null}
      </section>
    );
  }
  // Roles and removals are its owners' — not a restricted owner's, who changes no one's place
  // (membersLocked, the server's decision; Notion's offers them and refuses, seen 2026-11-18).
  const changesMembers = canManageSelected && !selected.membersLocked;
  const normalized = query.trim().toLocaleLowerCase();
  const rank = (member: TeamspaceMember) => (member.role === "owner") === ownersFirst ? 0 : 1;
  const shown = (normalized ? members.filter((member) => controller.memberLabel(member).toLocaleLowerCase().includes(normalized)) : members)
    .slice().sort((left, right) => rank(left) - rank(right)
      || controller.memberLabel(left).localeCompare(controller.memberLabel(right)));

  // Every role change goes through here; giving up your own ownership asks first.
  async function changeRole(member: TeamspaceMember, role: TeamspaceMemberRole, you: boolean) {
    if (you && role === "owner" && !canManageSelected) {
      if (await confirmAction(teamspacePrivilegedQuestion("owner"))) void controller.becomeOwner(controller.currentUserId, t("notice.memberUpdated"));
      return;
    }
    if (you && member.role === "owner" && role !== "owner" && !await confirmAction(teamspaceOwnerDropQuestion())) return;
    void controller.updateMember(member, role, t("notice.memberUpdated"));
  }

  async function remove(member: TeamspaceMember, you: boolean) {
    if (!selected) return;
    // Removing yourself is leaving it: its question, notice and close.
    if (you && onLeave) { onLeave(); return; }
    if (await confirmAction(teamspaceRemoveMemberQuestion(controller.memberLabel(member), selected.name))) {
      void controller.removeMember(member, t("notice.memberRemoved"));
    }
  }

  return (
    <section className={styles.members} data-teamspace-members>
      <div className={styles.membersHead}>
        <label className={styles.search}>
          <Search size="small" aria-hidden="true" />
          <input value={query} placeholder={t("memberList.search")} aria-label={t("memberList.search")} onChange={(event) => setQuery(event.target.value)} />
        </label>
        {canInviteSelected ? <button type="button" className={styles.addButton} disabled={busy} onClick={() => setInviting(true)} data-teamspace-add-members>
          {t("memberList.add")}
        </button> : null}
      </div>
      {selected.isDefault ? <p className={styles.muted}>{t("defaultMembers")}</p> : null}
      {/* A search that finds no one keeps only the table's top rule and says so under it, its
          "이름 / 역할" head gone, as Notion's (2026-11-19, its Korean and English screens). */}
      <div className={styles.table} role="table" aria-label={t("members")}>
        {shown.length ? <div className={styles.headRow} role="row">
          <span role="columnheader">{t("memberList.name")}</span>
          <span role="columnheader" aria-sort={ownersFirst ? "descending" : "ascending"}>
            <button type="button" className={styles.sortButton} onClick={() => setOwnersFirst((current) => !current)}>
              {ownersFirst ? <ArrowDown size="tiny" aria-hidden="true" /> : <ArrowUp size="tiny" aria-hidden="true" />}
              {t("memberList.role")}
            </button>
          </span>
        </div> : null}
        {shown.map((member) => {
          const label = controller.memberLabel(member);
          const you = member.principalType === "user" && member.principalId === controller.currentUserId;
          // As Notion's row: the name, "(나)", the workspace owner's or a restricted member's badge, the email under it.
          const option = controller.principalOptions.find((item) => item.principalType === member.principalType
            && item.principalId === member.principalId);
          return (
            <div key={member.id} className={styles.row} role="row" data-teamspace-member={member.principalId}>
              <span className={styles.person} role="cell">
                {member.principalType === "user"
                  ? <PersonAvatar className={styles.avatar} userId={member.principalId} name={label} size={32} />
                  : <span className={styles.avatar} aria-hidden="true">{label.trim().slice(0, 1).toUpperCase() || "?"}</span>}
                <span className={styles.personText}>
                  <span className={styles.personName}>
                    <span>{label}</span>
                    {you ? <span className={styles.youMark}>{t("memberList.youMark")}</span> : null}
                    {option?.workspaceOwner ? <span className={styles.roleBadge}>{t("memberList.workspaceOwner")}</span> : null}
                    {option?.restricted ? <span className={styles.restrictedBadge}>{t("memberList.restricted")}</span> : null}
                  </span>
                  {option?.email ? <span className={styles.email}>{option.email}</span> : null}
                </span>
              </span>
              <span role="cell">
                {/* A member who may invite removes members only; roles are the owners', and a workspace owner's own. */}
                {changesMembers || (canInviteSelected || you && workspaceAdmin) && member.role !== "owner" ? (
                  <TeamspaceRoleMenu value={member.role} disabled={busy} label={t("memberList.roleMenu", { name: label })} rolesLocked={!changesMembers}
                    openRole={you && workspaceAdmin && member.principalType === "user" ? "owner" : undefined}
                    onChange={(role) => void changeRole(member, role, you)}
                    onRemove={() => void remove(member, you)} removeDisabled={selected.isDefault} />
                ) : <span className={styles.roleFixed}>{t(member.role === "owner" ? "memberList.ownerRole" : "memberList.memberRole")}</span>}
              </span>
            </div>
          );
        })}
        {normalized && !shown.length ? <p className={styles.noMatch} role="status">{t("memberList.noMatch")}</p> : null}
      </div>
      {/* A further window that failed, read again from where it failed. */}
      {readFailed ? <ReadFailed message={readFailed} onRetry={() => void controller.loadMore("members")} />
        : memberCursor ? <button type="button" className={styles.more} disabled={busy} onClick={() => void controller.loadMore("members")}>{t("loadMore")}</button> : null}
      {inviting ? <TeamspaceInviteDialog controller={controller} onClose={() => setInviting(false)} /> : null}
    </section>
  );
}
