import type {
  AccountLanguageState,
  AccountPasswordState,
  Session,
} from "@/authWire.generated";

export interface AuthenticatedAccountGateTransport {
  readPasswordState(session: Session, signal?: AbortSignal): Promise<AccountPasswordState>;
  readLanguageState(session: Session, signal?: AbortSignal): Promise<AccountLanguageState>;
}

export interface AuthenticatedAccountGateState {
  mustChangePassword: boolean;
  language: AccountLanguageState | null;
}

export async function readAuthenticatedAccountGateState(
  transport: AuthenticatedAccountGateTransport,
  session: Session,
  signal?: AbortSignal,
): Promise<AuthenticatedAccountGateState> {
  const [password, language] = await Promise.allSettled([
    transport.readPasswordState(session, signal),
    transport.readLanguageState(session, signal),
  ]);
  return {
    mustChangePassword: password.status === "fulfilled"
      ? password.value.mustChangePassword
      : false,
    language: language.status === "fulfilled" ? language.value : null,
  };
}
