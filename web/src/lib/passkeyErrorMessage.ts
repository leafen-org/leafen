import { authErrorCodes } from "@/authWire.generated";
import { productErrorMessage, type ProductErrorContext } from "./productErrorPresentation";
import { productErrorLine } from "./authErrorPresentation";
import { authWords, type AuthLine } from "./authLine";
import { PasskeyDeviceUnsupported, PasskeyPromptCancelled, PasskeyPromptRefused, type PasskeyPromptEnd } from "./passkeys";

/** Each way the browser's prompt ends without a passkey, in Leafen's words (passkeys.ts promptEnd). */
const promptEnds: Record<PasskeyPromptEnd, string> = {
  notAllowed: "passkeyNotAllowed",
  alreadyOnDevice: "passkeyAlreadyOnDevice",
  address: "passkeyWrongAddress",
  authenticator: "passkeyAuthenticatorUnsupported",
  deviceFailed: "passkeyDeviceFailed",
};

type PasskeyErrorContext = Extract<ProductErrorContext, "accountPasskeys.read" | "accountPasskeys.update" | "auth.passkey">;

export function passkeyErrorMessage(error: unknown, text: (key: string) => string, context: PasskeyErrorContext) {
  const key = passkeyErrorKey(error);
  return key ? text(key) : productErrorMessage(error, context);
}

/** The same held as what to say, for the sign-in screen (lib/authLine.ts). */
export function passkeyErrorLine(error: unknown, context: PasskeyErrorContext): AuthLine {
  const key = passkeyErrorKey(error);
  return key ? authWords(`workspaceSettingsDialog:${key}`) : productErrorLine(error, context);
}

/** The passkey's own words for how it ended (the workspaceSettingsDialog catalog's key), or "" where the product's error says it. */
function passkeyErrorKey(error: unknown): string {
  if (error instanceof PasskeyPromptCancelled) return "passkeyCancelled";
  if (error instanceof PasskeyDeviceUnsupported) return "passkeyDeviceUnsupported";
  if (error instanceof PasskeyPromptRefused) return promptEnds[error.end];
  const code = (error as { code?: string })?.code;
  const keys: Record<string, string> = {
    [authErrorCodes.passkeyUnavailable]: "passkeyUnavailable",
    [authErrorCodes.passkeyLimit]: "passkeyLimit",
    [authErrorCodes.passkeyInvalid]: "passkeyStartAgain",
    [authErrorCodes.passkeyConflict]: "passkeyRefresh",
    LEAFEN_SAML_REQUIRED: "passkeySSO",
  };
  return (code && keys[code]) || "";
}
