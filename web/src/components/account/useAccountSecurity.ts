import { useCallback, useEffect, useRef, useState } from "react";
import { useAttemptNotice } from "@/lib/useNoticeFor";
import { useTranslation } from "react-i18next";
import { api } from "@/api";
import type {
  AuthMFAEnrollment,
  AuthMFARevalidationInput,
  AuthSession,
} from "@/authWire.generated";
import { accountPasswordService } from "@/lib/accountPasswordService";
import {
  accountSecurityService,
  type AccountSecuritySnapshot,
} from "@/lib/accountSecurityService";
import { productErrorMessage } from "@/lib/productErrorPresentation";
import { authErrorCodes } from "@/authWire.generated";
import { loadSession, sameSessionSignIn } from "@/lib/authSessionStore";
import type { Session } from "@/types";
import { withLoadDeadline } from "@/lib/lazyScreen";
import { passwordAcceptable } from "@/lib/passwordRule";

type SecurityBusy =
  | ""
  | "mfa:disable"
  | "mfa:enroll"
  | "mfa:recovery-codes"
  | "mfa:verify"
  | "password:change"
  | "password:set"
  | "password:forget"
  | "sessions:others"
  | `session:${string}`;

function sessionContext(session: Session): string {
  return `${session.user.id}\u0000${session.sessionId ?? session.token}`;
}

function revalidation(value: string): AuthMFARevalidationInput {
  const normalized = value.trim();
  return /^\d{6}$/u.test(normalized) ? { code: normalized } : { password: normalized };
}

