import { useRef, useState } from "react";
import { createPortal } from "react-dom";
import type { TFunction } from "i18next";
import { useTranslation } from "react-i18next";
import { ArchiveIcon, Copy, DotsHorizontal as DotsThree, LeaveIcon, PersonKeyIcon, Plus, Settings, SharePeopleIcon, UserPlusIcon } from "@/icons/leafen";
import type { DialogTeamspace } from "@/components/teamspace/teamspaceDialogModel";
import { SidebarActionMenu, type SidebarMenuRow } from "./SidebarActionMenu";
import { styles } from "./sidebarStyles";
import type { SidebarTeamspaceAction } from "./useSidebarTeamspaceActions";
import { focusOpener } from "@/lib/overlayLayer";

/** A teamspace's "…" in the sidebar, as Notion's (seen 2026-10-06), its rows
 * those of teamspaceMenuRows, as wide as they are (Notion's, measured
 * 2026-11-11: 208px, 282px with "팀스페이스 복제"'s note). */
export function SidebarTeamspaceMenu({ teamspace, canCreate, onManage, onAct }: {
  teamspace: DialogTeamspace; canCreate: boolean;
  onManage(teamspaceId: string, invite: boolean): void; onAct(teamspace: DialogTeamspace, action: SidebarTeamspaceAction): void;
}) {
  const { t } = useTranslation(["sidebar", "teamspaceDialog", "common"]);
  const button = useRef<HTMLButtonElement>(null);
  const [open, setOpen] = useState(false);
  function close(restoreFocus = true) {
    setOpen(false);
    if (restoreFocus) focusOpener(button.current, { preventScroll: true });
  }
  return <>
    <button ref={button} type="button" className={styles.sectionAction} aria-label={t("sidebar:manageTeamspace", { name: teamspace.name })}
      aria-haspopup="menu" aria-expanded={open} onClick={() => setOpen((current) => !current)}>
      <DotsThree size="small" aria-hidden="true" />
    </button>
    {open ? createPortal(<SidebarActionMenu button={button} label={t("sidebar:manageTeamspace", { name: teamspace.name })}
      backdropLabel={t("common:actions.close")} close={close} contentWidth
      rows={teamspaceMenuRows({ t, teamspace, canCreate, onManage, onAct, close })} />, document.body) : null}
  </>;
}

/**
 * A teamspace's rows in its "…" — the sidebar's, its section's (which starts
 * with them, seen 2026-11-11) and 설정 › 팀스페이스's table — as Notion's
 * TeamOverflowMenu gives them to someone who is no workspace owner on an
 * Enterprise plan (its code, 2026-11-11), whatever their workspace role:
 * - "멤버 추가" for those the teamspace lets add members (its owners, or its
 *   members too where they may invite); hidden for the rest;
 * - "팀스페이스 참여" for someone not in an open teamspace (none for a closed
 *   or private one), then "팀스페이스 설정" for its members and "멤버 보기"
 *   for the rest, both opening its settings;
 * - "팀스페이스 복제" for those who may make teamspaces, dim for a default one;
 * - "팀스페이스 나가기" for its members only, dim in a default one — in the
 *   menu's own text colour everywhere, as Notion's: it turns red only for an
 *   Enterprise workspace owner in the settings table or a teamspace's members
 *   tab, when "소유자 되기" or "소유자로 참여" stand beside it (its code,
 *   2026-11-11; never in the sidebar; seen not red in a free workspace's
 *   table);
 * - "팀스페이스 보관" for its owners only, red, dim where it may not go.
 * Leafen's workspace has every Enterprise power (decision 2026-10-12), so a
 * workspace owner who doesn't own the teamspace gets, in 설정 › 팀스페이스
 * (`privileged`), Notion's Enterprise rows first (its code, 2026-11-18):
 * "멤버로 참여" and "소유자로 참여" when not in it, "소유자 되기" and a red
 * "팀스페이스 나가기" when its member, a line, then its settings or members,
 * "멤버 추가" and "팀스페이스 복제" (privilegedTeamspaceMenuRows).
 */
