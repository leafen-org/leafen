import { useTranslation } from "react-i18next";
import { setActiveProfileId, LOCAL_PROFILE_ID } from "@/lib/profileRegistry";
import { acceptServerKey } from "@/lib/serverKeyPin";
import styles from "./ProductLoadingScreen.module.css";

/**
 * The packaged app's stop when a server it remembers at its Tailscale
 * address answers with another identity — it says what that may mean
 * and offers going back to this device's Leafen, or taking the new identity
 * when the administrator reinstalled the server — or with a certificate its
 * identity didn't sign (pin ""), which only goes back.
 */
export function ServerKeyChangedScreen({ origin, pin }: { origin: string; pin: string }) {
  const { t } = useTranslation("app");
  const host = new URL(origin).hostname;
  return (
    <main className={styles.screen} role="alert" data-server-key-changed>
      <div className={styles.content}>
        <h1 className={styles.alertTitle}>{t("serverKeyChanged.title")}</h1>
        <p className={styles.alertText}>{t(pin ? "serverKeyChanged.body" : "serverKeyChanged.unvouched", { host })}</p>
        <div className={styles.alertActions}>
          <button type="button" className={styles.alertPrimary} onClick={() => {
            setActiveProfileId(LOCAL_PROFILE_ID);
            window.location.reload();
          }}>{t("serverKeyChanged.back")}</button>
          {pin ? (
            <button type="button" className={styles.alertSecondary} onClick={() => {
              acceptServerKey(origin, pin);
              window.location.reload();
            }}>{t("serverKeyChanged.accept")}</button>
          ) : null}
        </div>
      </div>
    </main>
  );
}
