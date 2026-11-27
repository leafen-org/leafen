// A workspace came or went outside the screens that keep the list — a tool
// import's place for its bundle, deleted as the import ends — so the list is
// read again rather than offering a workspace that is gone (2026-11-07: the
// menu kept "Obsidian에서 옮기는 중", and choosing it failed three ways).

const WORKSPACE_LIST_CHANGED_EVENT = "leafen:workspace-list-changed";

export function announceWorkspaceListChanged() {
  if (typeof window === "undefined") return;
  window.dispatchEvent(new Event(WORKSPACE_LIST_CHANGED_EVENT));
}

export function onWorkspaceListChanged(listener: () => void): () => void {
  if (typeof window === "undefined") return () => {};
  window.addEventListener(WORKSPACE_LIST_CHANGED_EVENT, listener);
  return () => window.removeEventListener(WORKSPACE_LIST_CHANGED_EVENT, listener);
}
