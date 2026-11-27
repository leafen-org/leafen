import { useEffect, useState } from "react";
import { Trans, useTranslation } from "react-i18next";
import { DEFAULT_LEGAL_LINKS, THIRD_PARTY_NOTICES_PATH } from "../../lib/legalLinks";
import { fetchRuntimeConfig } from "../../lib/runtimeConfigService";
import type {
  AdministratorUpdateSettings as UpdateStatus,
  AdministratorUpdateSettingsInput,
} from "../../administratorWire.generated";
import styles from "./AdministratorSettingsPage.module.css";
import own from "./AdministratorUpdateSettings.module.css";
import { SettingRow, SettingSwitch } from "./AdministratorSettingRows";
import { AppAutoUpdateSetting } from "./AppAutoUpdateSetting";
import { BuiltWithThanks } from "./BuiltWithThanks";
import { displayDateTime } from "@/lib/displayDate";

/**
 * The newer-release notice only administrators see, from the signed release
 * manifest the server checks once a day, with the daily check's switch (the
 * sponsor credit's switch was taken out with the credit, user decision
 * 2026-11-25; LEAFEN_SPONSOR_CREDIT=off stays the server's). The version line
 * links the open source notices and the source (2026-10-18: once a line on
 * the sign-in screen), over a line thanking the tools that help build Leafen.
 */
export function AdministratorUpdateSettings({
  status,
  value,
  busy,
  onChange,
}: {
  status: UpdateStatus;
  value: AdministratorUpdateSettingsInput;
  busy: boolean;
  onChange: (change: Partial<AdministratorUpdateSettingsInput>) => void;
}) {
  const { t } = useTranslation("serverConsole");
  const checking = value.enabled && !status.environmentDisabled;
  const checkedAt = status.checkedAt ? displayDateTime(status.checkedAt) : "";
  const [sourceUrl, setSourceUrl] = useState(DEFAULT_LEGAL_LINKS.sourceUrl);
  useEffect(() => {
    let mounted = true;
    void fetchRuntimeConfig().then(({ config }) => {
      if (mounted) setSourceUrl(config.legal.sourceUrl);
    });
    return () => {
      mounted = false;
    };
  }, []);
  // One group in 설정's rows (2026-11-05: boxed blocks with an "on/off" word over a checkbox each).
  return (
    <section className={styles.section} aria-labelledby="administrator-updates-title">
      <div id="administrator-updates-title" className={styles.sectionTitle}>{t("updates.title")}</div>
      <div className={styles.settingRows}>
        <SettingRow title={t("updates.currentTitle")} data-version-links=""
          description={<>
            <span className={own.links}>
              <a href={THIRD_PARTY_NOTICES_PATH} target="_blank" rel="noopener noreferrer">{t("updates.openSourceNotices")}</a>
              {" · "}
              <a href={sourceUrl} target="_blank" rel="noopener noreferrer">{t("updates.sourceCode")}</a>
            </span>
            <BuiltWithThanks />
          </>}>
          <code className={own.version}>{status.currentVersion}</code>
        </SettingRow>
        {status.newer ? (
          <SettingRow title={t("updates.available", { version: status.latestVersion })} data-update-notice="newer"
            description={<>
              <span className={own.links}>
                <a href={status.notesUrl} target="_blank" rel="noopener noreferrer">{t("updates.notes")}</a>
                {" · "}
                <a href={status.upgradeUrl} target="_blank" rel="noopener noreferrer">{t("updates.howTo")}</a>
              </span>
              {/* In Docker, how to update it there: the image pulled again, the container made again (docs/install.md "Updating"). */}
              {status.upgradeRuns === "container"
                ? <span className={own.dockerWay} data-update-docker-way=""><Trans t={t} i18nKey="updates.dockerWay" components={{ code: <code /> }} /></span>
                : null}
            </>}>
            {status.security ? <strong className={own.security} data-update-security>{t("updates.security")}</strong> : <span />}
          </SettingRow>
        ) : (
          // "The latest" only once a check said so (2026-10-16: shown before any check).
          <div data-update-notice="current" data-update-checked={status.checkFailed ? "failed" : status.checkedAt ? "yes" : "no"}>
            <SettingRow title={status.checkFailed && checking ? t("updates.checkFailed")
              : status.checkedAt ? t("updates.latest") : checking ? t("updates.notChecked") : t("updates.checkOff")}
              description={checkedAt ? t("updates.lastChecked", { time: checkedAt }) : undefined}><span /></SettingRow>
          </div>
        )}
        <SettingSwitch title={t("updates.dailyCheck")} on={checking} disabled={busy || status.environmentDisabled}
          description={status.environmentDisabled ? t("updates.checkEnvironmentOff") : t("updates.checkHelp")}
          onChange={(enabled) => onChange({ enabled })} />
        <AppAutoUpdateSetting />
      </div>
    </section>
  );
}
