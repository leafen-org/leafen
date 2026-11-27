import { api } from "@/api";
import type { PasskeyLoginFinishInput, PasskeyRegistrationFinishInput } from "@/authWire.generated";
import { pkcePair } from "./appSignIn";

export function browserPasskeysAvailable() {
  return window.isSecureContext && typeof PublicKeyCredential !== "undefined" && Boolean(navigator.credentials);
}

export class PasskeyPromptCancelled extends Error {}
export class PasskeyDeviceUnsupported extends Error {}

/** Why the browser's passkey prompt ended without a passkey, as passkeyErrorMessage names it. */
export type PasskeyPromptEnd = "notAllowed" | "alreadyOnDevice" | "address" | "authenticator" | "deviceFailed";
export class PasskeyPromptRefused extends Error {
  constructor(readonly end: PasskeyPromptEnd) {
    super(end);
  }
}

/**
 * The one place a WebAuthn error becomes its cause (WebAuthn Level 3 §5.1.3,
 * §5.1.4): our own abort is a cancel; NotAllowedError is the person closing
 * the browser's prompt or its time running out, which the browser doesn't
 * tell apart; InvalidStateError, only on adding, a device that already
 * holds one of this account's passkeys (the server lists them to exclude);
 * SecurityError an address this server's passkeys aren't for; ConstraintError
 * a device or key that can't keep a passkey as asked; NotSupportedError a
 * browser or device with no passkey it can make; UnknownError the device
 * failing. Anything else is not the prompt's and goes on as it came.
 */
function promptEnd(error: unknown, adding: boolean): Error | null {
  if (!(error instanceof DOMException)) return null;
  switch (error.name) {
    case "AbortError": return new PasskeyPromptCancelled();
    case "NotAllowedError": return new PasskeyPromptRefused("notAllowed");
    case "InvalidStateError": return new PasskeyPromptRefused(adding ? "alreadyOnDevice" : "deviceFailed");
    case "SecurityError": return new PasskeyPromptRefused("address");
    case "ConstraintError": return new PasskeyPromptRefused("authenticator");
    case "NotSupportedError": return new PasskeyDeviceUnsupported();
    case "UnknownError": return new PasskeyPromptRefused("deviceFailed");
    default: return null;
  }
}

function buffer(value: string): ArrayBuffer {
  const raw = atob(value.replaceAll("-", "+").replaceAll("_", "/"));
  return Uint8Array.from(raw, (character) => character.charCodeAt(0)).buffer;
}

function encoded(value: ArrayBuffer): string {
  return btoa(String.fromCharCode(...new Uint8Array(value))).replaceAll("+", "-").replaceAll("/", "_").replace(/=+$/u, "");
}

function creationOptions(json: Record<string, unknown>): PublicKeyCredentialCreationOptions {
  if (typeof PublicKeyCredential.parseCreationOptionsFromJSON === "function") {
    return PublicKeyCredential.parseCreationOptionsFromJSON(json as unknown as PublicKeyCredentialCreationOptionsJSON);
  }
  const options = json as unknown as PublicKeyCredentialCreationOptionsJSON;
  return { ...options, challenge: buffer(options.challenge), user: { ...options.user, id: buffer(options.user.id) },
    excludeCredentials: options.excludeCredentials?.map((item) => ({ ...item, type: "public-key", id: buffer(item.id), transports: item.transports as AuthenticatorTransport[] })) } as unknown as PublicKeyCredentialCreationOptions;
}

function requestOptions(json: Record<string, unknown>): PublicKeyCredentialRequestOptions {
  if (typeof PublicKeyCredential.parseRequestOptionsFromJSON === "function") {
    return PublicKeyCredential.parseRequestOptionsFromJSON(json as unknown as PublicKeyCredentialRequestOptionsJSON);
  }
  const options = json as unknown as PublicKeyCredentialRequestOptionsJSON;
  return { ...options, challenge: buffer(options.challenge), allowCredentials: options.allowCredentials?.map((item) => ({ ...item, type: "public-key", id: buffer(item.id), transports: item.transports as AuthenticatorTransport[] })) } as unknown as PublicKeyCredentialRequestOptions;
}

function credentialJSON(value: PublicKeyCredential): Record<string, unknown> {
  if (typeof value.toJSON === "function") return value.toJSON() as unknown as Record<string, unknown>;
  const response = value.response;
  const serialized: Record<string, unknown> = { clientDataJSON: encoded(response.clientDataJSON) };
  if (response instanceof AuthenticatorAttestationResponse) {
    serialized.attestationObject = encoded(response.attestationObject);
    serialized.transports = response.getTransports?.() ?? [];
  } else {
    const assertion = response as AuthenticatorAssertionResponse;
    serialized.authenticatorData = encoded(assertion.authenticatorData);
    serialized.signature = encoded(assertion.signature);
    serialized.userHandle = assertion.userHandle ? encoded(assertion.userHandle) : null;
  }
  return { id: value.id, rawId: encoded(value.rawId), type: value.type, response: serialized,
    authenticatorAttachment: value.authenticatorAttachment, clientExtensionResults: value.getClientExtensionResults() };
}

async function prompt(action: () => Promise<Credential | null>, adding: boolean) {
  if (!browserPasskeysAvailable()) throw new PasskeyDeviceUnsupported();
  try {
    const result = await action();
    if (!(result instanceof PublicKeyCredential)) throw new PasskeyPromptCancelled();
    return credentialJSON(result);
  } catch (error) {
    throw promptEnd(error, adding) ?? error;
  }
}

export async function preparePasskeyRegistration(ticket: string, options: Record<string, unknown>, signal: AbortSignal): Promise<PasskeyRegistrationFinishInput> {
  return { ticket, credential: await prompt(() => navigator.credentials.create({ publicKey: creationOptions(options), signal }), true) };
}

export async function preparePasskeyLogin(signal: AbortSignal, appReturn = "", appChallenge = ""): Promise<PasskeyLoginFinishInput> {
  const { verifier, challenge } = await pkcePair();
  const begun = await api.beginPasskeyLogin({ challenge, appReturn, appChallenge }, signal);
  const credential = await prompt(() => navigator.credentials.get({ publicKey: requestOptions(begun.options), signal }), false);
  return { ticket: begun.ticket, credential, verifier };
}
