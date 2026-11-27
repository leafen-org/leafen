import { useTranslation } from "react-i18next";
import styles from "../AuthGate.module.css";
import type { AdministratorSetupPlatform } from "@/administratorWire.generated";
import { useSeenSetupStatus } from "@/lib/useSeenSetupStatus";

/**
 * How the server was started, as its setup state says to a screen opened
 * from outside (internal/app/setup_hosting_platform.go, 2026-11-27): given
 * a setup code (LEAFEN_SETUP_CODE — a hosting template makes one), and the
 * hosting platform it knows it runs on. A server given a code shows the code
 * field first, with where the code is; one on a known platform names its log
 * there and gives no same-network advice. The setup screens read only these.
 */
export function useSetupStart(): { codeGiven: boolean; platform: AdministratorSetupPlatform | "" } {
  const status = useSeenSetupStatus();
  return { codeGiven: status?.codeGiven === true, platform: status?.platform ?? "" };
}

/**
 * One line under the setup code's field of a server given a code: where the
 * code is, in its platform's own words (Railway's screens are in English:
 * Variables), or where a platform keeps it in general. Leafen's own screen:
 * Notion has no server to set up.
 */
export function SetupCodePlace({ id }: { id?: string }) {
  const { t } = useTranslation("authGate");
  const { platform } = useSetupStart();
  return (
    <p id={id} className={styles.fieldHelp} data-setup-code-place={platform || "any"}>
      {t(platform === "railway" ? "setupCodePlaceRailway" : "setupCodePlaceAny")}
    </p>
  );
}
