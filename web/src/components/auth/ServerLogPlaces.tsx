import { useTranslation } from "react-i18next";
import guide from "./SetupOutsideGuide.module.css";

/**
 * Where a server install's log is read — a Synology NAS's Container Manager,
 * Docker Compose, any other Docker — for whoever looks in it for what the
 * server printed there: the first setup's link from outside
 * (SetupOutsideGuide) and an administrator's one-time sign-in code
 * (ServerLogCodeHint).
 */
export function ServerLogPlaces() {
  const { t } = useTranslation("authGate");
  return (
    <ul className={guide.places} data-server-log-places>
      <li>{t("setupOutsideSynology")}</li>
      <li>Docker Compose: <code className={guide.command}>docker compose logs leafen</code></li>
      <li>{t("setupOutsideOtherDocker")} <code className={guide.command}>{t("setupOutsideDockerCommand")}</code></li>
    </ul>
  );
}
