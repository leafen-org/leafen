import { useCallback, useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { api } from "@/api";
import type { AccountPasskeyState, PasskeyRegistrationFinishInput } from "@/authWire.generated";
import { authErrorCodes } from "@/authWire.generated";
import type { Session } from "@/types";
import { newId } from "@/lib/ids";
import { passkeyErrorMessage } from "@/lib/passkeyErrorMessage";
import { preparePasskeyRegistration } from "@/lib/passkeys";

export function useAccountPasskeys(session: Session) {
  const { t } = useTranslation("workspaceSettingsDialog");
  const [state, setState] = useState<AccountPasskeyState | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const active = useRef(session);
  active.current = session;
  const context = `${session.user.id}\u0000${session.sessionId ?? session.token}`;
  const liveContext = useRef(context);
  liveContext.current = context;
  const generation = useRef(0);
  const controller = useRef<AbortController | null>(null);
  const pending = useRef<PasskeyRegistrationFinishInput | null>(null);
  const requestID = useRef("");

  const refresh = useCallback(async () => {
    const issued = ++generation.current;
    controller.current?.abort();
    const abort = new AbortController();
    controller.current = abort;
    pending.current = null;
    requestID.current = "";
    setBusy(true); setError("");
    try {
      const result = await api.readAccountPasskeys(active.current, abort.signal);
      if (issued === generation.current && liveContext.current === context) setState(result);
    } catch (failure) {
      if (!abort.signal.aborted && issued === generation.current && liveContext.current === context) setError(passkeyErrorMessage(failure, (key) => t(key), "accountPasskeys.read"));
    } finally {
      if (issued === generation.current && liveContext.current === context) setBusy(false);
    }
  }, [context, t]);

  useEffect(() => {
    setState(null); void refresh();
    return () => { generation.current += 1; controller.current?.abort(); pending.current = null; };
  }, [refresh]);

  async function perform(removeID?: string) {
    if (busy || !state) return false;
    const issued = ++generation.current;
    const abort = new AbortController();
    controller.current?.abort(); controller.current = abort;
    const current = () => !abort.signal.aborted && issued === generation.current && liveContext.current === context;
    setBusy(true); setError("");
    try {
      let result: AccountPasskeyState;
      if (removeID) {
        result = await api.removeAccountPasskey(active.current, { credentialId: removeID, baseRevision: state.revision }, abort.signal);
      } else {
        if (!pending.current) {
          requestID.current ||= newId();
          const begun = await api.beginPasskeyRegistration(active.current, { baseRevision: state.revision, requestId: requestID.current }, abort.signal);
          if (!current()) return false;
          const proof = await preparePasskeyRegistration(begun.ticket, begun.options, abort.signal);
          if (!current()) return false;
          pending.current = proof;
        }
        result = await api.finishPasskeyRegistration(active.current, pending.current, abort.signal);
      }
      if (!current()) return false;
      setState(result); pending.current = null; requestID.current = "";
      return true;
    } catch (failure) {
      if (current()) {
        const code = (failure as { code?: string })?.code;
        if (code === authErrorCodes.passkeyConflict || code === authErrorCodes.passkeyInvalid) { pending.current = null; requestID.current = ""; }
        setError(passkeyErrorMessage(failure, (key) => t(key), "accountPasskeys.update"));
      }
      return false;
    } finally { if (issued === generation.current && liveContext.current === context) setBusy(false); }
  }

  return { state, busy, error, refresh, add: () => perform(), remove: (id: string) => perform(id), clearError: () => setError("") };
}
