import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import type { NetworkProxyInForce, NetworkProxyInput } from "@/lib/desktopNetworkProxy";
import { AppSelect } from "@/components/AppSelect";
import { styles as settingsStyles } from "@/components/settings/workspaceSettingsOverviewStyles";
import css from "./NetworkProxyForm.module.css";
import { SettingField, SettingRow, SettingSwitch } from "@/components/admin/AdministratorSettingRows";
import { useSettingsUnsaved } from "./settingsUnsaved";

export type NetworkProxySetting = {
  mode: "system" | "manual";
  url: string;
  noProxy: string;
  username: string;
  passwordSet: boolean;
};

/**
 * A network proxy setting (internal/netproxy): the system's settings and the
 * environment by default, with nothing to set where they name the proxy, or
 * an address, the hosts reached directly and a sign-in set here. It says
 * which proxy is in force and where it comes from; a password is typed and
 * never shown again. `onEdit` hears each change by hand, after which what
 * was said about the last save no longer holds.
 */
export function NetworkProxyForm({ setting, inForce, busy, onSave, onEdit }: {
  setting: NetworkProxySetting;
  inForce: NetworkProxyInForce;
  busy: boolean;
  onSave: (input: NetworkProxyInput) => Promise<boolean>;
  onEdit?: () => void;
}) {
  const { t } = useTranslation("networkProxy");
  const [mode, setMode] = useState(setting.mode);
  const [url, setURL] = useState(setting.url);
  const [noProxy, setNoProxy] = useState(setting.noProxy);
  const [username, setUsername] = useState(setting.username);
  const [password, setPassword] = useState("");
  const [clearPassword, setClearPassword] = useState(false);
  useEffect(() => {
    setMode(setting.mode);
    setURL(setting.url);
    setNoProxy(setting.noProxy);
    setUsername(setting.username);
    setPassword("");
    setClearPassword(false);
  }, [setting]);

  const by = <T,>(set: (value: T) => void) => (value: T) => {
    onEdit?.();
    set(value);
  };

  const address = inForce.https || inForce.http || "";
  const status = inForce.source === "leafen" ? t("inForce.leafen", { address })
    : inForce.source === "environment" ? t("inForce.environment", { address })
      : inForce.source === "system" && inForce.autoConfig ? t("inForce.autoconfig", { address: inForce.autoConfig })
        : inForce.source === "system" && inForce.autoDiscover ? t("inForce.autodiscover")
          : inForce.source === "system" ? t("inForce.system", { address }) : t("inForce.none");
  const changed = mode !== setting.mode || (mode === "manual" && (url !== setting.url || noProxy !== setting.noProxy
    || username !== setting.username || password !== "" || clearPassword));
  useSettingsUnsaved(changed);
  const valid = mode === "system" || /^https?:\/\/[^/\s:]+(:\d+)$/u.test(url.trim().replace(/\/$/u, ""));
  // 설정's rows: the choice with what is in force under it, the address fields once it is set here,
  // and a save only when something changed (2026-11-05: a grid of boxed fields and a grey save, always).
  return (
    <form className={css.form} data-network-proxy onSubmit={(event) => {
      event.preventDefault();
      void onSave({ mode, url: url.trim(), noProxy: noProxy.trim(), username: username.trim(),
        ...(password ? { password } : {}), ...(clearPassword ? { clearPassword } : {}) });
    }}>
      <SettingRow title={t("mode")} description={<>
        <span data-network-proxy-in-force={inForce.source || "none"}>{status}</span>
        {!inForce.source && inForce.unfollowed ? <> <span data-network-proxy-unfollowed>{t(`unfollowed.${inForce.unfollowed}`)}</span></> : null}
      </>}>
        <AppSelect look="setting" value={mode} disabled={busy} aria-label={t("mode")} data-network-proxy-mode
          onChange={(event) => by(setMode)(event.target.value === "manual" ? "manual" : "system")}>
          <option value="system">{t("system")}</option>
          <option value="manual">{t("manual")}</option>
        </AppSelect>
      </SettingRow>
      {mode === "manual" ? (
        <>
          <SettingField title={t("address")} description={t("help")} error={url.trim() && !valid ? t("invalid") : ""}>
            <input type="url" value={url} disabled={busy} placeholder="http://proxy.example.com:8080" autoComplete="off" spellCheck={false}
              onChange={(event) => by(setURL)(event.target.value)} data-network-proxy-url />
          </SettingField>
          <SettingField title={t("noProxy")}>
            <input value={noProxy} disabled={busy} placeholder=".corp.example, 10.0.0.0/8" autoComplete="off" spellCheck={false}
              onChange={(event) => by(setNoProxy)(event.target.value)} data-network-proxy-no-proxy />
          </SettingField>
          <SettingField title={t("username")}>
            <input value={username} disabled={busy} autoComplete="off" spellCheck={false} onChange={(event) => by(setUsername)(event.target.value)}
              data-network-proxy-username />
          </SettingField>
          {username.trim() ? (
            <SettingField title={t("password")}>
              <input type="password" value={password} disabled={busy || clearPassword} autoComplete="off"
                placeholder={setting.passwordSet && !clearPassword ? t("unchanged") : ""} onChange={(event) => by(setPassword)(event.target.value)}
                data-network-proxy-password />
            </SettingField>
          ) : null}
          {username.trim() && setting.passwordSet ? (
            <SettingSwitch title={t("clearPassword")} on={clearPassword} disabled={busy} onChange={(on) => by(setClearPassword)(on)} />
          ) : null}
        </>
      ) : null}
      {changed ? (
        <div className={css.actions}>
          <button type="submit" className={settingsStyles.primaryButton} disabled={busy || !valid} data-network-proxy-save>
            {t("save")}
          </button>
        </div>
      ) : null}
    </form>
  );
}
