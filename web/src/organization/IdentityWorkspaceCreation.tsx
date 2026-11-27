import { SwitchMark } from "@/components/Switch";
import { useTranslation } from "react-i18next";
import type { OrganizationSettingsMutation } from "@/lib/workspaceOrganizationSettings";
import { organizationSettingsActions, type OrganizationSettingsSummary } from "@/organizationWire.generated";
import { styles } from "@/components/settings/workspaceSettingsOverviewStyles";

/**
 * Identity → 도메인 관리's "워크스페이스 생성", as Notion's (its help, read
 * 2026-10-02; the row seen behind its upgrade): whether the people with
 * emails on the organization's verified domains may create new workspaces —
 * "Allow users to create new workspaces" — or only its owners and admins,
 * the default. The server refuses the others' new workspaces.
 */
export function IdentityWorkspaceCreation({ organization, disabled, mutate }: {
  organization: OrganizationSettingsSummary;
  disabled: boolean;
  mutate: (mutation: OrganizationSettingsMutation) => void | Promise<void>;
}) {
  const { t } = useTranslation("workspaceSettingsDialog");
  const allowed = organization.workspaceCreationPolicy === "members";
  return (
    <div className={styles.securityRow} data-workspace-creation>
      <div className={styles.securityInstruction}>
        <strong>{t("workspaceCreationTitle")}</strong>
        <span>{t(allowed ? "workspaceCreationMembersDescription" : "workspaceCreationOwnersDescription")}</span>
      </div>
      <button
        type="button"
        role="switch"
        aria-checked={allowed}
        aria-label={t("workspaceCreationAllow")}
        disabled={disabled || !organization.capabilities.canManageIdentity}
        data-workspace-creation-switch
        onClick={() => void mutate({
          action: organizationSettingsActions.workspaceCreationPolicy,
          value: allowed ? "owners_admins" : "members",
        })}
      >
        <SwitchMark on={allowed} />
      </button>
    </div>
  );
}
