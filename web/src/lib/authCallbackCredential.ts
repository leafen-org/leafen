export type AuthCallbackCredentialName = "code" | "token" | "samlCompletion";

export interface AuthCallbackUrlAdapter {
  readonly href: string;
  replace(relativeUrl: string): void;
}

const MAX_AUTH_CALLBACK_CREDENTIAL_LENGTH = 8_192;

function browserAdapter(): AuthCallbackUrlAdapter {
  return {
    get href() {
      return typeof window === "undefined" ? "" : window.location.href;
    },
    replace(relativeUrl) {
      if (typeof window === "undefined") return;
      window.history.replaceState(window.history.state, "", relativeUrl);
    },
  };
}

export function consumeAuthCallbackCredential(
  name: AuthCallbackCredentialName,
  adapter: AuthCallbackUrlAdapter = browserAdapter(),
): string {
  if (name !== "code" && name !== "token" && name !== "samlCompletion") return "";

  let url: URL;
  try {
    url = new URL(adapter.href);
  } catch {
    return "";
  }

  const fragment = url.hash.startsWith("#") ? url.hash.slice(1) : url.hash;
  const fragmentParams = new URLSearchParams(fragment);
  const queryValues = url.searchParams.getAll(name);
  const fragmentValues = fragmentParams.getAll(name);
  if (queryValues.length === 0 && fragmentValues.length === 0) return "";

  const credential = fragmentValues[0] ?? queryValues[0] ?? "";
  url.searchParams.delete(name);
  if (fragmentValues.length > 0) {
    fragmentParams.delete(name);
    const nextFragment = fragmentParams.toString();
    url.hash = nextFragment ? `#${nextFragment}` : "";
  }

  try {
    adapter.replace(`${url.pathname}${url.search}${url.hash}`);
  } catch {
    return "";
  }
  return credential.length <= MAX_AUTH_CALLBACK_CREDENTIAL_LENGTH ? credential : "";
}
