import { authErrorCodes, type AuthMFAChallenge } from "@/authWire.generated";
import type { AuthText } from "@/lib/authLine";
import type { Session } from "@/types";

// What the sign-in screen (AuthGate) reads off the server's answers, and its
// round of signing in through the system's browser.

export const EMAIL_RE = /^[^\s@]+@[^\s@]+\.[^\s@]+$/u;

export function isMFAChallenge(result: Session | AuthMFAChallenge): result is AuthMFAChallenge {
  return "mfaRequired" in result;
}

export function isSessionCapacity(error: unknown) {
  return typeof error === "object" && error !== null
    && Reflect.get(error, "code") === authErrorCodes.sessionCapacity;
}

export function deadResetLink(caught: unknown) {
  const code = (caught as { code?: unknown } | null)?.code;
  return code === authErrorCodes.authTokenInvalid || code === authErrorCodes.authTokenConsumed;
}

/**
 * One sign-in through the system's browser (the app's own way, app_sign_in.go
 * and a provider's): the browser opens and the screen stays usable; once it
 * brings the sign-in back the screen is busy while it is exchanged and
 * finished. Nothing coming back leaves the screen as it was; a failure says why.
 */
export async function signInThroughBrowserRound<T>(round: {
  open: () => Promise<{ code: string; verifier: string } | null>;
  exchange: (code: string, verifier: string) => Promise<T>;
  finish: (result: T) => Promise<void>;
  setBusy: (busy: boolean) => void;
  setNotice: (notice: AuthText) => void;
  fail: (caught: unknown) => void;
}) {
  try {
    const returned = await round.open();
    if (!returned) {
      round.setNotice("");
      return;
    }
    round.setBusy(true);
    await round.finish(await round.exchange(returned.code, returned.verifier));
  } catch (caught) {
    round.setNotice("");
    round.fail(caught);
    round.setBusy(false);
  }
}
