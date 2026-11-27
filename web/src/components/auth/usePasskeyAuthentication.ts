import { useEffect, useRef, useState } from "react";
import { api } from "@/api";
import type { PasskeyLoginFinishInput } from "@/authWire.generated";
import type { Session } from "@/types";
import { signInThroughApp } from "@/lib/appSignIn";
import { preparePasskeyLogin, PasskeyPromptCancelled } from "@/lib/passkeys";
import { passkeyErrorLine } from "@/lib/passkeyErrorMessage";
import { authWords, type AuthText } from "@/lib/authLine";
import { authErrorCodes } from "@/authWire.generated";
import type { AuthErrorContext } from "@/lib/authErrorPresentation";

type Run = (load: () => Promise<Session | null>, publish: (session: Session | null) => void | Promise<void>,
  context: AuthErrorContext, failure: (error: unknown) => boolean) => Promise<void>;

export function usePasskeyAuthentication({ step, otherServer, origin, run, acceptSession, setNotice, setError, offerCapacity }: {
  step: string; otherServer: boolean; origin: string; run: Run; acceptSession: (session: Session) => Promise<void>;
  setNotice: (text: AuthText) => void; setError: (text: AuthText) => void; offerCapacity: (error: unknown) => boolean;
}) {
  const pending = useRef<PasskeyLoginFinishInput | null>(null);
  const controller = useRef<AbortController | null>(null);
  const [running, setRunning] = useState(false);
  const mounted = useRef(true);
  useEffect(() => {
    pending.current = null;
    controller.current?.abort();
  }, [step, otherServer, origin]);
  useEffect(() => { mounted.current = true; return () => { mounted.current = false; controller.current?.abort(); }; }, []);

  async function start(replaceOldestSession = false) {
    if (running) return;
    const active = new AbortController();
    controller.current?.abort();
    controller.current = active;
    setRunning(true);
    await run(async () => {
      if (otherServer) {
        const returned = await signInThroughApp(`${origin}/auth/passkey`, "passkeyCompletion", active.signal);
        if (!returned || active.signal.aborted) throw new PasskeyPromptCancelled();
        return api.exchangePasskeyApp(returned.code, returned.verifier);
      }
      pending.current ??= await preparePasskeyLogin(active.signal);
      if (active.signal.aborted) throw new PasskeyPromptCancelled();
      const result = await api.finishPasskeyLogin({ ...pending.current, replaceOldestSession }, active.signal);
      if (!result.session) throw new Error("Missing passkey session.");
      pending.current = null;
      return result.session;
    }, async (session) => { if (session && !active.signal.aborted) await acceptSession(session); }, "auth.passkey", (error) => {
      if (active.signal.aborted) return true;
      if (offerCapacity(error)) return true;
      if ((error as { code?: string })?.code === authErrorCodes.passkeyInvalid) pending.current = null;
      setError(passkeyErrorLine(error, "auth.passkey"));
      return true;
    });
    if (mounted.current && controller.current === active) setRunning(false);
  }

  return {
    running, start,
    hasPending: () => Boolean(pending.current),
    reset: () => { pending.current = null; controller.current?.abort(); },
    cancel: () => { controller.current?.abort(); pending.current = null; setNotice(authWords("workspaceSettingsDialog:passkeyCancelled")); },
  };
}
