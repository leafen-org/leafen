import { useSyncExternalStore } from "react";

/**
 * The workspace this page went into from the switcher's menu, until the page
 * loads again. Notion's switcher says "게스트" after a guest workspace's name
 * only then: chosen from its menu the tag shows, and every load after that,
 * or a guest workspace a link or the address opens, shows the name alone
 * (seen 2026-11-12, at 1440 and 1024 wide). A switch to another account
 * starts the page over; that start takes its workspace from the switch's
 * request and says so here (useAuthenticatedBootstrap.ts).
 */
let entered = "";
const listeners = new Set<() => void>();

export function markSwitcherEntry(workspaceId: string): void {
  if (entered === workspaceId) return;
  entered = workspaceId;
  for (const listener of listeners) listener();
}

function subscribe(listener: () => void): () => void {
  listeners.add(listener);
  return () => { listeners.delete(listener); };
}

export function useEnteredFromSwitcher(workspaceId: string | undefined): boolean {
  const current = useSyncExternalStore(subscribe, () => entered);
  return !!workspaceId && current === workspaceId;
}
