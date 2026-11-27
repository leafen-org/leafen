import { personDistinction } from "@/lib/peopleDirectory";
import { useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";

import { AppSelect } from "@/components/AppSelect";
import { confirmAction } from "@/components/ConfirmActionDialog";
import { PersonAvatar } from "@/components/people/PersonAvatar";
import { SidebarActionMenu, type SidebarMenuRow } from "@/components/sidebar/SidebarActionMenu";
import { DotsHorizontal } from "@/icons/leafen";
import { requestAccountHistory } from "@/lib/accountHistory";
import type { OrganizationSettingsMutation } from "@/lib/workspaceOrganizationSettings";
import {
  organizationRoles,
  organizationSettingsActions,
  type OrganizationRole,
  type OrganizationSettingsMember,
} from "@/organizationWire.generated";
import {
  editableOrganizationRoles,
  organizationAdminRoleLabel,
  organizationMemberLabel,
  organizationRoleLabel,
} from "./organizationSettingsModel";
import { SettingsSearchToggle } from "./SettingsSearchToggle";
import groupStyles from "./WorkspaceGroups.module.css";
import { sortWorkspaceMembers, type WorkspaceMemberSort } from "./workspaceMemberTableModel";
import { WorkspaceMemberSortHead } from "./WorkspaceMemberSortHead";
import tableStyles from "./WorkspaceMemberTable.module.css";
import local from "./WorkspaceOrganizationMembers.module.css";
import { styles } from "./workspaceSettingsOverviewStyles";

/** An organization role's place when the table is sorted by role: the owner first, then as the roles are listed. */
const roleRank = Object.fromEntries(Object.values(organizationRoles).map((role, index) => [role, index])) as Record<OrganizationRole, number>;

/**
 * The organization's people, as 사람's member table: who, their role, and
 * what else can be done with them under "⋯" (seen on Notion's member rows).
 * Its order is that table's too — by name, A to Z at first, under
 * "사용자 ↑", or by role (2026-11-15: rows came as the server listed them,
 * the owner between the others).
 */
export function WorkspaceOrganizationMembers({
  organizationId,
  members,
  userId,
  disabled,
  mutate,
}: {
  /** Opens a member's history (AccountHistoryDialog), where given. */
  organizationId?: string;
  members: OrganizationSettingsMember[];
  userId: string;
  disabled: boolean;
  mutate: (mutation: OrganizationSettingsMutation) => void | Promise<void>;
}) {
  const { t, i18n } = useTranslation(["workspaceSettingsDialog", "common"]);
  const [query, setQuery] = useState("");
  const [sort, setSort] = useState<WorkspaceMemberSort>({ key: "user", descending: false });
  const [openId, setOpenId] = useState("");
  const trigger = useRef<HTMLButtonElement>(null);
  const filtered = useMemo(() => {
    const normalized = query.trim().toLocaleLowerCase();
    const found = !normalized ? members : members.filter((member) => (
      member.displayName.toLocaleLowerCase().includes(normalized)
      || member.email.toLocaleLowerCase().includes(normalized)
    ));
    return sortWorkspaceMembers(found, sort, (member) => organizationMemberLabel(member, userId, t), i18n.language,
      (member) => roleRank[member.role] ?? Number.MAX_SAFE_INTEGER);
  }, [members, query, sort, userId, t, i18n.language]);

  // A row's action starts with the menu closed: a window it opens takes the keys.
  const closing = (action: () => void) => () => { setOpenId(""); action(); };
  const rowsFor = (member: OrganizationSettingsMember): SidebarMenuRow[] => {
    const label = organizationMemberLabel(member, userId, t);
    const deactivated = member.status === "deactivated";
    const rows: SidebarMenuRow[] = [];
    if (organizationId) {
      rows.push({
        key: "history", label: t("accountHistory.title"), data: { "data-member-history": member.userId },
        run: closing(() => requestAccountHistory({ organizationId, userId: member.userId, name: label })),
      });
    }
    if (member.canTransferOwnership) {
      rows.push({
        key: "owner", label: t("makeOwner"), disabled,
        run: closing(() => void confirmAction({
          // Passing ownership makes the owner a member; only the new owner can pass it back.
          message: t("makeOwnerConfirm", { name: label }), detail: t("makeOwnerConfirmDetail"),
          confirmLabel: t("makeOwner"), danger: true,
        }).then((confirmed) => {
          if (confirmed) void mutate({ action: organizationSettingsActions.ownerTransfer, targetId: member.id });
        })),
      });
    }
    if (member.canManageLifecycle) {
      rows.push({
        key: "lifecycle", label: deactivated ? t("reactivate") : t("deactivate"), disabled,
        description: deactivated ? undefined : t("deactivateDescription"),
        run: closing(() => void mutate({
          action: deactivated ? organizationSettingsActions.memberReactivate : organizationSettingsActions.memberDeactivate,
          targetId: member.id,
        })),
      });
      rows.push({
        // Someone who owns a workspace stays until it has passed to someone
        // else, which that workspace's settings do.
        key: "remove", label: t("remove"), separatorBefore: true, disabled: disabled || member.requiresContentReassignment,
        description: member.requiresContentReassignment ? t("removeOwnsWorkspace") : undefined,
        run: closing(() => void confirmAction({
          // Removing someone asks first, as Notion's does (seen 2026-10-08).
          message: t("removeOrgMemberConfirm", { name: label }), detail: t("removeOrgMemberConfirmDetail"),
          confirmLabel: t("remove"), danger: true,
        }).then((confirmed) => {
          if (confirmed) void mutate({ action: organizationSettingsActions.memberRemove, targetId: member.id });
        })),
      });
    }
    return rows;
  };
  const openMember = members.find((member) => member.id === openId);

  return (
    <section className={styles.section} role="region" aria-label={t("organizationMembersTitle")} data-organization-members>
      <div className={styles.sectionTitle}>{t("organizationMembersTitle")}</div>
      <div className={groupStyles.tools}>
        <span />
        <span className={groupStyles.toolButtons}>
          <SettingsSearchToggle query={query} onQueryChange={setQuery} placeholder={t("searchPeoplePlaceholder")} />
        </span>
      </div>
      <div className={tableStyles.scroller} aria-busy={disabled}>
        <div role="table" className={local.table} aria-label={t("organizationMembersTitle")}>
          <div role="row" className={`${local.headRow} ${tableStyles.headRow}`}>
            <WorkspaceMemberSortHead sortKey="user" text={t("memberColumnUser")} sort={sort} onSort={setSort} />
            <WorkspaceMemberSortHead sortKey="role" text={t("memberColumnRole")} sort={sort} onSort={setSort} />
            <div role="columnheader" className={tableStyles.headCell} aria-label={t("domainActionsColumn")} />
          </div>
          {filtered.map((member) => {
            const label = organizationMemberLabel(member, userId, t);
            const deactivated = member.status === "deactivated";
            const note = [
              member.email || (member.userId ? personDistinction(member.userId) : ""),
              deactivated ? t("statusDeactivated") : "",
              ...member.adminRoles.map((role) => organizationAdminRoleLabel(role, t)),
            ].filter(Boolean).join(" · ");
            const hasMenu = rowsFor(member).length > 0;
            return (
              <div key={member.id} role="row" className={local.row} data-organization-member={member.userId} data-muted={deactivated ? "true" : undefined}>
                <div role="cell" className={tableStyles.userCell}>
                  <PersonAvatar className={tableStyles.avatar} userId={member.userId} name={label} size={28} />
                  <span className={styles.memberText}>
                    <strong>{label}</strong>
                    <span>{note}</span>
                  </span>
                </div>
                <div role="cell" className={tableStyles.cell}>
                  {member.canManageRole ? (
                    <AppSelect value={member.role} look="value" className={local.roleSelect} aria-label={t("ariaOrgRoleFor", { label })} disabled={disabled}
                      onChange={(event) => void mutate({
                        action: organizationSettingsActions.memberRole,
                        targetId: member.id,
                        value: event.target.value as OrganizationRole,
                      })}>
                      {editableOrganizationRoles.map((role) => (
                        <option key={role} value={role}>{organizationRoleLabel(role, t)}</option>
                      ))}
                    </AppSelect>
                  ) : <span className={tableStyles.role}>{organizationRoleLabel(member.role, t)}</span>}
                </div>
                <div role="cell" className={local.menuCell}>
                  {hasMenu ? (
                    <button type="button" className={groupStyles.iconButton} aria-haspopup="menu" aria-expanded={openId === member.id}
                      aria-label={t("organizationMemberMenu", { name: label })} data-organization-member-menu={member.userId}
                      onClick={(event) => { trigger.current = event.currentTarget; setOpenId(openId === member.id ? "" : member.id); }}>
                      <DotsHorizontal size="regular" aria-hidden="true" />
                    </button>
                  ) : null}
                </div>
              </div>
            );
          })}
        </div>
      </div>
      {!filtered.length ? (
        <div className={styles.emptyState}>{query.trim() ? t("noSearchResults") : t("noOrgMembers")}</div>
      ) : null}
      {openMember ? (
        <SidebarActionMenu button={trigger} rows={rowsFor(openMember)} backdropLabel={t("common:actions.close")} width={260}
          label={t("organizationMemberMenu", { name: organizationMemberLabel(openMember, userId, t) })}
          close={() => setOpenId("")} />
      ) : null}
    </section>
  );
}
