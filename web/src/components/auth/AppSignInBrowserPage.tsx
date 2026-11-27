import { useEffect, useRef, useState } from "react";
import { useViewEntryFocus } from "@/lib/viewEntryFocus";
import { useTranslation } from "react-i18next";
import { api } from "@/api";
import { clearSessionIfCurrent } from "@/lib/authSessionStore";
import { onSignedOutResponse } from "@/lib/signedOutResponse";
import { markSignedOutByServer } from "@/lib/signOutMarks";
import type { Session } from "@/types";
import { AuthGate } from "./AuthGate";
import styles from "../AuthGate.module.css";
import { pageMarkSource } from "@/lib/pageMark";

/**
 * The server's "register this app" page (internal/app/app_sign_in.go), which
 * a packaged app opens in the system's browser: a browser signed in here —
 * or once it signs in as usual — registers the app named, as a device of
 * this account, with one press; the app's code goes back to the app alone.
 */
export function AppSignInBrowserPage({ initialSession }: { initialSession: Session | null }) {
  const { t } = useTranslation("authGate");
  const [entry] = useState(() => new URLSearchParams(window.location.search));
  // Signed in here, the page stays at its own address: a reload still asks to register the app.
  const [address] = useState(() => `${window.location.pathname}${window.location.search}`);
  const [session, setSession] = useState(initialSession);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [done, setDone] = useState<"" | "registered" | "cancelled">("");
  // The server ended the sign-in kept here, which the sign-in screen says, as the app's does (App.tsx).
  const [ended, setEnded] = useState(false);
  // How many times someone signed in on this page: each sign-in draws the card anew.
  const [signIns, setSignIns] = useState(0);
  const appName = (entry.get("app_name") ?? "").trim().slice(0, 120) || t("appSignIn.someApp");
  // A sign-in kept here that the server ended (elsewhere, or by running
  // out) signs in again first, as it's checked when the page opens.
  useEffect(() => {
    if (!session) return undefined;
    const stop = onSignedOutResponse((token) => {
      if (token !== session.token) return;
      markSignedOutByServer(session);
      void clearSessionIfCurrent(session, true);
      setError("");
      setEnded(true);
      setSession(null);
    });
    const check = new AbortController();
    void api.readAccountProfile(session, check.signal).catch(() => undefined);
    return () => { stop(); check.abort(); };
  }, [session]);
  // The card that replaces the sign-in screen, and the answer that replaces
  // its buttons, take the focus (lib/viewEntryFocus.ts). Every hook runs
  // before the sign-in screen takes the page's place: a sign-in that ends,
  // or begins, between two renders changes no hook count (2026-11-18: an
  // ended one blanked the page with React's error #300).
  const panel = useRef<HTMLElement>(null);
  useViewEntryFocus(panel, session ? `card${signIns}|${done}` : "sign-in", { enterFirst: false });
  if (!session) return <AuthGate sessionEnded={ended} onSession={(next) => {
    window.history.replaceState(null, "", address);
    setEnded(false);
    setSignIns((count) => count + 1);
    setSession(next);
  }} />;
  const account = session.user.name || session.user.email;

  async function register() {
    if (!session) return;
    setBusy(true);
    setError("");
    try {
      const answer = await api.registerAppByBrowser(session, { appReturn: entry.get("app_return") ?? "", appChallenge: entry.get("app_challenge") ?? "" });
      setDone("registered");
      window.location.assign(answer.returnUrl);
    } catch {
      setError(t("appSignIn.failed"));
      setBusy(false);
    }
  }

  return (
    <main className={styles.screen} data-app-sign-in>
      <section ref={panel} className={styles.panel} aria-busy={busy}>
        <div className={styles.brand}>
          <img className={styles.mark} src={pageMarkSource()} alt="" aria-hidden="true" />
          <div><h1>Leafen</h1><p>{window.location.host}</p></div>
        </div>
        <div className={styles.notice}>
          <strong>{t("appSignIn.title")}</strong>
          <p>{t("appSignIn.body", { app: appName, account })}</p>
        </div>
        {done ? (
          <p className={styles.fieldHelp} role="status" data-app-sign-in-done={done}>{t(done === "registered" ? "appSignIn.registered" : "appSignIn.cancelled")}</p>
        ) : (
          <div className={styles.form}>
            <button className={styles.primary} type="button" disabled={busy} onClick={() => void register()} data-app-sign-in-register>
              {t("appSignIn.register")}
            </button>
            <button className={styles.secondary} type="button" disabled={busy} onClick={() => setDone("cancelled")}>
              {t("appSignIn.cancel")}
            </button>
          </div>
        )}
        {error ? <p className={styles.error} role="alert">{error}</p> : null}
      </section>
    </main>
  );
}
