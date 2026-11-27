import { api } from "@/api";
import type { AuthMFAChallenge, AuthProviderList, Session } from "@/authWire.generated";

export interface AuthProviderOption {
  provider: string;
  label: string;
}

interface AuthProviderTransport {
  list(): Promise<AuthProviderList>;
  startURL(provider: string): string;
  exchange(code: string, verifier?: string): Promise<Session | AuthMFAChallenge>;
}

const providerID = /^[a-z][a-z0-9_-]{0,31}$/;

export function createAuthProviderService(
  transport: AuthProviderTransport,
  navigate: (url: string) => void = (url) => window.location.assign(url),
) {
  return {
    async listOptions(): Promise<AuthProviderOption[]> {
      const result = await transport.list();
      return result.providers.flatMap(({ id, name }) => {
        const provider = id.trim();
        if (!providerID.test(provider)) return [];
        return [{ provider, label: name.trim() || provider }];
      });
    },
    start(provider: string): void {
      navigate(transport.startURL(provider));
    },
    /** The provider's sign-in address on the server, for the packaged app's own sign-in. */
    startURL(provider: string): string {
      return transport.startURL(provider);
    },
    exchange(code: string, verifier?: string): Promise<Session | AuthMFAChallenge> {
      return transport.exchange(code, verifier);
    },
  };
}

export const authProviderService = createAuthProviderService({
  list: () => api.authProviders(),
  startURL: (provider) => api.authProviderStartURL(provider),
  exchange: (code, verifier) => api.exchangeAuthProvider(code, verifier),
});
