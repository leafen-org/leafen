import { LeafenApiError } from "../../apiTransport";
import { administratorErrorCodes } from "../../administratorWire.generated";
import { userFailureText } from "@/lib/settingsErrors";

// A refused or unreachable request reads as the failed action, with the
// diagnostic reference only of a failure no one foresaw.
export function accountError(cause: unknown, fallback: string, referenceLabel: string) {
  return userFailureText(cause, fallback, referenceLabel);
}

/**
 * The server refused a change made to an account as this screen last read
 * it: another administrator, an agent or a step it left changed the account
 * meanwhile (2026-11-23). The screen reads the list again and says so,
 * rather than that the action failed.
 */
export function accountChangedMeanwhile(cause: unknown) {
  return cause instanceof LeafenApiError && cause.code === administratorErrorCodes.administratorAccountRevisionConflict;
}
