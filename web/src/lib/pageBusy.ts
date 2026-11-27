import { useSyncExternalStore } from "react";

// A page an operation holds for a while — copied, its blocks moved, turned
// into a wiki, deleted for good — takes no edits until it is done. The page
// on screen says which, rather than that the person may no longer edit, and
// offers no edit request (2026-10-16).
let busy: { pageId: string; reason: string } | null = null;
const listeners = new Set<() => void>();
const follow = (listener: () => void) => {
  listeners.add(listener);
  return () => { listeners.delete(listener); };
};

/** Says the page is held by an operation (a reason), or no longer (null). */
export function setPageBusy(pageId: string, reason: string | null) {
  const next = reason ? { pageId, reason } : busy?.pageId === pageId ? null : busy;
  if (next?.pageId === busy?.pageId && next?.reason === busy?.reason) return;
  busy = next;
  for (const listener of listeners) listener();
}

/** The operation holding the page, if any. */
export function usePageBusy(pageId: string | null | undefined): string | null {
  return useSyncExternalStore(follow, () => (pageId && busy?.pageId === pageId ? busy.reason : null), () => null);
}

/**
 * A body copy's own databases being copied into the page, after the rest of
 * it (lib/pageDatabaseCopies.ts): its body shows as being filled until then.
 */
export const pageBusyCopyingDatabases = "copyingDatabases";

/**
 * Whether a request was refused because an operation holds its page for a
 * while (the server's 423, page_busy.go): what it asked goes again once the
 * operation is done, rather than as for a page not found. The one rule that
 * parts a 423 by its code: any other is a page's or a database's lock's
 * refusal (lock_refusal.go), which stands until someone unlocks it, so what
 * it refused is told and goes no more.
 */
export function isPageBusyRefusal(error: unknown): boolean {
  const refusal = error as { code?: unknown; status?: unknown } | null;
  return Boolean(refusal) && refusal?.status === 423 && refusal.code === "LEAFEN_PAGE_BUSY";
}

// A read of a page open here refused while an operation holds it — one
// following its changes, one reading it again — holds the page as its own
// read's refusal does (useLocalFirstContentGraph): it asks whether the
// operation is done and is read again once then. Each such read asked again
// as the page's room told of the operation's changes, every refusal an
// error nobody caught (2026-11-14: a page button adding 203 blocks drew more
// than ten refused reads in half a second, each an error in the console).
const holdsHeard = new Set<(pageId: string) => void>();

/** Hears the pages a read was refused for while an operation holds them; returns what stops it. */
export function hearPageHeld(listener: (pageId: string) => void): () => void {
  holdsHeard.add(listener);
  return () => { holdsHeard.delete(listener); };
}

/** A read of a page refused while an operation holds it: the page is told it is held, and the read comes to nothing (null); any other refusal stays one. */
export function heldRead(pageId: string, error: unknown): null {
  if (!isPageBusyRefusal(error)) throw error;
  for (const listener of holdsHeard) listener(pageId);
  return null;
}

/**
 * A read whose refusal for a hold is kept while it opens the page (`opening`):
 * a read the page's writes settle first (readSettled) turns that refusal into
 * nothing (heldRead), and a page opening read so would say it couldn't open,
 * its server unknown, rather than wait for the hold as it opens (2026-11-23: a
 * page whose blocks were being moved said "열지 못했습니다. 연결이 돌아오면
 * 엽니다." beside a red "원인을 확인할 수 없습니다"). A page already shown, read
 * again, keeps none: it comes to nothing there, and the page, told it is held,
 * stays as it is — its text as writable as it was — and is read again once
 * the hold ends; kept, the refusal took the page's writing away while the
 * hold lasted (2026-11-23: a line sent to another page with "/페이지 위치:",
 * then ⌘Z and a letter at once — the title, read-only as the caret came back
 * to it, lost the letter and the caret). `kept()` is the refusal met, or null.
 */
export function keepingHeldRefusal<Value>(read: () => Promise<Value>, opening: boolean): { read: () => Promise<Value>; kept: () => unknown } {
  let refused: unknown = null;
  return {
    read: () => read().catch((error: unknown) => {
      if (opening && isPageBusyRefusal(error)) refused = error;
      throw error;
    }),
    kept: () => refused,
  };
}

/** The page's notice for a reason. */
export function pageBusyNoticeKey(reason: string): string {
  if (reason === pageBusyCopyingDatabases) return "syncStatusBadge:pageBusy.copying";
  return ["copying", "moving", "wiki", "deleting", "workspaceMoved", "workspaceSending", "workspaceMoving"].includes(reason) ? `syncStatusBadge:pageBusy.${reason}` : "syncStatusBadge:pageBusy.other";
}
