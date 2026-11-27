import { authErrorCodes } from "@/authWire.generated";
import { productErrorMessage } from "@/lib/productErrorPresentation";

const ERROR_KEYS: Record<string, string> = {
  [authErrorCodes.accountExists]: "managedUserEmailTaken",
  [authErrorCodes.accountEmailManaged]: "emailManaged",
  [authErrorCodes.accountEmailLimit]: "emailLimit",
  [authErrorCodes.accountEmailPrimary]: "emailUpdateFailed",
  [authErrorCodes.accountEmailChallengeInvalid]: "emailCodeInvalid",
  [authErrorCodes.accountEmailChanged]: "emailChanged",
  [authErrorCodes.accountEmailMissing]: "emailMissing",
  [authErrorCodes.mfaCodeInvalid]: "codeOrPasswordMismatch",
  [authErrorCodes.emailDeliveryFailed]: "emailSendFailed",
  LEAFEN_MANAGED_USER_EMAIL_INVALID: "managedUserEmailInvalid",
  LEAFEN_MANAGED_USER_EMAIL_TAKEN: "managedUserEmailTaken",
};

/** Expected email decisions share copy; storage/transport failures keep the
 * product's diagnostic reference and recovery guidance. */
export function accountEmailErrorMessage(failure: unknown, text: (key: string) => string,
  context: "accountEmails.read" | "accountEmails.update" = "accountEmails.update") {
  const code = (failure as { code?: string })?.code;
  const key = code && ERROR_KEYS[code];
  return key ? text(key) : productErrorMessage(failure, context);
}

/**
 * Whether a refusal says the email settings shown are older than the
 * server's — changed elsewhere since they were read, or naming an address
 * taken away there (2026-11-14: its removal on another device left it listed
 * here, and removing it said a code was wrong): the list is read again, and
 * the refusal says what happened.
 */
export function accountEmailsShownBehind(failure: unknown) {
  const code = (failure as { code?: string })?.code;
  return code === authErrorCodes.accountEmailChanged || code === authErrorCodes.accountEmailMissing;
}

/** Whether a refusal says the address named is no longer the account's. */
export function accountEmailGone(failure: unknown) {
  return (failure as { code?: string })?.code === authErrorCodes.accountEmailMissing;
}
