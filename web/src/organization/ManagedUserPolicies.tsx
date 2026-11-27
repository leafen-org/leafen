import { SwitchMark } from "@/components/Switch";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import {
  changeManagedUserPolicies,
  readManagedUserPolicies,
  SESSION_DURATION_HOURS,
  type ManagedUserPolicies as Policies,
} from "@/lib/organizationManagedUserPolicies";
import { settingsErrorMessage } from "@/lib/settingsErrors";
import type { Session } from "@/types";
import { styles as settingsStyles } from "@/components/settings/workspaceSettingsOverviewStyles";
import { AppSelect } from "@/components/AppSelect";

/**
 * Identity → 사용자 관리's policies, as Notion's rows (seen 2026-10-02 behind
 * its upgrade; its help read the same day): "관리 대상 사용자의 프로필 편집
 * 허용", "외부 워크스페이스 액세스" and "세션 기간" (an hour to 180 days).
 * Notion's support access row is its own staff's.
 */
export function ManagedUserPolicies({ session, organizationId }: { session: Session; organizationId: string }) {
  const { t } = useTranslation("workspaceSettingsDialog");
  const [policies, setPolicies] = useState<Policies | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => {
    const controller = new AbortController();
    readManagedUserPolicies(session, organizationId, controller.signal)
      .then(setPolicies)
      .catch((failure) => {
        if (!controller.signal.aborted) setError(settingsErrorMessage(failure, t("managedUserPoliciesFailed")));
      });
    return () => controller.abort();
  }, [organizationId, session, t]);

  async function change(next: Partial<Policies>) {
    setBusy(true);
    setError("");
    try {
      setPolicies(await changeManagedUserPolicies(session, organizationId, next));
    } catch (failure) {
      setError(settingsErrorMessage(failure, t("managedUserPoliciesFailed")));
    } finally {
      setBusy(false);
    }
  }

  if (!policies) return error ? <div className={settingsStyles.errorText} role="alert">{error}</div> : null;
  const hours = SESSION_DURATION_HOURS.includes(policies.sessionDurationHours as (typeof SESSION_DURATION_HOURS)[number])
    ? [...SESSION_DURATION_HOURS]
    : [...SESSION_DURATION_HOURS, policies.sessionDurationHours].sort((a, b) => a - b);
  const switchRow = (key: "profileChanges" | "externalWorkspaces", title: string, description: string) => (
    <div className={settingsStyles.securityRow} data-managed-user-policy={key}>
      <div className={settingsStyles.securityInstruction}>
        <strong>{t(title)}</strong>
        <span>{t(description)}</span>
      </div>
      <button type="button" role="switch" aria-checked={policies[key]} aria-label={t(title)} disabled={busy}
        onClick={() => void change({ [key]: !policies[key] })}>
        <SwitchMark on={policies[key]} />
      </button>
    </div>
  );
  return (
    <>
      {switchRow("profileChanges", "managedUserProfileChanges", "managedUserProfileChangesDescription")}
      {switchRow("externalWorkspaces", "managedUserExternalWorkspaces", "managedUserExternalWorkspacesDescription")}
      <div className={settingsStyles.securityRow} data-managed-user-policy="secondaryEmails">
        <div className={settingsStyles.securityInstruction}><strong>{t("emailSecondaryPolicy")}</strong></div>
        <AppSelect value={policies.secondaryEmails} aria-label={t("emailSecondaryPolicy")} disabled={busy}
          onChange={(event) => void change({ secondaryEmails: event.target.value as Policies["secondaryEmails"] })}>
          <option value="disabled">{t("emailPolicyDisabled")}</option>
          <option value="verified_domains">{t("emailPolicyVerifiedDomains")}</option>
          <option value="any">{t("emailPolicyAny")}</option>
        </AppSelect>
      </div>
      <div className={settingsStyles.securityRow} data-managed-user-policy="sessionDurationHours">
        <div className={settingsStyles.securityInstruction}>
          <strong>{t("managedUserSessionDuration")}</strong>
          <span>{t("managedUserSessionDurationDescription")}</span>
        </div>
        <AppSelect value={policies.sessionDurationHours} aria-label={t("managedUserSessionDuration")} disabled={busy}
          onChange={(event) => void change({ sessionDurationHours: Number(event.target.value) })}>
          {hours.map((value) => (
            <option key={value} value={value}>
              {value % 24 === 0 ? t("managedUserSessionDays", { count: value / 24 }) : t("managedUserSessionHours", { count: value })}
            </option>
          ))}
        </AppSelect>
      </div>
      {error ? <div className={settingsStyles.errorText} role="alert">{error}</div> : null}
    </>
  );
}
