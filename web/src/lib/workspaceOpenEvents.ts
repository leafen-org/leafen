// Another part of the app asks the product to open a workspace it may not
// list yet, as an app's screen opens the app's own workspace: the product
// reads its lists again, then opens it.

const WORKSPACE_OPEN_EVENT = "leafen:open-workspace";

export function announceWorkspaceOpen(workspaceId: string) {
  if (typeof window === "undefined" || !workspaceId) return;
  window.dispatchEvent(new CustomEvent<string>(WORKSPACE_OPEN_EVENT, { detail: workspaceId }));
}

export function onWorkspaceOpenRequest(listener: (workspaceId: string) => void): () => void {
  if (typeof window === "undefined") return () => {};
  const handle = (event: Event) => {
    const workspaceId = (event as CustomEvent<string>).detail;
    if (typeof workspaceId === "string" && workspaceId) listener(workspaceId);
  };
  window.addEventListener(WORKSPACE_OPEN_EVENT, handle);
  return () => window.removeEventListener(WORKSPACE_OPEN_EVENT, handle);
}
