import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { api } from "@/api";
import { LocalAddressHint } from "@/components/product/LocalAddressHint";
import { addressReach } from "@/lib/addressReach";
import { productErrorMessage } from "@/lib/productErrorPresentation";
import { readAndFollow } from "@/lib/followAcrossTabs";
import { linkOrigin } from "@/lib/serverOrigin";
import { useDeviceCode } from "@/lib/useDeviceCode";
import { useQRCode } from "@/lib/useQRCode";
import { useViewEntryFocus } from "@/lib/viewEntryFocus";
import type { Session } from "@/types";
import styles from "../AuthGate.module.css";
import css from "./SecondDeviceOffer.module.css";
import { pageMarkSource } from "@/lib/pageMark";

/**
 * The phone offered once a server first set up on an address only this
 * computer opens has an outside one (PhoneOfferLater): a phone scanning the
 * QR, or any device typing the code, or a backup sign-in set right there;
 * "Later" lets it go. The first run itself asks for no other device
 * (2026-10-29, as Notion's sign-up). Opened from this computer's own address
 * it shows no QR (2026-10-26: it opened nothing), only the backup sign-in.
 *
 * It says what scanning does (user decision 2026-11-22: the QR and the code
 * were one sentence, and nothing said what came of scanning): Leafen opens on
 * the phone, signed in, in a line of its own; typing the code, for a device
 * without a camera, in a small one; and the address the QR opens under it, to
 * check on the phone or type where a camera shows no link.
 */
export function SecondDeviceOffer({ session, onDone, onBackup }: {
  session: Session;
  /** Done with the offer, saying whether another device joined. */
  onDone(joined: boolean): void;
  /**
   * Sets a backup sign-in in the same screen (BackupSignInScreen): every
   * place that shows the offer has it (2026-10-26: a member joined by
   * invitation was told to set one with no way to).
   */
  onBackup: () => void;
}) {
  const { t } = useTranslation("authGate");
  const reach = addressReach(linkOrigin());
  const phoneless = reach === "loopback";
  const [joined, setJoined] = useState(false);
  const { code, failed } = useDeviceCode(session, !joined && !phoneless);
  const picture = useQRCode(code?.url ?? "", 176);
  const host = qrHost(code?.url);
  const seen = useRef<Set<string> | null>(null);

  // The devices signed in now are read at once; then one of the person's
  // tabs in sight asks for all of them, and a tab behind asks nothing
  // (followAcrossTabs; 2026-11-13: every tab of the first run asked every 3 s).
  useEffect(() => {
    if (joined || phoneless) return undefined;
    return readAndFollow(`auth-sessions:${session.user.id}`, 3_000, true,
      () => api.listAuthSessions(session).then((sessions) => sessions.map((item) => ({ id: item.id, registered: item.registered }))),
      (value) => {
        if (!Array.isArray(value)) return;
        const sessions = value as Array<{ id: string; registered?: boolean }>;
        if (!seen.current) {
          seen.current = new Set(sessions.map((item) => item.id));
          return;
        }
        if (sessions.some((item) => item.registered && !seen.current?.has(item.id))) setJoined(true);
      });
  }, [joined, phoneless, session]);

  // The offer takes the focus as it replaces the step before it, and again
  // when another device joins (2026-10-28: a joined member's offer, drawn
  // without a first-run layer, left it on the page).
  const panel = useRef<HTMLElement>(null);
  useViewEntryFocus(panel, `${phoneless}|${joined}`);
  return (
    <main className={styles.screen} data-second-device-offer={joined ? "joined" : phoneless ? "local" : "phone"}>
      <section ref={panel} className={`${styles.panel} ${css.panel}`}>
        <div className={styles.brand}>
          <img className={styles.mark} src={pageMarkSource()} alt="" aria-hidden="true" />
          <div><h1>Leafen</h1></div>
        </div>
        {/* Spaced as the steps before it, its button the panel's width (2026-10-25: all touched, "나중에" its words' width). */}
        <div className={styles.form}>
          <div className={styles.notice}>
            <strong>{t(joined ? "secondDeviceJoinedTitle" : "phoneDeviceTitle")}</strong>
            <p>{t(joined ? "secondDeviceJoinedBody" : "phoneDeviceBody")}</p>
          </div>
          {joined || phoneless ? null : failed ? (
            <p className={styles.error} role="alert">{productErrorMessage(failed, "accountSecurity.read")}</p>
          ) : (
            <div className={css.join}>
              <div className={css.scan}>
                <div className={css.qr}>{picture ? <img src={picture} alt={t("secondDeviceQRAlt")} width={176} height={176} /> : null}</div>
                {host ? <p className={css.host} data-second-device-host>{host}</p> : null}
              </div>
              <div className={css.steps}>
                <p className={css.lead}>{t("secondDeviceScan")}</p>
                <p>{t("secondDeviceScanApp")}</p>
                <p>{t("secondDeviceTypeCode")}</p>
                <code className={css.code} data-second-device-code>{code?.code ?? "····-····-····"}</code>
                {reach === "home" ? <LocalAddressHint session={session} /> : null}
                <p>{t("secondDeviceBackup")}</p>
              </div>
            </div>
          )}
          {/* A backup sign-in is set right here (BackupSignInScreen), never by a trip to Settings. */}
          {joined ? null : (
            <button className={phoneless ? styles.primary : styles.secondary} type="button" onClick={onBackup} data-second-device-backup>
              {t("secondDeviceSetBackup")}
            </button>
          )}
          <button className={phoneless ? styles.secondary : styles.primary} type="button" onClick={() => onDone(joined)} data-second-device-done>
            {t(joined ? "secondDeviceDone" : "secondDeviceLater")}
          </button>
        </div>
      </section>
    </main>
  );
}

/** The host of the address the QR opens, "" while there is none. */
function qrHost(url: string | undefined): string {
  if (!url) return "";
  try {
    return new URL(url).host;
  } catch {
    return "";
  }
}
