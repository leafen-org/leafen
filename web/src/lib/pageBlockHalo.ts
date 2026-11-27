import { useSyncExternalStore } from "react";

/**
 * The page whose page block, in a page open in the body, shows the
 * selection's wash while a menu elsewhere works on that page: a sidebar
 * row's "…" or right-click menu, as Notion's (seen 2026-11-11 and
 * 2026-11-13: the parent open, a child's row menu washes the child's page
 * block in its body, rgba(35,131,226,.14) 2px in, gone as the menu closes).
 */
let haloPageId: string | null = null;
const listeners = new Set<() => void>();

function subscribe(listener: () => void) {
  listeners.add(listener);
  return () => { listeners.delete(listener); };
}

function set(pageId: string | null) {
  if (haloPageId === pageId) return;
  haloPageId = pageId;
  listeners.forEach((listener) => listener());
}

export function showPageBlockHalo(pageId: string) {
  set(pageId);
}

/** Takes the wash away, unless another page's menu put its own since. */
export function hidePageBlockHalo(pageId: string) {
  if (haloPageId === pageId) set(null);
}

/** Whether the page block of this page shows the wash now. */
export function usePageBlockHalo(pageId: string) {
  return useSyncExternalStore(subscribe, () => haloPageId === pageId, () => false);
}
