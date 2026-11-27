import { useSyncExternalStore } from "react";

// The app asks every few seconds whether the workspace changed
// (useWorkspaceChangeFollow). Some signs of a change come sooner — the server
// closing an open page's connection when someone's access to it changed —
// and ask at once.

const WORKSPACE_CHANGE_CHECK_EVENT = "leafen:workspace-change-check";

/** Asks the workspace's change follow to check now rather than at its next turn. */
export function requestWorkspaceChangeCheck() {
  if (typeof window === "undefined") return;
  window.dispatchEvent(new Event(WORKSPACE_CHANGE_CHECK_EVENT));
}

/** Follows the requests to check for a change now. */
export function onWorkspaceChangeCheck(listener: () => void): () => void {
  if (typeof window === "undefined") return () => {};
  window.addEventListener(WORKSPACE_CHANGE_CHECK_EVENT, listener);
  return () => window.removeEventListener(WORKSPACE_CHANGE_CHECK_EVENT, listener);
}

// What the person may do there may have changed — their sharing, role or
// organization, as the change check says — so the open page is read again.
// A change of content alone doesn't: the page's own changes reach it through
// its room.
let accessRevision = 0;
const accessListeners = new Set<() => void>();

/** Says the person's access may have changed. */
export function announceAccessChange() {
  accessRevision += 1;
  for (const listener of accessListeners) listener();
}

/** Follows each time the person's access may have changed. */
export const followAccess = (listener: () => void) => {
  accessListeners.add(listener);
  return () => { accessListeners.delete(listener); };
};

/** A count that moves each time the person's access may have changed. */
export function useAccessRevision(): number {
  return useSyncExternalStore(followAccess, () => accessRevision, () => 0);
}
