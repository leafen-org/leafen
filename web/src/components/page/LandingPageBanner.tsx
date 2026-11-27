import { useTranslation } from "react-i18next";
import { requestSettingsWindow } from "@/components/settings/settingsWindowRequest";
import styles from "./PublishedSiteBanner.module.css";

/**
 * The bar over the workspace's custom landing page, as Notion's (seen
 * 2026-11-09): a line saying new members land on this page, and "페이지
 * 변경하기", which opens 설정 › 일반 with that row lit. It shows to
 * those who can change the page — the workspace's owners — as only they reach
 * that row; it has no close button.
 */
export function LandingPageBanner() {
  const { t } = useTranslation(["topBar", "workspaceSettingsDialog"]);
  return (
    <div className={styles.banner} role="status" data-landing-page-banner>
      <span className={styles.message}>{t("landingBanner.text")}</span>
      <button type="button" className={styles.button} data-landing-page-banner-change
        onClick={() => requestSettingsWindow({ section: "general", lit: [t("workspaceSettingsDialog:landingPageField")] })}>
        {t("landingBanner.change")}
      </button>
    </div>
  );
}
