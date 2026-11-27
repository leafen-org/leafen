import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { onSignedOutResponse } from "@/lib/signedOutResponse";
import { markSignedOutByServer } from "@/lib/signOutMarks";
import { clearSessionIfCurrent, followSessionRenewals, followStoredSession, loadSession } from "@/lib/authSessionStore";
import { api } from "@/api";
import { authErrorCodes, type PasskeyCapability, type PasskeyLoginFinishInput } from "@/authWire.generated";
import type { Session } from "@/types";
import { preparePasskeyLogin, browserPasskeysAvailable } from "@/lib/passkeys";
import { passkeyErrorMessage } from "@/lib/passkeyErrorMessage";
import { AccountPasskeysSection } from "../account/AccountPasskeysSection";
import { AuthGate } from "./AuthGate";
import { useViewEntryFocus } from "@/lib/viewEntryFocus";
import styles from "../AuthGate.module.css";

// A native app's RP lives on its server's browser origin. This page never
// stores the app login's bearer credential; only a PKCE-bound completion
// returns to the app. Management uses the browser's own authenticated account.
export function PasskeyBrowserPage({ initialSession }: { initialSession: Session | null }) {
  const { t } = useTranslation(["workspaceSettingsDialog", "authGate"]);
  const [entry] = useState(() => new URLSearchParams(window.location.search));
  const managing = entry.get("manage") === "1";
  // Signed in here, the page stays at its own address: a reload still manages the app's passkeys.
  const [address] = useState(() => `${window.location.pathname}${window.location.search}`);
  const [session, setSession] = useState(managing ? initialSession : null);
  const heldSession = useRef(session);
  heldSession.current = session;
  // The server ended the sign-in kept here, which the sign-in screen says, as the app's does (App.tsx).
  const [ended, setEnded] = useState(false);
  const [capability, setCapability] = useState<PasskeyCapability | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [capacity, setCapacity] = useState(false);
  const pending = useRef<PasskeyLoginFinishInput | null>(null);
  const abort = useRef<AbortController | null>(null);
  const mounted = useRef(true);
  // The session limit's choice that replaces the passkey button takes the focus (lib/viewEntryFocus.ts).
  const panel = useRef<HTMLElement>(null);
  useViewEntryFocus(panel, `${managing && session ? "manage" : "sign-in"}|${capacity}`, { enterFirst: false });
  useEffect(() => {
    const controller = new AbortController();
    mounted.current = true;
    void api.passkeyCapability(controller.signal).then(setCapability).catch((failure) => { if (!controller.signal.aborted) setError(passkeyErrorMessage(failure, (key) => t(key), "auth.passkey")); });
    return () => { mounted.current = false; controller.abort(); abort.current?.abort(); };
  }, [t]);

  useEffect(() => {
    if (!managing || !session) return;
    return onSignedOutResponse((token) => { void (async () => {
      if (token !== session.token) return;
      markSignedOutByServer(session);
      await clearSessionIfCurrent(session, true);
      if (heldSession.current?.token !== session.token) return;
      const replacement = loadSession();
      setEnded(!replacement);
      setSession(replacement);
    })().catch(() => { if (heldSession.current?.token === session.token) { setEnded(true); setSession(null); } }); });
  }, [managing, session]);

  useEffect(() => managing ? followStoredSession(() => heldSession.current, setSession) : undefined, [managing]);
  useEffect(() => managing && session ? followSessionRenewals(session) : undefined, [managing, session]);

  async function signIn(replaceOldestSession = false) {
    if (busy) return;
    const controller = new AbortController(); abort.current = controller;
    setBusy(true); setError("");
    try {
      if (!entry.get("app_return") || !entry.get("app_challenge")) {
        throw { code: authErrorCodes.passkeyInvalid };
      }
      pending.current ??= await preparePasskeyLogin(controller.signal, entry.get("app_return") ?? "", entry.get("app_challenge") ?? "");
      if (controller.signal.aborted) return;
      const result = await api.finishPasskeyLogin({ ...pending.current, replaceOldestSession }, controller.signal);
      if (!mounted.current || controller.signal.aborted) return;
      if (!result.returnUrl) throw new Error("Missing passkey app return.");
      pending.current = null;
      window.location.assign(result.returnUrl);
    } catch (failure) {
      if (!mounted.current || controller.signal.aborted) return;
      if ((failure as { code?: string })?.code === authErrorCodes.sessionCapacity) setCapacity(true);
      else {
        if ((failure as { code?: string })?.code === authErrorCodes.passkeyInvalid) pending.current = null;
        setError(passkeyErrorMessage(failure, (key) => t(key), "auth.passkey"));
      }
    } finally { if (mounted.current) setBusy(false); }
  }

  async function done() {
    setBusy(true); setError("");
    try {
      const result = await api.passkeyManageReturn(entry.get("app_return") ?? "", entry.get("app_challenge") ?? "");
      if (mounted.current && result.returnUrl) window.location.assign(result.returnUrl);
    } catch (failure) { if (mounted.current) setError(passkeyErrorMessage(failure, (key) => t(key), "auth.passkey")); }
    finally { if (mounted.current) setBusy(false); }
  }

  if (managing && !session) return <AuthGate sessionEnded={ended} onSession={(next) => {
    window.history.replaceState(null, "", address);
    setEnded(false);
    setSession(next);
  }} />;
  return <main className={styles.screen}><section ref={panel} className={styles.panel} aria-busy={busy}>
    <h1>Leafen</h1>
    {managing && session ? <><p>{session.user.email || session.user.name}</p><AccountPasskeysSection session={session} /><button className={styles.secondary} disabled={busy} onClick={() => void done()}>{t("authGate:backToThisDevice")}</button></>
      : <><h2>{t("continueWithPasskey")}</h2><p>{t("passkeysDescription")}</p>
        {capacity ? <div className={styles.notice}><p>{t("authGate:sessionCapacityBody")}</p><button className={styles.primary} disabled={busy} onClick={() => void signIn(true)}>{t("authGate:replaceOldestSession")}</button></div>
          : <button className={styles.primary} disabled={busy || !capability?.available || capability.origin !== window.location.origin || !browserPasskeysAvailable()} onClick={() => void signIn()}>{t("continueWithPasskey")}</button>}
        {capability && !capability.available ? <p>{t("passkeyUnavailable")}</p> : null}
        {capability?.available && (capability.origin !== window.location.origin || !browserPasskeysAvailable()) ? <p>{t("passkeyDeviceUnsupported")}</p> : null}
        {busy ? <button className={styles.secondary} onClick={() => { abort.current?.abort(); setError(t("passkeyCancelled")); }}>{t("managedUsersCancel")}</button> : null}
      </>}
    {error ? <p className={styles.error} role="alert">{error}</p> : null}
  </section></main>;
}
