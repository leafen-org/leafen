import { administratorErrorCodes } from "@/administratorWire.generated";
import { authErrorCodes } from "@/authWire.generated";
import { authLine, sayAuthLine, type AuthLine } from "@/lib/authLine";
import { retryWaitText } from "@/lib/retryWait";
import { guestLinkHeldText, invitationInvalidText } from "@/lib/signupInvitation";
import { isPageBusyRefusal } from "@/lib/pageBusy";
import {
  productErrorMessage,
  productErrorPresentation,
  productErrorReceiptPresentation,
  productErrorRetold,
  type ProductErrorContext,
} from "@/lib/productErrorPresentation";

// The lines a sign-in screen holds and draws (lib/authLine.ts).
export { authLine, authWords, sayAuthLine, type AuthLine, type AuthText } from "@/lib/authLine";

export type AuthErrorContext = Extract<ProductErrorContext, `auth.${string}`>;

// Limits the server counts by the address a refusal came from, said as what
// was counted (its auth_invitation_join.go and setup_code.go); any other
// limit says the product's own line, with the wait it named.
const ADDRESS_LIMITS: Record<string, string> = {
  [authErrorCodes.invitationJoinLimited]: "authGate:invitationJoinLimited",
  [administratorErrorCodes.setupCodeLimited]: "authGate:setupCodeLimited",
};

/**
 * What an auth failure says, held as what to say (lib/authLine.ts): the
 * sign-in screen makes its words where it draws the line, in the language
 * then.
 */
export function authErrorLine(
  error: unknown,
  context: AuthErrorContext = "auth.signIn",
): AuthLine {
  return authOwnLine(error) ?? productErrorLine(error, context);
}

// The failures auth says in its own words; any other is the product's.
function authOwnLine(error: unknown): AuthLine | null {
  // Guessing slowed this account's password sign-in (the server's
  // password_backoff.go): how long, and the way in that doesn't wait.
  const { code, retryAfter } = (error ?? {}) as { code?: unknown; retryAfter?: unknown };
  if (code === authErrorCodes.passwordWait) {
    const minutes = Math.max(1, Math.ceil((typeof retryAfter === "number" ? retryAfter : 60) / 60));
    return authLine((t) => String(t("authGate:passwordWait", { count: minutes })));
  }
  // An invitation that can't let anyone in says so, and a guest invite link
  // that ended says why (lib/signupInvitation.ts), on whichever step met it
  // (2026-11-23: the name step said a used guest link's person had no
  // permission, where no one was signed in).
  if (code === authErrorCodes.signupInvitationInvalid) return authLine((t) => invitationInvalidText(error, t));
  // A guest invite link one of whose pages is held a while waits unused, and says so.
  if (isPageBusyRefusal(error)) return authLine((t) => guestLinkHeldText(error, t));
  // How long to wait is the one the refusal named (lib/retryWait.ts): an
  // hour's limit says an hour, not "a moment" (2026-11-23).
  const counted = typeof code === "string" ? ADDRESS_LIMITS[code] : undefined;
  if (!counted) return null;
  return authLine((t) => `${t(counted)} ${retryWaitText(retryAfter, t) || t("productErrors:meanings.waitMomentBeforeTryingAgain")}`);
}

/**
 * A failure said as its screen says it (productErrorPresentation), held as
 * what to say: logged once, here, under one reference, and told again in
 * each language it is drawn in under that same reference. A failure the
 * server already gave a reference (a provider's return) keeps that one.
 */
export function productErrorLine(error: unknown, context: ProductErrorContext, { receipt = false } = {}): AuthLine {
  const { reference } = (receipt ? productErrorReceiptPresentation : productErrorPresentation)(error, context);
  return { say: () => productErrorRetold(error, context, reference) };
}

/** The same in the language now, for a screen that draws it once. */
export function authErrorMessage(
  error: unknown,
  context: AuthErrorContext = "auth.signIn",
): string {
  const own = authOwnLine(error);
  return own ? sayAuthLine(own) : productErrorMessage(error, context);
}
