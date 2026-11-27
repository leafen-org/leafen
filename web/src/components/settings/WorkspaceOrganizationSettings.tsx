import { useId } from "react";
import { useTranslation } from "react-i18next";

import { organizationModule } from "@/lib/organizationModule";
import type { OrganizationSettingsMutation } from "@/lib/workspaceOrganizationSettings";
import type { OrganizationSettingsSnapshot } from "@/organizationWire.generated";
import { WorkspaceOrganizationAudit } from "./WorkspaceOrganizationAudit";
import { WorkspaceOrganizationGroups } from "./WorkspaceOrganizationGroups";
import { WorkspaceOrganizationMembers } from "./WorkspaceOrganizationMembers";
import { WorkspaceOrganizationRetention } from "./WorkspaceOrganizationRetention";
import { organizationAdminRoleLabel, organizationRoleLabel } from "./organizationSettingsModel";
import { styles } from "./workspaceSettingsOverviewStyles";
import { SettingsSectionTitle } from "@/components/settings/SettingsSectionTitle";

export function WorkspaceOrganizationSettings({
  snapshot,
  userId,
  mutating,
  mutate,
}: {
  snapshot: OrganizationSettingsSnapshot;
  userId: string;
  mutating: boolean;
  mutate: (mutation: OrganizationSettingsMutation) => void | Promise<void>;
}) {
  const { t } = useTranslation("workspaceSettingsDialog");
  const titleId = useId();
  const { organization } = snapshot;
  // Admin roles are the organization module's to give.
  const AdminRoles = organizationModule()?.AdminRoles;
  return (
    <>
      <section id="organization" className={styles.section} aria-labelledby={titleId} aria-busy={mutating}>
        <SettingsSectionTitle id={titleId} className={styles.sectionTitle} restates>{t("organizationAdminTitle")}</SettingsSectionTitle>
        <div className={styles.securityRow}>
          <div className={styles.securityInstruction}>
            <strong>{organization.name || t("organizationFallback")}</strong>
            <span>{[organizationRoleLabel(organization.role, t), ...organization.adminRoles.map((role) => organizationAdminRoleLabel(role, t))].join(" · ")}</span>
          </div>
        </div>
      </section>
      {/* Each admin role sees only the parts its role covers. */}
      {organization.capabilities.canManagePeople ? (
        <>
          <WorkspaceOrganizationMembers
            organizationId={organization.organizationId}
            members={snapshot.members}
            userId={userId}
            disabled={mutating}
            mutate={mutate}
          />
          <WorkspaceOrganizationGroups
            groups={snapshot.groups}
            members={snapshot.members}
            userId={userId}
            canManage={organization.capabilities.canManagePeople}
            disabled={mutating}
            mutate={mutate}
          />
        </>
      ) : null}
      {AdminRoles && organization.capabilities.canManageRoles ? (
        <AdminRoles
          members={snapshot.members}
          userId={userId}
          disabled={mutating}
          mutate={mutate}
        />
      ) : null}
      <WorkspaceOrganizationRetention organization={organization} disabled={mutating} mutate={mutate} />
      {organization.capabilities.canViewAuditLog ? <WorkspaceOrganizationAudit organizationId={organization.organizationId} members={snapshot.members} /> : null}
    </>
  );
}
