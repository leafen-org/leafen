import { useTranslation } from "react-i18next";
import guide from "./SetupOutsideGuide.module.css";
import { useSetupStart } from "./SetupCodePlace";

/**
 * Where a server install's log is read — a Synology NAS's Container Manager,
 * Docker Compose, any other Docker — for whoever looks in it for what the
 * server printed there: the first setup's link from outside
 * (SetupOutsideGuide, SetupCodeField) and an administrator's one-time
 * sign-in code (ServerLogCodeHint). A server that knows its hosting platform
 * names that platform's place alone, in its words; one given a setup code
 * at its start may be on a platform it doesn't know, so its list opens with
 * a platform's log in general (SetupCodePlace.tsx).
 */
export function ServerLogPlaces() {
  const { t } = useTranslation("authGate");
  const { codeGiven, platform } = useSetupStart();
  if (platform === "railway") {
    return <ul className={guide.places} data-server-log-places="railway"><li>{t("setupLogPlaceRailway")}</li></ul>;
  }
  return (
    <ul className={guide.places} data-server-log-places>
      {codeGiven ? <li>{t("setupLogPlaceAny")}</li> : null}
      <li>{t("setupOutsideSynology")}</li>
      <li>Docker Compose: <code className={guide.command}>docker compose logs leafen</code></li>
      <li>{t("setupOutsideOtherDocker")} <code className={guide.command}>{t("setupOutsideDockerCommand")}</code></li>
    </ul>
  );
}
