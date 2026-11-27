import { useTranslation } from "react-i18next";
import type { AdministratorSettings } from "@/administratorWire.generated";
import styles from "./BackupFailureNotice.module.css";

/**
 * A security release the server's daily check found, told to its
 * administrators in the band over the app (user decision 2026-11-05): only a
 * security one; an ordinary new version stays in 설정 › 서버 › 업데이트. As the
 * backup band, it comes in with a page, not under a pointer about to press
 * (useSteadyBand).
 */
export function useSecurityUpdateNotice(settings: AdministratorSettings | null, openUpdates: () => void) {
  const { t } = useTranslation("serverConsole");
  const shown = settings?.updates.newer && settings.updates.security ? settings.updates.latestVersion || "" : null;
  if (shown === null) return null;
  const text = t("updates.securityBand", { version: shown });
  return (
    <div className={styles.suggestion} role="status" data-security-update-notice>
      <span className={styles.text} title={text}>{text}</span>
      <button type="button" className={styles.suggestionAction} onClick={openUpdates}>{t("updates.securityBandOpen")}</button>
    </div>
  );
}
