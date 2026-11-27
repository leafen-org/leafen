import { useCallback, useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { api } from "@/api";
import type { AccountEmailPending, AccountEmailState } from "@/authWire.generated";
import { saveSessionIfCurrent } from "@/lib/authSessionStore";
import type { Session } from "@/types";
import { accountEmailErrorMessage, accountEmailGone, accountEmailsShownBehind } from "./accountEmailErrorMessage";

export function useAccountEmails(session: Session, onSession: (session: Session) => void) {
  const { t } = useTranslation("workspaceSettingsDialog");
  const [state, setState] = useState<AccountEmailState | null>(null);
  const [pending, setPending] = useState<AccountEmailPending | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const active = useRef(session);
  active.current = session;
  const generation = useRef(0);
  const requestAbort = useRef<AbortController | null>(null);
  const context = `${session.user.id}\u0000${session.sessionId ?? session.token}`;
  const currentContext = useRef(context);
  currentContext.current = context;

  const report = useCallback((failure: unknown, context: "accountEmails.read" | "accountEmails.update") => {
    setError(accountEmailErrorMessage(failure, (key) => t(key), context));
  }, [t]);

  const refresh = useCallback(async () => {
    const issued = ++generation.current;
    const controller = new AbortController();
    requestAbort.current?.abort();
    requestAbort.current = controller;
    setBusy(true);
    setError("");
    try {
      const result = await api.readAccountEmails(active.current, controller.signal);
      if (issued === generation.current && currentContext.current === context) setState(result);
    } catch (failure) {
      if (!controller.signal.aborted && issued === generation.current) report(failure, "accountEmails.read");
    } finally {
      if (issued === generation.current && currentContext.current === context) setBusy(false);
    }
  }, [context, report]);

  useEffect(() => {
    setState(null);
    setPending(null);
    void refresh();
    return () => { generation.current += 1; requestAbort.current?.abort(); };
  }, [refresh]);

  /**
   * "pending" while a code was sent, "done" once the addresses changed,
   * "gone" when the address named was already taken away elsewhere (the list
   * read again), false on another failure.
   */
  async function perform(action: () => Promise<AccountEmailState | AccountEmailPending>): Promise<"pending" | "done" | "gone" | false> {
    if (busy || !state) return false;
    const issued = ++generation.current;
    setBusy(true);
    setError("");
    try {
      const result = await action();
      if (issued !== generation.current || currentContext.current !== context) return false;
      if ("nonce" in result) {
        setPending(result);
        return "pending";
      } else {
        setState(result);
        setPending(null);
        const primary = result.emails.find((email) => email.primary)?.email;
        if (primary && primary !== active.current.user.email) {
          const next = { ...active.current, user: { ...active.current.user, email: primary } };
          if (!await saveSessionIfCurrent(active.current, next) || issued !== generation.current || currentContext.current !== context) return false;
          onSession(next);
        }
      }
      return "done";
    } catch (failure) {
      if (issued !== generation.current || currentContext.current !== context) return false;
      // Settings changed elsewhere since they were read show as they are
      // now, with what happened (accountEmailErrorMessage.ts).
      if (accountEmailsShownBehind(failure)) {
        const now = await api.readAccountEmails(active.current).catch(() => null);
        if (issued !== generation.current || currentContext.current !== context) return false;
        if (now) setState(now);
      }
      report(failure, "accountEmails.update");
      return accountEmailGone(failure) ? "gone" : false;
    } finally {
      if (issued === generation.current && currentContext.current === context) setBusy(false);
    }
  }

  return {
    state, pending, busy, error, refresh,
    clearError: () => setError(""),
    add: (email: string) => perform(() => api.addAccountEmail(active.current, {
      email, baseRevision: state!.revision,
    })),
    confirm: (verificationCode: string, currentVerificationCode?: string) => perform(() => api.confirmAccountEmail(active.current, {
      email: pending!.email, nonce: pending!.nonce, verificationCode,
      ...(pending!.currentEmail ? { currentVerificationCode } : {}),
    })),
    change: (email: string, primary: boolean) => perform(() => (
      primary ? api.promoteAccountEmail : api.removeAccountEmail
    )(active.current, { email, baseRevision: state!.revision })),
  };
}
