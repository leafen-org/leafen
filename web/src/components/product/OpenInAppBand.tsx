import { useState } from "react";
import { useTranslation } from "react-i18next";
import { api } from "@/api";
import { X } from "@/icons/leafen";
import { iosSafari, openInAppLink, phoneBrowser } from "@/lib/openInApp";
import type { Session } from "@/types";
import { PhoneSheet, PhoneSheetGroup, PhoneSheetRow } from "@/components/shell/PhoneSheet";
import styles from "../admin/BackupFailureNotice.module.css";
import band from "./OpenInAppBand.module.css";

const dismissedKey = "leafen:open-in-app-dismissed";

function dismissed(): boolean {
  try {
    return window.localStorage.getItem(dismissedKey) === "1";
  } catch {
    return false;
  }
}

/**
 * A phone's browser offers the Leafen app (the multi-server decision's
 * 2026-10-20 "같은 날 다듬음"): a thin band opening this page in the app,
 * which signs in through this browser where it isn't signed in. An iPhone's
 * Safari adds "add to Home Screen", whose sheet says a home-screen Leafen
 * gets notifications and how to add it, with a one-time code to sign in
 * there, as its storage is its own. Closed, it doesn't come back in this
 * browser; a home-screen Leafen and the app never show it.
 */
export function useOpenInAppBand(session: Session) {
  const { t } = useTranslation("app");
  const [shown, setShown] = useState(() => phoneBrowser() && !dismissed());
  const [code, setCode] = useState("");
  const [sheet, setSheet] = useState(false);
  if (!shown) return null;
  function dismiss() {
    try {
      window.localStorage.setItem(dismissedKey, "1");
    } catch {
      // Shown again next time.
    }
    setShown(false);
  }
  const homeScreen = iosSafari();
  return (
    <div className={styles.suggestion} role="status" data-open-in-app-band>
      {homeScreen ? null : <span className={styles.text}>{t("openInApp.lead")}</span>}
      <a className={styles.suggestionAction} href={openInAppLink(window.location.href, true)} data-open-in-app>{t("openInApp.open")}</a>
      {homeScreen ? (
        <button type="button" className={styles.suggestionAction} onClick={() => setSheet(true)} data-open-in-app-home>
          {t("openInApp.addToHome")}
        </button>
      ) : null}
      <button type="button" className={`${styles.dismiss} ${homeScreen ? band.end : ""}`} onClick={dismiss}
        aria-label={t("openInApp.dismiss")} title={t("openInApp.dismiss")}>
        <X size="regular" aria-hidden="true" />
      </button>
      {sheet ? (
        <PhoneSheet title={t("openInApp.addToHome")} onClose={() => setSheet(false)} data={{ "data-open-in-app-sheet": "" }}>
          <p className={band.steps}>{t("openInApp.homeScreen")}</p>
          {code ? <p className={band.code} data-open-in-app-code-shown>{code}</p> : null}
          {code ? <p className={band.steps}>{t("openInApp.homeScreenCode", { code })}</p> : (
            <PhoneSheetGroup>
              <PhoneSheetRow label={t("openInApp.showCode")} more={false} data={{ "data-open-in-app-code": "" }}
                onPress={() => void api.createDeviceCode(session).then((made) => setCode(made.code), () => undefined)} />
            </PhoneSheetGroup>
          )}
        </PhoneSheet>
      ) : null}
    </div>
  );
}
