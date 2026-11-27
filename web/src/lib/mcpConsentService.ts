import { api } from "@/api";
import {
  mcpOAuthErrorCodes,
  type MCPAuthorizationConsent,
  type MCPAuthorizationRedirect,
} from "@/mcpOAuth";
import type { CurrentRequestGeneration } from "@/lib/readGeneration";
import type { Session } from "@/types";

export type MCPConsentTransport = typeof api.mcpAuthorizationConsent;

type MCPConsentActionOptions = {
  execute: (signal: AbortSignal) => Promise<MCPAuthorizationRedirect>;
  generation: CurrentRequestGeneration;
  navigate: (redirectUrl: string) => void;
  // A web agent waiting at the tunnel's address goes on from its own page.
  answeredRemotely?: () => void;
  publishFailure: (cause: unknown) => void;
};

export async function runMCPConsentAction({
  execute,
  generation,
  navigate,
  answeredRemotely,
  publishFailure,
}: MCPConsentActionOptions): Promise<"failed" | "navigated" | "obsolete"> {
  let result: MCPAuthorizationRedirect;
  try {
    result = await execute(generation.signal);
  } catch (cause) {
    if (!generation.isCurrent()) return "obsolete";
    publishFailure(cause);
    return "failed";
  }
  if (!generation.isCurrent()) return "obsolete";
  if (result.remote && answeredRemotely) answeredRemotely();
  else navigate(result.redirectUrl);
  return "navigated";
}

function isSnapshotConflict(cause: unknown) {
  return typeof cause === "object"
    && cause !== null
    && "code" in cause
    && cause.code === mcpOAuthErrorCodes.requestConflict;
}

export function createMCPConsentService(transport: MCPConsentTransport = api.mcpAuthorizationConsent) {
  const initial = (
    session: Session,
    authorizationRequest: string,
    signal?: AbortSignal,
  ) => transport(session, authorizationRequest, "", { signal });

  return {
    initial,
    async continueOrRestart(
      session: Session,
      authorizationRequest: string,
      cursor: string,
      membershipSnapshot: string,
      signal?: AbortSignal,
    ): Promise<{ consent: MCPAuthorizationConsent; restarted: boolean }> {
      try {
        const consent = await transport(session, authorizationRequest, cursor, { signal });
        if (consent.membershipSnapshot === membershipSnapshot) {
          return { consent, restarted: false };
        }
      } catch (cause) {
        if (!isSnapshotConflict(cause)) throw cause;
      }
      return {
        consent: await initial(session, authorizationRequest, signal),
        restarted: true,
      };
    },
  };
}

export const mcpConsentService = createMCPConsentService();
