/**
 * A page turned into a wiki, or back, changes more than itself: its
 * sub-pages become its wiki database's pages, or pages again, and the page
 * gains its database or loses it.
 */
export const WIKI_CHANGED_EVENT = "leafen:wiki-changed";

export function announceWikiChange(pageId: string) {
  window.dispatchEvent(new CustomEvent(WIKI_CHANGED_EVENT, { detail: { pageId } }));
}

const newWikis = new Set<string>();

/**
 * A page this person just asked to turn into a wiki, whose wiki greets them
 * when it first draws. It is marked at the ask and forgotten if refused: the
 * wiki may draw before the job is answered, its page read again meanwhile.
 */
export function markNewWiki(pageId: string) {
  newWikis.add(pageId);
}

/** Whether the wiki was just made here and hasn't greeted yet. */
export function isNewWiki(pageId: string) {
  return newWikis.has(pageId);
}

export function forgetNewWiki(pageId: string) {
  newWikis.delete(pageId);
}
