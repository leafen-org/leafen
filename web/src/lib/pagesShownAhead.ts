import type { Bootstrap, Page } from "@/types";

/**
 * Pages shown here as they will be once the server has taken a change in
 * several steps — a page made as a full-page database, an inline database
 * turned into one — as a page list read meanwhile shows them: the list read
 * between the steps (the workspace's change probe reads it once the server
 * made the page) showed the new page as the empty page the server held,
 * "새 페이지" with a page's icon, until its last step (2026-11-13, NAS
 * condition: a turned database's block flickered for half a second).
 * Each holds until `until` settles; the list read after it is the server's.
 */
const shownAhead = new Map<string, Partial<Pick<Page, "kind" | "title">>>();

/** Shows `pageId` as `shown` in the page lists read until `until` settles. */
export function showPageAhead(pageId: string, shown: Partial<Pick<Page, "kind" | "title">>, until: Promise<unknown>) {
  shownAhead.set(pageId, shown);
  const done = () => { if (shownAhead.get(pageId) === shown) shownAhead.delete(pageId); };
  void until.then(done, done);
}

/** A page list read from the server, with the pages shown ahead as they show. */
export function withPagesShownAhead(list: Bootstrap): Bootstrap {
  if (!shownAhead.size || !list.pages.some(({ id }) => shownAhead.has(id))) return list;
  return { ...list, pages: list.pages.map((page) => {
    const shown = shownAhead.get(page.id);
    return shown ? { ...page, ...shown } : page;
  }) };
}