export function useAccountSecurity(
  session: Session,
  onSession: (session: Session) => void,
) {
  const { t } = useTranslation("workspaceSettingsDialog");
  const [snapshot, setSnapshot] = useState<AccountSecuritySnapshot | null>(null);
  const heldSnapshot = useRef(snapshot);
  heldSnapshot.current = snapshot;
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState<SecurityBusy>("");
  const [notice, setNotice] = useState("");
  const [enrollment, setEnrollment] = useState<AuthMFAEnrollment | null>(null);
  const [qrCodeDataUrl, setQRCodeDataUrl] = useState("");
  const [qrCodeError, setQRCodeError] = useState("");
  const [recoveryCodes, setRecoveryCodes] = useState<string[]>([]);
  const [passwordCurrent, setPasswordCurrent] = useState("");
  const [passwordNew, setPasswordNew] = useState("");
  const [passwordConfirm, setPasswordConfirm] = useState("");
  const [setupCode, setSetupCode] = useState("");
  const [disableCode, setDisableCode] = useState("");
  const [recoveryConfirm, setRecoveryConfirm] = useState("");
  // What a try said about the typed passwords or codes goes once one is typed again (2026-10-27).
  const [error, setError] = useAttemptNotice<string>(passwordCurrent, passwordNew, passwordConfirm, setupCode, disableCode, recoveryConfirm);
  // Which password field a refusal is about, gone as the fields change (AccountPasswordSection marks it).
  const [passwordInvalid, setPasswordInvalid] = useAttemptNotice<"current" | "new" | "confirm">(passwordCurrent, passwordNew, passwordConfirm);
  const lifecycleGeneration = useRef(0);
  const readGeneration = useRef(0);
  const currentSession = useRef(session);
  currentSession.current = session;

  const isCurrent = useCallback((lifecycle: number, context: string) => (
    lifecycleGeneration.current === lifecycle
      && sessionContext(currentSession.current) === context
  ), []);

  const refresh = useCallback(async (activeSession: Session = session) => {
    const lifecycle = lifecycleGeneration.current;
    const issued = ++readGeneration.current;
    const context = sessionContext(activeSession);
    const readIsCurrent = () => isCurrent(lifecycle, context) && readGeneration.current === issued;
    setLoading(true);
    setError("");
    try {
      const next = await accountSecurityService.read(activeSession);
      if (!readIsCurrent()) return false;
      setSnapshot(next);
      return true;
    } catch (caught) {
      if (readIsCurrent()) {
        setError(productErrorMessage(caught, "accountSecurity.read"));
      }
      return false;
    } finally {
      if (readIsCurrent()) setLoading(false);
    }
  }, [isCurrent, session]);

  // The devices alone, read again without marking the page busy: another
  // device signing in or out shows in a list left open. A first read that
  // failed is tried again the same quiet way, so the list comes once the
  // server answers (2026-11-06: a read during a server restart left
  // "no active sessions" until the settings opened again).
  const refreshSessions = useCallback(async () => {
    const lifecycle = lifecycleGeneration.current;
    const issued = readGeneration.current;
    const context = sessionContext(session);
    if (!heldSnapshot.current) {
      const next = await accountSecurityService.read(session).catch(() => null);
      if (!next || !isCurrent(lifecycle, context) || readGeneration.current !== issued || heldSnapshot.current) return;
      setSnapshot(next);
      setError("");
      return;
    }
    const sessions = await api.listAuthSessions(session).catch(() => null);
    if (!sessions || !isCurrent(lifecycle, context) || readGeneration.current !== issued) return;
    setSnapshot((current) => {
      if (!current) return current;
      const same = JSON.stringify(current.sessions) === JSON.stringify(sessions);
      return same ? current : { ...current, sessions };
    });
  }, [isCurrent, session, setError]);

  useEffect(() => {
    const lifecycle = ++lifecycleGeneration.current;
    void refresh();
    return () => {
      if (lifecycleGeneration.current === lifecycle) lifecycleGeneration.current += 1;
      readGeneration.current += 1;
    };
  }, [refresh]);

  async function changePassword() {
    setPasswordInvalid(null);
    if (!passwordCurrent || !passwordNew) {
      setError(t("enterCurrentAndNewPassword"));
      setPasswordInvalid(passwordCurrent ? "new" : "current");
      return false;
    }
    if (!passwordAcceptable(passwordNew)) {
      setError(t("newPasswordTooShort"));
      setPasswordInvalid("new");
      return false;
    }
    if (passwordNew !== passwordConfirm) {
      setError(t("newPasswordMismatch"));
      setPasswordInvalid("confirm");
      return false;
    }
    setBusy("password:change");
    setError("");
    setNotice("");
    const lifecycle = lifecycleGeneration.current;
    const context = sessionContext(session);
    try {
      const next = await accountPasswordService.change(session, {
        currentPassword: passwordCurrent,
        newPassword: passwordNew,
        newPasswordConfirm: passwordConfirm,
      });
      if (!isCurrent(lifecycle, context)) return false;
      setPasswordCurrent("");
      setPasswordNew("");
      setPasswordConfirm("");
      setNotice(t("passwordChangedNotice"));
      setBusy("");
      onSession(next);
      return true;
    } catch (caught) {
      if (!isCurrent(lifecycle, context)) return false;
      setError(productErrorMessage(caught, "accountSecurity.password"));
      if ((caught as { code?: unknown } | null)?.code === authErrorCodes.invalidCredentials) setPasswordInvalid("current");
      return false;
    } finally {
      if (isCurrent(lifecycle, context)) setBusy("");
    }
  }

  // A backup password for an account whose person knows none: no current
  // password, and the devices stay signed in (account_password.go).
  async function setPassword() {
    setPasswordInvalid(null);
    if (!passwordAcceptable(passwordNew)) {
      setError(t("newPasswordTooShort"));
      setPasswordInvalid("new");
      return false;
    }
    if (passwordNew !== passwordConfirm) {
      setError(t("newPasswordMismatch"));
      setPasswordInvalid("confirm");
      return false;
    }
    setBusy("password:set");
    setError("");
    setNotice("");
    const lifecycle = lifecycleGeneration.current;
    const context = sessionContext(session);
    try {
      await api.setAccountPassword(session, { currentPassword: "", newPassword: passwordNew, newPasswordConfirm: passwordConfirm });
      if (!isCurrent(lifecycle, context)) return false;
      setPasswordNew("");
      setPasswordConfirm("");
      // Without an email the password signs in nowhere yet: it says what to add.
      setNotice(t(session.user.email ? "passwordSetNotice" : "passwordSetNoticeNoEmail"));
      await refresh();
      return true;
    } catch (caught) {
      if (isCurrent(lifecycle, context)) setError(productErrorMessage(caught, "accountSecurity.password"));
      return false;
    } finally {
      if (isCurrent(lifecycle, context)) setBusy("");
    }
  }

  async function forgetPassword() {
    setBusy("password:forget");
    setError("");
    setNotice("");
    const lifecycle = lifecycleGeneration.current;
    const context = sessionContext(session);
    try {
      await api.forgetAccountPassword(session);
      if (!isCurrent(lifecycle, context)) return false;
      setNotice(t("passwordForgottenNotice"));
      await refresh();
      return true;
    } catch (caught) {
      if (isCurrent(lifecycle, context)) setError(productErrorMessage(caught, "accountSecurity.password"));
      return false;
    } finally {
      if (isCurrent(lifecycle, context)) setBusy("");
    }
  }

  async function startEnrollment() {
    const lifecycle = lifecycleGeneration.current;
    const context = sessionContext(session);
    setBusy("mfa:enroll");
    setError("");
    setNotice("");
    setQRCodeDataUrl("");
    setQRCodeError("");
    try {
      const next = await api.enrollAccountTOTP(session);
      if (!isCurrent(lifecycle, context)) return;
      setEnrollment(next);
      try {
        const { default: QRCode } = await withLoadDeadline(import("qrcode"));
        if (!isCurrent(lifecycle, context)) return;
        const dataUrl = await QRCode.toDataURL(next.qrCodeUri, { margin: 1, width: 280 });
        if (isCurrent(lifecycle, context)) setQRCodeDataUrl(dataUrl);
      } catch {
        if (isCurrent(lifecycle, context)) setQRCodeError(t("qrCodeFailed"));
      }
    } catch (caught) {
      if (!isCurrent(lifecycle, context)) return;
      setError(productErrorMessage(caught, "accountSecurity.mfa"));
    } finally {
      if (isCurrent(lifecycle, context)) setBusy("");
    }
  }

  async function confirmEnrollment() {
    if (!enrollment || setupCode.length !== 6) {
      setError(t("totpCodeMismatch"));
      return;
    }
    setBusy("mfa:verify");
    setError("");
    setNotice("");
    const lifecycle = lifecycleGeneration.current;
    const context = sessionContext(session);
    try {
      const state = await api.confirmAccountTOTP(session, {
        factorId: enrollment.factorId,
        code: setupCode,
      });
      if (!isCurrent(lifecycle, context)) return;
      setSnapshot((current) => current ? { ...current, mfa: state } : current);
      setRecoveryCodes(enrollment.recoveryCodes);
      setEnrollment(null);
      setQRCodeDataUrl("");
      setSetupCode("");
      setNotice(t("mfaEnabledNotice"));
    } catch (caught) {
      if (!isCurrent(lifecycle, context)) return;
      setError(productErrorMessage(caught, "accountSecurity.mfa"));
    } finally {
      if (isCurrent(lifecycle, context)) setBusy("");
    }
  }

  async function disableTOTP() {
    if (!disableCode.trim()) {
      setError(t("enterCodeOrPassword"));
      return false;
    }
    setBusy("mfa:disable");
    setError("");
    setNotice("");
    const lifecycle = lifecycleGeneration.current;
    const context = sessionContext(session);
    try {
      await api.disableAccountTOTP(session, revalidation(disableCode));
      if (!isCurrent(lifecycle, context)) return false;
      setSnapshot((current) => current ? {
        ...current,
        mfa: { factors: [], recoveryCodesRemaining: 0 },
      } : current);
      setDisableCode("");
      setRecoveryCodes([]);
      setEnrollment(null);
      setNotice(t("mfaDisabledNotice"));
      return true;
    } catch (caught) {
      if (!isCurrent(lifecycle, context)) return false;
      setError(productErrorMessage(caught, "accountSecurity.mfa"));
      return false;
    } finally {
      if (isCurrent(lifecycle, context)) setBusy("");
    }
  }

  async function regenerateRecoveryCodes() {
    if (!recoveryConfirm.trim()) {
      setError(t("enterCodeOrPassword"));
      return;
    }
    setBusy("mfa:recovery-codes");
    setError("");
    setNotice("");
    const lifecycle = lifecycleGeneration.current;
    const context = sessionContext(session);
    try {
      const result = await api.regenerateAccountMFARecoveryCodes(
        session,
        revalidation(recoveryConfirm),
      );
      if (!isCurrent(lifecycle, context)) return;
      setRecoveryCodes(result.recoveryCodes);
      setRecoveryConfirm("");
    } catch (caught) {
      if (!isCurrent(lifecycle, context)) return;
      setError(productErrorMessage(caught, "accountSecurity.recovery"));
    } finally {
      if (isCurrent(lifecycle, context)) setBusy("");
    }
  }

  async function revokeSession(target: AuthSession) {
    if (target.current) return;
    if (!sameSessionSignIn(loadSession(), session)) return;
    setBusy(`session:${target.id}`);
    setError("");
    setNotice("");
    const lifecycle = lifecycleGeneration.current;
    const context = sessionContext(session);
    try {
      await api.revokeAuthSession(session, target.id);
      if (!isCurrent(lifecycle, context)) return;
      setNotice(t("noticeSessionRevoked"));
      await refresh();
    } catch (caught) {
      if (!isCurrent(lifecycle, context)) return;
      setError(productErrorMessage(caught, "accountSecurity.sessionRevoke"));
    } finally {
      if (isCurrent(lifecycle, context)) setBusy("");
    }
  }

  async function revokeOtherSessions() {
    if (!sameSessionSignIn(loadSession(), session)) return;
    const targets = snapshot?.sessions.filter((item) => !item.current) ?? [];
    if (!targets.length) {
      setNotice(t("noticeNoOtherSessions"));
      return;
    }
    setBusy("sessions:others");
    setError("");
    setNotice("");
    const lifecycle = lifecycleGeneration.current;
    const context = sessionContext(session);
    const settled = await Promise.allSettled(
      targets.map((target) => api.revokeAuthSession(session, target.id)),
    );
    if (!isCurrent(lifecycle, context)) return;
    const refreshed = await refresh();
    if (!isCurrent(lifecycle, context)) return;
    if (!refreshed) {
      setBusy("");
      return;
    }
    const failure = settled.find((result) => result.status === "rejected");
    if (failure?.status === "rejected") {
      setError(productErrorMessage(failure.reason, "accountSecurity.sessionRevoke"));
    } else {
      setNotice(t("noticeOtherSessionsRevoked", { count: targets.length }));
    }
    setBusy("");
  }

  function clearPasswordInput() {
    setPasswordCurrent(""); setPasswordNew(""); setPasswordConfirm(""); setError("");
  }

  function dismissNotice() {
    setNotice("");
  }

  function clearMFAInput() {
    setEnrollment(null); setQRCodeDataUrl(""); setQRCodeError(""); setRecoveryCodes([]);
    setSetupCode(""); setDisableCode(""); setRecoveryConfirm(""); setError("");
  }

  return {
    busy, disableCode, enrollment, error, loading, notice, passwordConfirm,
    passwordCurrent, passwordInvalid, passwordNew, qrCodeDataUrl, qrCodeError, recoveryCodes,
    recoveryConfirm, setupCode, snapshot,
    changePassword, setPassword, forgetPassword, clearPasswordInput, clearMFAInput, confirmEnrollment, disableTOTP, refresh, refreshSessions,
    regenerateRecoveryCodes, revokeOtherSessions, revokeSession, startEnrollment, dismissNotice,
    setDisableCode, setPasswordConfirm, setPasswordCurrent, setPasswordNew,
    setRecoveryConfirm, setSetupCode,
  };
}
