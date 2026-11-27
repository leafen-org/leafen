import { LeafenTransportError } from "../apiTransport";
import { i18next } from "@/i18n";
import { workspaceMembershipErrorCodes } from "@/workspaceMembershipWire.generated";
import { isPageBusyRefusal } from "./pageBusy";
import { productErrorMessage, productErrorPresentation, type ProductErrorContext } from "./productErrorPresentation";

/**
 * Whether a write that failed waits in the outbox for the server, the one
 * rule for every kind kept there (a page's structure, text, settings, a page
 * made here; a page's text since 2026-11-13, when one whose page was gone
 * waited for good under "connection lost"): when
 * it couldn't be reached, was busy or unavailable (502–504: its store locked,
 * a restart, a proxy), or signed the reader out, or the server was restored
 * from a backup since this device's copies (the write stays here until its
 * person lets the copies go — lib/dataEpoch.ts), or the page is held by an
 * operation for now (isPageBusyRefusal — it takes the write once done).
 * Any other refusal is final, and the write is dropped and told — a page's
 * or a database's lock's 423 too, which stands until someone unlocks it (a
 * write kept while away from a page locked meanwhile was sent again every
 * two seconds, unsaid, and every change kept after it waited with it); a 500 too:
 * the server took that write and failed on it, and would again, while the
 * writes kept behind it waited with it (2026-11-07: one image resize the
 * server failed on held every later change on the device, a new page's
 * title among them, under "connection lost").
 */
export function graphWriteWaitsForServer(error: unknown): boolean {
  if (error instanceof LeafenTransportError) return true;
  const status = error && typeof error === "object" ? Reflect.get(error, "status") : undefined;
  if (typeof status !== "number") return true;
  if (status === 409 && Reflect.get(error as object, "code") === workspaceMembershipErrorCodes.dataEpochChanged) return true;
  return status === 401 || status === 408 || isPageBusyRefusal(error) || status === 429 || status >= 502;
}

/**
 * What is said of a change kept on this device that the server refused for
 * good, dropped from it with no writer there to hear: what the outbox did and
 * why the server refused (2026-11-13: one whose page was gone kept saying
 * "connection lost" instead).
 */
export function keptChangeRefusedMessage(error: unknown): string {
  return `${i18next.t("syncStatusBadge:keptChangeRefused")} ${productErrorPresentation(error, "contentGraph.update").cause}`;
}

/** What is said of a page's change that failed, in the words of what it was doing (`context`). */
export function nativePageMutationFailure(
  error: unknown,
  context: ProductErrorContext,
): string {
  return productErrorMessage(error, context);
}
