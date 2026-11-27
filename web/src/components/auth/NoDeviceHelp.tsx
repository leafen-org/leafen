import { useTranslation } from "react-i18next";
import styles from "../AuthGate.module.css";
import guide from "./SetupOutsideGuide.module.css";

/**
 * The code step's "로그인된 기기가 없나요?", folded under its field: a person
 * asks an administrator for a one-time code; only the one running the server
 * has a command to run, folded once more, said in a line and the command on
 * its own (user 2026-11-23: no command up front, one thing a line).
 */
export function NoDeviceHelp() {
  const { t } = useTranslation("authGate");
  return (
    <details className={styles.moreHelp} data-auth-no-device>
      <summary>{t("deviceCodeNoDevice")}</summary>
      <p>{t("deviceCodeAskAdministrator")}</p>
      <details className={styles.moreHelp} data-auth-server-administrator>
        <summary>{t("deviceCodeServerAdministrator")}</summary>
        <p>{t("deviceCodeNoDeviceHint")}</p>
        <p><code className={guide.command}>{t("deviceCodeNoDeviceCommand")}</code></p>
      </details>
    </details>
  );
}
