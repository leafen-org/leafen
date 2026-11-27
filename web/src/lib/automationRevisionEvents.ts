// A database's automations live outside its page's graph: once one is made,
// changed, turned on or off, paused or deleted — on another device, by the
// API or an import — the server tells the page's room the automations'
// revision (database_automation_room_word.go), and the screens showing the
// page's databases read their automations again: the toolbar's count
// (databaseAutomationCount.ts) and an open list (useAutomationListReadAgain).
// Notion's open phone settings showed "자동화 1", and its open list
// "중단됨", within seconds of the change made on a desktop (seen 2026-11-18
// on a Galaxy A90 at 411); a phone here said none until it was reloaded.

const AUTOMATION_REVISION_EVENT = "leafen:automation-revision";

type AutomationRevision = { pageId: string; revision: number };

/** The automations' revision the server's own awareness state tells the room, if it tells one. */
export function announcedAutomationRevision(state: unknown) {
  const revision = (state as { leafenAutomations?: { revision?: unknown } } | null)?.leafenAutomations?.revision;
  return typeof revision === "number" && Number.isSafeInteger(revision) && revision > 0 ? revision : 0;
}

/** Tells the screens showing a page's databases that their automations changed. */
export function announceAutomationRevision(pageId: string, revision: number) {
  if (typeof window === "undefined") return;
  window.dispatchEvent(new CustomEvent<AutomationRevision>(AUTOMATION_REVISION_EVENT, { detail: { pageId, revision } }));
}

/** Follows the changes a page's room tells of its databases' automations. */
export function onAutomationRevision(pageId: string, listener: () => void): () => void {
  if (typeof window === "undefined") return () => {};
  const handle = (event: Event) => {
    if ((event as CustomEvent<AutomationRevision>).detail?.pageId === pageId) listener();
  };
  window.addEventListener(AUTOMATION_REVISION_EVENT, handle);
  return () => window.removeEventListener(AUTOMATION_REVISION_EVENT, handle);
}
