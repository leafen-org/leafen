import type { GraphOperation } from "@/contentGraph";
import { graphWireErrorCodes } from "@/contentGraphWire.generated";
import { enterpriseDLPErrorCodes } from "@/enterpriseDLPWire.generated";
import { authenticatedProductErrorMessage } from "@/lib/authenticatedProductErrors";
import type { ProductStatusSink } from "@/lib/productStatus";
import { propertyWriteRefusalReason } from "@/lib/propertyWriteRefusal";

// Pages whose block change was refused for the text written with it, and
// when: a change made on top of it (a line split from the refused one) is
// refused next for what it builds on, the same refusal again.
const textRefusedAt = new Map<string, number>();
const FOLLOWING_REFUSAL_MS = 10_000;

/**
 * Says why the server refused a change of a page's content, unless another
 * says it: a block change the organization's DLP policy refused for the text
 * written with it (its words) is said by the page's room, which sends that
 * text next and is refused it, offering what was written to copy
 * (onDLPRefusedEdits), and so are the changes refused after it for what they
 * built on it — said here too, one refusal showed two notices (2026-11-15).
 * A property change says why (propertyWriteRefusal.ts).
 */
export function sayGraphChangeRefusal(say: ProductStatusSink, error: unknown, operations?: readonly GraphOperation[], wordsRode = false, pageId = "") {
  const code = (error as { code?: unknown } | null)?.code;
  if (wordsRode && code === enterpriseDLPErrorCodes.policyBlocked) {
    if (pageId) textRefusedAt.set(pageId, performance.now());
    return;
  }
  const refusedAt = textRefusedAt.get(pageId);
  if (refusedAt !== undefined && code === graphWireErrorCodes.referenceInvalid && performance.now() - refusedAt < FOLLOWING_REFUSAL_MS) return;
  say(propertyWriteRefusalReason(error, operations) || authenticatedProductErrorMessage(error, "contentGraphUpdate"), "error");
}
