import { useTranslation } from "react-i18next";
import { addressReach } from "@/lib/addressReach";
import { useServerAdministrator } from "@/lib/serverAdministrator";
import { pushRoute } from "@/lib/router";
import { linkOrigin } from "@/lib/serverOrigin";
import type { Session } from "@/types";
import styles from "./LocalAddressHint.module.css";

/**
 * Where a link or QR for another device is made while the server has only a
 * local address (2026-10-20): one line on what can open it, and to the
 * server's administrator, who alone can, the way to the setting that lets
 * other devices; once the server has an outside address, links are made with
 * it and nothing shows.
 */
export function LocalAddressHint({ session, address, className, onLeave }: {
  session: Session;
  address?: string;
  className?: string;
  /** The place the hint is in closes as the setting opens. */
  onLeave?: () => void;
}) {
  const { t } = useTranslation("app");
  const reach = addressReach(address || linkOrigin());
  const administrator = useServerAdministrator(session, reach !== null);
  if (!reach) return null;
  return (
    <p className={className} data-local-address-hint={reach}>
      {t(reach === "loopback" ? "localReach.loopbackOnly" : "localReach.homeOnly")}
      {administrator ? <>{" "}<button type="button" className={styles.turnOn} data-local-address-turn-on onClick={() => {
        onLeave?.();
        // The server section's address, as the server's notices and the apps' notes name it:
        // /settings knows only the workspace's and the account's sections (2026-11-13: it opened 일반).
        pushRoute(`/admin/settings?section=tunnel&from=${encodeURIComponent(`${window.location.pathname}${window.location.search}`)}`);
      }}>{t("localReach.turnOn")}</button></> : null}
    </p>
  );
}
