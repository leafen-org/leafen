import { useState } from "react";
import { useTranslation } from "react-i18next";
import type { AppTenancy } from "@/appPlatformWire.generated";
import { settingsErrorMessage } from "@/lib/settingsErrors";
import type { Session } from "@/types";
import { setAppStrict } from "./administratorAppsApi";
import { SettingRowGroup, SettingSwitch } from "./AdministratorSettingRows";
import oversightStyles from "./AppOversightSettings.module.css";

/**
 * Strict mode for single organizations (docs/app-platform.md 7), under the
 * server's switch while that is off: each organization's apps then reach
 * outside only through the relay.
 */
export function AppStrictTenancies({ session, tenancies, disabled, onChange }: {
  session: Session;
  tenancies: AppTenancy[];
  disabled: boolean;
  onChange: (tenancies: AppTenancy[]) => void;
}) {
  const { t } = useTranslation("serverConsole");
  const [saving, setSaving] = useState("");
  const [error, setError] = useState("");
  // With one organization the server's own switch says it all.
  if (tenancies.length < 2) return null;

  async function toggle(tenancy: AppTenancy, strict: boolean) {
    setSaving(tenancy.key);
    setError("");
    try {
      onChange(await setAppStrict(session, tenancy.key, strict));
    } catch (failure) {
      setError(settingsErrorMessage(failure, t("apps.changeFailed")));
    } finally {
      setSaving("");
    }
  }

  return (
    <>
      <SettingRowGroup description={t("apps.strict.byOrganization")} data-app-strict-tenancies>
        {tenancies.map((tenancy) => (
          <SettingSwitch key={tenancy.key} title={tenancy.name || t("apps.serverTenancy")} on={tenancy.strict} disabled={disabled || Boolean(saving)}
            onChange={(on) => void toggle(tenancy, on)} data-app-strict-tenancy={tenancy.key} />
        ))}
      </SettingRowGroup>
      {error ? <span className={oversightStyles.error} role="alert">{error}</span> : null}
    </>
  );
}