export function teamspaceMenuRows({ t, teamspace, canCreate, privileged = false, onManage, onAct, close }: {
  t: TFunction<["sidebar", "teamspaceDialog", "common"]>; teamspace: DialogTeamspace; canCreate: boolean;
  /** A workspace owner's rows in 설정 › 팀스페이스, as Notion's Enterprise ones. */
  privileged?: boolean;
  onManage(teamspaceId: string, invite: boolean): void; onAct(teamspace: DialogTeamspace, action: SidebarTeamspaceAction): void;
  close(restoreFocus?: boolean): void;
}): SidebarMenuRow[] {
  const teamOwner = teamspace.membershipRole === "owner";
  const member = teamspace.joined;
  // A restricted member adds no one, its owner or not (membersLocked, the server's decision).
  const canInvite = !teamspace.membersLocked && (teamOwner || (member && teamspace.membersCanInvite !== false));
  function act(action: SidebarTeamspaceAction) {
    close(false);
    onAct(teamspace, action);
  }
  const manage = () => { close(false); onManage(teamspace.id, false); };
  if (privileged && !teamOwner) {
    return privilegedTeamspaceMenuRows({ t, teamspace, canCreate, canInvite, act, manage,
      invite: () => { close(false); onManage(teamspace.id, true); } });
  }
  const rows: SidebarMenuRow[] = [];
  if (canInvite) {
    rows.push({ key: "invite", label: t("teamspaceDialog:sidebarAddMembers"), icon: <UserPlusIcon size="regular" aria-hidden="true" />,
      data: { "data-sidebar-teamspace-invite": "" }, run: () => { close(false); onManage(teamspace.id, true); } });
  }
  const first = rows.length;
  if (!member && teamspace.canJoin) {
    rows.push({ key: "join", label: t("teamspaceDialog:sidebarJoin"), icon: <Plus size="regular" aria-hidden="true" />,
      data: { "data-sidebar-teamspace-join": "" }, run: () => act("join") });
  }
  rows.push(member
    ? { key: "settings", label: t("teamspaceDialog:sidebarSettings"), icon: <Settings size="regular" aria-hidden="true" />,
      data: { "data-sidebar-teamspace-settings": "" }, run: manage }
    : { key: "settings", label: t("teamspaceDialog:sidebarViewMembers"), icon: <SharePeopleIcon size="regular" aria-hidden="true" />,
      data: { "data-sidebar-teamspace-settings": "" }, run: manage });
  if (canCreate) {
    rows.push({ key: "duplicate", label: t("teamspaceDialog:sidebarDuplicate"), description: t("teamspaceDialog:sidebarDuplicateDescription"),
      icon: <Copy size="regular" aria-hidden="true" />, disabled: teamspace.isDefault,
      title: teamspace.isDefault ? t("teamspaceDialog:sidebarDuplicateDefault") : undefined,
      data: { "data-sidebar-teamspace-duplicate": "" }, run: () => act("duplicate") });
  }
  if (member) {
    rows.push({ key: "leave", label: t("teamspaceDialog:manageSection.leave"), icon: <LeaveIcon size="regular" aria-hidden="true" />,
      // Someone in it only through a group is told so on choosing it, as Notion's (its code, 2026-11-11).
      disabled: teamspace.isDefault, title: teamspace.isDefault ? t("teamspaceDialog:sidebarLeaveDefault") : undefined,
      data: { "data-sidebar-teamspace-leave": "" }, run: () => act("leave") });
  }
  // Only its owners see it, even the workspace's owners not (Notion's code, 2026-11-11: team role owner).
  if (teamOwner) {
    rows.push({ key: "archive", label: t("teamspaceDialog:manageSection.archive"), icon: <ArchiveIcon size="regular" aria-hidden="true" />,
      // Red, as Notion's (seen 2026-11-07): the line that takes it from everyone's sidebar. A default
      // one goes while another stays; the only one is dim with Notion's tip (seen 2026-11-11).
      danger: true, disabled: Boolean(teamspace.archiveLock),
      title: teamspace.archiveLock === "multipleMembers" ? t("teamspaceDialog:manageSection.archiveMultipleMembers")
        : teamspace.archiveLock === "otherTeamspaces" ? t("teamspaceDialog:manageSection.archiveOtherTeamspaces") : undefined,
      data: { "data-sidebar-teamspace-archive": "" }, run: () => act("archive") });
  }
  // A line parts "멤버 추가" from the rest, as Notion's two sections.
  if (first > 0 && rows.length > first) rows[first] = { ...rows[first], separatorBefore: true };
  return rows;
}

