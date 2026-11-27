import { useTranslation } from "react-i18next";
import type { AppInstall } from "@/appPlatformWire.generated";
import styles from "./AdministratorApps.module.css";

/**
 * What an app's row says of its data that couldn't be made
 * (docs/app-platform.md 6): an organization's first making or catch-up with
 * an update, and how many people's own data, each with why — shown until a
 * later opening or reach of the app makes it.
 */
export function AppDataFailureNotes({ install }: { install: AppInstall }) {
  const { t } = useTranslation("serverConsole");
  return (
    <>
      {install.dataFailure ? (
        <span className={styles.trashNote} data-app-data-failure={install.dataUpdateFailed ? "update" : "setup"}>{t(install.dataUpdateFailed ? "apps.dataUpdateFailure" : "apps.dataFailure", { reason: install.dataFailure })}</span>
      ) : null}
      {install.personDataFailures ? (
        <span className={styles.trashNote} data-app-data-failure="person">{t("apps.personDataFailure", { count: install.personDataFailures, reason: install.personDataFailure })}</span>
      ) : null}
    </>
  );
}
