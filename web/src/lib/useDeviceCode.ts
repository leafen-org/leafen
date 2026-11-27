import { useEffect, useRef, useState } from "react";
import { api } from "@/api";
import type { AccountDeviceCode } from "@/authWire.generated";
import { onServerAnswersAgain } from "@/lib/serverAnswers";
import type { Session } from "@/types";

// A code another device joins this account with (account_device_codes.go),
// held while it is shown: a new one before the last runs out, and the one
// shown ended when it stops showing.

const RENEW_BEFORE_MS = 30_000;
const RETRY_FIRST_MS = 5_000;
const RETRY_MOST_MS = 60_000;

export function useDeviceCode(session: Session, showing: boolean): {
  code: AccountDeviceCode | null;
  failed: unknown;
} {
  const [code, setCode] = useState<AccountDeviceCode | null>(null);
  const [failed, setFailed] = useState<unknown>(null);
  const shown = useRef<AccountDeviceCode | null>(null);
  useEffect(() => {
    if (!showing) return undefined;
    let current = true;
    let timer: ReturnType<typeof setTimeout> | undefined;
    let retryIn = RETRY_FIRST_MS;
    // A code that could not be made comes by itself once the server answers
    // (2026-11-05: the panel stayed on its error after the server was back).
    const unlisten = onServerAnswersAgain(() => {
      if (!shown.current && timer) { clearTimeout(timer); make(); }
    });
    const make = () => {
      timer = undefined;
      void api.createDeviceCode(session).then((made) => {
        if (!current) {
          void api.cancelDeviceCode(session, made.id).catch(() => undefined);
          return;
        }
        shown.current = made;
        setCode(made);
        setFailed(null);
        retryIn = RETRY_FIRST_MS;
        timer = setTimeout(make, Math.max(5_000, made.expiresAt - Date.now() - RENEW_BEFORE_MS));
      }, (error: unknown) => {
        if (!current) return;
        setFailed(error);
        // A refusal (single sign-on only, no right to it) stays one: only no
        // answer, the server's own failure or a limit to wait out is tried again.
        const status = error && typeof error === "object" ? Reflect.get(error, "status") : undefined;
        if (typeof status === "number" && status < 500 && status !== 429) return;
        timer = setTimeout(make, retryIn);
        retryIn = Math.min(RETRY_MOST_MS, retryIn * 2);
      });
    };
    make();
    return () => {
      current = false;
      unlisten();
      if (timer) clearTimeout(timer);
      const last = shown.current;
      shown.current = null;
      setCode(null);
      if (last) void api.cancelDeviceCode(session, last.id).catch(() => undefined);
    };
  }, [session, showing]);
  return { code, failed };
}
