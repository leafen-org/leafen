import { useCallback, useEffect, useSyncExternalStore } from "react";
import type * as Y from "yjs";

/*
 * The text of the pages open on this screen (each open page's shared
 * document, collaboration.ts), by page: what reads a page while it is open
 * here — a mention's card (editor/PageMentionPreview.tsx) — draws its words
 * from it, as they are written, rather than from the server, which keeps
 * them a moment later.
 */
const held = new Map<string, Y.Doc[]>();
const listeners = new Set<() => void>();
const tell = () => { for (const listener of [...listeners]) listener(); };

/** Holds `document` as the text of `pageId` open here while the caller lives. */
export function useHoldPageTextHere(pageId: string | null, document: Y.Doc | null) {
  useEffect(() => {
    if (!pageId || !document) return undefined;
    held.set(pageId, [...held.get(pageId) ?? [], document]);
    tell();
    return () => {
      const left = (held.get(pageId) ?? []).filter((one) => one !== document);
      if (left.length) held.set(pageId, left); else held.delete(pageId);
      tell();
    };
  }, [pageId, document]);
}

function subscribe(listener: () => void) {
  listeners.add(listener);
  return () => { listeners.delete(listener); };
}

/**
 * The text of `pageId` where it is open here (the latest opened), null where
 * it isn't, and how many times it changed since: the caller draws again as
 * it is written, here or by anyone else in the page.
 */
export function usePageTextHere(pageId: string | null): { document: Y.Doc | null; changes: number } {
  const document = useSyncExternalStore(subscribe, () => (pageId ? held.get(pageId)?.at(-1) ?? null : null), () => null);
  const followText = useCallback((notify: () => void) => {
    if (!document) return () => undefined;
    updatesOf(document);
    document.on("update", notify);
    return () => document.off("update", notify);
  }, [document]);
  const changes = useSyncExternalStore(followText, () => (document ? updatesOf(document) : 0), () => 0);
  return { document, changes };
}

// Each document's updates counted as they come, read as a number that changes with them.
const counts = new WeakMap<Y.Doc, { count: number }>();
function updatesOf(document: Y.Doc) {
  let entry = counts.get(document);
  if (!entry) {
    const counted = { count: 0 };
    document.on("update", () => { counted.count += 1; });
    counts.set(document, entry = counted);
  }
  return entry.count;
}
