import { type ButtonHTMLAttributes, type ReactNode, useEffect, useId, useState } from "react";
import { useTranslation } from "react-i18next";
import { api } from "@/api";
import { loadSession } from "@/lib/authSessionStore";
import styles from "../AuthGate.module.css";
import type { AuthGateScreenProps } from "./AuthGateScreen";
import { ServerLogCodeHint } from "./ServerLogCodeHint";

/**
 * The sign-in a server opens with (user decisions 2026-11-18 and 2026-11-23
 * "꺼내", changing the 2026-11-16 order that put "에이전트로 로그인" first
 * and folded a signed-in device's code away): "코드로 로그인" first, the code
 * step holding its field and, where a camera can scan, "QR 찍기"; one line
 * under it, always shown, says where a signed-in device gives the code
 * (2026-11-23 "빼고 올려"); right under it the gray
 * "AI로 로그인" with its one line, for whom an agent is connected to Leafen;
 * then email and passkey, and the server's sign-in providers and company
 * sign-in beside them. No way is folded away. On the app's own server, its
 * owner check comes first. Coming back from a way ("뒤로"), the control that
 * opened it takes the focus again (lib/viewEntryFocus.ts).
 */

// Whether a server install's log shows its administrator a one-time code,
// asked once a page load (administrator_sign_in_code.go): never for a page
// opened from outside the server's network.
let serverLogCode: Promise<boolean> | null = null;

/**
 * The page landed signed out, not for an account added beside one or an
 * auth link's step: the app's own window signs the person who set its
 * server up back in (useDeviceOwnerSignIn), and a server install says where
 * its administrator finds a one-time code when its log shows one (user
 * decision 2026-11-19 "넣어": a first administrator made with a name alone
 * is never kept out).
 */
function useWayBackIn(props: AuthGateScreenProps): boolean {
  const landed = !props.inWindow && !props.signedIn && !loadSession();
  const local = props.deviceOwner.local;
  const { signInBack } = props.deviceOwner;
  const [logCode, setLogCode] = useState(false);
  useEffect(() => {
    if (!landed) return undefined;
    if (local) {
      signInBack();
      return undefined;
    }
    let current = true;
    serverLogCode ??= api.askAdministratorSignInCode().then((answer) => answer.shown === true, () => false);
    void serverLogCode.then((shown) => { if (current) setLogCode(shown); });
    return () => { current = false; };
    // eslint-disable-next-line react-hooks/exhaustive-deps -- once, as the ways show
  }, []);
  return logCode;
}

/**
 * A way in, drawn as a gray button that keeps its own name. The one line
 * under it — what the way is for, or, dimmed, why it can't be used here — is
 * shown to everyone and is the button's description (2026-11-23: the
 * passkey's reason was a hover title only, out of reach on a touch screen and
 * read out in place of "패스키로 로그인"). Every gray way is drawn here.
 */
function Way({ line, lineData, children, ...button }: ButtonHTMLAttributes<HTMLButtonElement> & {
  /** The line under it: what it is for, or why it can't be used here. */
  line?: string;
  /** The line's own data attribute, which the screen's checks find it by. */
  lineData?: string;
  children: ReactNode;
}) {
  const id = useId();
  return (
    <>
      <button className={styles.secondary} type="button" {...button} aria-describedby={line ? id : undefined}>{children}</button>
      {line ? <p id={id} className={`${styles.fieldHelp} ${styles.centered}`} {...(lineData ? { [lineData]: "" } : {})}>{line}</p> : null}
    </>
  );
}

export function SignInWays(props: AuthGateScreenProps) {
  const { t } = useTranslation(["authGate", "common"]);
  const { busy, notice, providers } = props;
  const logCode = useWayBackIn(props);
  return (
    <div className={styles.form} data-auth-sign-in-ways>
      {notice ? <div className={styles.notice} role="status" aria-live="polite"><p>{notice}</p></div> : null}
      {props.deviceOwner.available ? (props.deviceOwner.accounts ? (
        <div data-device-owner-accounts>
          <p className={styles.fieldHelp}>{t("authGate:deviceOwnerChoose")}</p>
          {props.deviceOwner.accounts.map((account) => (
            <button key={account.id} className={styles.secondary} type="button" disabled={busy} onClick={() => props.deviceOwner.choose(account.id)}>
              {t("authGate:deviceOwnerAs", { name: account.name })}
            </button>
          ))}
        </div>
      ) : (
        <>
          <button className={styles.secondary} type="button" disabled={busy} onClick={props.deviceOwner.start} data-device-owner-sign-in>
            {t("authGate:deviceOwnerButton")}
          </button>
          <p className={styles.fieldHelp}>{t(/Windows/u.test(navigator.userAgent) ? "authGate:deviceOwnerHelpWindows" : "authGate:deviceOwnerHelp")}</p>
        </>
      )) : props.deviceOwner.local && props.deviceOwner.available === false ? (
        <p className={styles.fieldHelp} data-device-owner-unavailable>{t("authGate:deviceOwnerUnavailable")}</p>
      ) : null}
      <button className={styles.primary} type="button" disabled={busy} onClick={() => props.switchStep("device-code")} data-auth-use-device-code>
        {t("authGate:enterCode")}
      </button>
      <p className={`${styles.fieldHelp} ${styles.centered}`} data-auth-code-hint>{t("authGate:deviceFirstHint")}</p>
      {logCode ? <ServerLogCodeHint /> : null}
      <Way disabled={busy} onClick={() => props.switchStep("agent")} data-auth-agent-sign-in
        line={t("authGate:agentSignInHelp")} lineData="data-auth-agent-sign-in-help">
        {t("authGate:agentSignIn")}
      </Way>
      <Way onClick={() => props.switchStep("email")} disabled={busy} data-auth-sign-in-email>
        {t("authGate:signInWithEmail")}
      </Way>
      <Way onClick={props.startPasskey} disabled={busy || !props.passkeyAvailable} data-auth-sign-in-passkey
        line={props.passkeyAvailable ? "" : props.passkeyReason} lineData="data-auth-sign-in-passkey-reason">
        {t("authGate:signInWithPasskey")}
      </Way>
      {props.passkeyRunning ? <button className={styles.linkButton} type="button" onClick={props.cancelPasskey}>{t("common:actions.cancel")}</button> : null}
      {providers.map((provider) => (
        <Way key={provider.provider} onClick={() => props.startOAuth(provider.provider)} disabled={busy}>
          {t("authGate:continueWith", { provider: provider.label })}
        </Way>
      ))}
      {props.companySignIn ? (
        <Way onClick={() => props.switchStep("company")} disabled={busy} data-auth-sign-in-company>
          {t("authGate:signInWithCompany")}
        </Way>
      ) : null}
      {props.browserSignIn ? (
        <Way disabled={busy} onClick={props.browserSignIn} data-auth-browser-sign-in>
          {t("authGate:appSignIn.browser")}
        </Way>
      ) : null}
      {/* Where sign-up is closed too: its step says who may still make an account and whom to ask (2026-10-26: a
          stranger, or someone of an allowed company domain, had nowhere to try and saw neither). */}
      <button className={styles.linkButton} type="button" onClick={() => props.switchStep("signup")} disabled={busy}>
        {t("authGate:createAccount")}
      </button>
    </div>
  );
}
