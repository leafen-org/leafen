import type { Page } from "@/types";

/**
 * Whether a page is drawn as its database's page (StandaloneRowPage), whole
 * or in a peek, with its properties and the page menu: a wiki's page, as
 * Notion's wiki pages. That screen reads the page's graph and opens its room.
 */
export function pageDrawnAsRowPage(page: Pick<Page, "wikiDataSourceId" | "isWiki"> | null | undefined): boolean {
  return Boolean(page?.wikiDataSourceId) && !page?.isWiki;
}

/**
 * The open page whose body the product's page view draws, and so edits
 * itself — its graph read and its room (usePageBodyEditing) — or null when
 * its database's page draws it: that screen's body is the page's one room.
 * Both opened it, a wiki's page opened whole had two connections to the page
 * and its graph read twice, one nobody drew (2026-11-18). A database's page
 * at its own address is in no page list, and is its screen's alone too.
 */
export function pageBodyOfPageView<P extends Pick<Page, "wikiDataSourceId" | "isWiki">>(page: P | null): P | null {
  return page && !pageDrawnAsRowPage(page) ? page : null;
}
