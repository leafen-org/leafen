import { useSyncExternalStore } from "react";

// Whether an app has the whole window (AppHost's full window): Leafen keeps
// only the app's own bar then (docs/apps.md "The screen"), and the bands that
// only advise across the top of the app wait until it gives the window back
// (2026-11-15: a phone's "앱에서 열기" band stayed over a full window's bar).

let full = false;
const listeners = new Set<() => void>();

/** Said by the app's screen as its full window opens and closes. */
export function setAppFullWindow(next: boolean) {
  if (full === next) return;
  full = next;
  for (const listener of listeners) listener();
}

/** Whether an app has the whole window now. */
export function useAppFullWindow(): boolean {
  return useSyncExternalStore((listener) => {
    listeners.add(listener);
    return () => { listeners.delete(listener); };
  }, () => full, () => false);
}
