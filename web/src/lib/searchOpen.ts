import { useSyncExternalStore } from "react";

// Whether search (⌘K) is open, kept here and not in the product's own state,
// so that opening and closing it redraws the search window alone. Held by the
// product, its press redrew the whole product — the sidebar, the page, its
// bar — on the press's frame: about 165 of the 300–380 ms a 44,000-page
// workspace's search took to show under the NAS condition (2026-11-14).
// What holds back while search is open — ⌘N's menu, ⌘/, the first run's
// steps, a phone's drawer as search closes — reads it here.

let open = false;
const listeners = new Set<() => void>();

export function openSearch() {
  setSearchOpen(true);
}

export function closeSearch() {
  setSearchOpen(false);
}

function setSearchOpen(next: boolean) {
  if (open === next) return;
  open = next;
  for (const listener of [...listeners]) listener();
}

/** Whether search is open now, for a key or a press to ask as it comes. */
export function searchIsOpen() {
  return open;
}

export function subscribeSearchOpen(listener: () => void) {
  listeners.add(listener);
  return () => { listeners.delete(listener); };
}

/** Whether search is open, drawn again as it opens and closes: for the part that changes with it alone. */
export function useSearchOpen() {
  return useSyncExternalStore(subscribeSearchOpen, searchIsOpen, () => false);
}
