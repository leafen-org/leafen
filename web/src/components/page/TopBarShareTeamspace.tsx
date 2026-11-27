import { useState } from "react";
import { useTranslation } from "react-i18next";
import { ChevronRight, ChevronUpDown, GroupPeopleIcon } from "@/icons/leafen";
import { publicMutationRoles, type PublicMutationTeamspace } from "@/publicMutationWire.generated";
import { sharePermissionLabel, TopBarSharePermissionPicker, type ShareRoleChoice } from "./TopBarSharePermissionPicker";
import { styles } from "./topBarStyles";
import access from "./TopBarShareAccess.module.css";
import inherited from "./TopBarShareInherited.module.css";
import { KeptIconGlyph } from "@/components/teamspace/KeptIconGlyph";

export type TeamspaceShareGroup = "owner" | "member";

const RANK: readonly ShareRoleChoice[] = ["none", publicMutationRoles.view, publicMutationRoles.comment, publicMutationRoles.edit, publicMutationRoles.fullAccess];
// A group may also have no access to a page, as Notion's "사용 권한 없음" (seen 2026-10-13).
const ROLES: ShareRoleChoice[] = [publicMutationRoles.fullAccess, publicMutationRoles.edit, publicMutationRoles.comment, publicMutationRoles.view, "none"];

/**
 * A teamspace page's teamspace in its share window, as Notion's (seen
 * 2026-10-12): "〈팀스페이스〉의 멤버 · 팀스페이스 · N명의 사용자" with the
 * widest role and ⇕, which opens to "소유자 · 팀 소유자 N명" and "멤버 · 멤버
 * N명", each with its role's menu. It is the window's other groups' kind of
 * row (TopBarShareInherited), as Notion's (measured 2026-11-17 at 1440, twice):
 * under the pointer a chevron takes its mark's place, turned down while it
 * is open, and open it hides its role; pressed, it grounds faintly until the
 * pointer leaves it, Notion's pressed look (its props, read 2026-11-17 —
 * a pointer only passing over it grounds nothing); each group 16px in under it with Notion's
 * two people on a round tint. Changing one below the teamspace's top says
 * it overrides the pages above — a database row's page unlinks from its
 * database's (seen 2026-11-11). Nobody changes the group they are in.
 */
export function TopBarShareTeamspace({ teamspace, disabled, readOnly, onChange }: {
  teamspace: PublicMutationTeamspace; disabled: boolean; readOnly: boolean;
  onChange(group: TeamspaceShareGroup, role: ShareRoleChoice): void;
}) {
  const { t } = useTranslation("topBar");
  const [open, setOpen] = useState(false);
  const widest = RANK.indexOf(teamspace.ownerRole) > RANK.indexOf(teamspace.role) ? teamspace.ownerRole : teamspace.role;
  const groups: { group: TeamspaceShareGroup; label: string; count: string; role: ShareRoleChoice }[] = [
    { group: "owner", label: t("teamspaceOwners"), count: t("teamspaceOwnerCount", { count: teamspace.ownerCount }), role: teamspace.ownerRole },
    { group: "member", label: t("teamspaceMembersGroup"), count: t("teamspaceMemberCount", { count: teamspace.memberCount }), role: teamspace.role },
  ];
  return <>
    <button type="button" className={`${styles.shareRow} ${inherited.groupRow} ${inherited.teamspaceHead}`} aria-expanded={open}
      onClick={() => setOpen(current => !current)} data-share-teamspace-row
      onPointerDown={(event) => event.currentTarget.setAttribute("data-pressed", "")}
      onPointerLeave={(event) => event.currentTarget.removeAttribute("data-pressed")}>
      <span className={inherited.groupLead} aria-hidden="true">
        <ChevronRight size="regular" className={`${inherited.groupChevron} ${inherited.teamspaceChevron}`} />
        <span className={`${access.teamspaceIcon} ${inherited.teamspaceLeadIcon}`}>
          <KeptIconGlyph icon={teamspace.icon} fallback={teamspace.name.trim().slice(0, 1).toUpperCase() || "T"} />
        </span>
      </span>
      <span className={styles.shareText}><span className={inherited.groupTitle}>{t("teamspaceMembers", { name: teamspace.name })}</span>
        <span>{t("teamspacePeople", { count: teamspace.memberCount })}</span></span>
      {open ? null : <span className={`${styles.sharePermission} ${inherited.teamspaceRole}`}>
        {sharePermissionLabel(widest, t)} <ChevronUpDown size="tiny" aria-hidden="true" />
      </span>}
    </button>
    {open ? groups.map(({ group, label, count, role }) => <div key={group} className={`${styles.shareRow} ${inherited.teamspaceGroup}`}
      data-share-teamspace-group={group}>
      <span className={`${inherited.groupIcon} ${inherited.teamspaceGroupIcon}`} aria-hidden="true"><GroupPeopleIcon pixels={20} weight="fill" /></span>
      <span className={styles.shareText}><span>{label}</span><span>{count}</span></span>
      <TopBarSharePermissionPicker value={role} roles={ROLES} readOnly={readOnly} disabled={disabled || teamspace.viewerGroup === group}
        note={teamspace.changeUnlinks ? t("teamspaceRoleNote") : undefined}
        onChange={next => { if (next !== role) onChange(group, next); }} />
    </div>) : null}
  </>;
}
