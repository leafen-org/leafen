import type { TFunction } from "i18next";
import { authErrorCodes, type AuthAuditMethod } from "@/authWire.generated";
import type { ProductErrorContext } from "@/lib/productErrorContextCatalog";
import { productErrorCause } from "@/lib/productErrorPresentation";

// Each way the server records a sign-in by (contracts/auth-wire.json) and the
// screen whose words its failures had; a way the contract adds fails the build
// here until it has both (2026-10-26: "로그인했습니다 · device_code").
const METHOD_SCREEN: Record<AuthAuditMethod, ProductErrorContext> = {
  password_signup: "auth.signUp",
  password_signin: "auth.signIn",
  oauth_signin: "auth.providerExchange",
  mfa_totp: "auth.mfa",
  mfa_recovery: "auth.mfa",
  saml_signin: "auth.providerExchange",
  passkey_signin: "auth.passkey",
  device_code: "auth.deviceCode",
  invitation_join: "auth.signUp",
  setup: "auth.serverSetup",
};

function knownMethod(method: string): method is AuthAuditMethod {
  return Object.hasOwn(METHOD_SCREEN, method);
}

/** A sign-in's way, in words (the workspaceSettingsDialog catalog). */
export function authAuditMethodText(t: TFunction, method: string): string {
  return t(knownMethod(method) ? `accountHistory.method.${method}` : "accountHistory.method.other");
}

/**
 * Why a sign-in didn't go in, as its screen said it then: by the answer's
 * code and status, and in the sign-in screen's own words for the two it says
 * itself (2026-10-26: both read "the cause couldn't be found" here). A record
 * from before statuses were kept goes by its code alone.
 */
export function authAuditReasonText(t: TFunction, method: string, code: string, status?: number | null): string {
  if (code === authErrorCodes.signupRestricted) return t("authGate:signupRestricted");
  if (code === authErrorCodes.passwordWait) return t("authGate:passwordWaitReason");
  return productErrorCause(code, knownMethod(method) ? METHOD_SCREEN[method] : "auth.signIn", status || null);
}

/**
 * What a sign-in record says happened: signed in, didn't, or — a right first
 * step that 2-step verification still asked its code for — went on to it
 * (2026-10-27: a right password that stopped at the code read "로그인했습니다").
 */
export function authAuditSignInText(t: TFunction, outcome: string, phase?: string): string {
  if (outcome !== "success") return t("accountHistory.signInFailed");
  return t(phase === "request" ? "accountHistory.signInContinued" : "accountHistory.signInSucceeded");
}

/** A sign-in record's step and result, in words. */
export function authAuditPhaseText(t: TFunction, phase: string): string {
  return phase === "request" || phase === "verify" ? t(`accountHistory.phase.${phase}`) : phase;
}

export function authAuditOutcomeText(t: TFunction, outcome: string): string {
  return outcome === "success" || outcome === "failure" ? t(`accountHistory.outcome.${outcome}`) : outcome;
}
