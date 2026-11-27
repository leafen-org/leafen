import { api } from "@/api";
import type {
  AccountLanguagePreference,
  AccountLanguageState,
  Session,
} from "@/authWire.generated";

export interface AccountLanguageService {
  read(signal?: AbortSignal): Promise<AccountLanguageState>;
  save(languagePreference: AccountLanguagePreference): Promise<AccountLanguageState>;
}

export function createAccountLanguageService(session: Session): AccountLanguageService {
  return Object.freeze({
    read: (signal?: AbortSignal) => api.readAccountLanguage(session, signal),
    save: (languagePreference: AccountLanguagePreference) => (
      api.saveAccountLanguage(session, languagePreference)
    ),
  });
}
