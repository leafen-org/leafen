import type { ReactNode } from "react";
import { useTranslation } from "react-i18next";
import type { AdministratorMapSettingsInput } from "../../administratorWire.generated";
import styles from "./AdministratorSettingsPage.module.css";
import { SettingField, SettingSwitch } from "./AdministratorSettingRows";

/**
 * Where map views get the map under their pins. External tiles come straight
 * from the tile server to each browser, OpenStreetMap's public tiles unless
 * another address is set; off, maps draw only the bundled outline.
 */
export function AdministratorMapSettings({
  value,
  environmentDisabled,
  busy,
  onChange,
  searchSettings,
}: {
  value: AdministratorMapSettingsInput;
  environmentDisabled: boolean;
  busy: boolean;
  onChange: (change: Partial<AdministratorMapSettingsInput>) => void;
  searchSettings?: ReactNode;
}) {
  const { t } = useTranslation("serverConsole");
  const on = value.enabled && !environmentDisabled;
  // 설정's rows; the tile server's fields only once tiles are on (2026-11-05: a checkbox over fields always shown).
  return (
    <>
      <section className={styles.section} aria-labelledby="administrator-map-title">
        <div id="administrator-map-title" className={styles.sectionTitle}>{t("map.title")}</div>
        <div className={styles.settingRows}>
          <SettingSwitch title={t("map.useTiles")} on={on} disabled={busy || environmentDisabled}
            description={environmentDisabled ? t("map.environmentOff") : t("map.privacy")}
            onChange={(enabled) => onChange({ enabled })} />
          {on ? (
            <>
              <SettingField title={t("map.tileUrl")} description={t("map.otherServers")}>
                <input type="url" value={value.tileUrl} placeholder={t("map.tileUrlPlaceholder")}
                  onChange={(event) => onChange({ tileUrl: event.target.value })} disabled={busy} />
              </SettingField>
              {value.tileUrl.trim() ? (
                <>
                  <SettingField title={t("map.attribution")}>
                    <input value={value.attribution} placeholder={t("map.attributionPlaceholder")} maxLength={200}
                      onChange={(event) => onChange({ attribution: event.target.value })} disabled={busy} />
                  </SettingField>
                  <SettingField title={t("map.attributionUrl")}>
                    <input type="url" value={value.attributionUrl} placeholder="https://"
                      onChange={(event) => onChange({ attributionUrl: event.target.value })} disabled={busy} />
                  </SettingField>
                </>
              ) : null}
            </>
          ) : null}
        </div>
      </section>
      {searchSettings}
    </>
  );
}
