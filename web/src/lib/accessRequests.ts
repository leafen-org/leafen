import { api, LeafenApiError } from "@/api";
import { accessRequestErrorCodes, type AccessRequestPageInput } from "@/accessRequestWire.generated";
import type { Session } from "@/types";

/** How asking for something went, as the screen tells it. */
export type AccessRequestOutcome = "requested" | "not-allowed" | "not-found" | "failed";

function accessRequestOutcomeOf(error: unknown): AccessRequestOutcome {
  if (!(error instanceof LeafenApiError)) return "failed";
  if (error.code === accessRequestErrorCodes.notAllowed) return "not-allowed";
  if (error.code === accessRequestErrorCodes.notFound) return "not-found";
  return "failed";
}

/** Asks the people with full access to a page for access to it. */
export async function requestPageAccess(
  session: Session, pageId: string, role: AccessRequestPageInput["role"] = "view",
): Promise<AccessRequestOutcome> {
  try {
    await api.requestPageAccess(session, pageId, role);
    return "requested";
  } catch (error) {
    return accessRequestOutcomeOf(error);
  }
}
