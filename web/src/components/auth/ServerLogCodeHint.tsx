import { Trans, useTranslation } from "react-i18next";
import styles from "../AuthGate.module.css";
import guide from "./SetupOutsideGuide.module.css";
import hint from "./ServerLogCodeHint.module.css";
import { ServerLogPlaces } from "./ServerLogPlaces";

/**
 * A server install's way back for its administrator with no device signed
 * in (user decision 2026-11-19 "넣어"), under "코드로 로그인"'s line while the
 * server's log shows a one-time code — which it does only for a sign-in page
 * opened on its own network (administrator_sign_in_code.go). One plain line
 * says who it is for and where the code comes from; where the log is, the
 * command that shows the code too and the ten minutes stay folded under
 * "방법 보기" (user 2026-11-23 "응 전반적으로 직관적으로 되게 적어놔").
 */
export function ServerLogCodeHint() {
  const { t } = useTranslation("authGate");
  return (
    <div className={hint.hint} data-auth-server-log-code>
      <p className={styles.fieldHelp}>{t("serverLogCodeHint")}</p>
      <details className={`${styles.moreHelp} ${hint.how}`} data-auth-server-log-code-how>
        <summary>{t("serverLogCodeHow")}</summary>
        <p>{t("serverLogCodeFind")}</p>
        <ServerLogPlaces />
        <p><Trans i18nKey="authGate:serverLogCodeStatus" components={{ command: <code className={guide.command} /> }} /></p>
        <p>{t("serverLogCodeEnter")}</p>
      </details>
    </div>
  );
}
