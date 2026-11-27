import { useSyncExternalStore } from "react";

/** Where a new page goes from the add window: a teamspace's top, or the private pages'. */
export type AddPagePlace = { kind: "teamspace"; teamspaceId: string } | { kind: "private" };

/** The add window as something opened it: where, whether it adds a page or a database, and the name typed in it before. */
export type AddPageRequest = { place: AddPagePlace; mode: "page" | "database"; title?: string };

// One window for the workspace, opened from the sidebar, the library and
// the sidebar's "새로 만들기"; the sidebar draws it.
let request: AddPageRequest | null = null;
const listeners = new Set<() => void>();

function publish(next: AddPageRequest | null) {
  request = next;
  for (const listener of listeners) listener();
}

export function openAddPageWindow(next: AddPageRequest) {
  publish(next);
}

export function closeAddPageWindow() {
  publish(null);
}

export function useAddPageWindowRequest() {
  return useSyncExternalStore((listener) => {
    listeners.add(listener);
    return () => listeners.delete(listener);
  }, () => request, () => null);
}
