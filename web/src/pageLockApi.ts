import { request } from "./apiTransport";
import type { Session } from "./types";

/** Whether a page is locked, and whether this person unlocked it for themselves. */
export type PageLockState = { locked: boolean; unlockedForMe: boolean };

/** The page lock endpoints: reading a page's lock and unlocking it for oneself. */
export function createPageLockApi() {
  return {
    pageLock: (session: Session, pageId: string, options: { signal?: AbortSignal } = {}) => request<PageLockState>(
      `/pages/${encodeURIComponent(pageId)}/lock`,
      { signal: options.signal },
      session,
    ),
    setPageLockForMe: (session: Session, pageId: string, unlocked: boolean) => request<PageLockState>(
      `/pages/${encodeURIComponent(pageId)}/lock/me`,
      { method: "PUT", body: JSON.stringify({ unlocked }) },
      session,
    ),
  };
}
