import { request } from "./apiTransport";
import type { Session } from "./types";
import type {
  AccountPasskeyState, PasskeyCapability, PasskeyCeremony, PasskeyLoginBeginInput,
  PasskeyLoginFinishInput, PasskeyLoginResult, PasskeyRegistrationBeginInput,
  PasskeyRegistrationFinishInput, PasskeyRemoveInput,
} from "./authWire.generated";

export function createPasskeyApi() {
  return {
    passkeyCapability: (signal?: AbortSignal) => request<PasskeyCapability>("/auth/passkeys", { signal }),
    beginPasskeyLogin: (input: PasskeyLoginBeginInput, signal?: AbortSignal) => request<PasskeyCeremony>(
      "/auth/passkeys/begin", { method: "POST", body: JSON.stringify(input), signal },
    ),
    finishPasskeyLogin: (input: PasskeyLoginFinishInput, signal?: AbortSignal) => request<PasskeyLoginResult>(
      "/auth/passkeys/finish", { method: "POST", body: JSON.stringify(input), signal },
    ),
    exchangePasskeyApp: (code: string, verifier: string) => request<Session>(
      "/auth/passkeys/app/exchange", { method: "POST", body: JSON.stringify({ code, verifier }) },
    ),
    passkeyManageReturn: (appReturn: string, appChallenge: string) => request<PasskeyLoginResult>(
      `/auth/passkeys/app/manage-return?${new URLSearchParams({ app_return: appReturn, app_challenge: appChallenge })}`,
    ),
    readAccountPasskeys: (session: Session, signal?: AbortSignal) => request<AccountPasskeyState>("/account/passkeys", { signal }, session),
    beginPasskeyRegistration: (session: Session, input: PasskeyRegistrationBeginInput, signal?: AbortSignal) => request<PasskeyCeremony>(
      "/account/passkeys/begin", { method: "POST", body: JSON.stringify(input), signal }, session,
    ),
    finishPasskeyRegistration: (session: Session, input: PasskeyRegistrationFinishInput, signal?: AbortSignal) => request<AccountPasskeyState>(
      "/account/passkeys/finish", { method: "POST", body: JSON.stringify(input), signal }, session,
    ),
    removeAccountPasskey: (session: Session, input: PasskeyRemoveInput, signal?: AbortSignal) => request<AccountPasskeyState>(
      "/account/passkeys", { method: "DELETE", body: JSON.stringify(input), signal }, session,
    ),
  };
}
