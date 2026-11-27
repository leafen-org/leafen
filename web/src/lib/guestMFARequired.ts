import { LeafenApiError } from "@/apiTransport";

/**
 * A request refused because the organization asks its guests for 2-step
 * verification first. The page shows its verification screen for it, so
 * the page's other readers — its comments, the inbox — say nothing more.
 */
export const GUEST_MFA_REQUIRED_CODE = "LEAFEN_GUEST_MFA_REQUIRED";

export function isGuestMFARequired(error: unknown) {
  return error instanceof LeafenApiError && error.code === GUEST_MFA_REQUIRED_CODE;
}
