import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { api } from "@/api";
import { X } from "@/icons/leafen";
import { addressReach, outsideAddress } from "@/lib/addressReach";
import { isPackagedClient } from "@/lib/packagedClient";
import { fetchRuntimeConfig } from "@/lib/runtimeConfigService";
import type { Session } from "@/types";
import styles from "../admin/BackupFailureNotice.module.css";
import { useOpenInAppBand } from "./OpenInAppBand";

const dismissedKey = "leafen:proper-address-offer-dismissed";

/** Whether this browser gets an answer at the address: any answer, as a
 * request that doesn't read it fails only on the name, the certificate or
 * the connection. */
async function properAddressAnswers(address: string): Promise<boolean> {
  try {
    await fetch(new URL("/api/leafen/v1/health", address), {
      mode: "no-cors", cache: "no-store", credentials: "omit", signal: AbortSignal.timeout(5000),
    });
    return true;
  } catch {
    return false;
  }
}

function dismissedFor(address: string): boolean {
  try {
    return window.localStorage.getItem(dismissedKey) === address;
  } catch {
    return false;
  }
}

/**
 * A browser opened at this computer's or the home network's address of a
 * server that now has a proper one (2026-10-20, "로컬호스트에서 넘어갈
 * 경우는?"): the local address keeps working — the way in at home when the
 * internet is down — and a band offers, once, to go on at the proper
 * address. Going on makes a one-time device code here and opens it there,
 * at the same page, so that browser is signed in as this one is. Nothing
 * shows in the apps, whose own server is local by design.
 */
export function useProperAddressOffer(session: Session) {
  const { t } = useTranslation("app");
  // A phone's browser offers the app last.
  const openInApp = useOpenInAppBand(session);
  const [address, setAddress] = useState("");
  const [busy, setBusy] = useState(false);
  const [failed, setFailed] = useState(false);

  // The app has no address bar to move; a browser on a stand-in address is offered the proper one.
  useEffect(() => {
    if (isPackagedClient() || !addressReach(window.location.origin)) return undefined;
    let live = true;
    void fetchRuntimeConfig().then(async ({ config }) => {
      const proper = config.publicAddress ?? "";
      if (!live || !outsideAddress(proper) || proper === window.location.origin || dismissedFor(proper)) return;
      // Offered only once this browser reaches it: a name that doesn't
      // resolve yet, or a certificate it doesn't trust, took everyone who
      // pressed the band to an error page (2026-11-06, the address saved
      // while its DNS record was still missing).
      if (!await properAddressAnswers(proper) || !live) return;
      setAddress(proper);
    }, () => undefined);
    return () => { live = false; };
  }, []);

  if (!address) return openInApp;
  async function goOn() {
    setBusy(true);
    setFailed(false);
    try {
      const code = await api.createDeviceCode(session);
      const here = `${window.location.pathname}${window.location.search}`;
      window.location.assign(`${address}/device/${encodeURIComponent(code.code)}?next=${encodeURIComponent(here)}`);
    } catch {
      setFailed(true);
      setBusy(false);
    }
  }
  function dismiss() {
    try {
      window.localStorage.setItem(dismissedKey, address);
    } catch {
      // Shown again next time.
    }
    setAddress("");
  }
  const text = failed ? t("properAddress.failed") : t("properAddress.offer", { address });
  return (
    <div className={styles.suggestion} role="status" data-proper-address-offer>
      <span className={styles.text} title={text}>{text}</span>
      <button type="button" className={styles.suggestionAction} disabled={busy} onClick={() => void goOn()}>
        {t("properAddress.goOn")}
      </button>
      <button type="button" className={styles.dismiss} onClick={dismiss} aria-label={t("properAddress.dismiss")} title={t("properAddress.dismiss")}>
        <X size="regular" aria-hidden="true" />
      </button>
    </div>
  );
}
