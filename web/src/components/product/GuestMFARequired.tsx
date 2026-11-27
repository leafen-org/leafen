import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { api } from "@/api";
import { TopBar } from "@/components/page/TopBar";
import styles from "@/components/page/PageView.module.css";
import { resetDocumentChrome } from "@/lib/documentChrome";
import { pushRoute } from "@/lib/router";
import type { Session } from "@/types";

type Step = { kind: "loading" } | { kind: "turnOn" } | { kind: "confirm" } | { kind: "others"; count: number } | { kind: "retry" };

/**
 * Notion's MFA enforcement screen (its help, read 2026-10-10): a guest of a
 * workspace whose organization requires 2-step verification goes on once
 * they turn it on, in their account's security settings. Leafen asks every
 * session they're signed in with to have gone through it, so the screen
 * asks for the step still missing: turning it on, confirming this sign-in
 * with a code, or signing out the sign-ins without it.
 */
export function GuestMFARequired({ session }: { session: Session }) {
  const { t } = useTranslation("routeState");
  const [step, setStep] = useState<Step>({ kind: "loading" });
  const [code, setCode] = useState("");
  const [busy, setBusy] = useState(false);
  const [failed, setFailed] = useState(false);
  useEffect(() => { resetDocumentChrome(); }, []);
  useEffect(() => {
    const controller = new AbortController();
    void Promise.all([api.readAccountMFA(session, controller.signal), api.listAuthSessions(session, controller.signal)]).then(([mfa, sessions]) => {
      // A session with a second factor — a code, a passkey or its sign-in
      // provider's assurance — asks only that the others have one too.
      const bare = sessions.filter((item) => !item.secondFactor);
      const twoStep = mfa.factors.some((factor) => factor.verified);
      setStep(bare.some((item) => item.current) ? twoStep ? { kind: "confirm" } : { kind: "turnOn" }
        : bare.length ? { kind: "others", count: bare.length }
        : twoStep || sessions.some((item) => item.secondFactor === "provider") ? { kind: "retry" } : { kind: "turnOn" });
    }, () => { if (!controller.signal.aborted) setStep({ kind: "turnOn" }); });
    return () => controller.abort();
  }, [session]);
  const back = `${window.location.pathname}${window.location.search}`;
  async function run(action: () => Promise<void>) {
    setBusy(true); setFailed(false);
    try { await action(); window.location.reload(); } catch { setFailed(true); } finally { setBusy(false); }
  }
  return (
    <>
      <TopBar title={t("guestMFATitle")} />
      <div className={styles.missing} data-surface="guest-mfa-required" data-guest-mfa-step={step.kind} role="status">
        <h1 className={styles.missingHeading}>{t("guestMFATitle")}</h1>
        <p>{step.kind === "confirm" ? t("guestMFAConfirmBody") : step.kind === "others" ? t("guestMFAOthersBody", { count: step.count }) : t("guestMFABody")}</p>
        {step.kind === "confirm" ? (
          <form className={styles.missingActions} onSubmit={(event) => { event.preventDefault(); void run(() => api.confirmSessionSecondFactor(session, code)); }}>
            <input value={code} aria-label={t("guestMFACodeLabel")} placeholder="123456" inputMode="numeric" autoComplete="one-time-code" maxLength={6}
              onChange={(event) => setCode(event.target.value.replace(/\D/gu, "").slice(0, 6))} disabled={busy} />
            <button type="submit" className={styles.restoreButton} disabled={busy || code.length !== 6}>{t("guestMFAConfirm")}</button>
          </form>
        ) : (
          <div className={styles.missingActions}>
            {step.kind === "others" ? (
              <button type="button" className={styles.restoreButton} disabled={busy}
                onClick={() => void run(() => api.revokeSessionsWithoutSecondFactor(session))}>{t("guestMFASignOutOthers")}</button>
            ) : step.kind === "retry" ? (
              <button type="button" className={styles.restoreButton} onClick={() => window.location.reload()}>{t("guestMFARetry")}</button>
            ) : (
              <button type="button" className={styles.restoreButton}
                onClick={() => pushRoute(`/settings?section=account&from=${encodeURIComponent(back)}`)}>
                {t("guestMFAOpenSecurity")}
              </button>
            )}
          </div>
        )}
        {failed ? <p role="alert">{t(step.kind === "confirm" ? "guestMFAConfirmFailed" : "guestMFASignOutFailed")}</p> : null}
      </div>
    </>
  );
}
