import { useTranslation } from "react-i18next";
import type { AdministratorUnenforcedPolicy } from "../../administratorWire.generated";
import styles from "./BackupFailureNotice.module.css";

const POLICY_KEYS = new Set([
  "legalHolds", "dlp", "sharingBlocks", "suspendedPeople", "guestTwoStep", "passwordTwoStep",
  "singleSignOn", "managedAccounts", "mcpClients", "workspaceCreation",
]);

/**
 * The band at the top of the app for a server's administrators while the
 * server runs without the organization module and its data keeps
 * organization policies the server doesn't enforce (2026-10-13: they went
 * off without a word). Nobody else sees it.
 */
export function UnenforcedPoliciesNotice({ policies }: { policies: readonly AdministratorUnenforcedPolicy[] }) {
  const { t } = useTranslation("serverConsole");
  const named = policies
    .filter((policy) => POLICY_KEYS.has(policy.key))
    .map((policy) => t(`unenforcedPolicies.policy.${policy.key}`, { count: policy.count }));
  if (named.length === 0) return null;
  const text = t("unenforcedPolicies.notice", { policies: named.join(", ") });
  return (
    <div className={styles.notice} role="status" data-unenforced-policies-notice>
      <span className={styles.text} title={text}>{text}</span>
    </div>
  );
}
