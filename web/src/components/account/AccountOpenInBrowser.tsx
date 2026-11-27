import { useState } from "react";
import { useTranslation } from "react-i18next";
import { api } from "@/api";
import { LeafenApiError } from "@/apiTransport";
import { isPackagedClient } from "@/lib/packagedClient";
import { productErrorMessage } from "@/lib/productErrorPresentation";
import { activeProfileId, LOCAL_PROFILE_ID } from "@/lib/profileRegistry";
import type { Session } from "@/types";
import { styles } from "./accountSettingsStyles";

/** Whether this is the desktop app's window on the app's own server. */
export function desktopOwnServer(): boolean {
  return isPackagedClient() && activeProfileId() === LOCAL_PROFILE_ID && !/Android|iPhone|iPad/u.test(navigator.userAgent);
}

/**
 * "브라우저에서 열기" (the multi-server decision, 2026-10-20): the desktop app's
 * own server opens in this computer's browser, on the page of a one-time
 * device code that signs that browser in as a device of this account
 * (desktop/local_browser.go).
 */
export function AccountOpenInBrowser({ session }: { session: Session }) {
  const { t } = useTranslation("workspaceSettingsDialog");
  const [state, setState] = useState<"idle" | "busy" | "opened" | "failed">("idle");
  const [failure, setFailure] = useState("");
  if (!desktopOwnServer()) return null;

  async function open() {
    setState("busy");
    let made: { id: string; code: string } | null = null;
    try {
      made = await api.createDeviceCode(session);
      const { Call } = await import("@wailsio/runtime");
      await Call.ByName("main.LocalBrowser.OpenDevicePage", made.code);
      setState("opened");
    } catch (caught) {
      if (made) void api.cancelDeviceCode(session, made.id).catch(() => undefined);
      setFailure(caught instanceof LeafenApiError ? productErrorMessage(caught, "accountSecurity.read") : t("openInBrowserFailed"));
      setState("failed");
    }
  }

  return (
    <div className={styles.securityRow} data-account-open-in-browser={state}>
      <div className={styles.securityInstruction}>
        <strong>{t("openInBrowser")}</strong>
        <span>{t("openInBrowserDesc")}</span>
        {state === "opened" ? <span role="status">{t("openInBrowserOpened")}</span> : null}
        {state === "failed" ? <span className={styles.errorText} role="alert">{failure}</span> : null}
      </div>
      <button type="button" className={styles.secondaryButton} disabled={state === "busy"} onClick={() => void open()}>
        {t("openInBrowser")}
      </button>
    </div>
  );
}
