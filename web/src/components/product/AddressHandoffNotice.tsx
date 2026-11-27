import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { X } from "@/icons/leafen";
import { addressHandoffFollowUps, clearAddressHandoffFollowUps } from "@/lib/addressHandoff";
import { turnDesktopPushOn } from "@/lib/desktopPush";
import { pushRoute } from "@/lib/router";
import type { Session } from "@/types";
import styles from "../admin/BackupFailureNotice.module.css";

/**
 * After a sign-in carried over from the server's earlier address
 * (lib/addressHandoff.ts), a band says so once and offers what that address
 * held: notifications on again here, and a passkey for this address, as
 * passkeys are an address's. It says so for that sign-in alone: once it
 * ends — signed out, another account, or a sign-in made again — it is gone.
 */
export function useAddressHandoffNotice(session: Session) {
  const { t } = useTranslation("app");
  const userId = session.user.id, sessionId = session.sessionId;
  const [dismissed, setDismissed] = useState(false);
  const followUps = useMemo(() => addressHandoffFollowUps(userId, sessionId), [sessionId, userId]);
  const [notificationsOn, setNotificationsOn] = useState(false);
  if (!followUps || dismissed) return null;
  function dismiss() {
    clearAddressHandoffFollowUps();
    setDismissed(true);
  }
  const host = window.location.host;
  return (
    <div className={styles.suggestion} role="status" data-address-handoff>
      <span className={styles.text}>{t("addressHandoff.carried", { host })}</span>
      {followUps.notifications && !notificationsOn ? (
        <button type="button" className={styles.suggestionAction} data-address-handoff-notifications
          onClick={() => void turnDesktopPushOn(session).then((state) => setNotificationsOn(state === "on"))}>
          {t("addressHandoff.notifications")}
        </button>
      ) : null}
      {followUps.passkey ? (
        <button type="button" className={styles.suggestionAction} data-address-handoff-passkey
          onClick={() => { dismiss(); pushRoute("/settings?section=account"); }}>
          {t("addressHandoff.passkey")}
        </button>
      ) : null}
      <button type="button" className={styles.dismiss} onClick={dismiss} aria-label={t("addressHandoff.dismiss")} title={t("addressHandoff.dismiss")}>
        <X size="regular" aria-hidden="true" />
      </button>
    </div>
  );
}
