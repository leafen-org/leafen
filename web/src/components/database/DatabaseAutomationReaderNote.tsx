import { useTranslation } from "react-i18next";
import { LightningIcon } from "@/icons/leafen";
import styles from "./DatabaseAutomation.module.css";

/** What a reader finds with none to list: "자동화" and why they can't make one, centred, as Notion's (seen 2026-11-05). */
export function DatabaseAutomationReaderNote() {
  const { t } = useTranslation("databaseView");
  return (
    <div className={styles.readerNote} data-automation-reader-note>
      <LightningIcon size="regular" aria-hidden="true" />
      <strong>{t("automationReaderTitle")}</strong>
      <p>{t("automationReaderDetail")}</p>
    </div>
  );
}
