import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { userErrorText } from "@/lib/settingsErrors";

import { request } from "@/apiTransport";
import { mcpConnectionListOperations, type OrganizationUserKeyPolicy } from "@/mcpConnectionListWire.generated";
import type { Session } from "@/types";
import { styles } from "./workspaceSettingsOverviewStyles";
import { AppSelect } from "@/components/AppSelect";
import { SettingsSwitchRow } from "./SettingsSwitchRow";

const SCOPES: OrganizationUserKeyPolicy["maxScope"][] = ["read", "content", "structure", "admin"];
const DAYS = [0, 7, 30, 90, 365];

function policyPath(path: string, organizationId: string) {
  return path.replace("{organizationId}", encodeURIComponent(organizationId));
}

/**
 * The organization's rule for its members' keys, above the keys: whether
 * they may make them, the widest scope and how soon they must expire. Only
 * those who manage the organization's security see it; the server tells,
 * by answering, who that is. As the settings' other rows, a change saves as
 * it is made, and the scope and expiry show once keys are allowed
 * (2026-11-06: a checkbox and a "저장" button here, unlike the rest).
 */
export function WorkspaceUserKeyPolicy({ session, organizationId }: { session: Session; organizationId: string }) {
  const { t } = useTranslation("workspaceSettingsDialog");
  const [policy, setPolicy] = useState<OrganizationUserKeyPolicy | null>(null);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => {
    if (!organizationId) return;
    let current = true;
    request<OrganizationUserKeyPolicy>(policyPath(mcpConnectionListOperations.readUserKeyPolicy.path, organizationId),
      { method: mcpConnectionListOperations.readUserKeyPolicy.method }, session)
      .then((read) => { if (current) setPolicy(read); })
      .catch(() => { if (current) setPolicy(null); });
    return () => { current = false; };
  }, [organizationId, session]);

  if (!policy) return null;
  const days = DAYS.includes(policy.maxDays) ? DAYS : [...DAYS, policy.maxDays].sort((a, b) => a - b);
  // Saved as chosen; one the server refuses goes back to what it was.
  async function change(next: OrganizationUserKeyPolicy) {
    if (!policy) return;
    const before = policy;
    setPolicy(next);
    setSaving(true);
    setError("");
    try {
      setPolicy(await request<OrganizationUserKeyPolicy>(
        policyPath(mcpConnectionListOperations.updateUserKeyPolicy.path, organizationId),
        { method: mcpConnectionListOperations.updateUserKeyPolicy.method, body: JSON.stringify(next) },
        session,
      ));
    } catch (caught) {
      setPolicy(before);
      setError(userErrorText(caught, t("keyPolicySaveFailed")));
    } finally {
      setSaving(false);
    }
  }
  return (
    <section className={styles.rowGroup} data-user-key-policy aria-busy={saving}>
      <div className={styles.sectionTitle}>{t("keyPolicyTitle")}</div>
      <SettingsSwitchRow title={t("keyPolicyAllowed")} description={t("keyPolicyDesc")} checked={policy.keysAllowed}
        disabled={saving} attributes={{ "data-key-policy-allowed": "" }}
        onChange={(keysAllowed) => void change({ ...policy, keysAllowed })} />
      {policy.keysAllowed ? <>
        <div className={styles.securityRow}>
          <div className={styles.securityInstruction}><strong>{t("keyPolicyMaxScope")}</strong></div>
          <AppSelect look="setting" aria-label={t("keyPolicyMaxScope")} value={policy.maxScope} disabled={saving} data-key-policy-scope
            onChange={(event) => void change({ ...policy, maxScope: event.target.value as OrganizationUserKeyPolicy["maxScope"] })}>
            {SCOPES.map((scope) => <option key={scope} value={scope}>{t(`keyScope.${scope}`)}</option>)}
          </AppSelect>
        </div>
        <div className={styles.securityRow}>
          <div className={styles.securityInstruction}><strong>{t("keyPolicyMaxDays")}</strong></div>
          <AppSelect look="setting" aria-label={t("keyPolicyMaxDays")} value={policy.maxDays} disabled={saving} data-key-policy-days
            onChange={(event) => void change({ ...policy, maxDays: Number(event.target.value) })}>
            {days.map((count) => <option key={count} value={count}>
              {count ? t("keyPolicyWithinDays", { count }) : t("keyPolicyNoExpiry")}
            </option>)}
          </AppSelect>
        </div>
      </> : null}
      {error ? <div className={styles.errorText} role="alert">{error}</div> : null}
    </section>
  );
}
