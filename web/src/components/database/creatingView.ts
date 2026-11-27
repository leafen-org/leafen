import { useSyncExternalStore } from "react";
import { viewDisplayName, viewOwnName } from "./databaseViewShared";

// The view whose "새 보기" settings are open: its tab reads "새 보기" until
// they close, unless it has been named, as Notion's (seen 2026-10-30); closed
// unnamed, it is called by its layout again.

let creating: string | null = null;
const listeners = new Set<() => void>();

export function setCreatingView(viewId: string | null) {
  creating = viewId;
  for (const listener of listeners) listener();
}

function subscribe(listener: () => void) {
  listeners.add(listener);
  return () => { listeners.delete(listener); };
}

/** The view being made in its settings, if any. */
export function useCreatingView(): string | null {
  return useSyncExternalStore(subscribe, () => creating, () => null);
}

/** A view tab's words: "새 보기" for the view being made while it keeps its layout's name. */
export function viewTabName(view: { id: string; name: string; type: string; config?: Record<string, unknown> }, creating: string | null, newViewLabel: string) {
  const name = viewDisplayName(view);
  return creating === view.id && !viewOwnName(view) ? newViewLabel : name;
}
