import { useCallback, useState } from "react";
import { useTranslation } from "react-i18next";
import { AccountEmailManagement } from "@/components/account/AccountEmailManagement";
import { AccountPasskeysSection } from "@/components/account/AccountPasskeysSection";
import { AccountPasswordSection } from "@/components/account/AccountPasswordSection";
import { styles as account } from "@/components/account/accountSettingsStyles";
import { useAccountSecurity } from "@/components/account/useAccountSecurity";
import type { Session } from "@/types";
import styles from "../AuthGate.module.css";
import offer from "../auth/SecondDeviceOffer.module.css";
import { FirstRunLayer } from "./FirstRunLayer";
import css from "./FirstSetup.module.css";
import { pageMarkSource } from "@/lib/pageMark";

/**
 * A backup sign-in set in the first run's own screen (2026-10-27: a first
 * setup's screens never leave for Settings): Settings › 계정's parts — the
 * email, a password, passkeys — on the screen's panel, back to the step it
 * came from when done, saying whether a backup sign-in is now set. Its
 * notices are Settings › 계정's: an error said in an open dialog isn't said
 * again behind it (2026-10-28).
 */
export function BackupSignInScreen({ session, onSession, onDone }: {
  session: Session;
  onSession(session: Session): void;
  /** Done, with whether the account now has a password or a passkey. */
  onDone(set: boolean): void;
}) {
  const { t } = useTranslation(["workspaceSettingsDialog", "authGate", "common"]);
  const security = useAccountSecurity(session, onSession);
  const [email, setEmail] = useState(false);
  const [actionOpen, setActionOpen] = useState(false);
  const [passkeys, setPasskeys] = useState(0);
  const countKeys = useCallback((count: number) => setPasskeys(count), []);
  return (
    <FirstRunLayer view={email ? "email" : "parts"} data={{ "data-first-setup-step": "backup" }}>
      <main className={styles.screen} data-backup-sign-in-screen>
        <section className={`${styles.panel} ${offer.panel}`}>
          <div className={styles.brand}>
            <img className={styles.mark} src={pageMarkSource()} alt="" aria-hidden="true" />
            <div><h1>Leafen</h1></div>
          </div>
          <div className={styles.form}>
            <div className={styles.notice}>
              <strong>{t("backupSignInTitle")}</strong>
              <p>{t("backupSignInDesc")}</p>
            </div>
            {email ? (
              <div className={css.backupParts}>
                <AccountEmailManagement session={session} onSession={onSession} />
                <button type="button" className={styles.secondary} onClick={() => setEmail(false)}>{t("common:actions.back")}</button>
              </div>
            ) : (
              <div className={css.backupParts}>
                <div className={account.securityRow}>
                  <div className={account.securityInstruction}><strong>{t("accountEmail")}</strong><span>{session.user.email || t("accountNoEmail")}</span></div>
                  <button type="button" className={account.secondaryButton} onClick={() => setEmail(true)} data-backup-email>{t("emailManageButton")}</button>
                </div>
                <AccountPasswordSection security={security} email={session.user.email} onOpenChange={setActionOpen} />
                <AccountPasskeysSection session={session} onKeys={countKeys} />
                {security.error && !actionOpen ? <div className={account.notice} role="alert">{security.error}</div> : null}
                {security.notice ? <div className={account.notice} data-tone="neutral" role="status">{security.notice}</div> : null}
              </div>
            )}
            <button type="button" className={styles.primary} onClick={() => onDone(Boolean(security.snapshot?.passwordSet) || passkeys > 0)} data-backup-done>{t("authGate:secondDeviceDone")}</button>
          </div>
        </section>
      </main>
    </FirstRunLayer>
  );
}
