import { useTranslation } from "react-i18next";
import { useSeenSetupStatus } from "@/lib/useSeenSetupStatus";

/**
 * The first run's word that scheduled backups are off (2026-11-16), under
 * the setup screen's lead: the server answers before anyone administers it,
 * and once it has an administrator says nothing of it. Where they are turned
 * on — Settings → Server → Backups — is named here; the first administrator
 * has no settings until they start. It comes from the setup state the
 * sign-in page read before it opened the screen, so it draws with the screen
 * (2026-11-18: read again after it, on a NAS's first visit it came 150 ms
 * late and pushed the name field and "계속" down 48.6 px).
 */
export function SetupBackupsOff() {
  const { t } = useTranslation("authGate");
  return useSeenSetupStatus()?.backupsOff === true ? <p data-setup-backups-off>{t("setupBackupsOff")}</p> : null;
}
