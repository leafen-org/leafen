import { useTranslation } from "react-i18next";
import styles from "./TunnelFailureDetails.module.css";

/**
 * A way in from outside that hasn't opened (a one-time link, Tailscale) is
 * said in the screen's own words by its screen, with what the person can do;
 * the tunnel's own words — cloudflared's or Tailscale's, in English
 * ("cloudflared stopped: exit status 1") — are never that line: they stay
 * folded here under "자세히", for whoever looks into it (2026-11-25: the
 * first run's restore showed them as they came, in every language).
 */
export function TunnelFailureDetails({ failure }: { failure: string }) {
  const { t } = useTranslation("serverConsole");
  return (
    <details className={styles.details} data-tunnel-failure-details>
      <summary>{t("remoteChoice.details")}</summary>
      <code lang="en" dir="ltr">{failure}</code>
    </details>
  );
}
