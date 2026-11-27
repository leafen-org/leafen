import { personDistinction } from "@/lib/peopleDirectory";
import { useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { confirmAction } from "@/components/ConfirmActionDialog";
import { PersonAvatar } from "@/components/people/PersonAvatar";
import { SidebarActionMenu, type SidebarMenuRow } from "@/components/sidebar/SidebarActionMenu";
import { GroupPeopleIcon } from "@/icons/leafen";
import { TeamspaceDialog } from "@/components/TeamspaceDialog";
import { serverUrl } from "@/lib/serverOrigin";
import type { Session, Workspace } from "@/types";
import type { WorkspaceMemberDirectoryItem, WorkspaceMemberInvitation } from "@/workspaceMembershipWire.generated";
import { sortWorkspaceMembers, type WorkspaceMemberSort, type WorkspaceMemberSortKey } from "./workspaceMemberTableModel";
import { WorkspaceMemberSortHead } from "./WorkspaceMemberSortHead";
import { WorkspaceMemberRoleMenu, workspaceMemberRoleName, type WorkspaceMemberRole } from "./WorkspaceMemberRoleMenu";
import { WorkspaceMemberPhoneList } from "./WorkspaceMemberPhoneList";
import { WorkspaceMemberRemoveSheet } from "./WorkspaceMemberRemoveSheet";
import tableStyles from "./WorkspaceMemberTable.module.css";
import { styles } from "./workspaceSettingsOverviewStyles";
import { useCopyNotice } from "@/lib/copyNotice";
import { KeptIconGlyph } from "@/components/teamspace/KeptIconGlyph";
import { useSidebarLayout } from "@/components/shell/AppShell";
import { SettingsChevron } from "./SettingsChevron";

function workspaceMemberLabel(member: WorkspaceMemberDirectoryItem) {
  return member.displayName.trim() || member.email.trim() || member.userId;
}

type OpenMemberships = { userId: string; kind: "teamspaces" | "groups" };

/** A person's list of more teamspaces than this gets a filter over it, as Notion's (its code, 2026-11-09). */
const FILTER_FROM = 6;

/**
 * 설정 → 사람's members as Notion's table (seen 2026-10-09 and 2026-10-10):
 * 사용자 (sortable), 팀스페이스 and 그룹 — how many, opening their names —
 * and 역할 (sortable), the owner changing each member's from its menu. A
 * teamspace in a person's list opens that teamspace's members, as Notion's
 * (seen 2026-11-09).
 */
export function WorkspaceMemberTable({ members, invitations, emptyText, session, workspace, canManage, restrictable, busyActions, onMakeOwner, onRestrict, onRemove, onCancelInvitation }: {
  members: readonly WorkspaceMemberDirectoryItem[];
  invitations: readonly WorkspaceMemberInvitation[];
  /** What the row under the head says when no one is listed (a search that finds no one). */
  emptyText?: string;
  session: Session;
  workspace: Workspace;
  canManage: boolean;
  /** The organization module offers the restricted member role. */
  restrictable: boolean;
  busyActions: ReadonlySet<string>;
  onMakeOwner(userId: string): void;
  onRestrict(userId: string, restricted: boolean): void;
  onRemove(userId: string): void;
  onCancelInvitation(invitationId: string): void;
}) {
  const { t, i18n } = useTranslation("workspaceSettingsDialog");
  const { phone } = useSidebarLayout(true);
  // The row's button keeps its words while a toast says the link was copied.
  const notice = useCopyNotice();
  function copyInvitationLink(token: string) {
    return notice(serverUrl(`/#invitation=${encodeURIComponent(token)}`), t("inviteLinkCopied"));
  }
  const [sort, setSort] = useState<WorkspaceMemberSort>({ key: "user", descending: false });
  const [open, setOpen] = useState<OpenMemberships | null>(null);
  const [filter, setFilter] = useState("");
  const [teamspace, setTeamspace] = useState<string | null>(null);
  const trigger = useRef<HTMLButtonElement>(null);
  // An app's account is named as the app it is, and managed with the
  // workspace's apps, not as a person.
  const label = (member: WorkspaceMemberDirectoryItem) => member.app
    ? t("appAccountName", { name: workspaceMemberLabel(member) }) : workspaceMemberLabel(member);
  const sorted = sortWorkspaceMembers(members, sort, label, i18n.language);

  const head = (key: WorkspaceMemberSortKey, text: string) => (
    <WorkspaceMemberSortHead sortKey={key} text={text} sort={sort} onSort={setSort} />
  );

  const memberships = (member: WorkspaceMemberDirectoryItem, kind: OpenMemberships["kind"]) => {
    const count = kind === "teamspaces" ? member.teamspaceCount ?? 0 : member.groupCount ?? 0;
    // No teamspace reads "사용 권한 없음", no group "없음", as Notion's (seen 2026-11-09).
    if (count === 0) return <span className={tableStyles.none}>{t(kind === "teamspaces" ? "memberTeamspaceNoAccess" : "memberMembershipNone")}</span>;
    const expanded = open?.userId === member.userId && open.kind === kind;
    return (
      <button type="button" className={tableStyles.countButton} aria-haspopup="menu" aria-expanded={expanded}
        onClick={(event) => { trigger.current = event.currentTarget; setFilter(""); setOpen(expanded ? null : { userId: member.userId, kind }); }}>
        {t(kind === "teamspaces" ? "memberTeamspaceCount" : "memberGroupCount", { count })}
        {/* A count that opens its list, as Notion's "팀스페이스 N개 ⌄" (seen 2026-11-07), its ⌄ as wide as its glyph (2026-11-16). */}
        <SettingsChevron phone={false} />
      </button>
    );
  };

  const busy = (userId: string) => ["owner", "remove", "role"].some((action) => busyActions.has(`${action}:${userId}`));
  // Leafen's workspace has one owner, so making someone it hands the
  // workspace over: asked first, unlike Notion's (which keeps its owners).
  async function confirmOwner(userId: string, name: string) {
    if (await confirmAction({ message: t("memberOwnerConfirm", { name }), detail: t("memberOwnerConfirmDetail"),
      confirmLabel: t("makeOwner"), danger: true })) onMakeOwner(userId);
  }
  // The owner's own line offers what Notion's does, and says why it can't
  // be done yet: stepping down is handing the workspace over.
  async function ownerStays() {
    await confirmAction({ message: t("memberOwnerStays"), detail: t("memberOwnerStaysDetail"),
      confirmLabel: t("memberOwnerStaysOk"), cancelLabel: null });
  }
  // Removing someone asks first, as Notion's does (seen 2026-10-08); a phone's asks in a whole sheet (WorkspaceMemberRemoveSheet).
  const [removing, setRemoving] = useState<{ userId: string; name: string } | null>(null);
  async function confirmRemove(userId: string, name: string) {
    if (phone) { setRemoving({ userId, name }); return; }
    if (await confirmAction({ message: t("memberRemoveConfirm", { name }), detail: t("memberRemoveConfirmDetail"),
      confirmLabel: t("memberRemoveFromWorkspace"), danger: true })) onRemove(userId);
  }

  const roleOf = (member: WorkspaceMemberDirectoryItem): WorkspaceMemberRole => member.role === "owner" ? "owner" : member.restricted ? "restricted" : "member";
  // A person's role: its menu where it can be changed, else its name — a member's own too, as Notion's member
  // reads "멤버" there and leaves from 일반 › 위험 구역 (seen 2026-11-21 in two tabs; WorkspaceMemberGeneral).
  function roleControl(member: WorkspaceMemberDirectoryItem) {
    const name = label(member), role = roleOf(member);
    const isOwner = member.role === "owner", isCurrent = member.userId === session.user.id;
    const menu = { value: role, label: t("memberRoleMenu", { name }), restrictable };
    if (canManage && !isOwner && !isCurrent && !member.app) {
      return <WorkspaceMemberRoleMenu {...menu} disabled={busy(member.userId)} onRemove={() => void confirmRemove(member.userId, name)}
        onChange={(next) => {
          if (next === "owner") void confirmOwner(member.userId, name);
          else onRestrict(member.userId, next === "restricted");
        }} />;
    }
    if (canManage && isOwner && isCurrent) {
      return <WorkspaceMemberRoleMenu {...menu} own disabled={false} onRemove={() => void ownerStays()} onChange={() => void ownerStays()} />;
    }
    return <span className={tableStyles.role}>{workspaceMemberRoleName(t, role)}</span>;
  }

  if (phone) {
    return <>
      <WorkspaceMemberPhoneList members={sorted} invitations={invitations} label={label} roleOf={roleOf} roleControl={roleControl}
        currentUserId={session.user.id} busyActions={busyActions}
        onCopyInvitation={(token) => void copyInvitationLink(token)} onCancelInvitation={onCancelInvitation} />
      {removing ? <WorkspaceMemberRemoveSheet userId={removing.userId} name={removing.name}
        avatar={members.find((member) => member.userId === removing.userId)?.avatar}
        onConfirm={() => onRemove(removing.userId)} onClose={() => setRemoving(null)} /> : null}
    </>;
  }

  const openMember = open ? members.find((member) => member.userId === open.userId) : undefined;
  const menuRows: SidebarMenuRow[] = [];
  const filtering = open?.kind === "teamspaces" && (openMember?.teamspaces?.length ?? 0) >= FILTER_FROM;
  if (open && openMember) {
    const wanted = filter.trim().toLocaleLowerCase(i18n.language);
    // Each row as Notion's: its mark, its name and "— N명의 멤버" ("멤버 N명" for a group).
    const listed = open.kind === "teamspaces"
      ? (openMember.teamspaces ?? [])
        .filter((entry) => !filtering || entry.name.toLocaleLowerCase(i18n.language).includes(wanted))
        .map((entry) => ({
          key: entry.id, label: entry.name,
          icon: <span className={tableStyles.teamspaceIcon} aria-hidden="true"><KeptIconGlyph icon={entry.icon} fallback={entry.name.trim().slice(0, 1).toUpperCase() || "T"} /></span>,
          note: entry.memberCount === undefined ? undefined : t("memberTeamspaceMembers", { count: entry.memberCount }),
          run: () => { setOpen(null); setTeamspace(entry.id); },
        }))
      : (openMember.groups ?? []).map((group) => ({
        key: group.id, label: group.name,
        icon: <span className={tableStyles.teamspaceIcon} aria-hidden="true"><KeptIconGlyph icon={group.icon} fallback={<GroupPeopleIcon size="tiny" />} /></span>,
        note: group.memberCount === undefined ? undefined : t("memberGroupMembers", { count: group.memberCount }),
        run: () => setOpen(null),
      }));
    menuRows.push(...listed);
    const total = open.kind === "teamspaces" ? openMember.teamspaceCount ?? 0 : openMember.groupCount ?? 0;
    const shown = open.kind === "teamspaces" ? openMember.teamspaces?.length ?? 0 : openMember.groups?.length ?? 0;
    if (total > shown) {
      menuRows.push({ key: "more", label: t("memberMembershipsMore", { count: total - shown }), disabled: true });
    }
  }

  return (
    <div className={tableStyles.scroller}>
      <div role="table" className={`${tableStyles.table} ${tableStyles.members}`} aria-label={t("peopleTabMembers")}>
        <div role="row" className={`${tableStyles.row} ${tableStyles.headRow}`}>
          {head("user", t("memberColumnUser"))}
          <div role="columnheader" className={tableStyles.headCell}><span className={tableStyles.headLabel}>{t("memberColumnTeamspaces")}</span></div>
          <div role="columnheader" className={tableStyles.headCell}><span className={tableStyles.headLabel}>{t("memberColumnGroups")}</span></div>
          {head("role", t("memberColumnRole"))}
        </div>
        {!sorted.length && !invitations.length && emptyText ? <div role="row" className={tableStyles.emptyRow}><span role="cell">{emptyText}</span></div> : null}
        {sorted.map((member) => {
          const name = label(member);
          const isCurrent = member.userId === session.user.id;
          return (
            <div key={member.id} role="row" className={tableStyles.row} data-workspace-member={member.userId}>
              <div role="cell" className={tableStyles.userCell}>
                <PersonAvatar className={tableStyles.avatar} userId={member.userId} name={name} size={28} />
                <span className={styles.memberText}>
                  <strong>{name}</strong>
                  <span className={tableStyles.userSub}>{[member.app ? t("appAccountMember") : member.email || personDistinction(member.userId), isCurrent ? t("me") : ""].filter(Boolean).join(" · ")}</span>
                </span>
              </div>
              <div role="cell" className={tableStyles.cell}>{member.app ? null : memberships(member, "teamspaces")}</div>
              <div role="cell" className={tableStyles.cell}>{member.app ? null : memberships(member, "groups")}</div>
              <div role="cell" className={`${tableStyles.cell} ${tableStyles.roleCell}`}>{roleControl(member)}</div>
            </div>
          );
        })}
        {invitations.map((invitation) => (
          // Where the server sends no mail, the owner copies the invitation's
          // sign-up link to hand on (2026-10-18).
          <div key={invitation.id} role="row" className={tableStyles.row} data-invitation="true">
            <div role="cell" className={tableStyles.userCell}>
              <PersonAvatar className={tableStyles.avatar} name={invitation.email} size={28} />
              <span className={styles.memberText}>
                <strong>{invitation.email}</strong>
                {invitation.mailFailed ? <span className={tableStyles.mailFailed} data-invitation-mail-failed>{t("invitationMailFailed")}</span>
                  : <span className={tableStyles.userSub}>{t("invitationPending")}</span>}
              </span>
            </div>
            <div role="cell" className={tableStyles.cell} />
            <div role="cell" className={tableStyles.cell} />
            <div role="cell" className={`${tableStyles.cell} ${tableStyles.roleCell} ${tableStyles.invitationRoleCell}`}>
              {/* The role the invitation brings (WorkspaceAddMembersRole). */}
              <span className={tableStyles.role}>{workspaceMemberRoleName(t, invitation.restricted ? "restricted" : "member")}</span>
              {invitation.signupToken ? (
                <button type="button" className={styles.memberAction} data-invitation-copy
                  onClick={() => void copyInvitationLink(invitation.signupToken ?? "")}>
                  {t("inviteLinkCopy")}
                </button>
              ) : null}
              <button type="button" className={styles.memberAction}
                disabled={busyActions.has(`cancel:${invitation.id}`)} onClick={() => onCancelInvitation(invitation.id)}>
                {t("cancelInvitation")}
              </button>
            </div>
          </div>
        ))}
      </div>
      {open && openMember ? (
        <SidebarActionMenu button={trigger} rows={menuRows} backdropLabel={t("common:actions.close")}
          label={t(open.kind === "teamspaces" ? "ariaMemberTeamspaces" : "ariaMemberGroups", { name: label(openMember) })}
          close={() => setOpen(null)}
          search={filtering ? { label: t("memberTeamspaceFilter"), query: filter, onQuery: setFilter } : undefined}
          width={filtering ? 280 : undefined} fit={350} />
      ) : null}
      {teamspace ? (
        <TeamspaceDialog session={session} workspace={workspace} initialView="manage" initialTeamspaceId={teamspace}
          initialTab="members" onClose={() => setTeamspace(null)} />
      ) : null}
    </div>
  );
}
