import { useState } from "react";
import { useTranslation } from "react-i18next";
import type { AppTenancy } from "@/appPlatformWire.generated";
import { settingsErrorMessage } from "@/lib/settingsErrors";
import type { Session } from "@/types";
import { setAppOversight } from "./administratorAppsApi";
import { SettingRowGroup, SettingSwitch } from "./AdministratorSettingRows";
import oversightStyles from "./AppOversightSettings.module.css";

/**
 * Overseeing an organization's apps (docs/app-platform.md 5): every app
 * action there asks a person first, and nobody may allow one for an hour on
 * the app's screen. One switch per organization; a server with one shows
 * just the switch.
 */
export function AppOversightSettings({ session, tenancies, disabled, onChange }: {
  session: Session;
  tenancies: AppTenancy[];
  disabled: boolean;
  onChange: (tenancies: AppTenancy[]) => void;
}) {
  const { t } = useTranslation("serverConsole");
  if (!tenancies.length) return null;
  return (
    <TenancySwitches tenancies={tenancies} disabled={disabled} onChange={onChange} mark="app-oversight"
      title={t("apps.oversight.title")} description={t("apps.oversight.body")} on={(tenancy) => tenancy.overseen}
      save={(tenancy, on) => setAppOversight(session, tenancy.key, on)} />
  );
}

/**
 * One policy's switch, or a row naming it with a switch per organization
 * under it. A press shows its new state at once; the server decides, and a
 * refusal puts the switch back with the reason.
 */
function TenancySwitches({ tenancies, disabled, onChange, mark, title, description, on, save }: {
  tenancies: AppTenancy[];
  disabled: boolean;
  onChange: (tenancies: AppTenancy[]) => void;
  mark: string;
  title: string;
  description: string;
  on: (tenancy: AppTenancy) => boolean;
  save: (tenancy: AppTenancy, on: boolean) => Promise<AppTenancy[]>;
}) {
  const { t } = useTranslation("serverConsole");
  const [pressed, setPressed] = useState<{ key: string; on: boolean } | null>(null);
  const [error, setError] = useState("");

  async function toggle(tenancy: AppTenancy, next: boolean) {
    setPressed({ key: tenancy.key, on: next });
    setError("");
    try {
      onChange(await save(tenancy, next));
    } catch (failure) {
      setError(settingsErrorMessage(failure, t("apps.changeFailed")));
    } finally {
      setPressed(null);
    }
  }

  const shown = (tenancy: AppTenancy) => (pressed?.key === tenancy.key ? pressed.on : on(tenancy));
  const name = (tenancy: AppTenancy) => tenancy.name || t("apps.serverTenancy");
  const busy = disabled || Boolean(pressed);
  return (
    <>
      {tenancies.length === 1 ? (
        <SettingSwitch title={title} description={description} on={shown(tenancies[0])} disabled={busy}
          onChange={(next) => void toggle(tenancies[0], next)} {...{ [`data-${mark}`]: tenancies[0].key }} />
      ) : (
        <SettingRowGroup title={title} description={description} {...{ [`data-${mark}-settings`]: "" }}>
          {tenancies.map((tenancy) => (
            <SettingSwitch key={tenancy.key} title={name(tenancy)} on={shown(tenancy)} disabled={busy}
              onChange={(next) => void toggle(tenancy, next)} {...{ [`data-${mark}`]: tenancy.key }} />
          ))}
        </SettingRowGroup>
      )}
      {error ? <span className={oversightStyles.error} role="alert">{error}</span> : null}
    </>
  );
}
