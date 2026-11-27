// The consents a signed-in person answers outside the workspace, named in
// the address: an MCP client that starts the standard sign-in on its own, or
// a Notion-compatible integration, asking to connect. Leafen hands out no
// link to them. An agent's action in Leafen, a connected service's tool and
// an app's tool ask inside the agent's conversation instead (person_ask.go on
// the server).
// Signing in keeps them, so the person lands on the question.

export type StandaloneConsent = { kind: "mcp" | "notion"; value: string };

const CONSENT_PARAMS = { mcp: "mcpAuthorization", notion: "notionAuthorization" } as const;

/** The consent the address asks for, if any. */
export function standaloneConsent(href: string): StandaloneConsent | null {
  const params = new URL(href).searchParams;
  for (const kind of ["mcp", "notion"] as const) {
    const value = params.get(CONSENT_PARAMS[kind]);
    if (value) return { kind, value };
  }
  return null;
}

/** The address that keeps the consent through signing in. */
export function standaloneConsentPath(href: string): string | null {
  const consent = standaloneConsent(href);
  return consent ? `/?${CONSENT_PARAMS[consent.kind]}=${encodeURIComponent(consent.value)}` : null;
}
