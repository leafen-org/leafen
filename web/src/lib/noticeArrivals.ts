// A new notice for the person reaches an open screen through its waiting
// workspace check (useWorkspaceChangeFollow, workspace_page_list_wait.go):
// the inbox's badge and an open inbox read again at once. A notice the badge
// learns of another way — a comment on the open page, the app coming back,
// its half-minute ask — reaches an open inbox as the badge's count changing
// (2026-10-26: a notice that came while the tab was hidden showed in the badge
// and not in the open inbox; 2026-10-27: a withdrawn mention left the badge
// and stayed in the open inbox).

const NOTICE_ARRIVAL_EVENT = "leafen:notice-arrival";
const UNREAD_CHANGE_EVENT = "leafen:inbox-unread-change";

function announce(name: string, workspaceId: string) {
  if (typeof window === "undefined") return;
  window.dispatchEvent(new CustomEvent<string>(name, { detail: workspaceId }));
}

function follow(name: string, listener: (workspaceId: string) => void): () => void {
  if (typeof window === "undefined") return () => {};
  const handle = (event: Event) => {
    const detail = (event as CustomEvent<string>).detail;
    if (typeof detail === "string") listener(detail);
  };
  window.addEventListener(name, handle);
  return () => window.removeEventListener(name, handle);
}

/** Tells the inbox readers a notice came for the person in a workspace. */
export function announceNoticeArrival(workspaceId: string) {
  announce(NOTICE_ARRIVAL_EVENT, workspaceId);
}

/** Follows the notices that come for the person, by their workspace. */
export function onNoticeArrival(listener: (workspaceId: string) => void): () => void {
  return follow(NOTICE_ARRIVAL_EVENT, listener);
}

/** Tells an open inbox the badge found another unread count than it had. */
export function announceUnreadCountChange(workspaceId: string) {
  announce(UNREAD_CHANGE_EVENT, workspaceId);
}

/** Follows the badge's count changing, by workspace. */
export function onUnreadCountChange(listener: (workspaceId: string) => void): () => void {
  return follow(UNREAD_CHANGE_EVENT, listener);
}
