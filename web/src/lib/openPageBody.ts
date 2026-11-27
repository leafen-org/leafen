import { useEffect, useRef, useSyncExternalStore } from "react";
import type * as Y from "yjs";
import { emptyContentGraph, type ContentGraph } from "@/contentGraph";
import { afterNextPaint } from "@/lib/afterPress";
import { collaborationState, type CollaborationState } from "@/lib/collaborationStatus";

/**
 * What the product around the open page reads of its body, whichever screen
 * draws it (lib/pageDrawnAsRow.ts): its room's state for the sync badge and
 * its storage notices, what the reader may do there, whether it is still
 * being opened (the first screen's cover) and holds anything (its row in the
 * page list), and, read when asked, its blocks and text (the search's lines
 * of the open page).
 */
export type OpenPageBody = {
  state: CollaborationState;
  canEdit: boolean;
  canSuggest: boolean;
  /** Whether it is still being read or opened. */
  pending: boolean;
  /** Whether it holds any block, once read. */
  hasContent: boolean | undefined;
  read(): { graph: ContentGraph; document: Y.Doc | null };
};

// The bodies its database's page draws whole — a wiki's page, a database's
// page at its own address — told by each (RowPageBodyEditor), which opened the
// page's one room: the product edits no body of its own for them. One left
// stays drawn until the next screen is painted (lib/pageLeftAfterPaint.ts).
const told = new Map<string, OpenPageBody>();
const listeners = new Set<() => void>();
const subscribe = (listener: () => void) => {
  listeners.add(listener);
  return () => { listeners.delete(listener); };
};
// What a body tells is taken in once the frame it was drawn in is painted:
// what the product reads of it can wait, and the product drawn again in that
// frame held a wiki page's ⤢ 8–24 ms longer (2026-11-18, a NAS-class
// server, CPU ×2).
const changes: (() => void)[] = [];
const later = (change: () => void) => {
  changes.push(change);
  if (changes.length > 1) return;
  afterNextPaint(() => {
    for (const made of changes.splice(0)) made();
    for (const listener of [...listeners]) listener();
  });
};

/** Tells the product the body a database's page draws whole of `pageId` (null: none). */
export function useTellRowPageBody(pageId: string | null, body: Omit<OpenPageBody, "read">, read: OpenPageBody["read"]) {
  const reading = useRef(read);
  reading.current = read;
  const { state: { status, diagnosticReference }, canEdit, canSuggest, pending, hasContent } = body;
  useEffect(() => {
    if (!pageId) return;
    const shown: OpenPageBody = { state: { status, diagnosticReference }, canEdit, canSuggest, pending, hasContent, read: () => reading.current() };
    later(() => told.set(pageId, shown));
    return () => later(() => {
      if (told.get(pageId) === shown) told.delete(pageId);
    });
  }, [pageId, status, diagnosticReference, canEdit, canSuggest, pending, hasContent]);
}

const noBody = { state: collaborationState("idle"), canEdit: true, canSuggest: false, hasContent: undefined,
  read: () => ({ graph: emptyContentGraph, document: null }) };

/**
 * The open page's body: the product's own (`own`) where its page view draws
 * it, else the one its database's page draws of the page at the address
 * (`pageId`), pending until that body tells it.
 */
export function useOpenPageBody(own: OpenPageBody | null, pageId: string | null): OpenPageBody {
  const row = useSyncExternalStore(subscribe, () => (!own && pageId ? told.get(pageId) ?? null : null), () => null);
  return own ?? row ?? { ...noBody, pending: pageId !== null };
}
