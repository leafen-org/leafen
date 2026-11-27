import { useState } from "react";
import { useTranslation } from "react-i18next";
import type { AppHook, AppHookSecret, AppTenancy } from "@/appPlatformWire.generated";
import { settingsErrorMessage } from "@/lib/settingsErrors";
import type { Session } from "@/types";
import { confirmAction } from "../ConfirmActionDialog";
import { makeAppHookSecret, removeAppHookOrg } from "./administratorAppsApi";
import styles from "./AdministratorApps.module.css";
import orgStyles from "./AppHookOrganizations.module.css";
import { displayDateTime } from "@/lib/displayDate";
import { AppSelect } from "@/components/AppSelect";

/**
 * A hook's addresses for single organizations (docs/app-platform.md 11):
 * each with its own secret — shown once when made — and what waits from
 * it; another organization's address is made from the list, and one
 * taken away refuses calls from then on.
 */
export function AppHookOrganizations({ session, installId, hook, tenancies, busy, onBusy, onChange }: {
  session: Session;
  installId: string;
  hook: AppHook;
  tenancies: AppTenancy[];
  busy: boolean;
  onBusy: (busy: boolean) => void;
  onChange: (hook: AppHook) => void;
}) {
  const { t } = useTranslation(["serverConsole", "common"]);
  const [made, setMade] = useState<AppHookSecret | null>(null);
  const [choice, setChoice] = useState("");
  const [error, setError] = useState("");
  const organizations = hook.organizations ?? [];
  const open = tenancies.filter((tenancy) => tenancy.key.startsWith("org:") && !organizations.some((item) => item.tenancyKey === tenancy.key));

  async function run(work: () => Promise<void>) {
    onBusy(true);
    setError("");
    try {
      await work();
    } catch (failure) {
      setError(settingsErrorMessage(failure, t("apps.changeFailed")));
    } finally {
      onBusy(false);
    }
  }
  const make = (tenancyKey: string, again: boolean) => run(async () => {
    if (again && !await confirmAction({ message: t("apps.hooks.orgs.remakeConfirm"), detail: t("apps.hooks.orgs.remakeConfirmDetail"), confirmLabel: t("apps.hooks.remake"), danger: true })) return;
    const next = await makeAppHookSecret(session, installId, hook.key, tenancyKey);
    setMade(next);
    setChoice("");
    onChange(next.hook);
  });
  const remove = (tenancyKey: string, name: string) => run(async () => {
    if (!await confirmAction({ message: t("apps.hooks.orgs.removeConfirm", { name }), detail: t("apps.hooks.orgs.removeConfirmDetail"), confirmLabel: t("apps.hooks.orgs.remove"), danger: true })) return;
    const listed = await removeAppHookOrg(session, installId, hook.key, tenancyKey.slice("org:".length));
    setMade(null);
    const next = listed.hooks.find((item) => item.key === hook.key);
    if (next) onChange(next);
  });

  if (!organizations.length && !open.length) return null;
  return (
    <div className={orgStyles.organizations} data-app-hook-orgs={hook.key}>
      <span className={orgStyles.title}>{t("apps.hooks.orgs.title")}</span>
      {organizations.map((organization) => (
        <div key={organization.tenancyKey} className={orgStyles.organization} data-app-hook-org={organization.tenancyKey}>
          <span className={styles.secretName}>
            <strong>{organization.name || organization.tenancyKey}</strong>
            <span>{[t("apps.hooks.set", { last4: organization.last4 }),
              ...(organization.waiting ? [t("apps.hooks.waiting", { count: organization.waiting })] : []),
              ...(organization.lastReceivedAt ? [t("apps.hooks.last", { date: displayDateTime(organization.lastReceivedAt) })] : []),
            ].join(" · ")}</span>
            {organization.needsNewSecret ? <span className={styles.unreadable}>{t("apps.hooks.needsNewSecret")}</span> : null}
            <code className={styles.callback}>{organization.url}</code>
            {organization.earlierAddress ? (
              <span className={styles.note} data-app-hook-earlier>{t("apps.hooks.earlier", { earlier: organization.earlierAddress })}</span>
            ) : null}
          </span>
          <span className={orgStyles.actions}>
            <button type="button" className={styles.secondary} disabled={busy} data-app-hook-org-remake
              onClick={() => void make(organization.tenancyKey, true)}>{t("apps.hooks.remake")}</button>
            <button type="button" className={styles.secondary} disabled={busy} data-app-hook-org-remove
              onClick={() => void remove(organization.tenancyKey, organization.name)}>{t("apps.hooks.orgs.remove")}</button>
          </span>
          {made?.tenancyKey === organization.tenancyKey ? (
            <div className={styles.hookOnce} data-app-hook-once>
              <span>{t("apps.hooks.once")}</span>
              <label>{t("apps.hooks.secret")}<input readOnly value={made.secret} onFocus={(event) => event.currentTarget.select()} /></label>
              <label>{t("apps.hooks.address")}<input readOnly value={made.url} onFocus={(event) => event.currentTarget.select()} /></label>
            </div>
          ) : null}
        </div>
      ))}
      {open.length ? (
        <div className={orgStyles.add}>
          <AppSelect value={choice} disabled={busy} aria-label={t("apps.hooks.orgs.choose")} data-app-hook-org-choose
            onChange={(event) => setChoice(event.target.value)}>
            <option value="">{t("apps.hooks.orgs.choose")}</option>
            {open.map((tenancy) => <option key={tenancy.key} value={tenancy.key}>{tenancy.name || tenancy.key}</option>)}
          </AppSelect>
          <button type="button" className={styles.secondary} disabled={busy || !choice} data-app-hook-org-make
            onClick={() => void make(choice, false)}>{t("apps.hooks.orgs.make")}</button>
        </div>
      ) : null}
      {error ? <div className={styles.error} role="alert">{error}</div> : null}
    </div>
  );
}
