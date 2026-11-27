import * as Y from "yjs";
import type { Session } from "@/types";
import { api } from "@/api";
import { bytesToBase64 } from "@/lib/collaborationCrdtCodec";

// The copies of a page's text open here, for what the server reads as stored,
// with the person each is open for.
const open = new Map<string, Set<Y.Doc>>();
const openFor = new WeakMap<Y.Doc, string>();

export function notePageTextDocument(pageId: string, document: Y.Doc, principalId: string) {
  const documents = open.get(pageId) ?? new Set<Y.Doc>();
  documents.add(document);
  open.set(pageId, documents);
  openFor.set(document, principalId);
  return () => {
    documents.delete(document);
    if (!documents.size && open.get(pageId) === documents) open.delete(pageId);
  };
}

/** The copy of the page's text opened here last and still open, and whose it is. */
export function pageTextShown(pageId: string): { document: Y.Doc; principalId: string } | null {
  const document = [...open.get(pageId) ?? []].pop();
  const principalId = document && openFor.get(document);
  return document && principalId ? { document, principalId } : null;
}

/** Whether a copy of the page's text is open here, whose words may not be stored yet. */
export function pageTextOpenHere(pageId: string) {
  return open.has(pageId);
}

/**
 * Waits for the server to have stored what this device's copy of the page
 * holds, for what the server does with the page's text next: it moves or copies
 * what it has stored (2026-11-05: a line moved a second after typing it lost the
 * words). False when it isn't there in time.
 */
export async function pageTextStored(session: Session, pageId: string, signal?: AbortSignal) {
  for (const document of open.get(pageId) ?? []) {
    const state = bytesToBase64(Y.encodeSnapshot(Y.snapshot(document)));
    if (!(await api.waitForPageTextStored(session, pageId, state, signal)).stored) return false;
  }
  return true;
}
