export type BinaryResponseSession = {
  token: string;
};

export type BinaryResponseErrorEnvelope = {
  error?: {
    code?: unknown;
    message?: unknown;
    reference?: unknown;
  };
};

export type AuthenticatedBinaryResponseOptions = {
  apiRoot: string;
  fetch?: typeof globalThis.fetch;
  /** Where a request's address goes: the active server, by default as is. */
  resolveUrl?: (url: string) => string;
  throwResponseError: (
    body: BinaryResponseErrorEnvelope | null,
    status: number,
  ) => never;
};

export function createAuthenticatedBinaryResponseRequest(
  options: AuthenticatedBinaryResponseOptions,
) {
  // Read when a request goes, so the page's bounds hold (lib/requestDeadline.ts).
  const fetchResponse = options.fetch ?? ((input: RequestInfo | URL, init?: RequestInit) => globalThis.fetch(input, init));
  const resolveUrl = options.resolveUrl ?? ((url: string) => url);
  return async (
    path: string,
    session: BinaryResponseSession,
    init: RequestInit = {},
  ): Promise<Response> => {
    if (Object.keys(init).length === 0) {
      const response = await fetchResponse(resolveUrl(`${options.apiRoot}${path}`), {
        headers: { authorization: `Bearer ${session.token}` },
      });
      if (!response.ok) {
        const body = await response.clone().json().catch(() => null) as
          BinaryResponseErrorEnvelope | null;
        options.throwResponseError(body, response.status);
      }
      return response;
    }
    const headers = new Headers(init.headers);
    headers.set("authorization", `Bearer ${session.token}`);
    const response = await fetchResponse(resolveUrl(`${options.apiRoot}${path}`), {
      ...init,
      headers,
    });
    if (!response.ok) {
      const body = await response.clone().json().catch(() => null) as
        BinaryResponseErrorEnvelope | null;
      options.throwResponseError(body, response.status);
    }
    return response;
  };
}
