import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";

import { organizationSettingsActions, type OrganizationAdminRole } from "@/organizationWire.generated";
import type { OrganizationAdminRolesProps } from "@/lib/organizationModule";
import {
  organizationAdminRoleDescription,
  organizationAdminRoleLabel,
  organizationAdminRoleOrder,
  organizationMemberLabel,
} from "@/components/settings/organizationSettingsModel";
import rows from "@/components/settings/SettingsRows.module.css";
import { styles } from "@/components/settings/workspaceSettingsOverviewStyles";
import { WorkspaceGroupAddMembers } from "@/components/settings/WorkspaceGroupAddMembers";
import local from "./OrganizationAdminRoles.module.css";

/**
 * Notion's "관리자 역할 관리" (its guide "Enterprise workspace management",
 * read 2026-10-04): each admin role with what it covers and who holds it;
 * the organization's owner adds members to a role and takes them off. A
 * member may hold several; a guest or the owner holds none. "멤버 추가"
 * opens the organization group window's picker (2026-11-13: a bare list of
 * names, with no search or email): the organization's people by name or
 * email, each with picture, name and email, several at once.
 */
export function OrganizationAdminRoles({ members, userId, disabled, mutate }: OrganizationAdminRolesProps) {
  const { t } = useTranslation("workspaceSettingsDialog");
  const [adding, setAdding] = useState<OrganizationAdminRole | null>(null);
  // The people picked go in one after another: each change is made against
  // the settings as the one before left them (sent at once, the second was
  // refused for its old revision).
  const [queued, setQueued] = useState<{ role: OrganizationAdminRole; memberId: string }[]>([]);
  useEffect(() => {
    if (!queued.length || disabled) return;
    const [next, ...rest] = queued;
    setQueued(rest);
    void mutate({ action: organizationSettingsActions.adminRoleAdd, targetId: next.memberId, value: next.role });
  }, [disabled, mutate, queued]);
  const candidatesFor = (role: OrganizationAdminRole) =>
    members.filter((member) => member.canManageAdminRoles && !member.adminRoles.includes(role));
  return (
    <section className={`${styles.section} ${local.roles}`} role="region" aria-label={t("adminRolesTitle")} data-organization-admin-roles>
      <div className={styles.sectionTitle}>{t("adminRolesTitle")}</div>
      <p className={rows.lead}>{t("adminRolesSubtitle")}</p>
      <table className={`${rows.table} ${local.table}`}>
        <thead>
          <tr><th>{t("adminRoleColumn")}</th><th className={rows.holdersColumn}>{t("adminRoleHoldersColumn")}</th></tr>
        </thead>
        <tbody>
          {organizationAdminRoleOrder.map((role) => {
            const label = organizationAdminRoleLabel(role, t);
            const holders = members.filter((member) => member.adminRoles.includes(role));
            const candidates = candidatesFor(role);
            return (
              <tr key={role} data-admin-role={role}>
                <td>
                  <strong>{label}</strong>
                  <span className={rows.cellNote}>{organizationAdminRoleDescription(role, t)}</span>
                </td>
                <td>
                  <div className={rows.wrapCell}>
                  {holders.map((member) => {
                    const name = organizationMemberLabel(member, userId, t);
                    return (
                      <span key={member.id} className={rows.holder}>
                        {name}
                        {member.canManageAdminRoles ? (
                          <button type="button" aria-label={t("ariaAdminRoleRemove", { name, role: label })} disabled={disabled}
                            onClick={() => void mutate({ action: organizationSettingsActions.adminRoleRemove, targetId: member.id, value: role })}>
                            ×
                          </button>
                        ) : null}
                      </span>
                    );
                  })}
                  {candidates.length > 0 ? (
                    <button type="button" className={rows.addHolder} aria-label={t("ariaAdminRoleAdd", { role: label })}
                      aria-haspopup="dialog" disabled={disabled || queued.length > 0} onClick={() => setAdding(role)}>
                      {t("adminRoleAddMember")}
                    </button>
                  ) : holders.length === 0 ? <span className={rows.none}>{t("adminRoleNoHolders")}</span> : null}
                  </div>
                </td>
              </tr>
            );
          })}
        </tbody>
      </table>
      {adding ? (
        <WorkspaceGroupAddMembers groupName={organizationAdminRoleLabel(adding, t)} candidates={candidatesFor(adding)} busy={disabled}
          lead={t("adminRoleAddLead")} onClose={() => setAdding(null)}
          onAdd={(userIds) => {
            const candidates = candidatesFor(adding);
            setQueued(userIds.flatMap((picked) => candidates.filter((member) => member.userId === picked)
              .map((member) => ({ role: adding, memberId: member.id }))));
            setAdding(null);
          }} />
      ) : null}
    </section>
  );
}
