import { api } from "@/api";
import { graphWireErrorCodes } from "@/contentGraphWire.generated";
import type { Session } from "@/types";

// A page's routes refuse a change whose ID they know under another body
// (page_metadata.go, page_mutation.go); the content graph's has its own code.
const PAGE_MUTATION_ID_REUSED = "LEAFEN_MUTATION_ID_REUSED";

/**
 * Whether a refusal of a change this device kept says the server took it
 * already — the one rule for every kept change sent again under its own ID.
 * Its ID is made for that change alone when it is kept, so a server that knows
 * the ID under another body holds the change: it went once, its answer lost
 * to a reload, a closed tab or the network, and it was sent again on the page
 * as it now stands, its base read again (2026-11-18: a database start;
 * 2026-11-22: a new page's title said "deleted" though it was saved).
 */
export function keptChangeTaken(error: unknown): boolean {
  const code = error && typeof error === "object" ? Reflect.get(error, "code") : undefined;
  return code === PAGE_MUTATION_ID_REUSED || code === graphWireErrorCodes.mutationIdReused;
}

/** A page change the server took already, answered as its route answers one sent again: the page as it is now. */
export async function takenPageChange(session: Session, pageId: string, mutationId: string, signal?: AbortSignal) {
  const page = await api.readPage(session, pageId, signal);
  return { page, receipt: { mutationId, cursor: page.cursor, duplicate: true } };
}

/**
 * Sends a page's text (the outbox's transport for it): a text sent again on
 * another base cursor than its first send went on — the texts of a page go
 * on the cursor the last one answered (nativePageTextChain.ts), which a new
 * document doesn't know — and taken already, is answered as taken.
 */
export async function sendPageText(session: Session, input: { baseCursor: number; content: string; mutationId: string; pageId: string },
  signal?: AbortSignal) {
  try {
    return await api.updatePage(session, input.pageId, input.content, input.mutationId, input.baseCursor, signal);
  } catch (error) {
    if (keptChangeTaken(error)) return takenPageChange(session, input.pageId, input.mutationId, signal);
    throw error;
  }
}
