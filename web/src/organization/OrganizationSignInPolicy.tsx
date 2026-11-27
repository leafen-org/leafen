import { SwitchMark } from "@/components/Switch";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { request } from "@/apiTransport";
import { settingsErrorMessage, writeUncertainMark } from "@/lib/settingsErrors";
import type { Session } from "@/types";
import { styles } from "@/components/settings/workspaceSettingsOverviewStyles";

type SignInPolicy = { passwordTwoStep: boolean };

const signInPolicyPath = (organizationId: string) => `/organizations/${encodeURIComponent(organizationId)}/sign-in-policy`;

/**
 * The organization's sign-in rule (the multi-server decision of 2026-10-20:
 * an organization's policy comes before device registration): its owner may
 * ask members to sign in with a password and 2-step verification instead of
 * joining devices with a code. Single sign-on only is the SAML provider's
 * own enforcement, above.
 */
export function OrganizationSignInPolicy({ session, organizationId }: { session: Session; organizationId: string }) {
  const { t } = useTranslation("workspaceSettingsDialog");
  const [policy, setPolicy] = useState<SignInPolicy | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => {
    const controller = new AbortController();
    request<SignInPolicy>(signInPolicyPath(organizationId), { signal: controller.signal }, session).then(setPolicy, (failure) => {
      if (!controller.signal.aborted) setError(settingsErrorMessage(failure, t("signInPolicyFailed")));
    });
    return () => controller.abort();
  }, [organizationId, session, t]);

  async function change(passwordTwoStep: boolean) {
    setBusy(true);
    setError("");
    try {
      setPolicy(await request<SignInPolicy>(signInPolicyPath(organizationId), {
        method: "PUT", body: JSON.stringify({ passwordTwoStep }),
      }, session));
    } catch (failure) {
      setError(settingsErrorMessage(failure, t("signInPolicyFailed")));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className={styles.organizationPolicyBlock} data-sign-in-policy>
      <div className={styles.organizationSubhead}><span>{t("signInPolicyTitle")}</span></div>
      <p className={styles.organizationMeta}>{t("signInPolicyDescription")}</p>
      {policy ? (
        <div className={styles.sharingPolicyGrid} aria-busy={busy}>
          <button
            type="button"
            className={styles.sharingPolicyToggle}
            aria-pressed={policy.passwordTwoStep}
            disabled={busy}
            onClick={() => void change(!policy.passwordTwoStep)}
          >
            <span>{t("signInPolicyPasswordTwoStep")}</span>
            <SwitchMark on={policy.passwordTwoStep} />
          </button>
        </div>
      ) : null}
      {/* Told under the switches, not above them: a box there moved what was pressed (2026-11-06). */}
      {error ? <div className={styles.notice} role="alert" {...writeUncertainMark(error)}>{error}</div> : null}
    </div>
  );
}