/** The workspace owner's Enterprise rows for a teamspace they don't own (teamspaceMenuRows). */
function privilegedTeamspaceMenuRows({ t, teamspace, canCreate, canInvite, act, manage, invite }: {
  t: TFunction<["sidebar", "teamspaceDialog", "common"]>; teamspace: DialogTeamspace; canCreate: boolean; canInvite: boolean;
  act(action: SidebarTeamspaceAction): void; manage(): void; invite(): void;
}): SidebarMenuRow[] {
  const member = teamspace.joined;
  const rows = selfMembershipRows(t, teamspace, act);
  if (member) {
    rows.push({ key: "leave", label: t("teamspaceDialog:manageSection.leave"), icon: <LeaveIcon size="regular" aria-hidden="true" />, danger: true,
      disabled: teamspace.isDefault, title: teamspace.isDefault ? t("teamspaceDialog:sidebarLeaveDefault") : undefined,
      data: { "data-sidebar-teamspace-leave": "" }, run: () => act("leave") });
  }
  rows.push({ key: "settings", label: t(member ? "teamspaceDialog:sidebarSettings" : "teamspaceDialog:sidebarViewMembers"),
    icon: member ? <Settings size="regular" aria-hidden="true" /> : <SharePeopleIcon size="regular" aria-hidden="true" />,
    separatorBefore: true, data: { "data-sidebar-teamspace-settings": "" }, run: manage });
  if (canInvite) {
    rows.push({ key: "invite", label: t("teamspaceDialog:sidebarAddMembers"), icon: <UserPlusIcon size="regular" aria-hidden="true" />,
      data: { "data-sidebar-teamspace-invite": "" }, run: invite });
  }
  if (canCreate) {
    rows.push({ key: "duplicate", label: t("teamspaceDialog:sidebarDuplicate"), description: t("teamspaceDialog:sidebarDuplicateDescription"),
      icon: <Copy size="regular" aria-hidden="true" />, disabled: teamspace.isDefault,
      title: teamspace.isDefault ? t("teamspaceDialog:sidebarDuplicateDefault") : undefined,
      data: { "data-sidebar-teamspace-duplicate": "" }, run: () => act("duplicate") });
  }
  return rows;
}

/**
 * A workspace owner putting themselves in a teamspace they don't own, as
 * Notion's Enterprise rows (its code, 2026-11-18): "멤버로 참여" and
 * "소유자로 참여" when not in it, "소유자 되기" when its member. An archived
 * one's "…" in 설정 › 팀스페이스 holds them over its way back too, which only
 * its owners take (WorkspaceTeamspacesSection).
 */
export function selfMembershipRows(t: TFunction<["sidebar", "teamspaceDialog", "common"]>, teamspace: DialogTeamspace,
  act: (action: SidebarTeamspaceAction) => void): SidebarMenuRow[] {
  const ownerMark = <PersonKeyIcon size="regular" aria-hidden="true" />;
  return teamspace.joined ? [
    { key: "becomeOwner", label: t("teamspaceDialog:sidebarBecomeOwner"), icon: ownerMark,
      data: { "data-sidebar-teamspace-become-owner": "" }, run: () => act("becomeOwner") },
  ] : [
    { key: "join", label: t("teamspaceDialog:sidebarJoinAsMember"), icon: <Plus size="regular" aria-hidden="true" />,
      data: { "data-sidebar-teamspace-join": "" }, run: () => act("joinAsMember") },
    { key: "joinAsOwner", label: t("teamspaceDialog:sidebarJoinAsOwner"), icon: ownerMark,
      data: { "data-sidebar-teamspace-join-owner": "" }, run: () => act("joinAsOwner") },
  ];
}
