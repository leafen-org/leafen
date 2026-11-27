// Whether the app, started at its root, is still choosing the page it opens
// with (components/product/useStartPage.ts): the first screen stays covered
// meanwhile, so Home doesn't show for a moment and then give way to the
// page (2026-11-06: with other tabs open, Home showed for seconds and the
// last page took its place under the pointer; Notion's opens on the page).

import { useSyncExternalStore } from "react";

let choosing = false;
const listeners = new Set<() => void>();

export function setStartChoosing(next: boolean) {
  if (choosing === next) return;
  choosing = next;
  for (const listener of [...listeners]) listener();
}

export function useStartChoosing() {
  return useSyncExternalStore(
    (listener) => { listeners.add(listener); return () => { listeners.delete(listener); }; },
    () => choosing,
    () => false,
  );
}
