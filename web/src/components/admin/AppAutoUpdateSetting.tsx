import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { desktopUpdater, desktopUpdateSwitchShown, type DesktopUpdaterState } from "../../lib/desktopUpdater";
import { SettingSwitch } from "./AdministratorSettingRows";

/**
 * The desktop app's switch for updating itself (2026-10-17), beside the
 * server's own update settings; a company's deployment decides it by the
 * environment and the switch only shows that. Only in the desktop app, and
 * not in the Microsoft Store's, which the store updates, nor in the Windows
 * zip, which only says a newer version is out.
 */
export function AppAutoUpdateSetting() {
  const { t } = useTranslation("serverConsole");
  const [state, setState] = useState<DesktopUpdaterState | null>(null);
  const shown = desktopUpdateSwitchShown();

  useEffect(() => {
    if (!shown) return;
    void desktopUpdater.state().then(setState, () => setState(null));
  }, [shown]);

  if (!shown || !state || state.store || state.manual) return null;
  return (
    <SettingSwitch title={t("updates.appAutoUpdate")} on={state.enabled} disabled={state.managed} data-app-auto-update=""
      description={t(state.managed ? "updates.appAutoUpdateManaged" : "updates.appAutoUpdateHelp")}
      onChange={(enabled) => {
        setState({ ...state, enabled });
        void desktopUpdater.setEnabled(enabled).catch(() => setState({ ...state }));
      }} />
  );
}
